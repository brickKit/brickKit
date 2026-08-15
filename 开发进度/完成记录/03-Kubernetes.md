---
created: 2026-08-12
updated: 2026-08-15
---

# Kubernetes：清单生成、真集群、网络策略

> 收录范围：Step 16-A–D、P26。全部条目的索引见 [README.md](README.md)，
> 总览与导航见 [../README.md](../README.md)。

---

### 结欠账（四）：P26 —— NetworkPolicy / ServiceAccount 生成 — ✅ 2026-08-15

**背景：** P26 当初记的理由是"三者都强依赖集群侧的策略约定，凭空生成一份多半是错的"。
重新看这条判断，它对 PDB 仍然成立，对另外两样**不成立**——因为漏了一件事：
**依赖图在平台手里**。谁该连谁是组件声明出来的，不是猜的。

手写 NetworkPolicy 之所以又难又容易过期，根源正是那张图得靠人记：
加一个依赖要记得去开口子，删一个依赖没人记得收回。生成器不会忘。

**新增两个 opt-in 开关**（与 `podSecurity` 同一个道理，D246：加上去可能让本来
跑得好好的东西不通，平台不替使用者决定）：

```yaml
deploy:
  networkPolicy:
    enabled: true
    ingressController:
      namespace: ingress-nginx
      podSelector: {app.kubernetes.io/name: ingress-nginx}
  serviceAccount:
    enabled: true
```

**在真能执行策略的集群上验证。** 这一条是整段工作里最要紧的准备：
原来那个 minikube 跑的是 plain bridge CNI，**没有任何 NetworkPolicy 控制器**——
策略 apply 会成功，然后被静默忽略。在那上面测等于什么都没测。
另起了一个 `--cni=calico` 的 profile（`minikube start -p netpol --cni=calico`），
装 4 个真组件 + 真 postgres/redis + ingress-nginx。

组件挑的是能覆盖全部分支的最小集：`people/basic` **强**依赖 `department/tree`，
`infra/api-docs` **弱**依赖它，`infra/redis-event-bus` 不依赖它。

| 验证 | 做法 | 结果 |
| --- | --- | --- |
| 强依赖能进 | 从 people/basic 的 Pod 连 `department-tree-1-0-0:8080` | `CONNECTED` |
| **没声明依赖的进不来** | 从 infra/redis-event-bus 的 Pod 连同一个地址 | `BLOCKED: TimeoutError` |
| 弱依赖能进 | 看 api-docs 自己的探测结果 | `department/tree: ok`（openapi+grpc 都发现了） |
| 对外组件可访问 | 经 Ingress `curl docs.local` | HTTP 200 |
| **那条规则确实必要** | 手工把 ingress controller 那条 from 删掉再访问 | **HTTP 000（连接超时）**，恢复后回到 200 |
| 令牌没挂载 | `ls /var/run/secrets/kubernetes.io/serviceaccount/` | `No such file or directory` |
| 探针不受影响 | 4 个组件全部 `running（healthy）` | 通过 |
| 幂等 / 清理 | 重跑 `up`；`down` 后查残留 | 策略与 SA 都清干净，只剩 K8s 自带的 `default` |

**"那条规则确实必要"这一行是整张表里最有价值的。** 删掉之后现象是
**连接超时**（HTTP 000），连 502 都没有——一眼看去像组件本身挂了，
最不容易联想到是自己刚打开的那个开关。所以 CLI 在生成阶段就硬性拦下
"开了策略 + 有 expose 组件 + 没说 controller 在哪"这个组合。

**PDB 刻意不做，而且是实测之后的决定。** 单副本下 PDB 无论怎么写都是死路：
`minAvailable: 1` 要留 1 个而总共就 1 个，等于一个也不许赶走；
`maxUnavailable: 1` 又等于没有这个 PDB。真在 calico 集群上试了一次——
`kubectl get pdb` 显示 **ALLOWED DISRUPTIONS: 0**，`kubectl drain` 报
"would violate the pod's disruption budget" 一直重试到超时，**排空永远不可能成功**。
要命的是代价落在谁身上：打开开关的是开发者，撞上的是几个月后升级集群的运维。
登记为 **P35**，绑在多副本（005 §5.8）之后。这条结论已固化成测试
（`TestNoPodDisruptionBudgetGenerated`），并顺带断言 `replicas` 还是写死的 1——
前提一变就会有人被提醒回来重新考虑。

