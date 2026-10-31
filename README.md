# CloudDrive

基于 GoZero 的微服务网盘系统：用户注册登录、文件夹管理、分片上传/断点续传、流式下载、文件分享、版本管理，以及配套的配额、权限、幂等与清理机制。

> 目标与验收标准见 [target.md](target.md)。本文档说明**当前已经实现的部分**、如何运行、如何验证，以及还差什么。

---

## 一、现在能做什么

### 已实现

| 能力 | 说明 |
| --- | --- |
| 注册 / 登录 / 刷新令牌 / 退出 | scrypt 密码哈希、JWT access token、refresh token 只存哈希且可撤销 |
| 文件夹管理 | 创建、重命名、移动（含环检测与重名检查）、递归删除 |
| 小文件上传 | 单片上传 → 服务端合并 → 落对象元数据 |
| 大文件分片上传 | 8MiB 分片、断点续传（查询缺失分片）、重复分片幂等覆盖 |
| 上传幂等 | `idempotencyKey` + 数据库唯一约束；重复完成不重复出版本、不重复扣配额 |
| 内容校验 | 客户端提供 SHA-256 时整文件校验，不匹配则拒绝并清理对象 |
| 流式下载 | 支持 `Range` 断点续传，64KiB 缓冲，不把文件读进内存 |
| 文件分享 | 不可预测 token（只存摘要）、bcrypt 密码、有效期、最大访问次数、可撤销 |
| 版本管理 | 每次上传追加版本；列表、回滚（回滚=追加新版本，历史不被改写） |
| 配额 | 预占 / 提交 / 归还三段式，删除文件归还已用额度 |
| 对象生命周期 | 临时分片清理、删除失败重入队、后台 GC 重试、按前缀清理孤儿分片 |
| 统一错误响应 | gRPC code → HTTP 状态码 + 稳定错误码 + requestId |
| 可观测性 | 结构化 JSON 日志、`/metrics`（go-zero 内置 + 业务指标）、Prometheus 告警规则、Grafana 看板 |
| 部署 | Docker Compose 一键启动；Kubernetes 清单；GitHub Actions CI |
| 测试 | 单元测试 + 端到端验收脚本（`go run ./scripts/e2e`） |

### 明确还未实现（见 [docs/roadmap.md](docs/roadmap.md)）

- Web 前端界面（当前只提供 HTTP API）。
- Kafka 异步任务、Redis 缓存优化、OpenTelemetry 链路追踪。
- 分享整个目录、秒传/去重、跨用户协作权限。
- 多副本下的全局限流（当前是进程内令牌桶）。

---

## 二、架构

```text
                     Client / curl / Postman
                                |
                                v
        +-----------------------------------------------+
        |   HTTP API 层（JWT 鉴权、统一错误、指标）        |
        |   user-api :8000   file-api :8001              |
        |   storage-api :8002（数据面，只做字节搬运）      |
        +-----------------------------------------------+
             |                    |                 |
             v                    v                 v
     +---------------+   +-----------------+   +------------------+
     |  user.rpc     |   |   file.rpc      |   |   storage.rpc    |
     |  用户/令牌/配额 |   | 文件/目录/分享/版本 |   | 上传任务/分片/对象 |
     +---------------+   +-----------------+   +------------------+
             |                    |                 |        |
             v                    v                 v        v
        user DB (MySQL)     file DB (MySQL)   storage DB   MinIO
                                               (MySQL)    (对象存储)

     服务间：gRPC + etcd 服务发现（除下载/上传字节流走 HTTP 数据面）
     监控：Prometheus + Grafana（可选 profile）
```

设计取舍和理由写在 [docs/architecture.md](docs/architecture.md) 与 [docs/adr/](docs/adr/)。

---

## 三、快速开始（Docker Compose）

```bash
cp .env.example .env          # 生产环境务必替换里面的密钥
docker compose up -d --build
docker compose ps             # 等所有服务变成 healthy / running
```

| 服务 | 宿主机端口 | 用途 |
| --- | --- | --- |
| user-api | 8000 | 注册 / 登录 / 用户信息 / 配额 / 退出 |
| file-api | 8001 | 目录、文件、上传编排、下载、分享、版本 |
| storage-api | 8002 | 分片上传（数据面）、内部对象下载 |
| MySQL | 13306 | 元数据 |
| Redis | 16379 | go-zero 缓存、限流辅助 |
| etcd | 12379 | 服务注册与发现 |
| MinIO | 19000 / 19001 | 对象存储 API / 控制台 |

