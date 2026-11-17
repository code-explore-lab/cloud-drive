package xkafka

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	// TopicStorageEvents 是对象清理等存储事件的主 topic。
	TopicStorageEvents = "storage.events"
	// TopicStorageEventsDLQ 是存储事件的死信 topic。
	TopicStorageEventsDLQ = "storage.events.dlq"
	// EventTypeObjectCleanupRequested 表示「对象待清理」事件。
	EventTypeObjectCleanupRequested = "storage.object.cleanup.requested"
	// EnvelopeVersion 是当前事件信封版本，消费者按它做版本分发。
	EnvelopeVersion = 1
)

// Envelope 是所有业务事件共用的信封。
// 新增字段只增不改；消费者应忽略未知字段，并对更高版本做明确处理（DLQ 或跳过）。
type Envelope struct {
	Version    int             `json:"version"`     // 当前 = 1，向前兼容
	Type       string          `json:"type"`        // 如 "storage.object.cleanup.requested"
	EventID    string          `json:"event_id"`    // 全局唯一，用于去重与排查
	OccurredAt time.Time       `json:"occurred_at"` // UTC
	Payload    json.RawMessage `json:"payload"`
}

// ObjectCleanupRequested 是「对象待清理」事件的 payload。
type ObjectCleanupRequested struct {
	ObjectKey string `json:"object_key"`
}

// NewEnvelope 组装一个可发布的信封：统一填充版本号、事件 ID 和 UTC 发生时间，
// 并把 payload 序列化成原始 JSON。调用方不要自己拼这些字段，避免各处版本号漂移。
func NewEnvelope(typ string, payload any) (Envelope, error) {
	if typ == "" {
		return Envelope{}, fmt.Errorf("xkafka: event type is required")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("xkafka: marshal payload: %w", err)
	}

	return Envelope{
		Version:    EnvelopeVersion,
		Type:       typ,
		EventID:    uuid.NewString(),
		OccurredAt: time.Now().UTC(),
		Payload:    body,
	}, nil
}