**顺手补的一处结构问题：** 子目录名原来散在 `k8s.go`（生成）与 `engine/kubectl.go`
（apply / delete）两处的字符串字面量里。这次新增两类清单正好撞上——
生成器产出了、引擎不认识的话，表现是"文件生成了、集群里却没有"，
`brickkit up` 一路成功退出码 0。改成 `k8s.ManifestDirs()` 单一来源，
并加了 `kubectl_dirs_test.go` 盯住两边一致；删除顺序改成部署顺序的严格反序
（先删 ingress 再删 deployment，中间那段时间请求干脆地 404，
而不是打到一个正在消失的后端上超时）。

**决策记录：**

| # | 决策 | 理由 |
| --- | --- | --- |
| D379 | NetworkPolicy 只生成 **Ingress** 方向，不生成 Egress | 出站生成不出正确规则：DNS 要放行 kube-dns（各集群位置不一），数据库在 K8s 下由运维部署（005 §5.1），配置里只有一个 host 字符串，变不成 podSelector 也变不成 CIDR。生成一份"为了不误伤而放行 0.0.0.0/0"的出站策略**比不生成更糟**——它会让人以为出站已经管住了 |
| D380 | 没人依赖的组件也要生成一份**空 ingress** 的策略 | 不生成等于"不管"，那个 Pod 完全敞着——NetworkPolicy 的语义是"没被任何策略选中就是全放行"。空规则才是"谁也不许进" |
| D381 | 弱依赖与强依赖**一样放行** | 弱依赖是"有就用、没有就降级"（003 §4.3），对方在的时候是真会去连的。漏掉的表现极迷惑：组件装了、起来了、健康检查也过，只有那条"可选"链路永远超时，看起来像对方本来就没装 |
| D382 | 只放行**本次真的会跑起来**的依赖方 | 被级联跳过或显式关掉的组件不在生成范围内，给它们留口子没有意义 |
| D383 | 端口收敛到组件**声明过**的那些；Ingress 那条只放行主端口 | people/basic 的 gRPC 在 extraPorts 的 9090，只放主端口的话 HTTP 通、gRPC 不通，而两者是同一个组件，排查时很容易往"组件挂了"想。反过来，Ingress 只会打到主端口，没必要把 extraPorts 也对外开 |
| D384 | `namespaceSelector` 与 `podSelector` 必须写在**同一个 from 元素**里 | 同一元素内是 AND，拆成两个元素是 OR——那会变成"该命名空间的所有 Pod + 所有命名空间里符合标签的 Pod"。这是 NetworkPolicy 最经典的坑：写错照样 apply 成功、照样通，只是范围比你以为的大得多，且没有任何迹象 |
| D385 | 开了策略 + 有 `expose: true` + 没写 `ingressController` → **阻断** | 实测：那条规则一删，站点是连接超时（HTTP 000），连 502 都没有。部署会全部成功而网站直接打不开，是最难往"我刚打开的那个开关"上想的一类故障 |
| D386 | `podSelector` 不写时只按命名空间放行 | 各家 controller 的标签五花八门（ingress-nginx / traefik / higress……），不该逼使用者非得写对；命名空间这一级已经收得够紧 |
| D387 | ServiceAccount 的重点是 `automountServiceAccountToken: false` | 默认所有 Pod 共用 default SA 并被塞进一张能跟 API Server 说话的令牌，而 003 的组件模型里根本没有"访问 K8s API"这回事。关掉是纯收益：组件被拿下也拿不到一张能问集群要东西的票。Pod 上再写一次不是冗余——Pod 级会覆盖 SA 级，以后有人手工把 SA 那个开关打开，这些 Pod 还是不挂载 |
| D388 | 写了 `serviceAccountName` 就只引用、不生成，且**不写** automount | 云上 SA 常绑着 IRSA / Workload Identity 的注解，由运维创建授权；平台重新生成一份会安静地抹掉那份授权。别人的 SA 可能正是靠令牌去调 API 的，平台无权替它决定 |
| D389 | **不生成 PodDisruptionBudget**，等多副本之后再说 | 单副本下 `minAvailable: 1` 让 ALLOWED DISRUPTIONS 恒为 0，节点永远排不空（真跑验证）；`maxUnavailable: 1` 又等于没写。代价落在几个月后升级集群的运维身上，而现场跟 brickkit.yaml 的某个开关联系不起来。登记 **P35** |
| D390 | 子目录名收敛到 `k8s.ManifestDirs()` 单一来源，删除顺序是部署顺序的反序 | 两边各写字符串时，新增一类清单的表现是"文件生成了、集群里却没有"，up 一路成功；down 漏掉则删不干净、下次 up 撞残留。反序删除还顺带让 ingress 先撤，请求干脆 404 而不是超时 |

