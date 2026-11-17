// Package consumer 消费存储事件，把 storage_objects 里待清理的对象从 MinIO 删掉。
//
// 重试语义见 docs/kafka-design.md 4.1：kafka-go 的 consumer group 模式下无法在进程内
// 回退 offset，所以「可重试失败」的唯一正确做法是停止消费、不 commit、关闭 reader，
// 退避后用同一 GroupID 重新入组，让 broker 从 last committed offset 重新投递。
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"cloud-drive/common/xkafka"
	"cloud-drive/service/storage/model"
	"cloud-drive/service/storage/worker/internal/config"
	"cloud-drive/service/storage/worker/internal/svc"

	"github.com/segmentio/kafka-go"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	defaultRetryMax = uint64(5)
	defaultBackoff  = 2 * time.Second

	// maxLastErrorLen 与 storage_objects.last_error 的列宽一致。
	maxLastErrorLen = 500
)

type action int

const (
	actionCommit action = iota // 成功/幂等跳过：commit
	actionRetry                // 可重试失败：不 commit，重建 reader
	actionDLQ                  // 永久失败/超限：发 DLQ，成功后再 commit
)

type outcome struct {
	action   action
	cause    error
	attempts int
}

// Worker 是 storage-worker 的消费循环。
type Worker struct {
	cfg      config.Config
	svcCtx   *svc.ServiceContext
	consumer *xkafka.Consumer
}

func NewWorker(cfg config.Config, svcCtx *svc.ServiceContext) *Worker {
	if cfg.Worker.RetryMax == 0 {
		cfg.Worker.RetryMax = defaultRetryMax
	}
	if cfg.Worker.Backoff <= 0 {
		cfg.Worker.Backoff = int64(defaultBackoff / time.Second)
	}
	if cfg.Worker.MaxBackoff < cfg.Worker.Backoff {
		cfg.Worker.MaxBackoff = cfg.Worker.Backoff
	}
	return &Worker{cfg: cfg, svcCtx: svcCtx}
}

// Start 反复「建 consumer → 消费 → 失败后退避重建」，直到 ctx 结束。
func (w *Worker) Start(ctx context.Context) {
	initBackoff := time.Duration(w.cfg.Worker.Backoff) * time.Second
	maxBackoff := time.Duration(w.cfg.Worker.MaxBackoff) * time.Second
	backoff := initBackoff

	// 正常情况下 topic 由 storage.rpc 的 dispatcher 创建；这里再尝试一次，
	// 失败也无妨，下面的消费循环会带着退避继续重试。
	topics := []string{xkafka.TopicStorageEvents, xkafka.TopicStorageEventsDLQ}
	if err := xkafka.EnsureTopics(ctx, w.cfg.Kafka.Brokers, topics...); err != nil {
		logx.Errorf("ensure kafka topics failed, will retry while consuming: %v", err)
	}

	for ctx.Err() == nil {
		if w.consumer == nil {
			c, err := xkafka.NewConsumer(w.cfg.Kafka)
			if err != nil {
				logx.Errorf("create consumer failed: %v", err)
				sleepCtx(ctx, backoff)
				backoff = nextBackoff(backoff, maxBackoff)
				continue
			}
			w.consumer = c
		}

		committed, err := w.consume(ctx)
		if ctx.Err() != nil {
			w.closeConsumer()
			return
		}
		w.closeConsumer()

		// 本轮有消息被成功处理：说明 broker/mysql/minio 都活着，退避从头开始。
		if committed > 0 {
			backoff = initBackoff
		}
		logx.Errorf("storage worker: consumer stopped, retry after %s: %v", backoff, err)
		sleepCtx(ctx, backoff)
		backoff = nextBackoff(backoff, maxBackoff)
	}
	w.closeConsumer()
}

// consume 持续拉取消息直到需要重试或 ctx 结束。返回值 committed 是本次入组期间
// 成功 commit 的消息数，用于判断是否应该重置退避。
func (w *Worker) consume(ctx context.Context) (int, error) {
	committed := 0
	for {
		msg, err := w.consumer.FetchMessage(ctx)
		if err != nil {
			return committed, err
		}

		o := w.handle(ctx, msg)
		switch o.action {
		case actionRetry:
			// 关键：不 commit 直接退出本轮；重建 reader 重新入组后这条会重投。
			return committed, fmt.Errorf("retry later: %w", o.cause)

		case actionDLQ:
			// 关键：DLQ 发送成功后才 commit；发送失败就退出，重投后再试。
			if err := w.publishDLQ(ctx, msg, o.cause, o.attempts); err != nil {
				return committed, fmt.Errorf("publish dlq: %w", err)
			}
			if err := w.consumer.CommitMessages(ctx, msg); err != nil {
				return committed, err
			}
			committed++

		case actionCommit:
			if err := w.consumer.CommitMessages(ctx, msg); err != nil {
				return committed, err
			}
			committed++
		}
	}
}

