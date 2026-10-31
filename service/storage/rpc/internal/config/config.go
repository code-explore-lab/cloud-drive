package config

import (
	"cloud-drive/common/objectstore"

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

type Config struct {
	zrpc.RpcServerConf
	Mysql struct {
		DataSource string
	}
	CacheRedis  cache.CacheConf
	Minio       objectstore.Config
	Maintenance MaintenanceConf
}
