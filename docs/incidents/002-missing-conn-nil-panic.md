# 故障记录 002：注册接口 nil panic（ServiceContext 未注入 Conn）

- 发现时间：2026-10-31
- 发现方式：`go run ./scripts/e2e`，第 1 组用例（注册）
- 影响：注册接口 100% 返回 500；其他接口不受影响
- 严重级别：高

## 现象

```text
== 1. 注册与登录 ==
  ✗ 注册用户 alice_7921
  ✗ 重复注册应返回 409，实际 500
```

user.rpc 日志：

```text
panic: runtime error: invalid memory address or nil pointer dereference
  cloud-drive/service/user/rpc/internal/logic.(*RegisterLogic).Register(...)
    service/user/rpc/internal/logic/registerlogic.go:62
```

`registerlogic.go:62` 是：

```go
err := l.svcCtx.Conn.TransactCtx(l.ctx, func(ctx context.Context, session sqlx.Session) error {
```

## 根因

`svc.NewServiceContext` 创建了 MySQL 连接，但**没有把它赋给 `ServiceContext.Conn`**：

```go
func NewServiceContext(c config.Config) *ServiceContext {
	conn := sqlx.NewMysql(c.Mysql.DataSource)
	return &ServiceContext{
		Config:      c,
		UsersModel:  model.NewUsersModel(conn, c.CacheRedis),
		QuotasModel: model.NewUserQuotasModel(conn, c.CacheRedis),
		TokensModel: model.NewRefreshTokensModel(conn, c.CacheRedis),
		// 缺少 Conn
	}
}
```

`sqlx.NewMysql` 返回的是接口值；没有赋值时 `Conn` 是 nil 接口，
调用它上面的方法直接 panic。

## 为什么单元测试没发现

`registerlogic_test.go` 用 `newTestSvc(...)` 手工构造 `ServiceContext`，
并且给 `Conn` 赋了 sqlmock 连接。测试验证的是"Register 的逻辑"，
而"生产构造函数是否把依赖都接上"属于另一层——这正是端到端测试的价值。

## 修复

```go
return &ServiceContext{
	Config:      c,
	Conn:        conn,   // 补上
	UsersModel:  model.NewUsersModel(conn, c.CacheRedis),
	...
}
```

## 验证

- 注册接口 200，重复注册 409，错误密码登录 401；
- e2e 第 1、2 组用例全绿；
- `go vet ./...` 与 `go test ./...` 均通过。

## 后续改进

1. 每个 `ServiceContext` 都在构造函数里统一创建 `Conn`，并在注释里说明"给需要事务的逻辑用"；
2. 可以在 `main` 里加一次启动自检：`if svcCtx.Conn == nil { logx.Must(...) }`——
   但更好的做法是靠 e2e，因为这类"接线遗漏"无法靠类型系统发现（接口的零值可用）。
3. 端到端脚本应当作为合并前的必跑项（CI 里跑不了的话，本地发布前必须跑）。
