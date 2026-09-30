# Git 仓库缓存

## 为什么不每次 `git clone`

Git 安装源上，一个组件一个仓库、一个版本一个 tag。CLI 要读某个版本的 `component.yaml`、契约文件，最直接的做法是每次克隆一遍仓库——
项目里二十个组件就是二十次完整克隆，每次 `up`、每个项目、每个组件的本地工作台都来一遍。

BrickKit 的做法是：每个组件仓库在本机只克隆**一次**，存成一个 **bare 仓库**（只有 Git 的数据，没有检出的工作区），之后只做增量的 `git fetch`；
要读某个版本的文件，直接从 bare 仓库里的那个 tag 取（`git show <tag>:component.yaml`），不需要检出。

## 放在哪

用户级的缓存目录，按完整的仓库地址分目录：

```text
~/.cache/brickkit/repos/
└── git.example.com/
    └── components/
        ├── demo-hello.git
        └── demo-quote.git
```

（macOS 上是 `~/Library/Caches/brickkit/repos/`。）不放在项目的 `.brickkit/` 里，是因为同一个组件仓库会被很多项目、以及组件自己的工作台共用——
放在每个项目里，同一个仓库就要克隆 N 次。按完整地址命名，不同组织下同名的仓库（`github.com/a/erp-api` 与 `github.com/b/erp-api`）不会撞在一起。

## 读取的四级优先级

要一个组件版本的 `component.yaml` 时，CLI 依次尝试，前一级有就不往下走：

| 级 | 从哪取 | 网络 |
| --- | --- | --- |
| 1 | 项目的 `.brickkit/manifests/<组件>/<版本>/` 永久缓存 | 不需要 |
| 2 | 缓存的 bare 仓库里的这个 tag（`git show`） | 不需要 |
| 3 | `git fetch --tags`：仓库在，但还没有这个 tag | 增量 |
| 4 | `git clone --bare`：这个仓库从没克隆过 | 完整克隆 |

来自本地安装源的组件不走这条路：本地源里的 `component.yaml` 每次都从目录里重新读，从不缓存——改了它，下一次命令就按新的来。

## 一次取文件的完整流程

1. 查项目的 Manifest 缓存。命中就结束。
2. 算出仓库地址：安装源的 `baseUrl` 加 `<scope>-<name>`；组件条目写了 `source.repo` 时用它。组件在仓库子目录里时，tag 带命名空间 `<scope>-<name>/<版本>`。
3. 确保 bare 仓库在缓存里：不在就克隆。克隆先落到缓存目录下的一个临时位置，完成后再改名——中途失败或被打断，不会留下半个仓库。
4. 仓库里有这个 tag，直接读 tag 里的文件。
5. 没有，就 `git fetch --tags` 取一次增量（一次运行里同一个仓库只取一次），再读。tag 被强制移动过时以远端为准；远端删掉的分支在缓存里也删掉。
6. 把读到的 `component.yaml`（以及组件带着的 `BRICKKIT.md`）写进项目的永久缓存。

"最新版本"（`add` 不写版本、`upgrade`）要列出 tag，这一步总是先 fetch 一次，保证看到的是远端现在的 tag。

## 离线

同一个版本只要取过一次，项目的永久缓存里就有它，之后不联网也能 `up`、`graph`、`deps`。换一个项目要用同一个版本时，bare 仓库已经在用户缓存里，
第 2 级就能取到，也不用联网。只有从没见过的版本、从没克隆过的仓库需要网络。

## 从不拉取 git submodule

组件仓库可能登记了 git submodule。BrickKit 在每一条路径上都一个也不拉：

| 路径 | 你得到的 |
| --- | --- |
| bare 仓库缓存 | tag 和它们的提交；submodule 在树里只是一个指针 |
| 读 `component.yaml`、`BRICKKIT.md`、产物 | 直接从 tag 读文件——从不读 submodule 里面的 |
| 从 tag `build` | tag 的导出：submodule 目录是空的，`build` 会为此警告 |
| `add --repo` | 不带 `--recurse-submodules` 的克隆；它会点名留空了哪些 submodule |

原因：submodule 指向另一个仓库，有它自己的地址、自己的凭据；拉它，就意味着装一个组件会悄悄伸手到谁都没声明过的仓库里。
组件的契约是它的 `component.yaml`、产物和镜像——这些都不该需要 submodule。构建确实需要时，组件应当发布镜像；在克隆下来的仓库里，
你随时可以自己 `git submodule update --init`。

## 鉴权

CLI 调用系统的 `git`，用的就是你已经配置好的凭据（SSH key、credential helper、CI 注入的 token）。它只设一个环境变量 `GIT_TERMINAL_PROMPT=0`：
没有凭据时让 `git` 直接失败，而不是在 CI 里停下来等一个永远不会来的密码输入。失败时 `git` 的原话会出现在报错里。
见 [Git 分发](../03-component-guide/10-git-distribution.md#私有仓库与鉴权)。

## 清理

`~/.cache/brickkit/repos/` 整个删掉是安全的：它只是缓存，下次用到时重新克隆。被中断的克隆留下的临时目录，放得够久的会在之后的运行里自动清掉。
