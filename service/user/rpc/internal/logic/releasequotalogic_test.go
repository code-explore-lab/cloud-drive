package logic

import (
	"context"
	"errors"
	"testing"

	"cloud-drive/service/user/rpc/types/user"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestReleaseQuota_Success(t *testing.T) {
	quotas := newFakeQuotasModel()
	quotas.releaseQuota = func(_ context.Context, userId, bytes uint64) (bool, error) {
		if userId != 1 || bytes != 300 {
			t.Fatalf("ReleaseQuota(%d, %d), want (1, 300)", userId, bytes)
		}
		return true, nil
	}

	l := NewReleaseQuotaLogic(context.Background(), newTestSvc(nil, quotas, nil))
	resp, err := l.ReleaseQuota(&user.ReleaseQuotaRequest{UserId: 1, ReleaseBytes: 300})
	if err != nil {
		t.Fatalf("ReleaseQuota: %v", err)
	}
	if !resp.Success {
		t.Fatalf("Success = false, reason = %q", resp.Reason)
	}
}

func TestReleaseQuota_ExceedsReserved(t *testing.T) {
	quotas := newFakeQuotasModel()
	quotas.releaseQuota = func(context.Context, uint64, uint64) (bool, error) {
		return false, nil
	}

	l := NewReleaseQuotaLogic(context.Background(), newTestSvc(nil, quotas, nil))
	resp, err := l.ReleaseQuota(&user.ReleaseQuotaRequest{UserId: 1, ReleaseBytes: 100})
	if err != nil {
		t.Fatalf("ReleaseQuota: %v", err)
	}
	if resp.Success {
		t.Fatal("释放量超过已预留量时 Success 应为 false")
	}
	if resp.Reason != "release exceeds reserved quota" {
		t.Fatalf("Reason = %q", resp.Reason)
	}
}

func TestReleaseQuota_InvalidArgument(t *testing.T) {
	quotas := newFakeQuotasModel()

	l := NewReleaseQuotaLogic(context.Background(), newTestSvc(nil, quotas, nil))
	resp, err := l.ReleaseQuota(&user.ReleaseQuotaRequest{UserId: 1, ReleaseBytes: 0})
	if err != nil {
		t.Fatalf("ReleaseQuota: %v", err)
	}
	if resp.Success {
		t.Fatal("非法参数时 Success 应为 false")
	}
}

func TestReleaseQuota_DBError(t *testing.T) {
	quotas := newFakeQuotasModel()
	quotas.releaseQuota = func(context.Context, uint64, uint64) (bool, error) {
		return false, errors.New("db down")
	}

	l := NewReleaseQuotaLogic(context.Background(), newTestSvc(nil, quotas, nil))
	_, err := l.ReleaseQuota(&user.ReleaseQuotaRequest{UserId: 1, ReleaseBytes: 100})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal", status.Code(err))
	}
}
