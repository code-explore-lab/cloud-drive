package model

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ FoldersModel = (*customFoldersModel)(nil)

type (
	// FoldersModel is an interface to be customized, add more methods here,
	// and implement the added methods in customFoldersModel.
	FoldersModel interface {
		foldersModel
		// FindByOwnerParent 查询某个目录下的全部未删除子目录。
		FindByOwnerParent(ctx context.Context, ownerId, parentId uint64) ([]*Folders, error)
		// CountByOwnerParent 统计某个目录下未删除的子目录数量。
		CountByOwnerParent(ctx context.Context, ownerId, parentId uint64) (int64, error)
		// MarkDeleted 批量软删除目录，并失效对应的缓存。
		MarkDeleted(ctx context.Context, folders []*Folders, now time.Time) error
	}

	customFoldersModel struct {
		*defaultFoldersModel
	}
)

// NewFoldersModel returns a model for the database table.
func NewFoldersModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) FoldersModel {
	return &customFoldersModel{
		defaultFoldersModel: newFoldersModel(conn, c, opts...),
	}
}

// FindByOwnerParent 查询某个目录下的全部未删除子目录。
// 目录数量通常较少，不做分页；文件列表才需要分页。
func (m *customFoldersModel) FindByOwnerParent(ctx context.Context, ownerId, parentId uint64) ([]*Folders, error) {
	var folders []*Folders
	err := m.QueryRowsNoCacheCtx(ctx, &folders,
		"select `id`,`owner_id`,`parent_id`,`name`,`created_at`,`updated_at`,`deleted_at` "+
			"from `folders` where `owner_id` = ? and `parent_id` = ? and `deleted_at` is null "+
			"order by `name` asc",
		ownerId, parentId)
	if err != nil {
		return nil, err
	}
	return folders, nil
}

// CountByOwnerParent 统计某个目录下未删除的子目录数量。
func (m *customFoldersModel) CountByOwnerParent(ctx context.Context, ownerId, parentId uint64) (int64, error) {
	var count int64
	err := m.QueryRowNoCacheCtx(ctx, &count,
		"select count(*) from `folders` where `owner_id` = ? and `parent_id` = ? and `deleted_at` is null",
		ownerId, parentId)
	return count, err
}

// MarkDeleted 用一条 UPDATE 批量软删除目录。
func (m *customFoldersModel) MarkDeleted(ctx context.Context, folders []*Folders, now time.Time) error {
	if len(folders) == 0 {
		return nil
	}

	keys := make([]string, 0, len(folders))
	placeholders := make([]string, 0, len(folders))
	args := make([]any, 0, len(folders)+1)
	args = append(args, sql.NullTime{Time: now, Valid: true})
	for _, f := range folders {
		placeholders = append(placeholders, "?")
		args = append(args, f.Id)
		keys = append(keys, fmt.Sprintf("%s%v", cacheFoldersIdPrefix, f.Id))
	}

	query := "update `folders` set `deleted_at` = ? where `id` in (" + joinWithComma(placeholders) + ")"
	_, err := m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (sql.Result, error) {
		return conn.ExecCtx(ctx, query, args...)
	}, keys...)
	return err
}

func joinWithComma(items []string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}
