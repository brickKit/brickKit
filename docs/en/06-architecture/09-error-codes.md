# Error codes

Every error that makes a command fail carries a stable **error code**. It's for scripts and CI: retry, raise an alert, or
call someone.

## Where the error code shows

The error is printed on stderr in human-readable form first, followed at once by one JSON log line holding `error_code`:

```text
❌ Error: database migration failed
   Component: shop/stock@0.2.0
   View logs: docker compose -p brickkit-shop logs shop-stock-0-2-0-migration
   Suggestions:
   1. When a migration fails the main service does not start: it waits for the migration to finish successfully
   2. Fix it and run brickkit up again: the migration container runs once more
```

```json
{"time":"2026-09-30T01:26:21.455089775+02:00","level":"ERROR","message":"Command failed","command":"brickkit up","elapsed_ms":2104,"error_code":"MIGRATION_FAILED","error":"MIGRATION_FAILED: Error: database migration failed; Component=shop/stock@0.2.0; View logs=docker compose -p brickkit-shop logs shop-stock-0-2-0-migration","exit_code":1}
```

A script that needs to tell kinds of failure apart reads `error_code`; it doesn't match the human-readable text: that text
follows the CLI's language (`brickkit lang`) and gets reworded as versions improve; the error code doesn't.
`--log-level off` switches this line off too — use it only where no program needs to parse `error_code`.

The JSON line is for programs, so a person typing at a terminal doesn't get it: when stderr is a terminal and no level
was chosen — neither `--log-level` nor `BRICKKIT_LOG_LEVEL` — only the `❌` block is printed, which already says
everything but the code. Wherever stderr is captured — a script, CI, `2> file`, an AI assistant's shell — it isn't a
terminal, and the line is always there. A program that runs `brickkit` under a pseudo-terminal (`docker run -t`,
`expect`, `script`) and reads `error_code` chooses a level explicitly: `BRICKKIT_LOG_LEVEL=warn`. To see the line at
your own terminal, do the same.

## Exit codes

| Exit code | Meaning |
| --- | --- |
| `0` | Success (warnings alone are still 0) |
| `1` | Failure: configuration, dependencies, engine, network… anything where the command ran but didn't get it done |
| `2` | Wrong usage: an unknown command, an unknown flag, a `--file` pointing at a file that doesn't exist |

Under `lint --strict`, warnings make the exit code `1`.

## Which are worth retrying

**Only `NETWORK_UNREACHABLE` is worth retrying as it is**: a network hiccup, a market temporarily unreachable, an image
registry that can't be reached — a moment later it may work. For every other code, retrying as it is gives the same result
however many times — configuration doesn't fix itself, dependencies don't appear by themselves. `ENGINE_FAILED` sits in
between: an occasional engine failure can be retried once; if it keeps failing, look at the engine's own output.

Error codes are only ever added: an existing code is never renamed or removed; a new situation goes under an existing code,
or gets a new one.

## How to read each section below

Each section is one error code. The first column of the table is the error title the CLI prints (`<…>` marks a specific
value), which you can search for on this page as it is. One error code often has several specific situations behind it,
told apart by their titles.

### INTERNAL

