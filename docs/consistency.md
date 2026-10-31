# 一致性设计

系统的"一致性"不是一个开关，而是三种不同的东西：

1. **单服务内的数据库事务** —— MySQL 保证；
2. **跨服务的业务一致性** —— 由编排 + 幂等 + 补偿保证；
3. **MySQL 与对象存储之间的一致性** —— 两个系统根本不在一个事务里，只能靠补偿、重试、对账。

本文分别说明处理方式，以及**已知的、没有被完全解决**的窗口。

---

## 1. 单服务内：真实事务

| 操作 | 事务内容 |
| --- | --- |
| 注册 | `insert users` + `insert user_quotas`（同一事务，失败则都不落库） |
| 追加版本（`AddVersion`/`RestoreVersion`） | `select files for update` + `insert file_versions` + `update files` |
| 撤销分享 | 单条 `update shares`（原子） |
| 批量软删 | 单条 `update ... where id in (...)`（原子） |

事务的选择标准：**同一份业务事实的两条写入**必须在一起。
例如"文件指针"和"版本行"必须同时生效，否则会读到一个不存在的版本。

`AppendVersion` 用 `SELECT ... FOR UPDATE` 而不是乐观重试：
同一文件的新版本上传是低频操作，锁行的代价远小于实现重试逻辑的复杂度。

---

## 2. 跨服务：编排 + 幂等 + 补偿

### 2.1 上传完成（最复杂的一条）

```text
storage.rpc.CompleteUpload   合并对象 + 写 storage_objects     ← 外部副作用，不可回滚
       │
       ▼
file.rpc.AddVersion          追加版本（幂等键 uploadId）        ← 本地事务
       │
       ▼
user.rpc.CommitQuota         used += size, reserved -= size    ← 本地事务
```

三步都不是分布式事务，靠以下机制收敛：

- **每一步都幂等**：`uploadId` 是 `CompleteUpload` 的幂等键、`AddVersion` 的幂等键；
  `CommitQuota` 只在"版本确实新增了"（`changed=true`）时调用。
- **顺序有讲究**：先落对象、再落版本。反过来的话，客户端会看到一个永远打不开的文件。
- **失败可重试**：任一步失败，客户端重发 `complete` 会重新走一遍；
  已经成功的步骤因幂等不会重复生效。

**未完全解决的窗口**：`AddVersion` 成功、`CommitQuota` 失败时，
文件可用但配额少扣了 `size` 字节。处理方式是记 error 日志，
由对账任务（`tools/reconcile`，见 §4）发现"版本总大小 > used_bytes"并修正。
这是有意的取舍：不允许因为记账失败而让用户的上传白做。

### 2.2 文件删除（Saga）

```text
file.rpc.DeleteFile      软删文件 + 撤销分享           ← 本地事务（两条语句，均可幂等重放）
       │
       ├─► user.rpc.ReleaseUsedQuota                   ← 失败只记日志
       │
       └─► storage.rpc.DeleteObject（每个对象一次）      ← 失败由 GC 重试
```

顺序不能反：先让文件在业务上不可见（`deleted_at`），再做外部清理。
反过来会出现"对象已经删了但文件还在列表里"的不可恢复状态——
对象删掉了就真的拿不回来了，而记录还在会让用户以为文件没坏。

`ReleaseUsedQuota` 与 `DeleteObject` 都是幂等的：

- `DeleteFile` 第二次调用返回 `changed=false`，文件服务就不会再次归还配额；
- `DeleteObject` 对已删除对象直接返回成功。

### 2.3 上传初始化

```text
file.rpc.CreateFile   →  user.rpc.ReserveQuota  →  storage.rpc.InitUpload
```

任一步失败都会补偿前面已经成功的步骤：

- `CreateFile` 成功、`ReserveQuota` 失败 → 文件标记 `failed`；
- `ReserveQuota` 成功、`InitUpload` 失败 → 归还预占 + 文件标记 `failed`。

补偿本身失败只会留下一条 `failed` 文件记录和一份预占额度，
后台清理任务会把预占额度随过期任务一起归还。

---

## 3. MySQL 与对象存储之间

这是系统里唯一"不能靠事务解决"的地方。原则：

