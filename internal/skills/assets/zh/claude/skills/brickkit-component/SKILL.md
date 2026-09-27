---
name: brickkit-component
description: 新写一个 BrickKit 组件或外壳、修改 component.yaml、写组件的 BRICKKIT.md、加数据库迁移、声明依赖与配置项（configSchema）、配置镜像或 build、发布新版本（brickkit release）时使用。含任何组件都必须满足的硬性契约、平台保留名、健康检查禁令与启动宽限期、外壳 shell.members 的规则。当用户说「写一个组件」「写一个外壳」「发个新版本」或在编辑 component.yaml 时，这个技能适用。
---

# 写一个 BrickKit 组件

## 什么时候用这个技能

- 从零写一个新组件，或一个把别的组件编进来的外壳
- 改 `component.yaml` 或组件的 `BRICKKIT.md`
- 给组件加配置项、依赖、数据库迁移、额外端口
- 要发布组件的新版本
- 组件起不来，怀疑是声明写错了

## 从零写：先跑 brickkit new

`brickkit new <scope>/<name>` 生成一份能通过校验的 `component.yaml` 和一份带标准章节
（组件定位、依赖说明、配置指南、契约索引、外壳声明）的 `BRICKKIT.md`，写到
`components/<scope>/<name>/`（本地安装源本来就扫描这个布局）。`--shell` 生成外壳骨架，写到
`shell/<scope>/<name>/`，里面有一个要换掉的占位成员。`--contract openapi|proto` 顺带生成契约
占位并登记进 `artifacts`。不生成 Dockerfile、不生成源码——平台不替你选语言；也不会自动
`add`，那是一次单独、可审阅的动作。

## 写完、改完：先跑 brickkit lint

只要动过 `component.yaml`，先跑 `brickkit lint`。离线、只读、不要 Docker，一次报出必填字段、
类型、不认识的键（拼写笔误）、版本格式、端口范围，外加两类警告：`configSchema` 属性里拼错的键
（比如 `defualt`，会静默不生效）、配置项撞上保留名。它**不查**依赖能不能解析、外壳成员对不对得上
——那要解析依赖图，留给项目里的 `brickkit up --dry-run`。

## 你会猜错的地方

**1. `component.yaml` 没有扩展字段机制。**

`apiVersion: brickkit/v1`，不认识的键**当场拒绝**，不是静默忽略。要给底层引擎带元数据
（监控抓取）用 `deployment.labels`；要随组件带文件（契约、SDK）用 `artifacts`——`add` / `fetch`
会下载到 `.brickkit/artifacts/<版本化服务名>/<type>/`，CLI 只下载不解析。

**2. `configSchema` 的键就是环境变量名。**

写 `DB_HOST`，组件拿到的就是 `DB_HOST`——没有驼峰转大写下划线那一步，使用者在
`config/<scope>-<name>.yaml` 里写的也是同一个键。所以键必须是合法的环境变量名（字母、数字、
下划线，不以数字开头），按你代码里真正读的名字写。

数据库、缓存、消息队列在平台眼里**没有特殊地位**：它们就是你声明的普通配置项
（`DB_HOST`、`DB_PASSWORD`……）。想让连接串之类的值被多个组件共用，是使用者的事
（`config/vars.yaml` 的公共变量），你只管声明自己要什么。

`required: [...]` 里没有 `default` 的键，使用者不填 `up` 就拒绝——只给平台真的猜不出来的值
（别的项目的地址、账号密码）用这个。密码、Token 写 `secret: true`，K8s 下它的值走生成的
Secret；平台从不按名字猜哪个是密钥。`enum` / `minimum` / `maximum` / `pattern` / `items`
只是说明书，**从不校验值**。

**3. 保留名不许碰。**

| 名字 | 匹配方式 |
| --- | --- |
| `COMPONENT_ID`、`COMPONENT_VERSION`、`BRICKKIT_SERVED_MEMBERS`、`BRICKKIT_SERVED_MEMBERS_CONFIG`、`PORT` | 精确 |
| `*_ENDPOINT` | 后缀 |

撞了：lint 与 up 警告，平台注入的值优先——你的配置项静默失效。`PORT` 是 `mode: local`
起裸进程时平台给的监听端口。

**4. 健康检查禁令：只检查本进程存活。**

**禁止**在里面查数据库、查依赖组件、查任何外部系统——一个下游抖动会让所有上游同时被判
不健康并一起重启，局部故障放大成雪崩。

**5. 冷启动超过 60 秒必须调大 `healthCheck.startPeriodSeconds`。**

`interval` / `timeout` / `failureThreshold` 平台固定（10s / 3s / 3），默认宽限期 60 秒。超了：
Docker 下 `unhealthy`、`up` 失败；K8s 下**永久 CrashLoopBackOff 而容器日志一路正常**。
宽限期只推迟「判死」不推迟「判活」，写大一点没有代价。

**6. `dependencies.components` 里一个组件 ID 只能出现一次。**

强依赖写 `- erp/api@1.0.0`，弱依赖写 `- id: infra/redis@1.0.0` 加 `optional: true`，都是精确版本。
不能同时依赖 X 的两个版本：变量名基于组件 ID 不带版本，两个版本会撞同一个 `X_ENDPOINT`。
菱形依赖（A 要 X@1、B 要 X@2）不受影响，那是项目级的多版本共存。

