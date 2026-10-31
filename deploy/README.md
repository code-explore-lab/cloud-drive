# Docker 部署说明

全容器化启动：MySQL、Redis、etcd、MinIO 和 6 个 Go 服务都跑在 compose 里。

## 前置要求

- Docker 20.10+（Docker Desktop 或 Linux 上的 dockerd）
- 首次构建需要能访问 Go module proxy 和镜像仓库
- 建议给 Docker 至少 4 CPU / 6GB 内存：6 个服务同时编译时资源不足会让 BuildKit 崩溃
  （典型报错：`failed to receive status: rpc error: code = Unavailable ... EOF`）
  构建卡住时可以用 `COMPOSE_PARALLEL_LIMIT=1 docker compose build` 串行构建

## 快速开始

```bash
# 1. 准备环境变量（不改也能跑，compose 里有开发默认值）
cp .env.example .env

# 2. 构建并启动
docker compose up -d --build

# 3. 看状态，等所有服务变成 healthy / running
docker compose ps
```

API 端口：

| 服务 | 宿主机端口 | 用途 |
|---|---|---|
| user-api | 8000 | 注册 / 登录 / 用户信息 / 配额 / 退出 |
| file-api | 8001 | 目录、文件列表、重命名、移动、删除、上传编排、下载、分享、版本 |
| storage-api | 8002 | 分片上传（数据面）、内部对象下载 |
| MySQL | 13306 | 数据库 |
| Redis | 16379 | 缓存 |
| etcd | 12379 | 服务注册与发现 |
| MinIO | 19000 / 19001 | 对象存储 API / 控制台 |

RPC 端口（9000/9001/9002）只在 compose 网络内部使用。
每个服务内置的 go-zero dev server 在容器内 `:6060` 暴露 `/healthz` 与 `/metrics`，
Prometheus 通过容器名抓取，不需要映射到宿主机。

## 验收

```bash
go run ./scripts/e2e
```

脚本会真实走一遍：注册登录、建目录、分片上传、下载校验、Range、版本、分享、越权、删除与配额归还。
失败会以非 0 退出码结束。

## 冒烟测试

```bash
# 健康检查
curl -s http://127.0.0.1:8000/healthz   # ok
curl -s http://127.0.0.1:8001/healthz   # ok
curl -s http://127.0.0.1:8002/healthz   # ok
curl -s http://127.0.0.1:8002/readyz    # 检查 MinIO 与 bucket

# 注册并登录
curl -s -X POST http://127.0.0.1:8000/v1/users/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"Passw0rd!123","email":"alice@example.com"}'

TOKEN=$(curl -s -X POST http://127.0.0.1:8000/v1/users/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"Passw0rd!123"}' | sed -n 's/.*"accessToken":"\([^"]*\)".*/\1/p')

# 建文件夹、列目录
curl -s -X POST http://127.0.0.1:8001/v1/folders \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"parentId":0,"name":"docs"}'

curl -s "http://127.0.0.1:8001/v1/files?parentId=0&page=1&pageSize=20" \
  -H "Authorization: Bearer $TOKEN"
```

## 监控（可选）

```bash
docker compose --profile observability up -d prometheus grafana
```

- Prometheus: http://127.0.0.1:9090
- Grafana: http://127.0.0.1:3000（默认 admin/admin）

## 配置与密钥

- 容器配置在 `deploy/config/*.yaml`，通过 volume 挂载进容器，不打进镜像。
- go-zero 的 `conf.Load` 会对配置文件做 `os.ExpandEnv`，所以 yaml 里的 `${VAR}` 会在启动时被容器环境变量替换。
- **注意**：某个 `${VAR}` 没有注入时会展开成空字符串，yaml 解析会报
  `type mismatch for field ...` 或把整个字段丢掉（`empty etcd hosts`）。
- 密码、JWT secret、盐、内部令牌都从 `.env` 注入；`.env` 不要提交到 git。
- 基础设施地址（`mysql:3306`、`redis:6379`、`etcd:2379`、`minio:9000`）直接写在 deploy 配置里，
  它们属于部署拓扑，不是密钥。

## 注意事项

- **数据库只会初始化一次**：三份 SQL 挂在 `/docker-entrypoint-initdb.d`，只在 `mysql-data` 卷为空时执行。
  改了 schema 要 `docker compose down -v` 重新初始化（会清空数据）。
- **不要混用宿主机的 go run 和容器里的 RPC**：Docker Desktop 下宿主机无法访问容器 IP，
  而 go-zero 通过 etcd 注册的是容器 IP。要么全容器，要么全宿主机
  （全宿主机时需要把 etcd 的 `--advertise-client-urls` 指到宿主机可达地址，见 `docs/troubleshooting.md`）。
- **MinIO 已真实接入**：分片先写 `parts/` 临时对象，完成时服务端合并到 `u/` 下的最终对象；
  上传未完成的对象不会被当作可下载文件。
- 构建慢或无外网时，可在 Dockerfile 构建阶段设置 `GOPROXY`（例如国内镜像 `https://goproxy.cn,direct`）。

## 常用命令

```bash
docker compose logs -f user.rpc       # 看某个服务日志
docker compose restart file-api       # 重启单个服务
docker compose down                   # 停止（保留数据卷）
docker compose down -v                # 停止并删除数据卷
```
