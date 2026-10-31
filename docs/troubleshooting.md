# 故障排查

按"现象 → 可能原因 → 验证方法 → 处理"组织。所有命令默认在仓库根目录执行。

真实发生过的两个故障（含完整分析）记录在 [incidents/](incidents/)：

- [001 注册成功但登录失败（缓存占位符污染）](incidents/001-register-login-cache-poisoning.md)
- [002 注册接口 nil panic（ServiceContext 未注入 Conn）](incidents/002-missing-conn-nil-panic.md)

## 0. 通用排查顺序

1. `docker compose ps`：是不是有容器在 `Restarting` / `unhealthy`；
2. `docker compose logs --tail 100 <service>`：看错误；
3. 拿到响应的 `X-Request-Id`，在日志里 grep 同一个 ID，串起整条链路；
4. 看指标：`/metrics` 或 Prometheus，确认是错误率、延迟还是依赖故障；
5. 只有前三步都指向同一处时再改代码。

---

## 1. 服务起不来

### 1.1 `empty etcd hosts` / `failed to connect to etcd`

原因：服务配置没有读到 `Etcd.Hosts`，或 etcd 没起来。

```bash
docker compose ps etcd
docker compose exec etcd etcdctl endpoint health
docker compose exec storage-api sh -c 'cat /app/etc/config.yaml | head -30'
```

注意 `conf.MustLoad` 会对配置做 `${VAR}` 展开：
**如果环境变量没注入，展开结果为空字符串，yaml 解析会丢掉整个字段或报类型错误。**
典型表现：

```text
error: config file ..., type mismatch for field "Minio.UseSSL"
```

说明 `MINIO_USE_SSL` 没有传进容器。检查 `docker compose config` 里该服务的 `environment`。

### 1.1b 宿主机直连容器时 RPC 解析到 `etcd:2379`

Docker Desktop 下容器内 etcd 的 `--advertise-client-urls` 是 `etcd:2379`，
宿主机进程拿到这个地址后无法解析。要么全部跑容器，要么像本地开发那样起一个
**对外广播 127.0.0.1 的 etcd**：

```bash
docker run -d --name cd-etcd-host -p 12379:2379 quay.io/coreos/etcd:v3.5.21 \
  /usr/local/bin/etcd --name=etcd0 --data-dir=/etcd-data \
  --listen-client-urls=http://0.0.0.0:2379 --advertise-client-urls=http://127.0.0.1:12379 \
  --listen-peer-urls=http://0.0.0.0:2380 --initial-advertise-peer-urls=http://127.0.0.1:2380 \
  --initial-cluster=etcd0=http://127.0.0.1:2380 --initial-cluster-state=new
```

### 1.2 `database not reachable` / MySQL 连接失败

```bash
docker compose logs mysql | grep -i error
docker compose exec mysql mysql -uroot -p"$MYSQL_ROOT_PASSWORD" -e 'show databases;'
```

如果 `cloud-drive` 库是空的：初始化脚本只在数据卷为空时执行一次。
需要重建：`docker compose down -v && docker compose up -d`（**会清空数据**）。

### 1.3 端口占用

`bind: address already in use`：改 `docker-compose.yml` 的宿主机端口映射，或停掉占用进程。

### 1.4 宿主机模式下 RPC 很慢（每次查询 1-2 秒）

Docker Desktop 的端口转发在部分机器上首次建立连接就要 1-2 秒。
表现是 `context deadline exceeded` 与 `[SQL] slowcall`，而容器内跑却没有这个问题。

处理方式（三选一）：

1. 全部跑容器（推荐，也是部署形态）；
2. 本地开发时把 `Timeout` 调大（本仓库的 `service/*/etc/*.yaml` 已设成 10-60 秒）；
3. DSN 加 `interpolateParams=true` 减少一次 Prepare 往返（本仓库已加）。

---

## 2. 上传相关

