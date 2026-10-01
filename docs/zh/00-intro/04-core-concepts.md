# 核心概念

只想花几分钟弄懂 BrickKit 的骨架、读文档和报错时不被术语绊住，看这一页就够了。每个词后面都指向
讲透它的那一篇。

## 一页纸速查表

| 术语 | 是什么 |
| --- | --- |
| **组件**（Component） | 最基本的安装和运行单元：一个能单独跑起来的程序，**一律是容器**，前端（nginx 托管静态文件）也不例外 |
| **Manifest**（`component.yaml`） | 组件的自我介绍：它依赖谁、监听哪个端口、需要哪些配置、怎么判断它活着、镜像从哪来 |
| **项目**（Project） | 用三层文件描述的一组组件，就是你要装配出来的那个系统 |
| **三层文件** | `brickkit.yaml`（有什么）、`deploy.yaml` / `deploy.local.yaml`（怎么跑）、`config/`（拿到什么配置），见 [三层架构总览](../01-three-layers/01-overview.md) |
| **锁文件** | `brickkit.yaml` 的角色：用到的每个组件都锁在一个精确版本上，没写进去的组件就不存在 |
| **安装源**（Source） | 去哪里找组件：Git 仓库（默认，版本就是 Git tag）、本机目录（本地源）、组件市场（可选） |
| **本地源**（Local Source） | 本机上的一个目录，里面按 `<scope>/<name>/component.yaml` 放着组件源码；它们的镜像由 `brickkit build` 构建 |
| **强依赖 / 弱依赖** | 强依赖缺了就报错、不启动；弱依赖（`optional: true`）缺了只是**不注入**它的地址变量——不是注入空字符串 |
| **契约**（Contract / Artifacts） | 组件对外公开的 API 描述（OpenAPI、Protobuf 等），在 `component.yaml` 的 `artifacts` 里声明，`add` / `fetch` 时下载 |
| **外壳**（Shell） | 把多个组件编进**一个进程**里跑的组件，用来省内存和 CPU，见 [外壳机制](../04-shell/README.md) |
| **成员**（Member） | 被外壳承载的组件；它没有自己的容器，但仍然有自己的配置和迁移 |
| **分形架构** | 组件开发时本身是一个项目，被使用时是一个黑盒，见 [分形架构](06-fractal-architecture.md) |
| **`BRICKKIT.md`** | 组件写给使用者（人和 AI）的文档：它负责什么、部署前要准备什么、配置是什么意思、有哪些契约。它跟着每个版本走（译本叫 `BRICKKIT.<语言>.md`），会缓存进使用它的项目，见 [组件的文档](../03-component-guide/08-component-doc-spec.md) |
| **`AGENTS.md`** | AI 编程工具最先读的导读（`CLAUDE.md` 里写着 `@AGENTS.md`，Claude Code 也就读到它）。项目的 `AGENTS.md` 写团队约定，末尾是一张项目组件表，由 CLI 跟着更新；组件的 `AGENTS.md` 写给开发这个组件的人，见 [创建项目](../02-project-guide/01-init-and-project-creation.md#项目的-agentsmd) |
| **本地模式** | `brickkit local on` 之后，所有命令改读个人的 `deploy.local.yaml`，见 [本地调试工作流](../02-project-guide/03-local-debug-workflow.md) |

## 贯穿全局的命名规则

这几条规则一旦知道，文档里、报错里、生成的文件里看到的名字都能自己推出来。

| 名字 | 规则 | 例子 |
| --- | --- | --- |
| 组件 ID | `scope/name`，全小写 | `people/basic` |
| 版本 | 精确的 `主.次.补丁`，不接受 `^1.0.0` 这类范围 | `1.0.0` |
| 版本化服务名 | 组件 ID 与版本里的 `/`、`.` 换成 `-` | `people-basic-1-0-0` |
| 依赖地址变量 | 组件 ID 转成大写，`/`、`-` 换成 `_`，加 `_ENDPOINT` | `PEOPLE_BASIC_ENDPOINT=http://people-basic-1-0-0:8080` |
| 配置文件名 | 组件 ID 的 `/` 换成 `-`；只给某个版本用的，加 `@版本` | `config/people-basic.yaml`、`config/people-basic@2.0.0.yaml` |
| 配置项 | `configSchema` 里的键**就是**环境变量名，原样注入 | `DB_HOST` |
| 镜像 tag | 与组件的 `metadata.version` 严格一致 | `registry.example.com/people/basic:1.0.0` |
| 发布 tag | 组件在仓库根目录时是版本号；在子目录时带上组件名 | `1.0.0`、`people-basic/1.0.0` |

变量**名**只从组件 ID 推导、从不带版本；变量的**值**才指向具体版本。所以两个版本的同一个组件可以并排跑
（`people-basic-1-0-0` 与 `people-basic-2-0-0` 是两个不冲突的 DNS 名字），而调用方的代码永远只读
`PEOPLE_BASIC_ENDPOINT` 这一个名字。

## 启停规则：跟着上层走

部署文件里每个组件可以写一个 `mode`；不写的时候，它**跟着上层走**：没人依赖的顶层组件默认运行，被依赖的组件
只要还有一个上游在跑，就跟着跑。

| 写法 | 意思 |
| --- | --- |
| 不写 | 跟着上层走 |
| `mode: enabled` | 一定跑，不管上层；它的强依赖被关掉时报错（两个意图冲突） |
| `mode: disable` | 一定不跑；依赖它的组件跟着不跑 |
| `mode: local` | 一定跑，但不在容器里：BrickKit 自己探测启动命令、在你机器上拉起这个进程并盯着它 |
| `mode: debug` | 一定跑，进程由你自己在 IDE 里启动；**只能写在个人的 `deploy.local.yaml` 里** |

`local` 和 `debug` 只在 Docker / Podman 目标下有意义：集群里的 Pod 连不到你笔记本上的进程。

## 几个关键动词

| 命令 | 做什么 |
| --- | --- |
| `brickkit add` | 拉取组件和它的依赖，写进三层文件，生成配置骨架，下载契约 |
| `brickkit build` | 构建需要在本机构建的镜像 |
| `brickkit up` | 生成部署文件 → 跑数据库迁移 → 起容器（绝不自动构建） |
| `brickkit upgrade` | 换版本，并按新旧 `configSchema` 迁移你的配置 |
| `brickkit local` | 打开 / 关闭个人的本地模式 |
| `brickkit release` | 校验组件 → 打 Git tag → 推送，失败时不留 tag |

全部命令见 [CLI 命令参考](../07-cli-reference/README.md)。