// handle 做失败分类，规则见 docs/kafka-design.md 4.2。
func (w *Worker) handle(ctx context.Context, msg kafka.Message) outcome {
	var env xkafka.Envelope
	if err := json.Unmarshal(msg.Value, &env); err != nil {
		return outcome{action: actionDLQ, cause: fmt.Errorf("bad envelope: %w", err)}
	}
	if env.Version > xkafka.EnvelopeVersion {
		return outcome{action: actionDLQ, cause: fmt.Errorf("unsupported version %d", env.Version)}
	}

	switch env.Type {
	case xkafka.EventTypeObjectCleanupRequested:
		var p xkafka.ObjectCleanupRequested
		if err := json.Unmarshal(env.Payload, &p); err != nil || p.ObjectKey == "" {
			return outcome{action: actionDLQ, cause: fmt.Errorf("bad payload: %v", err)}
		}
		return w.cleanupObject(ctx, p.ObjectKey)

	default:
		// 未知事件类型：别的服务的事件不应进这个 group；丢 DLQ 便于排查。
		return outcome{action: actionDLQ, cause: fmt.Errorf("unknown type %q", env.Type)}
	}
}

func (w *Worker) cleanupObject(ctx context.Context, key string) outcome {
	obj, err := w.svcCtx.ObjectsModel.FindOneByObjectKey(ctx, key)
	if errors.Is(err, model.ErrNotFound) {
		return outcome{action: actionCommit} // 幂等
	}
	if err != nil {
		// DB 抖动：不动 gc_attempts，退避重投。
		return outcome{action: actionRetry, cause: err}
	}
	if obj.Status != "deleting" {
		return outcome{action: actionCommit} // 已被 GC 或其他 worker 处理
	}

	if err := w.svcCtx.Store.Remove(ctx, key); err != nil {
		obj.GcAttempts++
		obj.LastError = truncate(err.Error(), maxLastErrorLen)
		if updateErr := w.svcCtx.ObjectsModel.Update(ctx, obj); updateErr != nil {
			return outcome{action: actionRetry, cause: updateErr}
		}
		attempts := int(obj.GcAttempts)
		if uint64(attempts) >= w.cfg.Worker.RetryMax {
			return outcome{action: actionDLQ, cause: err, attempts: attempts}
		}
		return outcome{action: actionRetry, cause: err, attempts: attempts}
	}

	obj.Status = "deleted"
	obj.DeletedAt.Valid = true
	obj.DeletedAt.Time = time.Now()
	obj.LastError = ""
	if err := w.svcCtx.ObjectsModel.Update(ctx, obj); err != nil {
		// Remove 幂等（404 视为成功），所以这里退避重投是安全的。
		return outcome{action: actionRetry, cause: err}
	}
	return outcome{action: actionCommit}
}

// publishDLQ 把原始消息原样转发到死信 topic，诊断信息只放 headers。
// Value 保持不变，才能从 DLQ 原样回放到主 topic 重放。
func (w *Worker) publishDLQ(ctx context.Context, msg kafka.Message, cause error,
	attempts int) error {
	return w.svcCtx.DLQ.PublishMessage(ctx, dlqMessage(msg, cause, attempts))
}

// dlqMessage 构造死信消息，抽成纯函数便于单测。
func dlqMessage(msg kafka.Message, cause error, attempts int) kafka.Message {
	if cause == nil {
		cause = errors.New("unknown error")
	}
	headers := make([]kafka.Header, 0, len(msg.Headers)+3)
	headers = append(headers, msg.Headers...)
	headers = append(headers,
		kafka.Header{Key: "x-error", Value: []byte(truncate(cause.Error(), maxLastErrorLen))},
		kafka.Header{Key: "x-attempts", Value: []byte(strconv.Itoa(attempts))},
		kafka.Header{Key: "x-failed-at", Value: []byte(time.Now().UTC().Format(time.RFC3339))},
	)

	return kafka.Message{
		Topic:   xkafka.TopicStorageEventsDLQ,
		Key:     msg.Key,
		Value:   msg.Value,
		Headers: headers,
	}
}

func (w *Worker) closeConsumer() {
	if w.consumer == nil {
		return
	}
	if err := w.consumer.Close(); err != nil {
		logx.Errorf("close kafka consumer failed: %v", err)
	}
	w.consumer = nil
}

func nextBackoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		return max
	}
	return next
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}
