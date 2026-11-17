package svc

import (
	"cloud-drive/common/objectstore"
	"cloud-drive/service/storage/model"
	"cloud-drive/service/storage/rpc/internal/config"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type ServiceContext struct {
	Config       config.Config
	Conn         sqlx.SqlConn
	ObjectsModel model.StorageObjectsModel
	PartsModel   model.UploadPartsModel
	TasksModel   model.UploadTasksModel
	OutboxModel  model.OutboxEventsModel
	Store        *objectstore.Store
}

func NewServiceContext(c config.Config) *ServiceContext {
	conn := sqlx.NewMysql(c.Mysql.DataSource)

	store, err := objectstore.New(c.Minio)
	if err != nil {
		// 配置错误属于启动即失败的问题，直接退出比带着坏配置跑更安全。
		logx.Must(err)
	}

	return &ServiceContext{
		Config:       c,
		Conn:         conn,
		ObjectsModel: model.NewStorageObjectsModel(conn, c.CacheRedis),
		PartsModel:   model.NewUploadPartsModel(conn, c.CacheRedis),
		TasksModel:   model.NewUploadTasksModel(conn, c.CacheRedis),
		OutboxModel:  model.NewOutboxEventsModel(conn),
		Store:        store,
	}
}
