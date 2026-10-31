// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package config

import (
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	rest.RestConf
	FileRpc    zrpc.RpcClientConf
	StorageRpc zrpc.RpcClientConf
	UserRpc    zrpc.RpcClientConf

	// StorageApi 是存储服务数据面的内网地址，文件下载由本服务鉴权后代理过去。
	StorageApi struct {
		BaseUrl string
	}
	// Internal.Token 与 storage-api 共享，用于内部下载接口鉴权。
	Internal struct {
		Token string
	}
	// Share.BaseUrl 用于拼出对外的分享访问地址。
	Share struct {
		BaseUrl string
	}
	Upload struct {
		// MaxFileBytes 是单文件大小上限，默认 10GiB。
		MaxFileBytes int64
	}
}