RPC 端口（9000/9001/9002）只在 compose 网络内部使用。

### 验收：一条命令

```bash
go run ./scripts/e2e
```

它会真实走一遍注册、登录、建目录、分片上传、下载校验、Range、版本、分享、越权、删除与配额归还，
每一步都会打印结果，失败会以非 0 退出码结束。

### 冒烟（手动 curl）

```bash
# 注册并登录
curl -s -X POST http://127.0.0.1:8000/v1/users/register -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"Passw0rd!123","email":"alice@example.com"}'

TOKEN=$(curl -s -X POST http://127.0.0.1:8000/v1/users/login -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"Passw0rd!123"}' | sed -n 's/.*"accessToken":"\([^"]*\)".*/\1/p')

# 建目录
curl -s -X POST http://127.0.0.1:8001/v1/folders -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"parentId":0,"name":"docs"}'

# 列目录
curl -s "http://127.0.0.1:8001/v1/files?parentId=0&page=1&pageSize=20" -H "Authorization: Bearer $TOKEN"
```

完整接口表见 [docs/api.md](docs/api.md)。

---

## 四、本地开发（不用容器跑 Go 服务）

> 注意：go-zero 通过 etcd 注册的是**容器 IP**。宿主机进程连不上容器 IP，
> 因此本地开发必须把基础设施和 Go 服务放在同一网络里；最省事的做法是只用容器基础设施 + `go run` 服务，
> 并让服务注册 `127.0.0.1`。若混用宿主机与容器，RPC 会解析到不可达地址。

1. 只启动基础设施：

```bash
docker compose up -d mysql redis etcd minio
```

2. 依次启动六个服务（各自的 `etc/*.yaml` 已按 `127.0.0.1:13306/16379/12379/19000` 配好）：

```bash
go run ./service/user/rpc      -f service/user/rpc/etc/user.yaml
go run ./service/file/rpc      -f service/file/rpc/etc/file.yaml
go run ./service/storage/rpc   -f service/storage/rpc/etc/storage.yaml
go run ./service/user/api      -f service/user/api/etc/user-api.yaml
go run ./service/storage/api   -f service/storage/api/etc/storage-api.yaml
go run ./service/file/api      -f service/file/api/etc/file-api.yaml
```

3. 数据库表结构按顺序执行：

```bash
mysql -h127.0.0.1 -P13306 -uroot -p cloud-drive < service/user/model/user.sql
mysql -h127.0.0.1 -P13306 -uroot -p cloud-drive < service/file/model/file.sql
mysql -h127.0.0.1 -P13306 -uroot -p cloud-drive < service/storage/model/storage.sql
```

---

## 五、目录结构

```text
cloud-drive/
├── common/                 # 跨服务共用代码
│   ├── objectstore/        # MinIO 封装（流式写、合并、Range 读、摘要校验）
│   ├── objkey/             # 对象 Key 生成规则（唯一来源）
│   ├── web/                # 请求 ID、统一错误响应
│   ├── jwtx/ cryptx/       # JWT 与密码哈希
│   ├── ratelimit/          # 进程内令牌桶（登录限流）
│   ├── ctxdata/            # 上下文用户信息
│   └── metrics/            # 业务指标
├── service/
│   ├── user/{api,rpc,model}
│   ├── file/{api,rpc,model}
│   └── storage/{api,rpc,model}
├── deploy/
│   ├── config/             # 容器内配置（挂载进 /app/etc/config.yaml）
│   ├── docker/             # 镜像构建说明
│   ├── kubernetes/         # K8s 清单
│   └── observability/      # Prometheus 规则 / Grafana 看板
├── docs/                   # 设计、接口、一致性、安全、测试、性能、排障、ADR
├── scripts/e2e/            # 端到端验收脚本
├── docker-compose.yml
├── Dockerfile              # 单 Dockerfile + SERVICE_DIR 构建参数
└── target.md               # 项目最终目标与验收标准
```

---

