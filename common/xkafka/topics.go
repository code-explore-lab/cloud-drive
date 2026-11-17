package xkafka

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/segmentio/kafka-go"
)

const (
	// defaultTopicPartitions 是自动创建 topic 时的分区数。
	// 同一 object_key 的事件固定进同一分区，多分区可以并行消费不同对象。
	defaultTopicPartitions = 3
	// defaultReplicationFactor 适配单节点 Redpanda；换成多 broker 集群时再调大。
	defaultReplicationFactor = 1
)

// EnsureTopics 幂等地创建 topic（已存在时无副作用）。
//
// 应在服务启动时调用，并由调用方带重试：broker 比进程晚启动时重试能自愈。
// 不能只依赖 Writer.AllowAutoTopicCreation，因为 broker 侧通常关闭了自动建 topic，
// 且自动建出来的分区数/副本数不受控。
func EnsureTopics(ctx context.Context, brokers []string, topics ...string) error {
	if len(topics) == 0 {
		return nil
	}
	if len(brokers) == 0 {
		brokers = defaultBrokers
	}

	conn, err := dialAny(ctx, brokers)
	if err != nil {
		return fmt.Errorf("xkafka: dial brokers: %w", err)
	}
	defer conn.Close()

	// 建 topic 必须发给 controller，普通 broker 会返回 NotController。
	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("xkafka: find controller: %w", err)
	}
	addr := net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port))
	ctrl, err := kafka.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("xkafka: dial controller %s: %w", addr, err)
	}
	defer ctrl.Close()

	configs := make([]kafka.TopicConfig, 0, len(topics))
	for _, topic := range topics {
		configs = append(configs, kafka.TopicConfig{
			Topic:             topic,
			NumPartitions:     defaultTopicPartitions,
			ReplicationFactor: defaultReplicationFactor,
		})
	}

	// CreateTopics 对已存在的 topic 返回 TopicAlreadyExists，kafka-go 内部会忽略。
	if err := ctrl.CreateTopics(configs...); err != nil {
		return fmt.Errorf("xkafka: create topics: %w", err)
	}
	return nil
}

// dialAny 依次尝试 broker 列表，返回第一个连接成功的连接。
func dialAny(ctx context.Context, brokers []string) (*kafka.Conn, error) {
	var lastErr error
	for _, broker := range brokers {
		conn, err := kafka.DialContext(ctx, "tcp", broker)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
