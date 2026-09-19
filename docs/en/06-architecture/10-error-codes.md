# Error Codes

Every error that ends a `brickkit` command carries a **stable error code**. This page is the dictionary: what each code means, the situations that produce it, and what to do next.

The CLI's messages are written in Chinese. This page quotes them verbatim, so you can search for exactly what your terminal printed.

## Reading a failure

A failing command prints a human-readable block and, directly after it, one JSON log line — both on stderr:

```
❌ 错误：项目配置文件不存在
   路径：brickkit.yaml
   建议：
   1. 在项目目录中执行 brickkit init <项目名称> 初始化项目
   2. 或用 --config 指定正确的配置文件路径
{"time":"2026-09-19T00:56:05+02:00","level":"ERROR","message":"命令执行失败","command":"brickkit up","elapsed_ms":0,"error_code":"PROJECT_MISSING","error":"PROJECT_MISSING: 错误：项目配置文件不存在; 路径=brickkit.yaml","exit_code":1}
```

The `error_code` in that log line is what this page is organized by. The first line of the block — the title — says which situation within that code you are in. The log line is on at the default log level (`info`); `--log-level off`, or `BRICKKIT_LOG_LEVEL=off`, silences it.

## What the codes promise

- **They are stable.** Codes are only ever added — never renamed, never reused for something else, never removed. A script can branch on them.
- **A code is a category, not a single situation.** `CONFIG_INVALID` sits behind seventy-odd different messages. The tables below list the ones you are likely to meet, each under the title the CLI prints.
- **Exit status.** `0` — success, including a run that printed warnings. `1` — the command failed. `2` — the command line itself was wrong: a missing or malformed argument, an unknown command or flag, or an argument that names something that isn't there (`brickkit remove` of a component that isn't in `brickkit.yaml`).
- **For scripts.** `NETWORK_UNREACHABLE` is the one worth retrying unchanged: the network, or the Market, may come back. `CONFIG_INVALID` fails identically however often you retry. Treat any other code as "something has to change first".
- **Warnings are separate.** A ⚠️ block never fails a command. The ones the CLI prints while it carries on don't produce the log line at all, so you recognize them by their title — see [Warnings](#warnings).

## Usage and internal errors

### INVALID_ARGUMENT

The command line is wrong. Every one of these is fixed by changing what you typed; `brickkit <command> --help` shows the usage.

| You'll see | Cause | What to do |
| --- | --- | --- |
| `请指定要添加的组件` | `brickkit add` with no component | Give one — `brickkit add people/basic@1.0.0` — or use `--local` to add everything in a local source |
| `请指定要移除的组件` | `brickkit remove` with no component | `brickkit remove people/basic` |
| `请指定项目名称：brickkit init <项目名称>` | `brickkit init` with no project name | `brickkit init my-shop` |
| `错误：组件 ID 不合法：<id>` | An ID that isn't of the form `<scope>/<name>` | Use an ID like `people/basic` |
| `错误：版本号不合法：<version>` | Not an exact `major.minor.patch` — ranges such as `^1.0.0` are rejected on purpose | Write the exact version |
| `错误：未知命令 <command>` | A mistyped command name | `brickkit --help` lists the commands |
| `错误：参数不合法` | An unknown or malformed flag | `brickkit <command> --help` |
| `错误：日志级别不合法` | `--log-level` (or `BRICKKIT_LOG_LEVEL`) isn't one of the accepted levels | Use one of `debug`, `info`, `warn`, `error`, `off` |

### NOT_IMPLEMENTED

Reserved. It dates from the skeleton stage, when a command could be a placeholder; no command produces it today. It stays in the list because codes are never removed. If you ever see it, that's a bug worth reporting.

### INTERNAL

