package logic

import (
	"context"
	"errors"
	"time"

	"cloud-drive/service/file/model"
	"cloud-drive/service/file/rpc/internal/svc"
	"cloud-drive/service/file/rpc/types/file"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type DeleteFileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteFileLogic {
	return &DeleteFileLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// DeleteFile 软删除文件，并把需要清理的存储对象返回给编排层。
//
// 这里只做本地数据库能保证的事：标记删除、撤销分享。
// 对象删除与配额归还是 file-api 编排的外部步骤（见 docs/consistency.md），
// 因为对象存储不参与 MySQL 事务。
// 幂等：重复删除返回 changed=false、released_bytes=0，避免重复归还配额。
func (l *DeleteFileLogic) DeleteFile(in *file.DeleteFileRequest) (*file.DeleteFileResponse, error) {
	if in.FileId <= 0 || in.OwnerId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}

	f, err := l.svcCtx.FilesModel.FindOne(l.ctx, uint64(in.FileId))
	if errors.Is(err, model.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "file not found")
	}
	if err != nil {
		l.Errorf("find file failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if f.OwnerId != uint64(in.OwnerId) {
		return nil, status.Error(codes.NotFound, "file not found")
	}
	if f.DeletedAt.Valid {
		return &file.DeleteFileResponse{Success: true, Changed: false}, nil
	}

	versions, err := l.svcCtx.VersionsModel.FindByFileId(l.ctx, f.Id)
	if err != nil {
		l.Errorf("list versions failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	objects := collectObjects(f, versions)

	if err := l.svcCtx.SharesModel.RevokeByFile(l.ctx, f.Id, f.OwnerId); err != nil {
		l.Errorf("revoke shares of file failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if err := l.svcCtx.FilesModel.MarkDeleted(l.ctx, []*model.Files{f}, time.Now()); err != nil {
		l.Errorf("mark file deleted failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	var released int64
	for _, obj := range objects {
		released += obj.Size
	}

	l.Infof("file deleted: file_id=%d owner_id=%d objects=%d released_bytes=%d",
		f.Id, f.OwnerId, len(objects), released)

	return &file.DeleteFileResponse{
		Success:       true,
		Changed:       true,
		ReleasedBytes: released,
		Objects:       objects,
	}, nil
}

// collectObjects 汇总文件用到的存储对象。同一个对象被多个版本引用时只算一次，
// 否则配额会被重复归还，删除时也会重复调用对象存储。
func collectObjects(f *model.Files, versions []*model.FileVersions) []*file.ObjectRef {
	seen := make(map[string]int64, len(versions)+1)
	order := make([]string, 0, len(versions)+1)

	add := func(key string, size uint64) {
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = int64(size)
		order = append(order, key)
	}

	if f.Status == "available" || f.ObjectKey != "" {
		add(f.ObjectKey, f.Size)
	}
	for _, v := range versions {
		add(v.ObjectKey, v.Size)
	}

	objects := make([]*file.ObjectRef, 0, len(order))
	for _, key := range order {
		objects = append(objects, &file.ObjectRef{ObjectKey: key, Size: seen[key]})
	}
	return objects
}
