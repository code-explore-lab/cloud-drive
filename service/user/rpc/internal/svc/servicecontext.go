package svc

import (
	"cloud-drive/service/user/model"
	"cloud-drive/service/user/rpc/internal/config"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type ServiceContext struct {
	Config      config.Config
	Conn        sqlx.SqlConn
	UsersModel  model.UsersModel
	QuotasModel model.UserQuotasModel
	TokensModel model.RefreshTokensModel
}

func NewServiceContext(c config.Config) *ServiceContext {
	conn := sqlx.NewMysql(c.Mysql.DataSource)
	return &ServiceContext{
		Config:      c,
		Conn:        conn,
		UsersModel:  model.NewUsersModel(conn, c.CacheRedis),
		QuotasModel: model.NewUserQuotasModel(conn, c.CacheRedis),
		TokensModel: model.NewRefreshTokensModel(conn, c.CacheRedis),
	}
}
