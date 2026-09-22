# 故障排除

覆盖 `brickkit up` / `down`、本地调试、签名验证和离线检查（`brickkit lint` 与编辑器 schema）里最容易踩的失败模式。这里没有的问题，先看对应命令的 `--help`，或者去看[架构文档](06-architecture/00-overview.md)里那个机制具体怎么设计的。

如果你手里的是一段 `❌` 错误块，而不是一个症状：它后面紧跟的那行 JSON 日志里的 `error_code`，就是[错误码](06-architecture/10-error-codes.md)的索引——那一篇覆盖了每个错误码，以及每个码底下的各种情形。

**怎么用这一页：** 先在下面的总表里按症状找，点第一列的名字，跳到对应编号的详细说明。每一条都是"症状 → 原因 → 解决"。

### A. `brickkit up` 失败

| 症状 | 一句话原因 | 怎么解 |
| --- | --- | --- |
| [1. 镜像拉不到](#1-镜像拉不到)<br>`No such image` / `pull access denied` | 镜像还没构建，或者仓库你访问不了 | 先 `docker build`；检查 `deployment.image`；私有仓库先 `docker login` |
| [2. 宿主机端口被多个组件占用](#2-宿主机端口被多个组件占用)<br>生成阶段就报错 | 两个 `expose: true` 的组件用了同一个宿主机端口 | 给其中一个设不同的 `exposePort`，或去掉它的 `expose` |
| [3. 迁移失败](#3-迁移失败)<br>主服务没有启动 | 数据库连不上，或迁移脚本本身有错 | 看迁移容器 / Job 的日志，修好后重新 `up` |
| [4. 依赖组件访问不到](#4-依赖组件访问不到)<br>`connection refused` / `no such host` | 依赖没起来，或没在 `brickkit.yaml` 里声明 | `brickkit status` 看依赖；弱依赖要用 `.get()` 读 |
| [5. 必填的组件配置没有值](#5-必填的组件配置没有值)<br>`up` 被拦下 | 组件声明某项必填、又没有默认值，项目里也没填 | 在 `brickkit.yaml` 那个组件的 `config` 下补上 |
| [6. brickkit.yaml 里引用的环境变量没有定义](#6-brickkityaml-里引用的环境变量没有定义)<br>`up` 被拦下 | 某个 `${VAR}` 求不出值 | 在 `.env` 里定义、`export`，或写默认值 `${VAR:-dev}` |

### B. `up` 成功了，但组件不对劲

| 症状 | 一句话原因 | 怎么解 |
| --- | --- | --- |
| [7. 一直 unhealthy，可组件日志说已就绪](#7-一直-unhealthy可组件日志说已就绪) | 镜像里没有探测用的命令，或启动比宽限期慢 | 装上 `wget` / `curl`；或调大 `healthCheck.startPeriodSeconds` |
| [8. 改了 config，但什么都没发生](#8-改了-config但什么都没发生) | 键名写错了，这一项没被注入，组件用了默认值 | 看 `up` 的警告，改成 `configSchema` 里的键；改完要重启 |
| [9. `docker compose logs` 什么都看不到](#9-docker-compose-logs-什么都看不到) | 没带项目名，Compose 在看另一个项目 | 加上 `-p brickkit-<项目名>` |

### C. Kubernetes

| 症状 | 一句话原因 | 怎么解 |
| --- | --- | --- |
| [10. 网络策略生成了，流量却没被挡住](#10-网络策略生成了流量却没被挡住) | 集群的网络插件（CNI）不执行 NetworkPolicy | 换一个会执行的 CNI，再验证被挡与不被挡 |

### D. `brickkit down` 之后

| 症状 | 一句话原因 | 怎么解 |
| --- | --- | --- |
| [11. Volume 还在](#11-volume-还在) | `down` 从不主动删卷，为了保护数据 | 要清理就手动 `docker volume rm` |
| [12. K8s namespace 还在](#12-k8s-namespace-还在) | `createNamespace: false`：不是 BrickKit 建的，就不会去删 | 想让它管，改回 `true`；否则自己 `kubectl delete` |

### E. 本地调试模式（`mode: debug`）

| 症状 | 一句话原因 | 怎么解 |
| --- | --- | --- |
| [13. `extra_hosts` 不生效](#13-extra_hosts-不生效) | Docker 太旧，不支持 `host-gateway` | 升级到 20.10 以上 |
| [14. 端口对不上](#14-端口对不上) | 本地进程实际监听的端口和 `localPort` 不一致 | 让两者一致 |
| [15. 迁移没跑](#15-迁移没跑)<br>`relation does not exist` | `mode: debug` 的组件不生成迁移容器 | 第一次跑之前自己手动跑一次迁移 |

### F. 签名验证失败

| 症状 | 一句话原因 | 怎么解 |
| --- | --- | --- |
| [16. 公钥不对](#16-公钥不对)<br>`brickkit add` 报签名验证失败 | `installer.publicKeys` 的公钥和发布者的私钥不是一对 | 找发布者确认正确的公钥，更新路径 |

### G. `brickkit lint` 与编辑器 schema

| 症状 | 一句话原因 | 怎么解 |
| --- | --- | --- |
| [17. `brickkit lint` 说没问题，`up` 却失败](#17-brickkit-lint-说没问题up-却失败)<br>`错误：强依赖缺失` | `lint` 只看每份文件自己；依赖或 `servedBy` 目标在不在，要看整张依赖图 | `brickkit up --dry-run`（或 `brickkit graph`）会解析依赖图并点名缺了什么 |
| [18. 合法的 `component.yaml` 被编辑器几乎处处画红线](#18-合法的-componentyaml-被编辑器几乎处处画红线)<br>`Property apiVersion is not allowed.` | 没接上 BrickKit 的 schema，编辑器给这个文件名套了别的工具的 schema | 用 `$schema` 注释或 `yaml.schemas` 设置接上 schema |
| [19. 编辑器标红了 CLI 接受的写法](#19-编辑器标红了-cli-接受的写法) | schema 在三处有意比 CLI 更严 | 写字面值、给数字加引号，或者改对键名 |

---

### 1. 镜像拉不到

- **症状：** `Error: No such image: xxx` 或 `Error: pull access denied`
- **原因：** 组件镜像还没构建，或者 Manifest 里的 `deployment.image` 指向的仓库你访问不了。
- **解决：**
  - 本地测试用的组件先自己 `docker build`（发布的组件本来就该已经在镜像仓库里，CLI 从不替你构建）。
  - 检查 `deployment.image` 的拼写和 tag 是否正确。
  - 私有镜像仓库需要先 `docker login`。
- **错误码：** `IMAGE_UNAUTHORIZED`——CLI 通常会把 Docker 的原始报错翻译成 `错误：镜像不存在` 或 `错误：镜像拉取未授权`；见[错误码](06-architecture/10-error-codes.md#image_unauthorized)。

---

### 2. 宿主机端口被多个组件占用

- **症状：** `brickkit up`（或 `--dry-run`）在生成阶段就直接报错。具体打印什么，取决于组件自己有没有写 `exposePort`。
- **原因：** 两个 `expose: true` 的组件用了同一个宿主机端口。两种情形都是在 CLI 还在生成阶段就会被挡下来，不会等到 Docker 启动第二个容器时才失败——所以你不会看到 Docker 自己的 "port is already allocated"。
- **解决：** 给其中一个组件加 `exposePort: <不同端口>`，或者去掉其中一个的 `expose: true`（组件之间在容器网络内互相访问本来就不需要 expose）。
- **错误码：** 两个都没写 `exposePort` 时是 `PORT_CONFLICT`，两个都显式写了同一个 `exposePort` 时是 `CONFIG_INVALID`；见[错误码](06-architecture/10-error-codes.md)。

两个都没写 `exposePort` 时，都默认用自己的 `deployment.port`：

```
❌ 错误：宿主机端口 8080 被多个组件占用
   组件：demo/caller@1.0.0
   组件：demo/hello@1.0.0
   宿主机端口：8080
   建议：
   1. 在 brickkit.yaml 中给其中一个组件设置不同的 exposePort
   2. 或去掉其中一个组件的 expose: true（组件之间在容器网络内互访不需要 expose）
```

两个都显式写了同一个 `exposePort` 时：

```
❌ 错误：brickkit.yaml 校验失败
   文件：brickkit.yaml
   components[1].exposePort：与 components[0].exposePort 冲突（宿主机端口 9000 已被占用）
```

如果看到的确实是 Docker 原生的 `Error: port is already allocated`（不是上面这两条 BrickKit 自己的报错），说明占用端口的是 BrickKit 管理之外的另一个进程或容器——`lsof -i :<端口>` 或 `docker ps` 查一下是谁占的。

---

### 3. 迁移失败

- **症状：** 迁移 Job / 容器失败，主服务没有启动。
- **原因：** 数据库连接失败，或迁移脚本本身有错误。
- **解决：**
  - Docker：`docker logs <迁移容器名>` 查看日志。
  - K8s：`kubectl logs job/<迁移-job-名>` 查看日志。
  - 修好迁移脚本后重新 `brickkit up`。
  - K8s 下不用你手动删旧 Job：CLI 自己会先清理掉任何残留的旧 Job（等价于 `kubectl delete job --ignore-not-found`），保证幂等。
- **错误码：** K8s 上是 `MIGRATION_FAILED`；Docker 上同样的失败以 `ENGINE_FAILED` 出现——见[错误码](06-architecture/10-error-codes.md#migration_failed)。

---

### 4. 依赖组件访问不到

- **症状：** 组件启动后调用依赖时 `connection refused` 或 `no such host`。
- **原因：** 依赖组件没起来，或者根本没在 `brickkit.yaml` 里声明。
- **解决：**
  - `brickkit status` 看依赖组件是不是 `healthy`。
  - 检查 `brickkit.yaml` / `component.yaml` 里依赖是否正确声明。
  - 如果是弱依赖：组件代码必须用 `os.environ.get()`，而不是 `os.environ["X"]`——弱依赖缺失时这个环境变量根本不会被注入，用后者会直接 `KeyError` 崩溃。这是设计好的行为，不是 bug（见 AGENTS.zh.md §9.13）。[教程第 2 篇](03-guide/02-what-runs.md)有 `brickkit up` 遇到弱依赖缺失时打出的真实警告。

---

### 5. 必填的组件配置没有值

- **症状：** `brickkit up` 直接拒绝生成任何东西，报 `错误：必填的组件配置没有值`。
- **原因：** 组件在 `configSchema.required` 里声明了某一项必填，又没有给默认值，项目里也没填。这一项平台推导不出来（比如另一个项目里某个服务的地址），只能由项目提供。放行的后果是组件照常启动、看上去完全健康，只是有一条调用路径永远走不通，所以平台选择在这里拦下。
- **解决：** 在 `brickkit.yaml` 里那个组件的 `config` 下补上；值里可以写 `${ENV_VAR}`，真值放 `.env`。
- **错误码：** `CONFIG_INVALID`；见[错误码](06-architecture/10-error-codes.md#config_invalid)。

真实的错误块（缺的是 `shop/pricing` 的 `pricingServiceUrl`）：

```
❌ 错误：必填的组件配置没有值
   缺少配置：shop/pricing@1.0.0 → pricingServiceUrl（注入为 PRICING_SERVICE_URL）
   原因：组件在 configSchema.required 里声明了它，又没有给默认值——这一项平台推导不出来，只能由项目提供
   建议：
   1. 在 brickkit.yaml 里给它一个值：
    components:
      - id: shop/pricing
        config:
          pricingServiceUrl: <值>
   2. 值里可以写 ${ENV_VAR}，真值放 .env
```

---

### 6. brickkit.yaml 里引用的环境变量没有定义

- **症状：** `brickkit up` 被拦下，报 `错误：brickkit.yaml 里引用的环境变量没有定义`。
- **原因：** `brickkit.yaml` 里某个 `${VAR}` 求不出值。K8s 的清单没法把替换推迟到运行时，所以在生成时就要求出来。
- **解决：** 在项目根的 `.env` 里定义它、`export` 出来，或者写上默认值：`${VAR:-dev}`。
- **错误码：** `CONFIG_INVALID`；见[错误码](06-architecture/10-error-codes.md#config_invalid)。

---

### 7. 一直 unhealthy，可组件日志说已就绪

- **症状：** `brickkit up` 报 `错误：部分组件没有正常启动`（Docker 下 `up -d --wait` 见到 `unhealthy` 就会失败），或 `brickkit status` 里一直是 `unhealthy` / `starting`。可是组件自己的日志一路正常，最后一行往往正好是"服务已启动"。
- **原因：** 最常见的是这两种：
  - **镜像里没有探测用的命令。** 健康检查是在**容器内部**跑的：HTTP 检查执行 `wget -q --spider <地址> || curl -fsS <地址>`，TCP 检查执行 `nc -z localhost <端口>`。`python:slim`、各种 distroless 这类精简镜像里往往一个都没有，探测就永远失败，尽管组件本身好好的。
  - **组件启动比宽限期慢。** `healthCheck.startPeriodSeconds` 默认是 60 秒。Spring Boot 冷启动、Django 预加载很重、.NET 首次 JIT 这类组件可能超过它。Docker 下 `up -d --wait` 直接失败；Kubernetes 下 livenessProbe 把 Pod 杀掉重启、再走一遍同样的时间，就是永久的 CrashLoopBackOff——而容器日志一路正常。
- **解决：**
  - 先 `brickkit status` 看状态。
  - 再看容器日志和最近几次探测的输出：`docker inspect --format '{{json .State.Health}}' <容器名>`。
  - 缺命令：往镜像里装 `wget` 或 `curl`（HTTP 检查），或 `nc`（TCP 检查）。
  - 启动慢：把 `healthCheck.startPeriodSeconds` 调到比实际冷启动更长。宽限期只推迟"判死"，不推迟"判活"——两秒就绪的组件照样两秒后转 healthy——所以给足没有代价。
  - `/healthz` 只能检查进程自己，别去探数据库或别的依赖：否则一个依赖一抖，所有上游会一起被判成不健康、一起重启。
  - 字段的完整约束见 [component.yaml 字段参考](06-architecture/07-component-yaml-reference.md)，两个引擎各自怎么生成探针见[部署文件生成](06-architecture/03-deployment-generation.md)。

---

### 8. 改了 config，但什么都没发生

- **症状：** 改了 `brickkit.yaml` 里某个组件的 `config`，重新 `up` 之后，组件的行为一点没变。
- **原因：** 最常见的是**键名写错了**：组件的 `configSchema` 里没有这个键，这一项不会被注入任何环境变量，组件用它自己的默认值照常运行，没有任何报错。CLI 会警告，并猜你想写的是哪一个。组件根本没声明 `configSchema` 时，整块 `config` 都不会生效，同样会警告。
- **解决：**
  - 看 `brickkit up`（或 `--dry-run`）打出的警告，把键名改成 `configSchema` 里的那个。
  - 改完要 `brickkit up` 重启才生效：配置是以环境变量注入的，没有热更新。
  - 警告的完整种类见[环境变量注入契约](06-architecture/04-environment-variables.md)第五节。

把 `greeting` 写成了 `greetting` 时，CLI 的真实警告：

```
⚠️ config 里有配置项不会生效：组件 demo/hello 的 greetting
   配置项：greetting
   原因：组件的 configSchema 里没有这一项，是不是想写 greeting？
   影响：这一项不会被注入任何环境变量；组件会使用它自己的默认值
```

---

### 9. `docker compose logs` 什么都看不到

- **症状：** 组件明明在跑，`docker compose logs` 却什么都不输出。
- **原因：** BrickKit 起的 Compose 项目名是 `brickkit-<项目名>`。你没带 `-p`，Compose 看的是另一个（默认的）项目。
- **解决：** 加上项目名：`docker compose -p brickkit-<项目名> logs`（项目名就是 `brickkit.yaml` 里的 `project`）。也可以直接 `docker logs <容器名>`。

---

### 10. 网络策略生成了，流量却没被挡住

- **症状：** `deploy.networkPolicy.enabled: true` 之后，`brickkit up` 报"已生成 N 份 NetworkPolicy"，并带一段警告；`kubectl get networkpolicy` 也看得见，可没被授权的访问照样能通，没有任何报错。
- **原因：** 集群的网络插件（CNI）不执行 NetworkPolicy。很多集群接受这些对象却完全不执行，minikube 和 kind 的**默认** CNI 就属于这一类。Kubernetes 没有提供查询这件事的 API，平台测不出来，所以 `up` 每次都会警告，让你自己验一次。
- **解决：**
  - 换一个会执行 NetworkPolicy 的 CNI（[教程第 13 篇](03-guide/13-network-policy.md)用的是 `minikube start --cni=calico`）。
  - 然后按教程里的办法验证：授权的路径照常能用，未授权的路径真的被挡住。

`brickkit up` 打出的警告：

```
🔒 已生成 2 份 NetworkPolicy（deploy.networkPolicy.enabled: true）
   ⚠️ 它们只在集群的 CNI 支持执行时才有效。不支持时：apply 会成功、
      kubectl get networkpolicy 看得见、而流量完全不受限制——没有任何报错。
      minikube / kind 的**默认** CNI 就属于这一类。
   平台测不出来（K8s 没有这个 API），只能你自己验一次
```

---

### 11. Volume 还在

- **症状：** 重新 `brickkit up` 后，数据库里的数据还在。
- **原因：** `down` 从来不主动删卷——这是为了保护数据，不是遗漏。
- **解决：** 需要彻底清理时手动执行 `docker volume rm <卷名>`，或者 `docker compose -p brickkit-<项目名> down -v`。

---

### 12. K8s namespace 还在

- **症状：** `brickkit down` 之后 K8s namespace 还在。
- **原因：** 大概率是 `brickkit.yaml` 里 `deploy.createNamespace` 被设成了 `false`——这种情况下 namespace 是运维手动建的，不是 BrickKit 自己创建的，`down` 就不会去删它（不是自己建的就不该自己删）。这不是失败，是设计好的行为。
- **解决：** 如果确实想让 BrickKit 管理这个 namespace 的生命周期，把 `createNamespace` 设为 `true`（默认值）。已经手动创建的 namespace 需要清理，自己 `kubectl delete namespace <name>`。

---

### 13. `extra_hosts` 不生效

- **症状：** 容器里解析不到本地组件的服务名。
- **原因：** Docker 版本过低，不支持 `host-gateway`。
- **解决：** 升级 Docker 到 20.10 以上。

---

### 14. 端口对不上

- **症状：** 容器调的是 `demo-hello-1-0-0:8080`，但本地进程实际监听在别的端口，调用方一直拿到 503。
- **原因：** `brickkit.yaml` 里这个组件的 `localPort`，和本地进程真实监听的端口不一致。
- **解决：** 让两者一致。

---

### 15. 迁移没跑

- **症状：** `mode: debug` 的组件报 `relation does not exist` 之类的错误。
- **原因：** `mode: debug` 的组件不会生成迁移容器 / Job——它跑在你自己的 IDE 里，CLI 管不到。
- **解决：** 第一次跑之前，自己手动执行一次迁移脚本。

---

### 16. 公钥不对

- **症状：** `brickkit add` 报签名验证失败。
- **原因：** `brickkit.yaml` 里 `installer.publicKeys` 指向的公钥，和发布者签名时用的私钥不是一对。
- **解决：** 找发布者确认正确的公钥（通常是 `<组件名>-release.pub` 这样的文件），更新 `installer.publicKeys` 里的路径。
- **提醒：** `publicKeys` 是唯一真正让签名校验生效的字段——一个公钥都没配的话，`requireSignature: true` 什么都不做（CLI 会警告一次，但不会替你补上信任锚点）。
- **错误码：** `SIGNATURE_INVALID`——见[错误码](06-architecture/10-error-codes.md#signature_invalid)。

---

### 17. `brickkit lint` 说没问题，`up` 却失败

- **症状：** `brickkit lint` 给每个文件都打了 `✅`、退出码 `0`，随后 `brickkit up`（或 `--dry-run`）却停在 `错误：强依赖缺失`——或者 `错误：servedBy 指向的组件不存在`。
- **原因：** `lint` 只检查每份文件自己的结构，别的都不管。依赖能不能在某个安装源里找到、`servedBy` 目标在不在，取决于项目里其余组件的 Manifest（市场和 Git 组件还要联网），所以 `lint` 从不去看——去看的话它就不再是离线的了。`lint` 干净只说明每份 YAML 都写得合规，不说明项目能起得来。
- **解决：** 跑 `brickkit up --dry-run`。它会解析依赖图，点出是哪个组件、缺哪个依赖、试过哪些安装源。用 `brickkit add` 把缺的组件补上，或者把声明里的组件 ID、版本号改对。（`brickkit graph` 解析的是同一张图，缺强依赖时它同样会停下；`servedBy` 目标不存在时它照声明把分组画出来，报错留给 `up`。）
- **错误码：** `DEPENDENCY_MISSING`（`servedBy` 目标不存在时是 `CONFIG_INVALID`）；见[错误码](06-architecture/10-error-codes.md#dependency_missing)。

这里 `people/basic` 需要的 `department/tree` 不在任何安装源里：

```
$ brickkit lint
✅ brickkit.yaml
✅ components/people/basic/component.yaml

📋 检查了 2 个文件：0 个有错误，0 条警告
$ brickkit up --dry-run
🚀 启动项目 demo-shop（deploy.target: docker）
❌ 错误：强依赖缺失
   组件：people/basic@1.0.0
   缺失依赖：department/tree@1.0.0
   原因：该组件在所有安装源中均未找到
   已尝试的安装源：local-dev（local）
   建议：
   1. 检查安装源配置（brickkit.yaml → sources）
   2. 确认组件是否已发布到市场
   3. 确认版本号是否正确
```

---

### 18. 合法的 `component.yaml` 被编辑器几乎处处画红线

- **症状：** 你打开一份 `brickkit lint` 认可的 `component.yaml`，编辑器却几乎处处画红线：`apiVersion` 上是 `Property apiVersion is not allowed.`，文件开头还有 `Missing property "implementation".`。
- **原因：** 没有接上 BrickKit 的 schema，YAML language server 就退回去用 SchemaStore（一个公开的 schema 目录）。截至 yaml-language-server 1.24.0 和写作时的那份目录，它把 `component.yaml` 这个文件名对应到了 Kubeflow Pipelines 的 schema（两者都是第三方的，会变）。这些提示说的是 Kubeflow 的字段，不是 BrickKit 的。（同一次检查里，目录里没有 `brickkit.yaml` 的同名条目，所以在你接上 schema 之前它只是完全没有检查。）
- **解决：** 接上 BrickKit 的 schema——在文件第一行写 `# yaml-language-server: $schema=…` 注释，或者用 `yaml.schemas` 设置把 `component.yaml` 映射过去。两种都会换掉目录的那个猜测；写法都在[给编辑器接上自动补全](00-quick-start.md#给编辑器接上自动补全)里。

---

### 19. 编辑器标红了 CLI 接受的写法

- **症状：** `brickkit.yaml` 或 `component.yaml` 里有一条红线，可 `brickkit lint` 和 `brickkit up` 都没有任何意见。
- **原因：** schema 在三处有意比 CLI 更严：
  - 封闭取值的字段里写了 `${VAR}`（`deploy.target: ${TARGET}`；`sources[].type`、`resources[].kind` 也一样）——CLI 先展开变量再校验，schema 校验的是字面文本；
  - CLI 读得很宽松的 YAML——字符串字段里不加引号的数字（`project: 2024`）、布尔值写成 `yes` 或 `on`、列表里的 `null` 元素或 map 里的 `null` 值、整数字段里写小数（`port: 5432.5`）；
  - `configSchema` 配置项声明里的多余键（把 `default` 拼成 `defualt`）——CLI 忽略它，`brickkit lint` 会警告它不会生效。
- **解决：** 把红线当成提示，不是 bug：写字面值、给数字加引号、用 `true` / `false`，或者把键名改对。每一种的来龙去脉，见[给编辑器接上自动补全](00-quick-start.md#给编辑器接上自动补全)里最后那个列表。

---

## 深入阅读

- [错误码](06-architecture/10-error-codes.md)
- [签名与信任模型](06-architecture/06-signing-and-trust.md)
- [依赖解析与启动顺序](06-architecture/02-dependency-resolution.md)
- [部署文件生成](06-architecture/03-deployment-generation.md)
