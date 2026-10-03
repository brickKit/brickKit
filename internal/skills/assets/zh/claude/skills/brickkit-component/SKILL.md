---
name: brickkit-component
description: 新写一个 BrickKit 组件或外壳、修改 component.yaml、写组件的文档（BRICKKIT.md、AGENTS.md、README.md）、加数据库迁移、声明依赖与配置项（configSchema）、配置镜像或 build、发布新版本（brickkit release）时使用。含任何组件都必须满足的硬性契约、平台保留名、健康检查禁令与启动宽限期、外壳 shell.members 的规则。当用户说「写一个组件」「写一个外壳」「发个新版本」或在编辑 component.yaml 时，这个技能适用。
---

# 写一个 BrickKit 组件

## 什么时候用这个技能

- 从零写一个新组件，或一个把别的组件编进来的外壳
- 改 `component.yaml` 或组件的 `BRICKKIT.md`
- 给组件加配置项、依赖、数据库迁移、额外端口
- 要发布组件的新版本
- 组件起不来，怀疑是声明写错了

## 从零写：先跑 brickkit new

`brickkit new <scope>/<name>` 生成一份能通过校验的 `component.yaml` 和四份带 `<!-- TODO: … -->` 提示的文档
（`BRICKKIT.md`、`AGENTS.md`、`CLAUDE.md`、`README.md`，见第 10 条），写到
`components/<scope>/<name>/`（本地安装源本来就扫描这个布局）。`--shell` 生成外壳骨架，写到
`shell/<scope>/<name>/`，里面有一个要换掉的占位成员。`--contract openapi|proto` 顺带生成契约
占位并登记进 `artifacts`；`--path` 写成一个独立的组件仓库。不生成 Dockerfile、不生成源码——平台不替你选语言；也不会自动
`add`，那是一次单独、可审阅的动作。

## 写完、改完：先跑 brickkit lint

只要动过 `component.yaml`，先跑 `brickkit lint`。离线、只读、不要 Docker，一次报出必填字段、
类型、不认识的键（拼写笔误）、版本格式、端口范围，外加两类警告：`configSchema` 属性里拼错的键
（比如 `defualt`，会静默不生效）、配置项撞上保留名。它**不查**依赖能不能解析、外壳成员对不对得上
——那要解析依赖图，留给项目里的 `brickkit up --dry-run`。在项目里，在组件目录下跑 `brickkit lint` 只查这个组件
（它的清单、文档、配置）；`brickkit lint --all` 查整个项目。

## 你会猜错的地方

**1. `component.yaml` 没有扩展字段机制。**

`apiVersion: brickkit/v1`，不认识的键**当场拒绝**，不是静默忽略。要给底层引擎带元数据
（监控抓取）用 `deployment.labels`；要随组件带文件（契约、SDK）用 `artifacts`——`add` / `fetch`
会下载到 `.brickkit/artifacts/<版本化服务名>/<type>/`，CLI 只下载不解析。

组件经消息系统发布或订阅事件？把事件名写进 `events: {publishes: [...], subscribes: [...]}`：`graph` / `deps` / `lint`
靠它看见谁发布、谁订阅，不影响部署。事件名照原样写；订阅一整类写成以 `*` 结尾的前缀（`orders.*`），别抄消息系统自己的通配符（`>` `#` `+`）。

**2. `configSchema` 的键就是环境变量名。**

写 `DB_HOST`，组件拿到的就是 `DB_HOST`——没有驼峰转大写下划线那一步，使用者在
`config/<scope>-<name>.yaml` 里写的也是同一个键。所以键必须是合法的环境变量名（字母、数字、
下划线，不以数字开头），按你代码里真正读的名字写。

数据库、缓存、消息队列在平台眼里**没有特殊地位**：它们就是你声明的普通配置项
（`DB_HOST`、`DB_PASSWORD`……）。想让连接串之类的值被多个组件共用，是使用者的事
（`config/vars.yaml` 的公共变量），你只管声明自己要什么。

