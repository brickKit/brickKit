---
name: brickkit-troubleshoot
description: brickkit 命令报错、组件起不来、地址注入不生效、依赖解析失败、或需要按错误码定位问题时使用。含部署文件与 brickkit.yaml 对不上（DEPLOY_INCONSISTENT）、deploy.local.yaml 过期要 local refresh、config/ 必填项为空、upgrade 留下的重复 key、$var 与 ${VAR} 引用未定义、提交被 pre-commit 检查拦下、IMAGE_MISSING / IMAGE_STALE、本地仓库版本不等于默认版本、外壳成员版本对不上、compose 启动环与 skipWaitFor、release 被拒，以及哪些「看起来像 bug」其实是刻意设计。当用户贴出 brickkit 的报错输出、或说「为什么起不来 / 连不上 / 找不到」时，这个技能适用。
---

# 排障

## 什么时候用这个技能

- 用户贴出一段 `brickkit` 的报错
- 组件起不来，或者起来了但连不上依赖
- 环境变量没注入、配置不生效
- `up` 之后跑起来的组件跟预期不一样
- Pod 一直 CrashLoopBackOff，但容器日志看着正常

## 先看报错里的 error_code

每个让命令失败的错误，在 `❌` 那一段之后的 stderr 上都有一行 JSON 日志，带稳定的 `error_code`。
按码定位比按文案快；写脚本判断成败也认它和退出码，别 grep 人读的文字。只有
`NETWORK_UNREACHABLE` 值得原样重试——那是根本没连上远端（断网、主机名解析不了）；Git 远端连上了却拒绝，
报的是 `AUTH_FAILED`，原样重试没用。报错下面的「提示」行通常已经给了具体命令，先照着做。
下面先详细讲最常见的几种，其余错误码在后面的速查表里。

## 症状 → 原因 → 处理

**1. `DEPLOY_INCONSISTENT`：部署文件与 `brickkit.yaml` 对不上。**

每个组件版本在部署文件里必须恰好一个条目（外壳下的成员也算），报错列出缺的和多的。通常是有人
手改了 `brickkit.yaml` 或部署文件。处理：按列出的补上或删掉条目；以后增删走 `add` / `remove` /
`upgrade`，它们会把三份文件一起改齐。

**2. 本地模式下报 `deploy.local.yaml` 已过期。**

还是 `DEPLOY_INCONSISTENT`，但原因是你开着本地模式，团队改了 `brickkit.yaml`（比如 `git pull`
进来一个新组件），而你的 `deploy.local.yaml` 没跟上。处理：`brickkit local refresh`——旧文件存成
`deploy.local.yaml.bak`，并列出你的每一处本地改动让你手工搬回去；或者手改文件；或者 `local off`。
`brickkit local status` 能先确认是不是这种情况。

**3. 改了 `deploy.yaml` 却没有任何效果。**

本地模式开着：跑部署、查部署的命令（`up`、`down`、`status`、`sync`、`lint`、`build`）读的是
`deploy.local.yaml`，完全不看 `deploy.yaml`；只有 `graph` 和 `deps` 永远读 `deploy.yaml`。
`brickkit local status` 确认；要么改本地文件，要么这次加 `--no-local`。

**4. 必填的组件配置没有值（`CONFIG_INVALID`）。**

`add` 在 `config/<scope>-<name>.yaml` 里给必填键写了 `KEY: ""`，那是要你填的空位。报错点名了组件和
键；去读 `.brickkit/manifests/<scope>/<name>/<版本>/BRICKKIT.md` 的配置指南看该填什么。

**5. 升级后 `up` 拒绝启动：检测到未解决的配置冲突（`CONFIG_CONFLICT`）。**

`upgrade --yes`（或没有终端时）把冲突写成了**两行重复键**加一段注释——故意的，让冲突没法被忽略。
用纯文本编辑器保留一行、删掉另一行和注释。**别用 yq 或编辑器的「格式化文档」**：它们会悄悄丢掉
一行，冲突就假装解决了。编辑器把文件标成非法 YAML 是正常的。

