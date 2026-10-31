package logic

import (
	"context"
	"errors"
	"time"

	"cloud-drive/service/storage/model"
	"cloud-drive/service/storage/rpc/internal/svc"
	"cloud-drive/service/storage/rpc/types/storage"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type DeleteObjectLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteObjectLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteObjectLogic {
	return &DeleteObjectLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// DeleteObject 删除对象并更新元数据。
//
// 失败语义：删除对象失败不返回 gRPC 错误，而是把记录置为 deleting 并累加失败次数，
// 由后台 GC 任务重试；调用方（文件删除流程）因此不会被外部存储的临时故障卡住。
// 返回值 Success=false 表示"还没删掉，已排队重试"。
func (l *DeleteObjectLogic) DeleteObject(in *storage.DeleteObjectRequest) (*storage.DeleteObjectResponse, error) {
	if in.ObjectKey == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}

	obj, err := l.svcCtx.ObjectsModel.FindOneByObjectKey(l.ctx, in.ObjectKey)
	if errors.Is(err, model.ErrNotFound) {
		// 幂等：对象本就不存在，删除视为成功。
		return &storage.DeleteObjectResponse{Success: true}, nil
	}
	if err != nil {
		l.Errorf("find storage object failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if obj.Status == "deleted" {
		return &storage.DeleteObjectResponse{Success: true}, nil
	}

	if err := l.svcCtx.Store.Remove(l.ctx, in.ObjectKey); err != nil {
		obj.Status = "deleting"
		obj.GcAttempts++
		obj.LastError = truncate(err.Error(), 500)
		if updateErr := l.svcCtx.ObjectsModel.Update(l.ctx, obj); updateErr != nil {
			l.Errorf("mark storage object deleting failed: %v", updateErr)
		}
		l.Errorf("remove object %s failed, queued for retry: %v", in.ObjectKey, err)
		return &storage.DeleteObjectResponse{Success: false}, nil
	}

	obj.Status = "deleted"
	obj.DeletedAt.Valid = true
	obj.DeletedAt.Time = time.Now()
	obj.LastError = ""
	if err := l.svcCtx.ObjectsModel.Update(l.ctx, obj); err != nil {
		l.Errorf("mark storage object deleted failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	l.Infof("object deleted: object_key=%s size=%d", obj.ObjectKey, obj.Size)
	return &storage.DeleteObjectResponse{Success: true}, nil
}
