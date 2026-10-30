package model

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ RefreshTokensModel = (*customRefreshTokensModel)(nil)

type (
	// RefreshTokensModel is an interface to be customized, add more methods here,
	// and implement the added methods in customRefreshTokensModel.
	RefreshTokensModel interface {
		refreshTokensModel
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
