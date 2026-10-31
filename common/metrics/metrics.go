// Package metrics 定义 CloudDrive 的业务指标。
//
// 这些指标和 go-zero 自带的 http_server_* / rpc_server_* 指标一起，
// 由每个服务内置的 dev server（默认 :6060/metrics）暴露给 Prometheus。
// 指标名刻意保持稳定，改动等同于修改监控契约，需要同步更新
// deploy/observability/grafana 与 alerts.yml。
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "clouddrive"

var (
	// UploadBytesTotal 记录成功落盘的上传字节数。
	UploadBytesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "upload",
		Name:      "bytes_total",
		Help:      "Total bytes of successfully completed uploads.",
	})

	// UploadCompletedTotal 按结果统计上传完成次数。
	UploadCompletedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "upload",
		Name:      "completed_total",
		Help:      "Total number of upload completion attempts.",
	}, []string{"result"})

	// DownloadBytesTotal 记录通过 file-api 代理下载的字节数。
	DownloadBytesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "download",
		Name:      "bytes_total",
		Help:      "Total bytes streamed to clients.",
	})

	// QuotaRejectedTotal 统计因配额不足被拒绝的上传请求。
	QuotaRejectedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "quota",
		Name:      "rejected_total",
		Help:      "Total upload requests rejected because of insufficient quota.",
	})

	// ActiveUploadTasks 是当前未完成的上传任务数，由存储服务定期刷新。
	ActiveUploadTasks = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "upload",
		Name:      "active_tasks",
		Help:      "Number of upload tasks that are not finished yet.",
	})

	// ObjectGCFailuresTotal 统计对象清理失败次数，用于"清理任务长期失败"告警。
	ObjectGCFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "storage",
		Name:      "object_gc_failures_total",
		Help:      "Total number of failed object deletions.",
	})

	// ObjectGCBacklog 是待清理对象数量（status=deleting）。
	ObjectGCBacklog = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "storage",
		Name:      "object_gc_backlog",
		Help:      "Number of objects waiting to be deleted.",
	})
)
