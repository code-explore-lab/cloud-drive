# 镜像构建说明

CloudDrive 的 7 个 Go 服务共用仓库根目录的同一个 `Dockerfile`，通过构建参数
`SERVICE_DIR` 决定编译哪个 `main` 包。这样做的好处是依赖解析、基础镜像、非 root 用户
和安全配置只有一份，不会出现 7 个 Dockerfile 逐渐分叉。

## 一、服务与构建参数

| 服务 | `SERVICE_DIR` | compose 中的镜像 | K8s 清单中的占位镜像 |
|---|---|---|---|
| user-api | `./service/user/api` | `cloud-drive/user-api:dev` | `cloud-drive/user-api:latest` |
| file-api | `./service/file/api` | `cloud-drive/file-api:dev` | `cloud-drive/file-api:latest` |
| storage-api | `./service/storage/api` | `cloud-drive/storage-api:dev` | `cloud-drive/storage-api:latest` |
| user-rpc | `./service/user/rpc` | `cloud-drive/user-rpc:dev` | `cloud-drive/user-rpc:latest` |
| file-rpc | `./service/file/rpc` | `cloud-drive/file-rpc:dev` | `cloud-drive/file-rpc:latest` |
| storage-rpc | `./service/storage/rpc` | `cloud-drive/storage-rpc:dev` | `cloud-drive/storage-rpc:latest` |
| storage-worker | `./service/storage/worker` | `cloud-drive/storage-worker:dev` | （尚未加入 K8s 清单） |

`SERVICE_DIR` 的默认值是 `./service/user/api`，所以不带参数构建出来的就是 user-api。

## 二、为什么构建上下文必须是仓库根目录

```bash
docker build --build-arg SERVICE_DIR=./service/user/api -t cloud-drive/user-api:dev .
```

最后的 `.` 不能换成 `./service/user/api`，原因：

1. `go.mod` / `go.sum` 在仓库根目录；Go 以模块为单位解析依赖，构建上下文里必须有它们。
2. 各服务都 import 了根目录下的 `common/` 包，也互相引用 `service/*/rpc/types` 等生成代码。
3. Dockerfile 先 `COPY go.mod go.sum ./` 再 `go mod download`，把依赖下载单独做成缓存层；
   这要求模块文件在上下文根目录。
4. `.dockerignore` 在根目录生效，用来排除 `.git`、`deploy/`、`docker-compose.yml`、
   `.env`、Markdown 等与编译无关的内容，避免把本地配置和密钥带进构建上下文。

因此最终运行镜像里只包含编译后的二进制：`deploy/config/*.yaml`、`.env` 和 compose 文件
都被 `.dockerignore` 排除在构建上下文之外；`service/` 下的 SQL 会随源码进入 builder
阶段，但只有 `/out/app` 会被复制到最终镜像。运行时的配置通过 volume 挂载到
`/app/etc/config.yaml`。

## 三、Dockerfile 结构

构建分两阶段（多阶段构建）：

1. **builder：`golang:1.27-alpine`**
   - 先复制 `go.mod` / `go.sum`，执行 `go mod download`，命中依赖缓存。
   - 复制其余源码后执行 `CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w"`，
     产出静态链接的 `/out/app`。
   - 构建参数 `GOPROXY` 默认是 `https://goproxy.cn,direct`，可通过
     `--build-arg GOPROXY=...` 覆盖。
2. **runtime：`alpine:3.20`**
   - 安装 `ca-certificates` 和 `tzdata`。`tzdata` 是必需的：MySQL DSN 里
     `loc=Asia%2FShanghai` 需要时区数据，否则驱动会报 `unknown time zone`。
   - 创建 uid/gid 都为 `10001` 的非 root 用户 `app`。
   - 只复制 `/out/app`，入口是 `["/app/app"]`，工作目录 `/app`。

镜像本身不 `EXPOSE` 端口：端口由各服务的配置文件决定（API 8000/8001/8002，
RPC 9000/9001/9002），容器编排层按配置映射即可。

## 四、构建、打 tag、推送

构建单个服务：

```bash
docker build \
  --build-arg SERVICE_DIR=./service/user/api \
  -t cloud-drive/user-api:dev \
  .
```

推荐给镜像打上可追溯的 tag 再推送：

```bash
IMAGE=registry.example.com/cloud-drive/user-api
TAG=$(git rev-parse --short HEAD)

docker tag cloud-drive/user-api:dev "$IMAGE:$TAG"
docker login registry.example.com
docker push "$IMAGE:$TAG"
```

部署到 Kubernetes 时把 `deploy/kubernetes/services.yaml` 中的
`cloud-drive/user-api:latest` 换成 `$IMAGE:$TAG`。生产环境不要依赖 `latest`，
因为 `latest` 不可复现，也无法准确回滚。