`required: [...]` 里没有 `default` 的键，使用者不填 `up` 就拒绝——只给平台真的猜不出来的值
（别的项目的地址、账号密码）用这个。密码、Token 写 `secret: true`，K8s 下它的值走生成的
Secret；平台从不按名字猜哪个是密钥。私钥、证书这类要能不重启就更换、或不想出现在环境变量里的密钥，
再加 `mount: file`：这个键的环境变量里是**文件路径**（`/run/brickkit/secrets/<服务名>/<键>`），值在文件里，
所以把键起名成 `…_FILE`，代码读文件并在需要时重新读；值变了平台不会重启组件。`type` / `enum` / `minimum` / `maximum` / `pattern` / `items`
只是说明书，**从不校验值**。`default` 按你写下的原文注入：`default: 1.10` 拿到的是 `1.10`，不是 `1.1`。

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
活着但还不能接流量（缓存预热、第一次同步、权限数据还没拉到）？再声明 `readinessCheck: {type: http, path: /readyz}`：
K8s 的 readinessProbe 和 Docker / Podman 的 healthcheck（依赖方与 `up` 等的就是它）用它，存活检查仍用 `healthCheck`。
它同样不因下游挂了而失败。

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
外壳启动时读两个变量：

- `BRICKKIT_SERVED_MEMBERS`：这次承载的成员的版本化服务名，逗号分隔。只初始化这些模块；空字符串表示
  一个都不承载，不能当成「变量不存在」去全部启动。
- `BRICKKIT_SERVED_MEMBERS_CONFIG`：一个 JSON 数组，每个承载的成员一项——
  `{componentId, version, httpPort, extraPorts: [{name, port}], config}`。`config` 是这个成员的全部环境
  （它的配置项和它的依赖的 `*_ENDPOINT`），值都已经由 CLI 求好（`$var:`、`${VAR}`、`file://`）。成员的
  配置**不会**摊进外壳自己的环境变量里：从这里按成员自己的键名取。成员的某个键写成 `existingSecret`
  会被拒绝——值只在集群里，CLI 读不到，JSON 却要值。一个都不承载时它是 `[]`，不是不存在。一项长这样：

  ```json
  [{"componentId": "erp/api", "version": "1.2.0", "httpPort": 8080,
    "extraPorts": [{"name": "grpc", "port": 9090}],
    "config": {"DB_HOST": "db.internal", "ERP_AUTH_ENDPOINT": "http://erp-shell-1-0-0:8081"}}]
  ```

**编进去 ≠ 这次承载。** `shell.members` 说的是镜像里有什么，至少列一个成员——`members: []` 在 `lint` 和 `add` 时报
`MANIFEST_INVALID`。这次哪些在它里面跑由部署文件决定（嵌在外壳条目下的成员），可以一个都没有。所以新外壳要和它的
第一个成员一起进项目：先把那个成员做好、构建好，写进 `shell.members`，再 `brickkit add` 外壳。调用方照旧用被承载成员
自己的服务名——它解析到外壳（docker / podman 上是网络别名，k8s 上是选中外壳 Pod 的 Service）。
成员共用一个进程，进程级的东西不能在成员之间串：每个成员各一份——自己那一项 `config`（不读 `os.Getenv`）、
`service.name` 等于组件 ID 的遥测 provider、指标注册表、数据库连接池、资源上限；会话级设置（`SET ROLE`、`search_path`）
只能用 `SET LOCAL`；由外壳只做一次——信号处理、日志输出端、框架的全局开关
（`brickkit docs 04-shell/05-shell-development`，"一个进程、几个成员"一节）。

每个成员仍然要有自己的镜像：它的迁移用成员自己的镜像跑。

**10. 组件带五份文档，各写给一类读者——跟代码一起改。**

| 文件 | 读者 | 写什么 |
| --- | --- | --- |
| `BRICKKIT.md` | 使用它的项目（它们的 AI 读缓存里的 `.brickkit/manifests/<scope>/<name>/<版本>/BRICKKIT.md`） | 六节：`组件定位`（负责什么、不负责什么、归谁）、`部署前准备`、`依赖说明`、`配置指南`、`契约索引`、`外壳声明` |
| `AGENTS.md` | 开发它的 AI | 五节——`代码地图`（表格；路径用反引号，目录以 `/` 结尾）、`构建与测试`、`设计取舍`、`易错点`（不许 / 症状 / 原因）、`改代码前自查`——末尾是 brickkit 维护的一段 |
| `CLAUDE.md` | Claude Code | 只有 `@AGENTS.md` 一行 |
| `README.md` | GitHub 上的人 | `在项目里使用`、`文档`（一张表，每个问题指向能回答它的文件）、`开发` |
| `component.yaml` | CLI | 依赖、配置项、端口、镜像；可选的 `metadata.repository` 是项目组件表里显示的链接 |