1. **对象内容先写，元数据后写**。这样最坏情况是"有对象没记录"（可回收的垃圾），
   而不是"有记录没对象"（坏文件）。
2. **对外可见性只看数据库**。`storage_objects.status='available'` 才可下载；
   临时分片对象永远不可下载。
3. **删除反向来**：先让数据库状态变成不可见/待删除，再删对象。
4. **失败要留痕、要能重试**：
   - `storage_objects.gc_attempts` / `last_error` 记录失败原因；
   - 后台维护任务每 10 分钟重试 `status='deleting'` 的对象；
   - 指标 `clouddrive_storage_object_gc_backlog` 与 `clouddrive_storage_object_gc_failures_total`
     暴露积压与失败，配了告警。

### 3.1 后台维护任务做了什么

`service/storage/rpc/internal/jobs/maintenance.go`：

| 步骤 | 作用 |
| --- | --- |
| `expireUploads` | 超时任务改 `failed`，清理其临时分片 |
| `recoverStuckCompleting` | 15 分钟仍停在 `completing` 的任务放回 `uploading`（进程崩溃残留） |
| `retryObjectDeletes` | 重试 `status='deleting'` 的对象删除 |
| `cleanupOrphanParts` | 清理 `parts/` 下 48h 前的临时对象（删除失败的兜底） |
| `refreshGauges` | 刷新活跃上传数、GC 积压指标 |

这些任务全部是**幂等且可重入**的，多副本同时跑也不会产生冲突
（每个动作都是"把状态推进到目标值"，重复执行无副作用）。

### 3.2 已知窗口

| 窗口 | 后果 | 兜底 |
| --- | --- | --- |
| 合并成功、`storage_objects` 写入失败 | 有对象无记录（孤儿对象） | 重试完成会覆盖写；`tools/reconcile` 能列出孤儿 |
| 对象删除成功、`status='deleted'` 更新失败 | 记录说 available，实际对象没了 | 下载时报错；reconcile 能发现"记录在但对象不在" |
| 版本追加成功、配额提交失败 | 配额少扣 | reconcile 对比版本总大小与 used_bytes |
| 目录递归删除中途失败 | 部分子项已删 | 重新调用删除（幂等），会继续删剩下的 |

---

## 4. 对账工具

`go run ./tools/reconcile` 做三件事：

1. 扫描 `storage_objects`，用 MinIO `StatObject` 检查对象是否真的存在；
2. 扫描 MinIO 的 `u/` 前缀，找出没有对应 `storage_objects` 记录的孤儿对象；
3. 对比 `file_versions` 的对象总大小与 `user_quotas.used_bytes` 的差异（按用户）。

默认只报告不修改（dry-run）。加 `-fix` 会把缺失记录的孤儿对象登记为 `deleting`，
由 GC 清理；配额差异只报告，不自动改账（账目自动修改需要人确认）。

运行示例：

```bash
go run ./tools/reconcile -config service/storage/rpc/etc/storage.yaml
```

---

## 5. 幂等记录的生命周期

| 幂等键 | 存在哪里 | 生命周期 |
| --- | --- | --- |
| `files.request_id` | files 表 | 与文件同生命周期（删除后行保留） |
| `upload_id` | upload_tasks | 任务行保留，超时后置 failed |
| `file_versions.request_id` | file_versions | 永久（历史版本不删） |
| 分享 token | shares | 撤销后行保留，可审计 |

没有单独的"幂等表"，所有幂等键都是业务表的唯一索引。
好处是不会出现"业务已成功但幂等记录丢了"的双写问题。

---

## 6. 一致性验收清单

- [x] 重复初始化上传 → 同一个 `fileId`
- [x] 重复完成上传 → 同一个版本号，配额只扣一次
- [x] 重复删除文件 → `changed=false`，不重复归还配额
- [x] 分片大小不符 → 拒绝并清理临时对象
- [x] 摘要不符 → 删除已合并对象并标记失败
- [x] 上传超时 → 任务失败 + 临时分片清理
- [x] 对象删除失败 → 进入 `deleting`，后台重试，有指标与告警
- [x] 服务重启 → `completing` 残留被回收
- [ ] 配额账目自动修复（目前只报告，需要人工确认）
