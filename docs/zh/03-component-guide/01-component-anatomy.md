# 组件仓库结构

## 一个组件仓库里有什么

`demo/quote` 的仓库长这样：

```text
demo-quote/
├── component.yaml      组件的自我描述：我是谁、依赖谁、要什么配置、怎么部署、怎么检查健康
├── BRICKKIT.md         写给使用者（人和 AI）：跟着组件进每一个 add 它的项目
├── BRICKKIT.zh.md      它的译本（可选；任意 BRICKKIT.<语言>.md）
├── AGENTS.md           写给开发它的 AI（和人）：代码地图、构建与测试、设计取舍
├── CLAUDE.md           只有一行 @AGENTS.md，让 Claude Code 读 AGENTS.md
├── README.md           写给 GitHub 上的人：主要是指向其它文档的路标
├── docs/               设计说明与编号的决策记录（可选）
├── api/
│   └── openapi.yaml    契约：别人怎么调用我
├── main.go             源码
├── go.mod
└── Dockerfile          怎么把源码构建成镜像
```

| 文件 | 必须有吗 | 谁读它 |
| --- | --- | --- |
| `component.yaml` | 必须 | CLI：安装、生成部署文件、注入环境变量全靠它 |
| `Dockerfile` | 组件没有预构建镜像时必须 | `brickkit build` |
| `BRICKKIT.md` | 应该有 | 使用这个组件的人和 AI 助手；它会缓存进使用方的项目，在那里脱离仓库单独被读 |
| `AGENTS.md`、`CLAUDE.md` | 应该有 | 开发这个组件的 AI 助手（和人）；从不离开这个仓库 |
| `README.md` | 应该有 | 在 GitHub 上浏览仓库的人 |
| `docs/`、`CHANGELOG.md` | 可选 | 开发这个组件的人；历史本身在 Git 里 |
| 契约文件 | 有对外接口就应该有 | 调用方，见 [契约与产物](06-artifacts-and-contracts.md) |
| 数据库迁移脚本 | 组件有数据库时 | 组件自己的迁移命令（`migration.command`） |
| 源码 | 视情况 | 平台从不读源码 |

每份文档怎么写、`brickkit lint` 怎么检查它们，见 [组件的文档](08-component-doc-spec.md)。译本放在原文旁边、带语言后缀
（`BRICKKIT.zh.md`、`README.zh.md`），不带后缀的那份是原文。`AGENTS.md` 通常不翻译（见
[写多种语言](08-component-doc-spec.md#写多种语言)）。

平台只要求组件满足三件事：有一份 `component.yaml`、是一个容器（能构建或拉取到镜像）、有一个健康检查。语言、框架、目录结构、日志格式都是组件自己的事。

组件仓库里还可能有 `brickkit.yaml`、`deploy.yaml`、`config/`——那是作者自己的本地联调工作台（见 [分形的本地开发](05-local-dev-fractal.md)），
与发布无关：使用方只读 `component.yaml` 和 `BRICKKIT.md`。

## 一个组件 = 一个 Git 仓库

组件是一个独立的**发布单位**（有自己的版本）、**移动单位**（`sync` 归档时整个目录连 `.git` 一起搬）和**权限单位**（谁能看、谁能改）。
所以一个组件放在一个自己的仓库里。同一块业务的几部分（契约、后端代码、迁移脚本）属于**同一个组件**，不用拆开。

| | 规则 | 例子 |
| --- | --- | --- |
| 组件 ID | `<scope>/<name>`，小写 | `demo/quote` |
| 仓库名 | `<scope>-<name>` | `demo-quote` |
| 仓库地址 | 安装源的 `baseUrl` + 仓库名 | `https://git.example.com/components/demo-quote` |
| 版本 | 精确版本 `主.次.修订` | `0.1.0` |
| 版本 tag | 就是版本号，不带 `v` | `0.1.0` |

地址推导不出来时（仓库在别的组织、名字对不上），使用方可以给这一个组件单独指定仓库，见 [Git 分发](10-git-distribution.md)。
几个组件放在同一个仓库的子目录里（monorepo）也能工作：tag 带上命名空间 `<scope>-<name>/<版本>`，各打各的。

## 最小可用组件

最少只要两个文件：`component.yaml` 和让镜像存在的东西。

- 组件写自己的代码：`component.yaml` + `Dockerfile` + 源码。
- 组件包装一个现成的镜像（比如一个开源服务）：`component.yaml` 里写 `deployment.image` 就够了，一行代码、一个 Dockerfile 都不需要。

前端组件也一样是容器：用 nginx 之类的 Web 服务器容器提供静态文件，`port: 80`。平台里没有"不是容器的组件"。
