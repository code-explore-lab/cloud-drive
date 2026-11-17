package jobs

import (
	"encoding/json"
	"testing"
	"time"

	"cloud-drive/common/xkafka"
	"cloud-drive/service/storage/model"
)

func TestBackoffFor(t *testing.T) {
	cases := []struct {
		attempts uint64
		want     time.Duration
	}{
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{20, dispatcherBackoffMax},
	}
	for _, c := range cases {
		if got := backoffFor(c.attempts); got != c.want {
			t.Fatalf("backoffFor(%d) = %s, want %s", c.attempts, got, c.want)
		}
	}
}

func TestToMessageBuildsEnvelope(t *testing.T) {
	created := time.Date(2026, 11, 17, 8, 30, 0, 0, time.FixedZone("CST", 8*3600))
	event := &model.OutboxEvents{
		Id:          7,
		EventId:     "evt-1",
		EventType:   xkafka.EventTypeObjectCleanupRequested,
		AggregateId: "u/1/a.txt",
		Payload:     `{"object_key":"u/1/a.txt"}`,
		CreatedAt:   created,
	}

	msg, err := toMessage(event)
	if err != nil {
		t.Fatalf("toMessage: %v", err)
	}
	if string(msg.Key) != event.AggregateId {
		t.Fatalf("key = %q, want %q", msg.Key, event.AggregateId)
	}
	if len(msg.Headers) != 1 || msg.Headers[0].Key != "x-event-id" ||
		string(msg.Headers[0].Value) != event.EventId {
		t.Fatalf("headers = %+v", msg.Headers)
	}

	var env xkafka.Envelope
	if err := json.Unmarshal(msg.Value, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.Version != xkafka.EnvelopeVersion || env.Type != event.EventType ||
		env.EventID != event.EventId {
		t.Fatalf("envelope = %+v", env)
	}
	if !env.OccurredAt.Equal(created) || env.OccurredAt.Location() != time.UTC {
		t.Fatalf("occurred_at = %s, want UTC instant of %s", env.OccurredAt, created)
	}
	if string(env.Payload) != event.Payload {
		t.Fatalf("payload = %s, want %s", env.Payload, event.Payload)
	}
}

func TestToMessageEmptyAggregateUsesNilKey(t *testing.T) {
	msg, err := toMessage(&model.OutboxEvents{
		EventId:   "evt-2",
		EventType: xkafka.EventTypeObjectCleanupRequested,
		Payload:   `{}`,
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("toMessage: %v", err)
	}
	if msg.Key != nil {
		t.Fatalf("key = %q, want nil（让 kafka.Hash 轮询分区）", msg.Key)
	}
}

func TestToMessageRejectsMalformedEvents(t *testing.T) {
	cases := []struct {
		name  string
		event model.OutboxEvents
	}{
		{"missing type", model.OutboxEvents{Payload: `{}`}},
		{"empty payload", model.OutboxEvents{EventType: xkafka.EventTypeObjectCleanupRequested}},
		{"invalid payload", model.OutboxEvents{
			EventType: xkafka.EventTypeObjectCleanupRequested,
			Payload:   `{not json`,
		}},
	}
	for _, c := range cases {
		if _, err := toMessage(&c.event); err == nil {
			t.Fatalf("%s: toMessage 应返回错误", c.name)
		}
	}
}
