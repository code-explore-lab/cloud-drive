package xkafka

import (
	"context"
	"testing"
)

func TestEnsureTopicsNoopWithoutTopics(t *testing.T) {
	// 没有 topic 时不应尝试连接 broker，因此传入不可达地址也应立即成功。
	if err := EnsureTopics(context.Background(), []string{"127.0.0.1:1"}); err != nil {
		t.Fatalf("EnsureTopics 空列表: %v", err)
	}
}
