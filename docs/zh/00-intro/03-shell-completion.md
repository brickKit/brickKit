# 命令补全

敲 `brickkit` 再敲 `rem`，按一下 TAB，shell 替你把词补完：`brickkit remove`。再按 TAB，它列出你项目里的组件——`demo/bus`、`demo/caller`、
`demo/hello`——你挑一个就行，不用自己敲。这就是**补全**：shell 提示接下来能写什么，TAB 把它填上。它省按键，更有用的是省掉打错字：
从列表里挑出来的组件 ID 不可能拼错。

用 `install.sh` 装的话，多半已经设置好了——开一个新终端试试。这一篇剩下的部分讲它设置了什么、怎么确认、以及怎么手动设置。

## 为什么 shell 需要一个文件

补全是 **shell** 的功能，不是程序的功能。你按 TAB 的时候，拿主意的是 bash、zsh 或 fish；`brickkit` 这时候还没运行。所以得事先告诉 shell 一次：
"遇到 `brickkit` 这条命令，这样去问它要候选"。这句话就是一小段脚本，每种 shell 都从自己固定会找的几个目录里加载这类脚本。

`brickkit completion <shell>` 打印的就是这段脚本。所谓"装补全"，就是把它的输出放进对的目录——这也是为什么什么办法都改变不了你已经打开的那个终端：
正在运行的 shell 早就加载完它的脚本了，新开的终端才会加载新的。

## `install.sh` 已经做了什么

装好 CLI 之后，`install.sh` 给它在你机器上找到的每一种 shell 写一份脚本，放进那种 shell 自己的用户级目录。它从不改你的 `~/.bashrc`、
`~/.zshrc` 或者任何属于你的文件。它的输出最后是这样的：

```text
Shell completion:
   bash  /home/you/.local/share/bash-completion/completions/brickkit (loaded by the bash-completion package)
   zsh   /home/you/.zsh/completions/_brickkit
   Add this one line to ~/.zshrc, above line 96 (source $ZSH/oh-my-zsh.sh) —
   that line turns completion on, so the folder has to be on fpath before it:
           fpath=(~/.zsh/completions $fpath)
   Open a new terminal (or run: exec $SHELL) for completion to take effect.
```

