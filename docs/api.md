# HTTP API

统一约定：

- 请求/响应均为 JSON（分片上传与文件下载例外：请求体/响应体是原始字节）。
- 认证：`Authorization: Bearer <accessToken>`。未标注"公开"的接口都需要它。
- 成功响应直接返回资源对象；失败响应统一为：

```json
{
  "code": "invalid_argument",
  "message": "size must not be negative",
  "requestId": "8f3c1e2a9b7d4551"
}
```

- 每个响应都带 `X-Request-Id` 头（客户端也可以自己传，服务端会沿用），日志里用同一个 ID。
- 错误码到 HTTP 状态的映射：

| code | HTTP | 典型场景 |
| --- | --- | --- |
| `invalid_argument` | 400 | 参数缺失/非法、分片大小不符 |
| `unauthenticated` | 401 | 没有 token、token 过期、密码错误 |
| `permission_denied` | 403 | 访问他人资源、分享密码错误 |
| `not_found` | 404 | 文件/目录/上传任务不存在 |
| `already_exists` | 409 | 同目录重名 |
| `failed_precondition` | 409 | 上传未完成就完成、上传已取消 |
| `resource_exhausted` | 429 | 配额不足、触发限流 |
| `unavailable` | 503 | 下游服务或对象存储不可用 |
| `internal` | 500 | 其他 |

---

## 一、用户服务 `user-api` :8000

### POST /v1/users/register （公开）

```json
{ "username": "alice", "password": "Passw0rd!123", "email": "alice@example.com" }
```

约束：用户名 3-32 字符；密码至少 8 位（scrypt 上限 72 字节）。

`200` → `{ "userId": 1, "username": "alice" }`
`409` → 用户名或邮箱已存在
`429` → 触发限流（按 IP，默认 5 rps / 突发 10）

### POST /v1/users/login （公开）

```json
{ "username": "alice", "password": "Passw0rd!123" }
```

`200` → `{ "accessToken": "...", "refreshToken": "...", "expiresAt": 1767225600 }`

`expiresAt` 是 access token 的过期时间（Unix 秒），有效期 15 分钟。
用户名不存在与密码错误返回同样的 `401 unauthenticated`，避免枚举用户名。

### POST /v1/users/refresh （公开）

```json
{ "refreshToken": "..." }
```

`200` → 新的 `accessToken` + 新的 `refreshToken`（旧的立即失效，轮换使用）。

### POST /v1/users/logout （需登录）

```json
{ "refreshToken": "..." }
```

- 传 `refreshToken`：只撤销这一个会话；
- 不传：撤销该用户全部有效会话。

`200` → `{ "success": true, "revokedCount": 2 }`

### GET /v1/users/me

`200` → `{ "userId": 1, "username": "alice", "email": "...", "createdAt": 1767000000 }`

### GET /v1/users/me/quota

`200` → `{ "totalBytes": 10737418240, "usedBytes": 4096, "reservedBytes": 0 }`

`usedBytes + reservedBytes <= totalBytes` 恒成立。

---

## 二、文件服务 `file-api` :8001

### GET /v1/files?parentId=0&page=1&pageSize=20

列出某个目录的直接子项。目录全量返回、文件按页返回，目录在前。`pageSize` 上限 100。

```json
{
  "items": [
    { "fileId": 3, "parentId": 0, "name": "docs", "size": 0, "contentType": "",
      "isFolder": true, "status": "", "version": 0, "createdAt": 1767000000, "updatedAt": 1767000000 },
    { "fileId": 7, "parentId": 0, "name": "a.txt", "size": 1024,
      "contentType": "text/plain", "isFolder": false, "status": "available",
      "version": 1, "createdAt": 1767000100, "updatedAt": 1767000200 }
  ],
  "total": 2, "page": 1, "pageSize": 20
}
```

> 注意：`fileId` 与目录 id 共用一个整数空间但来自两张表，
> 对"文件"操作（如 `/v1/files/{id}/content`）传入目录 id 会得到 404。

### POST /v1/folders

```json
{ "parentId": 0, "name": "docs" }
```

`200` → `{ "folderId": 3 }`；同目录重名 → `409 already_exists`。

### DELETE /v1/folders/{folderId}

递归删除目录及其全部内容（`204`-less：返回 `200` 与释放的字节数）：

```json
{ "success": true, "releasedBytes": 2048 }
```

删除是不可恢复的（当前版本没有回收站）。历史版本的对象也会一起清理。

### GET /v1/files/{fileId}

`200` → `{ "file": { ...FileItem } }`；不存在或非本人 → `404`（不区分"不存在"和"不属于你"，避免泄露资源存在性）。

### PUT /v1/files/{fileId}/name

```json
{ "name": "new-name.txt" }
```

`200` → `true`（空 body）。同目录重名 → `409`。

### PUT /v1/files/{fileId}/parent

移动文件/目录，可同时改名：

```json
{ "targetParentId": 3, "name": "optional-new-name.txt" }
```

- 目标目录必须是自己的；
- 目录移动不能进入自己的子树（`400 invalid_argument`）；
- 目标目录下不能重名（`409`）。

### DELETE /v1/files/{fileId}

```json
{ "success": true, "releasedBytes": 1024 }
```

行为：软删文件（含历史版本）→ 撤销该文件所有分享 → 归还配额 → 删除对象。
**重复删除是幂等的**：返回 `200`、`success=true`、`releasedBytes=0`，不会重复归还配额。
对象删除失败不影响接口成功，由后台 GC 重试。

### GET /v1/files/{fileId}/content

下载文件内容，返回原始字节流。

- 支持 `Range: bytes=0-1023`，命中时返回 `206` 与 `Content-Range`；
- 响应头带 `Content-Disposition` 与 `Accept-Ranges: bytes`；
- 文件状态不是 `available` 时返回 `409`。