**设计书修订：** 005 新增 §5.13（网络策略与最小权限身份），003 §4.1 补
`tlsSecret` / `serviceAccountName` 两个组件字段。

**复盘时补写的两节（第一版漏了）：**

- **§5.13.0 前提：集群必须有能执行策略的 CNI。** 第一版只把这件事写进了试用指南，
  而它恰恰是整个功能唯一会**无声失效**的前提——没有 CNI 时 apply 成功、
  `get networkpolicy` 看得见、流量完全不受影响、没有任何警告。
  规范里不写，读规范的人就会在托管集群上想当然地开这个开关。
  同时写明**为什么 CLI 不代查**：K8s 没有 API 能回答"本集群是否执行 NetworkPolicy"，
  靠 grep kube-system 的 Pod 名去猜会在托管集群（CNI 跑在控制面、用户看不见）
  上误报"不支持"，把正确的部署拦下来。
- **§5.13.1.1 已知限制：依赖图之外的入站方来源。** 复查时发现的**真缺口**——
  002 早有 `observability.metrics` 字段，声明了它的组件会被 Prometheus 抓取，
  而生成的策略会把抓取挡掉，**现象是指标悄悄停了**、服务本身完全正常。
  这与"不生成 Egress"性质不同：后者是平台推导不出正确规则（有意的边界），
  前者是漏了。登记 **P36**。

**§5.13.5 后续方向（回答"能不能指明一个 policy 再照着生成"）：** 遵循平台已有的
分工线——能推导的由平台推导，推导不出的由使用者声明，但声明的是**意图**而非
NetworkPolicy 的 YAML（贴原始 YAML 的话平台既没法与推导结果合并也没法校验，
使用者不如自己 apply）。`ingressController` 已经是这条线的实例。
拟推广为 `allowFrom`（P36）与 `allowTo`（P37）。

**试用指南：** 新增 [17-网络策略与最小权限.md](../../试用指南/17-网络策略与最小权限.md)，
含"把它弄坏"的两个实验（删规则看站点挂、非依赖方连不上）。

---

---

### Step 16-D：面向真实集群 — ✅ 2026-08-15

**背景：** 你说明 minikube 只是本地测试，真实场景用正常 K8s。据此核对了一遍
"minikube 上试不出来、真集群上必然撞上"的东西，发现 5 个缺口，分两段做掉。
**全部在真集群上验证过**（包括把命名空间打上 `pod-security.kubernetes.io/enforce: restricted`）。

**16-D-1 集群定位**（"部到了错误的地方，而且成功了"这一类）：

| 能力 | 说明 |
| --- | --- |
| `deploy.context` + `--context` | 钉住 kubeconfig 上下文。不一致就**阻断**，并说明期望哪个、当前哪个 |
| `deploy.namespace` | 用运维分配的命名空间（默认仍是 `brickkit-<项目名>`） |
| `deploy.createNamespace: false` | 不生成也不 apply `namespace.yaml`；`down` 也不会删它 |

**16-D-2 集群约束**（"集群拒收或安静地不生效"这一类）：