- 一个事实只写一处：依赖和配置项在 `component.yaml`，接口在契约文件，历史在 Git。文档讲它们说不清的部分，不再抄一遍。
- `BRICKKIT.md` **不放相对链接**：它在别的项目缓存里是单独读的。文件名用行内代码写。
- 组件的任何文档都不链出组件目录（`../…`）——`AGENTS.md`、`README.md`、`docs/` 也一样：仓库是被单独克隆、单独读的（`DOC_LINK_NOT_PORTABLE`）。
- 译本放在旁边——`BRICKKIT.zh.md`、`README.zh.md`、`docs/design.zh.md`——整个 `docs/` 双语时也可以每种语言一棵树：`docs/<主语言>/` 和 `docs/<语言>/` 相对路径一一对应，每一页在每棵树里都有（主语言是 `AGENTS.md` 维护段的 `lang=`）。原文为准；每个语言版本在开头链接其余每一份（`BRICKKIT*.md` 除外）；译本的 `##` 小节与原文一样多（维护段不算）。`AGENTS.md` 一般不翻译；给人工审查者的 `AGENTS.<语言>.md` 按普通译本查。读和写都只对着原文。
- 固定小节按标题一字不差地认，中英文都行——英文原文的译本要用这些英文名：`BRICKKIT.md` Purpose / Before you deploy / Dependencies / Configuration / Contracts / Shell declaration；`AGENTS.md` Code map / Build and test / Design decisions / Pitfalls / Before changing code；`README.md` Use it in a project / Documentation / Development。
- 外壳的 `外壳声明` 里的成员要与 `shell.members` 一致。
- `brickkit lint` 都会查（警告；`--strict` 下算失败）：`DOC_FILE_MISSING`、`DOC_SECTION_MISSING`、`DOC_PATH_MISSING`（代码地图里的路径没了——第一张表第一列的每个行内代码都当路径查，`main.go`、`Dockerfile` 也算；别的格子里只有含 `/` 的才算；以 `/` 开头的是路由，比如 `/healthz`，从不当路径）、`DOC_LINK_BROKEN`、`DOC_LINK_NOT_PORTABLE`、`DOC_OUT_OF_STEP`（`component.yaml` 里有、文档没提的依赖、必填键、契约文件或外壳成员）、`DOC_PLACEHOLDER`、`DOC_TRANSLATION_DRIFT`、`AGENTS_BLOCK_MISSING`、`CLAUDE_IMPORT_MISSING`。文档和代码在同一个提交里改：下一个 AI 读的就是你留下的。
- 文档和代码一样是版本的一部分。朝新版本改——先升 `metadata.version`、测、再发布——发布前随便改。已经发布的版本不许原地改：把它当本地源的机器和从 tag 取它的机器会往项目 `AGENTS.md` 的组件表写不同的行，来回改。

**11. `deployment.resources` 只写 `requests`，`limits` 留给部署方。**

配额逐字段合并（部署条目 > `component.yaml` > 平台默认），你在这里写了 `limits.cpu`，用它的项目
就再也去不掉它，只能改成别的值。组件知道自己稳态占多少；允许它涨到多少是部署方的判断。

**12. `deployment.labels` 的值必须加引号写成字符串，平台自己的键不许写。**

`app`、`brickkit.io/*`、`com.docker.compose.*` 当场被拒。只写你自己拥有的事实
（`prometheus.io/port: "9090"`）；网关路由这类部署决策由使用方写在部署条目的 `labels` 上。

**13. 迁移容器和主容器是同一个镜像，入口必须对不认识的参数快速失败。**

`migration.command` 是数组，拿到的环境变量与主进程一样（包括全部配置值）。如果入口在不认识的
参数上落回「启动服务」，迁移容器就变成第二个服务容器、永不退出，整个部署卡在 `Created`，而日志
一片正常。先校验参数，再读环境变量、连数据库。迁移状态自己记；两个组件共用一个库时，状态表主键
要带组件标识。