Something failed that isn't about your input — usually writing a file in the working directory — or the CLI hit an error it doesn't classify. Any error that isn't otherwise classified is reported under this code, with its own text as the title.

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：创建生成目录失败` | The directory for generated files couldn't be created | Check write permission on the project directory |
| `错误：写入部署文件失败` | The generated Compose/Kubernetes file couldn't be written | Same — permission or disk space |
| `错误：写入本地调试环境变量文件失败` | `local-debug.env` couldn't be written | Same |
| `错误：写入 pre-commit hook 失败` | The hook file under `.git/hooks/` couldn't be written | Check the permissions of `.git/hooks/` |
| `错误：AI 助手技能装入失败` | The project was created, but the AI assistant skills couldn't be installed | Fix the permission problem and run `brickkit skills update`; the skills aren't needed for anything else |
| `错误：缺少项目标签选择器，已中止删除` | A safety stop: `brickkit down` on Kubernetes refused to delete without a project label to scope it | Report it — it means the project name didn't reach the selector |

## Configuration

### CONFIG_INVALID

The widest code. Either `brickkit.yaml` (or something it points at) is invalid, or a precondition on the state of the project isn't met. Validation failures list every problem in the block at once, one line per field, so one round of edits fixes them all.

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：brickkit.yaml 校验失败` | One or more fields are invalid; each is listed below the title with its reason, for example `components[1].exposePort：与 components[0].exposePort 冲突` | Fix each listed field — the [brickkit.yaml Field Reference](08-brickkit-yaml-reference.md) has every constraint |
| `错误：项目配置文件不是合法的 YAML` | A YAML syntax error | Check the indentation at the reported line |
| `错误：项目配置文件内容为空` | The file exists but is empty | Write a `brickkit.yaml`, or run `brickkit init` in a fresh directory |
| `错误：项目名称不合法` | The name isn't lowercase letters, digits, and hyphens starting and ending with a letter or digit — it becomes a Kubernetes namespace and a Docker network name | Pick a name of that shape |
| `错误：必填的组件配置没有值` | A key in a component's `configSchema.required` has no `default` and the project supplies no value — `up` is blocked because the variable would otherwise silently never exist | Set it under that component's `config` in `brickkit.yaml` |
| `错误：brickkit.yaml 里引用的环境变量没有定义` | A `${VAR}` reference has no value (Kubernetes manifests can't defer substitution, so it's resolved at generation time) | Define it in `.env` at the project root, `export` it, or give a default: `${VAR:-dev}` |
| `错误：local: true 只能在 deploy.target: docker 下使用` | A Pod in a cluster can't reach a process on your laptop | Remove `local: true`, or set the target back to `docker` |
| `错误：deploy.target: k8s 下 expose: true 的组件必须写 hostname` | An Ingress without a host would match every domain | Add `hostname:` |
| `错误：域名 <hostname> 被多个组件占用` | Two components declare the same `hostname` | Give each its own |
| `错误：当前连着的不是配置里指定的集群` | The current `kubectl` context differs from `deploy.context` | `kubectl config use-context <name>`, or `brickkit up --context <name>` |
| `错误：servedBy 指向的组件不存在` | The `servedBy` value names a component that isn't in the project | Check the shell's ID and version |
| `错误：servedBy 指向的外壳当前没有在运行` | The shell is turned off (`enabled: false`) | Turn it back on, or drop `servedBy` so the component deploys on its own |
| `错误：外壳 <shell> 下两个成员对同一个环境变量给出了不同的值` | Two members of one shell depend on different versions of the same component | Align them on one exact version, or don't put them in the same shell |
| `错误：没有可用的安装源` | `sources` is empty or every source is disabled | Add at least one source; for local development, a `type: local` source at `./components` |
| `错误：本地安装源路径不存在` | A `local` source's `path` is wrong (it's relative to `brickkit.yaml`) | Correct the path, or set that source to `enabled: false` |
| `错误：安装源类型不合法：<type>` | `type` isn't `market`, `git`, or `local` | Use one of the three |
| `错误：可信公钥不可用` | An `installer.publicKeys` entry can't be used: wrong path, or not a PEM public key | Point it at the `.pub` file `cosign generate-key-pair` produced — never at `cosign.key`, the private key |
| `错误：找不到 cosign` | `brickkit publish --sign` needs cosign to sign (installing never does) | Install cosign, and make sure it's on `PATH` |
| `错误：这里不是一个 git 仓库` | `brickkit init --hooks` outside a Git repository | `git init` first |
| `错误：这个仓库还没有任何提交` | `brickkit restore` restores to the last commit, and there isn't one | Commit once first |
| `错误：<path> 没有被 git 跟踪` | `brickkit restore` was pointed at something Git doesn't track | `git add` and commit it once so there is a baseline |
| `错误：源码删掉就找不回来了` | `brickkit remove` refuses to delete component source with changes that exist nowhere else | Commit and push it, or copy the directory away; `--force` if you're sure. To just stop using a component, `enabled: false` then `brickkit sync` archives it instead of deleting it |

