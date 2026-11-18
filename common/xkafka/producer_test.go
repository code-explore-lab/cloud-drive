package xkafka

import (
	"context"
	"testing"

	"github.com/segmentio/kafka-go"
)

func TestMessageKey(t *testing.T) {
	if got := messageKey(""); got != nil {
		t.Fatalf("空 key 应转成 nil（走轮询分区），实际 = %q", got)
	}

	got := messageKey("u/7/a.txt")
	if string(got) != "u/7/a.txt" {
		t.Fatalf("messageKey = %q, want u/7/a.txt", got)
	}
}

func TestPublishBatchEmptyIsNoop(t *testing.T) {
	// 空批次不应连接 broker，直接返回 nil。
	p := NewProducer(Config{Brokers: []string{"127.0.0.1:1"}})
	defer func() {
		if err := p.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	if err := p.PublishBatch(context.Background(), nil); err != nil {
		t.Fatalf("PublishBatch(nil): %v", err)
	}
}

func TestNormalizeTopic(t *testing.T) {
	// 与 Writer 相同的 topic 应清空，否则 kafka-go 会拒绝发送。
	msg := normalizeTopic(TopicStorageEventsDLQ, kafka.Message{Topic: TopicStorageEventsDLQ})
	if msg.Topic != "" {
		t.Fatalf("相同 topic 未清空: %q", msg.Topic)
	}

	// 不同 topic 保留原样，让 kafka-go 报错而不是静默改投。
	msg = normalizeTopic(TopicStorageEventsDLQ, kafka.Message{Topic: TopicStorageEvents})
	if msg.Topic != TopicStorageEvents {
		t.Fatalf("不同 topic 被改写: %q", msg.Topic)
	}

	// 消息没指定 topic 时保持为空，走 Writer 的 topic。
	msg = normalizeTopic(TopicStorageEvents, kafka.Message{})
	if msg.Topic != "" {
		t.Fatalf("空 topic 被改写: %q", msg.Topic)
	}
}

func TestNewProducerAppliesDefaults(t *testing.T) {
	// 不连接 broker，只验证构造与 Close 不 panic。
	p := NewProducer(Config{})
	defer func() {
		if err := p.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	if p.writer == nil {
		t.Fatal("writer 未初始化")
	}
	if p.writer.Addr.String() == "" || p.writer.Topic != TopicStorageEvents {
		t.Fatalf("writer 默认值不正确: addr=%s topic=%s", p.writer.Addr, p.writer.Topic)
	}
	if !p.writer.AllowAutoTopicCreation {
		t.Fatal("AllowAutoTopicCreation 应为 true")
	}
}
