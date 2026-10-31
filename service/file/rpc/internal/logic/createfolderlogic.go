package logic

import (
	"context"
	"errors"

	"cloud-drive/service/file/model"
	"cloud-drive/service/file/rpc/internal/svc"
	"cloud-drive/service/file/rpc/types/file"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CreateFolderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateFolderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateFolderLogic {
	return &CreateFolderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CreateFolder 在指定目录下创建子目录。
// 校验三件事：名字合法、父目录属于自己、同目录下不重名。
func (l *CreateFolderLogic) CreateFolder(in *file.CreateFolderRequest) (*file.CreateFolderResponse, error) {
	if in.OwnerId <= 0 || in.ParentId < 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}
	name, err := validateName(in.Name)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid folder name")
	}

	ownerId := uint64(in.OwnerId)
	parentId := uint64(in.ParentId)

	if parentId > 0 {
		parent, err := l.svcCtx.FoldersModel.FindOne(l.ctx, parentId)
		if errors.Is(err, model.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "parent folder not found")
		}
		if err != nil {
			l.Errorf("find parent folder failed: %v", err)
			return nil, status.Error(codes.Internal, "internal error")
		}
		if parent.OwnerId != ownerId || parent.DeletedAt.Valid {
			return nil, status.Error(codes.NotFound, "parent folder not found")
		}
	}

	if err := ensureNameAvailable(l.ctx, l.svcCtx, ownerId, parentId, name, 0); err != nil {
		return nil, err
	}

	res, err := l.svcCtx.FoldersModel.Insert(l.ctx, &model.Folders{
		OwnerId:  ownerId,
		ParentId: parentId,
		Name:     name,
	})
	if err != nil {
		l.Errorf("insert folder failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	id, err := res.LastInsertId()
	if err != nil {
		l.Errorf("get last insert id failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &file.CreateFolderResponse{FolderId: id}, nil
}
