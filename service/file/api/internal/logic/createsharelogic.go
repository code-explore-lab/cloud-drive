package logic

import (
	"context"
	"strings"

	"cloud-drive/common/ctxdata"
	"cloud-drive/service/file/api/internal/svc"
	"cloud-drive/service/file/api/internal/types"
	"cloud-drive/service/file/rpc/fileclient"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CreateShareLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateShareLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateShareLogic {
	return &CreateShareLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// CreateShare 创建分享链接。返回的 token 只在这里出现一次，库里只存摘要。
func (l *CreateShareLogic) CreateShare(req *types.CreateShareReq) (resp *types.CreateShareResp, err error) {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}
	if req.FileID <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid file id")
	}

	rpcResp, err := l.svcCtx.FileRpc.CreateShare(l.ctx, &fileclient.CreateShareRequest{
		OwnerId:        int64(userId),
		FileId:         req.FileID,
		Password:       req.Password,
		ExpiresAt:      req.ExpiresAt,
		MaxAccessCount: req.MaxAccessCount,
	})
	if err != nil {
		return nil, err
	}

	base := strings.TrimRight(l.svcCtx.Config.Share.BaseUrl, "/")
	return &types.CreateShareResp{
		ShareID:   rpcResp.ShareId,
		Token:     rpcResp.Token,
		URL:       base + "/v1/shares/" + rpcResp.Token,
		ExpiresAt: rpcResp.ExpiresAt,
	}, nil
}
