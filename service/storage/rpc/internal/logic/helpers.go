package logic

import (
	"time"

	"cloud-drive/common/objkey"
	"cloud-drive/service/storage/model"
)

const (
	// defaultChunkSize 取 8MiB：既高于 MinIO 服务端合并要求的 5MiB 下限，也不会让分片元数据过多。
	defaultChunkSize = 8 << 20
	// maxTotalChunks 与 MinIO 单对象最大分片数保持一致（10000）。
	maxTotalChunks = 10000
	// uploadTTL 是未完成上传任务的保留时长，过期任务由后台清理任务回收。
	uploadTTL = 24 * time.Hour
	// defaultContentType 对应 upload_tasks.content_type 的库默认值。
	defaultContentType = "application/octet-stream"
)

// objectKeyFor 生成最终对象的 Key（见 common/objkey）。
func objectKeyFor(userId uint64, uploadId, fileName string) string {
	return objkey.Object(userId, uploadId, fileName)
}

// partObjectKey 是分片临时对象的 Key，合并成功后由完成流程清理。
func partObjectKey(uploadId string, partNumber int32) string {
	return objkey.Part(uploadId, int64(partNumber))
}

// expectedPartSize 计算某个分片应有的字节数：除最后一片外都是 chunkSize。
func expectedPartSize(task *model.UploadTasks, partNumber int32) int64 {
	if partNumber < int32(task.TotalChunks) {
		return int64(task.ChunkSize)
	}
	remain := int64(task.TotalSize) - int64(task.TotalChunks-1)*int64(task.ChunkSize)
	if remain < 0 {
		return 0
	}
	return remain
}

// truncate 把错误信息裁剪到列宽以内，避免写库失败。
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
