package model

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ UploadTasksModel = (*customUploadTasksModel)(nil)

type (
	// UploadTasksModel is an interface to be customized, add more methods here,
	// and implement the added methods in customUploadTasksModel.
	UploadTasksModel interface {
		uploadTasksModel
		// ClaimForComplete 把任务从 initiated/uploading 原子地推进到 completing。
		// 返回 false 表示别的请求已经抢到合并权（或者任务已经不在可合并状态）。
		ClaimForComplete(ctx context.Context, task *UploadTasks) (bool, error)
		// FindExpiredActive 找出已经超时但仍未结束的任务，供后台清理。
		FindExpiredActive(ctx context.Context, now time.Time, limit int64) ([]*UploadTasks, error)
		// FindStuckCompleting 找出卡在 completing 的任务（进程崩溃留下的半状态）。
		FindStuckCompleting(ctx context.Context, olderThan time.Time, limit int64) ([]*UploadTasks, error)
		// CountActive 统计未结束的上传任务数量，用于暴露指标。
		CountActive(ctx context.Context) (int64, error)
	}

	customUploadTasksModel struct {
		*defaultUploadTasksModel
	}
)

// NewUploadTasksModel returns a model for the database table.
func NewUploadTasksModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) UploadTasksModel {
	return &customUploadTasksModel{
		defaultUploadTasksModel: newUploadTasksModel(conn, c, opts...),
	}
}

// ClaimForComplete 用条件更新实现"只有一个请求能执行合并"。
// 条件里带上原状态，等价于一次乐观锁。
func (m *customUploadTasksModel) ClaimForComplete(ctx context.Context, task *UploadTasks) (bool, error) {
	res, err := m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (sql.Result, error) {
		return conn.ExecCtx(ctx,
			"update `upload_tasks` set `status` = 'completing' "+
				"where `upload_id` = ? and `status` in ('initiated','uploading')",
			task.UploadId)
	}, fmt.Sprintf("%s%v", cacheUploadTasksIdPrefix, task.Id),
		fmt.Sprintf("%s%s", cacheUploadTasksUploadIdPrefix, task.UploadId))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// FindExpiredActive 找出超时任务。清理任务只处理 initiated/uploading，
// completing 由 FindStuckCompleting 单独处理。
func (m *customUploadTasksModel) FindExpiredActive(ctx context.Context, now time.Time,
	limit int64) ([]*UploadTasks, error) {
	var tasks []*UploadTasks
	err := m.QueryRowsNoCacheCtx(ctx, &tasks,
		"select `id`,`upload_id`,`user_id`,`file_id`,`file_name`,`total_size`,`content_type`,"+
			"`expected_hash`,`object_key`,`chunk_size`,`total_chunks`,`uploaded_chunks`,`uploaded_bytes`,"+
			"`status`,`last_error`,`expires_at`,`created_at`,`updated_at` from `upload_tasks` "+
			"where `status` in ('initiated','uploading') and `expires_at` < ? order by `id` asc limit ?",
		now, limit)
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

// FindStuckCompleting 找出长时间停留在 completing 的任务。
func (m *customUploadTasksModel) FindStuckCompleting(ctx context.Context, olderThan time.Time,
	limit int64) ([]*UploadTasks, error) {
	var tasks []*UploadTasks
	err := m.QueryRowsNoCacheCtx(ctx, &tasks,
		"select `id`,`upload_id`,`user_id`,`file_id`,`file_name`,`total_size`,`content_type`,"+
			"`expected_hash`,`object_key`,`chunk_size`,`total_chunks`,`uploaded_chunks`,`uploaded_bytes`,"+
			"`status`,`last_error`,`expires_at`,`created_at`,`updated_at` from `upload_tasks` "+
			"where `status` = 'completing' and `updated_at` < ? order by `id` asc limit ?",
		olderThan, limit)
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

// CountActive 统计当前未结束（initiated/uploading/completing）的上传任务数。
func (m *customUploadTasksModel) CountActive(ctx context.Context) (int64, error) {
	var count int64
	err := m.QueryRowNoCacheCtx(ctx, &count,
		"select count(*) from `upload_tasks` where `status` in ('initiated','uploading','completing')")
	return count, err
}
