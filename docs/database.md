# 数据库设计

三份 schema 分别属于三个服务，逻辑上互相独立：

| 文件 | 属主 | 表 |
| --- | --- | --- |
| `service/user/model/user.sql` | user.rpc | `users`、`user_quotas`、`refresh_tokens` |
| `service/file/model/file.sql` | file.rpc | `folders`、`files`、`file_versions`、`shares` |
| `service/storage/model/storage.sql` | storage.rpc | `upload_tasks`、`upload_parts`、`storage_objects` |

开发阶段共用一个 MySQL 实例（库名 `cloud-drive`），但没有任何跨服务的 JOIN 或外键。
这条约束是刻意的：以后拆库时不需要改查询。

时间字段统一存本地时区 `Asia/Shanghai`（DSN 里 `loc=Asia%2FShanghai`），
业务代码里统一用 `time.Now()`，不混用 UTC 与本地时间。

---

## 1. 用户与配额

### users

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGINT UNSIGNED PK | 用户 ID |
| username | VARCHAR(32) | 唯一；登录名 |
| email | VARCHAR(255) NULL | 唯一；可为空 |
| password_hash | VARCHAR(255) | scrypt 哈希（含随机盐由 `USER_SALT` 提供） |
| status | TINYINT | 1=正常，0=禁用 |
| created_at / updated_at | DATETIME | |
| deleted_at | DATETIME NULL | 软删标记 |

唯一索引 `uk_users_username`、`uk_users_email` 是并发注册正确性的**唯一**保证；
代码里的"先查再插"只是为了返回更友好的错误。

### user_quotas

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| user_id | BIGINT UNSIGNED | 唯一 |
| total_bytes | BIGINT UNSIGNED | 默认 10 GiB |
| used_bytes | BIGINT UNSIGNED | 已用 |
| reserved_bytes | BIGINT UNSIGNED | 已预占（上传中） |

三个数字的关系：`used_bytes + reserved_bytes <= total_bytes`。
这条不变式由带条件的原子 UPDATE 维持，不是由应用层判断：

```sql
-- 预占：只有额度足够时才更新成功
update user_quotas set reserved_bytes = reserved_bytes + ?
 where user_id = ? and used_bytes + reserved_bytes + ? <= total_bytes;

-- 提交：预占转已用
update user_quotas set used_bytes = used_bytes + ?, reserved_bytes = reserved_bytes - ?
 where user_id = ? and reserved_bytes >= ?;

-- 归还已用（删除文件）
update user_quotas set used_bytes = used_bytes - ? where user_id = ? and used_bytes >= ?;
```

并发上传时不会超卖，因为判断和更新在同一条 SQL 里。

### refresh_tokens

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| user_id | BIGINT UNSIGNED | |
| token_hash | VARCHAR(255) | 只存 SHA-256，不存明文 |
| expires_at | DATETIME | 30 天 |
| revoked_at | DATETIME NULL | 非空表示已撤销 |

退出登录 = 置 `revoked_at`；"退出所有设备" = 批量置。
access token 是无状态 JWT（15 分钟），不能被逐个撤销，这是显式取舍（见 security.md）。

---

## 2. 文件与目录

### folders

| 字段 | 说明 |
| --- | --- |
| owner_id | 属主 |
| parent_id | 父目录；0 = 根目录 |
| name | 目录名，同一父目录下唯一（应用层校验） |
| deleted_at | 软删 |

索引 `(owner_id, parent_id)` 覆盖「列目录」的主查询。

### files

| 字段 | 说明 |
| --- | --- |
| owner_id / parent_id | 位置与属主 |
| name | 同一父目录下唯一（应用层校验） |
| size | **当前版本**的大小 |
| content_type / content_hash / object_key | **当前版本**的内容指针 |
| version | 当前版本号；**0 表示还没有内容** |
| status | `uploading` → `available`；失败为 `failed`；删除为 `deleting` |
| request_id | 幂等键（上传任务 ID），`(owner_id, request_id)` 唯一 |
| deleted_at | 软删 |

状态机：

```text
（创建）──► uploading ──AddVersion──► available ──DeleteFile──► deleting(+deleted_at)
                │
                └── 失败/取消 ──► failed
```

关键点：

- `version = 0` 是"还没上传完"的显式表示，避免把 0 字节文件与"没内容"混淆。
- `deleted_at` 非空的行永久保留（审计与幂等），业务查询一律过滤。

### file_versions

| 字段 | 说明 |
| --- | --- |
| file_id / version | 唯一 |
| size / content_type / content_hash / object_key | 该版本的不可变内容 |
| request_id | 幂等键，`(file_id, request_id)` 唯一 |

**每个版本一行，包括当前版本**。所以：

