// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package svc

import (
	"net"
	"net/http"
	"strings"
	"time"

	"cloud-drive/service/file/api/internal/config"
	"cloud-drive/service/file/rpc/fileclient"
	"cloud-drive/service/storage/rpc/storageclient"
	"cloud-drive/service/user/rpc/userclient"

	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config     config.Config
	FileRpc    fileclient.File
	StorageRpc storageclient.Storage
	UserRpc    userclient.User
	// StorageHTTP 专门用于向 storage-api 转发下载流。
	// 注意：它不能有整体超时，否则大文件下载会被中途切断。
	StorageHTTP *http.Client
	StorageBase string
}

func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config:     c,
		FileRpc:    fileclient.NewFile(zrpc.MustNewClient(c.FileRpc)),
		StorageRpc: storageclient.NewStorage(zrpc.MustNewClient(c.StorageRpc)),
		UserRpc:    userclient.NewUser(zrpc.MustNewClient(c.UserRpc)),
		StorageHTTP: &http.Client{
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				ResponseHeaderTimeout: 30 * time.Second,
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   16,
				IdleConnTimeout:       90 * time.Second,
			},
		},
		StorageBase: strings.TrimRight(c.StorageApi.BaseUrl, "/"),
	}
}
