# Error Codes

Every error that ends a `brickkit` command carries a **stable error code**. This page is the dictionary: what each code means, the situations that produce it, and what to do next.

The CLI's messages follow its language — English by default, Chinese after `brickkit lang set zh` (see [`brickkit lang`](09-cli-reference.md#brickkit-lang)). This page quotes the English wording; the [Chinese edition](../../zh/06-architecture/10-error-codes.md) quotes the Chinese one. Either way you can search for exactly what your terminal printed. The error **codes** themselves never change with the language — that is what makes them safe for scripts to rely on.

## Reading a failure

A failing command prints a human-readable block and, directly after it, one JSON log line — both on stderr:

```
❌ Error: project config file not found
   Path: brickkit.yaml
   Suggestions:
   1. Run brickkit init <project-name> inside the project directory to initialize it
   2. Or point --config at the correct config file path
{"time":"2026-09-19T00:56:05+02:00","level":"ERROR","message":"Command failed","command":"brickkit up","elapsed_ms":0,"error_code":"PROJECT_MISSING","error":"PROJECT_MISSING: Error: project config file not found; Path=brickkit.yaml","exit_code":1}
```

The `error_code` in that log line is what this page is organized by. The first line of the block — the title — says which situation within that code you are in. The log line is on at the default log level (`info`); `--log-level off`, or `BRICKKIT_LOG_LEVEL=off`, silences it.

## What the codes promise

- **They are stable.** Codes are only ever added — never renamed, never reused for something else, never removed. A script can branch on them.
- **A code is a category, not a single situation.** `CONFIG_INVALID` sits behind seventy-odd different messages. The tables below list the ones you are likely to meet, each under the title the CLI prints.
- **Exit status.** `0` — success, including a run that printed warnings (the one exception is `brickkit lint --strict`, where a warning fails the run). `1` — the command failed. `2` — the command line itself was wrong: a missing or malformed argument, an unknown command or flag, or an argument that names something that isn't there (`brickkit remove` of a component that isn't in `brickkit.yaml`).
- **For scripts.** `NETWORK_UNREACHABLE` is the one worth retrying unchanged: the network, or the Market, may come back. `CONFIG_INVALID` fails identically however often you retry. Treat any other code as "something has to change first".
- **Warnings are separate.** A ⚠️ block never fails a command on its own — the one thing that turns warnings into a failure is `brickkit lint --strict`, which reports it as `LINT_FAILED`. The ones the CLI prints while it carries on don't produce the log line at all, so you recognize them by their title — see [Warnings](#warnings).

## Index: error codes by category

Click a code to jump to its section; the table in each section lists the specific situations by the title you'll see.

**Usage and internal errors**

