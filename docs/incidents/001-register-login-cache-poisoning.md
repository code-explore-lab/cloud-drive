# 故障记录 001：注册成功但登录失败（缓存占位符污染）

- 发现时间：2026-10-31
- 发现方式：`go run ./scripts/e2e` 端到端验收脚本，第 1 组用例
- 影响：所有"注册后立即登录"的请求返回 `401 invalid username or password`
- 严重级别：高（新用户完全无法登录）

## 现象

```text
== 1. 注册与登录 ==
  ✓ 注册用户 alice_42851300
  ✗ 登录获取 access token
  ✗ 带 token 能读取自己的信息
```

但 `mysql` 里确实有这条用户记录，`password_hash` 与手工计算的一致；
手动执行 `redis-cli FLUSHALL` 之后，同一个账号立刻能登录成功。

## 定位过程

1. 确认数据库里有用户、密码哈希正确 → 排除注册写库问题。
2. 观察 user.rpc 日志：`/user.User/Login` 只用了 **6ms**，且 `cache hit_ratio: 55.6%`；
   而正常登录要走一次 scrypt（~100ms），说明登录**根本没查数据库**。
3. 查 Redis：存在 `cache:users:username:<name>` 键。
4. 逐个排查谁写了这个键：注册流程为了给出"用户名已存在"的友好错误，
   会先调用 `UsersModel.FindOneByUsername`；此时用户不存在，
   go-zero 的 `QueryRowIndexCtx` 会把"查不到"的结果写成**占位符**缓存。
5. 随后注册走的是事务里的**裸 SQL**（`session.ExecCtx`），绕过了 model，
   自然不会失效上面那个占位符。
6. 于是登录时 `FindOneByUsername` 命中占位符 → 返回 `ErrNotFound` →
   登录逻辑返回"用户名或密码错误"。

## 根因

两条写入路径不一致：

- 读路径走 `model`（带缓存）；
- 写路径走 `sqlx.Session`（无缓存失效）。

缓存里留下了"用户不存在"的负缓存，而数据库里已经有了这个用户。

## 修复

在 `UsersModel` 上增加显式的缓存失效方法，并在注册事务提交后调用：

```go
// service/user/model/usersmodel.go
func (m *customUsersModel) Invalidate(ctx context.Context, id uint64, username string,
	email sql.NullString) error {
	keys := []string{
		fmt.Sprintf("%s%v", cacheUsersIdPrefix, id),
		fmt.Sprintf("%s%v", cacheUsersUsernamePrefix, username),
	}
	if email.Valid && email.String != "" {
		keys = append(keys, fmt.Sprintf("%s%v", cacheUsersEmailPrefix, email.String))
	}
	return m.DelCacheCtx(ctx, keys...)
}
```

```go
// service/user/rpc/internal/logic/registerlogic.go（事务提交后）
if err := l.svcCtx.UsersModel.Invalidate(l.ctx, userId, username, email); err != nil {
	l.Errorf("invalidate user cache failed: %v", err)
}
```

## 验证

- 端到端脚本第 1 组用例全部通过（注册 → 登录 → 读自己信息 → 错误密码 401）；
- 回归方式：不清 Redis 的情况下连续注册、登录多个新用户均成功。

## 后续改进

1. 任何绕过 model 的写操作都必须显式失效缓存——这条规则已写进
   [troubleshooting.md](troubleshooting.md) 的"重新生成代码/新增逻辑"一节。
2. 长期方案是让注册复用 model 的 `Insert`（它自带缓存失效），
   但"插入用户 + 插入配额"需要跨两条语句的事务，而 go-zero 的 model 不支持
   在自定义事务里复用缓存失效逻辑。当前选择保留裸 SQL + 显式失效。
3. 可以在集成测试里加一条：注册后立即登录必须成功（e2e 已覆盖）。
