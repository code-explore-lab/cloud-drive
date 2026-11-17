# Kafka 事件驱动对象清理 · 设计文档

> 状态：**设计定稿，待实现**。本文档回答三个问题——为什么用 Kafka 而不是 RabbitMQ、选哪个 Kafka、Kafka 在这个项目里做什么——并给出可照着实现的设计。
>
> 关联：`target.md` Phase 5、`docs/consistency.md`、`docs/adr/0004-consistency-without-2pc.md`、`docs/roadmap.md`。

---

## 1. 背景与目标

对象清理（删除文件后清掉 MinIO 里的实际对象）目前靠「数据库状态 + 后台轮询」实现：

- `storage.rpc.DeleteObject`（`service/storage/rpc/internal/logic/deleteobjectlogic.go`）标记
  `storage_objects.status='deleting'`，失败时累加 `gc_attempts`；
- `jobs/maintenance.go` 的 `retryObjectDeletes` 每 10 分钟扫一遍 `status='deleting'` 重试删除。

这套机制已经能保证「最终清理」，但它把「要清理什么」和「什么时候清理」绑在了轮询周期上，
也缺少事件驱动的解耦与可观测性。本项目 `target.md` 的 Phase 5（分布式能力）一直标为「未开始」，
其验收要求是：

- 为耗时任务设计异步执行机制；
- 在适合的场景引入 Kafka，设计事件格式与版本兼容；
- 处理消息重复投递、消费失败与重试；
- 设计死信/失败任务流程；
- 明确事件最终一致性的边界。

本次范围：**只把「对象清理」这一条链路事件化**，作为引入 Kafka 的第一个真实场景。
不扩展到审计日志、秒传去重、Redis 缓存优化、OpenTelemetry 或完整 Phase 5 通用框架。

设计目标：

1. 事务内写 outbox，事件不因进程崩溃而丢失；
2. 事件带版本号，可向前兼容；
3. 消费幂等（重复投递不重复删、不报错）；
4. 消费失败可重试，达到上限进入死信队列；
5. 保留现有 10 分钟 GC 作为「最终一致性兜底」，不推翻已验证的正确性。

---

## 2. 为什么是 Kafka，而不是 RabbitMQ

### 2.1 项目既定技术栈已经点名 Kafka

`target.md` 技术栈表写的是「消息队列 | Kafka，可选进阶」，Phase 5 清单也是「在适合的场景引入 Kafka」。
这是项目自己定下的方向。

### 2.2 对象清理是「事件流」形状，不是「任务队列」形状

- 清理事件是**只追加、可重放、要多个消费者独立消费**的：一个「对象待删除」事件，未来可以被
  「清理消费者」「审计消费者」「对账/指标消费者」各自读一遍，互不影响。
  Kafka 的 consumer group + 消息保留天然支持这个。
- RabbitMQ 是「消费完就没了」的队列模型，主打灵活路由、延迟队列、优先级、逐条 ack，
  这些本事在这个场景用不上。

### 2.3 「事件最终一致性 + 死信 + 重放」这条主线，Kafka 更贴合

Phase 5 验收要求「重复执行、消费失败、服务重启都不静默丢失」。Kafka 的 append-only 落盘 +
offset 重放让「重放一遍事件补账」变得自然；死信是一个独立 topic，可以随时回放排查。
RabbitMQ 也有死信交换机，但重放整个历史流要麻烦得多。

### 2.4 诚实的边界

对这个单机课程项目，RabbitMQ 完全够用，甚至部署更轻。但「够用」不是引入它的理由——
现在的「DB 状态 + 轮询 GC」也够用。引 Kafka 本身就是为了示范事件驱动那套范式，
而 Kafka 的事件日志语义正好和「对象清理事件化」是同一件事。RabbitMQ 的强项在这里是冗余的。

**结论：跟项目既定技术栈走 + 场景是事件流而非 RPC 任务队列，所以选 Kafka。**

---

## 3. 选哪个 Kafka：Redpanda

| 项 | 选择 | 理由 |
| --- | --- | --- |
| Broker | **Redpanda**（Kafka 协议兼容，单节点 KRaft） | 单二进制、内存占用小，适配 Windows + Docker Desktop 资源受限（README 已记录 BuildKit 因资源不足崩溃）；代码走标准 Kafka 协议，生产可无缝换成 Apache Kafka |
| Go 客户端 | **segmentio/kafka-go** | 纯 Go 无 cgo、API 简单、支持 consumer group + 手动 commit；比 sarama/franz-go 更适合课程项目 |
| 生产者 | `storage.rpc`（outbox dispatcher） | 复用其 MySQL/MinIO/后台任务模式 |
| 消费者 | 新增 `storage-worker` 服务 | 生产/消费分离，worker 可独立扩缩容（Kafka 的核心收益） |

> 备选：Apache Kafka（bitnami，KRaft 模式）。代码完全相同，只有 broker 镜像不同。

---

