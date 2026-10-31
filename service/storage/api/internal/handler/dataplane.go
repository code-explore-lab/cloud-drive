// 数据面的两个流式接口在这里手写：
//   - 分片上传：需要直接读请求体，并边写对象存储边算摘要；
//   - 对象下载：需要直接写 ResponseWriter，支持 Range 断点续传。
//
// goctl 生成的 handler 走 httpx.Parse + JSON 序列化，这两类请求都不适用，
// 所以它们不进 .api 的路由声明，而是由 routes.go 手工注册。
package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cloud-drive/common/ctxdata"
	"cloud-drive/common/metrics"
	"cloud-drive/common/objectstore"
	"cloud-drive/common/objkey"
	"cloud-drive/common/web"
	"cloud-drive/service/storage/api/internal/svc"
	"cloud-drive/service/storage/rpc/storageclient"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
	"github.com/zeromicro/go-zero/rest/pathvar"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UploadPartHandler 接收一个分片的原始字节流。
//
// 顺序很重要：先校验任务归属与分片号（避免匿名写入），再落对象，最后记元数据。
// 任何一步失败都不会让任务进入"看起来完成其实缺数据"的状态。
func UploadPartHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userId, ok := ctxdata.UserIdFromCtx(ctx)
		if !ok {
			httpx.ErrorCtx(ctx, w, status.Error(codes.Unauthenticated, "unauthenticated"))
			return
		}

		vars := pathvar.Vars(r)
		uploadId := vars["uploadId"]
		partNumber, err := strconv.ParseInt(vars["partNumber"], 10, 32)
		if uploadId == "" || err != nil || partNumber <= 0 {
			httpx.ErrorCtx(ctx, w, status.Error(codes.InvalidArgument, "invalid upload id or part number"))
			return
		}

		task, err := svcCtx.StorageRpc.GetUploadTask(ctx, &storageclient.GetUploadTaskRequest{
			UploadId: uploadId,
			UserId:   int64(userId),
		})
		if err != nil {
			httpx.ErrorCtx(ctx, w, err)
			return
		}
		if task.Status != "initiated" && task.Status != "uploading" {
			httpx.ErrorCtx(ctx, w, status.Errorf(codes.FailedPrecondition,
				"upload task is %s", task.Status))
			return
		}
		if partNumber > task.TotalChunks {
			httpx.ErrorCtx(ctx, w, status.Errorf(codes.OutOfRange,
				"part number %d exceeds total chunks %d", partNumber, task.TotalChunks))
			return
		}

		expected := expectedPartSize(task.ChunkSize, task.TotalChunks, task.TotalSize, partNumber)
		if maxBytes := svcCtx.Config.MaxChunkBytes; maxBytes > 0 && expected > maxBytes {
			httpx.ErrorCtx(ctx, w, status.Errorf(codes.ResourceExhausted,
				"part size %d exceeds limit %d", expected, maxBytes))
			return
		}

		objectKey := objkey.Part(uploadId, partNumber)
		result, err := svcCtx.Store.PutStream(ctx, objectKey, r.Body, expected, "application/octet-stream")
		if err != nil {
			logx.WithContext(ctx).Errorf("write part object failed: upload_id=%s part=%d err=%v",
				uploadId, partNumber, err)
			httpx.ErrorCtx(ctx, w, status.Error(codes.InvalidArgument,
				"failed to store part, body size does not match the declared size"))
			return
		}

		// 客户端可能多发了字节，声明长度之外的流量必须显式拒绝，否则会被静默截断。
		if n, _ := io.CopyN(io.Discard, r.Body, 1); n > 0 {
			_ = svcCtx.Store.Remove(ctx, objectKey)
			httpx.ErrorCtx(ctx, w, status.Error(codes.InvalidArgument,
				"request body is larger than the declared part size"))
			return
		}

		recorded, err := svcCtx.StorageRpc.RecordPart(ctx, &storageclient.RecordPartRequest{
			UploadId:     uploadId,
			UserId:       int64(userId),
			PartNumber:   int32(partNumber),
			Size:         result.Size,
			Etag:         result.ETag,
			ContentHash:  result.SHA256,
			ObjectPartId: objectKey,
		})
		if err != nil {
			// 元数据没记上，分片对象成为孤儿。删除是尽力而为，失败由后台按前缀清理。
			_ = svcCtx.Store.Remove(ctx, objectKey)
			httpx.ErrorCtx(ctx, w, err)
			return
		}

		logx.WithContext(ctx).Infof("part uploaded: upload_id=%s part=%d size=%d progress=%d/%d",
			uploadId, partNumber, result.Size, recorded.UploadedChunks, task.TotalChunks)

		httpx.OkJsonCtx(ctx, w, map[string]any{
			"uploadId":    uploadId,
			"partNumber":  partNumber,
			"size":        result.Size,
			"etag":        result.ETag,
			"contentHash": result.SHA256,
			"progress": map[string]any{
				"uploadedChunks": recorded.UploadedChunks,
				"uploadedBytes":  recorded.UploadedBytes,
				"totalChunks":    task.TotalChunks,
			},
		})
	}
}