## 六、测试与质量

```bash
gofmt -l .            # 应无输出
go vet ./...
go test ./... -race -count=1
go run ./scripts/e2e  # 需要先 docker compose up -d
```

当前状态（2026-10-31，本机 Windows + Docker Desktop，容器基础设施 + 宿主机服务）：

| 检查 | 结果 |
| --- | --- |
| `go build ./...` | ✅ |
| `go vet ./...` | ✅ |
| `gofmt -l .` | ✅ 无输出 |
| `go test ./... -count=1` | ✅ 8 个包全绿 |
| `go run ./scripts/e2e` | ✅ 45 项检查全部通过 |

端到端脚本抓出并修复了两个真实缺陷（分析见 `docs/incidents/`）：

- 注册成功后立刻登录失败——注册的裸 SQL 写库没有失效缓存里的"用户不存在"占位符；
- 注册接口 nil panic——`user.rpc` 的 `ServiceContext` 忘了注入 `Conn`，单元测试用假上下文掩盖了它。

未在当前环境验证的两项：`-race`（CI 里跑）与全容器 compose 端到端（本机 BuildKit 反复崩溃）。
详见 [docs/testing.md](docs/testing.md)。

### 一条故障排查的例子

上传完成后对象存在但下载 404，通常是 `storage_objects.status` 不是 `available`，
或者 `storage-api` 的 `/readyz` 不健康（bucket 不存在）。排查路径见
[docs/troubleshooting.md](docs/troubleshooting.md)。

---

## 七、可观测性

每个服务都内置 go-zero dev server，在容器内 `:6060` 暴露：

- `/healthz`（存活）、`/metrics`（Prometheus）、`/debug/pprof/*`（性能分析）

启动监控栈：

```bash
docker compose --profile observability up -d prometheus grafana
```

- Prometheus: http://127.0.0.1:9090
- Grafana: http://127.0.0.1:3000（默认 admin/admin）

看板 `CloudDrive Overview` 包含 QPS、5xx 比例、P95 延迟、RPC 延迟、上传/下载吞吐、
活跃上传任务数、对象清理积压。告警规则见 `deploy/observability/alerts.yml`。

---

## 八、已知限制

- **单实例限流**：登录/注册限流是进程内令牌桶，多副本下总量会放大。
- **配额是"最终一致"**：上传完成后提交配额失败只会记日志，需要对账任务修复账目。
- **版本占用空间**：历史版本的对象保留，配额按所有版本的对象计费。
- **目录列表分页**：目录全量返回、文件分页返回（避免跨表分页的复杂度）。
- **MinIO 单桶**：所有用户共用 `cloud-drive` bucket，靠 Key 前缀隔离。
- **无 Web 前端**、**无 Kafka/OTel**：见 roadmap。

---

## 九、文档索引

| 文档 | 内容 |
| --- | --- |
| [docs/architecture.md](docs/architecture.md) | 服务边界、调用关系、为什么这样拆 |
| [docs/database.md](docs/database.md) | 表结构、索引、状态机、字段语义 |
| [docs/api.md](docs/api.md) | HTTP 接口、错误码、示例 |
| [docs/upload-design.md](docs/upload-design.md) | 分片上传、断点续传、幂等、合并 |
| [docs/consistency.md](docs/consistency.md) | 跨资源一致性、补偿、对账 |
| [docs/security.md](docs/security.md) | 认证、授权、越权防护、密钥管理 |
| [docs/testing.md](docs/testing.md) | 测试分层与如何跑 |
| [docs/performance.md](docs/performance.md) | 性能测试方法（目前没有编造的数字） |
| [docs/troubleshooting.md](docs/troubleshooting.md) | 常见故障与定位步骤 |
| [docs/roadmap.md](docs/roadmap.md) | Phase 0-6 完成状态 |
| [docs/adr/](docs/adr/) | 架构决策记录（6 篇） |
| [docs/incidents/](docs/incidents/) | 真实故障的分析与修复记录 |
| [deploy/README.md](deploy/README.md) | Compose 部署说明 |
| [deploy/kubernetes/README.md](deploy/kubernetes/README.md) | Kubernetes 部署说明 |
| [deploy/docker/README.md](deploy/docker/README.md) | 镜像构建约定 |
