package config

import (
	"cloud-drive/common/objectstore"
	"cloud-drive/common/xkafka"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/zrpc"
)

// MaintenanceConf 控制存储侧的后台清理任务。
type MaintenanceConf struct {
	Enabled bool `json:",default=true"`
	// Interval 单位秒，默认 600。
	Interval int64 `json:",default=600"`
	// UploadTTL 单位小时，默认 24。
	UploadTTL int64 `json:",default=24"`
	// BatchSize 是单轮处理的最大记录数，默认 200。
	BatchSize int64 `json:",default=200"`
}

// OutboxConf 控制事务性 outbox 的投递循环。
type OutboxConf struct {
	Enabled bool `json:",default=true"`
	// Interval 是轮询 pending 事件的间隔，单位秒，默认 1。
	Interval int64 `json:",default=1"`
	// BatchSize 是单轮投递的最大事件数，默认 100。
	BatchSize int64 `json:",default=100"`
	// MaxAttempts 是投递失败的上限，超过后事件标为 failed，默认 5。
	MaxAttempts uint64 `json:",default=5"`
}

type Config struct {
	zrpc.RpcServerConf
	Mysql struct {
		DataSource string
	}
	CacheRedis  cache.CacheConf
	Minio       objectstore.Config
	Maintenance MaintenanceConf
	Kafka       xkafka.Config
	Outbox      OutboxConf
}
