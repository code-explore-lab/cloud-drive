package logic

import (
	"context"
	"errors"
	"testing"

	"cloud-drive/service/user/model"
	"cloud-drive/service/user/rpc/types/user"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestReserveQuota_Success(t *testing.T) {
	quotas := newFakeQuotasModel()
	quotas.reserveQuota = func(_ context.Context, userId, bytes uint64) (bool, error) {
		if userId != 1 || bytes != 500 {
			t.Fatalf("ReserveQuota(%d, %d), want (1, 500)", userId, bytes)
		}
		return true, nil
	}
	quotas.findByUserId = func(_ context.Context, userId uint64) (*model.UserQuotas, error) {
		return &model.UserQuotas{UserId: userId, ReservedBytes: 800}, nil
	}

	l := NewReserveQuotaLogic(context.Background(), newTestSvc(nil, quotas, nil))
	resp, err := l.ReserveQuota(&user.ReserveQuotaRequest{UserId: 1, RequestBytes: 500, RequestId: "req-1"})
	if err != nil {
		t.Fatalf("ReserveQuota: %v", err)
	}
	if !resp.Success {
		t.Fatalf("Success = false, reason = %q", resp.Reason)
	}
	if resp.ReservedBytes != 800 {
		t.Fatalf("ReservedBytes = %d, want 800", resp.ReservedBytes)
	}
}

func TestReserveQuota_Insufficient(t *testing.T) {
	quotas := newFakeQuotasModel()
	quotas.reserveQuota = func(context.Context, uint64, uint64) (bool, error) {
		return false, nil // 条件更新没有命中，说明额度不够
	}

	l := NewReserveQuotaLogic(context.Background(), newTestSvc(nil, quotas, nil))
	resp, err := l.ReserveQuota(&user.ReserveQuotaRequest{UserId: 1, RequestBytes: 999999})
	if err != nil {
		t.Fatalf("ReserveQuota: %v", err)
	}
	if resp.Success {
		t.Fatal("额度不足时 Success 应为 false")
	}
	if resp.Reason != "insufficient quota" {
		t.Fatalf("Reason = %q", resp.Reason)
	}
}

func TestReserveQuota_InvalidArgument(t *testing.T) {
	called := false
	quotas := newFakeQuotasModel()
	quotas.reserveQuota = func(context.Context, uint64, uint64) (bool, error) {
		called = true
		return true, nil
	}

	l := NewReserveQuotaLogic(context.Background(), newTestSvc(nil, quotas, nil))
	resp, err := l.ReserveQuota(&user.ReserveQuotaRequest{UserId: 0, RequestBytes: 100})
	if err != nil {
		t.Fatalf("ReserveQuota: %v", err)
	}
	if resp.Success {
		t.Fatal("非法参数时 Success 应为 false")
	}
	if called {
		t.Fatal("参数非法时不应访问数据库")
	}
}

func TestReserveQuota_DBError(t *testing.T) {
	quotas := newFakeQuotasModel()
	quotas.reserveQuota = func(context.Context, uint64, uint64) (bool, error) {
		return false, errors.New("db down")
	}

	l := NewReserveQuotaLogic(context.Background(), newTestSvc(nil, quotas, nil))
	_, err := l.ReserveQuota(&user.ReserveQuotaRequest{UserId: 1, RequestBytes: 100})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal", status.Code(err))
	}
}
