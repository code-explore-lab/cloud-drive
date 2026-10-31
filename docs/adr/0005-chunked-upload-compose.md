# ADR-0005：分片先写临时对象，完成时服务端合并

- 状态：已采纳
- 日期：2026-10

## 背景

MinIO 支持原生 multipart upload：`NewMultipartUpload` → `PutObjectPart` → `CompleteMultipartUpload`。
也可以自己把每片写成独立对象，完成时用 `ComposeObject` 拼接。

## 决策

采用**临时对象 + 服务端合并**：

- 分片写成 `parts/{uploadId}/{partNumber}`；
- 完成时单片用 `CopyObject`，多片用 `ComposeObject`；
- 合并后删除临时对象，失败则由后台按前缀清理。

## 理由

1. **幂等最自然**：重传同一片就是覆盖同名对象，不需要在 MinIO 侧维护 uploadId 与分片列表；
2. **状态只有一个来源**：所有进度都在 MySQL 的 `upload_parts` 表里，
   进程重启、服务扩缩容都不会丢状态；
3. **可观测**：`parts/` 前缀下的对象数量就是"正在上传的分片"，便于排查。

## 代价

- 合并阶段 MinIO 需要多读一遍源对象（服务端内部 IO，不占客户端带宽）；
- 必须满足 `ComposeObject` 的限制：非末尾分片 ≥ 5MiB（本项目分片固定 8MiB，天然满足）；
- 临时对象必须被可靠清理（合并后删除 + 48h 兜底扫描）。

## 替代方案

| 方案 | 结论 |
| --- | --- |
| MinIO 原生 multipart | 少一次合并 IO，但要在 MinIO 侧保存 uploadId；重传/恢复语义更绕 |
| 客户端直传预签名 URL | 带宽最优，但需要额外凭证下发与回调校验，列入后续演进 |

## 后果

- `chunkSize` 不能小于 5MiB（否则合并失败），当前固定 8MiB 并在 `InitUpload` 校验分片总数上限 10000；
- 端到端测试必须覆盖"分片长度不符"被拒绝的路径。