## 4. 事件流

```text
file-api DeleteFile
   └─> storage.rpc DeleteObject
          │  （同一 DB 事务）
          ├─ storage_objects.status = 'deleting'
          └─ INSERT outbox_events (pending)           ← 事务性 outbox，不丢事件
   ┌──────────── storage.rpc 后台 dispatcher（轮询 outbox pending）────────────┐
   │  publish → Redpanda topic `storage.events` → 标记 published               │
   └──────────────────────────────────────────────────────────────────────────┘
   storage-worker（consumer group）
      │  ReadMessage
      ├─ 状态 != 'deleting' → commit，跳过（幂等）
      ├─ MinIO Remove 成功 → status='deleted' → commit
      ├─ 失败且 gc_attempts < 上限 → 不 commit（重投递，at-least-once）
      └─ 失败且 gc_attempts >= 上限 → 发 `storage.events.dlq` → commit
   ──────────────────────────────────────────────────────────────
   兜底：jobs/maintenance.go retryObjectDeletes 仍每 10 分钟扫 'deleting'
```

关键点：

- **复用现有 `storage_objects.gc_attempts` / `last_error` 作为消费者的重试计数**，不新增重复字段；
- `status` 字段天然提供幂等：消费时发现状态已非 `deleting` 就 commit 跳过；
- dispatcher 只负责「把 outbox 里的待发事件可靠地搬到 broker」，是否真的清理成功由
  `storage_objects.status`（`deleting → deleted`）+ `gc_attempts` 追踪；
- GC 兜底保持不变，Kafka 是「低延迟解耦快路径」，轮询 GC 是「最终一致性安全网」。

---

## 5. 事件格式（含版本）

```go
// common/xkafka/event.go
type Envelope struct {
    Version    int             `json:"version"`     // 当前 = 1，向前兼容
    Type       string          `json:"type"`        // "storage.object.cleanup.requested"
    EventID    string          `json:"event_id"`
    OccurredAt time.Time       `json:"occurred_at"`
    Payload    json.RawMessage `json:"payload"`
}

type ObjectCleanupRequested struct {
    ObjectKey string `json:"object_key"`
}
```

Topic 常量：`storage.events`、`storage.events.dlq`。

版本兼容策略：消费者按 `Envelope.Version` 分发；未来新增字段只增不改，旧消费者忽略未知字段。
这是 target.md「设计事件格式和版本兼容策略」的落地。

---

## 6. 改动点清单（供实现参考）

> 每个条目标注了要复用的现有模式，避免重新造轮子。

### 6.1 依赖
- `go.mod` 新增 `github.com/segmentio/kafka-go`。

### 6.2 `common/xkafka/`（现有空目录，填入）
- `config.go` — `Config{ Brokers []string; Topic string; GroupID string }`，带 go-zero 默认值 tag。
- `producer.go` — 封装 `kafka.Writer`：`NewProducer` / `Publish(ctx, key, value)` / `Close`。
- `consumer.go` — 封装 `kafka.Reader`（group）：`NewConsumer` / `ReadMessage` / `CommitMessages` / `Close`。
- `event.go` — `Envelope` + `ObjectCleanupRequested` + 事件类型/topic 常量。

### 6.3 数据库
- `service/storage/model/storage.sql` 新增 `outbox_events` 表，沿用 `IF NOT EXISTS` 可重放约定：

