package svc

import (
	"cloud-drive/common/objectstore"
	"cloud-drive/common/xkafka"
	"cloud-drive/service/storage/model"
	"cloud-drive/service/storage/worker/internal/config"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type ServiceContext struct {
	Config       config.Config
	ObjectsModel model.StorageObjectsModel
	Store        *objectstore.Store
	// DLQ 固定写入死信 topic，发布失败时调用方会重投而不是 commit。
	DLQ *xkafka.Producer
}

func NewServiceContext(c config.Config) *ServiceContext {
	conn := sqlx.NewMysql(c.Mysql.DataSource)

	store, err := objectstore.New(c.Minio)
	if err != nil {
		// 配置错误属于启动即失败的问题，直接退出比带着坏配置跑更安全。
		logx.Must(err)
	}

	dlqConfig := c.Kafka
	dlqConfig.Topic = xkafka.TopicStorageEventsDLQ

	return &ServiceContext{
		Config:       c,
		ObjectsModel: model.NewStorageObjectsModel(conn, c.CacheRedis),
		Store:        store,
		DLQ:          xkafka.NewProducer(dlqConfig),
	}
}
