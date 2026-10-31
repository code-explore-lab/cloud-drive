// Package objectstore 封装 MinIO 的对象读写，只暴露业务真正需要的少量语义：
// 流式写入、服务端合并、按 Range 读取、删除与校验。
//
// 设计约定：
//   - 对象 Key 由服务端生成，客户端永远不能直接指定（见 ADR-0003）。
//   - 写入一律流式完成，不把文件读进内存，避免大文件 OOM。
//   - 上传分片先写成临时对象，完成时由 MinIO 服务端合并，这样"重传某一片"
//     天然幂等（覆盖同名临时对象即可），无需依赖 MinIO 的 multipart uploadId。
package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ErrNotFound 表示对象在存储侧不存在，调用方据此返回 404，而不是 500。
var ErrNotFound = errors.New("object not found")

// Config 是 MinIO 连接配置，各服务通过 yaml 的 Minio 段注入。
// 字段带 go-zero 的 default/optional 标记：配置项缺失时用默认值，
// 而不是在启动阶段因为"Region 没填"直接失败。
type Config struct {
	Endpoint  string `json:",default=localhost:9000"`
	AccessKey string `json:",optional"`
	SecretKey string `json:",optional"`
	Bucket    string `json:",default=cloud-drive"`
	UseSSL    bool   `json:",default=false"`
	Region    string `json:",optional"`
	// PartSize 是分片上传时单片的期望大小，用于未知长度写入时的缓冲策略。
	PartSize int64 `json:",optional"`
}

// Store 是一个 bucket 维度上的对象存储客户端，并发安全。
type Store struct {
	client *minio.Client
	bucket string
	conf   Config
}

// Info 是对象元数据快照。
type Info struct {
	Key          string
	Size         int64
	ContentType  string
	ETag         string
	LastModified time.Time
}

// PutResult 是一次流式写入的结果。
type PutResult struct {
	Size   int64
	SHA256 string
	ETag   string
}

// New 创建客户端。它不会立刻建立网络连接，因此 MinIO 暂时不可用时服务仍能启动。
func New(cfg Config) (*Store, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, errors.New("objectstore: endpoint is empty")
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("objectstore: bucket is empty")
	}
	if cfg.PartSize <= 0 {
		cfg.PartSize = defaultPartSize
	}

	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("objectstore: new client: %w", err)
	}

	return &Store{client: client, bucket: cfg.Bucket, conf: cfg}, nil
}

const (
	// defaultPartSize 与 MinIO 服务端合并的对象下限（5MiB）保持同量级，留出余量。
	defaultPartSize = 16 << 20
	// MinComposeSize 是 MinIO ComposeObject 对非末尾源对象的硬性下限。
	MinComposeSize = 5 << 20
)

// Bucket 返回当前使用的 bucket 名。
func (s *Store) Bucket() string { return s.bucket }

// Endpoint 返回配置的 endpoint，用于日志和健康检查信息。
func (s *Store) Endpoint() string { return s.conf.Endpoint }

// Ensure 确保 bucket 存在。启动阶段可以反复重试调用，直到 MinIO 就绪。
func (s *Store) Ensure(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("objectstore: check bucket: %w", err)
	}
	if exists {
		return nil
	}
	region := s.conf.Region
	if region == "" {
		region = "us-east-1"
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: region}); err != nil {
		// 并发创建时可能已经被别人建好，回查一次再决定是否算成功。
		if ok, existsErr := s.client.BucketExists(ctx, s.bucket); existsErr == nil && ok {
			return nil
		}
		return fmt.Errorf("objectstore: make bucket: %w", err)
	}
	return nil
}

// EnsureLoop 在后台反复尝试创建 bucket，直到成功或 ctx 结束。
// 服务启动顺序不保证 MinIO 先就绪，这种"自愈式"重试比启动即失败更适合容器环境。
func (s *Store) EnsureLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.Ensure(ctx); err == nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Ping 用于健康检查：能访问 bucket 即认为存储可用。
func (s *Store) Ping(ctx context.Context) error {
	ok, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("objectstore: bucket %q not found", s.bucket)
	}
	return nil
}