**6. `$var:` 引用了哪里都没有定义的公共变量（`CONFIG_INVALID`）。**

`$var:NAME` 只从部署文件的 `vars:` 和 `config/vars.yaml` 取，没有别的兜底。在其中一处定义它。注意
`-f deploy.prod.yaml` 时读的是那份文件的 `vars:`；写成 `$var: NAME`（冒号后带空格）是另一回事，YAML
会把它读成映射。

**7. `${VAR}` 没有定义，或者写错了格式（`CONFIG_INVALID`）。**

「错误：config/ 或部署文件里引用的环境变量没有定义」：`${VAR}` 在进程环境和 `.env` 里都找不到。生成部署文件
时就查，每种部署目标都查——否则 compose 会悄悄换成空字符串，组件拿着坏值跑起来却没有任何报错。处理：写进
`.env` 或 export；或者给默认值 `${VAR:-dev}`，可以为空的值写 `${VAR:-}`。「… 不是合法的 ${} 引用」：
`${DB_HOST`、`${DB-HOST}` 这种写法，加载文件时就报；写成 `${NAME}` 或 `${NAME:-默认值}`。

**8. 配置项「写了没生效」。**

- 键拼错或不在组件的 `configSchema` 里：CLI 警告「不会生效」并猜你想写哪个。键就是环境变量名，
  原样注入，没有大小写转换
- 撞上保留名（`COMPONENT_ID`、`COMPONENT_VERSION`、`BRICKKIT_SERVED_MEMBERS`、
  `BRICKKIT_SERVED_MEMBERS_CONFIG`、`PORT`、`*_ENDPOINT`）：警告并跳过，平台注入的值优先
- 改的是别的版本的文件：兼容版本读 `config/<scope>-<name>@<版本>.yaml`，默认版本读不带版本的那份

**9. `IMAGE_MISSING` / `IMAGE_STALE` / `IMAGE_UNVERIFIED`。**

`up` 从不构建。`IMAGE_MISSING`：本地源的组件（或只有 `deployment.build` 的）还没构建 →
`brickkit build <id>`。`IMAGE_STALE`：本机构建的外壳镜像里编进的成员版本与它的 `component.yaml`
不一致 → `brickkit build <外壳> --force`。`IMAGE_UNVERIFIED` 只是警告：外壳镜像没有 `brickkit build`
留下的标签，无法核对成员版本。改了代码没升版本号时，镜像 tag 没变，也要 `--force` 重建。

**10. 从本地仓库运行的组件：仓库里的版本 ≠ 项目的默认版本。**

`mode: local` / `mode: debug` 或裸进程外壳里的成员，是从本地仓库跑的；仓库里 `metadata.version`
必须等于 `brickkit.yaml` 的默认版本。处理：真要跑仓库里的版本就 `brickkit upgrade <id>@<仓库版本>`；
否则在仓库里检出对应的 tag。兼容版本（带 `requiredBy` 的）不能从本地仓库跑。

**11. 外壳承载的成员版本与外壳编进的版本对不上。**

三条出路：升级外壳到一个编进了这个版本的外壳版本；把那个成员条目挪出外壳，独立跑；两个版本都要——
给外壳编进的版本加一行 `requiredBy: [<外壳>]`，把 `id@那个版本` 嵌进外壳，另一个版本留在顶层。

**12. 把成员放进外壳后报启动环（`DEPENDENCY_CYCLE`）。**

组件之间没有互相依赖，但外壳容器作为整体启动，继承了每个成员的依赖，compose 下就成了 `depends_on`
环。报错列出了边。出路：把环上外面的那个组件也挪进外壳；把一个成员挪出来；或在条目上写
`skipWaitFor: [<ID>]`——只去掉启动等待，组件自己必须重试直到依赖起来。

**13. `DEPENDENCY_MISSING`：强依赖的版本没在 `brickkit.yaml` 里声明。**

