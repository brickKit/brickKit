# 缓存设计

## 两处缓存

| 在哪 | 放什么 | 谁共用 |
| --- | --- | --- |
| 项目的 `.brickkit/` | 这个项目用到的组件版本的 `component.yaml`、文档、契约；生成的部署文件；本机状态 | 只有这个项目 |
| 用户级 `~/.cache/brickkit/repos/` | 组件 Git 仓库的 bare 仓库 | 这台机器上所有项目，以及组件自己的工作台 |

bare 仓库见 [Git 仓库缓存](06-bare-repo-mechanism.md)。这一篇讲项目里的 `.brickkit/`。

## `.brickkit/` 里有什么

```text
.brickkit/
├── manifests/<scope>/<name>/<版本>/
│   ├── component.yaml     这个版本的 Manifest（永久缓存）
│   ├── BRICKKIT.md        组件带着文档时才有
│   └── signature.json     来源与签名信息
├── artifacts/<版本化服务名>/<type>/…   组件声明的契约文件（add / fetch 下载）
├── generated/
│   ├── compose.yaml       Docker / Podman 的部署文件
│   ├── env/               放密钥的 env 文件（0600）
│   ├── k8s/               Kubernetes 清单
│   └── local-debug.<服务名>.env   mode: debug 组件的环境变量
├── last-run               上一次 up 运行的组件版本，版本变更提示以它为基线
├── local-mode             存在即本地模式开着
├── session.lock           mode: local 会话正在运行
├── skills.lock            装进项目的 AI 助手技能的版本与语言
└── credentials            组件市场的登录令牌（0600）
```

## 缓存规则

**Manifest 永久缓存，从不过期。** 精确版本是不可变的：`demo/hello@1.0.0` 的 `component.yaml` 今天是什么，永远就是什么——发布出去的版本不再改动
（`release` 拒绝移动一个已存在的 tag）。所以没有"缓存过期"这回事，取过一次就一直用。

**本地源从不缓存。** 来自本地安装源（`components/`、`shell/`）的组件，`component.yaml` 每次都从目录里重新读：那是正在开发的代码，改了就该立刻生效。
正好是这个版本的本地源目录存在时，文档路径也指向它，而不是缓存里的快照。

**生成物每次重写。** `generated/` 下的一切都是 `up` 的输出，每次重新生成；手改会被下一次 `up` 覆盖。

## `.brickkit/` 不进 Git

`init` 生成的 `.gitignore` 把它排除在外。它里面的东西要么能重新得到（Manifest、契约、生成物），要么是这台机器、这个人的状态（本地模式开关、会话锁、登录令牌）。

第一次 `git clone` 一个项目之后，`.brickkit/` 不存在，没关系：第一次运行命令时，CLI 按 `brickkit.yaml` 从安装源重新取回需要的 Manifest 与契约。
这与 `package.json` 进 Git、`node_modules` 不进 Git 是同一个道理：`brickkit.yaml` 是锁文件，`.brickkit/` 是按它装出来的东西。

## 什么可以删

| 删掉 | 后果 |
| --- | --- |
| `.brickkit/` 整个 | 安全。下次命令重新取回 Manifest 与契约、重新生成部署文件；本地模式会变成关闭，市场需要重新登录 |
| `manifests/`、`artifacts/` | 安全，下次按需重新取回（从用户级 bare 仓库取，通常不用联网） |
| `generated/` | 安全，下次 `up` 重新生成 |
| `~/.cache/brickkit/repos/` | 安全，下次用到时重新克隆 |

删缓存从来不是修问题的办法——缓存里的精确版本不会"变坏"。本地源的改动没生效，先确认组件确实来自本地源（`brickkit.yaml` 里的安装源顺序）。
