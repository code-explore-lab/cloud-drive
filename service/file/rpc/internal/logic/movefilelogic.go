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

// maxParentWalk 限制向上遍历父目录的层数，防止环形数据导致死循环。
const maxParentWalk = 64

type MoveFileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewMoveFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MoveFileLogic {
	return &MoveFileLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// MoveFile 把文件或目录移动到目标目录，可选同时改名。
//
// 校验顺序（每一步失败都会明确报错，而不是静默忽略）：
//  1. 目标目录存在且属于当前用户；
//  2. 目录移动不能形成环（目标不能是自己或自己的子孙）；
//  3. 目标目录下不能有同名项。
func (l *MoveFileLogic) MoveFile(in *file.MoveFileRequest) (*file.MoveFileResponse, error) {
	if in.FileId <= 0 || in.OwnerId <= 0 || in.TargetParentId < 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}

	ownerId := uint64(in.OwnerId)
	targetParentId := uint64(in.TargetParentId)
	if targetParentId == uint64(in.FileId) {
		return nil, status.Error(codes.InvalidArgument, "cannot move a folder into itself")
	}

	if err := l.checkTargetFolder(ownerId, targetParentId); err != nil {
		return nil, err
	}

	f, err := l.svcCtx.FilesModel.FindOne(l.ctx, uint64(in.FileId))
	switch {
	case err == nil:
		return l.moveFileEntry(f, ownerId, targetParentId, in.Name)
	case errors.Is(err, model.ErrNotFound):
		// 不是文件，尝试当作目录处理。
	default:
		l.Errorf("find file failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	folder, err := l.svcCtx.FoldersModel.FindOne(l.ctx, uint64(in.FileId))
	if errors.Is(err, model.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "file not found")
	}
	if err != nil {
		l.Errorf("find folder failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	return l.moveFolderEntry(folder, ownerId, targetParentId, in.Name)
}

func (l *MoveFileLogic) moveFileEntry(f *model.Files, ownerId, targetParentId uint64,
	newName string) (*file.MoveFileResponse, error) {
	if f.OwnerId != ownerId || f.DeletedAt.Valid {
		return nil, status.Error(codes.NotFound, "file not found")
	}

	name := f.Name
	if newName != "" {
		validated, err := validateName(newName)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid name")
		}
		name = validated
	}

	if f.ParentId == targetParentId && name == f.Name {
		return &file.MoveFileResponse{Success: true}, nil
	}
	if err := ensureNameAvailable(l.ctx, l.svcCtx, ownerId, targetParentId, name, f.Id); err != nil {
		return nil, err
	}

	f.ParentId = targetParentId
	f.Name = name
	if err := l.svcCtx.FilesModel.Update(l.ctx, f); err != nil {
		l.Errorf("update file failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	return &file.MoveFileResponse{Success: true}, nil
}

func (l *MoveFileLogic) moveFolderEntry(folder *model.Folders, ownerId, targetParentId uint64,
	newName string) (*file.MoveFileResponse, error) {
	if folder.OwnerId != ownerId || folder.DeletedAt.Valid {
		return nil, status.Error(codes.NotFound, "file not found")
	}

	name := folder.Name
	if newName != "" {
		validated, err := validateName(newName)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid name")
		}
		name = validated
	}

	if folder.ParentId == targetParentId && name == folder.Name {
		return &file.MoveFileResponse{Success: true}, nil
	}

	// 环检测：从目标目录向上走，如果遇到被移动目录自己，说明目标在其子树里。
	if err := l.ensureNoCycle(folder.Id, targetParentId); err != nil {
		return nil, err
	}
	if err := ensureNameAvailable(l.ctx, l.svcCtx, ownerId, targetParentId, name, 0); err != nil {
		return nil, err
	}

	folder.ParentId = targetParentId
	folder.Name = name
	if err := l.svcCtx.FoldersModel.Update(l.ctx, folder); err != nil {
		l.Errorf("update folder failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	return &file.MoveFileResponse{Success: true}, nil
}

func (l *MoveFileLogic) ensureNoCycle(folderId, targetParentId uint64) error {
	cur := targetParentId
	for i := 0; i < maxParentWalk; i++ {
		if cur == 0 {
			return nil
		}
		if cur == folderId {
			return status.Error(codes.InvalidArgument, "cannot move a folder into its own subtree")
		}
		parent, err := l.svcCtx.FoldersModel.FindOne(l.ctx, cur)
		if errors.Is(err, model.ErrNotFound) {
			return status.Error(codes.NotFound, "target folder not found")
		}
		if err != nil {
			l.Errorf("walk parent folder failed: %v", err)
			return status.Error(codes.Internal, "internal error")
		}
		cur = parent.ParentId
	}
	return status.Error(codes.InvalidArgument, "folder tree is too deep")
}

func (l *MoveFileLogic) checkTargetFolder(ownerId, targetParentId uint64) error {
	if targetParentId == 0 {
		return nil
	}
	parent, err := l.svcCtx.FoldersModel.FindOne(l.ctx, targetParentId)
	if errors.Is(err, model.ErrNotFound) {
		return status.Error(codes.NotFound, "target folder not found")
	}
	if err != nil {
		l.Errorf("find target folder failed: %v", err)
		return status.Error(codes.Internal, "internal error")
	}
	if parent.OwnerId != ownerId || parent.DeletedAt.Valid {
		return status.Error(codes.NotFound, "target folder not found")
	}
	return nil
}
