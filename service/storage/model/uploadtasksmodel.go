package model

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ UploadTasksModel = (*customUploadTasksModel)(nil)

type (
	// UploadTasksModel is an interface to be customized, add more methods here,
	// and implement the added methods in customUploadTasksModel.
	UploadTasksModel interface {
		uploadTasksModel
	}

	customUploadTasksModel struct {
		*defaultUploadTasksModel
	}
)

// NewUploadTasksModel returns a model for the database table.
func NewUploadTasksModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) UploadTasksModel {
	return &customUploadTasksModel{
		defaultUploadTasksModel: newUploadTasksModel(conn, c, opts...),
	}
}
