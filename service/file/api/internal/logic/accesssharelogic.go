package logic

import (
	"context"
	"net/url"
	"strings"

	"cloud-drive/service/file/api/internal/svc"
	"cloud-drive/service/file/api/internal/types"
	"cloud-drive/service/file/rpc/fileclient"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AccessShareLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAccessShareLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AccessShareLogic {
	return &AccessShareLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AccessShare 是公开接口：不需要登录，用分享 token + 可选密码换取文件元信息与下载地址。
// 密码错误 / 分享过期 / 次数用尽都会返回 granted=false，而不是 5xx。
func (l *AccessShareLogic) AccessShare(req *types.AccessShareReq) (resp *types.AccessShareResp, err error) {
	if req.Token == "" {
		return nil, status.Error(codes.InvalidArgument, "token is required")
	}

	rpcResp, err := l.svcCtx.FileRpc.AccessShare(l.ctx, &fileclient.AccessShareRequest{
		Token:    req.Token,
		Password: req.Password,
	})
	if err != nil {
		return nil, err
	}

	resp = &types.AccessShareResp{
		Granted:     rpcResp.Granted,
		FileName:    rpcResp.FileName,
		Size:        rpcResp.Size,
		ContentType: rpcResp.ContentType,
	}
	if rpcResp.Granted {
		base := strings.TrimRight(l.svcCtx.Config.Share.BaseUrl, "/")
		resp.DownloadURL = base + "/v1/shares/" + url.PathEscape(req.Token) + "/content"
	}
	return resp, nil
}