| 能力 | 说明 |
| --- | --- |
| `deploy.podSecurity: restricted` | 按 Pod Security Standards 的 restricted 级别生成 securityContext（主容器 + 迁移容器） |
| `deploy.imagePullSecrets` | 私有 registry 拉取凭据 |
| `deploy.ingressClass` / `ingressAnnotations` | 写进 `spec.ingressClassName`；注解原样透传（cert-manager 等） |
| `components[].tlsSecret` | Ingress 的 TLS 证书 |

**真集群验证：**
错 context → 阻断且一条 kubectl 都不执行、不留生成物；对上 → 部署成功。
`namespace: team-a-prod` + `createNamespace: false` → 不生成 `namespace.yaml`、部进既有命名空间，
`down` 之后**命名空间还在**。给该命名空间打上 `enforce: restricted` 后：不加 `podSecurity` →
Pod 被拒收；加上 → 部署成功，`kubectl get pod -o jsonpath` 确认 securityContext 四项都在。

**真跑抓到 1 个 UX bug：** PSA 拒收时，`kubectl apply` 那个 Job **是成功的**（Job 对象建出来了），
但 Job 控制器创建 Pod 时被拒——Job 既不 Complete 也不 Failed，`kubectl wait` **静默挂满 10 分钟**，
而且**一条日志都没有**（Pod 根本没被创建），原因只写在 Job 的 events 里。
已把失败提示改成"**先看事件再看日志**"，并点出准入控制这个最常见的原因。

**决策记录：**

| # | 决策 | 理由 |
| --- | --- | --- |
| D243 | `deploy.context` 不一致就阻断，且**每条 kubectl 都带 `--context`** | 只在执行前校验一次不够：校验与执行之间有时间差，使用者可能在另一个终端切走了 context |
| D244 | 取不到 current-context 时**不拦** | kubeconfig 形态千奇百怪（in-cluster 等）。因为读不到 context 就拒绝部署，只会让人绕开 CLI |
| D245 | 命名空间不是我们建的就不能由我们删；`down` 时逐个子目录删，不用 `-R` | `delete -R` 一旦扫到一份残留的 `namespace.yaml`，会把整个团队的东西一起端了 |
| D246 | `podSecurity` 默认**什么都不生成** | 加 securityContext 可能让本来跑得好好的组件起不来（镜像以 root 运行、要绑特权端口）。平台不替使用者做这个决定 |
| D247 | 不生成 `readOnlyRootFilesystem` | restricted 级别并不要求它，而它会让任何往 /tmp 写东西的组件直接挂掉。005 §14.3 把它列为可选加固项 |
| D248 | restricted + 端口 < 1024 在生成阶段就警告 | restricted drop 掉了 NET_BIND_SERVICE。不警告的话 Pod 会建出来然后一直崩溃重启，`permission denied` 埋在日志最深处 |
| D249 | `ingressAnnotations` 原样透传，平台不认识也不该认识 | cert-manager、nginx 参数……集群侧的能力千差万别；平台一旦开始理解它们，就得跟着每个 controller 演进 |

**设计书修订：** 005 新增 §5.5.2（面向真实集群的四个字段）与 §5.11（部到哪个集群），→ 1.12.0。

**未做（明确记录）：** 多副本与 HPA 仍是后期能力（005 §5.8）；NetworkPolicy、ServiceAccount
与 PodDisruptionBudget 未生成，登记为 **P26**。

---

---

### 接上真集群（minikube）+ 试用指南 — ✅ 2026-08-15

**背景：** 你装了 minikube，K8s 的真机验证（**P25**）与本机无集群的阻塞（**L7**）就此关闭。
同时把"目前已实现的每一项能力怎么用"整理成 [试用指南/](../../试用指南/)，可以按编号一节一节跟着走。

**真集群验证（minikube v1.38.1 / k8s v1.35.1）：** 用 `department/tree` 真镜像 +
集群内 PostgreSQL 跑通了完整生命周期：`up`（namespace → secret → 清旧 Job → 迁移 Job →
`wait` 完成 → deployments/services → `rollout status`）→ `status` → **幂等重跑** → `down`。
逐项确认：Job `Complete`、Secret 里是 `.env` 里的真密码、
**Service 的 endpoints 只有 1 个**（迁移 Pod 因为标签不同没被算进去，16-B 那个修复在真集群上得到证实）。

