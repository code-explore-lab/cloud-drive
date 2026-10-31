# ADR-0002：存储服务拆成控制面（rpc）与数据面（api）

- 状态：已采纳
- 日期：2026-10

## 背景

文件内容必须进出 MinIO。有两种实现方式：

1. 全部走 gRPC：`file-api` → `storage.rpc`，由 RPC 方法收发字节流；
2. 拆成两个进程：`storage.rpc` 管理状态，`storage-api` 用 HTTP 搬字节。

## 决策

采用方案 2。`storage-api` 是数据面，只做三件事：校验分片归属、写 MinIO、把对象流写回客户端。
所有状态变更（任务、分片记录、对象元数据）都落 `storage.rpc`。

## 理由

1. **gRPC 不适合大字节流**。protobuf 序列化需要把分片放进内存 message，
   `bytes` 字段会带来额外拷贝；HTTP 的 `io.Reader` → `io.Writer` 可以直接流式转发。
2. **Range 与浏览器兼容**。HTTP `Range` 是标准能力，gRPC 需要自定义。
3. **超时语义不同**。控制面适合 3-10s 超时，数据面必须能跑几分钟，
   拆开之后可以分别配置（`rest.WithTimeout(0)` 只作用于数据面路由）。

## 代价

- 多一个进程、多一份 MinIO 配置；
- 客户端需要知道两个地址（`file-api` 谈业务，`storage-api` 传字节）；
- 分片上传每次要额外一次 `GetUploadTask` RPC 做归属校验。

## 替代方案

| 方案 | 问题 |
| --- | --- |
| 全部走 gRPC 流式 | 需要在 protobuf 里定义 chunk 流；网关/代理支持差 |
| file-api 直接写 MinIO | file-api 持有存储凭据，职责膨胀；上传带宽压在业务服务上 |
| 预签名 URL 直传 MinIO | 更优的带宽模型，但需要额外的凭证下发与回调校验；列入后续演进 |

## 后果

- `common/objectstore` 同时被两个进程使用，接口必须保持流式、无缓冲；
- 对象 Key 生成规则抽到 `common/objkey`，防止两边拼出不同路径。