### 2.1 `429 resource_exhausted: quota exceeded`

不是故障，是配额不足。查看：

```bash
curl -s http://127.0.0.1:8000/v1/users/me/quota -H "Authorization: Bearer $TOKEN"
```

如果 `usedBytes` 明显偏高但用户没有那么多文件，说明有账目漂移：

```bash
go run ./tools/reconcile -f service/storage/rpc/etc/storage.yaml
```

### 2.2 分片上传返回 400

| 返回信息 | 原因 |
| --- | --- |
| `request body is larger than the declared part size` | 多发字节 |
| `failed to store part, body size does not match the declared size` | 少发字节 |
| `part N size mismatch: got X want Y` | 与服务端计算的期望长度不一致 |
| `part number N exceeds total chunks M` | 分片号越界 |

分片长度必须严格等于：

```text
part <  totalChunks → chunkSize
part == totalChunks → size - (totalChunks-1)*chunkSize
```

### 2.3 `complete` 返回 409 `cannot complete upload: upload incomplete`

有分片没传成功。查进度：

```bash
curl -s http://127.0.0.1:8001/v1/uploads/$UPLOAD_ID -H "Authorization: Bearer $TOKEN"
```

`missingParts` 就是还缺的片，补传即可。

### 2.4 `complete` 返回 400 `content hash mismatch`

客户端算的 SHA-256 与服务端读到的内容不一致。常见原因：

- 客户端上传时分片顺序错乱；
- 客户端把压缩/编码后的内容算成了原文的 hash；
- 传输过程中内容被中间代理改写（极少见）。

服务端在检测到不一致时会删除已合并对象并把任务标记 `failed`。

### 2.5 卡在 `completing`

正常情况下 15 分钟内会被后台任务放回 `uploading`。手动干预：

```bash
docker compose exec mysql mysql -uroot -p -e \
 "update cloud-drive.upload_tasks set status='uploading' where upload_id='<id>';"
```

---

## 3. 注册 / 登录

### 3.1 注册成功但登录提示"用户名或密码错误"

**先看是不是缓存问题**：

```bash
docker compose exec redis redis-cli -n 0 KEYS 'cache:users:*'
docker compose exec redis redis-cli -n 0 FLUSHALL   # 仅开发环境
```

如果 flush 之后能登录，说明缓存里残留了"用户不存在"的占位符。
根因是**绕过 model 的写操作没有失效缓存**——完整分析见
[incidents/001](incidents/001-register-login-cache-poisoning.md)。

规则：任何直接用 `session.ExecCtx` / `ExecNoCacheCtx` 写库的地方，
都必须显式调用对应的 `Invalidate` / `DelCacheCtx`。

### 3.2 注册接口 500，日志里是 nil pointer

看 panic 的调用栈指向哪个 `l.svcCtx.Xxx`：

```bash
docker compose logs --tail 200 user.rpc | grep -A5 'nil pointer'
```

`ServiceContext` 里某个依赖没有在 `NewServiceContext` 中赋值。
完整案例见 [incidents/002](incidents/002-missing-conn-nil-panic.md)。

### 3.3 登录/注册 429

触发限流（默认每 IP 5 rps，突发 10）。生产环境调 `deploy/config/user-api.yaml` 的
`Limit.RPS` / `Limit.Burst`，或换用基于 Redis 的全局限流。

---

## 4. 下载相关

### 4.1 下载 404，但文件列表里有

可能原因：

1. 文件状态不是 `available`（上传没完成）；
2. `storage_objects.status != 'available'`；
3. 对象在 MinIO 里真的没了。

```bash
docker compose exec mysql mysql -uroot -p -e \
 "select id,status,object_key from cloud-drive.files where id=<fileId>;"
docker compose exec mysql mysql -uroot -p -e \
 "select object_key,status,size,gc_attempts,last_error from cloud-drive.storage_objects where object_key='<key>';"
```

