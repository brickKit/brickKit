# deploy.local.yaml 详解

## 它是什么

你个人的部署文件：结构与 `deploy.yaml` 完全相同，**不进 Git**。用来写只属于你、只属于此刻的部署方式——
"我正在 IDE 里调这个组件"（`mode: debug`）、"我机器上 8080 被占了"、"我连自己的数据库"、
"我这台机器上用 Podman"。

## 完整替换，不是属性覆盖

本地模式开着时，运行或检查部署的命令（`up`、`down`、`status`、`sync`、`lint`、`build`）**只读** `deploy.local.yaml`，完全不看 `deploy.yaml`。
它不是"在团队文件上叠几个字段"。（`graph` 与 `deps` 例外：它们始终读 `deploy.yaml`，因为它们的输出是要分享的，不能随谁的机器而变。）

为什么这么做：叠加（覆盖层）意味着你要在脑子里把两份文件合起来，才知道到底生效的是什么；出了问题，
你得同时读两份文件、再加一套合并规则。完整替换时，你打开的那份文件就是实际生效的全部——所见即所得。
代价是团队改了 `deploy.yaml` 之后你要同步，这由下面的一致性校验和 `refresh` 负责。

## `brickkit local`

```bash
brickkit local on
```

```text
✅ 本地模式已开启：命令现在读取 deploy.local.yaml
   deploy.local.yaml 从 deploy.yaml 复制而来——随意修改，它不会被提交
```

第一次 `on` 时把 `deploy.yaml` 复制过来，内容一字不差；唯一的例外是 `init` 写的那段文件头注释（"团队文件：请提交它"）换成了个人文件的说明——那句话放在一个从不提交的文件里是假话：

```yaml
# deploy.local.yaml —— 你个人的部署文件：本地模式开着时，命令读它而不是 deploy.yaml。
# 从 deploy.yaml 复制而来，不会被提交；团队改了 deploy.yaml 之后，brickkit local refresh 重新复制。
target: docker          # docker | podman | k8s
```

之后随便改它，比如：

```yaml
components:
  - id: demo/hello
    mode: debug
    localPort: 18080
```

| 子命令 | 做什么 |
| --- | --- |
| `brickkit local on` | 开启本地模式；没有 `deploy.local.yaml` 时从 `deploy.yaml` 复制一份，有就接着用 |
| `brickkit local off` | 关闭本地模式；**不删**文件，下次 `on` 接着用 |
| `brickkit local status` | 开关状态、文件在不在、与 `brickkit.yaml` 是否一致 |
| `brickkit local refresh` | 按当前的 `deploy.yaml` 重新生成，旧文件备份为 `deploy.local.yaml.bak`，并列出旧文件里的每一处本地修改 |

开关状态记在 `.brickkit/` 里，只属于这台机器。

```bash
brickkit local status
```

```text
本地模式：开启
deploy.local.yaml：存在，正在使用
✅ deploy.local.yaml 与 brickkit.yaml 一致
```

## 严格一致性校验

规则：`deploy.local.yaml` 列出的组件，必须与 `brickkit.yaml` 声明的一一对应，和 `deploy.yaml` 的要求一样。

它通常在你 `git pull` 之后被触发：队友加了一个组件，`brickkit.yaml` 和 `deploy.yaml` 都变了，而你的个人文件没变。
此时任何读部署文件的命令都会停下来：

```bash
brickkit up --dry-run
```

```text
❌ 错误：deploy.local.yaml 已过期，与 brickkit.yaml 的组件对不上
   文件：deploy.local.yaml
   缺少条目：demo/caller
   原因：你开启了本地模式，而 brickkit.yaml 变了，你的 deploy.local.yaml 没有同步
   建议：
   1. 方案 A（推荐）：brickkit local refresh 按 deploy.yaml 重新生成 deploy.local.yaml，旧文件备份为 deploy.local.yaml.bak
   2. 方案 B：在 deploy.local.yaml 里手动增删下面列出的条目
   3. 方案 C：brickkit local off 切回团队的 deploy.yaml
```

为什么不自动补上：自动补等于替你决定新组件在你机器上怎么跑，而你可能正需要它 `mode: debug`。大声停下来、
给出三条路，比悄悄猜一个更好。`lint` 不管本地模式开没开，都会检查 `deploy.local.yaml`（存在的话）。

你自己用 `add` / `remove` / `upgrade` 时，CLI 会同时改 `deploy.yaml` 和 `deploy.local.yaml`，个人文件不会因此落后。

## `refresh` 与本地修改的合并

```bash
brickkit local refresh
```

```text
✅ 已从 deploy.yaml 生成最新的 deploy.local.yaml。旧文件已备份至 deploy.local.yaml.bak。
ℹ️ 旧文件中有 2 处本地修改，请把仍然需要的手动合并到新的 deploy.local.yaml 中：
   - [demo/hello] mode: debug（当前未设置）
   - [demo/hello] localPort: 18080（当前未设置）
```

`refresh` 不替你合并：它列出旧文件里每一处与团队文件不同的地方，你决定哪些还要。旧文件在 `.bak` 里。

什么算"本地修改"，以**你的文件当初复制自的那一份**为准：`local on` 与 `refresh` 把那份团队文件存为 `.brickkit/deploy.local.base.yaml`。
你改过或加上的值、你**删掉**的字段，都是本地修改；你没动过的值不是，哪怕团队后来改了它——刷新之后它自然跟随团队的新值。
清单里只列与团队文件现在不同的修改。比如你为了本地调试换掉了团队写的 `expose`：

```text
ℹ️ 旧文件中有 4 处本地修改，请把仍然需要的手动合并到新的 deploy.local.yaml 中：
   - [demo/hello] mode: debug（当前未设置）
   - [demo/hello] localPort: 18080（当前未设置）
   - [demo/hello] expose: 本地删掉了（当前为 `true`）
   - [demo/hello] exposePort: 本地删掉了（当前为 `18080`）
```

没有这份记录时（`deploy.local.yaml` 是你手写的），`refresh` 只能比较新旧两份文件：列出旧文件里设了、新文件里没有或不一样的值，
而"你删掉了某个字段"和"团队后来加了它"分不开——那时请自己拿 `deploy.local.yaml.bak` 和新文件对照一下。

## 与 `-f` / `--no-local` 的关系

| 写法 | 读哪份部署文件 |
| --- | --- |
| 默认 | 本地模式开着时读 `deploy.local.yaml`，否则读 `deploy.yaml` |
| `--no-local` | 这一次读 `deploy.yaml`，本地模式开关不变 |
| `-f deploy.prod.yaml` | 这一次只读指定的文件，完全不看 `deploy.local.yaml` 与本地模式开关 |

`--no-local` 与 `-f` 都只管这一次命令。支持它们的命令见 [CLI 命令参考](../07-cli-reference/README.md)。

## 常见用法

- **在 IDE 里调一个组件：** `mode: debug` 加 `localPort`，其余组件照常在容器里跑，见 [本地调试工作流](../02-project-guide/03-local-debug-workflow.md)。
- **本机端口被占：** 改这个组件的 `exposePort`。
- **连自己的数据库：** 在 `vars:` 里覆盖对应的公共变量，比如 `PG_HOST: localhost`。
- **换一个引擎：** `target: podman`；团队文件是 `k8s` 时，个人文件写 `docker` 就能在本机跑起同一套组件。
