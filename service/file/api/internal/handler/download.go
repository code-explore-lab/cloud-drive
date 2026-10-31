// 文件下载相关的手写 handler：需要把上游的对象流原样转发给客户端，
// 因此直接在 handler 里写 ResponseWriter，而不是走 goctl 生成的 JSON 逻辑。
package handler

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"cloud-drive/common/ctxdata"
	"cloud-drive/common/metrics"
	"cloud-drive/common/web"
	"cloud-drive/service/file/api/internal/svc"
	"cloud-drive/service/file/rpc/fileclient"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
	"github.com/zeromicro/go-zero/rest/pathvar"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const internalTokenHeader = "X-Internal-Token"

// DownloadFileHandler 是受保护的文件下载：
// 先用文件服务校验归属，再通过内部接口从 storage-api 拉流。
// 客户端永远拿不到 objectKey，也拿不到可以直接访问对象存储的地址。
func DownloadFileHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userId, ok := ctxdata.UserIdFromCtx(ctx)
		if !ok {
			httpx.ErrorCtx(ctx, w, status.Error(codes.Unauthenticated, "unauthenticated"))
			return
		}

		fileId, err := strconv.ParseInt(pathvar.Vars(r)["fileId"], 10, 64)
		if err != nil || fileId <= 0 {
			httpx.ErrorCtx(ctx, w, status.Error(codes.InvalidArgument, "invalid file id"))
			return
		}

		f, err := svcCtx.FileRpc.GetFile(ctx, &fileclient.GetFileRequest{
			FileId: fileId,
			UserId: int64(userId),
		})
		if err != nil {
			httpx.ErrorCtx(ctx, w, err)
			return
		}
		if f.Status != "available" || f.ObjectKey == "" {
			httpx.ErrorCtx(ctx, w, status.Error(codes.FailedPrecondition, "file content is not available"))
			return
		}

		proxyObject(w, r, svcCtx, f.ObjectKey, f.Name, f.ContentType)
	}
}

// DownloadSharedFileHandler 是分享下载：先做分享校验（含密码与次数），再代理对象流。
// 分享 token 不是对象 Key，因此不存在"猜到地址就能拿到别人文件"的路径。
func DownloadSharedFileHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		token := pathvar.Vars(r)["token"]
		password := r.URL.Query().Get("password")
		if password == "" {
			password = r.Header.Get("X-Share-Password")
		}

		access, err := svcCtx.FileRpc.AccessShare(ctx, &fileclient.AccessShareRequest{
			Token:    token,
			Password: password,
		})
		if err != nil {
			httpx.ErrorCtx(ctx, w, err)
			return
		}
		if !access.Granted {
			httpx.ErrorCtx(ctx, w, status.Error(codes.PermissionDenied, access.Reason))
			return
		}

		proxyObject(w, r, svcCtx, access.ObjectKey, access.FileName, access.ContentType)
	}
}

// HealthHandler 是存活探针。
func HealthHandler(_ *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.OkJsonCtx(r.Context(), w, map[string]any{
			"status": "ok",
			"time":   time.Now().Unix(),
		})
	}
}

// proxyObject 把 storage-api 的对象流（支持 Range）原样转发给客户端。
//
// 只做字节搬运，不做缓冲：缓冲区固定 64KiB，因此下载 1GB 文件也只占 64KiB 内存。
func proxyObject(w http.ResponseWriter, r *http.Request, svcCtx *svc.ServiceContext,
	objectKey, filename, contentType string) {
	ctx := r.Context()

	target := svcCtx.StorageBase + "/internal/v1/storage/objects/content?objectKey=" +
		url.QueryEscape(objectKey) + "&filename=" + url.QueryEscape(filename)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		httpx.ErrorCtx(ctx, w, status.Error(codes.Internal, "failed to build storage request"))
		return
	}
	req.Header.Set(internalTokenHeader, svcCtx.Config.Internal.Token)
	if rg := r.Header.Get("Range"); rg != "" {
		req.Header.Set("Range", rg)
	}

	resp, err := svcCtx.StorageHTTP.Do(req)
	if err != nil {
		web.LogError(ctx, "request storage object failed", err)
		httpx.ErrorCtx(ctx, w, status.Error(codes.Unavailable, "storage service unavailable"))
		return
	}
	defer resp.Body.Close()

	// 上游的错误响应（404/416 等）原样透传，客户端看到的状态码才准确。
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		for _, header := range []string{"Content-Type", "Content-Range"} {
			if v := resp.Header.Get(header); v != "" {
				w.Header().Set(header, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(resp.Body, 8<<10))
		return
	}

	for _, header := range []string{
		"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Content-Disposition",
	} {
		if v := resp.Header.Get(header); v != "" {
			w.Header().Set(header, v)
		}
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(resp.StatusCode)

	buf := make([]byte, 64<<10)
	written, copyErr := io.CopyBuffer(w, resp.Body, buf)
	metrics.DownloadBytesTotal.Add(float64(written))
	if copyErr != nil {
		// 客户端断开是常态（取消下载），只记录不报错。
		logx.WithContext(ctx).Infof("download interrupted after %d bytes: %v", written, copyErr)
	}
}
