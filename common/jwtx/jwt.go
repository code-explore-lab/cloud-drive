// Package jwtx 提供 JWT 的签发与解析，不依赖任何业务配置。
package jwtx

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

var ErrInvalidToken = errors.New("invalid token")

// Claims 是 token 中携带的业务信息。
type Claims struct {
	UserId   uint64 `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// Generate 使用 HS256 签发 access token，过期时间由调用方决定。
func Generate(secret string, userId uint64, username string, expiresAt time.Time) (string, error) {
	claims := Claims{
		UserId:   userId,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// Parse 校验签名和过期时间，并返回 token 中的业务信息。
func Parse(secret, tokenStr string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		// 只接受 HMAC 签名，防止 alg=none 之类的攻击
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
