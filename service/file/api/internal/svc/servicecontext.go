// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package svc

import (
	"cloud-drive/service/file/api/internal/config"
	"cloud-drive/service/file/model"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type ServiceContext struct {
	Config    config.Config
	FileModel model.FilesModel
}

func NewServiceContext(c config.Config) *ServiceContext {
	conn := sqlx.NewMysql(c.Mysql.DataSource)
	return &ServiceContext{
		Config:    c,
		FileModel: model.NewFilesModel(conn, c.CacheRedis),
	}
}
