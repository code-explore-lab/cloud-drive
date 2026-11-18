package model

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// objectErrorMaxLen 与 storage_objects.last_error 的列宽一致。
const objectErrorMaxLen = 512

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
		// MarkDeleted 把 deleting 对象标记为 deleted，返回实际更新的行数。
		// 带 status='deleting' 条件，已经被 GC 处理过的行不会被再次覆盖。
		MarkDeleted(ctx context.Context, id uint64, objectKey string) (int64, error)
		// BumpGCAttempts 原子累加 gc_attempts 并记录最后错误，返回实际更新的行数。
		// 同样只在 status='deleting' 时生效，避免把 GC 已处理的对象改回 deleting 的语义。
		BumpGCAttempts(ctx context.Context, id uint64, objectKey, lastError string) (int64, error)
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
	if err := m.DelCacheCtx(ctx, m.objectCacheKeys(id, objectKey)...); err != nil {
		logx.WithContext(ctx).Errorf("invalidate storage object cache failed: id=%d object_key=%s: %v",
			id, objectKey, err)
	}
}

// MarkDeleted 只更新删除流程需要的列，并把条件收紧到 status='deleting'。
// 相比整行 Update，它不会覆盖 GC 并发写入的 gc_attempts / last_error。
func (m *customStorageObjectsModel) MarkDeleted(ctx context.Context, id uint64,
	objectKey string) (int64, error) {
	result, err := m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (sql.Result, error) {
		return conn.ExecCtx(ctx,
			"update `storage_objects` set `status` = 'deleted', `deleted_at` = now(), "+
				"`last_error` = '' where `id` = ? and `status` = 'deleting'", id)
	}, m.objectCacheKeys(id, objectKey)...)
	// go-zero 的约定：DB 成功但缓存失效失败时，result 非 nil、err 也非 nil。
	// 此时写库已经生效，不能当失败重试，否则会重复累加。
	if result == nil {
		return 0, err
	}
	if err != nil {
		logx.WithContext(ctx).Errorf("invalidate storage object cache failed: id=%d object_key=%s: %v",
			id, objectKey, err)
	}
	return result.RowsAffected()
}

// BumpGCAttempts 用 SQL 自增而不是「读出 + 加一 + 整行写回」，
// 避免 worker 与 GC 并发时丢计数。
func (m *customStorageObjectsModel) BumpGCAttempts(ctx context.Context, id uint64,
	objectKey, lastError string) (int64, error) {
	result, err := m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (sql.Result, error) {
		return conn.ExecCtx(ctx,
			"update `storage_objects` set `gc_attempts` = `gc_attempts` + 1, `last_error` = ? "+
				"where `id` = ? and `status` = 'deleting'",
			truncate(lastError, objectErrorMaxLen), id)
	}, m.objectCacheKeys(id, objectKey)...)
	// 同 MarkDeleted：缓存失效失败不代表数据库更新失败。
	if result == nil {
		return 0, err
	}
	if err != nil {
		logx.WithContext(ctx).Errorf("invalidate storage object cache failed: id=%d object_key=%s: %v",
			id, objectKey, err)
	}
	return result.RowsAffected()
}

// objectCacheKeys 返回同一对象的两份缓存 key。
func (m *customStorageObjectsModel) objectCacheKeys(id uint64, objectKey string) []string {
	return []string{
		fmt.Sprintf("%s%v", cacheStorageObjectsIdPrefix, id),
		fmt.Sprintf("%s%v", cacheStorageObjectsObjectKeyPrefix, objectKey),
	}
}
