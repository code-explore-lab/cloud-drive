package xkafka

import (
	"context"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
)

// Producer 封装 kafka-go 的 Writer，提供更简单的发布接口。
type Producer struct {
	writer *kafka.Writer
}

// NewProducer 创建并配置一个同步 Producer。
// 构造函数不连接 broker：Kafka 晚于进程启动时，Publish 会返回错误，由调用方重试。
func NewProducer(cfg Config) *Producer {
	cfg = cfg.withDefaults()

	w := &kafka.Writer{
		Addr:         kafka.TCP(cfg.Brokers...),
		Topic:        cfg.Topic,
		Balancer:     &kafka.Hash{},         // 相同 Key 进同一分区，保证同一对象的处理顺序
		RequiredAcks: kafka.RequireAll,      // 等所有 ISR 确认，防丢
		Async:        false,                 // 同步模式，WriteMessages 会阻塞到确认或超时
		BatchTimeout: 10 * time.Millisecond, // 控制延迟，课程项目可设小
		// 兜底：即使调用方漏了 EnsureTopics，首次发布也能让 broker 建 topic。
		// 生产环境仍建议显式调用 EnsureTopics 控制分区数和副本数。
		AllowAutoTopicCreation: true,
	}

	return &Producer{writer: w}
}

// Publish 发送一条消息。key 建议用 object_key，保证分区一致性。
// 空 key 会转成 nil，让 kafka.Hash 走轮询而不是把所有消息塞进同一个分区。
func (p *Producer) Publish(ctx context.Context, key string, value []byte) error {
	return p.PublishMessage(ctx, kafka.Message{
		Key:   messageKey(key),
		Value: value,
	})
}

// PublishMessage 发送完整 kafka.Message，保留 Headers 等字段。
// 死信转发靠它携带原始消息和诊断 header。
func (p *Producer) PublishMessage(ctx context.Context, msg kafka.Message) error {
	if p.writer == nil {
		return fmt.Errorf("xkafka: producer is not initialized")
	}
	return p.writer.WriteMessages(ctx, msg)
}

// PublishBatch 一次写入多条消息，减少逐条发送的往返。
//
// kafka-go 的 WriteMessages 要么全部成功，要么返回一个错误但不指出具体哪几条失败；
// 调用方在失败时应把整批当作「可能部分成功」处理（重投 + 消费端幂等），不要只重发一部分。
func (p *Producer) PublishBatch(ctx context.Context, msgs []kafka.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	if p.writer == nil {
		return fmt.Errorf("xkafka: producer is not initialized")
	}
	return p.writer.WriteMessages(ctx, msgs...)
}

// messageKey 把空 key 转成 nil。
// kafka.Hash 只对 nil key 做 round-robin；[]byte("") 是合法的非 nil key，
// 会被当成同一个 key 哈希，导致所有空 key 消息落到同一分区。
func messageKey(key string) []byte {
	if key == "" {
		return nil
	}
	return []byte(key)
}

// Close 刷新并关闭 Producer，进程退出前必须调用。
func (p *Producer) Close() error {
	if p.writer == nil {
		return nil
	}
	return p.writer.Close()
}
