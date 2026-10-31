package logic

import (
	"context"
	"database/sql"
	"errors"

	"cloud-drive/service/file/model"
	"cloud-drive/service/file/rpc/internal/svc"
	"cloud-drive/service/file/rpc/types/file"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CreateFileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateFileLogic {
	return &CreateFileLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CreateFile 创建一个"等待上传"的文件元数据。
//
// 幂等：request_id（上传任务 ID）在 (owner_id, request_id) 上有唯一约束，
// 重复的 InitUpload 请求会拿到同一个 file_id，而不是产生两条记录。
func (l *CreateFileLogic) CreateFile(in *file.CreateFileRequest) (*file.CreateFileResponse, error) {
	if in.OwnerId <= 0 || in.Size < 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}
	name, err := validateName(in.Name)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid file name")
	}
	if in.ParentId < 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid parent id")
	}

	ownerId := uint64(in.OwnerId)
	fileStatus := in.Status
	if fileStatus == "" {
		fileStatus = "uploading"
	}
	if !validFileStatus(fileStatus) {
		return nil, status.Error(codes.InvalidArgument, "invalid file status")
	}

	requestId := sql.NullString{String: in.RequestId, Valid: in.RequestId != ""}
	if requestId.Valid {
		existing, err := l.svcCtx.FilesModel.FindOneByOwnerIdRequestId(l.ctx, ownerId, requestId)
		switch {
		case err == nil:
			return &file.CreateFileResponse{FileId: int64(existing.Id), Success: true, Existed: true}, nil
		case !errors.Is(err, model.ErrNotFound):
			l.Errorf("find file by request id failed: %v", err)
			return nil, status.Error(codes.Internal, "internal error")
		}
	}

	if err := l.checkParent(ownerId, uint64(in.ParentId)); err != nil {
		return nil, err
	}

	res, err := l.svcCtx.FilesModel.Insert(l.ctx, &model.Files{
		OwnerId:     ownerId,
		ParentId:    uint64(in.ParentId),
		Name:        name,
		Size:        uint64(in.Size),
		ContentType: normalizeContentType(in.ContentType),
		ContentHash: in.ContentHash,
		ObjectKey:   in.ObjectKey,
		Version:     0,
		Status:      fileStatus,
		RequestId:   requestId,
	})
	if err != nil {
		// 并发重复请求撞唯一键：回查一次，把已有记录返回给调用方。
		if requestId.Valid {
			if existing, findErr := l.svcCtx.FilesModel.FindOneByOwnerIdRequestId(l.ctx, ownerId, requestId); findErr == nil {
				return &file.CreateFileResponse{FileId: int64(existing.Id), Success: true, Existed: true}, nil
			}
		}
		l.Errorf("insert file failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	id, err := res.LastInsertId()
	if err != nil {
		l.Errorf("get last insert id failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &file.CreateFileResponse{FileId: id, Success: true}, nil
}

// checkParent 校验父目录存在且属于同一个用户。parent_id = 0 表示根目录。
func (l *CreateFileLogic) checkParent(ownerId, parentId uint64) error {
	if parentId == 0 {
		return nil
	}
	parent, err := l.svcCtx.FoldersModel.FindOne(l.ctx, parentId)
	if errors.Is(err, model.ErrNotFound) {
		return status.Error(codes.NotFound, "parent folder not found")
	}
	if err != nil {
		l.Errorf("find parent folder failed: %v", err)
		return status.Error(codes.Internal, "internal error")
	}
	if parent.OwnerId != ownerId || parent.DeletedAt.Valid {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	return nil
}

func normalizeContentType(contentType string) string {
	if contentType == "" {
		return "application/octet-stream"
	}
	if len(contentType) > 255 {
		return contentType[:255]
	}
	return contentType
}
