// Command storage-worker 消费 storage.events，异步清理 MinIO 对象。
//
// 它是独立进程而不是 storage.rpc 里的 goroutine：消费能力可以单独扩缩容，
// 清理慢也不会拖住在线请求。
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"cloud-drive/service/storage/worker/internal/config"
	"cloud-drive/service/storage/worker/internal/consumer"
	"cloud-drive/service/storage/worker/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
)

var configFile = flag.String("f", "etc/storage-worker.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c, conf.UseEnv())
	if c.Log.ServiceName == "" {
		c.Log.ServiceName = c.Name
	}
	logx.Must(logx.SetUp(c.Log))

	// 配置里没写 group 时用固定默认值，多实例才会落在同一个 consumer group。
	if c.Kafka.GroupID == "" {
		c.Kafka.GroupID = "storage-worker"
	}

	svcCtx := svc.NewServiceContext(c)
	defer func() {
		if err := svcCtx.DLQ.Close(); err != nil {
			logx.Errorf("close kafka producer failed: %v", err)
		}
	}()

	// SIGTERM 触发 context 取消：consumer 先 Close 再退出，未 commit 的消息下次重投。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	consumer.NewWorker(c, svcCtx).Start(ctx)
	logx.Info("storage worker stopped")
}
