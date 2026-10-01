# Shell completion

Type `brickkit` and then `rem`, press TAB, and the shell finishes the word: `brickkit remove`. Press TAB again and it offers the
components in your project — `demo/bus`, `demo/caller`, `demo/hello` — so you pick one instead of typing it. That is
**completion**: the shell suggests what can come next, and TAB fills it in. It saves keystrokes, and more usefully it
saves typos: a component ID you pick from a list can't be misspelled.

If you installed with `install.sh`, it is probably already set up — open a new terminal and try it. The rest of this
page explains what was set up, how to check it, and how to do it by hand.

## Why the shell needs a file

Completion is the **shell's** feature, not the program's. When you press TAB, bash, zsh or fish decide what to offer;
`brickkit` isn't even running yet. So the shell needs to be told, once, "for the command `brickkit`, ask it for
candidates like this". That instruction is a small script, and each shell loads such scripts from directories it
already searches.

`brickkit completion <shell>` prints that script. Installing completion means putting its output in the right
directory — which is also why nothing can change the terminal you already have open: a running shell has already
loaded its scripts. A new terminal loads the new one.

## What `install.sh` already did

After installing the CLI, `install.sh` writes the script for every shell it finds on your machine, into that shell's
own user-level directory. It never edits your `~/.bashrc`, `~/.zshrc` or any other file of yours. Its output ends like
this:

```text
Shell completion:
   bash  /home/you/.local/share/bash-completion/completions/brickkit (loaded by the bash-completion package)
   zsh   /home/you/.zsh/completions/_brickkit
   Add this one line to ~/.zshrc, above line 96 (source $ZSH/oh-my-zsh.sh) —
   that line turns completion on, so the folder has to be on fpath before it:
           fpath=(~/.zsh/completions $fpath)
   Open a new terminal (or run: exec $SHELL) for completion to take effect.
```

