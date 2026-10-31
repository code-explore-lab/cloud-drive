// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package config

import (
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	rest.RestConf
	// Limit 控制敏感接口（登录/注册/刷新令牌）的限流参数。
	// 令牌签发与校验都在 user.rpc 里完成，user-api 不持有 JWT 密钥。
	Limit struct {
		// RPS 是每 IP 每秒允许的请求数，默认 5。
		RPS float64 `json:",default=5"`
		// Burst 是允许的瞬时突发量，默认 10。
		Burst int `json:",default=10"`
	}
	UserRpc zrpc.RpcClientConf
}