Something went wrong in the CLI itself, or reading or writing a local file failed (a full disk, no permission). Not a
mistake in your configuration.

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: failed to write the deployment files` | The generated deployment files can't be written into `.brickkit/generated/` | Check disk space and directory permissions |
| `Error: failed to create the generated directory` | `.brickkit/generated/` can't be created | As above |
| `Error: failed to write the local-debug environment variable file` | `local-debug.*.env` can't be written | As above |
| `Error: failed to install the AI assistant skills` | `init` failed writing `.claude/skills/` | As above; or add `--no-skills` |
| `Error: failed to write the pre-commit hook` | `.git/hooks/pre-commit` can't be written | Check the permissions of `.git/hooks/` |
| `Failed to start the local process` | A `mode: local` process won't start | Read the system reason in the error |
| `Internal error: <…> cannot be evaluated to a value here` | An internal invariant of the CLI was broken | This is a bug; please report it with the whole output |

### INVALID_ARGUMENT

The command line is wrong. In most cases the exit code is `2`.

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: unknown command <…>` | There's no such command | `brickkit --help` lists every command |
| `Error: incorrect command usage` | The wrong number of arguments, an unknown flag | `brickkit <command> --help` |
| `Error: the deploy file given with --file does not exist` | The file `-f` points at isn't there | The path is relative to the project root; check the spelling |
| `Error: invalid component ID: <…>` | The component ID isn't of the form `<scope>/<name>` | Lowercase letters, digits and hyphens, with one `/` in the middle |
| `Error: invalid version: <…>` | Not an exact version | Write `major.minor.patch`; `^1.0.0` and `latest` aren't accepted |
| `Error: <…> has several versions (<…>); name the one to remove` | `remove` on a component with several versions, without a version | `brickkit remove <component>@<version>` |
| `Error: --init only works together with --local` | `add --init` on its own | Write `add --local --init` |
| `Error: --focus and --all contradict each other` | `up --focus` and `--all` in one command | Pick one |
| `Error: --focus and --all change deploy.local.yaml, which -f and --no-local skip` | `up --focus` or `--all` together with `-f` / `--no-local` | Drop `-f` / `--no-local`, or edit the `focus:` line of `deploy.local.yaml` yourself |
| `Error: <…> is a compatibility version (it has requiredBy); upgrade only moves default versions` | `upgrade` on a version with `requiredBy` | Upgrade the components depending on it; the compatibility version follows |
| `Error: invalid log level` | A wrong `--log-level` value | `debug`, `info`, `warn`, `error`, `off` |
| `Error: the user name must not be empty` | `login` got no user name | Type it at the prompt, or `--username` |

### NOT_IMPLEMENTED

A reserved error code: no command produces it at present.

### CONFIG_INVALID

Something is written wrong in the three layers, the install sources or the signature configuration. This is the most
common error code, with many situations behind it, told apart by their titles.

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: <…> failed validation` | The structure of `brickkit.yaml` or a deploy file is wrong: an unknown field, a wrong type, a required field missing | The error names the field and line; fix it there |
| `Error: <…> failed validation` (on the focus field) | `focus:` written in `deploy.yaml` (it's personal), with `target: k8s` (a cluster can't reach a process on your machine), or not a component ID; `up --focus` refuses to write such a focus | Keep `focus` in `deploy.local.yaml` on `docker` / `podman`; see [Developing inside the project](../02-project-guide/04-focus-run.md) |
| `Error: <…> is not valid YAML` | A YAML syntax error | Check indentation and colons at the line the error gives |
| `Error: <…> contains more than one YAML document (a stray --- line?)` | The file has an extra `---` line, and the part after it would be dropped silently | Delete the extra `---` |
| `Error: a required component config item has no value` | An item in `configSchema.required` has no default, and nothing was filled in under `config/` | Fill it in in `config/<component>.yaml` |
| `Error: config files reference shared variables that are defined nowhere` | `$var:NAME` can't be found | Define it in `config/vars.yaml` or under the deploy file's `vars:` |
| `Error: environment variables referenced in config/ or the deploy file are not defined` | When generating deployment files, `${VAR}` is found in neither the process environment nor `.env` (K8s needs the value; on Docker, compose would turn it into an empty string) | Set it in `.env` or the current shell, or write a default: `${VAR:-x}` |
| `Error: <…> points at a file that cannot be read` | The file `file://` points at doesn't exist or can't be read | The path is relative to the project root |
| `Error: the cluster currently connected is not the one the configuration specifies` | `kubectl`'s current context differs from the deploy file's `k8s.context` | Switch contexts, or use the deploy file naming that cluster |
| `Error: with target: k8s, a component with expose: true must set hostname` | Exposed on K8s without a domain name | Write `hostname` on the deploy entry |
| `Error: this change would leave a project that does not load; all three files were restored` | After `add` / `remove` / `upgrade` changed things, the project failed to load | Read the reasons the error lists; nothing was changed |
| `Error: <…> is still required by: <…>` | `remove` on a version that's still a required dependency | Remove or upgrade the components depending on it first |
| `Error: the source directory could not be recovered once deleted, so nothing was removed` | The source directory to delete has uncommitted or unpushed changes | Commit and push first, or add `--force` |
| `Error: the member versions this run hosts in a shell differ from the ones its component.yaml says are compiled in` | A shell hosts a member version it didn't compile in | The error gives three ways out; see [Upgrading a shell](../04-shell/07-shell-upgrade.md) |
| `Error: <…> is listed in the members of <…>, but that shell's component.yaml does not contain it` | The deploy file puts a component under a shell that didn't compile it in | Move the entry to the top level, or use a shell that compiles it in |
| `Error: <…> is marked kind: shell in brickkit.yaml, but its component.yaml declares no shell block` | `kind: shell` disagrees with the Manifest | Delete `kind: shell` |
| `Error: skipWaitFor names something that is not a required dependency` | `skipWaitFor` lists a component that isn't a required dependency | List only real required dependencies of this component version |
| `Error: config item <…> of shell member <…> is not valid UTF-8 text` | A shell member's config value can't be carried in JSON | base64-encode binary content first |
| `Error: no install source is available` | `brickkit.yaml` has no enabled install source | Add one under `sources` |
| `Error: the local install source path does not exist` | A local source's `path` points at a directory that doesn't exist | Change `path`, or create the directory |
| `Error: invalid project name` | The project name breaks the rules | Lowercase letters, digits and hyphens, starting and ending with a letter or digit |
| `Error: cosign not found` | `publish --sign` needs cosign, and it isn't installed | Install cosign (only publishers need it) |
| `Error: trusted public key unusable` | A public key file `installer.publicKeys` points at has a problem | Check the file's path and contents |
| `Couldn't determine how to start <…>` | The start command of a `mode: local` component can't be recognised | Write `language` or `runCommand` under `local` in `component.yaml` |
| `Code that runs from a local repository does not match this run` | For a component running as a process on this machine (`mode: local`, or the focus), the local repository's version isn't the one in `brickkit.yaml` — or there is no local repository at all | `brickkit upgrade <component>@<repository version>`, or check out the matching tag in the repository; without a repository, `brickkit add <component> --repo` |
| `This project already has a local session running (pid <…>) — stop it first, or switch to that terminal` | `up` in another terminal is supervising `mode: local` processes | `Ctrl+C` in that terminal |

