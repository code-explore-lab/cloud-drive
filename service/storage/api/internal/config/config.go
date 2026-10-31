// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package config

import (
	"cloud-drive/common/objectstore"

	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	rest.RestConf
	StorageRpc zrpc.RpcClientConf
	UserRpc    zrpc.RpcClientConf
	Minio      objectstore.Config
	// Internal.Token 是服务间内部调用的共享令牌，只用于 /internal 接口。
	Internal struct {
		Token string
	}
	// MaxChunkBytes 是单个分片请求体上限，防止异常客户端把磁盘或内存打满。
	MaxChunkBytes int64
}
