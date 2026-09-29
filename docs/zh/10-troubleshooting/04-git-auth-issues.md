# Git 鉴权问题

组件来自 Git 仓库时，BrickKit 调用的是你机器上的 `git`，用的是 `git` 已经配置好的凭据——SSH key、credential helper、CI 注入的 token。
平台自己不存、不管任何凭据。所以这一类问题几乎都在 `git` 与托管平台之间，而不是 BrickKit 里。背景见 [Git 分发](../03-component-guide/10-git-distribution.md)。

先分清是哪一类：看报错里 **Git 错误** 那一行 git 自己的原话。

| Git 的原话里有 | 是哪一类 | 看哪一节 |
| --- | --- | --- |
| `Permission denied (publickey)` | SSH key 不对 | [SSH 鉴权失败](#ssh-鉴权失败) |
| `Host key verification failed` | 没确认过这台主机的指纹 | [第一次连一台 SSH 主机](#第一次连一台-ssh-主机) |
| `could not read Username … terminal prompts disabled` | HTTPS 没有凭据 | [HTTPS 鉴权失败](#https-鉴权失败) |
| `Could not resolve host`、`Connection refused`、`timed out` | 根本没连上 | [离线或连不上](#离线或连不上) |

## SSH 鉴权失败

**症状**

```text
❌ 拉取组件失败：demo/hello
   安装源：company-git（git）
   仓库：git@github.com:brickkit-nonexistent-org/demo-hello
   Git 错误：git@github.com: Permission denied (publickey).
      fatal: Could not read from remote repository.
      Please make sure you have the correct access rights
      and the repository exists.
   组件：demo/hello@1.0.0
   建议：
   1. SSH：确认 ~/.ssh/ 下有对应的 key，且已添加到仓库托管平台
   2. HTTPS：确认已配置 git credential helper（store、cache、osxkeychain、manager……）
   3. CI/CD：确认环境里已注入 token（GIT_ASKPASS、.netrc 或 CI 平台的凭据）；仓库地址拼错、仓库不存在也会是这个报错
```

**原因**

托管平台不接受你的 SSH key：key 没有添加到平台上、用的不是那把 key、或者你对这个仓库没有权限。仓库地址拼错、仓库根本不存在时，
很多平台为了不泄露"仓库存不存在"，也回答同一句话。

**解决**

1. 脱离 BrickKit 单独试：`git ls-remote git@github.com:<组织>/<仓库>`。它也失败，问题就与 BrickKit 无关。
2. `ssh -T git@github.com`（或你的托管平台）看平台认不认你的 key。
3. 核对仓库地址：它由安装源的 `baseUrl` 加上组件 ID 推出来（`demo/hello` → `demo-hello`），报错里的"仓库"一行就是推出来的结果。

## 第一次连一台 SSH 主机

**症状**

在终端里执行时，命令停下来问你：

```text
The authenticity of host 'github.com (140.82.121.4)' can't be established.
ED25519 key fingerprint is: SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU
This key is not known by any other names.
Are you sure you want to continue connecting (yes/no/[fingerprint])?
```

回答 `no`，或者在没有终端的环境里（CI、脚本）执行，得到的是：

```text
   Git 错误：Host key verification failed.
      fatal: Could not read from remote repository.
```

**原因**

`~/.ssh/known_hosts` 里还没有这台主机的指纹，SSH 要你确认"连的确实是它"。

BrickKit 设置了 `GIT_TERMINAL_PROMPT=0`，让 `git` 在缺凭据时直接失败、不在 CI 里挂住等一个永远不会来的输入。但这个变量只管 `git` 自己的提问
（HTTPS 的用户名、密码）。**SSH 的提问由 `ssh` 直接从终端读**——确认主机指纹、给带口令的私钥输口令——`GIT_TERMINAL_PROMPT` 管不到它们。
所以有终端时你会被问到；没有终端时 `ssh` 读不到回答，直接失败。

**解决**

- 本机：核对指纹与托管平台公布的一致之后回答 `yes`，之后就不会再问。
- CI：事先把主机指纹写进 `known_hosts`。`ssh-keyscan github.com >> ~/.ssh/known_hosts` 能取到指纹，但它本身不验证真伪——
  把取到的指纹与平台公布的对照一次，再固定写进 CI 的配置里。
- 私钥带口令：本机用 `ssh-agent` 事先加载（`ssh-add`）；CI 里用不带口令、只读权限的部署 key，或者改用 HTTPS 加 token。

## HTTPS 鉴权失败

**症状**

```text
❌ 拉取组件失败：demo/hello
   安装源：company-git（git）
   仓库：https://github.com/brickkit-nonexistent-org/demo-hello
   Git 错误：fatal: could not read Username for 'https://github.com': terminal prompts disabled
```

**原因**

`git` 需要用户名和密码（或 token），而没有 credential helper 能提供，它又被禁止在终端里问你（`GIT_TERMINAL_PROMPT=0`）。
仓库不存在时，不少平台也先要求登录，所以同样是这一句。

**解决**

- 本机：配一个 credential helper（`git config --global credential.helper store`、`cache`、`osxkeychain`、`manager`……），
  然后用 `git ls-remote <仓库地址>` 手动登录一次，凭据就存下了。
- 核对仓库地址，确认它真的存在、你有权限。

## CI 环境的鉴权

CI 里没有终端，也没有你的个人凭据。BrickKit 不提供任何 CI 专用的鉴权参数——CI 平台注入凭据的方式本来就很成熟，`git` 都认：

| 方式 | 做法 |
| --- | --- |
| HTTPS + token | 用 `.netrc`（`machine git.example.com login <用户> password <token>`），或 `GIT_ASKPASS` 指向一个输出 token 的脚本，或 CI 平台自带的凭据注入 |
| SSH 部署 key | 把只读的部署 key 放进 CI 的密钥存储，运行时写进 `~/.ssh/`（权限 600）；主机指纹事先写进 `known_hosts`（见上一节） |
| 改写地址 | `git config --global url."https://<token>@git.example.com/".insteadOf "https://git.example.com/"`，项目里的 `baseUrl` 不用改 |

token 不要写进 `brickkit.yaml`：它要进 Git。

## 离线或连不上

**症状**

```text
❌ 拉取组件失败：demo/bus
   安装源：company-git（git）
   仓库：https://git.example.com/components/demo-bus
   Git 错误：fatal: unable to access 'https://git.example.com/components/demo-bus/': Could not resolve host: git.example.com
   组件：demo/bus
   建议：
   1. 连不上仓库：检查网络，以及仓库地址里的主机名；网络恢复后重试
   2. 查最新版本要联网。离线时写明版本号（demo/bus@<版本>）：本机缓存里已有的版本不用联网——1.0.0
```

**原因**

没连上远端：离线、主机名拼错、代理或防火墙挡住了。

同样离线，**写不写版本号结果不同**：

| 命令 | 离线时 |
| --- | --- |
| `brickkit add demo/bus@1.0.0` | 本机的仓库缓存里已经有这个版本的 tag，就直接用，不联网 |
| `brickkit add demo/bus`（不写版本） | 要知道"最新"是哪个版本，必须问远端——失败 |

"最新版本"只以远端为准：缓存里最高的那个版本未必是远端的最新，拿它冒充"最新"，你会以为自己装的是最新版。所以 CLI 不猜，而是把缓存里已有的版本列出来，让你明确选一个。
`brickkit upgrade <组件>`（不写版本）同理。

**解决**

联网后重试；或者照提示写明一个缓存里已有的版本。仓库缓存在用户级目录（Linux 上 `~/.cache/brickkit/repos/`），同一台机器上的所有项目共用——
在一个项目里联网取过的版本，在别的项目里离线也能用。
