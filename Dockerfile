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
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG SERVICE_DIR=./service/user/api
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app ${SERVICE_DIR}

FROM alpine:3.20

# tzdata：MySQL DSN 里的 loc=Asia/Shanghai 需要时区数据，否则连接会报 unknown time zone。
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 app \
    && adduser -D -u 10001 -G app app

WORKDIR /app
COPY --from=builder /out/app /app/app

USER 10001:10001

ENTRYPOINT ["/app/app"]
