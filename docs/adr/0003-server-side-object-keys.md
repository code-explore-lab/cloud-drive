# ADR-0003：对象 Key 由服务端生成，且不暴露给客户端

- 状态：已采纳
- 日期：2026-10

## 背景

客户端需要上传、下载、分享文件。最"直接"的做法是让客户端传 `objectKey`，
服务端原样读写 MinIO。这会把对象存储的寻址能力直接交给客户端。

## 决策

- 对象 Key 由服务端生成：`u/{userId}/{uploadId}{ext}`；
- 分片临时 Key：`parts/{uploadId}/{partNumber:05d}`；
- 客户端拿到的只有 `fileId`、`uploadId`、`shareToken`，**永远拿不到 objectKey**；
- 下载统一走 `GET /v1/files/{id}/content`，由 `file-api` 鉴权后代理到 `storage-api` 的内部接口。

## 理由

1. 如果 objectKey 是客户端可控的，攻击者可以构造 `u/1/xxx` 猜测别人的对象；
2. 即使加了鉴权，把存储寻址暴露出去也会让"对象存储"变成事实上的公开 API；
3. 内部 Key 泄露（日志、错误信息）不会直接导致数据泄露，因为还需要通过鉴权代理。

## 代价

- 每次下载多一跳（`file-api` → `storage-api`），带宽翻倍（在集群内网）；
- 需要内部鉴权（`INTERNAL_TOKEN`）；
- 分享下载也无法直接给 MinIO 预签名地址（未来可做，但要重新评估泄露面）。

## 替代方案

| 方案 | 风险 |
| --- | --- |
| 客户端传 objectKey | 可枚举、可越权，直接否决 |
| 预签名 URL | 链接本身就是授权凭证，泄露即失控；适合 CDN 场景，不适合"权限可撤销"的需求 |
| 本方案 | 多一跳，但权限始终在服务端 |

## 后果

- `storage-api` 的内部下载接口必须校验 `X-Internal-Token`，且该值为空时**拒绝所有请求**；
- 对象 Key 的生成规则只有一处实现（`common/objkey`）。
