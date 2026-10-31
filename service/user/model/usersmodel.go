package model

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ UsersModel = (*customUsersModel)(nil)

type (
	// UsersModel is an interface to be customized, add more methods here,
	// and implement the added methods in customUsersModel.
	UsersModel interface {
		usersModel
		// Invalidate 失效用户相关的缓存键（主键 / 用户名 / 邮箱索引）。
		//
		// 为什么需要它：注册流程为了给出友好错误会先查一次用户名/邮箱，
		// 而"查不到"这个结果会被 go-zero 的缓存写成占位符。
		// 随后的 INSERT 绕过了 model（走事务里的裸 SQL），不会自动失效缓存，
		// 于是紧接着登录会读到"用户不存在"的占位符，表现为"注册成功但登录失败"。
		Invalidate(ctx context.Context, id uint64, username string, email sql.NullString) error
	}

	customUsersModel struct {
		*defaultUsersModel
	}
)

// NewUsersModel returns a model for the database table.
func NewUsersModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) UsersModel {
	return &customUsersModel{
		defaultUsersModel: newUsersModel(conn, c, opts...),
	}
}

// Invalidate 删除用户相关的缓存键。email 为空表示该用户没有邮箱索引。
func (m *customUsersModel) Invalidate(ctx context.Context, id uint64, username string,
	email sql.NullString) error {
	keys := []string{
		fmt.Sprintf("%s%v", cacheUsersIdPrefix, id),
		fmt.Sprintf("%s%v", cacheUsersUsernamePrefix, username),
	}
	if email.Valid && email.String != "" {
		keys = append(keys, fmt.Sprintf("%s%v", cacheUsersEmailPrefix, email.String))
	}
	return m.DelCacheCtx(ctx, keys...)
}