**真跑抓到 3 个新 bug：**

**① `.env` 根本没被 compose 读到（真 bug，影响所有按文档写 `${VAR}` 的项目）。**
compose 在**「项目目录」**下找 `.env`，而项目目录默认就是**部署文件所在目录**——
我们的文件固定在 `.brickkit/generated/` 下，使用者的 `.env` 在项目根。
更糟的是 compose 对未定义变量**不报错**，直接替换成空串：
`password: ${PG_PASSWORD}` 变成空密码，postgres 以
`Database is uninitialized and superuser password is not specified` 崩溃重启，
而使用者手里的 `.env` 明明写着密码。设计书 006 §6.2 当时只写了"Docker Compose 自动从 .env 读取"。
已修：`UpRequest`/`DownRequest` 增加 `ProjectDir`，引擎显式传 `--project-directory`；
CLI 输出里给的 `docker compose logs` 命令同步带上（否则每次看日志先刷三行 warning）。

**② `local-debug.*.env` 里的 `${VAR}` 没求值。** 这份文件是给 **IDE** 读的，
而 VS Code 的 `envFile` 与 IntelliJ 的 EnvFile 插件**都不做变量替换**——
IDE 里的进程会拿着字面量 `${PG_PASSWORD}` 去连库。已改为生成时求值（求不出则原样保留，
不阻断——它只是调试辅助）。**与 compose 文件刻意相反**：那份文件必须留占位符，
明文密码进 git diff 就是泄露。判据统一成"**谁不做替换，就在生成时替换给谁**"。

**③ K8s 下 `status` 谎报资源不可达。** 资源地址是集群内 DNS 名（`postgres.infra`），
CLI 却从**开发者本机**拨号——对一个完全健康的部署报"不可达"，而组件正连着这个库跑着。
已改为只列地址并说明判据，附上从集群内验证的 `kubectl run ... nc -zv` 命令。
`up --check-resources` 同样处理。

**决策记录：**

| # | 决策 | 理由 |
| --- | --- | --- |
| D238 | 引擎调用一律显式传 `--project-directory` | compose 的 `.env` 查找位置跟着项目目录走，而项目目录默认是部署文件所在目录。不显式传，使用者放在项目根的 `.env` 永远读不到，且**静默**替换成空串 |
| D239 | "谁不做替换，就在生成时替换给谁" | compose 文件保留占位符（compose 会展开，且那份文件会进 git）；`local-debug.env` 与 K8s Secret 求值后写真值（IDE 与 kubectl 都不替换），文件权限 `0600` |
| D240 | `local-debug.env` 求不出值时**原样保留**，不阻断 | 它只是调试辅助，不该让整个 `up` 失败；留着占位符至少还能看出漏了哪个变量。K8s Secret 会真的部署上去，所以那边必须阻断 |
| D241 | K8s 下不从本机探测资源可达性 | 集群内 DNS 名本机解析不了。必然出现、又必然错误的警告，会让人从此不看警告 |
| D242 | 试用指南单独成册，不并进设计书 | 设计书回答"为什么这么设计"，试用指南回答"我现在该敲哪一行"。两种读法混在一起，两边都难读 |

**试用指南（[试用指南/](../../试用指南/)）：** 11 份文档 + 一个 `准备.sh`。每节结构统一为
「验证什么 / 操作 / 预期 / **怎么停** / 代码在哪」。所有命令与输出都是**真跑出来的**，
不是照着设计书抄的。试验场 `试用指南/playground/` 与 `bin/` 已加进 `.gitignore`。

---

---

### Step 16-C：K8s 目标接线（up / down / status） — ✅ 2026-08-15

**开发方式：TDD。** 先写 28 个业务行为测试（kubectl 命令序列 16.14 + 三个命令的 K8s 分支）
→ 确认全部失败 → 实现。CLI 模块 **1068 个测试**（16-B 时 1042）。

