package model

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ UserQuotasModel = (*customUserQuotasModel)(nil)

type (
	// UserQuotasModel is an interface to be customized, add more methods here,
	// and implement the added methods in customUserQuotasModel.
	UserQuotasModel interface {
		userQuotasModel
		ReserveQuota(ctx context.Context, userId, bytes uint64) (bool, error)
		CommitQuota(ctx context.Context, userId, bytes uint64) (bool, error)
		ReleaseQuota(ctx context.Context, userId, bytes uint64) (bool, error)
		ReleaseUsedQuota(ctx context.Context, userId, bytes uint64) (bool, error)
	}

	customUserQuotasModel struct {
		*defaultUserQuotasModel
	}
)

// NewUserQuotasModel returns a model for the database table.
func NewUserQuotasModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) UserQuotasModel {
	return &customUserQuotasModel{
		defaultUserQuotasModel: newUserQuotasModel(conn, c, opts...),
	}
}

// ReserveQuota 原子地增加 reserved_bytes：只有剩余额度足够时才会更新成功。
// 条件写在 WHERE 里，避免「先查再改」在并发下超卖。
func (m *customUserQuotasModel) ReserveQuota(ctx context.Context, userId, bytes uint64) (bool, error) {
	return m.execQuota(ctx, userId,
		"update user_quotas set reserved_bytes = reserved_bytes + ? "+
			"where user_id = ? and used_bytes + reserved_bytes + ? <= total_bytes",
		bytes, userId, bytes)
}

// CommitQuota 把预占额度转成已用额度：used += bytes, reserved -= bytes。
// 上传完成时调用；reserved 不足说明调用链有问题，返回 false 而不是把数字改成负数。
func (m *customUserQuotasModel) CommitQuota(ctx context.Context, userId, bytes uint64) (bool, error) {
	return m.execQuota(ctx, userId,
		"update user_quotas set used_bytes = used_bytes + ?, reserved_bytes = reserved_bytes - ? "+
			"where user_id = ? and reserved_bytes >= ?",
		bytes, bytes, userId, bytes)
}

// ReleaseQuota 原子地减少 reserved_bytes：不会减到负数。
func (m *customUserQuotasModel) ReleaseQuota(ctx context.Context, userId, bytes uint64) (bool, error) {
	return m.execQuota(ctx, userId,
		"update user_quotas set reserved_bytes = reserved_bytes - ? "+
			"where user_id = ? and reserved_bytes >= ?",
		bytes, userId, bytes)
}

// ReleaseUsedQuota 原子地减少 used_bytes：不会减到负数。
func (m *customUserQuotasModel) ReleaseUsedQuota(ctx context.Context, userId, bytes uint64) (bool, error) {
	return m.execQuota(ctx, userId,
		"update user_quotas set used_bytes = used_bytes - ? "+
			"where user_id = ? and used_bytes >= ?",
		bytes, userId, bytes)
}

// execQuota 执行带条件的额度更新，并按 userId 失效缓存。
func (m *customUserQuotasModel) execQuota(ctx context.Context, userId uint64,
	query string, args ...any) (bool, error) {
	key := fmt.Sprintf("%s%v", cacheUserQuotasUserIdPrefix, userId)
	res, err := m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (sql.Result, error) {
		return conn.ExecCtx(ctx, query, args...)
	}, key)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
