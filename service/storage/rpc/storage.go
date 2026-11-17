package main

import (
	"context"
	"flag"
	"fmt"

	"cloud-drive/service/storage/rpc/internal/config"
	"cloud-drive/service/storage/rpc/internal/jobs"
	"cloud-drive/service/storage/rpc/internal/server"
	"cloud-drive/service/storage/rpc/internal/svc"
	"cloud-drive/service/storage/rpc/types/storage"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/storage.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c, conf.UseEnv())
	svcCtx := svc.NewServiceContext(c)
	// 先注册 producer 的关闭，保证 cancel 先执行：dispatcher 停下来之后才关 writer。
	defer func() {
		if err := svcCtx.Producer.Close(); err != nil {
			logx.Errorf("close kafka producer failed: %v", err)
		}
	}()

	// bucket 初始化与清理任务放在后台跑：MinIO 比本服务晚启动时也能自愈。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if c.Maintenance.Enabled {
		jobs.NewMaintenance(svcCtx).Start(ctx)
	} else {
		logx.Info("storage maintenance is disabled by config")
	}
	if c.Outbox.Enabled {
		jobs.NewDispatcher(svcCtx).Start(ctx)
	} else {
		logx.Info("storage outbox dispatcher is disabled by config")
	}

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		storage.RegisterStorageServer(grpcServer, server.NewStorageServer(svcCtx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	defer s.Stop()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
