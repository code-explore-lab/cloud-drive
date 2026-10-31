# Kubernetes 部署说明

本文档描述如何把 CloudDrive 的 6 个 Go 服务（3 个 API + 3 个 RPC）和 4 个基础设施
（MySQL / Redis / etcd / MinIO）部署到一个 Kubernetes 集群。

> 这些清单面向开发、演示和单节点测试集群。生产环境请使用云托管数据库、托管 Redis、
> 托管 etcd 和对象存储，或至少使用带备份、多副本、监控的 Operator；详见文末“已知限制”。

## 一、前置条件

- Kubernetes 1.25+（清单只使用稳定的 `apps/v1` / `v1` API）。
- `kubectl` 已连接到目标集群。
- 集群有默认 StorageClass，能够动态创建 PVC（MySQL 20Gi、Redis 5Gi、etcd 5Gi、MinIO 50Gi）。
  如没有默认 StorageClass，请在 `infra.yaml` 的 `volumeClaimTemplates` 中显式指定
  `storageClassName` 并调整容量。
- 6 个服务镜像已经构建并推送到集群可访问的镜像仓库；或使用 kind/minikube 时先
  `kind load docker-image` / `minikube image load` 把本地镜像加载进节点。
- 已阅读 `deploy/docker/README.md` 的镜像构建约定。

## 二、文件清单

| 文件 | 内容 |
|---|---|
| `namespace.yaml` | 命名空间 `cloud-drive` |
| `configmap.yaml` | `cloud-drive-config`（6 份服务配置）+ `cloud-drive-env`（非敏感环境变量） |
| `secret.example.yaml` | Secret 骨架与占位值，**不要直接用于生产** |
| `infra.yaml` | MySQL / Redis / etcd / MinIO 的 StatefulSet、Service、PVC 模板，以及 MySQL 初始化 SQL 的 ConfigMap |
| `services.yaml` | 6 个 Go 服务的 Deployment + Service |
| `README.md` | 本文档 |

没有 `kustomization.yaml`，按下面的顺序用 `kubectl apply -f` 逐个应用即可。

## 三、准备镜像

`services.yaml` 里的镜像名是占位符：

```text
cloud-drive/user-api:latest
cloud-drive/file-api:latest
cloud-drive/storage-api:latest
cloud-drive/user-rpc:latest
cloud-drive/file-rpc:latest
cloud-drive/storage-rpc:latest
```

用仓库根目录的 `Dockerfile` 构建（`SERVICE_DIR` 指定服务目录）：

```bash
# 以 user-api 为例，其余服务把 SERVICE_DIR 和镜像名替换掉即可
docker build \
  --build-arg SERVICE_DIR=./service/user/api \
  -t registry.example.com/cloud-drive/user-api:$(git rev-parse --short HEAD) \
  .
docker push registry.example.com/cloud-drive/user-api:$(git rev-parse --short HEAD)
```

然后修改 `services.yaml` 中对应的 `image:`。生产环境建议使用 Git SHA 等不可变 tag，
不要长期依赖 `latest`。

如果使用 kind：

```bash
kind load docker-image cloud-drive/user-api:latest
```

## 四、部署步骤

### 1. 创建命名空间

```bash
kubectl apply -f deploy/kubernetes/namespace.yaml
```

### 2. 创建 Secret

仓库里只有 `secret.example.yaml`。二选一：

**方式 A：复制并填写示例**

```bash
cp deploy/kubernetes/secret.example.yaml deploy/kubernetes/secret.yaml
# 编辑 secret.yaml，替换所有 REPLACE_WITH_* 占位值
kubectl apply -f deploy/kubernetes/secret.yaml
```

> `secret.yaml` 含明文密钥，不要提交到 Git。仓库目前没有 `.gitignore`，
> 请自行确认它不会被 `git add` 进去。

**方式 B：直接用 kubectl 创建（推荐，密钥不落盘）**