**14. 发布新版本 = 改版本号、提交、推送、`brickkit release`。**

版本号只取 `metadata.version`。`release` 先检查：能通过校验、组件目录干净、有上游且没有未推送的
提交、tag 不存在；然后打 tag 并推送，推送失败就删掉本地 tag。tag 是 `1.2.0`（没有 `v`），
monorepo 子目录里的组件是 `<scope>-<name>/1.2.0`。组件仓库里自己的 `brickkit.yaml`（本地联调
工作台）不影响发布什么——`release` 只读 `component.yaml`——但它的文件照样算进"组件目录干净"：
把工作台（`brickkit.yaml`、`deploy.yaml`、`config/`、`.gitignore`）提交进去，否则 `release` 会拒绝。
只在本地、从没推送过的 tag 不算发布：推上去或者删掉。发到市场是另一条命令 `brickkit publish`。

发版说明可写可不写，但使用方升级前读的就是它：`brickkit release --notes-file <文件>`（或
`--notes "<文字>"`）把 Markdown 原样写进带注释的 tag，`brickkit upgrade` 动手之前列出跨过的每个
版本的说明。先写使用方必须做的——哪个键的含义或单位变了、哪个接口删了——再写新增了什么。
文件放在组件目录外面（目录里一个没提交的文件就过不了"目录干净"的检查）。不能和 `--local`
一起用（一份说明对应一个组件的一个版本）；`publish` 接同样两个参数。

## 机制是怎么运作的

**地址注入。** 每个依赖的主端口注入成 `{组件ID}_ENDPOINT`（`/` 和 `-` → `_`，全大写），额外端口
是 `{组件ID}_{NAME}_ENDPOINT`（端口名同样处理：`admin-api` → `ERP_API_ADMIN_API_ENDPOINT`），值指向版本化服务名：

```
PEOPLE_BASIC_ENDPOINT=http://people-basic-1-0-0:8080
PEOPLE_BASIC_GRPC_ENDPOINT=http://people-basic-1-0-0:9090
```

版本化服务名是组件 ID 和精确版本用 `-` 连起来，其中的 `/`、`.` 也换成 `-`。地址格式在 Docker 与 K8s 下完全一样，组件代码零修改。依赖在外壳里跑时，地址指向外壳，端口仍是
依赖自己声明的端口——调用方不知道也不需要知道。另外固定注入 `COMPONENT_ID`、`COMPONENT_VERSION`。

**`deployment.type` 固定是 `container`**，前端组件也一样（nginx 容器，`port: 80`）。
`deployment.port` 必填，健康检查和 `_ENDPOINT` 都用它。

**本地运行。** `mode: local` 时 BrickKit 从源码探测怎么启动；可选的 `local: { language, runCommand }`
在探测不对时手动指定。仓库里的 `metadata.version` 必须等于项目里这个组件的默认版本。进程继承终端的
环境变量，平台自己的名字除外——`COMPONENT_ID`、`COMPONENT_VERSION`、`PORT`、`BRICKKIT_SERVED_MEMBERS`、
`BRICKKIT_SERVED_MEMBERS_CONFIG`、所有 `*_ENDPOINT`，以及你自己 `configSchema` 里的键：这些只取 BrickKit
给的值，终端里残留的 `export` 冒充不了项目没给的值。

**分形。** 组件仓库里也可以有自己的 `brickkit.yaml`，当作本地联调工作台（仓库里 `brickkit init`，
或在上层项目 `brickkit add --local --init`）；`up` / `add` 把它当项目，`release` 只读 `component.yaml`。

## 去哪查更细的

- 这个版本的 BrickKit 文档，离线可读：`brickkit docs` 列出全部页——写组件 `brickkit docs 03-component-guide`、组件文档 `brickkit docs 03-component-guide/08-component-doc-spec`、外壳 `brickkit docs 04-shell`、`component.yaml` 全部字段 `brickkit docs 11-reference/01-component-yaml-schema`
- 参数：`brickkit new --help`、`brickkit lint --help`、`brickkit build --help`、`brickkit release --help`
- 完整规范：<https://github.com/brickKit/brickKit> 根目录 `AGENTS.zh.md`
- 别的组件的写法样例：你项目里 `.brickkit/manifests/` 下缓存的 `component.yaml` 与 `BRICKKIT.md`