| 文件 | 内容 |
| --- | --- |
| `internal/engine/kubectl.go` | kubectl 引擎：按顺序 apply、清旧 Job、等迁移、等 rollout、按 Deployment 查状态 |
| `internal/cli/up_k8s.go` | K8s 那条路：落盘 → 建库提示 → 体检 → 部署；`envLookup` 读 `.env` |
| `internal/cli/up.go` | `plan.generate` 按 `deploy.target` 分岔；两种目标共用到注入为止的全部结论 |
| `internal/cli/lifecycle.go` | `deployedPath`（Docker 是文件、K8s 是目录）、K8s 的 `logsCommand`、命名空间 |
| `internal/deploy/database.go` | 「要先建哪些库」从 compose 里抽出来，两种目标共用（D229） |

**真实验证（本机无 kubectl / 集群）：** 用真实二进制跑了一个 `target: k8s` 的项目：
`${VAR}` 漏配时阻断并列出变量名 → 补一个 `.env` 后生成 6 份清单、Secret 权限 `0600`、
密码求值成 `.env` 里的真值 → `up` / `status` / `down` 三个命令都停在
"找不到 kubectl"并给出安装建议。**Docker 那条路用真容器复验**（生成 → 建库提示 →
迁移失败时如实报错 → 建库后全绿 → status 框线表 → down），确认这次重构没碰坏它。

**真跑发现的问题：**

**① P5 的明文密码告警是反的（真 bug，本次重构撞出来的）。** 解析器在读 brickkit.yaml 时
就把 `${ENV_VAR}` 展开成真实值了（003 §5.4），而告警拿**展开后**的值判断"是不是明文密码"——
于是：变量**配好了**才报警（展开成明文），变量**漏配**反倒不吭声（占位符原样保留）。
使用者做对了被骂、做错了没提示，正好反了。既有用例
`TestUpDoesNotWarnAboutEnvVarPassword` 之所以一直是绿的，只因为跑测试的机器上
恰好没设 `POSTGRES_PASSWORD`；这次本机设了，它立刻红了。
已改为在展开**之前**记下原文写没写 `${...}`（`config.Resource.PasswordFromEnv`），
并补了两侧的回归用例。

**决策记录：**

| # | 决策 | 理由 |
| --- | --- | --- |
| D229 | `internal/deploy` 承载两种目标共用的结论（当前只有「要先建哪些库」） | 两处各算一遍，迟早出现"docker 下提示建库、k8s 下不提示"。`compose.DatabaseRequirement` 保留为类型别名，命令层不受影响 |
| D230 | kubectl 复用 `Engine` 接口，`UpRequest.File` 在 K8s 下解释为**目录** | 命令层要的东西是一样的（起起来、停下来、现在什么状态），差别全在引擎层消化。为此 `UpRequest` 增加 `MigrationJobs`：K8s 没有 compose 的 `depends_on`，只能由 CLI 串行控制 |
| D231 | `resolveEngineFor` 按 `deploy.target` 选引擎 | K8s 与 Docker/Podman 不是"同一类引擎的两个牌子"，是两种部署目标。选错的后果 Step 16 之前撞到过一次：target: k8s 的项目被按 Docker 处理，一切"成功"，只是跑在错误的编排器上 |
| D232 | `status` 查 **Deployment** 而不是 Pod | Deployment 与组件是 1:1，名字就是版本化服务名；Pod 名带随机后缀，还要按标签反查。Pod 级细节（CrashLoopBackOff）由输出里给的 `kubectl describe` 命令承接 |
| D233 | K8s 下 `CheckImage` 是空操作 | 拉镜像的是集群的 kubelet，用集群的凭据；开发机上能不能拉到说明不了任何问题，"检查"一遍只会给出误导性结论 |
| D234 | `${VAR}` 的取值顺序：进程环境 → 项目根 `.env` | CI 靠环境变量注入真实密码，本地靠 `.env`。反过来的话，开发机上的假密码会顶掉 CI 传进来的真密码 |
| D235 | `.env` 只认最基本的 `KEY=value` | 这个文件同时被 docker compose 读；在这里支持更花哨的语法，只会让两边对同一个文件解释不一致 |
| D236 | 明文密码告警看**原文**，不看解析后的值 | 见上面的真 bug。判据必须在环境变量展开之前取 |
| D237 | K8s 下 `down` 不提"数据卷"，改说"基础资源由运维部署" | 集群里本来就没有 CLI 托管的资源，照抄 Docker 的话术只会让人以为平台动了他们的数据库 |