```bash
# 先设置一个变量，保证 MYSQL_DSN 里的密码与 MYSQL_ROOT_PASSWORD 一致
MYSQL_ROOT_PASSWORD='替换成强密码'

kubectl -n cloud-drive create secret generic cloud-drive-secret \
  --from-literal=MYSQL_ROOT_PASSWORD="$MYSQL_ROOT_PASSWORD" \
  --from-literal=MYSQL_DSN="root:${MYSQL_ROOT_PASSWORD}@tcp(mysql:3306)/cloud-drive?charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai" \
  --from-literal=JWT_SECRET="$(openssl rand -hex 32)" \
  --from-literal=USER_SALT="$(openssl rand -hex 16)" \
  --from-literal=INTERNAL_TOKEN="$(openssl rand -hex 32)" \
  --from-literal=MINIO_ACCESS_KEY="$(openssl rand -hex 8)" \
  --from-literal=MINIO_SECRET_KEY="$(openssl rand -hex 16)"
```

Secret 键说明：

| 键 | 用途 |
|---|---|
| `MYSQL_DSN` | 3 个 RPC 服务的 MySQL 连接串，主机名必须是 `mysql` |
| `MYSQL_ROOT_PASSWORD` | **仅** `infra.yaml` 的 MySQL StatefulSet 初始化使用，必须与 DSN 中的密码一致 |
| `JWT_SECRET` | user.rpc 的 JWT 签名密钥 |
| `USER_SALT` | user.rpc 的密码哈希盐 |
| `INTERNAL_TOKEN` | file-api 与 storage-api 的内部调用令牌 |
| `MINIO_ACCESS_KEY` | MinIO 访问凭据，同时作为 MinIO root 用户名 |
| `MINIO_SECRET_KEY` | MinIO 访问凭据，同时作为 MinIO root 密码 |

配置里通过 `${INTERNAL_TOKEN}`、`${MYSQL_DSN}` 等引用这些值；go-zero 启动时用
`os.ExpandEnv` 展开，Deployment 通过 `envFrom.secretRef` 注入。

> 如果 MySQL 密码包含 `@`、`:`、`/` 等 DSN 保留字符，需要先做 URL 编码，
> 否则 `MYSQL_DSN` 会被截断或解析失败。

### 3. 创建服务配置

```bash
kubectl apply -f deploy/kubernetes/configmap.yaml
```

`cloud-drive-config` 里包含 6 份配置；Deployment 用 `subPath` 把它们分别挂到
`/app/etc/config.yaml`。`cloud-drive-env` 注入 `TZ`、`MINIO_ENDPOINT`、`MINIO_BUCKET`、
`MINIO_USE_SSL`、`PUBLIC_BASE_URL`、`REDIS_PASSWORD` 等非敏感变量。

### 4. 部署基础设施

```bash
kubectl apply -f deploy/kubernetes/infra.yaml
```

`infra.yaml` 会创建：

- `cloud-drive-mysql-init` ConfigMap：把 `service/user/model/user.sql`、
  `service/file/model/file.sql`、`service/storage/model/storage.sql` 原样嵌入，
  挂载到 `/docker-entrypoint-initdb.d`，MySQL 首次初始化时按文件名顺序执行。
- MySQL / Redis / etcd / MinIO 的 StatefulSet、Service 和 PVC 模板。

等待基础设施就绪（首次拉镜像和初始化会比较慢）：

```bash
kubectl -n cloud-drive rollout status statefulset/mysql --timeout=600s
kubectl -n cloud-drive rollout status statefulset/redis --timeout=300s
kubectl -n cloud-drive rollout status statefulset/etcd  --timeout=300s
kubectl -n cloud-drive rollout status statefulset/minio --timeout=300s
```

### 5. 部署 6 个 Go 服务

```bash
kubectl apply -f deploy/kubernetes/services.yaml
```

然后等待滚动完成：

