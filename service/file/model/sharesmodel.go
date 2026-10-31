package model

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ SharesModel = (*customSharesModel)(nil)

type (
	// SharesModel is an interface to be customized, add more methods here,
	// and implement the added methods in customSharesModel.
	SharesModel interface {
		sharesModel
		// FindByOwner 分页返回某个用户创建的分享（含已失效的，便于前端展示状态）。
		FindByOwner(ctx context.Context, ownerId uint64, offset, limit int64) ([]*Shares, error)
		// CountByOwner 返回某个用户的分享总数。
		CountByOwner(ctx context.Context, ownerId uint64) (int64, error)
		// Revoke 把分享置为失效。返回 false 表示分享不存在或不属于该用户。
		Revoke(ctx context.Context, share *Shares) (bool, error)
		// RevokeByFile 撤销某个文件上的全部分享（文件被删除时调用）。
		RevokeByFile(ctx context.Context, fileId, ownerId uint64) error
		// IncrementAccess 原子地占用一次访问次数，并在同一条 SQL 里校验
		// 状态、有效期和最大访问次数，避免并发下的超额访问。
		IncrementAccess(ctx context.Context, share *Shares, now time.Time) (bool, error)
	}

	customSharesModel struct {
		*defaultSharesModel
	}
)

// NewSharesModel returns a model for the database table.
func NewSharesModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) SharesModel {
	return &customSharesModel{
		defaultSharesModel: newSharesModel(conn, c, opts...),
	}
}

// FindByOwner 分页查询某个用户创建的分享。
func (m *customSharesModel) FindByOwner(ctx context.Context, ownerId uint64,
	offset, limit int64) ([]*Shares, error) {
	var shares []*Shares
	err := m.QueryRowsNoCacheCtx(ctx, &shares,
		"select `id`,`file_id`,`owner_id`,`token_hash`,`password_hash`,`expires_at`,"+
			"`max_access_count`,`access_count`,`last_access_at`,`status`,`created_at`,`updated_at` "+
			"from `shares` where `owner_id` = ? order by `id` desc limit ? offset ?",
		ownerId, limit, offset)
	if err != nil {
		return nil, err
	}
	return shares, nil
}

// CountByOwner 统计分享数量。
func (m *customSharesModel) CountByOwner(ctx context.Context, ownerId uint64) (int64, error) {
	var count int64
	err := m.QueryRowNoCacheCtx(ctx, &count,
		"select count(*) from `shares` where `owner_id` = ?", ownerId)
	return count, err
}

// Revoke 把分享置为失效，同时失效相关缓存。
func (m *customSharesModel) Revoke(ctx context.Context, share *Shares) (bool, error) {
	res, err := m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (sql.Result, error) {
		return conn.ExecCtx(ctx, "update `shares` set `status` = 0 where `id` = ? and `owner_id` = ?",
			share.Id, share.OwnerId)
	},
		fmt.Sprintf("%s%v", cacheSharesIdPrefix, share.Id),
		fmt.Sprintf("%s%v", cacheSharesTokenHashPrefix, share.TokenHash))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// RevokeByFile 撤销某个文件上的全部分享。文件删除后分享必须一起失效，
// 否则会出现"文件没了但链接还能访问"的洞。
func (m *customSharesModel) RevokeByFile(ctx context.Context, fileId, ownerId uint64) error {
	rows, err := m.FindByOwner(ctx, ownerId, 0, 1000)
	if err != nil {
		return err
	}
	var keys []string
	for _, s := range rows {
		if s.FileId != fileId || s.Status == 0 {
			continue
		}
		keys = append(keys,
			fmt.Sprintf("%s%v", cacheSharesIdPrefix, s.Id),
			fmt.Sprintf("%s%v", cacheSharesTokenHashPrefix, s.TokenHash))
	}
	if len(keys) == 0 {
		// 这里只关心本文件，用一条 SQL 处理更直接。
		_, err = m.ExecNoCacheCtx(ctx,
			"update `shares` set `status` = 0 where `file_id` = ? and `owner_id` = ? and `status` = 1",
			fileId, ownerId)
		return err
	}

	_, err = m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (sql.Result, error) {
		return conn.ExecCtx(ctx,
			"update `shares` set `status` = 0 where `file_id` = ? and `owner_id` = ? and `status` = 1",
			fileId, ownerId)
	}, keys...)
	return err
}

// IncrementAccess 用一条带条件的 UPDATE 完成"校验 + 计数"，
// 并发访问时不会有两次请求同时通过次数检查。
func (m *customSharesModel) IncrementAccess(ctx context.Context, share *Shares,
	now time.Time) (bool, error) {
	res, err := m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (sql.Result, error) {
		return conn.ExecCtx(ctx,
			"update `shares` set `access_count` = `access_count` + 1, `last_access_at` = ? "+
				"where `id` = ? and `status` = 1 "+
				"and (`expires_at` is null or `expires_at` > ?) "+
				"and (`max_access_count` is null or `access_count` < `max_access_count`)",
			now, share.Id, now)
	},
		fmt.Sprintf("%s%v", cacheSharesIdPrefix, share.Id),
		fmt.Sprintf("%s%v", cacheSharesTokenHashPrefix, share.TokenHash))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