**设计书修订：** 005 §5.7 重写部署命令序列（每条都要 `-n`、`delete` 要 `-R`、两处 `--timeout`、
`rollout status` 不能省、只 apply 真实存在的子目录、K8s 下不检查镜像权限）；
006 §6.2 增加"明文密码要看原文"的对照表。版本：005 → 1.10.0、006 → 1.5.0。

**Step 16 收口：** 15 项验证全部通过。真集群验证仍是 **P25**（本机无 kubectl，L7）。

---

---

### Step 16-A / 16-B：K8s 清单生成 — ✅ 2026-08-15

**开发方式：TDD。** 先写 43 个业务行为测试（覆盖 16.1–16.13、16.15）→ 确认全部失败
（`no non-test Go files`）→ 实现 → 再补 14 个代码层测试。CLI 模块 **1042 个测试**（此前 985）。

**16-A（Namespace / Deployment / Secret）：**

| 文件 | 内容 |
| --- | --- |
| `internal/k8s/k8s.go` | `Generate`：配置 + 三份计算结果 → 一组 `File{Path, YAML}`；`Namespace()` = `brickkit-<项目名>` |
| `internal/k8s/deployment.go` | Deployment：标签/注解、replicas、selector、容器（镜像、端口、env、探针、配额） |
| `internal/k8s/secret.go` | 按**资源**归集的 Secret（`<资源ID>-secret`），Deployment 侧只留 `secretKeyRef` |
| `internal/k8s/expand.go` | `${VAR}` / `${VAR:-默认值}` 求值；展开不了的变量一次性列出并阻断生成 |
| `internal/inject/inject.go` | `Var` 增加 `ResourceID` 与 `SecretKey`：**哪一条是密码由注入引擎标记**，不让渲染器按变量名去猜 |

**16-B（Service / Ingress / 迁移 Job / 落盘）：**

| 文件 | 内容 |
| --- | --- |
| `internal/k8s/service.go` | Service（ClusterIP，端口一律带 name，含 extraPorts）+ Ingress（仅 `expose: true`）+ hostname 缺失拦截 |
| `internal/k8s/job.go` | 迁移 Job：`backoffLimit: 0`、`restartPolicy: Never`、命令整条替换 ENTRYPOINT、env 与主容器一致 |
| `internal/k8s/write.go` | `WriteFiles`：先清空再写；Secret 文件 `0600`，其余 `0644` |

**真跑发现的问题（生成物逐份人工核对，本机无集群）：**

**① 设计书里的标签是非法的（真 bug）。** 005 §5.3 的样例写着
`brickkit.io/component-id: people/basic`——**K8s 的标签「值」不允许出现 `/`**
（斜杠只在标签「键」的前缀里合法）。这份 Deployment 会被 API Server **整份拒绝**，
报的还是一句通用的 `a valid label must consist of alphanumeric characters...`，
完全看不出是组件 ID 的锅。已改为标签 `brickkit.io/component: people-basic`（服务名写法）
+ 注解 `brickkit.io/component-id: people/basic`（原样），并加了一条**扫描所有生成文件、
逐个标签值验正则**的测试兜底。设计书 005 / 004 / 附录合集共 9 处已修。

**② 迁移 Pod 会被当成组件 Service 的后端（我自己实现的 bug，人工核对生成物时发现）。**
一开始让迁移 Job 复用了组件的标签，于是 Pod 带着 `app: people-basic-1-0-0`——
而 Service 的 selector 正是它。迁移 Pod 没有就绪探针，K8s 认为它随时可用，
**迁移期间打到该组件的请求会有一部分被转发给一个根本不监听端口的 Pod**，
表现成偶发的 connection refused，而所有 YAML 看上去都没问题。
改为 `app: <服务名>-migration` + `brickkit.io/role: migration`，并加了回归测试。

