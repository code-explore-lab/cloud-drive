// Command e2e 是对 CloudDrive 的端到端验收脚本。
//
// 它只用标准库，因此可以在任何装了 Go 的机器上跑：
//
//	go run ./scripts/e2e
//	USER_API=http://127.0.0.1:8000 go run ./scripts/e2e
//
// 覆盖的验收点来自 target.md 第十节：
//  1. 注册、登录、鉴权
//  2. 建目录、上传、下载并校验内容一致
//  3. 幂等：重复初始化上传、重复完成上传
//  4. Range 下载
//  5. 版本：列表与回滚
//  6. 分享：密码、访问次数、撤销
//  7. 跨用户越权访问被拒绝
//  8. 删除文件并归还配额
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type env struct {
	userAPI    string
	fileAPI    string
	storageAPI string
}

type tester struct {
	env    env
	client *http.Client
	failed int
}

func main() {
	e := env{
		userAPI:    getenv("USER_API", "http://127.0.0.1:8000"),
		fileAPI:    getenv("FILE_API", "http://127.0.0.1:8001"),
		storageAPI: getenv("STORAGE_API", "http://127.0.0.1:8002"),
	}
	t := &tester{
		env: e,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
	t.run()
	if t.failed > 0 {
		fmt.Printf("\n%d 个检查失败 ❌\n", t.failed)
		os.Exit(1)
	}
	fmt.Println("\n全部通过 ✅")
}

func (t *tester) run() {
	suffix := fmt.Sprint(time.Now().UnixNano())
	alice := "alice_" + suffix[len(suffix)-8:]
	bob := "bob_" + suffix[len(suffix)-8:]
	const password = "Passw0rd!123"

	t.section("0. 健康检查")
	for name, base := range map[string]string{"user-api": t.env.userAPI, "file-api": t.env.fileAPI, "storage-api": t.env.storageAPI} {
		status, _ := t.request(http.MethodGet, base+"/healthz", "", nil, nil)
		t.must(status == http.StatusOK, name+" /healthz 返回 "+fmt.Sprint(status))
	}

	t.section("1. 注册与登录")
	status, _ := t.requestJSON(http.MethodPost, t.env.userAPI+"/v1/users/register", "",
		map[string]any{"username": alice, "password": password, "email": alice + "@example.com"}, nil)
	t.must(status == http.StatusOK, "注册用户 "+alice)

	status, _ = t.requestJSON(http.MethodPost, t.env.userAPI+"/v1/users/register", "",
		map[string]any{"username": alice, "password": password}, nil)
	t.must(status == http.StatusConflict, "重复注册应返回 409，实际 "+fmt.Sprint(status))

	var login struct {
		AccessToken string `json:"accessToken"`
	}
	status, _ = t.requestJSON(http.MethodPost, t.env.userAPI+"/v1/users/login", "",
		map[string]any{"username": alice, "password": password}, &login)
	t.must(status == http.StatusOK && login.AccessToken != "", "登录获取 access token")
	token := login.AccessToken

	status, _ = t.request(http.MethodGet, t.env.userAPI+"/v1/users/me", "", nil, nil)
	t.must(status == http.StatusUnauthorized, "未带 token 访问 /v1/users/me 应 401，实际 "+fmt.Sprint(status))

	status, _ = t.request(http.MethodGet, t.env.userAPI+"/v1/users/me", token, nil, nil)
	t.must(status == http.StatusOK, "带 token 能读取自己的信息")

	status, _ = t.requestJSON(http.MethodPost, t.env.userAPI+"/v1/users/login", "",
		map[string]any{"username": alice, "password": "wrong-password"}, nil)
	t.must(status == http.StatusUnauthorized, "错误密码应 401，实际 "+fmt.Sprint(status))

	t.section("2. 目录")
	var folder struct {
		FolderID int64 `json:"folderId"`
	}
	status, _ = t.requestJSON(http.MethodPost, t.env.fileAPI+"/v1/folders", token,
		map[string]any{"parentId": 0, "name": "docs"}, &folder)
	t.must(status == http.StatusOK && folder.FolderID > 0, "创建目录")

	status, _ = t.requestJSON(http.MethodPost, t.env.fileAPI+"/v1/folders", token,
		map[string]any{"parentId": 0, "name": "docs"}, nil)
	t.must(status == http.StatusConflict, "同目录重名应返回 409，实际 "+fmt.Sprint(status))

	t.section("3. 上传（幂等 + 分片）")
	payload := bytes.Repeat([]byte("cloud-drive e2e payload\n"), 4096)
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	idempotencyKey := "e2e-" + suffix

	var initResp struct {
		UploadID    string `json:"uploadId"`
		FileID      int64  `json:"fileId"`
		ChunkSize   int64  `json:"chunkSize"`
		TotalChunks int64  `json:"totalChunks"`
	}
	initBody := map[string]any{
		"parentId": folder.FolderID, "name": "payload.txt", "size": len(payload),
		"contentType": "text/plain", "contentHash": hash, "idempotencyKey": idempotencyKey,
	}
	status, _ = t.requestJSON(http.MethodPost, t.env.fileAPI+"/v1/uploads", token, initBody, &initResp)
	t.must(status == http.StatusOK && initResp.UploadID != "", "初始化上传")
	t.must(initResp.TotalChunks >= 1, "分片数至少为 1")

	var initAgain struct {
		FileID int64 `json:"fileId"`
	}
	status, _ = t.requestJSON(http.MethodPost, t.env.fileAPI+"/v1/uploads", token, initBody, &initAgain)
	t.must(status == http.StatusOK && initAgain.FileID == initResp.FileID,
		"同一幂等键返回同一个 fileId")

	// 分片上传：按 chunkSize 切分，逐片 PUT 到 storage-api。
	parts := splitParts(payload, initResp.ChunkSize)
	for i, part := range parts {
		partURL := fmt.Sprintf("%s/v1/uploads/%s/parts/%d", t.env.storageAPI, initResp.UploadID, i+1)
		status, body := t.request(http.MethodPut, partURL, token, bytes.NewReader(part), map[string]string{
			"Content-Type": "application/octet-stream",
		})
		t.must(status == http.StatusOK, fmt.Sprintf("上传分片 %d（%s）", i+1, truncate(string(body), 120)))
	}

	var progress struct {
		UploadedChunks int64   `json:"uploadedChunks"`
		MissingParts   []int64 `json:"missingParts"`
		Status         string  `json:"status"`
	}
	status, _ = t.requestJSON(http.MethodGet, t.env.fileAPI+"/v1/uploads/"+initResp.UploadID, token, nil, &progress)
	t.must(status == http.StatusOK && progress.UploadedChunks == int64(len(parts)),
		fmt.Sprintf("进度查询 uploadedChunks=%d", progress.UploadedChunks))
	t.must(len(progress.MissingParts) == 0, "没有缺失分片")

	// 分片大小不匹配必须被拒绝（这里故意少发一个字节）。
	badURL := fmt.Sprintf("%s/v1/uploads/%s/parts/%d", t.env.storageAPI, initResp.UploadID, 1)
	status, _ = t.request(http.MethodPut, badURL, token, bytes.NewReader(parts[0][:len(parts[0])-1]),
		map[string]string{"Content-Type": "application/octet-stream"})
	t.must(status == http.StatusBadRequest, "分片大小不符应返回 400，实际 "+fmt.Sprint(status))

	quotaBefore := t.quota(token)

	var complete struct {
		FileID  int64 `json:"fileId"`
		Version int64 `json:"version"`
	}
	status, _ = t.requestJSON(http.MethodPost,
		t.env.fileAPI+"/v1/uploads/"+initResp.UploadID+"/complete", token, nil, &complete)
	t.must(status == http.StatusOK && complete.Version == 1, "完成上传且版本号为 1")

	quotaAfter := t.quota(token)
	t.must(quotaAfter.UsedBytes == quotaBefore.UsedBytes+int64(len(payload)),
		fmt.Sprintf("配额已用额度增加了文件大小（%d -> %d）", quotaBefore.UsedBytes, quotaAfter.UsedBytes))

	var completeAgain struct {
		Version int64 `json:"version"`
	}
	status, _ = t.requestJSON(http.MethodPost,
		t.env.fileAPI+"/v1/uploads/"+initResp.UploadID+"/complete", token, nil, &completeAgain)
	t.must(status == http.StatusOK && completeAgain.Version == complete.Version,
		"重复完成返回同一个版本")
	quotaAgain := t.quota(token)
	t.must(quotaAgain.UsedBytes == quotaAfter.UsedBytes, "重复完成没有重复扣配额")

	t.section("4. 下载与内容校验")
	status, downloaded := t.request(http.MethodGet,
		fmt.Sprintf("%s/v1/files/%d/content", t.env.fileAPI, initResp.FileID), token, nil, nil)
	gotSum := sha256.Sum256(downloaded)
	t.must(status == http.StatusOK && hex.EncodeToString(gotSum[:]) == hash, "下载内容与上传一致")

	status, ranged := t.request(http.MethodGet,
		fmt.Sprintf("%s/v1/files/%d/content", t.env.fileAPI, initResp.FileID), token, nil,
		map[string]string{"Range": "bytes=0-4"})
	t.must(status == http.StatusPartialContent && len(ranged) == 5,
		fmt.Sprintf("Range 请求返回 206 且长度为 5，实际 %d/%d", status, len(ranged)))

	t.section("5. 版本")
	var versions struct {
		Items []struct {
			Version int64 `json:"version"`
			Current bool  `json:"current"`
		} `json:"items"`
	}
	status, _ = t.requestJSON(http.MethodGet,
		fmt.Sprintf("%s/v1/files/%d/versions", t.env.fileAPI, initResp.FileID), token, nil, &versions)
	t.must(status == http.StatusOK && len(versions.Items) == 1 && versions.Items[0].Current,
		"版本列表包含 1 个当前版本")

	var restore struct {
		Version int64 `json:"version"`
	}
	status, _ = t.requestJSON(http.MethodPost,
		fmt.Sprintf("%s/v1/files/%d/versions/1/restore", t.env.fileAPI, initResp.FileID), token, nil, &restore)
	t.must(status == http.StatusOK && restore.Version == 1, "回滚到当前版本不产生新版本")

	t.section("6. 分享")
	var share struct {
		ShareID int64  `json:"shareId"`
		Token   string `json:"token"`
	}
	status, _ = t.requestJSON(http.MethodPost, t.env.fileAPI+"/v1/shares", token,
		map[string]any{"fileId": initResp.FileID, "password": "share-pass", "maxAccessCount": 3}, &share)
	t.must(status == http.StatusOK && share.Token != "", "创建带密码的分享")

	var denied struct {
		Granted bool `json:"granted"`
	}
	status, _ = t.requestJSON(http.MethodPost, t.env.fileAPI+"/v1/shares/"+share.Token+"/access", "",
		map[string]any{"password": "wrong"}, &denied)
	t.must(status == http.StatusOK && !denied.Granted, "错误密码被拒绝")

	var granted struct {
		Granted     bool   `json:"granted"`
		DownloadURL string `json:"downloadUrl"`
	}
	status, _ = t.requestJSON(http.MethodPost, t.env.fileAPI+"/v1/shares/"+share.Token+"/access", "",
		map[string]any{"password": "share-pass"}, &granted)
	t.must(status == http.StatusOK && granted.Granted, "正确密码可访问")
	t.must(strings.Contains(granted.DownloadURL, share.Token), "下载地址包含分享 token")

	status, shared := t.request(http.MethodGet,
		t.env.fileAPI+"/v1/shares/"+share.Token+"/content?password=share-pass", "", nil, nil)
	sharedSum := sha256.Sum256(shared)
	t.must(status == http.StatusOK && hex.EncodeToString(sharedSum[:]) == hash, "分享下载内容一致")

	status, _ = t.request(http.MethodGet,
		t.env.fileAPI+"/v1/shares/"+share.Token+"/content?password=share-pass", "", nil, nil)
	t.must(status == http.StatusOK, "第三次访问仍在次数上限内")

	status, _ = t.request(http.MethodGet,
		t.env.fileAPI+"/v1/shares/"+share.Token+"/content?password=share-pass", "", nil, nil)
	t.must(status == http.StatusForbidden, "超过访问次数应 403，实际 "+fmt.Sprint(status))

	status, _ = t.request(http.MethodDelete,
		fmt.Sprintf("%s/v1/shares/%d", t.env.fileAPI, share.ShareID), token, nil, nil)
	t.must(status == http.StatusOK, "撤销分享")

	var revoked struct {
		Granted bool   `json:"granted"`
		Reason  string `json:"reason"`
	}
	status, _ = t.requestJSON(http.MethodPost, t.env.fileAPI+"/v1/shares/"+share.Token+"/access", "",
		map[string]any{"password": "share-pass"}, &revoked)
	t.must(status == http.StatusOK && !revoked.Granted, "撤销后分享不可用")

	t.section("7. 越权访问")
	status, _ = t.requestJSON(http.MethodPost, t.env.userAPI+"/v1/users/register", "",
		map[string]any{"username": bob, "password": password}, nil)
	t.must(status == http.StatusOK, "注册第二个用户")

	var bobLogin struct {
		AccessToken string `json:"accessToken"`
	}
	status, _ = t.requestJSON(http.MethodPost, t.env.userAPI+"/v1/users/login", "",
		map[string]any{"username": bob, "password": password}, &bobLogin)
	t.must(status == http.StatusOK, "第二个用户登录")

	status, _ = t.request(http.MethodGet,
		fmt.Sprintf("%s/v1/files/%d", t.env.fileAPI, initResp.FileID), bobLogin.AccessToken, nil, nil)
	t.must(status == http.StatusNotFound, "读取他人文件应 404，实际 "+fmt.Sprint(status))

	status, _ = t.request(http.MethodGet,
		fmt.Sprintf("%s/v1/files/%d/content", t.env.fileAPI, initResp.FileID), bobLogin.AccessToken, nil, nil)
	t.must(status == http.StatusNotFound, "下载他人文件应 404，实际 "+fmt.Sprint(status))

	status, _ = t.request(http.MethodGet,
		fmt.Sprintf("%s/v1/files/%d/versions", t.env.fileAPI, initResp.FileID), bobLogin.AccessToken, nil, nil)
	t.must(status == http.StatusNotFound, "查看他人版本应 404，实际 "+fmt.Sprint(status))

	// 他人不能用第一个用户的 uploadId 去写分片。
	status, _ = t.request(http.MethodPut,
		fmt.Sprintf("%s/v1/uploads/%s/parts/1", t.env.storageAPI, initResp.UploadID),
		bobLogin.AccessToken, bytes.NewReader([]byte("hijack")),
		map[string]string{"Content-Type": "application/octet-stream"})
	t.must(status == http.StatusForbidden, "他人写分片应 403，实际 "+fmt.Sprint(status))

	t.section("8. 删除与配额归还")
	beforeDelete := t.quota(token)
	status, _ = t.request(http.MethodDelete,
		fmt.Sprintf("%s/v1/files/%d", t.env.fileAPI, initResp.FileID), token, nil, nil)
	t.must(status == http.StatusOK, "删除文件")

	afterDelete := t.quota(token)
	t.must(afterDelete.UsedBytes < beforeDelete.UsedBytes,
		fmt.Sprintf("删除后已用额度减少（%d -> %d）", beforeDelete.UsedBytes, afterDelete.UsedBytes))

	status, _ = t.request(http.MethodGet,
		fmt.Sprintf("%s/v1/files/%d", t.env.fileAPI, initResp.FileID), token, nil, nil)
	t.must(status == http.StatusNotFound, "删除后查询应 404")

	// 重复删除是幂等的：返回 200 且 releasedBytes=0，不会重复归还配额。
	var repeatDelete struct {
		Success       bool  `json:"success"`
		ReleasedBytes int64 `json:"releasedBytes"`
	}
	status, _ = t.requestJSON(http.MethodDelete,
		fmt.Sprintf("%s/v1/files/%d", t.env.fileAPI, initResp.FileID), token, nil, &repeatDelete)
	t.must(status == http.StatusOK && repeatDelete.Success && repeatDelete.ReleasedBytes == 0,
		"重复删除幂等：200 且不再归还配额")

	t.section("9. 目录递归删除")
	status, _ = t.request(http.MethodDelete,
		fmt.Sprintf("%s/v1/folders/%d", t.env.fileAPI, folder.FolderID), token, nil, nil)
	t.must(status == http.StatusOK, "递归删除目录")
}

type quotaResp struct {
	TotalBytes    int64 `json:"totalBytes"`
	UsedBytes     int64 `json:"usedBytes"`
	ReservedBytes int64 `json:"reservedBytes"`
}

func (t *tester) quota(token string) quotaResp {
	var q quotaResp
	status, body := t.requestJSON(http.MethodGet, t.env.userAPI+"/v1/users/me/quota", token, nil, &q)
	if status != http.StatusOK {
		t.fatalf("读取配额失败：%d %s", status, truncate(string(body), 200))
	}
	return q
}

func (t *tester) section(title string) {
	fmt.Printf("\n== %s ==\n", title)
}

func (t *tester) must(ok bool, message string) {
	if ok {
		fmt.Printf("  \033[32m✓\033[0m %s\n", message)
		return
	}
	t.fatalf("%s", message)
}

func (t *tester) fatalf(format string, args ...any) {
	t.failed++
	fmt.Printf("  \033[31m✗\033[0m %s\n", fmt.Sprintf(format, args...))
}

func (t *tester) requestJSON(method, url, token string, body any, out any) (int, []byte) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.fatalf("序列化请求体失败：%v", err)
			return 0, nil
		}
		reader = bytes.NewReader(raw)
	}
	status, resp := t.request(method, url, token, reader,
		map[string]string{"Content-Type": "application/json"})
	if out != nil && len(resp) > 0 {
		if err := json.Unmarshal(resp, out); err != nil {
			t.fatalf("解析响应失败：%v body=%s", err, truncate(string(resp), 200))
		}
	}
	return status, resp
}

func (t *tester) request(method, url, token string, body io.Reader,
	headers map[string]string) (int, []byte) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.fatalf("构造请求失败：%v", err)
		return 0, nil
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" && body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}

	resp, err := t.client.Do(req)
	if err != nil {
		t.fatalf("请求 %s %s 失败：%v", method, url, err)
		return 0, nil
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.fatalf("读取响应失败：%v", err)
		return resp.StatusCode, nil
	}
	return resp.StatusCode, raw
}

func splitParts(payload []byte, chunkSize int64) [][]byte {
	if chunkSize <= 0 {
		return [][]byte{payload}
	}
	var parts [][]byte
	for offset := int64(0); offset < int64(len(payload)); offset += chunkSize {
		end := offset + chunkSize
		if end > int64(len(payload)) {
			end = int64(len(payload))
		}
		parts = append(parts, payload[offset:end])
	}
	if len(parts) == 0 {
		parts = append(parts, []byte{})
	}
	return parts
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
