// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package main

import (
	"flag"
	"fmt"

	"cloud-drive/common/web"
	"cloud-drive/service/file/api/internal/config"
	"cloud-drive/service/file/api/internal/handler"
	"cloud-drive/service/file/api/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

var configFile = flag.String("f", "etc/file-api.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c, conf.UseEnv())

	ctx := svc.NewServiceContext(c)

	// 统一错误响应（gRPC code -> HTTP 状态）与请求 ID。
	web.Setup()

	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()

	server.Use(web.RequestID)
	handler.RegisterHandlers(server, ctx)

	fmt.Printf("Starting server at %s:%d...\n", c.Host, c.Port)
	server.Start()
}