- 首次上传：`version` 0 → 1，插入 1 行；
- 覆盖上传：插入 `version+1` 行并更新 `files` 指针；
- 回滚：把目标版本的内容作为新的一行插入（版本号 = max+1），历史不被改写。

`AppendVersion` 在一个事务里 `SELECT ... FOR UPDATE` 锁住 `files` 行，
因此并发上传同一文件不会得到重复版本号。

### shares

| 字段 | 说明 |
| --- | --- |
| file_id / owner_id | 被分享的文件与属主 |
| token_hash | 分享 token 的 SHA-256（唯一） |
| password_hash | bcrypt；空表示无密码 |
| expires_at | 可空 |
| max_access_count / access_count | 可空 / 已访问次数 |
| status | 1=有效，0=已撤销 |
| last_access_at | 最近一次成功访问 |

`access_count` 的自增与有效性校验在同一条 SQL 里完成：

```sql
update shares set access_count = access_count + 1, last_access_at = ?
 where id = ? and status = 1
   and (expires_at is null or expires_at > ?)
   and (max_access_count is null or access_count < max_access_count);
```

影响行数为 0 就表示"这次访问不被允许"，不存在两次并发请求同时通过次数检查的窗口。

---

## 3. 上传与对象

### upload_tasks

一次上传任务一行，是上传状态机的唯一来源。

| 字段 | 说明 |
| --- | --- |
| upload_id | 唯一；也用作幂等键 |
| user_id / file_id / file_name | 归属 |
| total_size / content_type / expected_hash | 期望值 |
| object_key | 最终对象 Key，**创建时就确定** |
| chunk_size / total_chunks | 分片参数（8MiB） |
| uploaded_chunks / uploaded_bytes | 进度（从分片表汇总） |
| status | `initiated` → `uploading` → `completing` → `completed`；或 `failed` / `aborted` |
| last_error | 失败原因（截断到 512 字节） |
| expires_at | 24 小时未完成即过期 |

状态机：

```text
initiated ──RecordPart──► uploading ──ClaimForComplete──► completing ──► completed
    │                        │                                │
    │                        │                                └─ 合并失败 → failed
    └──────────── AbortUpload ──► aborted       超时清理 → failed（回收为 uploading）
```

`ClaimForComplete` 用条件更新实现"只有一个请求能执行合并"：

```sql
update upload_tasks set status = 'completing'
 where upload_id = ? and status in ('initiated','uploading');
```

### upload_parts

| 字段 | 说明 |
| --- | --- |
| upload_id + part_number | 唯一 |
| size / content_hash / etag | 该分片的信息 |
| object_part_id | 临时对象 Key（`parts/{uploadId}/{n}`） |

写入用 `INSERT ... ON DUPLICATE KEY UPDATE`，所以**重传同一片天然幂等**。

### storage_objects

| 字段 | 说明 |
| --- | --- |
| object_key | 唯一（最终对象） |
| backend / bucket_name | 存储后端与桶 |
| size / content_type / content_hash / hash_verified | 内容信息 |
| status | `available` / `deleting` / `deleted` |
| gc_attempts / last_error | 清理重试的观测信息 |

`status != available` 的对象对下载方一律表现为"不存在"，避免读到半成品。

---

## 4. 索引设计

| 表 | 索引 | 支撑的查询 |
| --- | --- | --- |
| files | `(owner_id, parent_id)` | 列目录 |
| files | `(owner_id, status)` | 统计未完成上传 |
| files | `(owner_id, request_id)` UK | 上传幂等 |
| file_versions | `(file_id, version)` UK | 版本列表/回滚 |
| file_versions | `(file_id, request_id)` UK | 完成上传幂等 |
| shares | `token_hash` UK | 分享访问 |
| shares | `(file_id, owner_id)` | 删文件时撤销分享 |
| upload_tasks | `upload_id` UK | 任务查询 |
| upload_tasks | `(expires_at)` | 过期清理扫描 |
| upload_parts | `(upload_id, part_number)` UK | 分片幂等 |
| storage_objects | `object_key` UK | 元数据查询 |
| storage_objects | `(status)` | GC 扫描 |

没有为"模糊搜索文件名"建索引：当前接口不支持模糊搜索，等有真实需求再加。

---

## 5. 变更管理

当前阶段使用「幂等 DDL + 版本化文件」：

- 每份 `*.sql` 都用 `CREATE TABLE IF NOT EXISTS`，重复执行安全；
- 结构变更时新增 `service/<svc>/model/migrations/000N-*.sql`，只追加，不修改已发布的迁移；
- 重建环境：`docker compose down -v` 后重新初始化（会清空数据）。

生产环境应换成正式的迁移工具（如 golang-migrate），并在 CI 里校验迁移可重放。
