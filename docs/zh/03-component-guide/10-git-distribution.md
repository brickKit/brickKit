# Git 分发

## 思路

不需要任何专门的服务器：**组件放在 Git 仓库里，版本是 Git tag。** 公司里已经有的 GitLab、Gitea、GitHub 组织就是一个组件源。
发布就是推一个 tag（`brickkit release`），安装就是按 tag 取文件。

## 组件地址怎么推导

使用方在 `brickkit.yaml` 里声明一个 Git 安装源：

```yaml
sources:
  - name: company-git
    type: git
    baseUrl: https://git.example.com/components/
```

之后每个组件的仓库地址按规则推出来，不用逐个配置：

| 组件 | 仓库 |
| --- | --- |
| `demo/quote` | `https://git.example.com/components/demo-quote` |
| `erp/backend` | `https://git.example.com/components/erp-backend` |

版本对应 tag：版本 `0.1.0` 就是 tag `0.1.0`（不带 `v`）。"最新版本"是精确版本形式的 tag 里最高的那个；`v1.0.0`、`latest` 这类 tag 不算版本。

**推导不出来时，单独指定。** 仓库在别的组织、名字对不上规则、或者组件在一个 monorepo 的子目录里，就在那个组件的条目上写 `source`：

```yaml
components:
  - id: erp/backend
    version: 2.1.0
    source:
      type: git
      repo: https://git.example.com/platform/erp.git
      path: services/backend      # 组件在仓库里的子目录；这时 tag 是 erp-backend/2.1.0
```

`source.type: local` 则指向本机的一个组件目录（`path` 相对项目根），见 [分形的本地开发](05-local-dev-fractal.md)。

## 缓存：每个仓库只克隆一次

CLI 在**用户级**缓存目录里为每个组件仓库保存一份 bare 仓库（只有 Git 数据、没有工作区），按完整的仓库地址分目录：

```text
~/.cache/brickkit/repos/
└── git.example.com/
    └── components/
        ├── demo-bus.git
        ├── demo-caller.git
        ├── demo-hello.git
        └── demo-quote.git
```

（macOS 上在 `~/Library/Caches/brickkit/repos/`。）放在用户级而不是每个项目的 `.brickkit/` 下，是因为同一个组件仓库会被很多项目、
以及组件自己的工作台共用——放在项目里，同一个仓库就要克隆 N 次。

要一个组件版本的 `component.yaml` 时，CLI 依次尝试：

| 顺序 | 从哪取 | 要不要网络 |
| --- | --- | --- |
| 1 | 项目的 `.brickkit/manifests/` 缓存 | 不要 |
| 2 | 缓存的 bare 仓库里的这个 tag（`git show`） | 不要 |
| 3 | `git fetch --tags`：仓库在、但还没有这个 tag | 增量 |
| 4 | `git clone --bare`：这个仓库从没克隆过 | 完整克隆 |

所以第一次安装要联网，之后同一个版本随时可以离线使用；发了新版本，只取增量。克隆先落到临时目录、完成后再改名，中途被打断不会留下半个仓库。

## 私有仓库与鉴权

**CLI 不管鉴权。** 它调用系统的 `git`，用的就是你的 git 已经配置好的凭据：

| 场景 | 怎么准备 |
| --- | --- |
| SSH 地址（`git@git.example.com:components/…`） | `~/.ssh/` 里有对应的 key，并已添加到托管平台 |
| HTTPS 地址 | 配好 git credential helper（store、cache、osxkeychain、manager……） |
| CI | 环境里注入 token（`GIT_ASKPASS`、`.netrc`，或 CI 平台自带的凭据） |

没有凭据时，CLI 让 `git` 直接失败而不是停下来等你输入密码——在 CI 里，那会让流水线挂住。失败时 git 的原话会原样给你：

```text
❌ 拉取组件失败：example/member
   安装源：company-git（git）
   仓库：https://git.example.com/components/example-member
   Git 错误：fatal: Could not read from remote repository.
   组件：example/member@0.1.0
   建议：
   1. SSH：确认 ~/.ssh/ 下有对应的 key，且已添加到仓库托管平台
   2. HTTPS：确认已配置 git credential helper（store、cache、osxkeychain、manager……）
   3. CI/CD：确认环境里已注入 token（GIT_ASKPASS、.netrc 或 CI 平台的凭据）；仓库地址拼错、仓库不存在也会是这个报错
```

（git 的原话随托管平台与失败原因不同；这里节选了其中一行。）

平台不碰凭据，是因为 git 已经把这件事解决得很好：SSH、各种 credential helper、CI 平台的注入方式，每一种都有成熟的做法。
CLI 另搞一套只会多一个要维护的地方，而且永远追不上所有托管平台。

## 怎么发现有哪些组件

平台没有注册表，也不提供搜索：组件在哪、有哪些，靠约定和文档——比如把组件都放在同一个 Git 组织下，
在组织的首页或者一份内部 wiki 上列出来，每个组件的 `BRICKKIT.md` 说清它是什么。需要一个可搜索的目录时，组件市场（`sources[].type: market`）是另一条路。