```sql
CREATE TABLE IF NOT EXISTS `outbox_events` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `event_type` VARCHAR(64) NOT NULL,
    `aggregate_id` VARCHAR(512) NOT NULL,
    `payload` JSON NOT NULL,
    `status` VARCHAR(16) NOT NULL DEFAULT 'pending' COMMENT 'pending, published, failed',
    `attempts` INT UNSIGNED NOT NULL DEFAULT 0,
    `last_error` VARCHAR(512) NOT NULL DEFAULT '',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `published_at` DATETIME DEFAULT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_outbox_status_created` (`status`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 6.4 storage.rpc（生产者 + dispatcher）
- `service/storage/model/outboxeventsmodel.go`（新）— 仿 `storageobjectsmodel.go`：
  `FindPending(ctx, limit)`、`MarkPublished(ctx, id)`、`MarkFailed(ctx, id, attempts, err)`。
- `service/storage/rpc/internal/config/config.go` — 新增 `Kafka xkafka.Config` 与
  `Outbox OutboxConf{Enabled, Interval, BatchSize}`（仿 `MaintenanceConf`）。
- `service/storage/rpc/internal/svc/servicecontext.go` — 注入 `OutboxModel` + `Producer`；
  `New` 不立即连接（沿用 `common/objectstore` 的「启动时基础设施未就绪也能起」约定）。
- `service/storage/rpc/internal/logic/deleteobjectlogic.go` — 改造：不再立即 `Store.Remove`，
  改用 `svcCtx.Conn.TransactCtx`（事务写法见 `service/file/model/fileversionsmodel.go:84` 的
  `AppendVersion`）在同一事务内 `UPDATE storage_objects SET status='deleting'` +
  `INSERT outbox_events`；对象不存在 / 已 `deleted` 仍幂等返回 `Success=true`、不发事件。
- `service/storage/rpc/internal/jobs/dispatcher.go`（新，仿 `maintenance.go` ticker 循环）—
  轮询 outbox pending → 组装 Envelope → `Publish` → 标 published；失败累加 attempts、超限标 failed。
- `service/storage/rpc/storage.go` — 启动 dispatcher（`if c.Outbox.Enabled`）。
- `deploy/config/storage.rpc.yaml` — 新增 `Kafka:` / `Outbox:` 段，Brokers 用 `${KAFKA_BROKERS}`。

### 6.5 storage-worker（新消费者服务）
- `service/storage/worker/worker.go`（main）、`internal/config/config.go`、
  `internal/svc/servicecontext.go`、`internal/consumer/consumer.go`。
- `config`：`Name/Log` + `Kafka xkafka.Config`（GroupID=`storage-worker`）+
  `Minio objectstore.Config` + `Mysql{DataSource}` + `CacheRedis` + `Worker{RetryMax, Backoff}`。
- `svc`：`ObjectsModel` + `Store`（MinIO）+ consumer reader。
- `consumer.go` 处理 `storage.object.cleanup.requested`：查 object → 非 `deleting` 则 commit 跳过；
  `Store.Remove` 成功 → `status='deleted'` + commit；失败 → `gc_attempts++`/`last_error`，
  未超 `RetryMax` 不 commit（重投递），超限发 DLQ 后 commit。
- `deploy/config/storage-worker.yaml`（新）。

### 6.6 基础设施
- `docker-compose.yml` 新增 `redpanda` 服务（单节点 KRaft：`--smp 1 --memory 512M --overprovisioned`，
  内部 `PLAINTEXT://redpanda:9092`、外部 `OUTSIDE://127.0.0.1:19092`，端口 19092，
  healthcheck `rpk cluster health`，命名卷 `redpanda-data`，镜像 pin 版本）。
- 新增 `x-kafka-env` 锚点：`KAFKA_BROKERS: "redpanda:9092"`，挂到 storage.rpc 与 storage-worker。
- 新增 `storage-worker` compose 服务（`SERVICE_DIR=./service/storage/worker`，
  `depends_on: redpanda(healthy)+mysql+minio`）。
- `.env.example` 新增 `KAFKA_BROKERS`（默认 redpanda:9092）。

### 6.7 CI
- `.github/workflows/ci.yml` 的 build 矩阵新增 `storage-worker` 条目。

### 6.8 文档
- 新 ADR `docs/adr/0007-event-driven-object-cleanup.md`（README 明确「引入新中间件需写 ADR」）。
- `docs/roadmap.md` Phase 5：勾选 异步任务机制 / Kafka 事件与版本兼容 / 消息重复投递 /
  消费失败与重试 / 死信队列 / 事件最终一致性边界；`Redis 缓存优化` 仍标未开始。
- `docs/architecture.md`（infra 图加 Redpanda、服务加 storage-worker）、
  `docs/consistency.md`（对象清理章节改为 outbox + Kafka + GC 兜底）、
  `docs/database.md`（outbox_events 表）。
- `README.md`：端口表加 Redpanda 19092；「明确还未实现/已知限制」移除 Kafka。

---

## 7. 验证方案

1. 静态：`gofmt -l .`、`go vet ./...`、`go build ./...`、`go test ./... -count=1`（CI 跑 `-race`）。
2. 重建并启动：`COMPOSE_PARALLEL_LIMIT=3 docker compose up -d --build`。
   ⚠️ `outbox_events` 表在**已存在的 mysql 卷上不会自动创建**（initdb 只在首次初始化执行）——
   需要 `docker compose down -v`（清空卷重初始化）或手动导入该 DDL。
3. 链路：`docker compose exec redpanda rpk topic list` 见 `storage.events` / `storage.events.dlq`；
   `go run ./scripts/e2e` 仍全绿（删除的元数据/配额路径不变，仍是同步；对象清理改为异步）。
4. 事件驱动删除：删文件后 `rpk topic consume storage.events` 看到 `storage.object.cleanup.requested`，
   且 `storage_objects.status='deleted'`、MinIO 对象消失（无需等 10 分钟）。
5. 失败与死信：`docker compose stop storage-worker`（或停 minio）后删文件 → 对象停留 `deleting` →
   恢复 worker 重试；反复失败达 `RetryMax` 后事件进入 `storage.events.dlq`；
   同时验证 GC 兜底仍能最终清理。

---

## 8. 不在本次范围

审计日志流、秒传/去重、Redis 缓存优化、OpenTelemetry、完整 Phase 5 通用 outbox 框架
（本次只做对象清理这一条事件，通用化留到有第二条事件需求时再抽象）。
