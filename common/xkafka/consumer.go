package xkafka

import (
	"context"
	"fmt"

	"github.com/segmentio/kafka-go"
)

// Consumer 封装 kafka-go 的 Reader，强制手动 commit 语义。
type Consumer struct {
	reader *kafka.Reader
}

// NewConsumer 创建 consumer group 消费者。
// 注意：GroupID 必填，CommitInterval 强制为 0（手动提交）。
//
// consumer group 模式下 Reader 不支持 Seek：需要重投时只能 Close 后用同一
// GroupID 重建，重新入组会从 last committed offset 继续。
func NewConsumer(cfg Config) (*Consumer, error) {
	cfg = cfg.withDefaults()
	if cfg.GroupID == "" {
		return nil, fmt.Errorf("xkafka: GroupID is required for consumer")
	}

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        cfg.Brokers,
		Topic:          cfg.Topic,
		GroupID:        cfg.GroupID,
		CommitInterval: 0, // 0 = 同步手动提交，只在 CommitMessages 时提交
		// MinBytes/MaxBytes 保持默认即可；课程项目不必调优
	})

	return &Consumer{reader: r}, nil
}

// FetchMessage 拉取一条消息但不提交 offset。
// 必须配合 CommitMessages 使用：处理成功才 commit。
func (c *Consumer) FetchMessage(ctx context.Context) (kafka.Message, error) {
	if c.reader == nil {
		return kafka.Message{}, fmt.Errorf("xkafka: consumer is not initialized")
	}
	return c.reader.FetchMessage(ctx)
}

// CommitMessages 提交 offset。只在消息处理成功后调用。
func (c *Consumer) CommitMessages(ctx context.Context, msgs ...kafka.Message) error {
	if c.reader == nil {
		return fmt.Errorf("xkafka: consumer is not initialized")
	}
	return c.reader.CommitMessages(ctx, msgs...)
}

// Close 关闭 consumer，释放 consumer group 成员资格。
func (c *Consumer) Close() error {
	if c.reader == nil {
		return nil
	}
	return c.reader.Close()
}
