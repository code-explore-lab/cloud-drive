package xkafka

import (
	"context"
	"testing"
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