（`install.sh` 在 CLI 装好之前运行，只说英文。）上面是装了 Oh My Zsh 的机器上的输出：意思是"在 `~/.zshrc` 第 96 行
（`source $ZSH/oh-my-zsh.sh`）**上面**加这一行"。zsh 那几行具体要你加什么，取决于你的 `~/.zshrc`——见
[zsh：告诉它文件在哪](#zsh告诉它文件在哪)。

- **bash**：一个文件，由 `bash-completion` 这个包加载（没有这个包的话见 [bash](#bash)）。
- **zsh**：zsh 的搜索路径（`$fpath`）上有能写的 `site-functions` 目录时——比如 Homebrew 的那个——文件就放那里，别的什么都不用做。
  没有的话放到 `~/.zsh/completions`，`~/.zshrc` 里要加一两行——加什么，`install.sh` 会读你的 `~/.zshrc` 判断（见下一节）。
- **fish**：`~/.config/fish/completions/brickkit.fish`；fish 不用任何设置就会加载。
- **PowerShell**：机器上有 `pwsh` 时，`install.sh` 会打印要加进配置文件的那一行。

不想要这些，安装时在环境里设 `BRICKKIT_NO_COMPLETION=1`。

### zsh：告诉它文件在哪

`~/.zshrc` 里有两样东西决定 zsh 能不能补全 `brickkit`：

- **`fpath`** 是 zsh 去找补全文件的文件夹列表。`~/.zsh/completions` 默认不在里面，所以要一行把它加进去：
  `fpath=(~/.zsh/completions $fpath)`。
- **`compinit`** 打开补全功能。它**只在执行的那一刻**把 `fpath` 里的文件夹读一遍——之后才加进去的文件夹，它永远看不到。

所以 `fpath` 那一行必须放在执行 `compinit` 的地方**之前**。看你是哪种情况：

| 你的 `~/.zshrc` | 加什么 | 加在哪 |
| --- | --- | --- |
| 用 Oh My Zsh（有 `source $ZSH/oh-my-zsh.sh` 这一行） | 只加 `fpath=(~/.zsh/completions $fpath)` | `source $ZSH/oh-my-zsh.sh` 那一行的**上面**——Oh My Zsh 自己会执行 `compinit` |
| 已经用别的方式执行了 `compinit` | 只加 `fpath=(~/.zsh/completions $fpath)` | 那一行 `compinit` 的**上面** |
| 都没有 | `fpath=(~/.zsh/completions $fpath)` 和 `autoload -Uz compinit && compinit` 两行 | 文件末尾 |

`install.sh` 会读你的 `~/.zshrc`（不会改它），打印适合你的那一种，并点名加在第几行上面；`fpath` 那一行已经有了，它就说不用改。
用 Oh My Zsh 的话，改完是这样：

```bash
plugins=(git)

fpath=(~/.zsh/completions $fpath)   # 新加的——必须在下面这行之前
source $ZSH/oh-my-zsh.sh
```

这种情况下别再在文件末尾加 `autoload -Uz compinit && compinit`：补全照样能用，但每开一个终端 zsh 都会初始化两遍，启动变慢。
改完开一个新终端（或者 `exec zsh`）。

### 确认一下

开一个新终端，敲 `brickkit` 再敲 `rem`，按 TAB。变成了 `brickkit remove`，就好了。

不想试、只想看，就问 shell 知不知道怎么补全 `brickkit`。bash 里：

```bash
complete -p brickkit
```

```text
complete -o default -F __start_brickkit brickkit
```

zsh 里：

```bash
print -r -- $_comps[brickkit]
```

```text
_brickkit
```

什么都没打印，说明脚本没加载——见[补全不出来](#补全不出来)。

## 手动设置

没走 `install.sh` 的安装——`go install`、下载的压缩包、`make install`——或者想重做一遍时用。每条命令写出的文件和 `install.sh` 写的一样。
`brickkit completion <shell> --help` 打印的也是这些步骤。

### bash

bash 的补全要 `bash-completion` 这个包，大多数 Linux 桌面已经装了：

```bash
sudo apt install bash-completion      # Debian、Ubuntu
sudo dnf install bash-completion      # Fedora
```

macOS 自带的 bash 太旧，用不了它；把新版 bash 和这个包一起装上，再照 Homebrew 打印的那几行，在 `~/.bash_profile` 里加载它：

```bash
brew install bash bash-completion@2
```

然后写文件：

```bash
mkdir -p ~/.local/share/bash-completion/completions
brickkit completion bash > ~/.local/share/bash-completion/completions/brickkit
```

### zsh

```bash
mkdir -p ~/.zsh/completions
brickkit completion zsh > ~/.zsh/completions/_brickkit
```

再按 [zsh：告诉它文件在哪](#zsh告诉它文件在哪) 把 `fpath` 那一行加进 `~/.zshrc`——用 Oh My Zsh 的话，只加
`fpath=(~/.zsh/completions $fpath)`，放在 `source $ZSH/oh-my-zsh.sh` 那一行上面。

### fish

```bash
mkdir -p ~/.config/fish/completions
brickkit completion fish > ~/.config/fish/completions/brickkit.fish
```

### PowerShell

把这一行加进你的配置文件（`$PROFILE` 指向的那个文件）：

```powershell
brickkit completion powershell | Out-String | Invoke-Expression
```

### 只在当前终端里试

什么都不装、先试一下：把它加载进你现在这个 shell，关掉终端就没了：

```bash
source <(brickkit completion bash)    # 或者：source <(brickkit completion zsh)
```

## 能补全什么

| 在这之后 | TAB 给出 |
| --- | --- |
| `brickkit` | 命令 |
| `remove`、`deps`、`build` | 项目 `brickkit.yaml` 里的组件；敲了 `<id>@` 之后，是这个组件在项目里的版本 |
| `upgrade` | 项目 `brickkit.yaml` 里的组件；敲了 `<id>@` 之后，是本机已知的版本 |
| `up --focus` | 项目 `brickkit.yaml` 里的组件 |
| `add` | 本地安装源里有的组件，以及项目清单缓存里有的组件；敲了 `<id>@` 之后，是本机已知的版本 |
| `-f` / `--file` | 项目根目录下的 `deploy*.yaml` |
| `lang set`、`skills update --lang` | CLI 支持的语言 |

补全只读你机器上的文件：从不连 Git 服务器或市场，所以按 TAB 立刻就有，断网也能用。"本机已知"指本地安装源、项目的清单缓存，
或者这台机器以前拉过的 Git 仓库。它在项目的任何子目录里都能用，和命令本身一样；在项目外面，它不给组件候选，也什么都不打印。

## 补全不出来

- **终端是在安装之前打开的。** 它在启动时就加载完了脚本。开一个新终端，或者运行 `exec $SHELL`。
- **zsh：少了 `fpath` 那一行，或者它放在了 `compinit` 后面**——用 Oh My Zsh 的话，就是放在了 `source $ZSH/oh-my-zsh.sh` 后面。
  用 `print -r -- $_comps[brickkit]` 看；什么都没打印就是 zsh 没加载这个文件。把那一行挪上去（见 [zsh：告诉它文件在哪](#zsh告诉它文件在哪)）。
- **zsh：文件放在了 `$fpath` 之外的地方。** `print -l $fpath` 列出 zsh 会去找的目录。
- **bash：`bash-completion` 没装，或者没加载。** `type _init_completion` 应该说它是一个函数；不是的话，装上这个包（见 [bash](#bash)）。
- **上一次安装留下的旧脚本。** 新版的 `brickkit` 能补全的东西，旧脚本可能不知道怎么去问。重新跑一遍 `install.sh`，或者用上面对应 shell 的命令重写这个文件。

下一篇：[核心概念](04-core-concepts.md)。所有命令见[命令参考](../07-cli-reference/README.md)；五分钟走一遍见[快速开始](02-quick-start.md)。
