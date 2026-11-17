package xkafka

import "testing"

func TestConfigWithDefaults(t *testing.T) {
	got := Config{}.withDefaults()

	if len(got.Brokers) != 1 || got.Brokers[0] != "redpanda:9092" {
		t.Fatalf("Brokers = %v, want [redpanda:9092]", got.Brokers)
	}
	if got.Topic != TopicStorageEvents {
		t.Fatalf("Topic = %q, want %q", got.Topic, TopicStorageEvents)
	}
}

func TestConfigWithDefaultsReturnsBrokerCopy(t *testing.T) {
	got := Config{}.withDefaults()
	got.Brokers[0] = "mutated:9092"

	if defaultBrokers[0] != "redpanda:9092" {
		t.Fatalf("包级默认 broker 被调用方修改: %v", defaultBrokers)
	}
}

func TestConfigWithDefaultsKeepsExplicitValues(t *testing.T) {
	got := Config{
		Brokers: []string{"kafka:9093", "kafka:9094"},
		Topic:   "custom.topic",
		GroupID: "storage-worker",
	}.withDefaults()

	if len(got.Brokers) != 2 || got.Brokers[0] != "kafka:9093" || got.Brokers[1] != "kafka:9094" {
		t.Fatalf("Brokers 被覆盖: %v", got.Brokers)
	}
	if got.Topic != "custom.topic" {
		t.Fatalf("Topic = %q, want custom.topic", got.Topic)
	}
	if got.GroupID != "storage-worker" {
		t.Fatalf("GroupID = %q, want storage-worker", got.GroupID)
	}
}
