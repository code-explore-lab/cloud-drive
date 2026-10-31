package model

import (
	"context"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ StorageObjectsModel = (*customStorageObjectsModel)(nil)

type (
	// StorageObjectsModel is an interface to be customized, add more methods here,
	// and implement the added methods in customStorageObjectsModel.
	StorageObjectsModel interface {
		storageObjectsModel
		// FindByStatus 按状态批量取对象，供后台 GC 与对账使用。
		FindByStatus(ctx context.Context, status string, limit int64) ([]*StorageObjects, error)
		// CountByStatus 统计某个状态的对象数量，用于暴露指标。
		CountByStatus(ctx context.Context, status string) (int64, error)
	}

	customStorageObjectsModel struct {
		*defaultStorageObjectsModel
	}
)

// NewStorageObjectsModel returns a model for the database table.
func NewStorageObjectsModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) StorageObjectsModel {
	return &customStorageObjectsModel{
		defaultStorageObjectsModel: newStorageObjectsModel(conn, c, opts...),
	}
}

// FindByStatus 按状态批量查询对象，结果按 id 升序，便于分页重试。
func (m *customStorageObjectsModel) FindByStatus(ctx context.Context, status string,
	limit int64) ([]*StorageObjects, error) {
	var objects []*StorageObjects
	err := m.QueryRowsNoCacheCtx(ctx, &objects,
		"select `id`,`object_key`,`backend`,`bucket_name`,`size`,`content_type`,`content_hash`,"+
			"`hash_verified`,`status`,`gc_attempts`,`last_error`,`deleted_at`,`created_at`,`updated_at` "+
			"from `storage_objects` where `status` = ? order by `id` asc limit ?",
		status, limit)
	if err != nil {
		return nil, err
	}
	return objects, nil
}

// CountByStatus 统计某个状态的对象数量。
func (m *customStorageObjectsModel) CountByStatus(ctx context.Context, status string) (int64, error) {
	var count int64
	err := m.QueryRowNoCacheCtx(ctx, &count,
		"select count(*) from `storage_objects` where `status` = ?", status)
	return count, err
}
