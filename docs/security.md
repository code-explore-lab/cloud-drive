# 安全设计

安全不是最后加上去的中间件，它体现在每个接口的默认行为里。本文按"攻击者会怎么做"来组织。

## 1. 身份认证

### 1.1 密码

- 使用 **scrypt**（N=32768, r=8, p=1, keyLen=32）加盐哈希，盐来自 `USER_SALT` 配置；
- 数据库只存哈希，接口从不返回 `password_hash`；
- 登录时用 `subtle.ConstantTimeCompare` 比较，避免时序侧信道；
- 用户名不存在与密码错误返回**同一个** `401 invalid username or password`，不泄露账号是否存在。

已知限制：`USER_SALT` 是全局盐而不是每用户随机盐。这是当前实现的简化，
升级路径是改为每用户随机盐并把盐存进 `users` 表（字段 `password_salt`），
迁移时对存量用户惰性重算。

### 1.2 Access Token

- HS256 JWT，15 分钟有效期，claims 含 `user_id`、`username`；
- 校验时只接受 HMAC 签名（防 `alg=none`）；
- **每次请求都查一次库**确认账号仍然存在且 `status=1`，
  这样禁用账号可以立刻生效，而不是等 token 过期。

代价是每次请求多一次数据库（走 go-zero 缓存）查询。对当前量级这是正确取舍。

### 1.3 Refresh Token

- 32 字节随机数，base64/hex 编码后返回给客户端；
- 库里只存 SHA-256；
- 30 天有效，**轮换使用**：刷新时旧 token 立即作废，新 token 生效；
- 已撤销的 token 再次使用会返回 `401` 并记录 `refresh token reused` 日志
  （这是被盗用的信号）。

### 1.4 退出登录

- 传 `refreshToken`：撤销该会话；
- 不传：撤销该用户全部会话。

access token 无法单独撤销（无状态），最多残留 15 分钟。这是显式取舍：
用短期 token + 服务端查库，换取无状态鉴权的横向扩展能力。

## 2. 授权

### 2.1 资源归属

所有文件/目录操作都在 `file.rpc` 里校验 `owner_id`：

```go
if f.OwnerId != uint64(in.UserId) { return NotFound }
```

**返回 404 而不是 403**：403 会告诉攻击者"这个 id 存在，只是不属于你"，
404 什么都不暴露。分享撤销同理（`RevokeShare` 对非本人分享返回 404）。

### 2.2 上传数据面

`PUT /v1/uploads/{uploadId}/parts/{n}` 需要同时满足：

1. 有效 JWT（userId 从 token 解析）；
2. `upload_tasks.user_id == userId`（由 `storage.rpc.GetUploadTask` 校验）；
3. 分片号在范围内、长度精确匹配。

只凭 `uploadId` 猜中也没用——第 2 条会拒绝。

### 2.3 对象 Key 不可由客户端指定

- 对象 Key 由服务端按 `u/{userId}/{uploadId}{ext}` 生成；
- 分片临时 Key 由服务端拼接，客户端传的 `objectPartId` 与期望值不符会被拒绝；
- 下载统一走 `GET /v1/files/{id}/content`，内部接口用 `X-Internal-Token` 保护。

因此**不存在**"构造一个 objectKey 就能读任意文件"的路径。

### 2.4 分享链接

- token 32 字节随机数，不可预测；库里只存 SHA-256（唯一索引查表）；
- 可选 bcrypt 密码（上限 72 字节，超长直接拒绝）；
- 可选有效期与最大访问次数；
- 访问计数与有效性校验在同一条 SQL 里完成，避免并发绕过次数限制；
- 文件删除时自动撤销该文件所有分享；
- 分享 token ≠ 对象 Key，撤销只改一个状态位。

## 3. 输入校验