```bash
for d in user-api file-api storage-api user-rpc file-rpc storage-rpc; do
  kubectl -n cloud-drive rollout status deployment/$d --timeout=300s
done
```

如果服务在基础设施就绪前已经启动并进入 CrashLoopBackOff，等基础设施 Ready 后
`kubectl rollout restart` 即可恢复。

## 五、验证

### 1. 看 Pod 与 Service

```bash
kubectl -n cloud-drive get pods -o wide
kubectl -n cloud-drive get svc
```

期望每个 Deployment 有 2 个 Ready 实例，基础设施 StatefulSet 各有 1 个 Ready 实例。

### 2. 端口转发并检查健康接口

API 服务都是 ClusterIP，需要 port-forward 到本机。每个命令占一个终端：

```bash
kubectl -n cloud-drive port-forward svc/user-api    8000:8000
kubectl -n cloud-drive port-forward svc/file-api    8001:8001
kubectl -n cloud-drive port-forward svc/storage-api 8002:8002
```

```bash
curl -fsS http://127.0.0.1:8000/healthz; echo   # ok
curl -fsS http://127.0.0.1:8001/healthz; echo   # ok
curl -fsS http://127.0.0.1:8002/healthz; echo   # ok
curl -fsS http://127.0.0.1:8002/readyz;  echo   # ok（额外检查 MinIO bucket）
```

`storage-api` 的 readinessProbe 用 `/readyz`：bucket 尚未建好时 Pod 不会接流量；
livenessProbe 仍用 `/healthz`，只看进程是否存活。其余 API 的 readiness/liveness 都用
`/healthz`。

### 3. 确认 MySQL 初始化

```bash
kubectl -n cloud-drive exec mysql-0 -- \
  sh -c 'mysql -uroot -p"$MYSQL_ROOT_PASSWORD" cloud-drive -e "SHOW TABLES;"'
```

应能看到 `users`、`folders`、`files`、`upload_tasks`、`storage_objects` 等表。

### 4. 看日志

```bash
kubectl -n cloud-drive logs deployment/user-api --tail=100
kubectl -n cloud-drive logs deployment/user-rpc --tail=100
kubectl -n cloud-drive logs statefulset/mysql --tail=100
```

RPC 服务的 Service（`user-rpc:9000` 等）只是排障入口；实际服务发现由 etcd 完成，
go-zero 注册的是 Pod IP。把 RPC Deployment 扩到 2 副本是安全的。

## 六、更新配置与镜像

- **改镜像**：修改 `services.yaml` 的 `image:` 后 `kubectl apply -f`，会触发滚动更新。
- **改配置**：修改 `configmap.yaml` 后 apply，然后必须重启对应 Deployment：

  ```bash
  kubectl apply -f deploy/kubernetes/configmap.yaml
  kubectl -n cloud-drive rollout restart deployment/user-api
  ```

  因为配置是通过 `subPath` 挂载到 `/app/etc/config.yaml` 的，ConfigMap 更新不会自动
  同步到已运行的容器；这是 `subPath` 的已知行为。
- **改 Secret**：更新 Secret 后同样需要 `kubectl rollout restart` 让容器重新读取环境变量。

## 七、回滚

```bash
# 查看历史版本
kubectl -n cloud-drive rollout history deployment/user-api

# 回滚到上一个版本
kubectl -n cloud-drive rollout undo deployment/user-api

# 回滚到指定 revision
kubectl -n cloud-drive rollout undo deployment/user-api --to-revision=2

# 确认
kubectl -n cloud-drive rollout status deployment/user-api
```

注意：只有 Pod 模板变化（镜像、配置引用、探针等）才会生成新的 Deployment revision。
只改 ConfigMap 内容再 `rollout restart` 也会生成新 revision，但回滚 Deployment 不会
自动回滚 ConfigMap；配置回滚需要重新 apply 旧版 `configmap.yaml`。

## 八、清理

StatefulSet 的 PVC 默认不会随 StatefulSet 删除，需要显式处理：

