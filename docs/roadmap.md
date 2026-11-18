# 阶段路线与完成状态

对照 [target.md](../target.md) 的 Phase 0-6，如实记录"完成 / 部分完成 / 未开始"。

## Phase 0：工程骨架 ✅

- [x] 审查现有目录结构（`common/ service/ deploy/ docs/ scripts/`）
- [x] 三个服务的 API 定义（`service/*/api/*.api`）
- [x] Protobuf 与 RPC 定义（`service/*/rpc/*.proto`）
- [x] SQL DDL（三份 schema，`IF NOT EXISTS` 可重放）
- [x] goctl 生成代码可编译（`go build ./...` 通过）
- [x] 各服务配置与启动方式（`etc/*.yaml` 本地 + `deploy/config/*.yaml` 容器）
- [x] 服务调用关系明确（见 `docs/architecture.md`）
- [x] 统一错误处理（`common/web`）与结构化日志（JSON）

## Phase 1：基础业务闭环 ✅

- [x] 用户注册和登录
- [x] Token 鉴权（JWT + 每次请求查库确认账号状态）
- [x] 文件夹创建与查询
- [x] 小文件上传（单片，真实落 MinIO）
- [x] 文件列表查询（分页）
- [x] 文件下载（流式 + Range）
- [x] 文件删除（软删 + 对象清理 + 配额归还）
- [x] 基础配额检查（预占/提交/归还）
- [x] MySQL 和 MinIO 本地部署（compose）
- [x] 核心业务单元测试 + 端到端验收脚本

## Phase 2：业务完善与数据可靠性 ✅（除标注项）

- [x] 文件重命名和移动（含环检测、重名检查）
- [x] 文件夹删除规则（递归删除，对象清单返回给编排层）
- [x] 大文件分片上传（8MiB 分片）
- [x] 上传进度查询
- [x] 断点续传（缺失分片列表）
- [x] 幂等提交（`idempotencyKey` / `uploadId` / `request_id`）
- [x] 文件内容校验（可选 SHA-256，不匹配拒绝并清理）
- [x] 上传失败清理（超时任务、孤儿分片、`completing` 回收）
- [x] 对象与元数据一致性检查（`tools/reconcile`）
- [x] 文件分享和撤销（密码、有效期、次数限制）
- [x] 文件版本管理（列表、追加、回滚）

## Phase 3：工程化与安全 ✅（除标注项）

- [x] 完善参数校验（名称、分片、Range、幂等键）
- [x] 完善权限控制（owner 校验，越权返回 404）
- [x] 统一错误码和响应结构（`common/web`）
- [x] 配置请求超时（HTTP Timeout，数据面单独关闭）与 RPC 超时（go-zero 默认）
- [x] 检查并发安全（`-race`；`AppendVersion` 行锁；配额条件更新）
- [x] 关键路径端到端测试
- [x] Docker 镜像（单 Dockerfile + `SERVICE_DIR`）
- [x] Docker Compose
- [x] CI 流程（GitHub Actions：lint / test / build / compose config）
- [x] 配置和密钥管理（环境变量 + `${VAR}` 展开 + `.env.example`）
- [x] 故障排查文档

## Phase 4：可观测性与性能（部分完成）

- [x] 统一结构化日志（JSON，含 trace/span）
- [x] HTTP 与 RPC 指标（go-zero 内置 `http_server_*` / `rpc_server_*`）
- [x] 集成 Prometheus（dev server `/metrics` + scrape 配置）
- [x] Grafana 仪表盘（`deploy/observability/grafana`）
- [ ] 链路追踪（OpenTelemetry）——未开始
- [x] 编写性能测试方法（`docs/performance.md`）
- [ ] 性能基线数据——未测量，文档里没有编造数字
- [ ] 有数据支撑的性能优化——未开始

## Phase 5：分布式能力（除标注项）

- [x] 异步任务机制
- [x] Kafka 事件与版本兼容
- [x] 消息重复投递处理
- [x] 消费失败与重试
- [x] 死信队列
- [ ] Redis 缓存优化
- [x] 事件最终一致性边界

现状说明：对象清理已改为「事务性 outbox → Kafka → storage-worker」，失败按退避重试、
超限进 DLQ，原有 10 分钟轮询 GC 保留为最终一致性兜底。设计见 `docs/kafka-design.md`，
决策见 `docs/adr/0007-event-driven-object-cleanup.md`。超时任务回收仍用"数据库状态 + 后台轮询"。
Redis 缓存优化尚未开始。

## Phase 6：生产化部署（部分完成）

- [x] 镜像构建（多阶段、非 root、精简）
- [x] Docker Compose 环境
- [x] Kubernetes 部署清单（Deployment/Service/StatefulSet/ConfigMap/Secret 模板）
- [x] 健康检查（`/healthz`、`/readyz`、readiness/liveness 探针）
- [x] 资源 requests/limits
- [ ] 滚动更新验证（清单已配置策略，未在真实集群演练）
- [ ] 服务重启与故障恢复演练（本地做过容器重启验证）
- [x] 监控与告警配置
- [ ] 部署、升级、回滚文档演练记录（`deploy/kubernetes/README.md` 已写步骤）
- [ ] 最终验收报告

---

## 下一步优先级建议

1. **用真实数据补性能基线**（Phase 4 的核心缺口）。
2. **K8s 演练**：滚动更新、Pod 故障、扩副本后观察 etcd 发现与限流表现。
3. **配额对账自动化**：`tools/reconcile` 目前只报告配额差异，改为可确认后修复。
4. **Web 前端**：哪怕是最小可用的上传/列表/下载页面，也能显著提升可演示性。
5. **审计日志**：谁在何时删了什么，是网盘类系统绕不开的需求。
