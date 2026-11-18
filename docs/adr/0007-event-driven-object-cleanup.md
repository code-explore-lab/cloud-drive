# ADR-0007：对象清理改为 outbox + Kafka 事件驱动，GC 保留为兜底

- 状态：已采纳
- 日期：2026-11

## 背景

对象清理原本是「数据库状态 + 后台轮询」：`DeleteObject` 同步调用 MinIO 删除，
失败时把 `storage_objects.status` 置为 `deleting` 并累加 `gc_attempts`，
由 `maintenance.go` 每 10 分钟重试一次。

这套机制能保证最终清理，但有两个问题：

1. 删除对象是同步的外部调用，MinIO 慢或不可用会拖长删除接口的响应时间；
2. 清理延迟被轮询周期绑定，最坏 10 分钟，且没有事件流可供审计/对账等其它消费者使用。

`target.md` Phase 5 要求：异步执行机制、引入 Kafka、事件版本兼容、重复投递处理、
消费失败与重试、死信流程、明确最终一致性边界。完整设计见 `docs/kafka-design.md`。

## 决策

1. **事务性 outbox**：`DeleteObject` 在同一个 MySQL 事务里把对象置为 `deleting` 并写入
   `outbox_events`（`event_id` 在事务内生成，重试不变），不再同步删除对象。
2. **dispatcher**：`storage.rpc` 内的后台任务轮询 `outbox_events`（`status='pending'` 且
   `next_attempt_at <= now()`），批量投递到 `storage.events`，成功后条件更新为 `published`；
   失败按指数退避重试，超过上限标 `failed` 供人工排查。
3. **独立消费者**：新增 `storage-worker`，以 consumer group 消费 `storage.events`，
   删除 MinIO 对象并把 `status` 推进到 `deleted`。
4. **at-least-once 重试**：消费失败时不 commit，停止消费、指数退避、重建 consumer 重新入组，
   由 broker 从 last committed offset 重投（原因见 `kafka-design.md` §4.1）。
5. **失败计数复用 `storage_objects.gc_attempts` / `last_error`**，达到 `RetryMax` 后把原始事件
   发到 `storage.events.dlq`，发送成功后才 commit。
6. **保留 10 分钟轮询 GC**（`retryObjectDeletes`）作为最终一致性安全网，不删除。

## 理由

- **outbox 解决双写问题**：如果先提交业务事务再发 Kafka，进程在两步之间崩溃会永久丢事件；
  反过来先发 Kafka 再提交事务，则可能发出一个数据库里不存在的删除。把「状态变更」和
  「待发事件」放进同一个事务，dispatcher 可以从容重试，事件不会丢。
- **Kafka 是事件流形状**：清理事件只追加、可重放、未来可以有清理/审计/对账多个消费组独立读取，
  这与 RabbitMQ 的「消费完即删除」队列模型不同。
- **消费端天然幂等**：`status='deleting'` 是幂等判据，重复投递只是多查一次库然后 commit；
  因此 dispatcher 重投、worker 重入组都不需要额外去重表。
- **DLQ 与 GC 职责不同**：DLQ 是告警和排查信号，不是「清理结束」的标志；
  即使事件进了 DLQ，GC 仍会继续尝试，避免对象永远残留。

## 代价

- 多一个 broker（Redpanda）和一个独立进程（storage-worker），本地运维成本上升。
- 重试 = 关闭 reader + rebalance + 重新入组，单 worker 场景可以接受，但每次重试有秒级开销，
  必须配合指数退避，否则 `gc_attempts` 会在毫秒级耗尽。
- `outbox_events` 中 `published` 的行目前不会自动清理，数据量随删除操作线性增长
  （当前数据量级可以忽略，后续需要归档或定期删除任务）。
- 删除接口变成「入队即成功」，调用方不再能立刻知道对象是否真的删掉；
  真实结果以 `storage_objects.status` 和指标为准。

## 替代方案

| 方案 | 结论 |
| --- | --- |
| 维持「同步删除 + 轮询 GC」 | 简单，但延迟高、无事件流，Phase 5 验收不满足 |
| 在业务事务里直接发 Kafka | 双写：进程崩溃会丢事件，或发出不存在的事件；否决 |
| RabbitMQ / Redis Stream | 也能实现，但项目技术栈已定 Kafka，且事件流语义更贴合 |
| 引入通用 outbox 框架 | 当前只有一条事件，抽象成本大于收益；留到第二条事件出现时再做 |

## 后果

- 删除相关的可观测点变为：`outbox_events` 积压、dispatcher 重试/失败数、
  `storage.events.dlq` 长度、`storage_objects.status='deleting'` 积压（原有 GC 指标继续有效）。
- 文档边界必须写清楚：Kafka 是「低延迟快路径」，GC 是「最终一致性兜底」，
  两者都不可单独删掉。
- 新增部署依赖：`docker compose` 中的 `redpanda` 与 `storage-worker`；
  `outbox_events` 表在已有 MySQL 卷上不会自动创建，需要重建卷或手动导入 DDL。
