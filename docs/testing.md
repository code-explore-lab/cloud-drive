# 测试策略

测试分四层，越靠下越快、越靠上越像真实使用。

## 1. 单元测试（快，无外部依赖）

`go test ./...` 不启动任何容器，覆盖纯逻辑与校验规则：

| 包 | 覆盖内容 |
| --- | --- |
| `common/jwtx` | 签发/解析、过期、算法篡改 |
| `common/objkey` | 对象 Key 生成、扩展名清洗、路径穿越防护 |
| `common/ratelimit` | 令牌桶：突发、补充、按 key 隔离 |
| `service/user/rpc/internal/logic` | 注册/登录/刷新/配额（fake model + 固定时钟） |
| `service/file/rpc/internal/logic` | 文件名校验、分享 token 与密码哈希、bcrypt 上限 |
| `service/file/api/internal/logic` | 幂等键格式校验 |
| `service/storage/rpc/internal/logic` | 分片大小计算、分片完整性校验、Key 生成 |
| `service/storage/api/internal/handler` | Range 解析（16 种边界）、Content-Disposition 清洗 |

运行：

```bash
go test ./... -race -count=1
```

`-race` 是必跑的：并发正确性不能靠"看起来没有共享状态"来判断。

## 2. 端到端验收（需要 docker compose）

`go run ./scripts/e2e` 是一个只用标准库的 Go 程序，直接打真实 HTTP 接口：

| # | 场景 | 断言 |
| --- | --- | --- |
| 0 | 健康检查 | 三个 API 的 `/healthz` 200 |
| 1 | 注册/登录 | 重复注册 409、错误密码 401、无 token 401 |
| 2 | 目录 | 创建成功、同目录重名 409 |
| 3 | 上传 | 分片 PUT、进度、**分片长度不符 400** |
| 4 | 幂等 | 同一 `idempotencyKey` 返回同一 `fileId`；重复 `complete` 版本不变、配额不变 |
| 5 | 下载 | 内容 sha256 一致；`Range` 返回 206 且长度正确 |
| 6 | 版本 | 列表含当前版本；回滚到当前版本是幂等的 |
| 7 | 分享 | 错误密码拒绝、正确密码通过、超过次数 403、撤销后失效 |
| 8 | 越权 | 用户 B 读/下载/看版本 → 404；用 A 的 uploadId 写分片 → 403 |
| 9 | 删除 | 配额归还、文件变 404、重复删除 404 |
| 10 | 目录删除 | 递归删除成功 |

失败会打印具体步骤并以非 0 退出，可以直接接进 CI（需要先起 compose）。

## 3. 集成测试（当前状态：手工 + e2e 覆盖）

对象存储、MySQL、etcd 的真实行为由 e2e 覆盖。
如果要给 model 层加自动化集成测试，建议：

- 用 testcontainers-go 起 MySQL + MinIO；
- 每个测试用独立 schema，结束后 drop；
- 重点覆盖 `AppendVersion` 的并发（两个 goroutine 同时追加，版本号必须连续不重复）。

当前仓库没有引入 testcontainers，理由是 e2e 已经覆盖了这些路径，
而引入容器化测试会显著拖慢 CI。这是一个明确的取舍，不是遗漏。

## 4. 性能测试

见 [performance.md](performance.md)。目前**没有**性能基线数据，
文档里也不会写一个编造的 QPS。

---

## 5. 关于"生成代码与手写代码"的边界

- `*_gen.go`、`*.pb.go`、`internal/handler/*handler.go`、`routing` 由 goctl / protoc 生成；
- 业务逻辑在 `internal/logic/*.go` 与 `internal/svc`，这些文件 goctl 不会覆盖；
- `routes.go` 是生成文件，但本仓库手工补充了中间件与手写路由，
  并加了注释说明哪些段落是手工维护的；
- 重新生成时使用的命令记录在 [docs/troubleshooting.md](troubleshooting.md) 的"重新生成代码"一节。

## 6. 提交前检查清单

```bash
gofmt -l .                       # 应无输出
go vet ./...                     # 应无输出
go test ./... -race -count=1     # 应全绿
go build ./...                   # 应成功
docker compose config -q         # compose 文件语法正确
go run ./scripts/e2e             # compose 起来后应全部通过
```

---

## 7. 验证记录

### 2026-10-31：本机（Windows + Docker Desktop）完整跑通

环境：Go 1.27.0 / MySQL 8.0 / Redis 7 / MinIO（容器）/ 六个服务以 `go run` 运行在宿主机。

```text
go build ./...                    ✅
go vet ./...                      ✅
gofmt -l .                        ✅ 无输出
go test ./... -count=1            ✅ 8 个包全部通过
go run ./scripts/e2e              ✅ 全部通过（45 项检查）
```

e2e 覆盖的 10 组场景全部通过，包括：

- 注册 / 重复注册 409 / 登录 / 未鉴权 401 / 错误密码 401；
- 建目录 / 同目录重名 409；
- 分片上传 / 进度查询 / 缺失分片为空 / **分片长度不符 400**；
- 幂等：同一 `idempotencyKey` 返回同一 `fileId`；重复 `complete` 版本不变、配额不变；
- 下载内容 sha256 一致；`Range` 返回 206 且长度为 5；
- 版本列表 / 回滚到当前版本幂等；
- 分享：密码校验、次数限制、撤销后失效、分享下载内容一致；
- 越权：他人文件 GET/下载/版本均 404，他人写分片 403；
- 删除：配额归还、文件 404、重复删除幂等（200 + releasedBytes=0）；
- 目录递归删除。

这次 e2e 直接抓出了两个真实缺陷（已修复并记录）：
[incidents/001](incidents/001-register-login-cache-poisoning.md)、
[incidents/002](incidents/002-missing-conn-nil-panic.md)。
这也说明"能编译 + 单元测试绿"离"能跑"还有距离。

### 未在本环境完成

- `go test ./... -race`：本机在 Docker Desktop 运行容器时长时间无进展，已停止；
  CI 中配置了 `-race`，需要在正常 CI 环境上确认。
- 全容器 `docker compose up` 的端到端：镜像构建在本机 Docker Desktop 上反复因
  BuildKit 崩溃中断（`failed to receive status: rpc error: ... EOF`），
  因此本次验证走的是"容器基础设施 + 宿主机服务"的组合。
  镜像与 compose 配置本身已通过 `docker compose config -q` 校验。