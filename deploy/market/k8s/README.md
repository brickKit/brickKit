# 市场的 Kubernetes 部署

把 BrickKit Market 部署到 K8s。与 `deploy/market/docker-compose.yaml`（单机）是两条路，选一条即可。

| 目录 | 部署什么 | 用在哪 |
| --- | --- | --- |
| `base/` | **只有市场本身**，数据库与对象存储指向集群外 | 生产：用云托管 PostgreSQL + S3，或运维统一部署的实例 |
| `overlays/in-cluster-deps/` | 市场 + PostgreSQL + RustFS（都在集群里） | 评估、自建、内网小规模 |

---

## 快速开始（集群内自带依赖）

```bash
cd deploy/market/k8s/base
cp .env.example .env && vi .env          # 填四个口令
kubectl apply -k ../overlays/in-cluster-deps

kubectl -n brickkit-market get pods -w
```

**首次部署时 `market-api` 会 CrashLoopBackOff 一两次，这是正常的。** K8s 没有 compose 的 `depends_on`——市场会先于 PostgreSQL 就绪而启动、连不上库、退出、被重启，直到数据库的 DNS 与端口都好了为止。日志里写的是：

```
市场启动失败：连接数据库失败：dial tcp: lookup postgres ... server misbehaving
```

等 `READY 1/1` 就是好了。验证：

```bash
kubectl -n brickkit-market port-forward svc/market-api 8080:8080
curl http://localhost:8080/api/v1/health
```

## 生产部署（依赖在集群外）

```bash
cd deploy/market/k8s/base
cp .env.example .env && vi .env
vi kustomization.yaml     # 改 DATABASE_HOST / RUSTFS_ENDPOINT 等
vi ingress.yaml           # 改 host 与 ingressClassName
kubectl apply -k .
```

---

## 部署之前必须改的四处

| 位置 | 改什么 | 不改会怎样 |
| --- | --- | --- |
| `base/.env` | 四个口令 | 用示例值等于没有口令 |
| `base/kustomization.yaml` | `DATABASE_HOST` / `RUSTFS_ENDPOINT` 等地址 | 连不上（用 in-cluster-deps 时不用改，overlay 已覆盖） |
| `base/ingress.yaml` | `host` | 域名是 `market.example.com`，谁也访问不到 |
| `base/ingress.yaml` | `ingressClassName` | 集群没有"默认 class"时，**apply 成功但域名打不开** |

## 口令怎么进去的

`base/.env` → kustomize `secretGenerator` → Secret → Pod 的 `envFrom`。

三件事值得注意：

- **`.env` 不进 Git**（已在 `.gitignore` 里）。仓库里只有 `.env.example`。
- **YAML 里不写 `${VAR}`**。kubectl 不做变量替换，占位符会被原样当成口令部署上去——Pod 以认证失败反复重启，而 YAML 看着完全正常。
- **生成的 Secret / ConfigMap 名字带内容哈希**（`market-secrets-dgk7hfb825`）。改了 `.env` 就是一个新名字，Deployment 的引用被自动改写，Pod **因此自动滚动重启**。不带哈希的话，改完口令还得自己记得 `rollout restart`，很容易漏。

## 为什么是 kustomize 而不是 Helm

`kubectl apply -k` 是 **kubectl 自带的**，不需要额外装任何东西，也不引入 release 状态——这份部署的读者是运维，不是要把市场当成产品分发出去的人。

要把市场作为产品分发给第三方时，Helm chart 更合适（版本化、依赖、values schema）。那件事还没做，登记在《开发进度》的延后清单里。

---

## 常见操作

```bash
# 看状态
kubectl -n brickkit-market get pods,svc,ingress

# 看日志
kubectl -n brickkit-market logs -f deployment/market-api

# 改了 .env 之后重新应用（Pod 会自动滚动）
kubectl apply -k ../overlays/in-cluster-deps

# 忘了管理员口令：改 base/.env 的 ADMIN_PASSWORD，
# 把 base/kustomization.yaml 里的 ADMIN_PASSWORD_RESET 改成 true，apply，
# 起来之后再改回 false 并 apply（运维指南 §9 Q5）

# 全部删掉（数据卷会保留，PVC 要单独删）
kubectl delete -k ../overlays/in-cluster-deps
kubectl -n brickkit-market delete pvc --all      # ← 这一条才会删数据
```

> **`base/.env` 删不得。** 任何 `kubectl ... -k` 操作（包括 `delete -k`）都要先渲染一遍
> kustomization，缺了 `.env` 会直接报 `evalsymlink failure on .../.env`——连删都删不掉。
> 实在删了，用 `kubectl delete namespace brickkit-market` 收尾。

## 扩容

`market-api` 默认 **1 个副本**：建表在启动时幂等执行，多个副本同时首次启动会并发建表。等第一个副本把表建好之后再扩：

```bash
kubectl -n brickkit-market scale deployment/market-api --replicas=3
```

集群内的 PostgreSQL 与 RustFS **不要扩**——它们是给评估用的单副本 StatefulSet。要高可用请换成云托管服务，然后改用 `base/`。

---

## 设计依据

| 内容 | 出处 |
| --- | --- |
| 市场的架构与端点 | [design/007-组件市场设计.md](../../../design/007-组件市场设计.md) |
| 部署模式选择 | [部署模式.md](../../../部署模式.md) |
| 单机部署与运维 | [市场部署与运维指南.md](../../../市场部署与运维指南.md) |
| K8s 清单的写法（探针、Secret、PSA） | [design/005-部署与运行规范.md](../../../design/005-部署与运行规范.md) §5 |
