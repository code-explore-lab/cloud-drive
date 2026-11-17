package model

import (
	"context"
	"fmt"

	"github.com/zeromicro/go-zero/core/logx"
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
		// Invalidate 失效按 id 与 object_key 建立的两份缓存副本。
		// 给绕过 model 的定向 UPDATE（事务内改状态、worker 标记完成/失败）在提交后调用。
		Invalidate(ctx context.Context, id uint64, objectKey string)
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

// Invalidate 删除对象在缓存里的两份副本（id 与 object_key 各一份）。
// 缓存失效失败只影响读到旧数据的窗口长度，不应让已经提交的业务失败。
func (m *customStorageObjectsModel) Invalidate(ctx context.Context, id uint64, objectKey string) {
	err := m.DelCacheCtx(ctx,
		fmt.Sprintf("%s%v", cacheStorageObjectsIdPrefix, id),
		fmt.Sprintf("%s%v", cacheStorageObjectsObjectKeyPrefix, objectKey))
	if err != nil {
		logx.WithContext(ctx).Errorf("invalidate storage object cache failed: id=%d object_key=%s: %v",
			id, objectKey, err)
	}
}
