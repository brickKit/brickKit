---
name: brickkit-troubleshoot
description: brickkit 命令报错、组件起不来、地址注入不生效、依赖解析失败、或需要按错误码定位问题时使用。含错误码到处理动作的映射、常见故障的排查顺序、以及哪些「看起来像 bug」其实是被刻意设计成这样的行为。当用户贴出 brickkit 的报错输出、或说「为什么起不来 / 连不上 / 找不到」时，这个技能适用。
---

# 排障

## 什么时候用这个技能

- 用户贴出一段 `brickkit` 的报错
- 组件起不来，或者起来了但连不上依赖
- 环境变量没注入
- `up` 之后跑起来的组件跟预期不一样
- Pod 一直 CrashLoopBackOff，但容器日志看着正常

## 先看这一条：有些「故障」是刻意设计

在开始查之前，确认它不是下面这几种**按设计如此**的行为。把它们当 bug 去修会越修越坏。

**1. 弱依赖缺失时环境变量完全不存在，不是空字符串。**

组件里 `os.environ["X"]` 抛 `KeyError` 然后崩掉——这是刻意的，让「依赖不在」在启动时
就暴露，而不是变成一个连向空地址的运行时谜题。修法是在组件代码里改成
`os.environ.get()` 并写降级逻辑，**不是**让平台注入空值。

**2. Pod 永久 CrashLoopBackOff 而容器日志一路正常。**

几乎总是启动预算问题。平台固定 `interval`/`timeout`/`failureThreshold` 为 10s/3s/3，
相乘 = **30 秒**默认启动预算。冷启动超过它的组件（Spring Boot、Django 预加载、
.NET 首次 JIT）会被 kill 重启，再走一遍同样的 30 秒。

修法：在 `component.yaml` 的 `healthCheck` 里写 `startPeriodSeconds`（默认 60，
写大一点没有代价——宽限期只推迟「判死」不推迟「判活」）。

**3. 没有注册中心、没有常驻服务、没有配置中心、没有网关。**

找不到它们不是装漏了。服务发现靠 Docker / K8s 原生 DNS，健康检查靠 Probe /
healthcheck + 重启策略。别建议加上，那是被论证过后拒绝的。

**4. 某个组件「莫名其妙」跟着起来了。**

启停是「跟着上层走」：只要还有一个上层在跑，它就跑，强弱依赖一视同仁。
读 CLI 输出里每行后面的理由——`启动（顶层）` / `启动（enabled: true）` /
`启动（X 需要）`——它直接说明判定来源。

**5. `configSchema` 里的某个配置项静默失效了。**

撞上平台保留变量了。CLI 会警告并跳过该配置项，平台注入的值优先。检查配置项名转成
大写下划线后是否命中 `DATABASE_*` / `REDIS_*` / `MQ_*` / `STORAGE_*` / `SEARCH_*` /
`SMTP_*` / `*_ENDPOINT` / `COMPONENT_ID` / `COMPONENT_VERSION`。
比如 `databaseTimeout` → `DATABASE_TIMEOUT`，撞了。

## 错误码 → 该干什么

CLI 的报错带错误码。按码定位比按文案快。

| 错误码 | 含义与第一步 |
| --- | --- |
| `DEPENDENCY_MISSING` | 强依赖在所有安装源里都没找到。查安装源配置、组件是否存在、版本是否为 stable |
| `RESOURCE_UNBOUND` | 组件要的资源没在 `brickkit.yaml` 的 `resources` 里声明或绑定。`kind` + `engine` 必须与组件声明**完全一致**。暂时不想跑它就给 `enabled: false`——不启动的组件不参与这条检查 |
| `COMPONENT_DISABLED` | 钉住的组件（`enabled: true`）撞上被关掉的强依赖，两个意图冲突。要么放开那个 `enabled: false`，要么别钉住它 |
| `VERSION_AMBIGUOUS` | 多版本共存时没指定版本。补上精确版本 |
| `DEPENDENCY_CYCLE` | 强依赖成环。环只在弱依赖里是合法的 |
| `MANIFEST_INVALID` | `component.yaml` 有问题。注意它**没有扩展字段机制**，不认识的键会被当场拒绝 |
| `COMPONENT_NOT_FOUND` | 安装源里没有这个组件 |
| `CONFIG_CONFLICT` | 配置项撞上保留变量（见上一节第 5 条）。这是警告，不阻断 |
| `IMAGE_UNAUTHORIZED` | 镜像拉取未授权。先 `docker login <registry>`，再重跑 |
| `MIGRATION_FAILED` | 迁移脚本失败。看迁移日志，修完重跑 |
| `PORT_CONFLICT` | 端口撞了。多为 `localPort` 或 `exposePort` 重复 |
| `SIGNATURE_INVALID` | 验签失败。注意：一个 `publicKeys` 都没配时验签是**整体失效**的，那种情况下不会报这个码，而是警告一句什么都没验 |
| `AUTH_REQUIRED` / `TOKEN_EXPIRED` | 重新 `brickkit login` |
| `ENGINE_MISSING` | Docker / kubectl 不在 PATH 上，或没起来 |
| `PROJECT_MISSING` | 当前目录不是 BrickKit 项目，或 `--config` 指错了文件 |
| `PROJECT_EXISTS` | 已初始化，不必重复 `init` |
| `CLONE_FAILED` | 两种常见原因：组件是闭源的（没有 Git 仓库，但**照样能正常安装使用**），或目标目录已存在 |

## 排查顺序

**组件起不来**，按这个顺序看，越靠前的越常见：

1. `brickkit status` —— 它到底有没有被判定为「启动」？不启动的组件也会列出来
2. 看 CLI 输出里那一行的**理由**（顶层 / enabled / X 需要）
3. 组件日志 —— 进程本身有没有起来
4. 健康检查是不是超了 30 秒预算（见上面第 2 条）
5. 环境变量 —— 依赖真在跑吗？弱依赖没在跑时**不会注入**那个 `*_ENDPOINT`
6. 资源绑定 —— `kind` 和 `engine` 对得上吗

**连不上依赖**：地址格式在本地和 K8s 完全一样，都是
`http://<版本化服务名>:<端口>`，服务名是组件 ID 的 `/`、`.` 换成 `-` 再加精确版本
（`people/basic` + `1.0.0` → `people-basic-1-0-0`）。变量名基于组件 ID 不带版本
（`PEOPLE_BASIC_ENDPOINT`），值才带版本。对不上就往这两条规则上核。

**想在不启动任何东西的情况下看生成结果**：`brickkit up --dry-run`。

## 去哪查更细的

- 参数：`brickkit <命令> --help`
- 常见报错的完整处置（含真实输出样例）：`design/011-组件安装与拼装指南.md` §17
- 错误码与错误文案的完整表：`design/004-CLI 设计.md`
- 「为什么这样设计」的完整论证（用户问「为什么不……」时）：
  `design/012-架构设计原理与考量.md`
