package model

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ FilesModel = (*customFilesModel)(nil)

type (
	// FilesModel is an interface to be customized, add more methods here,
	// and implement the added methods in customFilesModel.
	FilesModel interface {
		filesModel
		// FindByOwnerParent 按 owner + parent 分页查询未删除的文件。
		FindByOwnerParent(ctx context.Context, ownerId, parentId uint64, offset, limit int64) ([]*Files, error)
		// CountByOwnerParent 统计某个目录下未删除的文件数量。
		CountByOwnerParent(ctx context.Context, ownerId, parentId uint64) (int64, error)
		// FindAllByOwnerParent 返回某个目录下的全部未删除文件（目录删除时遍历用）。
		FindAllByOwnerParent(ctx context.Context, ownerId, parentId uint64) ([]*Files, error)
		// MarkDeleted 批量软删除文件，并失效对应的缓存。
		MarkDeleted(ctx context.Context, files []*Files, now time.Time) error
		// UpdateContent 把文件指向新的版本内容（AppendVersion 内部使用）。
		UpdateContent(ctx context.Context, fileId uint64, version uint64, size uint64,
			contentType, contentHash, objectKey string) error
	}

	customFilesModel struct {
		*defaultFilesModel
	}
)

// NewFilesModel returns a model for the database table.
func NewFilesModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) FilesModel {
	return &customFilesModel{
		defaultFilesModel: newFilesModel(conn, c, opts...),
	}
}

// FindByOwnerParent 按 owner + parent 分页查询未删除的文件。
// 列表查询不缓存，避免分页缓存键爆炸和一致性维护成本。
func (m *customFilesModel) FindByOwnerParent(ctx context.Context, ownerId, parentId uint64,
	offset, limit int64) ([]*Files, error) {
	var files []*Files
	err := m.QueryRowsNoCacheCtx(ctx, &files,
		"select `id`,`owner_id`,`parent_id`,`name`,`size`,`content_type`,`content_hash`,"+
			"`object_key`,`version`,`status`,`request_id`,`created_at`,`updated_at`,`deleted_at` from `files` "+
			"where `owner_id` = ? and `parent_id` = ? and `deleted_at` is null "+
			"order by `name` asc limit ? offset ?",
		ownerId, parentId, limit, offset)
	if err != nil {
		return nil, err
	}
	return files, nil
}

// CountByOwnerParent 统计某个目录下未删除的文件数量。
func (m *customFilesModel) CountByOwnerParent(ctx context.Context, ownerId, parentId uint64) (int64, error) {
	var count int64
	err := m.QueryRowNoCacheCtx(ctx, &count,
		"select count(*) from `files` where `owner_id` = ? and `parent_id` = ? and `deleted_at` is null",
		ownerId, parentId)
	return count, err
}

// FindAllByOwnerParent 返回目录下的全部文件，用于递归删除目录。
func (m *customFilesModel) FindAllByOwnerParent(ctx context.Context, ownerId,
	parentId uint64) ([]*Files, error) {
	var files []*Files
	err := m.QueryRowsNoCacheCtx(ctx, &files,
		"select `id`,`owner_id`,`parent_id`,`name`,`size`,`content_type`,`content_hash`,"+
			"`object_key`,`version`,`status`,`request_id`,`created_at`,`updated_at`,`deleted_at` from `files` "+
			"where `owner_id` = ? and `parent_id` = ? and `deleted_at` is null order by `id` asc",
		ownerId, parentId)
	if err != nil {
		return nil, err
	}
	return files, nil
}

// MarkDeleted 用一条 UPDATE 批量软删除，同时失效主键缓存与幂等键缓存。
func (m *customFilesModel) MarkDeleted(ctx context.Context, files []*Files, now time.Time) error {
	if len(files) == 0 {
		return nil
	}

	keys := make([]string, 0, len(files)*2)
	placeholders := make([]string, 0, len(files))
	args := make([]any, 0, len(files)+1)
	args = append(args, sql.NullTime{Time: now, Valid: true})
	for _, f := range files {
		placeholders = append(placeholders, "?")
		args = append(args, f.Id)
		keys = append(keys, fmt.Sprintf("%s%v", cacheFilesIdPrefix, f.Id))
		if f.RequestId.Valid {
			keys = append(keys, fmt.Sprintf("%s%v:%v", cacheFilesOwnerIdRequestIdPrefix,
				f.OwnerId, f.RequestId))
		}
	}

	_, err := m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (sql.Result, error) {
		query := "update `files` set `deleted_at` = ?, `status` = 'deleting' where `id` in (" +
			joinPlaceholders(placeholders) + ")"
		return conn.ExecCtx(ctx, query, args...)
	}, keys...)
	return err
}

// UpdateContent 更新文件当前内容。调用方需自行保证并发（见 AppendVersion）。
func (m *customFilesModel) UpdateContent(ctx context.Context, fileId, version, size uint64,
	contentType, contentHash, objectKey string) error {
	_, err := m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (sql.Result, error) {
		return conn.ExecCtx(ctx,
			"update `files` set `size` = ?, `content_type` = ?, `content_hash` = ?, "+
				"`object_key` = ?, `version` = ?, `status` = 'available' where `id` = ?",
			size, contentType, contentHash, objectKey, version, fileId)
	}, fmt.Sprintf("%s%v", cacheFilesIdPrefix, fileId))
	return err
}

func joinPlaceholders(items []string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}