// DownloadObjectHandler 是内部下载接口：file-api 校验完权限后按 objectKey 取流。
// 支持 Range，因此断点续传和播放器拖动都能用；只接受内部令牌，不对外暴露。
func DownloadObjectHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		objectKey := r.URL.Query().Get("objectKey")
		if objectKey == "" {
			httpx.ErrorCtx(ctx, w, status.Error(codes.InvalidArgument, "objectKey is required"))
			return
		}

		info, err := svcCtx.Store.Stat(ctx, objectKey)
		if errors.Is(err, objectstore.ErrNotFound) {
			httpx.ErrorCtx(ctx, w, status.Error(codes.NotFound, "object not found"))
			return
		}
		if err != nil {
			logx.WithContext(ctx).Errorf("stat object failed: %v", err)
			httpx.ErrorCtx(ctx, w, status.Error(codes.Internal, "storage unavailable"))
			return
		}

		contentType := info.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Disposition", contentDisposition(r.URL.Query().Get("filename")))

		start, length, ranged, rangeErr := parseRange(r.Header.Get("Range"), info.Size)
		if rangeErr != nil {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", info.Size))
			web.WriteError(ctx, w, http.StatusRequestedRangeNotSatisfiable, "invalid_range", rangeErr.Error())
			return
		}
		if !ranged {
			start, length = 0, info.Size
		}

		reader, err := svcCtx.Store.OpenRange(ctx, objectKey, start, length)
		if err != nil {
			httpx.ErrorCtx(ctx, w, status.Error(codes.Internal, "failed to open object"))
			return
		}
		defer reader.Close()

		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		if ranged {
			w.Header().Set("Content-Range",
				fmt.Sprintf("bytes %d-%d/%d", start, start+length-1, info.Size))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		// 64KiB 缓冲区：避免大文件下载时的系统调用次数，也不会把整个文件读进内存。
		buf := make([]byte, 64<<10)
		written, copyErr := io.CopyBuffer(w, reader, buf)
		metrics.DownloadBytesTotal.Add(float64(written))
		if copyErr != nil {
			logx.WithContext(ctx).Errorf("stream object %s interrupted after %d bytes: %v",
				objectKey, written, copyErr)
		}
	}
}

// HealthHandler 是存活探针：只要进程能响应就算活着。
func HealthHandler(_ *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.OkJsonCtx(r.Context(), w, map[string]any{
			"status": "ok",
			"time":   time.Now().Unix(),
		})
	}
}

// ReadyHandler 是就绪探针：依赖不可用时返回 503，避免把请求路由到坏实例。
func ReadyHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := contextWithTimeout(r, 2*time.Second)
		defer cancel()

		if err := svcCtx.Store.Ping(ctx); err != nil {
			httpx.ErrorCtx(r.Context(), w, status.Errorf(codes.Unavailable, "object storage not ready"))
			return
		}
		httpx.OkJsonCtx(r.Context(), w, map[string]any{"status": "ready"})
	}
}

func expectedPartSize(chunkSize, totalChunks, totalSize, partNumber int64) int64 {
	if partNumber < totalChunks {
		return chunkSize
	}
	remain := totalSize - (totalChunks-1)*chunkSize
	if remain < 0 {
		return 0
	}
	return remain
}

// parseRange 解析单段 Range 请求头。返回 ranged=false 表示没有 Range 或 Range 覆盖整个对象。
func parseRange(header string, size int64) (start, length int64, ranged bool, err error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0, size, false, nil
	}
	const prefix = "bytes="
	if !strings.HasPrefix(header, prefix) {
		return 0, 0, false, fmt.Errorf("unsupported range unit")
	}
	spec := strings.TrimSpace(header[len(prefix):])
	if strings.Contains(spec, ",") {
		return 0, 0, false, fmt.Errorf("multiple ranges are not supported")
	}

	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false, fmt.Errorf("invalid range")
	}

	if parts[0] == "" {
		// bytes=-N：最后 N 个字节
		suffix, convErr := strconv.ParseInt(parts[1], 10, 64)
		if convErr != nil || suffix <= 0 {
			return 0, 0, false, fmt.Errorf("invalid suffix range")
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, suffix, true, nil
	}

	first, convErr := strconv.ParseInt(parts[0], 10, 64)
	if convErr != nil || first < 0 {
		return 0, 0, false, fmt.Errorf("invalid range start")
	}
	if first >= size {
		return 0, 0, false, fmt.Errorf("range start exceeds object size")
	}

	last := size - 1
	if parts[1] != "" {
		parsed, convErr := strconv.ParseInt(parts[1], 10, 64)
		if convErr != nil || parsed < first {
			return 0, 0, false, fmt.Errorf("invalid range end")
		}
		if parsed < last {
			last = parsed
		}
	}

	if first == 0 && last == size-1 {
		return 0, size, false, nil
	}
	return first, last - first + 1, true, nil
}

func contentDisposition(filename string) string {
	if filename == "" {
		return "attachment"
	}
	// 去掉控制字符和引号，避免头部注入。
	safe := strings.Map(func(r rune) rune {
		if r < 0x20 || r == '"' || r == '\\' {
			return -1
		}
		return r
	}, filename)
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, safe, urlEncode(safe))
}

func urlEncode(s string) string {
	var b strings.Builder
	for _, r := range []byte(s) {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' || r == '~' {
			b.WriteByte(r)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", r)
	}
	return b.String()
}

// contextWithTimeout 用于探针等需要硬超时的场景。
func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}
