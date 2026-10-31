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

// maxFolderDepth 限制递归深度，防止异常数据导致遍历失控。
const maxFolderDepth = 64

type DeleteFolderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteFolderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteFolderLogic {
	return &DeleteFolderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// DeleteFolder 递归删除目录：先广度优先收集所有子目录和文件，再批量软删除。
//
// 删除规则（写进 docs/api.md）：目录下的所有内容都会被删除，不提供"非空目录不可删"的选项；
// 返回的对象清单由 file-api 负责清理对象存储与归还配额。
func (l *DeleteFolderLogic) DeleteFolder(in *file.DeleteFolderRequest) (*file.DeleteFolderResponse, error) {
	if in.FolderId <= 0 || in.OwnerId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}

	root, err := l.svcCtx.FoldersModel.FindOne(l.ctx, uint64(in.FolderId))
	if errors.Is(err, model.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "folder not found")
	}
	if err != nil {
		l.Errorf("find folder failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if root.OwnerId != uint64(in.OwnerId) {
		return nil, status.Error(codes.NotFound, "folder not found")
	}
	if root.DeletedAt.Valid {
		return &file.DeleteFolderResponse{Success: true, Changed: false}, nil
	}

	folders := []*model.Folders{root}
	queue := []uint64{root.Id}
	visited := map[uint64]struct{}{root.Id: {}}
	var files []*model.Files

	for depth := 0; len(queue) > 0; depth++ {
		if depth > maxFolderDepth {
			l.Errorf("folder tree too deep, stop at depth %d", depth)
			break
		}
		next := make([]uint64, 0, len(queue))
		for _, parentId := range queue {
			children, err := l.svcCtx.FoldersModel.FindByOwnerParent(l.ctx, root.OwnerId, parentId)
			if err != nil {
				l.Errorf("list child folders failed: %v", err)
				return nil, status.Error(codes.Internal, "internal error")
			}
			for _, child := range children {
				if _, ok := visited[child.Id]; ok {
					continue
				}
				visited[child.Id] = struct{}{}
				folders = append(folders, child)
				next = append(next, child.Id)
			}

			childFiles, err := l.svcCtx.FilesModel.FindAllByOwnerParent(l.ctx, root.OwnerId, parentId)
			if err != nil {
				l.Errorf("list files in folder failed: %v", err)
				return nil, status.Error(codes.Internal, "internal error")
			}
			files = append(files, childFiles...)
		}
		queue = next
	}

	objects := make([]*file.ObjectRef, 0)
	seen := make(map[string]struct{})
	var released int64
	for _, f := range files {
		versions, err := l.svcCtx.VersionsModel.FindByFileId(l.ctx, f.Id)
		if err != nil {
			l.Errorf("list versions failed: %v", err)
			return nil, status.Error(codes.Internal, "internal error")
		}
		for _, obj := range collectObjects(f, versions) {
			if _, ok := seen[obj.ObjectKey]; ok {
				continue
			}
			seen[obj.ObjectKey] = struct{}{}
			objects = append(objects, obj)
			released += obj.Size
		}
		if err := l.svcCtx.SharesModel.RevokeByFile(l.ctx, f.Id, f.OwnerId); err != nil {
			l.Errorf("revoke shares failed: %v", err)
			return nil, status.Error(codes.Internal, "internal error")
		}
	}

	now := time.Now()
	if err := l.svcCtx.FilesModel.MarkDeleted(l.ctx, files, now); err != nil {
		l.Errorf("mark files deleted failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if err := l.svcCtx.FoldersModel.MarkDeleted(l.ctx, folders, now); err != nil {
		l.Errorf("mark folders deleted failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	l.Infof("folder deleted: folder_id=%d owner_id=%d folders=%d files=%d objects=%d",
		root.Id, root.OwnerId, len(folders), len(files), len(objects))

	return &file.DeleteFolderResponse{
		Success:       true,
		Changed:       true,
		ReleasedBytes: released,
		Objects:       objects,
	}, nil
}
