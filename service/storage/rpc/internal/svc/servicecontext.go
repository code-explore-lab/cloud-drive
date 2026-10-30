package svc

import (
	"cloud-drive/service/storage/model"
	"cloud-drive/service/storage/rpc/internal/config"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type ServiceContext struct {
	Config       config.Config
	ObjectsModel model.StorageObjectsModel
	PartsModel   model.UploadPartsModel
	TasksModel   model.UploadTasksModel
}

func NewServiceContext(c config.Config) *ServiceContext {
	conn := sqlx.NewMysql(c.Mysql.DataSource)
	return &ServiceContext{
		Config:       c,
		ObjectsModel: model.NewStorageObjectsModel(conn, c.CacheRedis),
		PartsModel:   model.NewUploadPartsModel(conn, c.CacheRedis),
		TasksModel:   model.NewUploadTasksModel(conn, c.CacheRedis),
	}
}