**7. 弱依赖缺失时变量「完全不存在」，不是空字符串。**

读它必须用 `os.environ.get()` / `System.getenv()`。`os.environ["X"]` 会 `KeyError` 让组件立刻
崩溃——这是刻意的。降级逻辑（Redis 不在时查库还是返空）是你的业务代码，平台不管。

**8. `deployment.image` 与 `deployment.build` 至少写一个，镜像 tag 就是 `metadata.version`。**

只有 `build: { context: ., dockerfile: Dockerfile }` 的组件，使用者要 `brickkit build`；`up` 从不
构建。所以**改了代码、没改版本**，旧镜像还在，`build` 会跳过——要么升 `metadata.version`，要么
让使用者 `brickkit build <id> --force`。

**9. 外壳：`shell.members` 写的是编进去的精确版本。**

```yaml
shell:
  members: [erp/api@1.2.0, erp/auth@1.0.0]
```

写了 `shell` 块，这个组件就是外壳：它的进程里编进了这些成员的代码。版本必须精确，一个成员只列
一个版本，而且要**和你真正编进去的代码一致**——项目里承载的成员版本跟这里不一样，`up` 会报错。
外壳启动时从 `BRICKKIT_SERVED_MEMBERS`（这次承载的成员服务名列表）决定初始化哪些模块，从
`BRICKKIT_SERVED_MEMBERS_CONFIG`（JSON：每个成员的 componentId、version、httpPort、extraPorts、
configEnvVars）找到每个成员的配置——成员的配置值在加了成员 ID 前缀的环境变量里，configEnvVars
告诉你原键对应哪个变量名。每个成员仍然要有自己的镜像：它的迁移用成员自己的镜像跑。

**10. `BRICKKIT.md` 是写给使用者和他们的 AI 的。**

它会随版本缓存进使用者项目的 `.brickkit/manifests/<scope>/<name>/<版本>/BRICKKIT.md`，是别人
不读你源码就能用好你的唯一途径。配置指南里讲清每个必填键填什么、依赖说明里讲清为什么要它。
外壳的 `BRICKKIT.md` 里的成员列表要与 `shell.members` 保持一致。

**11. 迁移容器和主容器是同一个镜像，入口必须对不认识的参数快速失败。**

`migration.command` 是数组，拿到的环境变量与主进程一样（包括全部配置值）。如果入口在不认识的
参数上落回「启动服务」，迁移容器就变成第二个服务容器、永不退出，整个部署卡在 `Created`，而日志
一片正常。先校验参数，再读环境变量、连数据库。迁移状态自己记；两个组件共用一个库时，状态表主键
要带组件标识。

**12. 发布新版本 = 改版本号、提交、推送、`brickkit release`。**

版本号只取 `metadata.version`。`release` 先检查：能通过校验、组件目录干净、有上游且没有未推送的
提交、tag 不存在；然后打 tag 并推送，推送失败就删掉本地 tag。tag 是 `1.2.0`（没有 `v`），
monorepo 子目录里的组件是 `<scope>-<name>/1.2.0`。组件仓库里自己的 `brickkit.yaml`（本地联调
工作台）不影响发布什么——`release` 只读 `component.yaml`——但它的文件照样算进"组件目录干净"：
把工作台（`brickkit.yaml`、`deploy.yaml`、`config/`、`.gitignore`）提交进去，否则 `release` 会拒绝。
只在本地、从没推送过的 tag 不算发布：推上去或者删掉。发到市场是另一条命令 `brickkit publish`。

## 机制是怎么运作的

**地址注入。** 每个依赖的主端口注入成 `{组件ID}_ENDPOINT`（`/` 和 `-` → `_`，全大写），额外端口
是 `{组件ID}_{NAME}_ENDPOINT`，值指向版本化服务名：

```
PEOPLE_BASIC_ENDPOINT=http://people-basic-1-0-0:8080
PEOPLE_BASIC_GRPC_ENDPOINT=http://people-basic-1-0-0:9090
```

地址格式在 Docker 与 K8s 下完全一样，组件代码零修改。依赖在外壳里跑时，地址指向外壳，端口仍是
依赖自己声明的端口——调用方不知道也不需要知道。另外固定注入 `COMPONENT_ID`、`COMPONENT_VERSION`。

**`deployment.type` 固定是 `container`**，前端组件也一样（nginx 容器，`port: 80`）。
`deployment.port` 必填，健康检查和 `_ENDPOINT` 都用它。

**`local: { language, runCommand }`** 可选：`mode: local` 自动探测启动命令失败时手动指定。

**分形。** 组件仓库里也可以有自己的 `brickkit.yaml`，当作本地联调工作台（仓库里 `brickkit init`，
或在上层项目 `brickkit add --local --init`）；`up` / `add` 把它当项目，`release` 只读 `component.yaml`。

## 去哪查更细的

- 参数：`brickkit new --help`、`brickkit lint --help`、`brickkit release --help`
- 完整规范：<https://github.com/brickKit/brickKit> 根目录 `AGENTS.zh.md`
- 别的组件的写法样例：你项目里 `.brickkit/manifests/` 下缓存的 `component.yaml` 与 `BRICKKIT.md`
