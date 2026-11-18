# 通用构建文件：用 SERVICE_DIR 指定要构建的服务目录。
# 例：docker build --build-arg SERVICE_DIR=./service/user/api -t cloud-drive/user-api .
#
# 说明：这里刻意不写 `# syntax=docker/dockerfile:1`。
# 那行会让 BuildKit 去 Docker Hub 拉一份前端镜像，在受限网络下会直接失败
# （典型报错：frontend grpc server closed unexpectedly）。
# 本文件只用到内置前端已支持的特性（多阶段构建 + 缓存挂载）。

FROM golang:1.27-alpine AS builder

WORKDIR /src

# 构建阶段可指定 module proxy。默认用国内镜像，proxy.golang.org 在部分网络下不可达。
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}

# 先只拷贝依赖清单，命中构建缓存。
#
# 两个 cache mount 是构建速度的关键：
#   - /go/pkg/mod     下载过的模块，跨构建复用（go.mod 不变时不用重新下载）
#   - /root/.cache/go-build  Go 编译缓存，未改动包的 .a 文件直接复用
# 没有它们时每次构建都在全新容器里从零编译全部依赖，7 个服务并行要 20 分钟以上；
# 有了之后只有首次（缓存为空）是冷的，后续重建基本是秒级。
# cache mount 的内容不写进镜像层，所以下面 go build 的那一步也要挂同样的目录。
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG SERVICE_DIR=./service/user/api
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app ${SERVICE_DIR}

FROM alpine:3.20

# tzdata：MySQL DSN 里的 loc=Asia/Shanghai 需要时区数据，否则连接会报 unknown time zone。
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 app \
    && adduser -D -u 10001 -G app app

WORKDIR /app
COPY --from=builder /out/app /app/app

USER 10001:10001

ENTRYPOINT ["/app/app"]
