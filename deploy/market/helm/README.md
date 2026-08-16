# 市场的 Helm chart

`deploy/market/k8s/`（kustomize）与这份 chart **做的是同一件事**，选哪个取决于你的角色：

| | kustomize（`k8s/`） | Helm（本目录） |
| --- | --- | --- |
| 需要装东西吗 | ❌ `kubectl apply -k` 自带 | ✅ 要装 helm |
| 改配置 | 直接编辑 YAML | 改 `values.yaml` 或 `--set` |
| **值写错了** | 照样 apply，集群里才崩 | **install 之前就被拦下**（`values.schema.json`） |
| 版本与回滚 | 靠你自己的 Git | `helm history` / `helm rollback` |
| 分发给第三方 | 得让人 clone 整个仓库 | `helm package` 出一个 tgz |

**自建自用选 kustomize**，不需要多装一个工具。
**要把市场当产品交付给别人**，或者想要 `rollback` 与值校验，选 Helm。

---

## 快速开始（评估：依赖也跑在集群里）

```bash
helm install market ./brickkit-market \
  --create-namespace -n brickkit-market \
  --set deps.enabled=true \
  --set image.tag=dev \
  --set ingress.host=market.your-domain.com \
  --set auth.databasePassword='<强口令>' \
  --set auth.rustfsAccessKey='<access key>' \
  --set auth.rustfsSecretKey='<强口令>' \
  --set auth.adminPassword='<强口令>' \
  --wait
```

`deps.enabled=true` 会在同一命名空间里起一个 PostgreSQL 和一个 RustFS，
并**自动**把数据库与对象存储地址指过去（不用你再改 `database.host`）。

> ⚠️ 仅供评估。生产请关掉它——备份、主从、版本升级这些事，一个 StatefulSet 替不了。

## 生产用法

口令放进你自己创建、自己轮换的 Secret：

```bash
kubectl -n brickkit-market create secret generic market-secrets \
  --from-literal=DATABASE_PASSWORD='...' \
  --from-literal=RUSTFS_ACCESS_KEY='...' \
  --from-literal=RUSTFS_SECRET_KEY='...' \
  --from-literal=ADMIN_PASSWORD='...'

helm install market ./brickkit-market -n brickkit-market \
  --set auth.existingSecret=market-secrets \
  --set database.host=your-postgres.rds.amazonaws.com \
  --set storage.endpoint=https://s3.your-region.amazonaws.com \
  --set ingress.host=market.your-domain.com \
  --set ingress.className=nginx
```

**为什么推荐 `existingSecret`：** 用 `--set` 传口令时，那些值会留在 Helm 的
release 记录里（集群中的 `sh.helm.release.v1.*` Secret），`helm get values`
就能读出来。自己建的 Secret 则由你决定谁能看、什么时候轮换。

## 三处容易被忽略的设计

### 1. 口令不给就**渲染失败**，而不是生成一份起不来的清单

`auth.existingSecret` 与四个口令字段一个都不给时，`helm install` 直接报错并说清怎么补。

不这样做的话，会生成一份能 apply、Pod 却起不来的 Deployment——
现场只有 `CrashLoopBackOff`，看不出是配置没填。

### 2. 改配置会自动滚动重启

kustomize 靠"ConfigMap 名字带内容哈希"白拿到这个行为；Helm 的名字是固定的，
必须自己在 Pod 模板上加 `checksum/config` 注解。**没有它的话**，改完口令
`helm upgrade` 完全成功，而 Pod 还在用旧值——你得自己记得 `rollout restart`，
那是最容易漏的一步。

已实测：`--set market.jwtExpiryHours=168` 之后 ReplicaSet 换了新的。

### 3. `replicaCount` 默认 1，不是保守

建表在**启动时**幂等执行（`market-server/internal/repo/postgres.go` 的 `Migrate`），
多个副本同时首次启动会并发建表。要扩容请**先让第一个副本把表建好**再改这个值。

## 常用命令

```bash
helm lint ./brickkit-market                    # 静态检查
helm template market ./brickkit-market ...     # 只看会生成什么，不部署
helm history market -n brickkit-market         # 版本历史
helm rollback market 1 -n brickkit-market      # 回滚到某一版
helm package ./brickkit-market                 # 打成 tgz 分发
```

> `helm lint` 对模板里的 `fail` 只报 INFO、退出码仍是 0。真正会拦住的是
> `helm template` 与 `helm install`（它们走同一条渲染路径）。
> 想在 CI 里把关，用 `helm template` 而不是 `helm lint`。

## 验证记录

这份 chart 在 minikube 上真部署过，不是只 `template` 看了看：

| 验证 | 结果 |
| --- | --- |
| 不给口令 | `helm template` 退出码 1，并说清两种补法 |
| 值写错（4 种） | schema 全部拦下，指名道姓说哪个字段、错在哪 |
| 与 kustomize 比对 | 配置项**一项不缺**（Namespace 除外——Helm 用 `--create-namespace`） |
| `helm install --wait` | 三个 Pod 全部 Running |
| 健康检查 | `{"success":true,"data":{"status":"ok",...}}` |
| 真业务（登录） | 返回真实 token |
| 改配置后 upgrade | ReplicaSet 换新，服务仍 200 |

真跑还抓到一个 bug：集群内依赖曾复用带死名字的标签集，导致 postgres 与 rustfs
的 Pod 也带上 `app.kubernetes.io/name: market-api`——`kubectl logs deployment/market-api`
会选中 rustfs 的 Pod。Service 当时只是**碰巧**没坏（`targetPort` 用的是命名端口），
改成数字端口就会立刻把流量转给数据库。已拆成 `market.commonLabels` + 各自的名字。
