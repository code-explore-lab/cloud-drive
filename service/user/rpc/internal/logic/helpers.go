package logic

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

const (
	accessTokenTTL    = 15 * time.Minute
	refreshTokenTTL   = 30 * 24 * time.Hour
	defaultQuotaBytes = int64(10 << 30) // 10 GiB
)

// randomToken 生成 32 字节随机数并转成 hex，作为 refresh token 的明文返回给客户端。
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hashToken 计算 refresh token 的 SHA-256，数据库只存哈希，不存明文。
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