批量构建 7 个服务：

```bash
for pair in \
  "user-api ./service/user/api" \
  "file-api ./service/file/api" \
  "storage-api ./service/storage/api" \
  "user-rpc ./service/user/rpc" \
  "file-rpc ./service/file/rpc" \
  "storage-rpc ./service/storage/rpc" \
  "storage-worker ./service/storage/worker"; do
  set -- $pair
  docker build --build-arg SERVICE_DIR="$2" -t "cloud-drive/$1:dev" .
done
```

## 五、运行方式

容器入口是 `/app/app`，启动时必须用 `-f` 指定配置文件。compose 和 K8s 都挂载到
`/app/etc/config.yaml`：

```bash
docker run --rm -p 8000:8000 \
  --env-file .env \
  -v "$PWD/deploy/config/user-api.yaml:/app/etc/config.yaml:ro" \
  cloud-drive/user-api:dev \
  -f /app/etc/config.yaml
```

注意：

- 这个例子只启动了 user-api 进程；它仍然需要能访问 etcd，完整联调请用
  `docker compose up -d`，见 `deploy/README.md`。
- 配置里的 `${VAR}` 由 go-zero 在启动时从环境变量展开；不要把真实密钥写进镜像。
- 容器以 `10001:10001` 运行，挂载的配置必须对该用户可读。Kubernetes 清单还设置了
  `readOnlyRootFilesystem: true`，因此业务代码不要往容器根文件系统写文件；
  需要临时目录时挂载 `emptyDir` 到 `/tmp`（`services.yaml` 已这样做）。

## 六、BuildKit 与缓存

Dockerfile 用了两个 `RUN --mount=type=cache` 挂载：`/go/pkg/mod`（模块下载）和
`/root/.cache/go-build`（编译缓存）。两者都不写入镜像层，但会在同一台机器上跨构建复用，
七个服务并行构建时也共享同一份缓存。

实测（6 核）：

| 场景 | 耗时 |
|---|---|
| 首次冷构建（缓存为空） | 单个服务约 2 分半 |
| 改一个源文件后重建 | 约 10 秒（只重编改动的包） |
| 源码未变 | 约 2 秒（整层命中缓存） |

没有这两个挂载时，每次构建都在全新容器里从零编译全部依赖，七个服务并行需要 20 分钟以上。

需要 BuildKit。Docker 23+ 默认启用；老版本可以显式开启：

```bash
DOCKER_BUILDKIT=1 docker build --build-arg SERVICE_DIR=./service/user/api -t cloud-drive/user-api:dev .
```

CI 中如果想复用 Docker 层缓存，可以在构建时使用 buildx 的 `--cache-from` /
`--cache-to`；当前 `.github/workflows/ci.yml` 只做构建校验，没有推送镜像，因此没有配置
registry 缓存。

## 七、多架构镜像（可选）

在 amd64 机器上构建 arm64 镜像（例如 Apple Silicon 或 ARM 节点）：

```bash
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --build-arg SERVICE_DIR=./service/user/api \
  -t registry.example.com/cloud-drive/user-api:$(git rev-parse --short HEAD) \
  --push \
  .
```

多架构构建必须直接 `--push` 到 registry（或使用 `--output type=oci`），不能只留在本地
docker 镜像列表里。

## 八、验证镜像

```bash
# 确认运行用户是 10001，而不是 root
docker run --rm --entrypoint id cloud-drive/user-api:dev

# 查看镜像大小与层
docker image inspect cloud-drive/user-api:dev --format '{{.Size}}'
```

## 九、常见问题

| 现象 | 原因与处理 |
|---|---|
| `unknown time zone Asia/Shanghai` | 运行阶段缺少 tzdata；本 Dockerfile 已安装，若自行改基础镜像需补上 |
| `RUN --mount requires BuildKit` | 设置 `DOCKER_BUILDKIT=1` 或升级 Docker |
| `go: module lookup disabled` / 下载超时 | 通过 `--build-arg GOPROXY=https://goproxy.cn,direct` 指定可用代理 |
| `permission denied` 写文件 | 容器是非 root 且 K8s 根文件系统只读；把可写目录挂到 `/tmp` 或数据卷 |
| 镜像里找不到配置文件 | 设计如此：配置不打进镜像，运行时挂载到 `/app/etc/config.yaml` 并用 `-f` 指定 |
| 某一步 `DONE` 了上万秒，构建像卡死 | 构建期间机器进了待机/休眠，WSL2 虚拟机被挂起，而 BuildKit 的墙钟仍在走。可用 `Get-WinEvent -FilterHashtable @{LogName='System'; ProviderName='Microsoft-Windows-Kernel-Power'; ID=506,507}` 核对待机时段；构建前 `powercfg /change standby-timeout-ac 0` 可避免 |
