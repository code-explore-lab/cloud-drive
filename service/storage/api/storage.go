// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"cloud-drive/common/web"
	"cloud-drive/service/storage/api/internal/config"
	"cloud-drive/service/storage/api/internal/handler"
	"cloud-drive/service/storage/api/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

var configFile = flag.String("f", "etc/storage-api.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)

	ctx := svc.NewServiceContext(c)

	// 统一错误响应（gRPC code -> HTTP 状态）与请求 ID。
	web.Setup()

	// bucket 可能在 MinIO 起来之后才可用，后台持续重试直到成功。
	ensureCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ctx.Store.EnsureLoop(ensureCtx, 10*time.Second)

	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()

	server.Use(web.RequestID)
	handler.RegisterHandlers(server, ctx)

	fmt.Printf("Starting server at %s:%d...\n", c.Host, c.Port)
	server.Start()
}
