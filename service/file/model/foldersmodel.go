package model

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ FoldersModel = (*customFoldersModel)(nil)

type (
	// FoldersModel is an interface to be customized, add more methods here,
	// and implement the added methods in customFoldersModel.
	FoldersModel interface {
		foldersModel
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