That is the output on a machine with Oh My Zsh. What the zsh lines ask for depends on your `~/.zshrc` — see
[zsh: telling it where the file is](#zsh-telling-it-where-the-file-is).

- **bash**: one file; the `bash-completion` package loads it (see [bash](#bash) if you don't have that package).
- **zsh**: when zsh has a writable `site-functions` directory on its search path (`$fpath`) — Homebrew's, for
  instance — the file goes there and there is nothing more to do. Otherwise it goes to `~/.zsh/completions`, and
  `~/.zshrc` needs one or two lines, which `install.sh` reads your `~/.zshrc` to work out (next section).
- **fish**: `~/.config/fish/completions/brickkit.fish`; fish loads it without any setup.
- **PowerShell**: when `pwsh` is present, `install.sh` prints the one line to add to your profile.

To skip all of this, install with `BRICKKIT_NO_COMPLETION=1` in the environment.

### zsh: telling it where the file is

Two things in `~/.zshrc` decide whether zsh can complete `brickkit`:

- **`fpath`** is the list of folders zsh looks in for completion files. `~/.zsh/completions` isn't on it by default,
  so a line adds it: `fpath=(~/.zsh/completions $fpath)`.
- **`compinit`** turns completion on. It reads the folders on `fpath` **once, at the moment it runs** — a folder added
  after that is never seen.

So the `fpath` line has to come **before** whatever runs `compinit`. Which case you are in:

| Your `~/.zshrc` | What to add | Where |
| --- | --- | --- |
| Uses Oh My Zsh (has `source $ZSH/oh-my-zsh.sh`) | Only `fpath=(~/.zsh/completions $fpath)` | **Above** the `source $ZSH/oh-my-zsh.sh` line — Oh My Zsh runs `compinit` itself |
| Already runs `compinit` some other way | Only `fpath=(~/.zsh/completions $fpath)` | **Above** that `compinit` line |
| Neither | `fpath=(~/.zsh/completions $fpath)` and `autoload -Uz compinit && compinit` | At the end |

`install.sh` reads your `~/.zshrc` (without changing it) and prints the right one, naming the line number to put it
above; if the `fpath` line is already there, it says there is nothing to change. With Oh My Zsh the result looks like
this:

```bash
plugins=(git)

fpath=(~/.zsh/completions $fpath)   # added — before the next line
source $ZSH/oh-my-zsh.sh
```

Don't also add `autoload -Uz compinit && compinit` at the end in that case: completion still works, but zsh then sets
it up twice every time a terminal opens, which makes start-up slower. Open a new terminal (or `exec zsh`) afterwards.

### Checking it

Open a new terminal, type `brickkit` and then `rem`, and press TAB. If it becomes `brickkit remove`, you're done.

To look without trying, ask the shell whether it knows how to complete `brickkit`. In bash:

```bash
complete -p brickkit
```

```text
complete -o default -F __start_brickkit brickkit
```

In zsh:

```bash
print -r -- $_comps[brickkit]
```

```text
_brickkit
```

No output means the script isn't loaded — see [It doesn't complete](#it-doesnt-complete).

## Setting it up by hand

For installs that didn't go through `install.sh` — `go install`, a downloaded archive, `make install` — or to redo it.
Each command writes the same file `install.sh` would. `brickkit completion <shell> --help` prints the same steps.

### bash

bash completion needs the `bash-completion` package, which most Linux desktops already have:

```bash
sudo apt install bash-completion      # Debian, Ubuntu
sudo dnf install bash-completion      # Fedora
```

On macOS, the bash that ships with the system is too old for it; install a current bash together with the package, and
follow the lines Homebrew prints to load it from `~/.bash_profile`:

```bash
brew install bash bash-completion@2
```

Then write the file:

```bash
mkdir -p ~/.local/share/bash-completion/completions
brickkit completion bash > ~/.local/share/bash-completion/completions/brickkit
```

### zsh

```bash
mkdir -p ~/.zsh/completions
brickkit completion zsh > ~/.zsh/completions/_brickkit
```

then add the `fpath` line to `~/.zshrc` as [zsh: telling it where the file is](#zsh-telling-it-where-the-file-is)
explains — with Oh My Zsh, only `fpath=(~/.zsh/completions $fpath)`, above the `source $ZSH/oh-my-zsh.sh` line.

### fish

```bash
mkdir -p ~/.config/fish/completions
brickkit completion fish > ~/.config/fish/completions/brickkit.fish
```

### PowerShell

Add this line to your profile (the file `$PROFILE` names):

```powershell
brickkit completion powershell | Out-String | Invoke-Expression
```

### Just this terminal

To try it without installing anything, load it into the shell you are in; it's gone when you close the terminal:

```bash
source <(brickkit completion bash)    # or: source <(brickkit completion zsh)
```

## What gets completed

| After | TAB offers |
| --- | --- |
| `brickkit` | Commands |
| `remove`, `deps`, `build` | The components in your project's `brickkit.yaml`; after `<id>@`, that component's versions in the project |
| `upgrade` | The components in your project's `brickkit.yaml`; after `<id>@`, the versions known on this machine |
| `up --focus` | The components in your project's `brickkit.yaml` |
| `add` | Components your local install sources provide, and components whose manifests the project has cached; after `<id>@`, the versions known on this machine |
| `-f` / `--file` | The `deploy*.yaml` files at the project root |
| `lang set`, `skills update --lang` | The CLI's languages |

Completion only reads files on your machine: it never contacts a Git server or a market, so TAB is instant and works
offline. "Known on this machine" means a local install source, the project's manifest cache, or Git repositories this
machine has fetched before. It works in any subdirectory of a project, the same way the commands do; outside a project
it offers no components and prints nothing.

## It doesn't complete

- **The terminal was open before the install.** It loaded its scripts at start-up. Open a new one, or run
  `exec $SHELL`.
- **zsh: the `fpath` line is missing, or it comes after `compinit`** — with Oh My Zsh, after the
  `source $ZSH/oh-my-zsh.sh` line. Check with `print -r -- $_comps[brickkit]`; empty output means zsh never loaded the
  file. Move the line up (see [zsh: telling it where the file is](#zsh-telling-it-where-the-file-is)).
- **zsh: the file is somewhere not on `$fpath`.** `print -l $fpath` lists where zsh looks.
- **bash: the `bash-completion` package isn't installed or isn't loaded.** `type _init_completion` should say it is a
  function; if it doesn't, install the package (see [bash](#bash)).
- **An old script from an earlier install.** A new `brickkit` version can complete things the old script doesn't know
  how to ask for. Run `install.sh` again, or rewrite the file with the command for your shell above.

Next: [Core concepts](04-core-concepts.md). Every command is in the [CLI reference](../07-cli-reference/README.md); the
five-minute walkthrough is the [Quick start](02-quick-start.md).