### CONFIG_CONFLICT

Two things that can't both hold: what you asked for collides with what already exists.

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：<component>@<version> 已经发布过了` | Publishing a version that's already on the Market — versions can't be reused | Bump `metadata.version` in `component.yaml` and publish again |
| `错误：<component>@<version> 上次建好了但没发完，而这次的 Manifest 与那份不一样` | An earlier publish of this version was created but never completed, and this Manifest differs from that one | Bump the version — or, to complete the earlier one, put `component.yaml` back as it was |
| `错误：pre-commit hook 已存在，不是 brickkit 写的` | `.git/hooks/pre-commit` exists and BrickKit never overwrites a hook it didn't write (it may be husky, lefthook, or yours) | Add the lines it prints to your own hook, or delete the file and re-run `brickkit init --hooks` |
| `错误：正在解决冲突，先把冲突处理完` | `brickkit restore` during a merge in progress | Resolve the merge first |
| `错误：有组件的源码在两处都存在` | The same component's source exists both active and archived | Keep one copy; the block names both paths |
| `提交被拦下：同一个组件的源码在提交里出现了两处` | The pre-commit hook: a commit would contain the same component's source in two places | Unstage one of them |
| `提交被拦下：组件源码提交在归档目录里，但 <path> 说它该启动` | The pre-commit hook: the source was archived but `enabled` in `brickkit.yaml` didn't come along with it | Commit the matching `brickkit.yaml` change too — or run `brickkit restore` |

The Market can also answer a publish with a version conflict; that surfaces under this code as well.

### PROJECT_EXISTS

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：项目已初始化，无需重复执行 init` | `brickkit init` in a directory that's already a project | Nothing to do — or `init` in a different directory |

### PROJECT_MISSING

The command needs a BrickKit project and there isn't one here.

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：项目配置文件不存在` | No `brickkit.yaml` at the path (default: the current directory) | Run it in the project root, run `brickkit init <name>` first, or point at the file with `--config` |
| `错误：项目未初始化` | The same, reached through a command that edits the config | Same |
| `错误：当前目录既不是 BrickKit 项目，也不是组件仓库` | `brickkit skills` found neither a `brickkit.yaml` nor a `component.yaml` | Run it in a project root, or in a component repository |
| `错误：这里没有 <file>，不是一个 BrickKit 项目` | `brickkit init --hooks` in a directory without a `brickkit.yaml` | `brickkit init <name>` first — it installs the hook when the project root is the repository root |

## Manifest and dependencies

### MANIFEST_INVALID

A `component.yaml` can't be used. The Manifest has no extension mechanism: an unrecognized key is rejected on the spot, not ignored.

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：component.yaml 校验失败` | A required field is missing or malformed, or a key isn't recognized; each problem is listed below the title | Fix each listed field — the [component.yaml Field Reference](07-component-yaml-reference.md) has every constraint |
| `错误：component.yaml 不是合法的 YAML` | A YAML syntax error | Check the indentation at the reported line |
| `错误：component.yaml 不存在` | The directory doesn't contain one | Check the `--path`, or the directory the source points at |
| `错误：component.yaml 内容为空` | The file exists but is empty | Write the Manifest |
| `错误：组件目录中没有 component.yaml` | `brickkit publish` was pointed at a directory without one | `--path` at the component's source directory — an archived one (`components/.archived/…`) works |
| `错误：artifacts 声明的文件不存在` | An `artifacts[].files` path doesn't exist — often a contract that hasn't been generated yet | Generate the file (protobuf, OpenAPI), or fix the path |
| `错误：component.yaml 里没有 deployment 段，无法钉住 digest` | Publishing pins the image digest, and there's no `deployment` section to pin | Add `deployment` |
| `错误：<component>@<version> 的 component.yaml 用不了` | A fetched Manifest failed to parse or validate | The block says why; if it came from the Market, tell the publisher |
| `错误：市场返回的 Manifest 为空` | The Market returned nothing for that version | Check the Market is healthy |
| `错误：市场返回的 Manifest 无法解析` | The Market returned something that isn't a Manifest | Check the Market's version is compatible |

