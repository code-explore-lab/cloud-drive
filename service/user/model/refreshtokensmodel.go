package model

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ RefreshTokensModel = (*customRefreshTokensModel)(nil)

type (
	// RefreshTokensModel is an interface to be customized, add more methods here,
	// and implement the added methods in customRefreshTokensModel.
	RefreshTokensModel interface {
		refreshTokensModel
		// FindActiveByUser 返回某个用户当前仍然有效的 refresh token。
		FindActiveByUser(ctx context.Context, userId uint64, now time.Time) ([]*RefreshTokens, error)
	}

	customRefreshTokensModel struct {
		*defaultRefreshTokensModel
	}
)

// NewRefreshTokensModel returns a model for the database table.
func NewRefreshTokensModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) RefreshTokensModel {
	return &customRefreshTokensModel{
		defaultRefreshTokensModel: newRefreshTokensModel(conn, c, opts...),
	}
}

// FindActiveByUser 查询未撤销且未过期的 refresh token，用于"退出登录（所有设备）"。
func (m *customRefreshTokensModel) FindActiveByUser(ctx context.Context, userId uint64,
	now time.Time) ([]*RefreshTokens, error) {
	var tokens []*RefreshTokens
	err := m.QueryRowsNoCacheCtx(ctx, &tokens,
		"select `id`,`user_id`,`token_hash`,`expires_at`,`revoked_at`,`created_at` "+
			"from `refresh_tokens` where `user_id` = ? and `revoked_at` is null and `expires_at` > ?",
		userId, now)
	if err != nil {
		return nil, err
	}
	return tokens, nil
}
