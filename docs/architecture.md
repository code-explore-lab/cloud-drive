# 架构说明

本文解释 CloudDrive 的服务边界、调用关系，以及每个边界为什么这么划。

## 1. 逻辑架构

```text
Client
  │  HTTP + JWT
  ▼
┌─────────────────────────────────────────────────────────────┐
│ API 层（无状态，可水平扩展）                                  │
│                                                             │
│  user-api      文件/账号：注册登录、用户信息、配额查询、退出     │
│  file-api      业务编排：目录、文件、上传流程、下载、分享、版本   │
│  storage-api   数据面：分片字节流、对象字节流（Range）           │
└─────────────────────────────────────────────────────────────┘
       │ gRPC                │ gRPC                 │ gRPC
       ▼                     ▼                      ▼
┌──────────────┐    ┌─────────────────┐    ┌──────────────────┐
│  user.rpc    │    │   file.rpc      │    │   storage.rpc    │
│  用户/令牌/配额│    │ 文件/目录/分享/版本│    │ 上传任务/分片/对象 │
└──────┬───────┘    └────────┬────────┘    └────────┬─────────┘
       │                     │                      │
       ▼                     ▼                      ▼
   users /           folders / files /        upload_tasks /
   user_quotas /     file_versions /          upload_parts /
   refresh_tokens    shares                   storage_objects
                                                   │  ▲
                                                   ▼  │ 状态推进
                                              MinIO（对象内容）
                                                   ▲
                                                   │ Remove
   storage.rpc ──outbox→dispatcher──► Redpanda（storage.events）──► storage-worker
```

对象清理是异步的：`storage.rpc` 在事务里写 outbox，dispatcher 投递到 Redpanda，
`storage-worker` 消费后删除 MinIO 对象并把状态推进到 `deleted`。
后台轮询 GC 仍然存在，作为 Kafka 链路不可用时的最终一致性兜底。

## 2. 为什么这样划分

### 2.1 按"数据的唯一属主"划分 RPC 服务

- 用户、令牌、配额只属于 `user.rpc`；
- 文件、目录、分享、版本只属于 `file.rpc`；
- 上传任务、分片、对象元数据只属于 `storage.rpc`。

任何跨服务的数据访问都必须通过 RPC，不允许直连别的服务的表。
这条规则的价值在改动时体现：改配额算法只需要动 user 服务，不需要全仓库搜索 SQL。

### 2.2 把"控制面"和"数据面"分开

`storage.rpc` 是控制面：记录状态机、校验参数、合并分片、写元数据。
`storage-api` 是数据面：只搬字节，不关心业务。

这样拆的原因：

1. **大流量不经过 gRPC 序列化**。视频/大文件的字节流如果走 gRPC，需要额外的分帧与内存拷贝。
2. **HTTP 天然支持 Range 与流式响应**，浏览器/CDN 也更容易对接。
3. 数据面只依赖 MinIO + 一次 RPC 校验，故障面小。

代价是 storage 有两个进程、两套配置。这个代价被认为是值得的（见 ADR-0002）。

### 2.3 file-api 做"编排者"，不做"数据持有者"

一次上传涉及三个服务、两张库、一个对象存储：

```text
file-api.InitUpload
  ├─ file.rpc.CreateFile            创建 uploading 元数据（幂等）
  ├─ user.rpc.ReserveQuota          预占配额
  └─ storage.rpc.InitUpload         创建上传会话（返回分片参数）
```

file-api 不写库，只做「顺序 + 补偿」。这样补偿逻辑集中在一处，
而不是散落在三个服务里互相调用。

其余编排同理：`CompleteUpload`（合并 → 追加版本 → 提交配额）、
`DeleteFile`（软删 → 归还配额 → 删对象）。

## 3. 关键调用链

### 3.1 上传