| 位置 | 校验 |
| --- | --- |
| 用户名 | 3-32 字符（`validate` 标签） |
| 密码 | 8-72 字节 |
| 文件名/目录名 | 非空、≤255 字符、不含 `/` `\` NUL 与控制字符、不能是 `.`/`..` |
| 分片号 | 正整数且 ≤ `totalChunks` |
| 分片长度 | 必须精确等于期望值 |
| `idempotencyKey` | 8-64 位 `[A-Za-z0-9._-]` |
| 分片内容类型 | 服务端固定为 `application/octet-stream`，不信任客户端 |
| Range 头 | 只接受单段 `bytes=`，非法返回 416 |
| 请求体大小 | `MaxBytes`（1MiB JSON）+ `MaxChunkBytes`（16MiB 分片） |

## 4. 数据库

- 全部使用参数化查询（`?` 占位符），没有字符串拼接 SQL；
- go-zero 的 model 生成代码本身就使用参数化；
- 唯一索引承担并发正确性（注册、幂等、版本号），不依赖"先查再写"；
- 事务用于需要原子性的两条写入（注册、追加版本）。

## 5. 密钥与配置

- 密钥全部来自环境变量：`JWT_SECRET`、`USER_SALT`、`INTERNAL_TOKEN`、
  `MYSQL_ROOT_PASSWORD`、`MINIO_ROOT_USER/PASSWORD`；
- `deploy/config/*.yaml` 里只有 `${VAR}` 引用，不含真实密钥；
- `.env` 在 `.gitignore` 与 `.dockerignore` 里，不会进镜像或仓库；
- `.env.example` 里全是 `please-change-me` 占位值；
- 镜像以非 root 用户（uid 10001）运行，`readOnlyRootFilesystem` 在 K8s 中开启。

## 6. 日志

- 结构化 JSON 日志，包含 service、level、trace、业务字段；
- **不记录**密码、token、`Authorization` 头、对象内容；
- 分享密码优先从 `X-Share-Password` 头读取，
  但仍支持 query 参数（这样会进访问日志，文档里已标注不推荐）；
- 错误响应只返回稳定错误码与简短 message，不返回堆栈、SQL、内部地址。

## 7. 限流与滥用防护

- 登录/注册/刷新令牌：按 IP 令牌桶（默认 5 rps，突发 10），超限 429；
- 上传：单分片大小上限 + 单文件上限（默认 10 GiB）；
- go-zero 内置 `MaxConns`、`Shedding`（CPU 自适应降载）、`Breaker`（熔断）默认开启。

已知限制：限流是**进程内**的，多副本时总量会放大到副本数倍。
需要严格全局限流时应换成 Redis 令牌桶（`go-zero` 的 `limit.PeriodLimit`）。

## 8. 部署侧安全

- 只有 API 端口映射到宿主机；RPC 端口（9000-9002）只在 compose 网络内；
- MinIO 控制台端口只在开发环境映射；
- K8s 清单里 ConfigMap 放非敏感配置、Secret 放密钥，禁止把 Secret 提交进仓库；
- 内部接口依赖 `INTERNAL_TOKEN`；该值为空时 storage-api **拒绝所有内部请求**
  （避免"忘记配置 = 全开放"）。

## 9. 威胁模型速查

| 威胁 | 防护 |
| --- | --- |
| 暴力破解密码 | scrypt + 登录限流 + 统一错误信息 |
| token 伪造 | HS256 密钥 + 拒绝非 HMAC 算法 |
| 越权读取文件 | 每个操作校验 owner，越权返回 404 |
| 猜 objectKey 直接读对象 | 对象 Key 服务端生成，下载需经过鉴权代理 |
| 分享链接被枚举 | 32 字节随机 token，只存摘要 |
| 分享密码爆破 | bcrypt + 访问次数限制 |
| SQL 注入 | 参数化查询 |
| 路径穿越 | 文件名/扩展名清洗，Key 由服务端拼接 |
| 大请求打爆内存 | 流式处理 + MaxBytes + 分片上限 |
| 密钥泄露 | 环境变量注入 + 非 root 运行 + 仓库不含真实密钥 |

## 10. 待改进

- 每用户随机盐（当前是全局盐）。
- 全局限流（当前进程内）。
- 上传内容类型白名单（当前允许任意类型）。
- 审计日志表（谁在什么时候删了哪个文件）。
- 双因素认证。
