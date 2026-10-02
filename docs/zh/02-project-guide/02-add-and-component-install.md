# 添加组件

## 基本用法

```bash
brickkit add demo/caller
```

```text
🔎 demo/caller 没写版本，最新的是 1.0.0（安装源 company-git）
➕ 加入 demo/caller@1.0.0
   ✅ demo/hello@1.0.0
   ✅ demo/caller@1.0.0
📝 已写：brickkit.yaml, deploy.yaml
📝 配置骨架：config/demo-hello.yaml, config/demo-caller.yaml
📦 产物：2 个文件，在 .brickkit/artifacts/
⚠️ 警告：弱依赖缺失：demo/bus@1.0.0
   影响组件：demo/caller@1.0.0
   原因：拉取组件失败：demo/bus
   影响：该组件的环境变量 DEMO_BUS_ENDPOINT 不会被注入
   💡 弱依赖降级由组件自行处理；如需启用，请确认该组件已发布并可从安装源获取
```

**版本。** 不写版本时，`add` 取安装源里最新的版本，并把这个**精确版本**写进 `brickkit.yaml`——和 npm 写 lockfile 一样，
解析只发生这一次，以后谁 clone 项目都得到同一个版本。写了版本（`brickkit add demo/caller@1.0.0`）就用那个版本。
`^1.0.0`、`latest` 这类范围写法一律拒绝：版本范围正是"我这里好好的，线上就坏了"的经典来源。

**依赖是递归拉进来的。** `demo/caller` 强依赖 `demo/hello@1.0.0`，于是 `demo/hello` 跟着加进来了。它弱依赖的 `demo/bus`
在安装源里找不到：弱依赖缺失只警告、不阻塞，而且它的地址变量 `DEMO_BUS_ENDPOINT` **根本不会注入**——不是注入一个空字符串。
组件读到"没有这个变量"，走它自己的降级分支。

## 一次 `add` 改了什么

**`brickkit.yaml`**：声明与精确版本。

```yaml
components:
  - id: demo/hello
    version: 1.0.0
  - id: demo/caller
    version: 1.0.0
```

**部署文件**：每个组件版本一个条目。什么都没写，就是"按默认方式跑"。`deploy.local.yaml` 存在时，它也一起加上同样的条目。

```yaml
components:
  - id: demo/hello
  - id: demo/caller
```

**`config/`**：按每个组件的 `configSchema`（组件作者写的配置说明书）生成一份骨架。必填又没有默认值的项写成 `KEY: ""`，
等你填；其余的写成注释，注释着就用组件的默认值。

```yaml
# Component: demo/caller@1.0.0
# 这个组件的环境变量，每个键原样注入。
# 公共变量：$var:NAME（config/vars.yaml）· 环境变量：${NAME} · 本地文件：file://path
# 另一个组件的地址：$endpoint:<scope>/<name>

# === 可选：注释掉的键使用组件默认值，取消注释即可覆盖 ===
# DATABASE_HOST:  # string | PostgreSQL host name
# DATABASE_NAME:  # string | Database name
# DATABASE_PORT: 5432  # integer | PostgreSQL port (默认值)
# DATABASE_USER:  # string | User to connect as
# MIGRATION_SHOULD_FAIL:  # string | Test switch: set to "1" to make the migration exit non-zero, to check that a failed migration holds back the main service
```

有必填项要填时，`add` 会点名是哪个文件、哪几个键：

```text
📝 配置骨架：config/demo-widget.yaml
   ✏️ config/demo-widget.yaml 里的必填项要自己填上：API_URL
```

不填就 `up`，`up` 会拒绝启动并告诉你缺哪一项。

**`.brickkit/manifests/<id>/<version>/`**：组件的 `component.yaml` 和它的文档——`BRICKKIT.md` 连同它带的每一份译本
（`BRICKKIT.zh.md` 之类），永久缓存；本地源、Git 源、市场都一样。

**`.brickkit/artifacts/`**：组件声明的契约文件（OpenAPI、proto 之类），写调用方时用得上。

