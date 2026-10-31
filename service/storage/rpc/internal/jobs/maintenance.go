// Package jobs 存放存储服务的后台任务。
//
// 当前只有一个归并任务：它做的事情都很朴素，但都是"没人做就一定会出事"的部分：
//   - 把超时的上传任务标成 failed，避免永远占着 uploaded 状态；
//   - 把卡在 completing 的任务放回 uploading，允许客户端重试；
//   - 重试删除失败的对象（storage_objects.status = deleting）；
//   - 清理过期的临时分片对象（parts/ 前缀）。
package jobs

import (
	"context"
	"time"

	"cloud-drive/common/metrics"
	"cloud-drive/service/storage/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	defaultInterval = 10 * time.Minute
	defaultTTL      = 24 * time.Hour
	defaultBatch    = int64(200)
	// orphanPartTTL 比任务 TTL 更长，避免把仍在重试的上传的临时分片删掉。
	orphanPartTTL = 48 * time.Hour
	// stuckCompletingAfter 之后认为 completing 是崩溃残留，可以回收。
	stuckCompletingAfter = 15 * time.Minute
)

// Maintenance 是存储侧的周期性清理任务，并发安全（单实例内串行执行）。
type Maintenance struct {
	svcCtx   *svc.ServiceContext
	interval time.Duration
	ttl      time.Duration
	batch    int64
}

func NewMaintenance(svcCtx *svc.ServiceContext) *Maintenance {
	m := &Maintenance{
		svcCtx:   svcCtx,
		interval: defaultInterval,
		ttl:      defaultTTL,
		batch:    defaultBatch,
	}
	if sec := svcCtx.Config.Maintenance.Interval; sec > 0 {
		m.interval = time.Duration(sec) * time.Second
	}
	if hours := svcCtx.Config.Maintenance.UploadTTL; hours > 0 {
		m.ttl = time.Duration(hours) * time.Hour
	}
	if n := svcCtx.Config.Maintenance.BatchSize; n > 0 {
		m.batch = n
	}
	return m
}

// Start 在后台启动任务循环，直到 ctx 结束。
func (m *Maintenance) Start(ctx context.Context) {
	go m.ensureBucket(ctx)

	ticker := time.NewTicker(m.interval)
	go func() {
		defer ticker.Stop()
		// 启动后先跑一轮，容器重启时能尽快收敛状态。
		m.RunOnce(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.RunOnce(ctx)
			}
		}
	}()
	logx.Infof("storage maintenance started: interval=%s ttl=%s", m.interval, m.ttl)
}

