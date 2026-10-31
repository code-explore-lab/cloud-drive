// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package svc

import (
	"cloud-drive/common/objectstore"
	"cloud-drive/service/storage/api/internal/config"
	"cloud-drive/service/storage/rpc/storageclient"
	"cloud-drive/service/user/rpc/userclient"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config     config.Config
	StorageRpc storageclient.Storage
	UserRpc    userclient.User
	Store      *objectstore.Store
}

func NewServiceContext(c config.Config) *ServiceContext {
	store, err := objectstore.New(c.Minio)
	if err != nil {
		logx.Must(err)
	}
	return &ServiceContext{
		Config:     c,
		StorageRpc: storageclient.NewStorage(zrpc.MustNewClient(c.StorageRpc)),
		UserRpc:    userclient.NewUser(zrpc.MustNewClient(c.UserRpc)),
		Store:      store,
	}
}
