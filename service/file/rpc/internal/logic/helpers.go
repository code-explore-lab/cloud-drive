package logic

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"

	"cloud-drive/service/file/rpc/internal/svc"

	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	maxNameLength     = 255
	maxSharePasswordL = 72 // bcrypt 的输入上限
)

var errInvalidName = errors.New("invalid name")

// validateName 统一校验文件名/目录名：非空、长度受限、不含路径分隔符与控制字符。
func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errInvalidName
	}
	if name == "." || name == ".." {
		return "", errInvalidName
	}
	if utf8.RuneCountInString(name) > maxNameLength {
		return "", errInvalidName
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return "", errInvalidName
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", errInvalidName
		}
	}
	return name, nil
}

// newShareToken 生成不可预测的分享标识（32 字节随机数的 URL-safe base64）。
func newShareToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashShareToken 只把 token 的摘要写库，数据库泄露时也无法反推可用链接。
func hashShareToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// hashSharePassword 用 bcrypt 处理分享密码。
func hashSharePassword(password string) (string, error) {
	if password == "" {
		return "", nil
	}
	if len(password) > maxSharePasswordL {
		return "", errors.New("password too long")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// verifySharePassword 校验分享密码；hash 为空表示该分享不需要密码。
func verifySharePassword(hash, password string) bool {
	if hash == "" {
		return true
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// ensureNameAvailable 检查同一目录下是否已有同名文件或目录。
// 同一目录内重名会让列表和移动语义变得含糊，因此在写入前就拒绝。
func ensureNameAvailable(ctx context.Context, svcCtx *svc.ServiceContext, ownerId, parentId uint64,
	name string, excludeFileId uint64) error {
	var fileCount int64
	err := svcCtx.Conn.QueryRowCtx(ctx, &fileCount,
		"select count(*) from `files` where `owner_id` = ? and `parent_id` = ? and `name` = ? "+
			"and `deleted_at` is null and `id` <> ?",
		ownerId, parentId, name, excludeFileId)
	if err != nil {
		return status.Error(codes.Internal, "internal error")
	}
	if fileCount > 0 {
		return status.Errorf(codes.AlreadyExists, "a file named %q already exists", name)
	}

	var folderCount int64
	err = svcCtx.Conn.QueryRowCtx(ctx, &folderCount,
		"select count(*) from `folders` where `owner_id` = ? and `parent_id` = ? and `name` = ? "+
			"and `deleted_at` is null",
		ownerId, parentId, name)
	if err != nil {
		return status.Error(codes.Internal, "internal error")
	}
	if folderCount > 0 {
		return status.Errorf(codes.AlreadyExists, "a folder named %q already exists", name)
	}
	return nil
}
