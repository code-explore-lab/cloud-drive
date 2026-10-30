package model

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ StorageObjectsModel = (*customStorageObjectsModel)(nil)

type (
	// StorageObjectsModel is an interface to be customized, add more methods here,
	// and implement the added methods in customStorageObjectsModel.
	StorageObjectsModel interface {
		storageObjectsModel
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