### DEPENDENCY_MISSING

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：强依赖缺失` | A required dependency isn't in any source; the block names the component and the missing dependency | Check `sources` in `brickkit.yaml`, confirm that exact version was published, then `brickkit add` it |
| `无法移除 <component>` | `brickkit remove` on a component that others require | Remove the dependents first |

Weak (optional) dependencies missing is a warning, not this error — see [Warnings](#warnings).

### DEPENDENCY_CYCLE

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：检测到循环依赖` | Required dependencies form a loop; the block prints the whole loop | Make one edge on the loop optional (`optional: true`). A weak dependency doesn't constrain start order, so a loop with one weak edge is no longer a deadlock |

### VERSION_AMBIGUOUS

| You'll see | Cause | What to do |
| --- | --- | --- |
| `<component> 存在多个版本（<versions>），请指定版本：` | `brickkit remove people/basic` while more than one version is in `brickkit.yaml` | `brickkit remove people/basic@1.0.0` |

### COMPONENT_DISABLED

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：强依赖 <component> 被禁用` | Something that runs requires it, but it is `enabled: false` — or you pinned `enabled: true` on a component whose required dependency is off: two conflicting intents | Remove `enabled: false` from the dependency, or remove `enabled: true` from the dependent so it follows the top |

### COMPONENT_NOT_FOUND

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：组件未找到` | No enabled source has that ID; the block lists the sources tried | Check `sources`, the ID, and that the version is published |
| `错误：安装源里有这个组件，但版本不是要的那个` | The source has the component, just not that version | Check the version |
| `错误：<component> 不在 brickkit.yaml 中` | `brickkit remove` of a component the project doesn't list (exit status `2`) | Check the ID; `brickkit status` lists what's there |
| `错误：<component>@<version> 不在 brickkit.yaml 中` | The same, for a specific version | Check the version — several can coexist |

The Market answering "no such component" is reported under this code too.

### COMPONENT_BLOCKED

The Market has delisted that version (`blocked`) — the last line of defense the trust model relies on. It can't be installed, and logging in doesn't change that. Use a different version, or ask the Market's administrator why. The message text comes from the Market, so there's no fixed title to search for.

## Resources and ports

### RESOURCE_UNBOUND

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：资源依赖未满足` | A component declares a resource dependency (`dependencies.resources`: a `kind` and an `engine`) that `brickkit.yaml` doesn't satisfy — no matching `resources` entry, or no `bindings` entry for that component. Every unmet resource is listed at once. A real `up` is blocked; `up --dry-run` only warns, so you can still see what would be generated | Add the resource with a binding for that component — see [Resource binding mechanics](05-resource-binding.md) — or, to keep the component off for now, `enabled: false` |

### PORT_CONFLICT

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：宿主机端口 <port> 被多个组件占用` | Two `expose: true` components end up on the same host port because neither writes `exposePort`, so both default to their own `deployment.port`. (If both write the *same* `exposePort` explicitly, it is `CONFIG_INVALID` instead: an `错误：brickkit.yaml 校验失败` naming both fields.) Either way it's caught at generation time, so you never see Docker's own "port is already allocated" | Give one an explicit, different `exposePort`, or drop `expose: true` — components reach each other over the container network without it |
| `错误：外壳 <shell> 上有两个组件都要用端口 <port>` | Members of one `servedBy` shell all end up in the same container or Pod | Give them different ports |