**`AGENTS.md`**：末尾的组件表（由 CLI 维护的那一段）给每个加进来的组件添一行：版本、干什么、带了哪些文档、主页
（见 [项目的 `AGENTS.md`](01-init-and-project-creation.md#项目的-agentsmd)）。那一段之外的内容不动；没有那一段的 `AGENTS.md`
原样不动。

改完之后项目装载不了（比如新组件与已有的冲突），`add` 把改过的文件全部还原——要么全做，要么一个都不改。

## 公共变量的提示

几个组件往往连同一个数据库。如果 `config/vars.yaml` 里已经有和配置项同名的公共变量，`add` 会问你要不要直接引用它：

```yaml
# config/vars.yaml
DATABASE_HOST: pg.internal
DATABASE_PORT: 5432
```

```text
config/vars.yaml 里已有 DATABASE_HOST，在 config/demo-caller.yaml 里引用它吗？[y/N] y
config/vars.yaml 里已有 DATABASE_PORT，在 config/demo-caller.yaml 里引用它吗？[y/N] 
```

答"是"的那一项写成 `$var:` 引用，没答的保持注释：

```yaml
DATABASE_HOST: $var:DATABASE_HOST  # string | PostgreSQL host name
# DATABASE_PORT: 5432  # integer | PostgreSQL port (默认值)
```

为什么要问、而不是自动引用：配置文件里每个值从哪来，应该一眼能看出来。`$var:` 的规则见
[公共变量与 $var:](../01-three-layers/06-vars-and-var-ref.md)。

## 外壳

`add` 一个外壳（把几个组件编进一个进程的组件）时，它编进的成员按外壳声明的版本一起加进来，而且在部署文件里**嵌在外壳条目的
`members` 下面**——成员归谁承载，只看这一处。反过来，`add` 一个普通组件时不会替你把它放进哪个外壳：那是部署方式的决定，
由你在部署文件里做。详见 [外壳机制](../04-shell/README.md)。

## 同一个组件的两个版本

`demo/caller@1.0.0` 要的是 `demo/hello@1.0.0`。如果项目里的 `demo/hello` 已经是 1.1.0，`add` 不会报错，而是把 1.0.0
也留下，标上是谁要它：

```yaml
components:
  - id: demo/hello
    version: 1.1.0
  - id: demo/hello
    version: 1.0.0
    requiredBy: [demo/caller]
```

两个版本同时运行，服务名分别是 `demo-hello-1-1-0` 和 `demo-hello-1-0-0`，互不冲突。没有 `requiredBy` 的那个是**默认版本**。
详见 [多版本共存](../03-component-guide/09-multi-version-coexistence.md)。

## `--local`：把本地源里的组件一次加进来

```bash
brickkit add --local
```

扫描所有本地安装源（默认是 `components/` 和 `shell/`），每个 `<scope>/<name>/component.yaml` 按它里面写的**真实版本**加进来。
刚用 `brickkit new` 写好的组件、或者手工放进 `components/` 的组件，这样一条命令就进了项目。

`--local --init` 还会先给每个还没有 `brickkit.yaml` 的本地组件在它自己的目录里建好本地联调工作台，
见 [分形的本地开发](../03-component-guide/05-local-dev-fractal.md)。

## 克隆源码：`--repo`、`--repo-all`

`add` 默认只取组件的 `component.yaml` 和契约文件，**不克隆源码**：大多数时候你只是用它，不改它。要改它的代码时：

```bash
brickkit add demo/hello@1.0.0 --repo
```

```text
✅ demo/hello@1.0.0 已经在项目里，三份文件都不用改
📥 demo/hello@1.0.0 的源码已克隆到 components/demo/hello/（检出 1.0.0）
```

组件已经在项目里时，`--repo` 只做克隆这一件事。克隆下来的目录检出的正是这个版本的 tag；它是一个完整的 Git 仓库，
分支、提交、推送都是你自己的事，CLI 不管 Git 权限。

注意：`components/` 是本地安装源，而且排在 Git 源前面。克隆之后，这个组件就从你的工作区里解析——你改的代码会被 `brickkit build`
打进镜像，`upgrade` 看到的"最新版本"也是工作区里的版本（见 [升级](07-upgrade-and-migration.md#组件来自本地源时)）。

`--repo-all` 把这次加进来的每个开源组件的源码都克隆下来（各自的默认版本）。

在项目里的任何位置运行——哪怕是在另一个组件的目录里——`add` 作用的都是项目，所以克隆总是落在项目自己的 `components/` 里：
组件源码只放一处（见[在项目里就地开发](04-focus-run.md#只有一个-components)）。例外是自带 `brickkit.yaml` 的组件目录
（[工作台](04-focus-run.md#焦点运行还是工作台)）：在那里离得最近的 `brickkit.yaml` 说了算，`add` 作用的是这个工作台，
而 `--repo` 会被拒绝——它会在项目里面再克隆出一份。

**从不拉取 git submodule。** 克隆下来它们是空目录，并且会点名：

```text
📥 demo/lib@1.0.0 的源码已克隆到 components/demo/lib/（检出 1.0.0）
   ℹ️  demo/lib@1.0.0 里有 git submodule，没有拉取：third_party/sdk（确实需要时 git submodule update --init）
```

代码确实需要它们的话，自己在克隆下来的仓库里拉。

## `--yes`：非交互

```bash
brickkit add demo/caller@1.0.0 --yes
```

每个问题都回答"是"，给 CI 用。没有 `--yes`、又没有终端输入时（比如在脚本里），问题一律按"否"处理。