```bash
# 1. 先删除工作负载与基础设施，PVC 会保留
kubectl delete -f deploy/kubernetes/services.yaml
kubectl delete -f deploy/kubernetes/infra.yaml

# 2. 确认不再需要数据后，删除 PVC（跳过则保留数据，方便重建后复用）
kubectl -n cloud-drive delete pvc data-mysql-0 data-redis-0 data-etcd-0 data-minio-0

# 3. 删除配置、Secret 和命名空间
kubectl delete -f deploy/kubernetes/configmap.yaml
kubectl delete -f deploy/kubernetes/secret.yaml   # 如果创建过
kubectl delete -f deploy/kubernetes/namespace.yaml
```

> 删除 PVC 或删除 namespace 都会永久删除数据；`kubectl delete namespace` 会级联删除
> 该 namespace 下所有资源（包括剩余的 PVC），执行前务必确认。

## 九、已知限制

- **基础设施是单节点**：MySQL、Redis、etcd、MinIO 都只有 1 个副本，没有主从/集群、
  没有自动故障转移和备份。任何节点故障都会导致数据不可用。生产请使用托管服务。
- **MySQL 只初始化一次**：`/docker-entrypoint-initdb.d` 只在数据目录为空时执行。
  修改 `service/*/model/*.sql` 后需要重建 PVC 或手工执行迁移；生产建议使用迁移工具。
- **没有 HPA**：6 个服务固定 2 副本，未配置 HorizontalPodAutoscaler，也没有基于
  CPU/QPS 的弹性伸缩。登录限流仍是进程内令牌桶，2 副本会把总限流放大。
- **没有 Ingress / TLS**：所有 Service 都是 ClusterIP，只能通过 `port-forward` 或
  集群内访问；`PUBLIC_BASE_URL` 目前是 `http://file-api:8001`，分享链接在集群外不可用。
  需要对外暴露时应增加 Ingress、证书和真实的公网域名。
- **没有监控采集**：未包含 ServiceMonitor / PodMonitor。go-zero dev server 的 `:6060`
  暴露 `/metrics`，但本清单没有暴露 6060 端口，也没有 Prometheus Operator 资源；
  需要时自行增加 PodMonitor/ServiceMonitor 和 6060 端口。
- **镜像 tag 是占位符**：`latest` 不可复现；`minio/minio:latest` 也随上游变化。
  生产应固定版本或 digest。
- **Secret 管理较简单**：只提供示例骨架，没有 External Secrets / Sealed Secrets /
  Vault 集成；`secret.yaml` 若被提交会泄露密钥。
- **MinIO 使用 root 凭据**：应用和 MinIO root 共用 `MINIO_ACCESS_KEY/SECRET_KEY`；
  生产应创建独立用户和最小权限 bucket policy。
- **MySQL 应用使用 root**：与 docker-compose 的单实例方式一致；生产应创建专用账号。
- **没有 NetworkPolicy / PDB / ResourceQuota**：Pod 之间默认互通，节点维护时也没有
  PodDisruptionBudget 保护。
- **没有 kustomize overlay**：没有 dev/staging/prod 分环境；需要时可按目录拆分并引入
  Kustomize。
- **Secret 注入范围偏大**：为保持清单简单，6 个 Deployment 都通过同一个
  `envFrom.secretRef` 挂载 `cloud-drive-secret`，API Pod 也能读到 `MYSQL_DSN` 等
  与自身无关的密钥。生产建议按服务拆分 Secret，只注入该服务真正需要的键。
- **PVC 依赖默认 StorageClass**：容量（20Gi/5Gi/5Gi/50Gi）只是示例值，未做容量规划。
- **私有镜像仓库**：清单没有配置 `imagePullSecrets`；如果镜像不在公开仓库，需要给
  Deployment/StatefulSet 增加对应的 pull secret 或给默认 ServiceAccount 绑定。
