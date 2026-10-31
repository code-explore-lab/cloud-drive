package model

import (
	"context"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ UploadPartsModel = (*customUploadPartsModel)(nil)

type (
	// UploadPartsModel is an interface to be customized, add more methods here,
	// and implement the added methods in customUploadPartsModel.
	UploadPartsModel interface {
		uploadPartsModel
		// UpsertPart 以 (upload_id, part_number) 为唯一键写入分片记录。
		// 同号分片重传时覆盖原记录，因此"重传"天然幂等。
		UpsertPart(ctx context.Context, part *UploadParts) error
		// FindByUploadId 返回某个上传任务下所有有效分片，按分片号升序。
		FindByUploadId(ctx context.Context, uploadId string) ([]*UploadParts, error)
		// DeleteByUploadId 删除某个上传任务的全部分片记录。
		DeleteByUploadId(ctx context.Context, uploadId string) error
	}

	customUploadPartsModel struct {
		*defaultUploadPartsModel
	}
)

// NewUploadPartsModel returns a model for the database table.
func NewUploadPartsModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) UploadPartsModel {
	return &customUploadPartsModel{
		defaultUploadPartsModel: newUploadPartsModel(conn, c, opts...),
	}
}

// UpsertPart 用 MySQL 的 ON DUPLICATE KEY UPDATE 实现分片记录幂等写入。
// 分片表没有单列唯一键，因此不走缓存，直接写库。
func (m *customUploadPartsModel) UpsertPart(ctx context.Context, part *UploadParts) error {
	_, err := m.ExecNoCacheCtx(ctx,
		"insert into `upload_parts` (`upload_id`,`part_number`,`size`,`content_hash`,`etag`,`object_part_id`,`status`) "+
			"values (?,?,?,?,?,?,1) "+
			"on duplicate key update `size`=values(`size`), `content_hash`=values(`content_hash`), "+
			"`etag`=values(`etag`), `object_part_id`=values(`object_part_id`), `status`=1",
		part.UploadId, part.PartNumber, part.Size, part.ContentHash, part.Etag, part.ObjectPartId,
	)
	return err
}

// FindByUploadId 查询某个上传任务的全部分片。
func (m *customUploadPartsModel) FindByUploadId(ctx context.Context, uploadId string) ([]*UploadParts, error) {
	var parts []*UploadParts
	err := m.QueryRowsNoCacheCtx(ctx, &parts,
		"select `id`,`upload_id`,`part_number`,`size`,`content_hash`,`etag`,`object_part_id`,`status`,"+
			"`created_at`,`updated_at` from `upload_parts` "+
			"where `upload_id` = ? and `status` = 1 order by `part_number` asc",
		uploadId)
	if err != nil {
		return nil, err
	}
	return parts, nil
}

// DeleteByUploadId 清理某个上传任务的分片记录。
func (m *customUploadPartsModel) DeleteByUploadId(ctx context.Context, uploadId string) error {
	_, err := m.ExecNoCacheCtx(ctx,
		"delete from `upload_parts` where `upload_id` = ?", uploadId)
	return err
}
