package logic

import (
	"context"
	"encoding/json"
	"errors"

	"cloud-drive/common/xkafka"
	"cloud-drive/service/storage/model"
	"cloud-drive/service/storage/rpc/internal/svc"
	"cloud-drive/service/storage/rpc/types/storage"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
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

// DeleteObject 把对象标记为待清理，并在同一事务内写入 outbox 事件。
//
// 对象清理不再同步执行：调用方只负责「登记要删什么」，dispatcher 负责把事件投递到 Kafka，
// storage-worker 消费后再删 MinIO 对象。这样外部存储抖动不会阻塞文件删除流程。
//
// 幂等：对象不存在 / 已 deleted / 已在 deleting，都返回 Success=true 且不重复发事件。
func (l *DeleteObjectLogic) DeleteObject(in *storage.DeleteObjectRequest) (*storage.DeleteObjectResponse, error) {
	if in.ObjectKey == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}

	// 缓存命中的快速路径：对象已不存在或已删除，不必开事务。
	obj, err := l.svcCtx.ObjectsModel.FindOneByObjectKey(l.ctx, in.ObjectKey)
	// 查不到
	if errors.Is(err, model.ErrNotFound) {
		return &storage.DeleteObjectResponse{Success: true}, nil
	}
	if err != nil {
		l.Errorf("find storage object failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if obj.Status == "deleted" {
		return &storage.DeleteObjectResponse{Success: true}, nil
	}

	var (
		objectID uint64
		eventID  string
		enqueued bool
	)
	// 开启mysql事务
	err = l.svcCtx.Conn.TransactCtx(l.ctx, func(ctx context.Context, session sqlx.Session) error {
		var state struct {
			Id     uint64 `db:"id"`
			Status string `db:"status"`
		}
		// 行锁保证并发删除同一对象时只发生一次状态迁移、只写一条事件。
		err := session.QueryRowCtx(ctx, &state,
			"select `id`, `status` from `storage_objects` where `object_key` = ? for update",
			in.ObjectKey)
		// 外面是缓存读， 事物内是真实DB状态
		if errors.Is(err, sqlx.ErrNotFound) {
			return model.ErrNotFound
		}
		if err != nil {
			return err
		}
		objectID = state.Id
		// 已经被并发请求或 GC 处理过：不重复下发事件。
		if state.Status == "deleting" || state.Status == "deleted" {
			return nil
		}
		// 状态迁移
		if _, err := session.ExecCtx(ctx,
			"update `storage_objects` set `status` = 'deleting', `last_error` = '' where `id` = ?",
			state.Id); err != nil {
			return err
		}

		body, err := json.Marshal(xkafka.ObjectCleanupRequested{ObjectKey: in.ObjectKey})
		if err != nil {
			return err
		}
		eventID = uuid.NewString()
		enqueued = true
		// outbox核心： 更新对象状态 + 插入outbox在同一个事务session
		return l.svcCtx.OutboxModel.InsertInTx(ctx, session, &model.OutboxEvents{
			EventId:     eventID,
			EventType:   xkafka.EventTypeObjectCleanupRequested,
			AggregateId: in.ObjectKey,
			Payload:     string(body),
		})
	})
	if errors.Is(err, model.ErrNotFound) {
		return &storage.DeleteObjectResponse{Success: true}, nil
	}
	if err != nil {
		l.Errorf("enqueue object cleanup failed: object_key=%s: %v", in.ObjectKey, err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if !enqueued {
		return &storage.DeleteObjectResponse{Success: true}, nil
	}

	// 事务提交后再失效缓存，避免把未提交的状态写回缓存。
	l.svcCtx.ObjectsModel.Invalidate(l.ctx, objectID, in.ObjectKey)
	l.Infof("object cleanup enqueued: object_key=%s event_id=%s", in.ObjectKey, eventID)
	return &storage.DeleteObjectResponse{Success: true}, nil
}
