package ratelimit

import (
	"testing"
	"time"
)

func TestLimiterAllowsBurstThenRejects(t *testing.T) {
	l := New(1, 3) // 每秒 1 个，桶容量 3
	now := time.Unix(0, 0)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if l.Allow("1.2.3.4") {
		t.Fatal("4th request should be rejected: burst is 3")
	}
}

func TestLimiterRefillsOverTime(t *testing.T) {
	l := New(1, 1)
	now := time.Unix(0, 0)
	l.now = func() time.Time { return now }

	if !l.Allow("ip") {
		t.Fatal("first request should be allowed")
	}
	if l.Allow("ip") {
		t.Fatal("second request should be rejected")
	}

	now = now.Add(2 * time.Second) // 补满 2 个令牌，但上限是 1
	if !l.Allow("ip") {
		t.Fatal("request after refill should be allowed")
	}
	if l.Allow("ip") {
		t.Fatal("burst capacity must cap the bucket")
	}
}

func TestLimiterIsolatesKeys(t *testing.T) {
	l := New(1, 1)
	now := time.Unix(0, 0)
	l.now = func() time.Time { return now }

	if !l.Allow("a") {
		t.Fatal("a should be allowed")
	}
	if !l.Allow("b") {
		t.Fatal("b should have its own bucket")
	}
	if l.Allow("a") {
		t.Fatal("a should now be limited")
	}
}
