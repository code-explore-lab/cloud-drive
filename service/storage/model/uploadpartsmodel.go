package model

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ UploadPartsModel = (*customUploadPartsModel)(nil)

type (
	// UploadPartsModel is an interface to be customized, add more methods here,
	// and implement the added methods in customUploadPartsModel.
	UploadPartsModel interface {
		uploadPartsModel
	}

	customUploadPartsModel struct {
		*defaultUploadPartsModel
	}
)

// NewUploadPartsModel returns a model for the database table.
func NewUploadPartsModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) UploadPartsModel {
	return &customUploadPartsModel{
		defaultUploadPartsModel: newUploadPartsModel(conn, c, opts...),
	}
}
