package consumer

import (
	"errors"
	"strings"
	"testing"
	"time"

	"cloud-drive/common/xkafka"

	"github.com/segmentio/kafka-go"
)

func TestNextBackoff(t *testing.T) {
	max := 60 * time.Second
	if got := nextBackoff(2*time.Second, max); got != 4*time.Second {
		t.Fatalf("nextBackoff = %s, want 4s", got)
	}
	if got := nextBackoff(40*time.Second, max); got != max {
		t.Fatalf("nextBackoff 应封顶到 %s，实际 %s", max, got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abcdef", 3); got != "abc" {
		t.Fatalf("truncate = %q", got)
	}
	if got := truncate("abc", 10); got != "abc" {
		t.Fatalf("truncate 短串 = %q", got)
	}
}

func TestDlqMessageKeepsOriginalPayload(t *testing.T) {
	original := kafka.Message{
		Key:     []byte("u/1/a.txt"),
		Value:   []byte(`{"version":1}`),
		Headers: []kafka.Header{{Key: "x-event-id", Value: []byte("evt-1")}},
	}

	msg := dlqMessage(original, errors.New("minio down"), 3)

	if msg.Topic != xkafka.TopicStorageEventsDLQ {
		t.Fatalf("topic = %q, want %q", msg.Topic, xkafka.TopicStorageEventsDLQ)
	}
	if string(msg.Key) != string(original.Key) || string(msg.Value) != string(original.Value) {
		t.Fatalf("原始 key/value 被修改: key=%q value=%q", msg.Key, msg.Value)
	}

	headers := map[string]string{}
	for _, h := range msg.Headers {
		headers[h.Key] = string(h.Value)
	}
	if headers["x-event-id"] != "evt-1" {
		t.Fatalf("原始 header 丢失: %v", headers)
	}
	if headers["x-error"] != "minio down" || headers["x-attempts"] != "3" {
		t.Fatalf("诊断 header = %v", headers)
	}
	if _, err := time.Parse(time.RFC3339, headers["x-failed-at"]); err != nil {
		t.Fatalf("x-failed-at 不是 RFC3339: %q", headers["x-failed-at"])
	}
}

func TestDlqMessageTruncatesLongError(t *testing.T) {
	msg := dlqMessage(kafka.Message{}, errors.New(strings.Repeat("x", maxLastErrorLen+50)), 1)
	for _, h := range msg.Headers {
		if h.Key == "x-error" && len(h.Value) != maxLastErrorLen {
			t.Fatalf("x-error 长度 = %d, want %d", len(h.Value), maxLastErrorLen)
		}
	}
}
