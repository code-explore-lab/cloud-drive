# 性能测试方法

> **当前没有任何性能数字。** 这是刻意的：target.md 明确要求"性能指标必须来自真实测试结果"。
> 本文只提供可复现的测量方法与解读框架，测出来的数字请补到下面"结果"一节，
> 并注明机器配置、版本和负载模型。

## 1. 测量什么

| 指标 | 说明 |
| --- | --- |
| 元数据接口吞吐/延迟 | 列表、创建目录、改名、删除 |
| 上传吞吐（MB/s） | 单分片、多分片、并发连接数不同时的表现 |
| 下载吞吐（MB/s） | 是否受 64KiB 缓冲或代理转发影响 |
| 错误率 | 并发上升后的 5xx/超时比例 |
| 资源占用 | CPU、内存、MySQL 连接数、MinIO 网络与磁盘 |

## 2. 环境要求（必须记录）

```text
机器：CPU / 内存 / 磁盘类型（SSD/HDD）/ 网络
系统：内核版本
Go：go version
MySQL：版本 + 关键参数（innodb_buffer_pool_size、max_connections）
MinIO：版本 + 单盘/多盘
部署方式：全 compose / 全 k8s / 混部
服务副本数
```

## 3. 测试数据准备

```bash
# 1) 起完整环境
docker compose up -d --build

# 2) 注册测试用户并拿 token
curl -s -X POST http://127.0.0.1:8000/v1/users/register -H 'Content-Type: application/json' \
  -d '{"username":"perf","password":"Passw0rd!123"}'
TOKEN=$(curl -s -X POST http://127.0.0.1:8000/v1/users/login -H 'Content-Type: application/json' \
  -d '{"username":"perf","password":"Passw0rd!123"}' | sed -n 's/.*"accessToken":"\([^"]*\)".*/\1/p')

# 3) 造文件：1MiB / 8MiB / 64MiB / 256MiB
dd if=/dev/urandom of=/tmp/f1m.bin bs=1M count=1
dd if=/dev/urandom of=/tmp/f8m.bin bs=1M count=8
```

## 4. 元数据接口

```bash
# hey 或 wrk 都可以，注意带 token
hey -n 20000 -c 50 \
  -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:8001/v1/files?parentId=0&page=1&pageSize=20"
```

关注：`Requests/sec`、`Latency P50/P95/P99`、`Non-2xx`。

## 5. 上传吞吐

单文件顺序上传（脚本见 `scripts/e2e`，可改造为计时循环）：

```bash
time go run ./scripts/e2e        # 会走完整上传链路，包含 300KiB 负载
```

更贴近真实的方法是写一个小的循环客户端：

1. `POST /v1/uploads` 拿 `uploadId/chunkSize/totalChunks`；
2. 依次 `PUT` 每个分片（并发数可调：1 / 4 / 16）；
3. `POST .../complete`；
4. 记录总耗时与总字节数。

必须分别测：**单连接顺序**、**单文件多连接**、**多用户并发**。

## 6. 下载吞吐

```bash
curl -s -o /dev/null -w '%{speed_download} B/s\n' \
  -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:8001/v1/files/$FILE_ID/content"
```

对比三组数据可以定位代理层开销：

1. 直连 storage-api（带内部令牌）；
2. 经 file-api 代理；
3. 用 `Range: bytes=0-1` 观察首字节延迟（RTT + 鉴权开销）。

## 7. 服务端观测

```bash
# 容器资源
docker stats --no-stream

# 指标（Prometheus 起来后）
curl -s http://127.0.0.1:9090/api/v1/query?query='rate(http_server_requests_code_total[1m])'
curl -s http://127.0.0.1:9090/api/v1/query?query='histogram_quantile(0.95,sum by(le)(rate(http_server_requests_duration_ms_bucket[5m])))'
```

重点看：

- `mysql_global_status_threads_connected`（如果接了 mysqld_exporter）；
- `clouddrive_upload_bytes_total` 的斜率；
- 进程 RSS 是否随下载量增长（应该不增长——流式转发）。

## 8. 已知的性能相关设计

| 设计 | 影响 |
| --- | --- |
| 下载 64KiB 缓冲 + `io.CopyBuffer` | 内存恒定，不受文件大小影响 |
| 上传分片 8MiB | 单请求内存占用小，但请求数多（大文件需要更多 RTT） |
| 服务端合并（ComposeObject） | 合并阶段有额外一次服务端 IO，不占客户端带宽 |
| 列表查询不走缓存 | 读多时会打 MySQL；有真实数据后再决定是否加缓存 |
| `AppendVersion` 行锁 | 同文件并发上传串行化；不同文件互不影响 |
| 整体摘要校验（仅当客户端提供 hash） | 多一次完整读对象，大文件时明显 |

## 9. 优化方法论

1. 先用指标找到瓶颈在哪一层（HTTP、RPC、MySQL、MinIO）；
2. 只改一处，重测同一负载；
3. 记录"问题 → 假设 → 改动 → 前/后数据"；
4. 如果没变快，回滚并记录这个负结果。

不要在没有测量的情况下引入缓存或消息队列——那只会把问题藏起来。

## 10. 结果（待填）

| 日期 | 场景 | 并发 | 吞吐 | P95 | 错误率 | 机器 |
| --- | --- | --- | --- | --- | --- | --- |
| — | — | — | — | — | — | — |
