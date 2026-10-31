package logic

import (
	"context"
	"regexp"
	"strings"

	"cloud-drive/common/metrics"
	"cloud-drive/service/file/api/internal/svc"
	"cloud-drive/service/user/rpc/userclient"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var requestIdPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

// normalizeRequestId 校验客户端提供的幂等键。
// 允许的字符集是刻意收紧的：这个值会进入日志和唯一索引，不适合放任意字符串。
func normalizeRequestId(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", nil
	}
	if !requestIdPattern.MatchString(key) {
		return "", status.Error(codes.InvalidArgument,
			"idempotencyKey must be 8-64 chars of [A-Za-z0-9._-]")
	}
	return key, nil
}

// reserveQuota 预占配额。额度不足时返回 ResourceExhausted（HTTP 429），
// 让客户端能明确区分"参数错"和"空间不够"。
func reserveQuota(ctx context.Context, svcCtx *svc.ServiceContext, userId, bytes int64,
	requestId string) error {
	if bytes <= 0 {
		return nil
	}
	resp, err := svcCtx.UserRpc.ReserveQuota(ctx, &userclient.ReserveQuotaRequest{
		UserId:       userId,
		RequestBytes: bytes,
		RequestId:    requestId,
	})
	if err != nil {
		logx.WithContext(ctx).Errorf("reserve quota failed: user_id=%d bytes=%d err=%v", userId, bytes, err)
		return err
	}
	if !resp.Success {
		metrics.QuotaRejectedTotal.Inc()
		return status.Errorf(codes.ResourceExhausted, "quota exceeded: %s", resp.Reason)
	}
	return nil
}

// releaseReserved 归还预占额度（上传取消/失败）。
func releaseReserved(ctx context.Context, svcCtx *svc.ServiceContext, userId, bytes int64,
	requestId string) {
	if bytes <= 0 {
		return
	}
	if _, err := svcCtx.UserRpc.ReleaseQuota(ctx, &userclient.ReleaseQuotaRequest{
		UserId:       userId,
		ReleaseBytes: bytes,
		RequestId:    requestId,
	}); err != nil {
		logx.WithContext(ctx).Errorf("release reserved quota failed: user_id=%d bytes=%d err=%v",
			userId, bytes, err)
	}
}

// commitQuota 把预占额度转成已用额度。失败只记录：文件内容已经落盘，
// 账目问题交给对账任务处理，不应该让用户看到一次"上传成功但其实失败"的结果。
func commitQuota(ctx context.Context, svcCtx *svc.ServiceContext, userId, bytes int64,
	requestId string) {
	if bytes <= 0 {
		return
	}
	resp, err := svcCtx.UserRpc.CommitQuota(ctx, &userclient.CommitQuotaRequest{
		UserId:    userId,
		Bytes:     bytes,
		RequestId: requestId,
	})
	if err != nil {
		logx.WithContext(ctx).Errorf("commit quota failed: user_id=%d bytes=%d err=%v",
			userId, bytes, err)
		return
	}
	if !resp.Success {
		logx.WithContext(ctx).Errorf("commit quota rejected: user_id=%d bytes=%d reason=%s",
			userId, bytes, resp.Reason)
	}
}

// releaseUsed 归还已用额度（删除文件后）。
func releaseUsed(ctx context.Context, svcCtx *svc.ServiceContext, userId, bytes int64,
	requestId string) {
	if bytes <= 0 {
		return
	}
	resp, err := svcCtx.UserRpc.ReleaseUsedQuota(ctx, &userclient.ReleaseUsedQuotaRequest{
		UserId:       userId,
		ReleaseBytes: bytes,
		RequestId:    requestId,
	})
	if err != nil {
		logx.WithContext(ctx).Errorf("release used quota failed: user_id=%d bytes=%d err=%v",
			userId, bytes, err)
		return
	}
	if !resp.Success {
		logx.WithContext(ctx).Errorf("release used quota rejected: user_id=%d bytes=%d reason=%s",
			userId, bytes, resp.Reason)
	}
}