## Migration and engine

### MIGRATION_FAILED

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：数据库迁移失败` | The migration Job failed on Kubernetes; the main service is deliberately not started, and the Job doesn't retry (`backoffLimit: 0`) | `kubectl logs job/<migration-job>`; fix the script and re-run `brickkit up` — the CLI deletes the old Job first |

On Docker the migration is a one-shot Compose service, and its failure surfaces as `ENGINE_FAILED`, with Compose's own output above the block.

### MIGRATION_SKIPPED

Only ever a warning, never an error — see [Warnings](#warnings). It says the platform isn't running a migration for a `local: true` or `servedBy` component, so the migration is yours to run.

### ENGINE_FAILED

Docker Compose or `kubectl` ran and failed. The engine's own raw output is printed above the block and usually says why.

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：部分组件没有正常启动` | `up` finished but some containers aren't healthy | `brickkit status`, then the container's logs. A component that takes longer than 30 seconds to start needs `healthCheck.startPeriodSeconds` |
| `错误：<command> 执行失败` | The engine command itself exited non-zero | Read the raw output above the block |
| `错误：kubectl 执行失败` | A `kubectl` call failed | Same |
| `错误：无法解析容器引擎的状态输出` | The Docker Compose installed is older than V2 | Upgrade Compose — `brickkit version` prints the detected engine |
| `错误：无法解析 kubectl 的输出` | `kubectl` printed something the CLI couldn't read | Check the `kubectl` version |
| `错误：没能从 registry 取到镜像 digest` | Publishing pins each image by digest and the registry didn't answer | Push the image first, and make sure this machine can reach the registry |
| `错误：无法确定镜像的 digest，发布已中止` | Same, for the final resolution | `docker push <image>`; `docker login` for a private registry. `--no-pin-digest` skips pinning, but then the signature covers only the tag |

### ENGINE_MISSING

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：找不到容器引擎 <engine>` | The engine binary isn't installed | Install Docker 20.10+ |
| `错误：没有找到可用的容器引擎` | No engine found at all | Install Docker 20.10+ — or use `brickkit up --dry-run` to generate the files without one |
| `错误：暂不支持 Podman，请使用 Docker` | Only Podman is installed. Podman support was built and then withdrawn: `down` fails on rootless Podman, and a project that can't be torn down is worse than one that never came up | Install Docker |
| `错误：找不到 kubectl` | `deploy.target: k8s` needs `kubectl` | Install it, or switch `deploy.target` to `docker` for local work |

## Network, authentication, and images

### NETWORK_UNREACHABLE

The one code worth retrying unchanged.

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：市场不可达` | The Market's address didn't answer | Check the network and the Market URL in `sources` (or `--market`), then retry |
| `错误：无法连接镜像仓库` | The image registry didn't answer | Check the network and the registry address |
| `错误：读取市场响应失败` | The connection dropped mid-response | Retry |
| `错误：市场返回的内容无法解析` | The URL answers, but it isn't a BrickKit Market | The URL should end in the Market's `/api/v1` |
| `错误：市场返回的内容格式不符` | The Market speaks a different protocol version | Check the Market's version |
| `错误：市场返回的版本列表无法解析` | Same, for the list of versions | Same |
| `错误：市场返回的产物列表无法解析` | Same, for the list of artifacts | Same |
| `错误：<component> 的产物一个都没下载成功` | `brickkit fetch` couldn't download any artifact | Check the network, and that the component declares artifacts |

### AUTH_REQUIRED

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：发布失败：未登录` | `brickkit publish` without a login | `brickkit login`, or set `sources.authToken` |
| `错误：无法确定要登录的市场地址` | `brickkit login` doesn't know which Market | `brickkit login --market https://market.example.com/api/v1` |
| `错误：brickkit.yaml 中没有可用的市场安装源` | No `type: market` source to log in to | Add one to `sources`, or pass `--market` |
| `错误：配置了多个市场安装源，无法确定登录哪一个` | More than one Market source | `--market` picks one |