### GET /v1/files/{fileId}/versions

非本人文件返回 `404`（与 `GET /v1/files/{id}` 一致，不泄露资源存在性）。

```json
{ "items": [ { "version": 2, "size": 1024, "contentType": "text/plain",
               "contentHash": "…", "current": true, "createdAt": 1767000200 } ] }
```

### POST /v1/files/{fileId}/versions — 上传新版本

```json
{ "size": 2048, "contentType": "text/plain", "contentHash": "可选 sha256" }
```

`200` → 与 `POST /v1/uploads` 相同的结构（`uploadId`/`chunkSize`/`totalChunks`）。
上传完成后照旧调用 `POST /v1/uploads/{uploadId}/complete`，会产生 `version+1`。

### POST /v1/files/{fileId}/versions/{version}/restore

```json
{ "success": true, "version": 3 }
```

回滚是"追加一个新版本"，不会删除或改写历史版本。
对当前版本回滚是幂等的（不产生新版本）。

---

## 三、上传（file-api 编排 + storage-api 数据面）

### POST /v1/uploads

```json
{
  "parentId": 3,
  "name": "big.bin",
  "size": 104857600,
  "contentType": "application/octet-stream",
  "contentHash": "可选，sha256 hex",
  "idempotencyKey": "可选，8-64 位 [A-Za-z0-9._-]"
}
```

`200` →

```json
{ "uploadId": "3f1b…", "fileId": 9, "chunkSize": 8388608,
  "totalChunks": 13, "expiresAt": 1767260000 }
```

- 同一个 `idempotencyKey` 重复调用返回同一个 `fileId`/`uploadId`；
- 配额不足 → `429 resource_exhausted`；
- 单文件超过 `Upload.MaxFileBytes`（默认 10 GiB）→ `429`。

### PUT http://storage-api:8002/v1/uploads/{uploadId}/parts/{partNumber}

请求体是**原始字节**，长度必须**精确等于**该分片应有的长度：

| 分片 | 期望长度 |
| --- | --- |
| `part < totalChunks` | `chunkSize` |
| `part == totalChunks` | `size - (totalChunks-1) * chunkSize` |

`200` →

```json
{ "uploadId": "3f1b…", "partNumber": 1, "size": 8388608, "etag": "…",
  "contentHash": "…", "progress": { "uploadedChunks": 1, "uploadedBytes": 8388608, "totalChunks": 13 } }
```

- 长度不符 → `400`；
- 非本人任务 → `403`；
- 分片号越界 → `400 out_of_range`；
- 重复上传同一片安全（覆盖写）。

### GET /v1/uploads/{uploadId}

断点续传用。返回 `status`、`uploadedChunks`、`missingParts`（还缺哪些片）、
`parts`（已上传分片的 ETag）、`expiresAt`。

### POST /v1/uploads/{uploadId}/complete

```json
{ "fileId": 9, "version": 1, "status": "available" }
```

- 分片不齐 → `409 failed_precondition`，任务标记为失败；
- 摘要不匹配 → `400 invalid_argument`，已合并对象会被删除；
- 重复调用 → 返回同一个版本，不重复扣配额。

### DELETE /v1/uploads/{uploadId}

取消上传：标记任务 aborted、清理临时分片、归还预占配额、文件标记为 failed。
重复取消是幂等的。

---

## 四、分享

### POST /v1/shares （需登录）

```json
{ "fileId": 9, "password": "可选", "expiresAt": 1767300000, "maxAccessCount": 5 }
```

`200` →

```json
{ "shareId": 1, "token": "kQ2…", "url": "http://127.0.0.1:8001/v1/shares/kQ2…", "expiresAt": 1767300000 }
```

`token` 只在这里返回一次，数据库只存它的 SHA-256。

### GET /v1/shares?page=1&pageSize=20 （需登录）

列出自己创建的分享，包含已失效的（`active=false`）。

### DELETE /v1/shares/{shareId} （需登录）

撤销分享。重复撤销幂等。

### POST /v1/shares/{token}/access （公开）

```json
{ "password": "可选" }
```

`200` →

```json
{ "granted": true, "fileName": "a.txt", "size": 1024,
  "contentType": "text/plain", "downloadUrl": "http://127.0.0.1:8001/v1/shares/kQ2…/content" }
```

被拒绝时返回 `granted: false`（仍是 `200`），并附原因：
`share not found` / `share was revoked` / `share has expired` / `share access limit reached` /
`invalid share password` / `file is not available`。

每次成功调用都会占用一次访问额度。

### GET /v1/shares/{token}/content?password=… （公开）

下载分享文件。密码也可以通过 `X-Share-Password` 头传（推荐，避免密码进访问日志）。
支持 `Range`。

---

## 五、健康检查

| 接口 | 服务 | 语义 |
| --- | --- | --- |
| `GET /healthz` | 三个 API | 进程存活（不检查依赖） |
| `GET /readyz` | storage-api | 依赖就绪（MinIO 可用、bucket 存在） |
| `GET :6060/healthz` | 全部六个服务 | go-zero dev server 的健康检查 |
| `GET :6060/metrics` | 全部六个服务 | Prometheus 指标 |

---

## 六、错误响应示例

```http
POST /v1/uploads
HTTP/1.1 429 Too Many Requests
X-Request-Id: 8f3c1e2a9b7d4551
Content-Type: application/json

{"code":"resource_exhausted","message":"quota exceeded: insufficient quota","requestId":"8f3c1e2a9b7d4551"}
```

```http
GET /v1/files/999
HTTP/1.1 404 Not Found

{"code":"not_found","message":"file not found","requestId":"…"}
```
