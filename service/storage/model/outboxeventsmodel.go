package model

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// Outbox 事件状态。
const (
	OutboxStatusPending   = "pending"
	OutboxStatusPublished = "published"
	OutboxStatusFailed    = "failed"
)

// outboxErrorMaxLen 与 outbox_events.last_error 的列宽一致。
const outboxErrorMaxLen = 512

var _ OutboxEventsModel = (*customOutboxEventsModel)(nil)

type (
	// OutboxEventsModel is an interface to be customized, add more methods here,
	// and implement the added methods in customOutboxEventsModel.
	//
	// 自定义方法都不走缓存：outbox 只用轮询读取（FindPending），没有按主键点查的热点；
	// 缓存会让 next_attempt_at 的退避判断失真，也会给 dispatcher 的每次条件更新增加无意义的失效调用。
	OutboxEventsModel interface {
		outboxEventsModel
		// InsertInTx 在调用方的事务内写入一条 pending 事件，保证「业务状态变更」与
		// 「事件落库」原子完成。event_id 由调用方生成并在重投时保持不变。
		InsertInTx(ctx context.Context, session sqlx.Session, event *OutboxEvents) error
		// FindPending 返回 status=pending 且已到 next_attempt_at 的事件，按 id 升序。
		FindPending(ctx context.Context, limit int64) ([]*OutboxEvents, error)
		// MarkPublished 把 pending 事件标记为 published，返回实际更新的行数。
		// 条件里带 status='pending'，避免覆盖并发路径写入的状态。
		MarkPublished(ctx context.Context, id uint64) (int64, error)
		// MarkRetry 保持 pending，累加 attempts 并推后 retryAfter 后再投递。
		MarkRetry(ctx context.Context, id uint64, attempts uint64, lastError string,
			retryAfter time.Duration) error
		// MarkFailed 超过最大尝试次数，标记为 failed 供人工排查 / 重新投递。
		MarkFailed(ctx context.Context, id uint64, attempts uint64, lastError string) error
	}

	customOutboxEventsModel struct {
		*defaultOutboxEventsModel
	}
)

// NewOutboxEventsModel returns a model for the database table.
func NewOutboxEventsModel(conn sqlx.SqlConn) OutboxEventsModel {
	return &customOutboxEventsModel{
		defaultOutboxEventsModel: newOutboxEventsModel(conn),
	}
}

// InsertInTx 在同一事务内写入事件；next_attempt_at 使用列默认值（当前时间）。
func (m *customOutboxEventsModel) InsertInTx(ctx context.Context, session sqlx.Session,
	event *OutboxEvents) error {
	result, err := session.ExecCtx(ctx,
		"insert into `outbox_events` (`event_id`,`event_type`,`aggregate_id`,`payload`) values (?,?,?,?)",
		event.EventId, event.EventType, event.AggregateId, event.Payload)
	if err != nil {
		return err
	}
	// id 只用于日志关联，取不到不影响事件本身已经落库。
	if id, err := result.LastInsertId(); err == nil {
		event.Id = uint64(id)
	}
	return nil
}

// FindPending 只捞已经到期的 pending 事件，退避中的行不会被立刻重新取出。
func (m *customOutboxEventsModel) FindPending(ctx context.Context, limit int64) ([]*OutboxEvents, error) {
	var events []*OutboxEvents
	err := m.conn.QueryRowsCtx(ctx, &events,
		"select `id`,`event_id`,`event_type`,`aggregate_id`,`payload`,`status`,`attempts`,"+
			"`last_error`,`next_attempt_at`,`created_at`,`published_at` from `outbox_events` "+
			"where `status` = ? and `next_attempt_at` <= now() order by `id` asc limit ?",
		OutboxStatusPending, limit)
	if err != nil {
		return nil, err
	}
	return events, nil
}

// MarkPublished 只在仍是 pending 时生效，重复调用不会把 attempts / last_error 改回去。
func (m *customOutboxEventsModel) MarkPublished(ctx context.Context, id uint64) (int64, error) {
	result, err := m.conn.ExecCtx(ctx,
		"update `outbox_events` set `status` = ?, `published_at` = now(), `last_error` = '' "+
			"where `id` = ? and `status` = ?",
		OutboxStatusPublished, id, OutboxStatusPending)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// MarkRetry 用 SQL 侧的 now() 推后退避时间，避免应用与 MySQL 时区不一致导致的提前/延后投递。
func (m *customOutboxEventsModel) MarkRetry(ctx context.Context, id uint64, attempts uint64,
	lastError string, retryAfter time.Duration) error {
	seconds := max(int64(retryAfter/time.Second), 0)
	_, err := m.conn.ExecCtx(ctx,
		"update `outbox_events` set `attempts` = ?, `last_error` = ?, "+
			"`next_attempt_at` = date_add(now(), interval ? second) "+
			"where `id` = ? and `status` = ?",
		attempts, truncate(lastError, outboxErrorMaxLen), seconds, id, OutboxStatusPending)
	return err
}

func (m *customOutboxEventsModel) MarkFailed(ctx context.Context, id uint64, attempts uint64,
	lastError string) error {
	_, err := m.conn.ExecCtx(ctx,
		"update `outbox_events` set `status` = ?, `attempts` = ?, `last_error` = ? where `id` = ?",
		OutboxStatusFailed, attempts, truncate(lastError, outboxErrorMaxLen), id)
	return err
}

// truncate 保证写入 last_error 的内容不超过列宽，避免严格模式下整条 UPDATE 失败。
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}
