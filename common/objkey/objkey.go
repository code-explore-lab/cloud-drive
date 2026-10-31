// Package objkey 统一生成对象存储的 Key。
//
// 上传分片 Key 由控制面（storage-rpc）和数据面（storage-api）共同使用，
// 放在同一个包里避免两边拼出不一样的路径。
package objkey

import (
	"fmt"
	"path"
	"strings"
	"unicode"
)

// Object 返回最终对象的 Key：按用户隔离 + 上传任务唯一。
func Object(userId uint64, uploadId, fileName string) string {
	return fmt.Sprintf("u/%d/%s%s", userId, uploadId, SafeExt(fileName))
}

// Part 返回上传分片的临时对象 Key。
func Part(uploadId string, partNumber int64) string {
	return fmt.Sprintf("parts/%s/%05d", uploadId, partNumber)
}

// SafeExt 只保留字母数字的扩展名，避免客户端传入路径分隔符等危险字符。
func SafeExt(fileName string) string {
	ext := path.Ext(fileName)
	if len(ext) > 16 {
		return ""
	}
	cleaned := strings.Map(func(r rune) rune {
		if r == '.' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, ext)
	if cleaned == "." {
		return ""
	}
	return cleaned
}
