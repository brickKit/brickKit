# 升级与配置迁移

组件作者发布了新版本。升级不只是改一个版本号：新版本可能加了配置项、删了配置项、改了默认值，你写在 `config/` 里的值要跟着搬过去。
`brickkit upgrade` 把这几件事一次做完。

## 三种写法

| 写法 | 做什么 |
| --- | --- |
| `brickkit upgrade` | 所有有新版本的组件都升到最新——要么全做，要么一个都不改 |
| `brickkit upgrade demo/hello` | 只升这一个，到最新 |
| `brickkit upgrade demo/hello@2.0.0` | 升（或降）到指定版本 |

不写版本时只往上走：安装源里最新的比项目里的还旧，就算已经最新，绝不悄悄降级。

## 先预览：`--dry-run`

```bash
brickkit upgrade --dry-run
```

```text
📋 upgrade 会做这些：
   ⬆️  demo/hello：1.0.0 → 1.1.0
   ✅ demo/hello@1.0.0（requiredBy: demo/caller）
📝 config/demo-hello.yaml
   ⚠️  冲突 GREETING：你的值 你好，新默认值 Hi
📝 会写：brickkit.yaml, deploy.yaml, deploy.local.yaml

⚠️  这些配置文件会留下冲突块：config/demo-hello.yaml——真正升级时终端里会逐条问你；--yes 或没有终端输入时写成冲突块，up 在解决之前拒绝启动

💡 --dry-run：一个文件都没有改
```

预览是在项目文件的一份副本上真的做一遍，所以会失败的升级在预览里就会失败。这里能读出三件事：

1. `demo/hello` 的默认版本从 1.0.0 移到 1.1.0；
2. `demo/caller@1.0.0` 还依赖 `demo/hello@1.0.0`，所以 1.0.0 **留下来**，标上是谁要它；
3. 你把 `GREETING` 改成了"你好"，而 1.1.0 把它的默认值从 Hello 改成了 Hi——这是一处冲突。

## 真正升级

```bash
brickkit upgrade
```

```text
demo/hello 的 GREETING：你的值 你好（1.0.0），新默认值 Hi（1.1.0）——[m] 留你的 / [n] 用新的 / 回车两行都留下、之后自己选：
   ⬆️  demo/hello：1.0.0 → 1.1.0
   ✅ demo/hello@1.0.0（requiredBy: demo/caller）
📝 config/demo-hello.yaml
   ⚠️  冲突 GREETING：你的值 你好，新默认值 Hi
📝 已写：brickkit.yaml, deploy.yaml, deploy.local.yaml

⚠️  这些配置文件里留下了冲突块，up 在解决之前会拒绝启动：config/demo-hello.yaml——用纯文本编辑器打开，保留一行、删掉另一行和说明注释
```

每处冲突在终端里逐条问你：`m` 留你的值，`n` 用新默认值，直接回车则两行都留下、之后自己选（上面就是直接回车的结果）。
`--yes` 或没有终端输入时（脚本、CI），一律两行都留下。

升级改了这些文件：

```yaml
# brickkit.yaml
components:
  - id: demo/hello
    version: 1.1.0
  - id: demo/bus
    version: 1.0.0
  - id: demo/caller
    version: 1.0.0
  - id: demo/hello
    version: 1.0.0
    requiredBy: [demo/caller]
```

部署文件多了留下来的那个版本的条目（`deploy.yaml`，以及存在的 `deploy.local.yaml`）：

```yaml
  - id: demo/hello@1.0.0
```

`config/demo-hello.yaml` 属于默认版本（1.1.0），迁移过了；留下的 1.0.0 拿到一份带版本号的配置文件
`config/demo-hello@1.0.0.yaml`，里面是你原来的值，一个字都没动：

```yaml
# Component: demo/hello@1.0.0
# 这个组件的环境变量，每个键原样注入。
# 公共变量：$var:NAME（config/vars.yaml）· 环境变量：${NAME} · 本地文件：file://path

# === 可选：注释掉的键使用组件默认值，取消注释即可覆盖 ===
GREETING: 你好
```

没有组件再依赖旧版本时，旧版本不会留下，它的配置移进 `config/.archive/`。

## 配置怎么迁移

对每一个配置项，按下表机械地决定，不猜重命名、不做类型转换：

