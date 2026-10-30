package model

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ FileVersionsModel = (*customFileVersionsModel)(nil)

type (
	// FileVersionsModel is an interface to be customized, add more methods here,
	// and implement the added methods in customFileVersionsModel.
	FileVersionsModel interface {
		fileVersionsModel
	}

	customFileVersionsModel struct {
		*defaultFileVersionsModel
	}
)

// NewFileVersionsModel returns a model for the database table.
func NewFileVersionsModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) FileVersionsModel {
	return &customFileVersionsModel{
		defaultFileVersionsModel: newFileVersionsModel(conn, c, opts...),
	}
}