`brickkit.yaml` 是锁文件，解析只用它声明过的版本。照提示 `brickkit add <id>@<版本>`（依赖需要另一个版本
时 add 会自动写成 `requiredBy` 兼容版本）。

**14. `RELEASE_BLOCKED` / `RELEASE_PUSH_FAILED`。**

被拒是发布前检查没过，什么都没写：组件目录有未提交的改动、当前分支没有上游或有未推送的提交、tag 已存在
（该升 `metadata.version` 了）。推送失败时本地 tag 已经删掉，解决远端原因后原样重试。

**15. 提交被拦下：「组件源码提交在归档目录里，但 … 说它该启动」（`CONFIG_CONFLICT`）。**

这是 `brickkit init --hooks` 装的 pre-commit 检查：部署文件说某个组件要启动，可这次提交里它的源码在
`components/.archived/` 下。处理：`brickkit restore` 把 `mode` 恢复到上次提交的样子；或者把目录的移动一起
提交。想不提交先查一遍，跑 `brickkit restore --check`。

## 其余错误码速查

| 错误码 | 情形 | 处理 |
| --- | --- | --- |
| `PROJECT_MISSING` | 不在项目里，或者缺 `deploy.yaml`；或者本地模式开着，`deploy.local.yaml` 却被删了 | `brickkit init`（不带名字）只补缺的文件，不动已有的；`brickkit local on` 重新生成本地文件 |
| `PROJECT_EXISTS` | `brickkit init <名字>` 的目录已存在且不空（或是个文件） | 换个名字；或者进去跑不带名字的 `brickkit init` |
| `INVALID_ARGUMENT` | 命令行写错（退出码 2）：未知命令或参数，组件 ID、版本格式不对 | `brickkit <命令> --help` |
| `COMPONENT_NOT_FOUND` | 安装源里没有这个组件或这个版本；Git 仓库没有这个版本的 tag | 核对 ID 和 `sources:`；请作者 `brickkit release` 这个版本 |
| `COMPONENT_BLOCKED` | 市场把这个组件版本下架了 | 换一个版本 |
| `COMPONENT_DISABLED` | 钉住要跑的组件（`enabled` / `local` / `debug`）有个强依赖写了 `disable` | 两个意图矛盾，去掉其中一个 |
| `MANIFEST_INVALID` | 某个 `component.yaml` 不合法，报错点名字段 | 由组件作者修（在组件仓库里 `brickkit lint`） |
| `PORT_CONFLICT` | `localPort` / `exposePort` 重复，或宿主机端口被占 | 换端口；只是你机器上的事就改在 `deploy.local.yaml` |
| `MIGRATION_FAILED` | 迁移命令失败（或入口没拒绝写错的参数） | 看迁移容器的日志，修好重跑 |
| `MIGRATION_SKIPPED`（警告） | `mode: local` / `debug` 的组件没有迁移容器 | 手动跑一次它的迁移 |
| `ENGINE_MISSING` | `docker` / `podman` / `kubectl` 不在 `PATH` 上或没在运行；或者只装了 Podman，部署文件却写着 `target: docker` | 装上、启动；或改成 `target: podman` |
| `ENGINE_FAILED` | `docker compose` / `podman compose` / `kubectl` 执行失败，组件起来了但不健康，或 `mode: local` 的进程崩了 | 报错里带着引擎自己的输出；再看组件日志 |
| `AUTH_REQUIRED` / `TOKEN_EXPIRED` | 市场要登录，或登录过期 | `brickkit login` |
| `AUTH_FAILED` | 市场拒绝了用户名或密码；或 Git 远端连上了但拒绝拉取——凭据被拒，或仓库根本不存在（托管平台对两者回答得一样） | 核对凭据；Git 的情形读报错里 git 自己的原话 |
| `IMAGE_UNAUTHORIZED` | 镜像仓库拒绝拉取，或者仓库里没有这个镜像 | `docker login <仓库>`，核对镜像地址；或者用 `brickkit build` 在本机构建 |
| `SIGNATURE_INVALID` | 开了 `requireSignature`，市场上的组件没签名 | 请发布者给一个签过名的版本 |
| `CLONE_FAILED` | `add --repo` 克隆失败：git 报错，`components/` 下已有同名目录，或源码在归档里 | 读 git 的原话；挪开那个目录；归档里的用 `brickkit sync` 拿回来 |
| `SUBMODULE_GUARD` | `sync` / `remove` 要移动或删除的目录是项目仓库登记过的 git submodule | 先在项目仓库里注销这个 submodule |
| `LINT_FAILED` | `brickkit lint` 查出问题，每条带文件和字段 | 修好重跑；警告只在 `--strict` 下算失败 |
| `INTERNAL` | 读写本地文件失败（磁盘满、没权限）；标题是「内部错误」的则是 CLI 的 bug | 查磁盘空间和权限；bug 请带完整输出报告 |