```text
Client → file-api  POST /v1/uploads
                    ├→ file.rpc CreateFile
                    ├→ user.rpc ReserveQuota
                    └→ storage.rpc InitUpload
         ← uploadId / chunkSize / totalChunks

Client → storage-api PUT /v1/uploads/:id/parts/:n   （每片一次）
                    ├→ storage.rpc GetUploadTask   （校验归属与分片号）
                    ├→ MinIO PutObject(parts/...)
                    └→ storage.rpc RecordPart

Client → file-api  POST /v1/uploads/:id/complete
                    ├→ storage.rpc CompleteUpload  （合并 + 校验 + 对象元数据）
                    ├→ file.rpc AddVersion         （追加版本，幂等）
                    └→ user.rpc CommitQuota        （仅在版本真的变化时）
```

### 3.2 下载

```text
Client → file-api GET /v1/files/:id/content
                 ├→ file.rpc GetFile            （归属校验、状态必须 available）
                 └→ storage-api GET /internal/v1/storage/objects/content
                        （内部令牌鉴权 + Range，直接流式转发）
```

客户端永远拿不到 `objectKey`；内部下载接口只接受带 `X-Internal-Token` 的调用。

### 3.3 分享下载

```text
Client → file-api POST /v1/shares/:token/access   （校验密码/有效期/次数，占用一次额度）
Client → file-api GET  /v1/shares/:token/content  （再校验一次并代理对象流）
```

分享 token 与对象 Key 是两套完全不同的标识，token 泄露不等于对象泄露；
撤销分享只需把 `shares.status` 置 0。

### 3.4 对象清理（事件驱动）

```text
file-api DeleteFile
  └─> storage.rpc DeleteObject
         │ 同一事务：status='deleting' + INSERT outbox_events
         └─ 返回 Success=true（入队即成功）

storage.rpc dispatcher（轮询 outbox_events）
  └─> PublishBatch → Redpanda topic `storage.events` → MarkPublished

storage-worker（consumer group）
  ├─ 非 deleting / 对象不存在 → commit（幂等）
  ├─ Remove 成功 → status='deleted' → commit
  ├─ 失败未超限 → 不 commit，退避后重建 consumer 重新入组（at-least-once）
  └─ 失败达上限 → storage.events.dlq 成功后才 commit
```

兜底：`maintenance.go` 的 `retryObjectDeletes` 仍每 10 分钟扫描 `status='deleting'`。
设计细节见 `docs/kafka-design.md`，决策见 `docs/adr/0007-event-driven-object-cleanup.md`。

## 4. 服务治理

- **服务发现**：etcd。RPC 客户端通过 `zrpc` 自动发现实例。
- **超时**：HTTP 全局 `Timeout`（10s，数据面路由单独关闭）；RPC 客户端默认超时由 go-zero 管理。
- **熔断/降级**：go-zero 内置 breaker 中间件默认开启。
- **限流**：登录/注册按 IP 令牌桶；上传分片受 `MaxBytes` 与 `MaxChunkBytes` 限制。
- **优雅关闭**：go-zero `proc` 在收到 SIGTERM 时停止接收新请求。

## 5. 部署形态

- 本地：Docker Compose，七个服务（含 `storage-worker`）+ 五个基础设施容器
  （MySQL / Redis / etcd / MinIO / Redpanda）。
- Kubernetes：Deployment（无状态服务）+ StatefulSet（MySQL/Redis/etcd/MinIO）。
  真实生产环境应把基础设施换成托管服务。
  当前清单尚未包含 Redpanda 与 `storage-worker`（见 `deploy/kubernetes/README.md`）。

## 6. 一个请求的完整视角

以「上传完成后客户端获取文件列表」为例：

1. Client → `file-api` `GET /v1/files?parentId=3`，带 Bearer token；
2. `file-api` 的 Auth 中间件调用 `user.rpc.ValidateToken`（JWT 验签 + 查库确认账号状态）；
3. `file-api` 调 `file.rpc.ListFiles`，file.rpc 查 `folders` + `files` 两张表；
4. `file.rpc` 返回条目，`file-api` 转成 HTTP DTO；
5. 全程有 requestId、结构化日志、Prometheus 指标。

每一步都对应一个可观测点：哪一步慢、哪一步错，日志和指标里都能直接看到。