| 情况 | 结果 |
| --- | --- |
| 你没写（骨架里还是注释） | 写成新版本的骨架行，跟随新的默认值 |
| 你写了，新版本的默认值没变 | 你的原文照抄——`$var:`、`${VAR}`、`file://`、引号、写在它上方的注释，一个字符都不动 |
| 你写了，而你的值正好等于旧默认值 | 能确认你没改过它：跟随新默认值 |
| 你写了、值不等于旧默认值，而新版本改了默认值 | **冲突**：你改过它，作者也改过它，谁对只有你知道 |
| 新版本删掉了这一项 | 不写进新文件，值留在旧版本的文件里（归档到 `config/.archive/`；旧版本留下时，就是改名后的 `config/<组件>@<旧版本>.yaml`），报告里列出来 |
| 新版本新增的项 | 写成骨架行；必填又没有默认值的写成 `KEY: ""`，`up` 会拦住直到你填上 |

你写的注释跟着它说的那一项走：文件开头的仍在开头，写在某一项上方的仍在那一项上方（不论那一项你写了值，还是仍是一条注释掉的骨架行），
写在最后一项之后的仍在末尾。新版本删掉的那一项，它上方的注释随它一起去掉。骨架自己生成的说明和分节标题按新版本重新生成，不会多出一份。
块标量（`|`、`|+`、`>` 这类多行写法）按原文搬过去，解析出来的值与原来一字不差。

旧版本不再保留时，它原来的整份配置文件存进 `config/.archive/<组件>@<旧版本>.yaml`，随时能对照。

## 解决冲突

冲突写成两行重复的键：

```yaml
# ⚠️ 配置冲突：升级到 1.1.0 时这个键的建议值变了。
# 保留一行、删掉另一行和本注释；在那之前 brickkit 拒绝启动。
GREETING: 你好  # brickkit:conflict current 1.0.0
GREETING: Hi  # brickkit:conflict proposed 1.1.0
```

在那之前，`up` 拒绝启动：

```text
❌ 错误：检测到未解决的配置冲突
   文件：config/demo-hello.yaml
   配置项：GREETING
   第 8 行：你好（你之前的值，1.0.0）
   第 9 行：Hi（1.1.0 的建议值）
   建议：
   1. 用纯文本编辑器打开文件，保留你要的那一行，删掉另一行和上方的注释，然后重新执行命令
   2. 不要对这个文件用 yq 或编辑器的"格式化文档"：它们会悄悄丢掉其中一个重复键，冲突没解决就消失了
   💡 编辑器可能把这个文件标成非法 YAML——这是正常的：BrickKit 故意写了重复键，让冲突不可能被忽略
```

为什么用重复键这种"坏的 YAML"：它让冲突**不可能被忽略**。换成一行注释提醒，文件照样能跑，而跑起来的值可能是错的。
用纯文本编辑器留下你要的那一行（行尾的 `# brickkit:conflict …` 标记也可以一起删掉），删掉另一行和两行说明：

```yaml
GREETING: 你好
```

然后照常 `build`（新版本的镜像）、`up`。两个版本同时在跑：

```text
📋 组件状态计算：
   ✅ demo/hello@1.1.0   启动（顶层）
   ✅ demo/bus@1.0.0     启动（demo/caller 需要）
   ✅ demo/hello@1.0.0   启动（demo/caller 需要）
   ✅ demo/caller@1.0.0  启动（顶层）
```

## 组件来自本地源时

`add --repo` 把一个组件的源码克隆进 `components/` 之后，这个组件就由本地源提供——本地源排在前面，第一个有它的源说了算。
这时它的"最新版本"就是**工作区里 `component.yaml` 写的版本**，Git 上更新的 tag 不算：

```text
✅ 所有组件都是最新版本
ℹ️ 这些组件来自本地源，“最新”就是它们工作区里 component.yaml 的版本——远端更新的 tag 不算：
   demo/hello@1.0.0（本地源 local-dev）
   💡 要换版本：在它的源码目录里检出那个版本（git checkout <版本>），再 brickkit upgrade
```

这是有意的：你把源码克隆下来，就是要跑你手里的这份代码。要升级，在源码目录里检出新版本：

```bash
git -C components/demo/hello checkout 1.1.0
brickkit upgrade
```

## 外壳

升级一个外壳时，它编进的成员跟着换成**新外壳声明的那一套版本**——外壳的镜像里编进的就是那些版本，成员不能单独升级。
升级之后成员的兼容性怎么核对、成员版本对不上时的出路，见 [外壳升级](../04-shell/07-shell-upgrade.md)。

## 升级之后

- 需要本机构建的组件要重新 `brickkit build`（新版本的镜像还不存在，`up` 会点名提醒）。
- 用 `-f` 部署的其它环境的部署文件，`upgrade` 不改——输出末尾会点名它们，自己把新条目补过去。
- 把 `brickkit.yaml`、部署文件、`config/` 的改动一起提交：这三个文件共同描述了这次升级。