The same code also has warnings (which don't make the command fail):

| Title | Situation | What to do |
| --- | --- | --- |
| `Warning: .gitignore is missing required entries — personal deploy files and secrets can be committed` | An existing `.gitignore` is missing entries, and `init` doesn't change it for you | Add each line listed |
| `<…>: <…> is not declared in the component's configSchema, so it has no effect` | A misspelled config key | Follow the "did you mean" suggestion |
| `Files under config/ may contain plaintext secrets` | A secret written in plain text, while `config/` goes into Git | Change it to `${VAR}` or `file://` |
| `existingSecret only works on K8s, and the current target is docker` | `existingSecret` used on Docker | The item isn't injected; on Docker use `${VAR}` or `file://` |
| `<…> has no effect with target: <…> and is ignored` | A field only useful for the other deploy target was written | It can stay; it takes effect when the target changes |

### CONFIG_CONFLICT

Two things say conflicting things, and the platform doesn't choose for you.

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: unresolved configuration conflicts` | A config file has duplicate keys: a conflict block left by an upgrade, or duplicated by hand | Keep one line and delete the other in a plain-text editor |
| `Commit blocked: component source is committed under the archive directory, but <…> says it should start` | The pre-commit check: `mode` and the directory layout disagree | `brickkit restore`, or commit the directory move together with it |
| `Commit blocked: the same component's source appears in two places in the commit` | It's in both the active and the archive directory | Delete one of them |
| `Error: a pre-commit hook already exists and wasn't written by brickkit` | Another pre-commit hook is already there | Add `brickkit restore --check` to that hook yourself |
| `Error: <…>@<…> has already been published` | The same version published to the market again | Raise the version |
| `Error: component source is nested inside another component's directory` | A component's source sits in another component's own `components/` (a workbench inside the project) — two copies of one component | Move or delete the nested copy yourself; the error says whether it exists anywhere else. BrickKit moves nothing |
| `Error: this workbench sits inside project <…>; --repo would clone a second copy here` | `add --repo` in a workbench that is itself a component of an enclosing project | Run `add --repo` in that project, or work on the component with a focus run there |

Warning: `Config conflict: the config item of component <…> was ignored` — a config item collides with a variable name the
platform reserves, and the platform's value wins. See
[The environment variable contract](03-env-injection-contract.md#reserved-names).

### DEPLOY_INCONSISTENT

The deploy file and `brickkit.yaml` disagree on components: an entry for a component version is missing, or there's an
entry `brickkit.yaml` doesn't have.

- When a deploy file disagrees, the error title names that file (e.g. "deploy.yaml does not match the components in
  brickkit.yaml"). The team changed `brickkit.yaml` and the deploy file didn't keep up: add or delete the entries listed.
- In local mode, the title is "deploy.local.yaml is out of date: it does not match the components in brickkit.yaml":
  `brickkit local refresh`, add the entries by hand, or `brickkit local off`.

### PROJECT_EXISTS

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: the directory <…> already exists and is not empty` | The target directory of `init <name>` already has something in it | Pick another name, or go in and run `init` without a name (it fills in what's missing) |
| `Error: <…> already exists and is not a directory` | Something of that name is a file | Pick another name |

### PROJECT_MISSING

The current directory isn't a BrickKit project, or a required file is missing.

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: project config file not found` | There's no `brickkit.yaml` | Go into the project directory, or `brickkit init` |
| `Error: deploy.yaml not found` | There's a `brickkit.yaml` but no deploy file | Run `brickkit init` in the project to fill it in |
| `Error: local mode is on, but deploy.local.yaml does not exist` | Local mode is on, but the file was deleted | `brickkit local off`, or `brickkit local on` to generate it again |
| `Error: there is no <…> to refresh` | `local refresh` before there's a `deploy.local.yaml` | `brickkit local on` first |
| `Error: the current directory is neither a BrickKit project nor a component repository` | `skills` run in some other directory | Go into a project or a component repository |

### MANIFEST_INVALID

A component's `component.yaml` has a problem — most likely for the component's author to fix.

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: <…> failed validation` | An unknown field, a required field missing, a version written as a range | The error names the field; as a user, contact the component's author |
| `Error: <…> does not exist` | The component directory has no `component.yaml` | Check the path |
| `Error: <…> is not valid YAML` | A YAML syntax error | Fix it at the line given |
| `Error: the component.yaml of <…> is unusable` | A Manifest fetched from an install source can't be parsed | Contact the component's author |
| `Error: the component ID in <…> doesn't match the directory name` | In a local source, `<scope>/<name>/` and `metadata.id` disagree | Rename the directory or change the ID |
| `Error: a file declared under artifacts does not exist` | When publishing, a file `artifacts.files` points at isn't there | Add the file or change the declaration |

Warning: `Warning: some keys declared on configSchema items won't take effect` — an item in `configSchema` has a misspelled
key (like `defualt`).

### DEPENDENCY_MISSING

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: required dependency missing` | A required dependency can't be found in the project | `brickkit add` it; or release it to the install source |
| `<…> is not declared in brickkit.yaml` | The version depended on isn't in `brickkit.yaml` | `brickkit add <dependency>@<version>` |

Warning: `Warning: optional dependency missing: <…>` — the optional dependency isn't there; start-up goes on, and its
address variable isn't injected.

### DEPENDENCY_CYCLE

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: dependency cycle detected` | Several components require each other in a loop, and no start order exists | Make one dependency on the loop optional (`optional: true`), or redraw the components |
| `Error: running the members inside shell <…> makes it wait for a component that in turn waits for the shell` | The components have no cycle, but merging them into a shell creates a wait cycle | The error gives three ways out; see [Managing members](../04-shell/04-members-management.md#start-cycles-caused-by-merging-and-skipwaitfor) |

### VERSION_AMBIGUOUS

A reserved error code: no command produces it at present. The situations where a version must be named because there
are several now report `INVALID_ARGUMENT`.

### COMPONENT_DISABLED

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: required dependency <…> is disabled` | A component pinned to run (`mode: enabled` / `debug` / `local`) has a required dependency written `mode: disable` | The two intents contradict each other: drop one |
| `Error: the focus <…> is written mode: disable` | The focused component's entry says `mode: disable` | Remove `mode: disable`, or focus on another component |

### COMPONENT_NOT_FOUND

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: component not found` | No install source has this component | Check the component ID and the install sources; a Git source needs a version tag |
| `Error: the install source has this component, but not the version that was asked for` | A local source holds another version | Write the right version, or use another install source |
| `The repository has no version <…>` | The Git repository has no tag for this version | Have the component's author `brickkit release` this version |
| `Error: <…> is not in the project` | `remove` on a component that isn't in the project | Check the component ID |
| `Error: the focus <…> is not a component of this project` | `focus:` or `--focus` names a component that isn't in `brickkit.yaml` (removed since, or a typo) | Follow the "did you mean"; `brickkit up --all` drops the focus |

### COMPONENT_BLOCKED

The component market marked this component version `blocked` (confirmed to be a problem), and it can't be installed any
more. Use another version, or contact the publisher.

### RESOURCE_UNBOUND

A reserved error code: no command produces it at present. BrickKit no longer has a "base resource" concept — database
addresses and the like are ordinary config items of the component.

### PORT_CONFLICT

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: two components on shell <…> both want port <…>` | A shell and one of its members, or two members, use the same port | Ports must all differ |
| `Error: host port <…> is claimed by more than one component` | Several processes on this machine or exposed ports collide on one host port | Change `localPort` or `exposePort` |

### MIGRATION_FAILED

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: database migration failed` | A component's migration exited non-zero (the migration container on Docker, the Job on K8s); the main service didn't start | Read the migration logs with the command the error gives, fix it, then `up` again |

### MIGRATION_SKIPPED

Only a warning: `Note: a mode: <…> component's database migration won't run automatically` — a component running as a
process on this machine has no migration container, and you run the migration once by hand.

### ENGINE_FAILED

The underlying engine (`docker compose`, `kubectl`) failed. The error carries the engine's own output, which usually says
why already.

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: <…> failed to run` | `docker compose` / `podman compose` returned a failure | Read the "Output" in the error |
| `Error: kubectl failed to run` | `kubectl` returned a failure | As above |
| `Error: some components did not start properly` | The engine finished, but some components aren't healthy | Read that component's logs as the hint says |
| `<…> local component(s) crashed` | A `mode: local` process exited unexpectedly | Read the last lines of output in the crash summary |

### ENGINE_MISSING

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: no usable container engine found` | Neither Docker nor Podman is installed | Install Docker (with Compose V2) |
| `Error: container engine <…> not found` | The engine for the deploy file's `target` isn't installed | Install it, or change `target` |
| `Error: kubectl not found` | `target: k8s`, but there's no `kubectl` | Install `kubectl` |
| `Error: Podman is installed, but not enabled` | Only Podman is installed, while the deploy file says `target: docker` | Change `target` to `podman` |

### NETWORK_UNREACHABLE

A network problem. **This is the only error code worth retrying as it is.**

| Title | Situation | What to do |
| --- | --- | --- |
| `Failed to fetch component <…>` | Fetching a component from a Git repository failed because the remote was never reached: offline, a host name that doesn't resolve, a refused connection | Check the network and the host name; retry once the network is back. Offline, name a version the local cache already has |
| `Error: the Market is unreachable` | The component market can't be reached | Check the network and the market address; retry later |
| `Error: could not reach the image registry` | The image registry can't be reached while checking images | Check the network; retry later |
| `Error: none of the artifacts of <…> could be downloaded` | Every artifact of a `fetch` failed to download | Retry later |
| `Error: could not ask the remote <…> which tags it has` | `release` failed to look up the remote's tags | Check the network and the remote address |

Warning: `Warning: artifact download failed and was skipped` — a single artifact failed to download; the command still
completes.

### AUTH_REQUIRED

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: publishing failed: not logged in` | Not logged in before `publish` | `brickkit login` |
| `Error: brickkit.yaml has no usable Market install source` | `login` can't find a market address | Add a `type: market` under `sources`, or `--market` |
| `Error: several Market install sources are configured, so it can't tell which to log in to` | Several markets | Name one with `--market` |

### AUTH_FAILED

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: login failed: wrong user name or password` | The market refused the credentials | Check the user name and password |
| `Error: the login credentials are malformed` | `.brickkit/credentials` is damaged | `brickkit logout`, then `login` |
| `Failed to fetch component <…>` | The Git remote was reached but refused: the credentials were refused, or the repository doesn't exist (hosting platforms often answer both the same way) | Read git's own words in the error, and tell which it is with the table in [Git authentication problems](../10-troubleshooting/04-git-auth-issues.md); retrying as it is changes nothing |

### TOKEN_EXPIRED

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: the token has expired` | The market token expired (the CLI doesn't refresh it) | `brickkit login` |

### IMAGE_UNAUTHORIZED

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: image pull not authorized` | The image registry refused the pull | `docker login` to that registry; or build it on this machine instead |
| `Error: image not found` | The registry has no such image | Check the image reference; or build it on this machine with `brickkit build` |

### IMAGE_MISSING

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: these images are built locally and have not been built yet` | An image to be built on this machine isn't there (`up` never builds) | `brickkit build` |

### IMAGE_STALE

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: the local image of shell <…> contains other member versions than its component.yaml declares` | A shell's `shell.members` changed without rebuilding the image | `brickkit build <shell> --force` |

### IMAGE_UNVERIFIED

Only a warning: a shell's image on this machine has no label recording member versions from `brickkit build` (built by
hand, or a third-party image), so which member versions were compiled in can't be checked.

### SIGNATURE_INVALID

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: the component is unsigned; installation is blocked` | `requireSignature: true`, and a market component isn't signed | Ask the publisher for a signed version; turn `requireSignature` off only once you're sure no signature is needed |

Warnings: `Warning: requireSignature is true, but the project declares no trusted public keys, so signature verification
isn't actually in effect`, `Warning: the signature comes from an undeclared publisher and was not verified`. See
[Security and signing](08-security-and-signing.md).

### CLONE_FAILED

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: clone failed` | `add --repo` failed to clone the source | Read git's own words in the error |
| `Clone failed: directory already exists` | A directory of the same name already exists under `components/` | Move it away and try again |
| `Clone failed: the source is already there, just archived` | The source is in `components/.archived/` | `brickkit sync` activates it |

### SUBMODULE_GUARD

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: can't move this component's source — it's a registered git submodule` | The directory `sync` wants to archive is a submodule registered in the project repository | Deregister the submodule in the project repository first |
| `Error: can't remove this component's source — it's a registered git submodule` | The directory `remove` wants to delete is a submodule | As above |

### SUBMODULES_SKIPPED

Only a warning, from `build`: `Warning: the source of <…> has git submodules, and they are empty directories here` —
BrickKit never fetches git submodules (see
[The bare-repository mechanism](06-bare-repo-mechanism.md#git-submodules-are-never-fetched)), so in the source it builds
from they are empty. Have the component publish an image (`deployment.image`), or make the build not need them; in a
cloned repository under `components/`, `git submodule update --init` if you must.

### RELEASE_BLOCKED

The checks `brickkit release` runs before tagging didn't pass. Nothing was written.

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: <…> has uncommitted changes` | The component directory has uncommitted changes | Commit or discard them |
| `Error: the branch <…> is released from has no upstream` | The branch was never pushed | `git push -u origin <branch>`, as the hint says |
| `Error: the branch has commits that are not pushed yet (releasing <…>)` | There are local commits not pushed yet | `git push` |
| `Error: <…> is already released (the tag <…> is on the current commit)` | This version was already released | Raise `metadata.version` |
| `Error: the tag <…> already exists on another commit` | A tag of the same name points at another commit | Raise the version; a released version never changes |
| `Error: HEAD is detached — <…> can only be released from a branch` | Not on any branch | Switch to a branch |
| `Error: the component directory is not in a Git repository` | The component directory has no Git | `git init` and set up a remote |

### RELEASE_PUSH_FAILED

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: pushing the tag <…> of <…> to <…> failed` | The tag was created, and pushing it failed (network, permission) | The local tag was already deleted, leaving nothing behind; fix the network or permission, then `release` again |

### LINT_FAILED

| Title | Situation | What to do |
| --- | --- | --- |
| `Error: the structure check did not pass` | `lint` found errors (including warnings under `--strict`) | Fix them at the locations listed above |

## Documentation checks

`brickkit lint` checks a component's documents and a project's `AGENTS.md` mechanically (see
[A component's documentation](../03-component-guide/08-component-doc-spec.md)). Every one of these is a warning: it
never stops `up` or `release`, and only `lint --strict` turns it into a failure (`LINT_FAILED`) — for a team that wants
its CI to hold the line. Each warning names the file, and the line where there is one. With `--log-level info`, every
finding also comes out as a JSON log line carrying its `error_code` and `file`, for scripts that tell them apart.

### DOC_FILE_MISSING

A required document is absent: in a component, `BRICKKIT.md`, `AGENTS.md`, `CLAUDE.md` or `README.md`; in a project,
`AGENTS.md` or `CLAUDE.md`. `brickkit new` writes all of them for a new component; for an existing one, write the
missing file (`brickkit skills update` creates `AGENTS.md` and `CLAUDE.md` where they are missing).

### DOC_SECTION_MISSING

A document lacks one of its fixed sections — for example "Before you deploy" in `BRICKKIT.md`, or "Code map" in a
component's `AGENTS.md`. Headings are recognised in English and Chinese, with or without a number in front. Add the
section under its heading.

### DOC_PATH_MISSING

A path in the Code map of a component's `AGENTS.md` does not exist (the code moved, the map didn't). Correct the path in
the map; a directory ends in `/`. A backticked token starting with `/` is read as an HTTP route, not a path, and is
never checked.

### DOC_LINK_BROKEN

A relative link in `README.md`, `AGENTS.md` or under `docs/` points at a file that does not exist. Fix the link or add the
file.

### DOC_LINK_NOT_PORTABLE

Either `BRICKKIT.md` has a relative link — it is read alone in other projects' caches, where the link is dead: name the
file as inline code, or use an absolute URL — or a component document links out of the component directory, to a file a
project using the component doesn't have.

### DOC_OUT_OF_STEP

`component.yaml` says something the doc doesn't mention where it belongs: a dependency missing from "Dependencies", a
required config key missing from "Configuration", an `artifacts` file missing from "Contracts", a shell member missing
from "Shell declaration". Mention it (a dependency by ID; its version stays in `component.yaml`).

### DOC_PLACEHOLDER

`TODO`, `TBD`, `FIXME`, or one of their three Chinese counterparts, is still in a document's text (code blocks and inline code don't count).
The skeleton `brickkit new` writes leaves `<!-- TODO: … -->` comments exactly so this lists what's left to fill in.

### DOC_TRANSLATION_DRIFT

A translation (`README.zh.md`, `BRICKKIT.zh.md`, `docs/design.zh.md`, or a page under `docs/zh/`) has no primary file,
a different number of `##` sections than its primary (the brickkit-maintained block at the end of `AGENTS.md` doesn't
count), or one language version doesn't link every other version near the top (except `BRICKKIT*.md`, which has no
relative links at all); with `docs/<lang>/` trees, a page of the primary tree is missing from another tree; or a file
looks like a translation but its suffix isn't a lowercase language code (`README.zh-CN.md`). Bring the translation back
in step with the primary — the primary is the one that is right.

### AGENTS_BLOCK_MISSING

`AGENTS.md` has no usable block maintained by brickkit (none, or its markers are broken), so its component table and
platform rules are not kept up to date. `brickkit skills update` appends one; `init`, `add`, `remove` and `upgrade`
never change your file.

### CLAUDE_IMPORT_MISSING

`CLAUDE.md` exists but has no `@AGENTS.md` line, so Claude Code doesn't read `AGENTS.md`. Add the line, or run
`brickkit skills update`.

### PROJECT_MAP_OBSOLETE

The project root still has the old project map `BRICKKIT.md` (the one with brickkit's markers). The component table now
lives at the end of `AGENTS.md`: move any notes of your own into `AGENTS.md`, then delete `BRICKKIT.md` — it is never
deleted for you.
