package svc

import (
	"cloud-drive/service/file/model"
	"cloud-drive/service/file/rpc/internal/config"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type ServiceContext struct {
	Config        config.Config
	FilesModel    model.FilesModel
	VersionsModel model.FileVersionsModel
	FoldersModel  model.FoldersModel
	SharesModel   model.SharesModel
}

func NewServiceContext(c config.Config) *ServiceContext {
	conn := sqlx.NewMysql(c.Mysql.DataSource)
	return &ServiceContext{
		Config:        c,
		FilesModel:    model.NewFilesModel(conn, c.CacheRedis),
		VersionsModel: model.NewFileVersionsModel(conn, c.CacheRedis),
		FoldersModel:  model.NewFoldersModel(conn, c.CacheRedis),
		SharesModel:   model.NewSharesModel(conn, c.CacheRedis),
	}
}
