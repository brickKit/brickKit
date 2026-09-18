# 故障排除

覆盖 `brickkit up`/`down` 和签名验证里最容易踩的失败模式。这里没有的问题，先看对应命令的 `--help`，或者去看 [架构文档](architecture/overview.md) 里那个机制具体怎么设计的。

如果你手里的是一段 `❌` 错误块，而不是一个症状：它后面紧跟的那行 JSON 日志里的 `error_code`，就是 [错误码](architecture/error-codes.md) 的索引——那一篇覆盖了每个错误码，以及每个码底下的各种情形。

## `brickkit up` 失败

### 镜像拉不到

**症状：** `Error: No such image: xxx` 或 `Error: pull access denied`

**原因：** 组件镜像还没构建，或者 Manifest 里的 `deployment.image` 指向的仓库你访问不了。

**解决：**
- 本地测试用的组件先自己 `docker build`（发布的组件本来就该已经在镜像仓库里，CLI 从不替你构建）
- 检查 `deployment.image` 拼写和 tag 是否正确
- 私有镜像仓库需要先 `docker login`

**错误码：** `IMAGE_UNAUTHORIZED`——CLI 通常会把 Docker 的原始报错翻译成 `错误：镜像不存在` 或 `错误：镜像拉取未授权`；见[错误码](architecture/error-codes.md#image_unauthorized)。

### 同一个宿主机端口被多个组件占用

**症状：** `brickkit up`（或 `--dry-run`）在生成阶段就直接报错。具体打印什么，取决于组件自己有没有写 `exposePort`。两个都没写时，都默认用自己的 `deployment.port`：

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

**原因：** 两个 `expose: true` 的组件用了同一个宿主机端口。两种情形都是在 CLI 还在生成阶段就会被挡下来，不会等到 Docker 启动第二个容器时才失败——所以你不会看到 Docker 自己的 "port is already allocated"。第一种是 `PORT_CONFLICT`，第二种是 `CONFIG_INVALID`；见[错误码](architecture/error-codes.md)。

**解决：** 给其中一个组件加 `exposePort: <不同端口>`，或者去掉其中一个的 `expose: true`（组件之间在容器网络内互相访问本来就不需要 expose）。

如果看到的确实是 Docker 原生的 `Error: port is already allocated`（不是上面这条 BrickKit 自己的报错），说明占用端口的是 BrickKit 管理之外的另一个进程或容器——`lsof -i :<端口>` 或 `docker ps` 查一下是谁占的。

### 迁移失败

**症状：** 迁移 Job/容器失败，主服务没有启动

**原因：** 数据库连接失败，或迁移脚本本身有错误

**解决：**
- Docker：`docker logs <迁移容器名>` 查看日志
- K8s：`kubectl logs job/<迁移-job-名>` 查看日志
- 修好迁移脚本后重新 `brickkit up`。K8s 下 CLI 自己会先清理掉任何残留的旧 Job（等价于 `kubectl delete job --ignore-not-found`），保证幂等，不用你手动删

**错误码：** K8s 上是 `MIGRATION_FAILED`；Docker 上同样的失败以 `ENGINE_FAILED` 出现——见[错误码](architecture/error-codes.md#migration_failed)。

### 依赖组件访问不到

**症状：** 组件启动后调用依赖时 `connection refused` 或 `no such host`

**原因：** 依赖组件没起来，或者根本没在 `brickkit.yaml` 里声明

**解决：**
- `brickkit status` 看依赖组件是不是 `healthy`
- 检查 `brickkit.yaml`/`component.yaml` 里依赖是否正确声明
- 如果是弱依赖：组件代码必须用 `os.environ.get()` 而不是 `os.environ["X"]`——弱依赖缺失时这个环境变量根本不会被注入，用后者会直接 `KeyError` 崩溃（这是设计好的行为，不是 bug，见 AGENTS.md §9.13）。[教程第 2 篇](guide/02-what-runs.md) 有 `brickkit up` 遇到弱依赖缺失时打出的真实警告。

## `brickkit down` 之后

### Volume 还在

**症状：** 重新 `brickkit up` 后，数据库里的数据还在

**原因：** `down` 从来不主动删卷——这是为了保护数据，不是遗漏

**解决：** 需要彻底清理时手动执行 `docker volume rm <卷名>`，或者 `docker compose -p brickkit-<项目名> down -v`

### K8s namespace 还在

**症状：** `brickkit down` 之后 K8s namespace 还在

**原因：** 大概率是 `brickkit.yaml` 里 `deploy.createNamespace` 被设成了 `false`——这种情况下 namespace 是运维手动建的，不是 BrickKit 自己创建的，`down` 就不会去删它（不是自己建的就不该自己删）。这不是失败，是设计好的行为。

**解决：** 如果确实想让 BrickKit 管理这个 namespace 的生命周期，把 `createNamespace` 设为 `true`（默认值）。已经手动创建的 namespace 需要清理，自己 `kubectl delete namespace <name>`。

## 本地调试模式（`local: true`）问题

### `extra_hosts` 不生效，容器里解析不到本地组件的服务名

**原因：** Docker 版本过低，不支持 `host-gateway`

**解决：** 升级 Docker 到 20.10 以上

### 端口对不上

**症状：** 容器调的是 `demo-hello-1-0-0:8080`，但本地进程实际监听在别的端口

**解决：** 检查 `brickkit.yaml` 里这个组件的 `localPort` 是否和本地进程真实监听的端口一致

### 迁移没跑

**症状：** `local: true` 的组件报 `relation does not exist` 之类的错误

**原因：** `local: true` 的组件不会生成迁移容器/Job——它跑在你自己的 IDE 里，CLI 管不到

**解决：** 第一次跑之前自己手动执行一次迁移脚本

## 签名验证失败（`brickkit add`）

### 公钥不对

**症状：** `brickkit add` 报签名验证失败

**原因：** `brickkit.yaml` 里 `installer.publicKeys` 指向的公钥，和发布者签名时用的私钥不是一对

**解决：** 找发布者确认正确的公钥（通常是 `<组件名>-release.pub` 这样的文件），更新 `installer.publicKeys` 里的路径

**提醒：** `publicKeys` 是唯一真正让签名校验生效的字段——一个公钥都没配的话，`requireSignature: true` 什么都不做（CLI 会警告一次，但不会替你补上信任锚点）。

**错误码：** `SIGNATURE_INVALID`——见[错误码](architecture/error-codes.md#signature_invalid)。

## 深入阅读

- [错误码](architecture/error-codes.md)
- [签名与信任模型](architecture/signing-and-trust.md)
- [依赖解析与启动顺序](architecture/dependency-resolution.md)
- [部署文件生成](architecture/deployment-generation.md)