## 不变的老问题：它们是刻意设计

**弱依赖缺失时变量完全不存在，不是空字符串。** `os.environ["X"]` 抛 `KeyError` 崩掉是故意的；改组件代码用
`os.environ.get()` 并写降级逻辑，**不是**让平台注入空值。

**CrashLoopBackOff 而容器日志一路正常。** 几乎总是冷启动超过了默认 60 秒的宽限期；在 `component.yaml`
的 `healthCheck` 里调大 `startPeriodSeconds`，写大没有代价。

**健康检查查了数据库或依赖。** 一个下游抖动会让所有上游一起被判不健康并重启。健康检查只查本进程。

**组件自己说 ready，平台说 unhealthy。** Docker / Podman 上的健康检查在容器里跑 `wget` 或 `curl`，
镜像里两个都没有就永远过不了，往镜像里加一个。（Kubernetes 的探针是 `httpGet`，不需要它们。）

**某个组件「莫名其妙」跟着起来了。** 启停跟着上层走，强弱依赖一视同仁。读 CLI 每行后面的理由：
`启动（顶层）` / `启动（mode: enabled）` / `启动（X 需要）`。

**`mode: debug` 写进 `deploy.yaml` 被拒。** 它只能写在 `deploy.local.yaml`：`brickkit local on`，再到那里写。
`target: k8s` 下 `mode: debug` / `mode: local` 都被拒，集群到不了你的机器。

**`mode: debug` 的组件报 `relation does not exist`。** 它不生成迁移容器，手动跑一次迁移。

**没有注册中心、配置中心、网关。** 找不到不是装漏了，别建议加上。

## 排查顺序

**组件起不来**，越靠前越常见：

1. `brickkit status` —— 它有没有被判定为启动？不启动的也会列出来
2. CLI 输出里那一行的**理由**
3. 组件日志：`docker compose -p brickkit-<项目名> logs <版本化服务名>`——少了 `-p`，compose 看的是别的项目
4. 启动是不是超过了 60 秒宽限期
5. 环境变量：依赖真在跑吗？弱依赖没在跑时不注入 `*_ENDPOINT`；配置值来自哪份 `config/` 文件
6. `brickkit lint`：离线查三层文件的一致性、必填值、未知键、重复键；`--strict` 再查 `${VAR}` 与 `file://` 能否取到

**连不上依赖**：地址是 `http://<版本化服务名>:<端口>`，服务名是组件 ID 的 `/`、`.` 换成 `-` 再加精确版本
（`people/basic` + `1.0.0` → `people-basic-1-0-0`）；变量名不带版本（`PEOPLE_BASIC_ENDPOINT`）。依赖在外壳里
时地址指向外壳。

**只想看生成结果不启动**：`brickkit up --dry-run`。**看依赖怎么连的**：`brickkit deps` 或 `brickkit graph`。

## 去哪查更细的

- 参数：`brickkit <命令> --help`
- 组件自己的说明（配置怎么填、依赖为什么要）：`.brickkit/manifests/<scope>/<name>/<版本>/BRICKKIT.md`
- 「为什么这样设计」的完整论证与全部错误码：<https://github.com/brickKit/brickKit> 根目录 `AGENTS.zh.md`