// PutStream 从 r 流式写入对象，同时计算 SHA-256。
// size 必须准确（未知时传 -1，此时会按 PartSize 分片缓冲，慎用于超大文件）。
func (s *Store) PutStream(ctx context.Context, key string, r io.Reader, size int64,
	contentType string) (*PutResult, error) {
	if key == "" {
		return nil, errors.New("objectstore: empty object key")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	hasher := sha256.New()
	counter := &countingReader{r: io.TeeReader(r, hasher)}

	info, err := s.client.PutObject(ctx, s.bucket, key, counter, size, minio.PutObjectOptions{
		ContentType: contentType,
		PartSize:    uint64(s.conf.PartSize),
	})
	if err != nil {
		return nil, fmt.Errorf("objectstore: put %s: %w", key, err)
	}
	if size >= 0 && counter.n != size {
		// 客户端声明的长度与实际写入不一致，删除半成品避免留下坏对象。
		_ = s.Remove(ctx, key)
		return nil, fmt.Errorf("objectstore: put %s: wrote %d bytes, declared %d", key, counter.n, size)
	}

	return &PutResult{
		Size:   counter.n,
		SHA256: hex.EncodeToString(hasher.Sum(nil)),
		ETag:   strings.Trim(info.ETag, `"`),
	}, nil
}

// Copy 在服务端复制对象，用于单片上传的"合并"。
func (s *Store) Copy(ctx context.Context, dstKey, srcKey, contentType string) error {
	if dstKey == "" || srcKey == "" {
		return errors.New("objectstore: empty object key")
	}
	_, err := s.client.CopyObject(ctx,
		minio.CopyDestOptions{Bucket: s.bucket, Object: dstKey},
		minio.CopySrcOptions{Bucket: s.bucket, Object: srcKey},
	)
	if err != nil {
		return fmt.Errorf("objectstore: copy %s -> %s: %w", srcKey, dstKey, err)
	}
	return nil
}

// Compose 把多个分片对象按顺序合并成一个对象。所有非末尾分片都必须不小于 5MiB。
func (s *Store) Compose(ctx context.Context, dstKey string, srcKeys []string) error {
	if dstKey == "" {
		return errors.New("objectstore: empty object key")
	}
	if len(srcKeys) == 0 {
		return errors.New("objectstore: no source parts")
	}
	srcs := make([]minio.CopySrcOptions, 0, len(srcKeys))
	for _, key := range srcKeys {
		srcs = append(srcs, minio.CopySrcOptions{Bucket: s.bucket, Object: key})
	}
	_, err := s.client.ComposeObject(ctx,
		minio.CopyDestOptions{Bucket: s.bucket, Object: dstKey},
		srcs...,
	)
	if err != nil {
		return fmt.Errorf("objectstore: compose %s: %w", dstKey, err)
	}
	return nil
}

// OpenRange 打开一个带 Range 的读取流，不做 Stat。
// 调用方应先用 Stat 拿到完整对象信息（这样 404 能在写响应头之前发现）。
func (s *Store) OpenRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	if key == "" {
		return nil, errors.New("objectstore: empty object key")
	}
	opts := minio.GetObjectOptions{}
	if offset > 0 || length > 0 {
		end := int64(-1)
		if length > 0 {
			end = offset + length - 1
		}
		opts.SetRange(offset, end)
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, opts)
	if err != nil {
		return nil, classify(key, err)
	}
	return obj, nil
}

// Get 读取对象，offset/length 用于断点续传（length<=0 表示读到结尾）。
// 调用方负责关闭返回的 ReadCloser。
func (s *Store) Get(ctx context.Context, key string, offset, length int64) (io.ReadCloser, *Info, error) {
	if key == "" {
		return nil, nil, errors.New("objectstore: empty object key")
	}
	opts := minio.GetObjectOptions{}
	if offset > 0 || length > 0 {
		opts.SetRange(offset, offset+length-1)
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("objectstore: get %s: %w", key, err)
	}
	stat, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		return nil, nil, classify(key, err)
	}
	return obj, infoOf(key, stat), nil
}

// Stat 查询对象元数据。
func (s *Store) Stat(ctx context.Context, key string) (*Info, error) {
	if key == "" {
		return nil, errors.New("objectstore: empty object key")
	}
	stat, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return nil, classify(key, err)
	}
	return infoOf(key, stat), nil
}

// VerifySHA256 流式读取对象并比对摘要。只有客户端显式提供摘要时才调用。
func (s *Store) VerifySHA256(ctx context.Context, key, expected string) error {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if expected == "" {
		return nil
	}
	obj, _, err := s.Get(ctx, key, 0, 0)
	if err != nil {
		return err
	}
	defer obj.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, obj); err != nil {
		return fmt.Errorf("objectstore: read %s: %w", key, err)
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if got != expected {
		return fmt.Errorf("objectstore: %s hash mismatch: got %s want %s", key, got, expected)
	}
	return nil
}

// SHA256 流式计算对象摘要并返回。
func (s *Store) SHA256(ctx context.Context, key string) (string, error) {
	obj, _, err := s.Get(ctx, key, 0, 0)
	if err != nil {
		return "", err
	}
	defer obj.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, obj); err != nil {
		return "", fmt.Errorf("objectstore: read %s: %w", key, err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// Remove 删除对象。对象本来就不存在时视为成功（幂等）。
func (s *Store) Remove(ctx context.Context, key string) error {
	if key == "" {
		return errors.New("objectstore: empty object key")
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		errResp := minio.ToErrorResponse(err)
		if errResp.Code == "NoSuchKey" || errResp.StatusCode == 404 {
			return nil
		}
		return fmt.Errorf("objectstore: remove %s: %w", key, err)
	}
	return nil
}

// List 列出某个前缀下的对象，用于对账和临时分片清理。
func (s *Store) List(ctx context.Context, prefix string) ([]Info, error) {
	var out []Info
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if obj.Err != nil {
			return nil, fmt.Errorf("objectstore: list %s: %w", prefix, obj.Err)
		}
		out = append(out, *infoOf(obj.Key, obj))
	}
	return out, nil
}

// PresignGet 生成带有效期的下载地址，供浏览器直连（Phase 4 使用）。
func (s *Store) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	u, err := s.client.PresignedGetObject(ctx, s.bucket, key, ttl, url.Values{})
	if err != nil {
		return "", fmt.Errorf("objectstore: presign %s: %w", key, err)
	}
	return u.String(), nil
}

func infoOf(key string, in minio.ObjectInfo) *Info {
	return &Info{
		Key:          key,
		Size:         in.Size,
		ContentType:  in.ContentType,
		ETag:         strings.Trim(in.ETag, `"`),
		LastModified: in.LastModified,
	}
}

func classify(key string, err error) error {
	errResp := minio.ToErrorResponse(err)
	switch errResp.Code {
	case "NoSuchKey", "NoSuchObject":
		return fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	if errResp.StatusCode == 404 {
		return fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	return fmt.Errorf("objectstore: stat %s: %w", key, err)
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
