package xkafka

import "testing"

func TestNewConsumerRequiresGroupID(t *testing.T) {
	_, err := NewConsumer(Config{Topic: TopicStorageEvents})
	if err == nil {
		t.Fatal("GroupID 为空时应返回错误")
	}
}

func TestNewConsumerAppliesDefaults(t *testing.T) {
	// Reader 在首次读取前不会连接 broker，也不启动 goroutine，
	// 这里只验证构造结果，不做 Close（Close 会等 group session 超时，拖慢单测）。
	c, err := NewConsumer(Config{GroupID: "storage-worker"})
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}

	cfg := c.reader.Config()
	if cfg.Topic != TopicStorageEvents {
		t.Fatalf("Topic = %q, want %q", cfg.Topic, TopicStorageEvents)
	}
	if len(cfg.Brokers) != 1 || cfg.Brokers[0] != "redpanda:9092" {
		t.Fatalf("Brokers = %v, want [redpanda:9092]", cfg.Brokers)
	}
	if cfg.GroupID != "storage-worker" {
		t.Fatalf("GroupID = %q, want storage-worker", cfg.GroupID)
	}
	if cfg.CommitInterval != 0 {
		t.Fatalf("CommitInterval = %s, want 0（手动提交）", cfg.CommitInterval)
	}
}
