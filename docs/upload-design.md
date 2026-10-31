# 上传设计

本文说明分片上传的完整设计：为什么这样切分片、如何保证幂等、中断后如何续传、
合并与校验怎么做、失败如何清理。

## 1. 为什么不用一个请求传完

单请求上传的问题不是"代码难写"，而是：

- 30MB 的请求体失败一次就得从头再来；
- 服务端要做"边收边写对象存储"，一旦中间断开，半截对象难以判断是否可复用；
- 无法在传输过程中校验分片完整性。

分片后，每一片是独立的、可重试的最小单元。

## 2. 分片参数

| 参数 | 取值 | 理由 |
| --- | --- | --- |
| `chunkSize` | 8 MiB | 高于 MinIO 服务端合并的 5 MiB 下限，又不会让分片元数据过多 |
| `totalChunks` | `ceil(size / chunkSize)`，最小 1 | 空文件也占一片，避免 0 分片的边界 |
| 上限 | 10000 片（约 78 GiB） | 与 MinIO 单对象分片上限一致 |

分片长度必须精确：

```text
part <  totalChunks → chunkSize 字节
part == totalChunks → size - (totalChunks-1)*chunkSize 字节
```

客户端多发一个字节会被拒绝（`400`），少发也会被拒绝。
"差不多对"的分片会让合并后的对象大小与元数据不一致，这是必须挡住的情况。

## 3. 一次上传的状态机

```text
                POST /v1/uploads
                       │
                       ▼
              ┌────────────────┐
              │   initiated    │  upload_tasks 一行，object_key 已确定
              └────────────────┘
                       │ 第 1 片落盘
                       ▼
              ┌────────────────┐   RecordPart 每片一次
              │   uploading    │◄──────── 重传同一片（幂等覆盖）
              └────────────────┘
                       │ ClaimForComplete（条件更新，只有一个赢家）
                       ▼
              ┌────────────────┐
              │  completing    │  合并 + 校验 + 写 storage_objects
              └────────────────┘
                   │        │
       成功        │        │ 失败
                   ▼        ▼
             completed    failed（last_error 记录原因，可重试完成）
```

`aborted`：客户端主动取消。
超时（24h）由后台任务改成 `failed` 并清理临时分片。

## 4. 幂等

三个层面都做了幂等，缺一层就会出现重复数据：

| 操作 | 幂等键 | 机制 |
| --- | --- | --- |
| 初始化上传 | `idempotencyKey` | `files(owner_id, request_id)` 唯一索引；命中则返回同一 `fileId` |
| 上传分片 | `(upload_id, part_number)` | `INSERT ... ON DUPLICATE KEY UPDATE` |
| 完成上传 | `upload_id` | `ClaimForComplete` 条件更新 + `file_versions(file_id, request_id)` 唯一 |

因此下面这些重复请求的结果都是**确定**的：

- 网络抖动后客户端重发 `POST /v1/uploads`（带同一个 key）→ 同一个上传任务；
- 客户端不确定第 3 片是否成功而重传 → 覆盖同名临时对象，进度不变；
- 客户端重发 `complete` → 返回同一个版本号，配额只扣一次。

## 5. 断点续传

```text
GET /v1/uploads/{uploadId}
→ { status, uploadedChunks, totalChunks, missingParts: [7,8,9], parts: [...] }
```

客户端只需要补传 `missingParts`。服务端不依赖客户端"记得"传到哪，
进度以数据库里的分片记录为准。

`status` 可能是 `expired`：任务已过期但还没被清理任务改写状态，
这时客户端应当重新发起上传，而不是继续传。

## 6. 分片如何合并

每个分片先写成临时对象：

```text
parts/{uploadId}/00001
parts/{uploadId}/00002
...
```

完成时由 MinIO **服务端**合并到最终对象：

- 只有 1 片：`CopyObject`（服务端复制，不经过网络传输内容）；
- 多片：`ComposeObject`（MinIO 内部按顺序拼接源对象）。

为什么不用 MinIO 原生 multipart upload？

| 方案 | 幂等性 | 状态管理 | 适用 |
| --- | --- | --- | --- |
| 原生 multipart（需保存 MinIO uploadID） | 重传分片需要 MinIO 侧配合 | 多一份外部状态，进程重启后可能丢 | 超大文件、直传对象存储 |
| 临时对象 + 服务端合并（本实现） | 重传就是覆盖同名对象，天然幂等 | 状态全在 MySQL | 本项目规模 |

代价是多一次重命名/拼接的开销，以及必须在合并后清理临时对象。
对本项目的量级，这个代价换来的是"状态只存在于一个地方"。

## 7. 完整性校验

- **分片级**：长度精确校验（服务端按 `expectedPartSize` 限流读取，多一个字节就 400）；
- **整体级**：客户端可在初始化时提供 `contentHash`（SHA-256）。
  完成时服务端会把合并后的对象完整读一遍计算摘要并比对，不一致就：
  删除已合并对象 → 任务置为 `failed` → 返回 `400`。

为什么不总是做整体校验：它需要把整个对象读一遍，对 10GB 文件是实打实的带宽与时间成本。
因此设计成"客户端提供才校验"（见 ADR-0005），并把结果记录在
`storage_objects.hash_verified`，便于统计有多少上传真正做过内容校验。

## 8. 失败与清理

| 场景 | 处理 |
| --- | --- |
| 分片上传失败 | 客户端重传；服务端临时对象被覆盖 |
| 分片元数据写入失败 | 服务端删除刚写的临时对象（尽力而为），返回错误 |
| 合并失败 | 任务 `failed` + `last_error`；重试完成会重新合并（覆盖写） |
| 进程在 `completing` 中崩溃 | 后台任务 15 分钟后把任务放回 `uploading` |
| 任务超时 | 后台任务标记 `failed`，删除临时分片 |
| 临时对象删不掉 | 后台任务按 `parts/` 前缀 + 48h 年龄清理 |
| 对象删除失败（删除文件） | `storage_objects.status='deleting'`，GC 重试并累加 `gc_attempts` |

关于"跨资源一致性"的完整讨论在 [consistency.md](consistency.md)。

## 9. 容量与资源控制

- 单分片请求体受 `MaxBytes` + `MaxChunkBytes` 双重限制；
- 上传时流式写 MinIO，内存里只有 32KiB 左右的缓冲；
- `upload_tasks` 有 `expires_at`，不会无限堆积；
- 并发分片数由客户端决定，服务端不做全局并发控制（这一点在性能测试里再评估）。

## 10. 客户端伪代码

```text
init = POST /v1/uploads {parentId, name, size, contentHash, idempotencyKey}
for part in init.totalChunks:
    if part in GET /v1/uploads/{id}.parts: continue      # 断点续传
    PUT storage-api/v1/uploads/{id}/parts/{part}
POST /v1/uploads/{id}/complete
```

重试策略：分片请求失败按指数退避重试 3-5 次；`complete` 失败先查进度再重试
（`complete` 是幂等的，直接重试也可以）。