// ensureBucket 在 MinIO 还没就绪时持续重试，避免服务启动顺序导致的失败。
func (m *Maintenance) ensureBucket(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if err := m.svcCtx.Store.Ensure(ctx); err != nil {
			logx.Errorf("ensure bucket %s failed: %v", m.svcCtx.Store.Bucket(), err)
		} else {
			logx.Infof("bucket %s is ready", m.svcCtx.Store.Bucket())
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce 执行一轮清理。单个步骤失败不影响其他步骤。
func (m *Maintenance) RunOnce(ctx context.Context) {
	if err := m.expireUploads(ctx); err != nil {
		logx.Errorf("expire uploads failed: %v", err)
	}
	if err := m.recoverStuckCompleting(ctx); err != nil {
		logx.Errorf("recover stuck completing uploads failed: %v", err)
	}
	if err := m.retryObjectDeletes(ctx); err != nil {
		logx.Errorf("retry object deletes failed: %v", err)
	}
	if err := m.cleanupOrphanParts(ctx); err != nil {
		logx.Errorf("cleanup orphan parts failed: %v", err)
	}
	m.refreshGauges(ctx)
}

// refreshGauges 刷新"活跃上传任务"与"待清理对象"两个指标，
// 让监控能直接回答"当前有多少未完成上传"和"清理任务积压多少"。
func (m *Maintenance) refreshGauges(ctx context.Context) {
	if n, err := m.svcCtx.TasksModel.CountActive(ctx); err == nil {
		metrics.ActiveUploadTasks.Set(float64(n))
	} else {
		logx.Errorf("count active uploads failed: %v", err)
	}
	if n, err := m.svcCtx.ObjectsModel.CountByStatus(ctx, "deleting"); err == nil {
		metrics.ObjectGCBacklog.Set(float64(n))
	} else {
		logx.Errorf("count deleting objects failed: %v", err)
	}
}

// expireUploads 把超时的上传任务标记为失败，并清理其临时分片。
func (m *Maintenance) expireUploads(ctx context.Context) error {
	tasks, err := m.svcCtx.TasksModel.FindExpiredActive(ctx, time.Now(), m.batch)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		parts, err := m.svcCtx.PartsModel.FindByUploadId(ctx, task.UploadId)
		if err != nil {
			logx.Errorf("list parts of %s failed: %v", task.UploadId, err)
		}
		for _, p := range parts {
			if p.ObjectPartId != "" {
				if err := m.svcCtx.Store.Remove(ctx, p.ObjectPartId); err != nil {
					logx.Errorf("remove expired part %s failed: %v", p.ObjectPartId, err)
				}
			}
		}
		_ = m.svcCtx.PartsModel.DeleteByUploadId(ctx, task.UploadId)

		task.Status = "failed"
		task.LastError = "upload task expired"
		if err := m.svcCtx.TasksModel.Update(ctx, task); err != nil {
			logx.Errorf("mark expired upload %s failed: %v", task.UploadId, err)
			continue
		}
		logx.Infof("expired upload task marked failed: upload_id=%s", task.UploadId)
	}
	return nil
}

// recoverStuckCompleting 把长时间停留在 completing 的任务放回 uploading。
func (m *Maintenance) recoverStuckCompleting(ctx context.Context) error {
	tasks, err := m.svcCtx.TasksModel.FindStuckCompleting(ctx, time.Now().Add(-stuckCompletingAfter), m.batch)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		task.Status = "uploading"
		task.LastError = "recovered from stuck completing state"
		if err := m.svcCtx.TasksModel.Update(ctx, task); err != nil {
			logx.Errorf("recover stuck upload %s failed: %v", task.UploadId, err)
		}
	}
	return nil
}

// retryObjectDeletes 重试删除失败的对象。
func (m *Maintenance) retryObjectDeletes(ctx context.Context) error {
	objects, err := m.svcCtx.ObjectsModel.FindByStatus(ctx, "deleting", m.batch)
	if err != nil {
		return err
	}
	for _, obj := range objects {
		if err := m.svcCtx.Store.Remove(ctx, obj.ObjectKey); err != nil {
			obj.GcAttempts++
			obj.LastError = truncate(err.Error(), 500)
			metrics.ObjectGCFailuresTotal.Inc()
			if updateErr := m.svcCtx.ObjectsModel.Update(ctx, obj); updateErr != nil {
				logx.Errorf("update object gc state failed: %v", updateErr)
			}
			continue
		}
		obj.Status = "deleted"
		obj.DeletedAt.Valid = true
		obj.DeletedAt.Time = time.Now()
		obj.LastError = ""
		if err := m.svcCtx.ObjectsModel.Update(ctx, obj); err != nil {
			logx.Errorf("mark object deleted failed: %v", err)
		}
	}
	return nil
}

// cleanupOrphanParts 删除 parts/ 下的过期临时对象。
// 正常情况下这些对象在合并或取消时已经删掉，这里兜底"删除失败留下的垃圾"。
func (m *Maintenance) cleanupOrphanParts(ctx context.Context) error {
	objects, err := m.svcCtx.Store.List(ctx, "parts/")
	if err != nil {
		return err
	}
	deadline := time.Now().Add(-orphanPartTTL)
	var removed int
	for _, obj := range objects {
		if obj.LastModified.After(deadline) {
			continue
		}
		if err := m.svcCtx.Store.Remove(ctx, obj.Key); err != nil {
			logx.Errorf("remove orphan part %s failed: %v", obj.Key, err)
			continue
		}
		removed++
	}
	if removed > 0 {
		logx.Infof("removed %d orphan part objects", removed)
	}
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