### AUTH_FAILED

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：登录失败：用户名或密码错误` | Wrong credentials | Check them; the Market's administrator resets a forgotten password |
| `错误：市场没有返回访问令牌` | The login answered without a token | Check the Market's version is compatible |
| `错误：读取登录凭据失败` | `.brickkit/credentials` couldn't be read | `brickkit login` again |
| `错误：登录凭据格式不合法` | That file is corrupt | Delete it, then `brickkit login` |
| `错误：写入登录凭据失败` | It couldn't be written | Check the directory's permissions |
| `错误：删除登录凭据失败` | `brickkit logout` couldn't delete it | Check the permissions, or delete it by hand |

A private component refusing you also arrives under this code; the Market's own message says so, and the fix is to confirm the account owns the component.

### TOKEN_EXPIRED

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：Token 已过期` | The stored token is past its lifetime | `brickkit login` again |

### IMAGE_UNAUTHORIZED

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：镜像拉取未授权` | The registry refused the pull | `docker login <registry>`, and confirm the account may pull that image |
| `错误：镜像不存在` | The image isn't in the registry, or not built locally | Check the spelling and tag of `deployment.image`. A component under local test needs its image built first — the CLI never builds one for you |

## Signing

### SIGNATURE_INVALID

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：组件未签名，安装被阻断` | `installer.requireSignature` is on (the default) and the component carries no signature | Ask the publisher to republish with `brickkit publish --sign`; for local development, `requireSignature: false` |

A signature that doesn't verify against your `installer.publicKeys` also arrives under this code, with a message assembled at runtime. The usual cause is a public key that doesn't match the key the publisher signed with — see [Signing and the trust model](06-signing-and-trust.md) and [Troubleshooting](../08-troubleshooting.md).

## Source workspace