确认对象是否存在：MinIO 控制台 http://127.0.0.1:19001 或

```bash
go run ./tools/reconcile -f service/storage/rpc/etc/storage.yaml
```

### 4.2 下载中断 / 卡住

- 检查 `storage-api` 是否 `Restarting`；
- 检查 `/readyz`：
  ```bash
  curl -s http://127.0.0.1:8002/readyz
  ```
  返回 503 通常是 bucket 不存在或 MinIO 不可用；
- 大文件下载时 file-api 的内存应该保持平稳（流式转发）。若内存上涨，
  说明有人把 `io.CopyBuffer` 改成了 `io.ReadAll`——不要这么改。

### 4.3 Range 请求返回 416

`Range` 越界或语法错误（多段 Range 不支持）。响应里带 `Content-Range: bytes */<size>`。

---

## 5. 分享相关

| 现象 | 原因 |
| --- | --- |
| `granted=false, reason=share was revoked` | 已被撤销 |
| `share has expired` | 过了 `expires_at` |
| `share access limit reached` | 达到 `maxAccessCount` |
| `invalid share password` | 密码错误（bcrypt 比对失败） |
| `file is not available` | 文件被删或未完成上传 |

分享下载 403 时先调 `POST /v1/shares/{token}/access` 看具体 reason。

---

## 6. 数据一致性

### 6.1 对象删不掉

```sql
select object_key, gc_attempts, last_error from storage_objects where status='deleting';
```

后台任务每 10 分钟重试一次。持续失败时检查：

- MinIO 是否可用、bucket 是否存在；
- MinIO 用户是否有 delete 权限；
- 指标 `clouddrive_storage_object_gc_failures_total` 与
  `clouddrive_storage_object_gc_backlog` 是否在上涨（Prometheus 告警会报）。

### 6.2 临时分片堆积

用 MinIO 控制台看 `parts/` 前缀。48 小时前的孤儿分片会被自动清理。
要立即清理：删除对应 `upload_tasks` 行后重启 storage.rpc（不建议手工操作生产数据）。

---

## 7. 性能问题

| 现象 | 先看什么 |
| --- | --- |
| 接口整体变慢 | `http_server_requests_duration_ms` 的 P95 按 path 拆分 |
| 只有 RPC 慢 | `rpc_server_requests_duration_ms` 按 method 拆分 |
| MySQL 慢 | `show processlist`、慢查询日志；列表查询是最重的 |
| 上传慢 | 单分片耗时 vs MinIO 写入耗时；先直连 MinIO 测一次 |
| 下载慢 | 对比直连 storage-api 与经 file-api 的吞吐 |

---

## 8. 重新生成代码

```bash
# RPC：在 service/<svc>/rpc 目录下
goctl rpc protoc <svc>.proto --go_out=./types --go-grpc_out=./types \
  --zrpc_out=. --module=cloud-drive/service/<svc>/rpc --client=true

# API：在 service/<svc>/api 目录下
goctl api go --api <svc>.api --dir .

# model：从 DDL 生成
goctl model mysql ddl -src service/<svc>/model/<svc>.sql -dir <临时目录> -c
# 只把 *_gen.go 拷回 service/<svc>/model/，保留手写的 *model.go
```

注意：

- goctl 不会覆盖已存在的 logic 文件，但会覆盖 `handler/`、`types/`、`routes.go`；
- `routes.go` 里有手工维护的中间件段落，重新生成后需要补回；
- 生成模型的 `package` 名取决于输出目录名，拷回前要改成 `package model`；
- **新增/修改任何绕过 model 的写库逻辑时，务必显式失效相关缓存**（见 3.1）。

---

## 9. 一键重置（开发环境）

```bash
docker compose down -v
docker compose up -d --build
go run ./scripts/e2e
```

这会清空 MySQL、Redis、etcd、MinIO 的全部数据。**只在开发环境这么做。**
