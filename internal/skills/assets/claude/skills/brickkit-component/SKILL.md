---
name: brickkit-component
description: 新写一个 BrickKit 组件、修改 component.yaml、加数据库迁移、或让组件对外提供 gRPC / HTTP 接口时使用。含任何组件都必须满足的硬性契约、平台保留变量的禁区、健康检查禁令与启动宽限期、以及依赖声明的规则。当用户说「写一个组件」「组件起不来该怎么声明」或在编辑 component.yaml 时，这个技能适用。
---

# 写一个 BrickKit 组件

## 什么时候用这个技能

- 从零写一个新组件
- 改 `component.yaml`
- 给组件加数据库迁移
- 让组件多开一个端口（比如 gRPC）
- 组件起不来，怀疑是声明写错了

## 你会猜错的地方

**1. `component.yaml` 没有扩展字段机制。**

`apiVersion` 是 `brickkit/v1`。不认识的键会被**当场拒绝**，不是静默忽略。
所以别往里加自定义字段——曾经有过 `observability` 和 `compatibility.minCliVersion`
两个「预留」字段，已经删掉了：它们从未被任何一处读取，而后者更糟，它长得像一道安全闸，
但写了 `minCliVersion: 2.0.0` 的组件在旧 CLI 上照装不误。

**2. 保留变量不许碰。** 这是最容易踩的一条。

`configSchema` 里的配置项名转成大写下划线后，不得与这些冲突：

| 模式 | 匹配方式 |
| --- | --- |
| `COMPONENT_ID`、`COMPONENT_VERSION` | 精确 |
| `*_ENDPOINT` | 后缀 |
| `DATABASE_*`、`REDIS_*`、`MQ_*`、`STORAGE_*`、`SEARCH_*`、`SMTP_*` | 前缀 |
| `{envPrefix}_*` | 前缀，envPrefix 由使用者在 `brickkit.yaml` 里定 |

撞了会怎样：市场在发布时**拒绝**；CLI 在注入时**警告并跳过该配置项**，平台注入的值优先。
也就是说，你的配置项会静默失效。命名规则是 `defaultPageSize` → `DEFAULT_PAGE_SIZE`，
所以叫 `databaseTimeout` 会撞 `DATABASE_*`——改成 `dbTimeout` 之类的。

**3. 健康检查禁令：`/healthz` 只检查本进程存活。**

**禁止**在里面查数据库、查依赖组件、查任何外部系统。原因是级联：一个下游抖动会让
所有上游同时被判不健康并一起重启，把一次局部故障放大成整片雪崩。

**4. 冷启动超过 30 秒的组件必须写 `startPeriodSeconds`。**

`interval` / `timeout` / `failureThreshold` 由平台固定（10s / 3s / 3），相乘就是默认启动
预算 = **30 秒**。超过它：Docker 下判 `unhealthy` 让 `up` 失败、依赖方卡在
`service_healthy`；K8s 下 Pod 被 kill 重启、再走一遍同样的 30 秒 → **永久
CrashLoopBackOff，而容器日志一路正常**。

Spring Boot、Django 预加载、.NET 首次 JIT 都在射程内。宽限期只推迟「判死」不推迟
「判活」（两秒就绪的组件照样两秒转 healthy），**所以写大一点没有任何代价**。
它是 `healthCheck` 下唯一可覆盖的时间参数，默认 60。

**5. `dependencies` 里一个组件 ID 只能出现一次。**

不能同时依赖 `people/basic@1.0.0` 和 `@2.0.0`。因为依赖地址的**变量名基于组件 ID、
不带版本号**，写两个版本会撞同一个 `PEOPLE_BASIC_ENDPOINT`，后者静默覆盖前者。
CLI 解析 Manifest 时就报错。

菱形依赖不受影响（A 依赖 X@1、B 依赖 X@2，各拿各的）。多版本共存是**项目级**能力。

**6. 弱依赖缺失时「完全不注入」，不是「注入空字符串」。**

这是 BrickKit 最容易被误解的设计。组件代码必须用安全读取：Python 的
`os.environ.get()`、Java 的 `System.getenv()`。用 `os.environ["X"]` 会抛 `KeyError`
让组件立刻崩溃——**这是刻意的**，让「依赖不在」在启动时就暴露，而不是变成一个
连向空地址的运行时谜题。

降级逻辑（Redis 挂了是查库、返空列表还是写本地文件）是你的业务代码，平台不管。

**7. `resources` 建议只写 `requests`，别写 `limits`。**

配额优先级是 `brickkit.yaml` > `component.yaml` > CLI 默认值，而且**逐字段合并**——
组件写了 `limits.cpu`，部署方就删不掉它。限额是业务判断，留给部署方。

## 机制是怎么运作的

**地址注入。** 平台给你注入依赖的地址，变量名基于组件 ID（`/` 和 `-` → `_`，全大写），
值指向带版本的服务名：

```
PEOPLE_BASIC_ENDPOINT=http://people-basic-1-0-0:8080
PEOPLE_BASIC_GRPC_ENDPOINT=http://people-basic-1-0-0:9090
```

额外端口的变量名是 `{组件ID前缀}_{NAME大写}_ENDPOINT`，`NAME` 来自
`deployment.extraPorts[].name`。

**地址格式在本地和生产完全一样**（都是 `http://<版本化服务名>:<端口>`），
所以组件代码在两个环境之间零修改。

**服务名 = 组件 ID 转换 + 精确版本**：`/` → `-`，`.` → `-`，全小写。
`people/basic` + `1.0.0` → `people-basic-1-0-0`。

**主端口的用途有两个**：健康检查和 `_ENDPOINT` 变量。`deployment.port` 必填。

**迁移**是 `migration.command`，数组格式。状态自己记（迁移状态表），平台只负责在
`up` 时把它作为一个前置步骤跑掉。

**`deployment.type` 固定是 `container`**，前端组件也一样——前端同样要监听端口、
提供健康检查、被 Ingress 暴露、通过环境变量拿后端地址。

## 去哪查更细的

- `component.yaml` 每个字段的规则：`design/002-组件规范.md`
- 完整字段参考：`design/附录合集.md` 附录 B
- 环境变量命名与保留变量：`design/附录合集.md` 附录 C
- 手把手教程（从 mkdir 到 publish，含 gRPC 双协议、前端 nginx 组件、迁移、断点调试）：
  `design/009-组件开发快速入门.md`
