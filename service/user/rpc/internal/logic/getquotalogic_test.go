package logic

import (
	"context"
	"testing"

	"cloud-drive/service/user/model"
	"cloud-drive/service/user/rpc/types/user"
)

func TestGetQuota_Success(t *testing.T) {
	quotas := newFakeQuotasModel()
	quotas.findByUserId = func(_ context.Context, userId uint64) (*model.UserQuotas, error) {
		return &model.UserQuotas{
			Id:            1,
			UserId:        userId,
			TotalBytes:    100,
			UsedBytes:     30,
			ReservedBytes: 20,
		}, nil
	}

	l := NewGetQuotaLogic(context.Background(), newTestSvc(nil, quotas, nil))
	resp, err := l.GetQuota(&user.GetQuotaRequest{UserId: 5})
	if err != nil {
		t.Fatalf("GetQuota: %v", err)
	}

	if resp.TotalBytes != 100 || resp.UsedBytes != 30 || resp.ReservedBytes != 20 {
		t.Fatalf("quota = %+v", resp)
	}
}

func TestGetQuota_NotFound(t *testing.T) {
	l := NewGetQuotaLogic(context.Background(), newTestSvc(nil, newFakeQuotasModel(), nil))

	if _, err := l.GetQuota(&user.GetQuotaRequest{UserId: 5}); err == nil {
		t.Fatal("额度记录不存在时应返回错误")
	}
}