**③ 设计书的 Secret 会把字面量当成密码部署上去。** 005 §5.6 原样写着
`stringData: password: ${PEOPLE_DB_PASSWORD}   # 从 .env 或环境变量获取`——
但 **kubectl 不做任何变量替换**（`docker compose` 才会自己读 `.env`）。
照抄的后果是 Pod 拿着字符串 `"${PEOPLE_DB_PASSWORD}"` 去连库，认证失败反复重启。
已改为在**生成时**求值，并把这条 Docker/K8s 的根本差别写成对照表（005 §5.6）。

**决策记录：**

| # | 决策 | 理由 |
| --- | --- | --- |
| D219 | K8s 生成器放 `internal/k8s`，与 `internal/compose` 并列 | 开发计划 §0.4 画的是 `internal/generator/{compose,k8s}`，但 Step 12 落地时就没建这层（`internal/compose`）。两处只有一个的话，"和 compose 并列"比"和计划里的图一致"更重要；已删掉 `internal/generator/` 下的两个空占位目录 |
| D220 | 敏感变量由**注入引擎**标记（`Var.SecretKey`），不由渲染器按变量名猜 | 谁生成的谁最清楚哪一条是密码。靠 `HasSuffix(name, "_PASSWORD")` 去猜，早晚会漏掉一种资源（对象存储的密钥就叫 `STORAGE_SECRET_KEY`），漏掉的后果是密码明文写进 Deployment |
| D221 | Secret 按**资源**归集，不按组件 | 同一个数据库被三个组件用到时密码只该有一份；按组件归集会生成三份内容相同的 Secret，改密码时改漏一份就是一次线上故障 |
| D222 | `${VAR}` 展开不了就**阻断生成**，不留占位符 | 留下的是一份"看着完全正常、跑起来必错"的清单。缺失的变量一次全报，与配置校验的做法一致 |
| D223 | `local: true` + `target: k8s` **报错**，不静默跳过 | 集群里的 Pod 连不到开发者的笔记本。跳过的话依赖方会拿到一个指向不存在 Service 的地址，表现成随机的连接超时，极难查 |
| D224 | `expose: true` 缺 `hostname` 在生成阶段再拦一次 | 配置校验已有同样的规则，但生成器是最后一道闸：一条没有 host 的 Ingress 会匹配**所有**进入集群的域名，一个内部组件可能就这样顶掉门户站点，而 `kubectl apply` 不会有任何抱怨 |
| D225 | Service 端口一律带 `name` | K8s 要求"要么都有名字、要么只有一个端口"。等组件加了 extraPorts 再补名字，是一次会改到既有端口定义的改动 |
| D226 | 迁移 Job 用 `brickkit.io/role: migration`，替换设计书原来的 `brickkit.io/migration: "true"` | 布尔标签不可扩展；后面若有别的一次性任务（种子数据、一次性修复）还得再加一个布尔量 |
| D227 | `WriteFiles` 先整个删掉再写 | 组件从 brickkit.yaml 删掉后，上次生成的清单若还留着，`kubectl apply -f k8s/` 会把它**又部署一遍**——使用者明明已经移除了组件，集群里却还跑着 |
| D228 | Secret 文件权限 `0600`，其余 `0644` | 里面是求值后的明文密码 |

**设计书修订：** 005 §5.3（标签样例 + 新增 §5.3.1「标签值不能带斜杠」，含探针差别与
`local: true` 不可用）、§5.4（端口 name 规则）、§5.6（Secret 求值 + 归集 + 权限 + stringData 理由）、
§6.3（迁移 Pod 标签 + 不生成探针端口）；004 与附录合集的 K8s 样例同步；003 §4.4 补
`local: true` 只能配 docker。版本：005 → 1.9.0、004 → 1.11.2、003 → 1.6.1、附录 → 1.7.3。

**未做（明确记录）：** 16.14（执行前清理旧 Job）与 `up`/`down`/`status` 的 K8s 接线属 **16-C**（已完成，见上一节）；
真实集群验证登记为 **P25**（本机无 kubectl / 集群，见 L7）。

---
