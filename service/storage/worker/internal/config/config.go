package config

import (
	"cloud-drive/common/objectstore"
	"cloud-drive/common/xkafka"

	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/core/stores/cache"
)

// WorkerConf 控制消费失败的重试策略。
type WorkerConf struct {
	// RetryMax 是对象删除连续失败多少次后进 DLQ，默认 5。
	RetryMax uint64 `json:",default=5"`
	// Backoff 是重建 consumer 前的基础退避，单位秒，默认 2。
	Backoff int64 `json:",default=2"`
	// MaxBackoff 是退避上限，单位秒，默认 60。
	MaxBackoff int64 `json:",default=60"`
}

type Config struct {
	service.ServiceConf
	Mysql struct {
		DataSource string
	}
	CacheRedis cache.CacheConf
	Minio      objectstore.Config
	Kafka      xkafka.Config
	Worker     WorkerConf
}