### CLONE_FAILED

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：Git 仓库克隆失败` | The clone failed | Check the network and the repository address; a private repository needs Git credentials; or set that source to `enabled: false` |
| `错误：clone 失败` | `--repo` couldn't clone the component's repository | Same |
| `clone 失败：目录已存在` | The destination directory already exists | If it's a mistake, delete or rename it; if the source is already there, you don't need to clone |
| `clone 失败：源码已经在了，只是被归档着` | The source exists but is archived under `components/.archived/` | `brickkit sync` brings it back according to what should run |
| `clone 失败：该组件为闭源组件` | Closed-source components have no repository to clone | Nothing to clone |
| `clone 失败：没有可用的 Git 仓库地址` | The source that provides this component has no Git URL | Put a Git source ahead of it in `sources`, or drop `--repo` |
| `错误：无法创建源码目录` | The destination couldn't be created | Check the permissions |

### SUBMODULE_GUARD

| You'll see | Cause | What to do |
| --- | --- | --- |
| `错误：无法删除组件源码——它是一个已登记的 git submodule` | `brickkit remove` won't touch a registered submodule: a plain delete or rename doesn't understand `.gitmodules` and would silently detach its history | Run the `git submodule deinit` / `git rm` commands the block lists, then re-run |
| `错误：无法移动组件源码——它是一个已登记的 git submodule` | `brickkit sync` won't move one either | Do the equivalent `git mv` the block lists, check `.gitmodules` and `git status`, then re-run `brickkit sync` |

## Warnings

A ⚠️ block never fails a command (exit status `0`). The ones printed while the CLI carries on don't produce the `error_code` log line, so this table is keyed by title; the code is what the same message carries in the CLI's source, for the curious.

| You'll see | Code | Meaning and what to do |
| --- | --- | --- |
| `警告：弱依赖缺失：<component>` | `DEPENDENCY_MISSING` | An optional dependency isn't available. Its `*_ENDPOINT` is not injected at all — not even as an empty string — so the component must read it defensively (`os.environ.get`, never `os.environ["X"]`) |
| `警告：<component> 被弱依赖` | `DEPENDENCY_MISSING` | You're removing something other components depend on optionally. It's allowed; they'll run without it |
| `警告：无法确认 <component> 的依赖关系` | `MANIFEST_INVALID` | `brickkit remove` couldn't read another component's Manifest, so it couldn't check whether that one needs the component being removed |
| `brickkit.yaml 中存在明文密码` | `CONFIG_INVALID` | A password is written literally. Use `password: ${DB_PASSWORD}` and put the value in `.env`, which must be in `.gitignore` |
| `brickkit.yaml 的 config 里可能写了明文密钥` | `CONFIG_INVALID` | A `config` key named like a secret has a literal value. The check goes on the name alone, never the value; if it isn't a secret, ignore it |
| `config 里有配置项不会生效：组件 <component> 的 <key>` | `CONFIG_INVALID` | A `config` key isn't in that component's `configSchema.properties` — usually a typo, and the CLI suggests the key it thinks you meant. Without this check the variable would simply never exist and the component would quietly use its default |
| `config 整块不会生效：组件 <component> 没有声明 configSchema` | `CONFIG_INVALID` | You wrote `config` for a component that declares no `configSchema`, so none of it takes effect |
| `警告：configSchema 里有配置项声明的键不会生效` | `MANIFEST_INVALID` | A property under `configSchema` has a misspelled key (`defualt:`). Shown when publishing or adding from a local source |
| `配置冲突：组件 <component> 的配置项已被忽略` | `CONFIG_CONFLICT` | A `configSchema` key, uppercased, collides with a reserved variable (`*_ENDPOINT`, `DATABASE_*`, …); the platform's value wins and the key is skipped. Rename the key — see the [Environment variable contract](04-environment-variables.md) |
| `基础资源的 host 看起来是个服务名，容器里可能解析不了` | `CONFIG_INVALID` | A resource `host` looks like a Compose service name, but resources aren't part of the project. Use `host.docker.internal` for one on your machine, or its real address |
| `配置里有只对 <target> 生效的字段` | `CONFIG_INVALID` | A field that only applies to the other `deploy.target` — for example `exposePort` under `k8s` — is doing nothing |
| `local: true 的组件上，labels 本次不生效` | `CONFIG_INVALID` | A `local: true` component has no container to label. Remove `local: true` to get platform-managed labels back |
| `提示：local 组件的数据库迁移不会自动执行` | `MIGRATION_SKIPPED` | A `local: true` component runs on your machine, so the CLI runs no migration for it. Run the migration command yourself once, with the variables from its `local-debug.<service>.env` |
| `提示：servedBy 组件的数据库迁移不会自动执行` | `MIGRATION_SKIPPED` | A `servedBy` member has no container of its own, so it has no migration container. The shell has to cover it |
| `提示：servedBy 组件自己的健康检查不会独立生效` | `CONFIG_INVALID` | The shell's health check is the one that counts |
| `提示：servedBy 组件上，<field> 本次不生效` | `CONFIG_INVALID` | `expose`, `exposePort`, `hostname`, `replicas`, `resources`, `serviceAccountName`, and `labels` describe how a component's own container is deployed, and a `servedBy` member has none. To deploy the component on its own, drop `servedBy` |
| `警告：requireSignature 为 true，但项目没有声明任何可信公钥，签名校验实际未生效` | `SIGNATURE_INVALID` | With zero `installer.publicKeys`, verification is off entirely — `requireSignature: true` alone doesn't verify anything. Declare the publisher's public key, or set `requireSignature: false` explicitly to silence this |
| `警告：签名来自未声明的发布者，未做校验` | `SIGNATURE_INVALID` | The signature names a publisher whose key you haven't declared, so it wasn't checked |
| `警告：产物下载失败，已跳过` | `NETWORK_UNREACHABLE` | An artifact couldn't be downloaded; the install carried on without it |
| `跳过组件结构检查：<reason>` | `CONFIG_INVALID` | The pre-commit hook couldn't run its structure check this time, and the commit goes ahead. `brickkit restore --check` runs it by hand |

## Read further

- [Troubleshooting](../08-troubleshooting.md) — the failures people actually hit, organized by what you see rather than by code.
- [Environment variable contract](04-environment-variables.md) — the reserved-variable warnings, with real examples.
- [CLI Command Reference](09-cli-reference.md) — every command and flag.
