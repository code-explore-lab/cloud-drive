package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cloud-drive/common/xkafka"
	"cloud-drive/service/storage/model"
	"cloud-drive/service/storage/rpc/internal/svc"

	"github.com/segmentio/kafka-go"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	defaultOutboxInterval    = time.Second
	defaultOutboxBatch       = int64(100)
	defaultOutboxMaxAttempts = uint64(5)

	// 投递失败后的指数退避，避免 broker 长时间不可用时每秒猛打一轮。
	dispatcherBackoffBase = 2 * time.Second
	dispatcherBackoffMax  = 5 * time.Minute

	// lastErrorMaxLen 与 outbox_events.last_error 的列宽一致。
	lastErrorMaxLen = 512
)

// Dispatcher 把 outbox_events 里的 pending 事件搬到 Kafka。
//
// 它只做「可靠投递」：业务上是否真的清理成功由 storage_objects.status 与
// gc_attempts 追踪，因此投递失败只需要退避重试，不需要补偿业务状态。
type Dispatcher struct {
	svcCtx      *svc.ServiceContext
	interval    time.Duration
	batch       int64
	maxAttempts uint64
}

func NewDispatcher(svcCtx *svc.ServiceContext) *Dispatcher {
	d := &Dispatcher{
		svcCtx:      svcCtx,
		interval:    defaultOutboxInterval,
		batch:       defaultOutboxBatch,
		maxAttempts: defaultOutboxMaxAttempts,
	}
	if sec := svcCtx.Config.Outbox.Interval; sec > 0 {
		d.interval = time.Duration(sec) * time.Second
	}
	if n := svcCtx.Config.Outbox.BatchSize; n > 0 {
		d.batch = n
	}
	if n := svcCtx.Config.Outbox.MaxAttempts; n > 0 {
		d.maxAttempts = n
	}
	return d
}

// Start 在后台启动建 topic 与投递循环，直到 ctx 结束。
func (d *Dispatcher) Start(ctx context.Context) {
	go d.ensureTopics(ctx)

	ticker := time.NewTicker(d.interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.RunOnce(ctx)
			}
		}
	}()
	logx.Infof("storage outbox dispatcher started: interval=%s batch=%d", d.interval, d.batch)
}

// 服务启动时 自动确保Kafka Topic存在
// ensureTopics 在 broker 未就绪时持续重试，与 MinIO 的 ensureBucket 同一套思路。
func (d *Dispatcher) ensureTopics(ctx context.Context) {
	topics := []string{xkafka.TopicStorageEvents, xkafka.TopicStorageEventsDLQ}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if err := xkafka.EnsureTopics(ctx, d.svcCtx.Config.Kafka.Brokers, topics...); err != nil {
			logx.Errorf("ensure kafka topics failed: %v", err)
		} else {
			logx.Infof("kafka topics are ready: %v", topics)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce 取一批到期的 pending 事件投递出去。
func (d *Dispatcher) RunOnce(ctx context.Context) {
	events, err := d.svcCtx.OutboxModel.FindPending(ctx, d.batch)
	if err != nil {
		logx.WithContext(ctx).Errorf("find pending outbox events failed: %v", err)
		return
	}
	if len(events) == 0 {
		return
	}

	msgs := make([]kafka.Message, 0, len(events))
	sendable := make([]*model.OutboxEvents, 0, len(events))
	for _, event := range events {
		msg, err := toMessage(event)
		if err != nil {
			// 毒事件隔离：单条坏事件标 failed，不阻塞同批次其他事件。
			logx.WithContext(ctx).Errorf("drop malformed outbox event: id=%d event_id=%s: %v",
				event.Id, event.EventId, err)
			if markErr := d.svcCtx.OutboxModel.MarkFailed(ctx, event.Id, event.Attempts+1,
				truncate(err.Error(), lastErrorMaxLen)); markErr != nil {
				logx.WithContext(ctx).Errorf("mark outbox event failed: id=%d: %v", event.Id, markErr)
			}
			continue
		}
		msgs = append(msgs, msg)
		sendable = append(sendable, event)
	}
	if len(msgs) == 0 {
		return
	}

	if err := d.svcCtx.Producer.PublishBatch(ctx, msgs); err != nil {
		// kafka-go 不返回「哪几条成功」，按整批可能部分成功处理：全部退避重投。
		// 消费端幂等（status='deleting' 才处理），重复投递是安全的。
		logx.WithContext(ctx).Errorf("publish outbox batch failed: size=%d: %v", len(msgs), err)
		for _, event := range sendable {
			d.retry(ctx, event, err)
		}
		return
	}

	for _, event := range sendable {
		if _, err := d.svcCtx.OutboxModel.MarkPublished(ctx, event.Id); err != nil {
			// 已投递但状态没更新：下一轮会重复投递，由消费端幂等兜住。
			logx.WithContext(ctx).Errorf("mark outbox event published failed: id=%d: %v",
				event.Id, err)
		}
	}
}

// retry 累加 attempts 并按指数退避推后下次投递；超过上限转 failed 供人工排查。
func (d *Dispatcher) retry(ctx context.Context, event *model.OutboxEvents, cause error) {
	attempts := event.Attempts + 1
	if attempts >= d.maxAttempts {
		if err := d.svcCtx.OutboxModel.MarkFailed(ctx, event.Id, attempts,
			truncate(cause.Error(), lastErrorMaxLen)); err != nil {
			logx.WithContext(ctx).Errorf("mark outbox event failed: id=%d: %v", event.Id, err)
		}
		return
	}

	backoff := backoffFor(attempts)
	if err := d.svcCtx.OutboxModel.MarkRetry(ctx, event.Id, attempts,
		cause.Error(), backoff); err != nil {
		logx.WithContext(ctx).Errorf("mark outbox event retry failed: id=%d: %v", event.Id, err)
	}
}

// toMessage 把 outbox 行组装成 Kafka 消息。
//
// event_id 与 occurred_at 来自写入时的那一行，重试时保持不变；
// key 用 aggregate_id（object_key），保证同一对象的事件进同一分区、按顺序处理。
func toMessage(event *model.OutboxEvents) (kafka.Message, error) {
	if event.EventType == "" {
		return kafka.Message{}, fmt.Errorf("missing event type")
	}
	payload := json.RawMessage(event.Payload)
	if len(payload) == 0 || !json.Valid(payload) {
		return kafka.Message{}, fmt.Errorf("invalid payload: %q", event.Payload)
	}

	value, err := json.Marshal(xkafka.Envelope{
		Version:    xkafka.EnvelopeVersion,
		Type:       event.EventType,
		EventID:    event.EventId,
		OccurredAt: event.CreatedAt.UTC(),
		Payload:    payload,
	})
	if err != nil {
		return kafka.Message{}, fmt.Errorf("marshal envelope: %w", err)
	}

	var key []byte
	if event.AggregateId != "" {
		key = []byte(event.AggregateId)
	}
	return kafka.Message{
		Key:   key,
		Value: value,
		Headers: []kafka.Header{
			{Key: "x-event-id", Value: []byte(event.EventId)},
		},
	}, nil
}

// backoffFor 返回第 attempts 次失败后的等待时间：base * 2^(attempts-1)，封顶 max。
func backoffFor(attempts uint64) time.Duration {
	backoff := dispatcherBackoffBase
	for i := uint64(1); i < attempts && backoff < dispatcherBackoffMax; i++ {
		backoff *= 2
	}
	if backoff > dispatcherBackoffMax {
		return dispatcherBackoffMax
	}
	return backoff
}
