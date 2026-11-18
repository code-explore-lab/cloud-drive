# 架构决策记录（ADR）

记录"为什么这样设计"以及被否掉的替代方案。格式参考 Michael Nygard 的 ADR 实践。

| 编号 | 决策 | 状态 |
| --- | --- | --- |
| [0001](0001-gozero-microservices.md) | 采用 GoZero 微服务拆分，而不是单体 | 已采纳 |
| [0002](0002-storage-control-and-data-plane.md) | 存储服务拆成控制面（rpc）与数据面（api） | 已采纳 |
| [0003](0003-server-side-object-keys.md) | 对象 Key 由服务端生成，且不暴露给客户端 | 已采纳 |
| [0004](0004-consistency-without-2pc.md) | 跨资源一致性用编排 + 幂等 + 补偿，不做分布式事务 | 已采纳 |
| [0005](0005-chunked-upload-compose.md) | 分片先写临时对象，完成时服务端合并 | 已采纳 |
| [0006](0006-optional-hash-verification.md) | 内容摘要按需校验，不做默认全量校验 | 已采纳 |
| [0007](0007-event-driven-object-cleanup.md) | 对象清理改为 outbox + Kafka 事件驱动，GC 保留为兜底 | 已采纳 |

## 什么时候写 ADR

- 影响服务边界、数据归属、部署形态的改动；
- 引入新中间件（Kafka/Redis/OTel）；
- 放弃某个"显而易见"的方案而选择另一个，且这个选择未来会被质疑。

## 不写什么

- 代码风格、命名、目录结构（README 里说明即可）；
- 随时可以回退的小改动。

## 模板

```markdown
# ADR-000N：标题

- 状态：提议 / 已采纳 / 已废弃
- 日期：YYYY-MM

## 背景
## 决策
## 理由
## 代价
## 替代方案
## 后果
```
