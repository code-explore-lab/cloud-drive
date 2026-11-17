package xkafka

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewEnvelope(t *testing.T) {
	before := time.Now().Add(-time.Second)

	env, err := NewEnvelope(EventTypeObjectCleanupRequested, ObjectCleanupRequested{
		ObjectKey: "u/7/upload-1.txt",
	})
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}

	if env.Version != EnvelopeVersion {
		t.Fatalf("Version = %d, want %d", env.Version, EnvelopeVersion)
	}
	if env.Type != EventTypeObjectCleanupRequested {
		t.Fatalf("Type = %q, want %q", env.Type, EventTypeObjectCleanupRequested)
	}
	if _, err := uuid.Parse(env.EventID); err != nil {
		t.Fatalf("EventID %q 不是合法 UUID: %v", env.EventID, err)
	}
	if env.OccurredAt.Location() != time.UTC {
		t.Fatalf("OccurredAt 应为 UTC，实际 location = %s", env.OccurredAt.Location())
	}
	if env.OccurredAt.Before(before) || env.OccurredAt.After(time.Now().Add(time.Second)) {
		t.Fatalf("OccurredAt 不在合理时间范围内: %s", env.OccurredAt)
	}

	var payload ObjectCleanupRequested
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.ObjectKey != "u/7/upload-1.txt" {
		t.Fatalf("ObjectKey = %q, want u/7/upload-1.txt", payload.ObjectKey)
	}
}

func TestNewEnvelopeKeepsPayloadAsRawJSON(t *testing.T) {
	env, err := NewEnvelope(EventTypeObjectCleanupRequested, ObjectCleanupRequested{ObjectKey: "k"})
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}

	// json.RawMessage 应保留原始 JSON，而不是被 base64 编码。
	if got := string(env.Payload); got != `{"object_key":"k"}` {
		t.Fatalf("Payload = %s, want {\"object_key\":\"k\"}", got)
	}
}

func TestNewEnvelopeGeneratesUniqueEventID(t *testing.T) {
	first, err := NewEnvelope(EventTypeObjectCleanupRequested, ObjectCleanupRequested{ObjectKey: "a"})
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	second, err := NewEnvelope(EventTypeObjectCleanupRequested, ObjectCleanupRequested{ObjectKey: "b"})
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}

	if first.EventID == second.EventID {
		t.Fatalf("两次生成的 EventID 相同: %s", first.EventID)
	}
}

func TestNewEnvelopeRejectsInvalidInput(t *testing.T) {
	if _, err := NewEnvelope("", struct{}{}); err == nil {
		t.Fatal("空事件类型应返回错误")
	}
	if _, err := NewEnvelope(EventTypeObjectCleanupRequested, make(chan int)); err == nil {
		t.Fatal("不可序列化的 payload 应返回错误")
	}
}

func TestEnvelopeJSONRoundTripIgnoresUnknownFields(t *testing.T) {
	// 模拟未来版本新增字段：旧消费者应忽略未知字段，仍能解析已知字段。
	raw := `{
		"version": 1,
		"type": "storage.object.cleanup.requested",
		"event_id": "6f1b1c1e-0d5a-4b8f-9a2e-2d0a3b4c5d6e",
		"occurred_at": "2026-11-17T03:04:05Z",
		"payload": {"object_key": "u/1/a", "future_field": true},
		"future_top_level": "ignored"
	}`

	var env Envelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.Version != EnvelopeVersion {
		t.Fatalf("Version = %d, want %d", env.Version, EnvelopeVersion)
	}
	if env.EventID != "6f1b1c1e-0d5a-4b8f-9a2e-2d0a3b4c5d6e" {
		t.Fatalf("EventID = %q", env.EventID)
	}
	if !env.OccurredAt.Equal(time.Date(2026, 11, 17, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("OccurredAt = %s", env.OccurredAt)
	}

	var payload ObjectCleanupRequested
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.ObjectKey != "u/1/a" {
		t.Fatalf("ObjectKey = %q, want u/1/a", payload.ObjectKey)
	}

	encoded, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if strings.Contains(string(encoded), "future_top_level") {
		t.Fatalf("重新序列化不应带上未知字段: %s", encoded)
	}
}
