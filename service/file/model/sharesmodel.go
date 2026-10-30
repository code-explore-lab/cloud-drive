package model

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ SharesModel = (*customSharesModel)(nil)

type (
	// SharesModel is an interface to be customized, add more methods here,
	// and implement the added methods in customSharesModel.
	SharesModel interface {
		sharesModel
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