| Code | In one line |
| --- | --- |
| [`INVALID_ARGUMENT`](#invalid_argument) | The command line is wrong: missing or malformed arguments, an unknown command or flag |
| [`NOT_IMPLEMENTED`](#not_implemented) | Reserved; no command produces it today |
| [`INTERNAL`](#internal) | The cause isn't your input (often a failure writing to the working directory), or the CLI hit an error it hasn't categorized |

**Configuration**

| Code | In one line |
| --- | --- |
| [`CONFIG_INVALID`](#config_invalid) | The widest code: `brickkit.yaml` is invalid, or the project's current state doesn't meet a precondition |
| [`CONFIG_CONFLICT`](#config_conflict) | What you're doing collides with something that already exists |
| [`PROJECT_EXISTS`](#project_exists) | You ran `brickkit init` in a directory that's already a project |
| [`PROJECT_MISSING`](#project_missing) | The command needs a BrickKit project and there isn't one here |

**Manifest and dependencies**

| Code | In one line |
| --- | --- |
| [`MANIFEST_INVALID`](#manifest_invalid) | A `component.yaml` is unusable (an unknown key is rejected on the spot) |
| [`DEPENDENCY_MISSING`](#dependency_missing) | A required dependency can't be found in any source |
| [`DEPENDENCY_CYCLE`](#dependency_cycle) | Required dependencies form a cycle |
| [`VERSION_AMBIGUOUS`](#version_ambiguous) | Several versions are listed and the command didn't say which |
| [`COMPONENT_DISABLED`](#component_disabled) | A component that something running requires has been turned off |
| [`COMPONENT_NOT_FOUND`](#component_not_found) | No enabled source has the component (or that version) |
| [`COMPONENT_BLOCKED`](#component_blocked) | The Market has marked that version `blocked` |

**Resources and ports**

| Code | In one line |
| --- | --- |
| [`RESOURCE_UNBOUND`](#resource_unbound) | A resource a component needs isn't bound in `brickkit.yaml` |
| [`PORT_CONFLICT`](#port_conflict) | Two components want the same host port (or two members of one shell want the same port) |

**Migration and engine**

| Code | In one line |
| --- | --- |
| [`MIGRATION_FAILED`](#migration_failed) | The migration Job failed on Kubernetes; the main service is deliberately not started |
| [`MIGRATION_SKIPPED`](#migration_skipped) | Only ever a warning: `mode: debug` and `servedBy` components don't get migrations |
| [`ENGINE_FAILED`](#engine_failed) | Docker Compose or `kubectl` ran, and failed |
| [`ENGINE_MISSING`](#engine_missing) | The container engine's executable can't be found |

**Network, authentication and images**

| Code | In one line |
| --- | --- |
| [`NETWORK_UNREACHABLE`](#network_unreachable) | The network or the Market can't be reached — the one code worth retrying as-is |
| [`AUTH_REQUIRED`](#auth_required) | This step needs you to be logged in |
| [`AUTH_FAILED`](#auth_failed) | Login failed: wrong credentials, or no token came back |
| [`TOKEN_EXPIRED`](#token_expired) | The stored token has expired |
| [`IMAGE_UNAUTHORIZED`](#image_unauthorized) | The registry refused the pull, or the image doesn't exist |

**Signing and the source workspace**

| Code | In one line |
| --- | --- |
| [`SIGNATURE_INVALID`](#signature_invalid) | Signature verification blocked the install: unsigned, or it doesn't verify |
| [`CLONE_FAILED`](#clone_failed) | Cloning a Git repository failed |
| [`SUBMODULE_GUARD`](#submodule_guard) | A component's source is a registered git submodule, which `remove` and `sync` won't touch |

**Structure check**

| Code | In one line |
| --- | --- |
| [`LINT_FAILED`](#lint_failed) | `brickkit lint` found problems — the details were printed above the summary |

Warnings carry no error code and can only be recognized by their title — see [Warnings](#warnings).

---

## Usage and internal errors

### INVALID_ARGUMENT

The command line is wrong. Every one of these is fixed by changing what you typed; `brickkit <command> --help` shows the usage.

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Please specify the component to add` | `brickkit add` with no component | Give one — `brickkit add people/basic@1.0.0` — or use `--local` to add everything in a local source |
| `Please specify the component to remove` | `brickkit remove` with no component | `brickkit remove people/basic` |
| `Please specify a project name: brickkit init <project-name>` | `brickkit init` with no project name | `brickkit init my-shop` |
| `Error: invalid component ID: <id>` | An ID that isn't of the form `<scope>/<name>` | Use an ID like `people/basic` |
| `Error: invalid version: <version>` | Not an exact `major.minor.patch` — ranges such as `^1.0.0` are rejected on purpose | Write the exact version |
| `Error: unknown command <command>` | A mistyped command name | `brickkit --help` lists the commands |
| `Error: invalid arguments` | An unknown or malformed flag | `brickkit <command> --help` |
| `Error: invalid log level` | `--log-level` (or `BRICKKIT_LOG_LEVEL`) isn't one of the accepted levels | Use one of `debug`, `info`, `warn`, `error`, `off` |

### NOT_IMPLEMENTED

Reserved. It dates from the skeleton stage, when a command could be a placeholder; no command produces it today. It stays in the list because codes are never removed. If you ever see it, that's a bug worth reporting.

### INTERNAL

Something failed that isn't about your input — usually writing a file in the working directory — or the CLI hit an error it doesn't classify. Any error that isn't otherwise classified is reported under this code, with its own text as the title.

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: failed to create the generated directory` | The directory for generated files couldn't be created | Check write permission on the project directory |
| `Error: failed to write the deployment files` | The generated Compose/Kubernetes file couldn't be written | Same — permission or disk space |
| `Error: failed to write the local-debug environment variable file` | `local-debug.env` couldn't be written | Same |
| `Error: failed to write the pre-commit hook` | The hook file under `.git/hooks/` couldn't be written | Check the permissions of `.git/hooks/` |
| `Error: failed to install the AI assistant skills` | The project was created, but the AI assistant skills couldn't be installed | Fix the permission problem and run `brickkit skills update`; the skills aren't needed for anything else |
| `Error: project label selector missing, deletion aborted` | A safety stop: `brickkit down` on Kubernetes refused to delete without a project label to scope it | Report it — it means the project name didn't reach the selector |

## Configuration

### CONFIG_INVALID

The widest code. Either `brickkit.yaml` (or something it points at) is invalid, or a precondition on the state of the project isn't met. Validation failures list every problem in the block at once, one line per field, so one round of edits fixes them all.

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: brickkit.yaml failed validation` | One or more fields are invalid; each is listed below the title with its reason, for example `components[1].exposePort: conflicts with components[0].exposePort` | Fix each listed field — the [brickkit.yaml Field Reference](08-brickkit-yaml-reference.md) has every constraint |
| `Error: the project config file is not valid YAML` | A YAML syntax error | Check the indentation at the reported line |
| `Error: the project config file is empty` | The file exists but is empty | Write a `brickkit.yaml`, or run `brickkit init` in a fresh directory |
| `Error: invalid project name` | The name isn't lowercase letters, digits, and hyphens starting and ending with a letter or digit — it becomes a Kubernetes namespace and a Docker network name | Pick a name of that shape |
| `Error: a required component config item has no value` | A key in a component's `configSchema.required` has no `default` and the project supplies no value — `up` is blocked because the variable would otherwise silently never exist | Set it under that component's `config` in `brickkit.yaml` |
| `Error: environment variables referenced in brickkit.yaml are not defined` | A `${VAR}` reference has no value (Kubernetes manifests can't defer substitution, so it's resolved at generation time) | Define it in `.env` at the project root, `export` it, or give a default: `${VAR:-dev}` |
| `Error: local: true can only be used with deploy.target: docker` | A Pod in a cluster can't reach a process on your laptop | Remove `local: true`, or set the target back to `docker` |
| `Error: with deploy.target: k8s, a component with expose: true must set hostname` | An Ingress without a host would match every domain | Add `hostname:` |
| `Error: domain <hostname> is claimed by more than one component` | Two components declare the same `hostname` | Give each its own |
| `Error: the cluster currently connected is not the one the configuration specifies` | The current `kubectl` context differs from `deploy.context` | `kubectl config use-context <name>`, or `brickkit up --context <name>` |
| `Error: the component servedBy points to does not exist` | The `servedBy` value names a component that isn't in the project | Check the shell's ID and version |
| `Error: the shell servedBy points to is not currently running` | The shell is turned off (`enabled: false`) | Turn it back on, or drop `servedBy` so the component deploys on its own |
| `Error: two members of shell <shell> produced different values for the same environment variable` | Two members of one shell depend on different versions of the same component | Align them on one exact version, or don't put them in the same shell |
| `Error: no install source is available` | `sources` is empty or every source is disabled | Add at least one source; for local development, a `type: local` source at `./components` |
| `Error: the local install source path does not exist` | A `local` source's `path` is wrong (it's relative to `brickkit.yaml`) | Correct the path, or set that source to `enabled: false` |
| `Error: invalid install source type: <type>` | `type` isn't `market`, `git`, or `local` | Use one of the three |
| `Error: trusted public key unusable` | An `installer.publicKeys` entry can't be used: wrong path, or not a PEM public key | Point it at the `.pub` file `cosign generate-key-pair` produced — never at `cosign.key`, the private key |
| `Error: cosign not found` | `brickkit publish --sign` needs cosign to sign (installing never does) | Install cosign, and make sure it's on `PATH` |
| `Error: this is not a git repository` | `brickkit init --hooks` outside a Git repository | `git init` first |
| `Error: this repository has no commits yet` | `brickkit restore` restores to the last commit, and there isn't one | Commit once first |
| `Error: <path> is not tracked by git` | `brickkit restore` was pointed at something Git doesn't track | `git add` and commit it once so there is a baseline |
| `Error: the source can't be recovered once it is deleted` | `brickkit remove` refuses to delete component source with changes that exist nowhere else | Commit and push it, or copy the directory away; `--force` if you're sure. To just stop using a component, `enabled: false` then `brickkit sync` archives it instead of deleting it |

### CONFIG_CONFLICT

Two things that can't both hold: what you asked for collides with what already exists.

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: <component>@<version> has already been published` | Publishing a version that's already on the Market — versions can't be reused | Bump `metadata.version` in `component.yaml` and publish again |
| `Error: <component>@<version> was created last time but not fully published, and this Manifest differs from that one` | An earlier publish of this version was created but never completed, and this Manifest differs from that one | Bump the version — or, to complete the earlier one, put `component.yaml` back as it was |
| `Error: a pre-commit hook already exists and wasn't written by brickkit` | `.git/hooks/pre-commit` exists and BrickKit never overwrites a hook it didn't write (it may be husky, lefthook, or yours) | Add the lines it prints to your own hook, or delete the file and re-run `brickkit init --hooks` |
| `Error: a conflict is being resolved; finish resolving it first` | `brickkit restore` during a merge in progress | Resolve the merge first |
| `Error: the source of some components exists in two places` | The same component's source exists both active and archived | Keep one copy; the block names both paths |
| `Commit blocked: the same component's source appears in two places in the commit` | The pre-commit hook: a commit would contain the same component's source in two places | Unstage one of them |
| `Commit blocked: component source is committed under the archive directory, but <path> says it should start` | The pre-commit hook: the source was archived but `enabled` in `brickkit.yaml` didn't come along with it | Commit the matching `brickkit.yaml` change too — or run `brickkit restore` |

The Market can also answer a publish with a version conflict; that surfaces under this code as well.

### PROJECT_EXISTS

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: the project is already initialized; there is no need to run init again` | `brickkit init` in a directory that's already a project | Nothing to do — or `init` in a different directory |

### PROJECT_MISSING

The command needs a BrickKit project and there isn't one here.

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: project config file not found` | No `brickkit.yaml` at the path (default: the current directory) | Run it in the project root, run `brickkit init <name>` first, or point at the file with `--config` |
| `Error: the project is not initialized` | The same, reached through a command that edits the config | Same |
| `Error: the current directory is neither a BrickKit project nor a component repository` | `brickkit skills` or `brickkit lint` found neither a `brickkit.yaml` nor a `component.yaml` | Run it in a project root, or in a component repository |
| `Error: there is no <file> here, so this is not a BrickKit project` | `brickkit init --hooks` in a directory without a `brickkit.yaml` | `brickkit init <name>` first — it installs the hook when the project root is the repository root |

## Manifest and dependencies

### MANIFEST_INVALID

A `component.yaml` can't be used. The Manifest has no extension mechanism: an unrecognized key is rejected on the spot, not ignored.

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: component.yaml failed validation` | A required field is missing or malformed, or a key isn't recognized; each problem is listed below the title | Fix each listed field — the [component.yaml Field Reference](07-component-yaml-reference.md) has every constraint |
| `Error: component.yaml is not valid YAML` | A YAML syntax error | Check the indentation at the reported line |
| `Error: component.yaml does not exist` | The directory doesn't contain one | Check the `--path`, or the directory the source points at |
| `Error: component.yaml is empty` | The file exists but is empty | Write the Manifest |
| `Error: the component ID in component.yaml doesn't match the directory name` | `brickkit lint` found a `component.yaml` whose `metadata.id` differs from the `<scope>/<name>` directory it sits in — a local source finds components by directory name | Make the two agree: rename the directory, or fix `metadata.id` |
| `Error: failed to read component.yaml` | `brickkit lint` couldn't read the file — usually its permissions | Fix the file's permissions |
| `Error: there is no component.yaml in the component directory` | `brickkit publish` was pointed at a directory without one | `--path` at the component's source directory — an archived one (`components/.archived/…`) works |
| `Error: a file declared under artifacts does not exist` | An `artifacts[].files` path doesn't exist — often a contract that hasn't been generated yet | Generate the file (protobuf, OpenAPI), or fix the path |
| `Error: component.yaml has no deployment section, so the digest can't be pinned` | Publishing pins the image digest, and there's no `deployment` section to pin | Add `deployment` |
| `Error: the component.yaml of <component>@<version> is unusable` | A fetched Manifest failed to parse or validate | The block says why; if it came from the Market, tell the publisher |
| `Error: the Manifest returned by the Market is empty` | The Market returned nothing for that version | Check the Market is healthy |
| `Error: the Manifest returned by the Market could not be parsed` | The Market returned something that isn't a Manifest | Check the Market's version is compatible |

### DEPENDENCY_MISSING

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: required dependency missing` | A required dependency isn't in any source; the block names the component and the missing dependency | Check `sources` in `brickkit.yaml`, confirm that exact version was published, then `brickkit add` it |
| `Cannot remove <component>` | `brickkit remove` on a component that others require | Remove the dependents first |

Weak (optional) dependencies missing is a warning, not this error — see [Warnings](#warnings).

### DEPENDENCY_CYCLE

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: dependency cycle detected` | Required dependencies form a loop; the block prints the whole loop | Make one edge on the loop optional (`optional: true`). A weak dependency doesn't constrain start order, so a loop with one weak edge is no longer a deadlock |

### VERSION_AMBIGUOUS

| You'll see | Cause | What to do |
| --- | --- | --- |
| `<component> has several versions (<versions>); please specify one:` | `brickkit remove people/basic` while more than one version is in `brickkit.yaml` | `brickkit remove people/basic@1.0.0` |

### COMPONENT_DISABLED

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: required dependency <component> is disabled` | Something that runs requires it, but it is `enabled: false` — or you pinned `enabled: true` on a component whose required dependency is off: two conflicting intents | Remove `enabled: false` from the dependency, or remove `enabled: true` from the dependent so it follows the top |

### COMPONENT_NOT_FOUND

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: component not found` | No enabled source has that ID; the block lists the sources tried | Check `sources`, the ID, and that the version is published |
| `Error: the install source has this component, but not the version that was asked for` | The source has the component, just not that version | Check the version |
| `Error: <component> is not in brickkit.yaml` | `brickkit remove` of a component the project doesn't list (exit status `2`) | Check the ID; `brickkit status` lists what's there |
| `Error: <component>@<version> is not in brickkit.yaml` | The same, for a specific version | Check the version — several can coexist |

The Market answering "no such component" is reported under this code too.

### COMPONENT_BLOCKED

The Market has delisted that version (`blocked`) — the last line of defense the trust model relies on. It can't be installed, and logging in doesn't change that. Use a different version, or ask the Market's administrator why. The message text comes from the Market, so there's no fixed title to search for.

## Resources and ports

### RESOURCE_UNBOUND

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: resource dependencies not satisfied` | A component declares a resource dependency (`dependencies.resources`: a `kind` and an `engine`) that `brickkit.yaml` doesn't satisfy — no matching `resources` entry, or no `bindings` entry for that component. Every unmet resource is listed at once. A real `up` is blocked; `up --dry-run` only warns, so you can still see what would be generated | Add the resource with a binding for that component — see [Resource binding mechanics](05-resource-binding.md) — or, to keep the component off for now, `enabled: false` |

### PORT_CONFLICT

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: host port <port> is claimed by more than one component` | Two `expose: true` components end up on the same host port because neither writes `exposePort`, so both default to their own `deployment.port`. (If both write the *same* `exposePort` explicitly, it is `CONFIG_INVALID` instead: an `Error: brickkit.yaml failed validation` naming both fields.) Either way it's caught at generation time, so you never see Docker's own "port is already allocated" | Give one an explicit, different `exposePort`, or drop `expose: true` — components reach each other over the container network without it |
| `Error: two components on shell <shell> both want port <port>` | Members of one `servedBy` shell all end up in the same container or Pod | Give them different ports |

## Migration and engine

### MIGRATION_FAILED

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: database migration failed` | The migration Job failed on Kubernetes; the main service is deliberately not started, and the Job doesn't retry (`backoffLimit: 0`) | `kubectl logs job/<migration-job>`; fix the script and re-run `brickkit up` — the CLI deletes the old Job first |

On Docker the migration is a one-shot Compose service, and its failure surfaces as `ENGINE_FAILED`, with Compose's own output above the block.

### MIGRATION_SKIPPED

Only ever a warning, never an error — see [Warnings](#warnings). It says the platform isn't running a migration for a `mode: debug` or `servedBy` component, so the migration is yours to run.

### ENGINE_FAILED

Docker Compose or `kubectl` ran and failed. The engine's own raw output is printed above the block and usually says why.

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: some components did not start properly` | `up` finished but some containers aren't healthy | `brickkit status`, then the container's logs. A component that takes longer than the default 60-second grace period to start needs a larger `healthCheck.startPeriodSeconds` |
| `Error: <command> failed to run` | The engine command itself exited non-zero | Read the raw output above the block |
| `Error: kubectl failed to run` | A `kubectl` call failed | Same |
| `Error: could not parse the container engine's status output` | The Docker Compose installed is older than V2 | Upgrade Compose — `brickkit version` prints the detected engine |
| `Error: could not parse kubectl's output` | `kubectl` printed something the CLI couldn't read | Check the `kubectl` version |
| `Error: could not get the image digest from the registry` | Publishing pins each image by digest and the registry didn't answer | Push the image first, and make sure this machine can reach the registry |
| `Error: the image digest could not be determined; publishing was aborted` | Same, for the final resolution | `docker push <image>`; `docker login` for a private registry. `--no-pin-digest` skips pinning, but then the signature covers only the tag |

### ENGINE_MISSING

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: container engine <engine> not found` | The engine binary isn't installed | Install Docker 20.10+ |
| `Error: no usable container engine found` | No engine found at all | Install Docker 20.10+ — or use `brickkit up --dry-run` to generate the files without one |
| `Error: Podman isn't supported yet — please use Docker` | Only Podman is installed. Podman support was built and then withdrawn: `down` fails on rootless Podman, and a project that can't be torn down is worse than one that never came up | Install Docker |
| `Error: kubectl not found` | `deploy.target: k8s` needs `kubectl` | Install it, or switch `deploy.target` to `docker` for local work |

## Network, authentication, and images

### NETWORK_UNREACHABLE

The one code worth retrying unchanged.

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: Market unreachable` | The Market's address didn't answer | Check the network and the Market URL in `sources` (or `--market`), then retry |
| `Error: could not reach the image registry` | The image registry didn't answer | Check the network and the registry address |
| `Error: failed to read the Market's response` | The connection dropped mid-response | Retry |
| `Error: the Market's response could not be parsed` | The URL answers, but it isn't a BrickKit Market | The URL should end in the Market's `/api/v1` |
| `Error: the Market's response has an unexpected format` | The Market speaks a different protocol version | Check the Market's version |
| `Error: the version list returned by the Market could not be parsed` | Same, for the list of versions | Same |
| `Error: the artifact list returned by the Market could not be parsed` | Same, for the list of artifacts | Same |
| `Error: none of the artifacts of <component> could be downloaded` | `brickkit fetch` couldn't download any artifact | Check the network, and that the component declares artifacts |

### AUTH_REQUIRED

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: publishing failed: not logged in` | `brickkit publish` without a login | `brickkit login`, or set `sources.authToken` |
| `Error: cannot determine which Market address to log in to` | `brickkit login` doesn't know which Market | `brickkit login --market https://market.example.com/api/v1` |
| `Error: brickkit.yaml has no usable Market install source` | No `type: market` source to log in to | Add one to `sources`, or pass `--market` |
| `Error: several Market install sources are configured, so it can't tell which to log in to` | More than one Market source | `--market` picks one |

### AUTH_FAILED

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: login failed: wrong user name or password` | Wrong credentials | Check them; the Market's administrator resets a forgotten password |
| `Error: the Market did not return an access token` | The login answered without a token | Check the Market's version is compatible |
| `Error: failed to read the login credentials` | `.brickkit/credentials` couldn't be read | `brickkit login` again |
| `Error: the login credentials are malformed` | That file is corrupt | Delete it, then `brickkit login` |
| `Error: failed to write the login credentials` | It couldn't be written | Check the directory's permissions |
| `Error: failed to delete the login credentials` | `brickkit logout` couldn't delete it | Check the permissions, or delete it by hand |

A private component refusing you also arrives under this code; the Market's own message says so, and the fix is to confirm the account owns the component.

### TOKEN_EXPIRED

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: the token has expired` | The stored token is past its lifetime | `brickkit login` again |

### IMAGE_UNAUTHORIZED

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: image pull not authorized` | The registry refused the pull | `docker login <registry>`, and confirm the account may pull that image |
| `Error: image not found` | The image isn't in the registry, or not built locally | Check the spelling and tag of `deployment.image`. A component under local test needs its image built first — the CLI never builds one for you |

## Signing

### SIGNATURE_INVALID

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: the component is unsigned; installation is blocked` | `installer.requireSignature` is on (the default) and the component carries no signature | Ask the publisher to republish with `brickkit publish --sign`; for local development, `requireSignature: false` |

A signature that doesn't verify against your `installer.publicKeys` also arrives under this code, with a message assembled at runtime. The usual cause is a public key that doesn't match the key the publisher signed with — see [Signing and the trust model](06-signing-and-trust.md) and [Troubleshooting](../08-troubleshooting.md).

## Source workspace

### CLONE_FAILED

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: failed to clone the Git repository` | The clone failed | Check the network and the repository address; a private repository needs Git credentials; or set that source to `enabled: false` |
| `Error: clone failed` | `--repo` couldn't clone the component's repository | Same |
| `Clone failed: directory already exists` | The destination directory already exists | If it's a mistake, delete or rename it; if the source is already there, you don't need to clone |
| `Clone failed: the source is already there, just archived` | The source exists but is archived under `components/.archived/` | `brickkit sync` brings it back according to what should run |
| `Clone failed: this component is closed-source` | Closed-source components have no repository to clone | Nothing to clone |
| `Clone failed: no usable Git repository address` | The source that provides this component has no Git URL | Put a Git source ahead of it in `sources`, or drop `--repo` |
| `Error: could not create the source directory` | The destination couldn't be created | Check the permissions |

### SUBMODULE_GUARD

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: can't remove this component's source — it's a registered git submodule` | `brickkit remove` won't touch a registered submodule: a plain delete or rename doesn't understand `.gitmodules` and would silently detach its history | Run the `git submodule deinit` / `git rm` commands the block lists, then re-run |
| `Error: can't move this component's source — it's a registered git submodule` | `brickkit sync` won't move one either | Do the equivalent `git mv` the block lists, check `.gitmodules` and `git status`, then re-run `brickkit sync` |

## Structure check

### LINT_FAILED

`brickkit lint` is the offline, read-only structure check: no network, no Docker or Kubernetes. It reads `brickkit.yaml` and the `component.yaml` files under your local sources (in a component repository, the one `component.yaml` in the current directory) and reports what is malformed. `LINT_FAILED` is its verdict on the whole run, not a description of any one problem: the problems themselves were already printed to **stdout**, one block each, followed by a summary line, `📋 Checked N files: M with errors, K warnings` — N files checked, M of them with errors, K warnings. The block below goes to stderr, after that report, and is followed by the usual JSON log line:

```
❌ Error: the structure check did not pass
   Checked: 4 files
   With errors: 2 files
   Suggestion: Fix them at the locations listed above, then run brickkit lint again
```

| You'll see | Cause | What to do |
| --- | --- | --- |
| `Error: the structure check did not pass` | At least one file has an error (the block says `With errors: N files`) — or, with `--strict`, at least one warning does (`Warnings: N (--strict: warnings count as failures)`). Exit status `1` | Go through the blocks `brickkit lint` printed on stdout — each names its file and field — fix them, and run `brickkit lint` again |

The problems on stdout keep the titles they'd have anywhere else: a `component.yaml` that doesn't validate is still `Error: component.yaml failed validation` (a `MANIFEST_INVALID` problem anywhere else), and an invalid `brickkit.yaml` still reads `Error: brickkit.yaml failed validation` (`CONFIG_INVALID` anywhere else). But they are plain blocks on stdout, with no JSON log line, so `LINT_FAILED` is the only code a script sees for what a file says — an invalid `brickkit.yaml` included, which ends the run as `LINT_FAILED` too, not as `CONFIG_INVALID`. That's deliberate: one run can find both kinds of problem, and the summary can carry only one code. Only two situations get a code of their own, and neither is about a file's content: `PROJECT_MISSING` when there is nothing to check (the directory has neither a `brickkit.yaml` nor a `component.yaml`; exit status `1`), and `INVALID_ARGUMENT` when the command line is wrong (exit status `2`). So in a CI script, `LINT_FAILED` means "lint ran and found problems"; any other code means it never got as far as checking.

It is not worth retrying: the same files fail the same way. Warnings alone don't fail `brickkit lint` (exit status `0`) unless you pass `--strict`, which is there for CI gates.

## Warnings

A ⚠️ block never fails a command by itself (exit status `0`; `brickkit lint --strict` is the opt-in exception). The ones printed while the CLI carries on don't produce the `error_code` log line, so this table is keyed by title; the code is what the same message carries in the CLI's source, for the curious.

| You'll see | Code | Meaning and what to do |
| --- | --- | --- |
| `Warning: optional dependency missing: <component>` | `DEPENDENCY_MISSING` | An optional dependency isn't available. Its `*_ENDPOINT` is not injected at all — not even as an empty string — so the component must read it defensively (`os.environ.get`, never `os.environ["X"]`) |
| `Warning: <component> is depended on as an optional dependency` | `DEPENDENCY_MISSING` | You're removing something other components depend on optionally. It's allowed; they'll run without it |
| `Warning: cannot confirm the dependencies of <component>` | `MANIFEST_INVALID` | `brickkit remove` couldn't read another component's Manifest, so it couldn't check whether that one needs the component being removed |
| `brickkit.yaml contains plaintext passwords` | `CONFIG_INVALID` | A password is written literally. Use `password: ${DB_PASSWORD}` and put the value in `.env`, which must be in `.gitignore` |
| `The config in brickkit.yaml may contain plaintext secrets` | `CONFIG_INVALID` | A `config` key declared `secret: true` in the component's `configSchema`, or merely named like a secret, has a literal value (an `existingSecret` reference is not a literal and never triggers this). For a name-only match the check goes on the name alone, never the value; if it isn't a secret, ignore it |
| `The existingSecret form has no effect: the config item doesn't declare secret: true` | `CONFIG_INVALID` | A config value written as `{ existingSecret, key }` on a property that isn't declared `secret: true` — the shape is silently not injected, this names it |
| `existingSecret only works on K8s, and the current target is docker` | `CONFIG_INVALID` | Docker has no concept of referencing an externally-created Secret; write the value directly (literal or `${VAR}`) |
| `A config item won't take effect: <key> on component <component>` | `CONFIG_INVALID` | A `config` key isn't in that component's `configSchema.properties` — usually a typo, and the CLI suggests the key it thinks you meant. Without this check the variable would simply never exist and the component would quietly use its default |
| `The whole config block won't take effect: component <component> declares no configSchema` | `CONFIG_INVALID` | You wrote `config` for a component that declares no `configSchema`, so none of it takes effect |
| `Warning: some keys declared on configSchema items won't take effect` | `MANIFEST_INVALID` | A property under `configSchema` has a misspelled key (`defualt:`). Shown when publishing, when adding from a local source, and by `brickkit lint` |
| `Config conflict: the config item of component <component> was ignored` | `CONFIG_CONFLICT` | A `configSchema` key, uppercased, collides with a reserved variable (`*_ENDPOINT`, `DATABASE_*`, …); the platform's value wins and the key is skipped. Rename the key — see the [Environment variable contract](04-environment-variables.md). `brickkit lint` reports it offline, before you run `up`, and for every key `configSchema` declares — `up` only meets it for a key that has a default or a `config` value |
| `A resource's host looks like a service name, which may not resolve inside the container` | `CONFIG_INVALID` | A resource `host` looks like a Compose service name, but resources aren't part of the project. Use `host.docker.internal` for one on your machine, or its real address |
| `The configuration has fields that only take effect on <target>` | `CONFIG_INVALID` | A field that only applies to the other `deploy.target` — for example `exposePort` under `k8s` — is doing nothing |
| `On a mode: debug component, labels has no effect this run` | `CONFIG_INVALID` | A `mode: debug` component has no container to label. Remove `mode: debug` to get platform-managed labels back |
| `Note: a mode: debug component's database migration won't run automatically` | `MIGRATION_SKIPPED` | A `mode: debug` component runs on your machine, so the CLI runs no migration for it. Run the migration command yourself once, with the variables from its `local-debug.<service>.env` |
| `Note: a servedBy component's database migration won't run automatically` | `MIGRATION_SKIPPED` | A `servedBy` member has no container of its own, so it has no migration container. The shell has to cover it |
| `Note: a servedBy component's own health check does not take effect independently` | `CONFIG_INVALID` | The shell's health check is the one that counts |
| `Note: on a servedBy component, <field> has no effect this run` | `CONFIG_INVALID` | `expose`, `exposePort`, `hostname`, `replicas`, `resources`, `serviceAccountName`, and `labels` describe how a component's own container is deployed, and a `servedBy` member has none. To deploy the component on its own, drop `servedBy` |
| `Warning: requireSignature is true, but the project declares no trusted public keys, so signature verification isn't actually in effect` | `SIGNATURE_INVALID` | With zero `installer.publicKeys`, verification is off entirely — `requireSignature: true` alone doesn't verify anything. Declare the publisher's public key, or set `requireSignature: false` explicitly to silence this |
| `Warning: the signature comes from an undeclared publisher and was not verified` | `SIGNATURE_INVALID` | The signature names a publisher whose key you haven't declared, so it wasn't checked |
| `Warning: artifact download failed and was skipped` | `NETWORK_UNREACHABLE` | An artifact couldn't be downloaded; the install carried on without it |
| `Skipping the component layout check: <reason>` | `CONFIG_INVALID` | The pre-commit hook couldn't run its structure check this time, and the commit goes ahead. `brickkit restore --check` runs it by hand |

## Read further

- [Troubleshooting](../08-troubleshooting.md) — the failures people actually hit, organized by what you see rather than by code.
- [Environment variable contract](04-environment-variables.md) — the reserved-variable warnings, with real examples.
- [CLI Command Reference](09-cli-reference.md) — every command and flag.
