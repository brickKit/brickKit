# The environment variable contract

Everything a component gets from the platform is an environment variable. This page lists every variable the platform may
inject, how its name is formed, and when its value is worked out. The only platform convention a component's code needs to
know is on this page.

## Every variable the platform may inject

| Variable | For | Value |
| --- | --- | --- |
| `COMPONENT_ID` | Every component | The component ID, e.g. `demo/hello` |
| `COMPONENT_VERSION` | Every component | The exact version, e.g. `1.0.0` |
| `<dependency>_ENDPOINT` | One per dependency running this time | The address of the dependency's main port, e.g. `http://demo-hello-1-0-0:8080` |
| `<dependency>_<port name>_ENDPOINT` | Every extra port a dependency declares | E.g. `PEOPLE_BASIC_GRPC_ENDPOINT=http://people-basic-1-0-0:9090` |
| The component's own config | Every component | The keys are the keys in `configSchema`; the values are resolved from `config/` |
| `BRICKKIT_SERVED_MEMBERS` | Shells | The service names of the members hosted this time, comma-separated |
| `BRICKKIT_SERVED_MEMBERS_CONFIG` | Shells | Each member's config and ports, as a JSON array (see [Config as JSON](../04-shell/02-json-injection.md)) |
| `PORT` | `mode: local` components | The port the platform picked for this process on this machine |

Beyond these, the platform injects nothing into a container. A `mode: local` process gets the same variables on top of
the environment of the terminal that ran `up`, minus every name the platform owns, plus a few language helpers; see
[What a `mode: local` process inherits](#what-a-mode-local-process-inherits).

## Dependency addresses

The **name** is derived from the component ID: `/` and `-` become `_`, all uppercase, followed by `_ENDPOINT`. The name
**carries no version**. An extra port's variable puts the port's name in between, by the same rule: `-` becomes `_`, all
uppercase (port `admin-api` → `PEOPLE_BASIC_ADMIN_API_ENDPOINT`), so every name is one a shell can read as `$NAME`.

The **value** does: `http://<versioned service name>:<port>`. The service name is the component ID with `/` and `.` turned
into `-`, all lowercase, followed by the exact version with its dots turned into `-` too (`demo/hello@1.0.0` →
`demo-hello-1-0-0`). This string is exactly the same on Docker and on Kubernetes.

| Dependency | Variable | Value |
| --- | --- | --- |
| `demo/hello@1.0.0` | `DEMO_HELLO_ENDPOINT` | `http://demo-hello-1-0-0:8080` |
| `people/basic@2.1.0` (extra port `grpc: 9090`) | `PEOPLE_BASIC_GRPC_ENDPOINT` | `http://people-basic-2-1-0:9090` |

Name and component ID can be derived from each other: seeing `PEOPLE_BASIC_ENDPOINT`, you know it points at
`people/basic`. That's why an ID can appear only once in one component's `dependencies`.

In a few special cases the address keeps this format but points elsewhere:

| This time the dependency | The address callers get points at |
| --- | --- |
| Is hosted by a shell | The shell: `http://<shell service name>:<the member's own port>` (the shell listens on that port for the member) |
| Runs as a process on this machine (`mode: debug` / `mode: local`) | The port becomes its port on this machine; inside containers the service name resolves to the host |

**When an optional dependency isn't running, the variable doesn't exist at all** — it isn't an empty string. A component
must handle "there is no such variable" when reading it:

```python
bus = os.environ.get("DEMO_BUS_ENDPOINT")   # None when it isn't running
if bus is None:
    ...  # degrade
```

An empty string would cause a class of very hard-to-find bugs: `f"{ENDPOINT}/healthz"` becomes `/healthz`, the request
hits the component **itself**, gets a 200, and you believe the dependency is fine.

An address the component didn't declare as a dependency, filled in by the project with `$endpoint:<component ID>` in
config (this is how the members of a slot family are referred to), is worked out by the same rule: versioned in the same
way, rewritten for shells and processes on this machine in the same way, under the config key the component declared —
no extra `*_ENDPOINT` is injected. See [Another component's address](../01-three-layers/06-vars-and-var-ref.md).

## The component's own config

The keys of `configSchema` are the environment variable names, injected as they are. Where a value comes from follows one
chain (details in [Resolution order](../01-three-layers/08-resolution-priority.md)):

1. The value written in `config/<component>.yaml` (`config/<component>@<version>.yaml` for a compatibility version);
2. When it's written as `$var:NAME`, the current deploy file's `vars:` is checked first, then `config/vars.yaml`, and it's
   an error when neither has it;
3. Not written: the `default` from `configSchema`;
4. None of these: an optional item isn't injected; a required item makes `up` refuse to start.

A secret declared `mount: file` is the exception: what is injected is the **path of a file**
(`/run/brickkit/secrets/<versioned service name>/<key>`; a process on your machine gets the absolute path on the host),
and the value is in that file, byte for byte. See
[Delivered as a file](../01-three-layers/07-sensitive-values.md#delivered-as-a-file-mount-file).

Value types: a scalar is injected as the text you wrote (`1.10` is injected as `1.10`, never as `1.1`), whether it's a
value in `config/` or a `default` in `configSchema`; lists and maps are encoded as one line of JSON.

### No implicit overrides

A variable of the same name that happens to be in the process environment doesn't override what's written in `config/`,
and doesn't fill in an item that has no value either. Environment variables take part only where you **explicitly** wrote
`${VAR}`. This holds for `mode: local` processes too, which otherwise inherit the terminal's environment (see
[below](#what-a-mode-local-process-inherits)).

## Reserved names

Names the platform injects itself can't be used as a component's config items:

| Rule | Names |
| --- | --- |
| Exact | `COMPONENT_ID`, `COMPONENT_VERSION`, `PORT`, `BRICKKIT_SERVED_MEMBERS`, `BRICKKIT_SERVED_MEMBERS_CONFIG` |
| Suffix | `*_ENDPOINT` |

What happens on a collision depends on the step that finds it:

| When | Handling |
| --- | --- |
| `up` / `lint` | A warning; the item is ignored and the platform-injected value takes precedence |
| Publishing to a component market | Refused |

```text
⚠️ Config conflict: the config item of component demo/widget was ignored
   Component: demo/widget
   Config item: UPSTREAM_ENDPOINT
   Conflicting reserved pattern: *_ENDPOINT
   Handling: This config item is ignored; the platform-injected value takes precedence
   Origin: components/demo/widget/component.yaml
   Suggestions:
   1. Rename the config item in configSchema to avoid the platform's reserved variables
   2. For example, rename it to UPSTREAM_BASE_URL
```

`lint` checks every item declared in `configSchema`; `up` only meets an item when it would really be injected.

## When values are worked out

A config value can hold four kinds of reference: `$var:NAME` (a shared variable), `${VAR}` (the process environment or
`.env`), `file://path` (a file's contents), and `{ existingSecret: name, key: key }` (a Kubernetes Secret already in the
cluster). `$var:` is always replaced by the shared variable's value when the CLI loads the project. `existingSecret` is
never read by the CLI at all: on Kubernetes the Deployment references that Secret through `secretKeyRef`; everywhere else
the item isn't injected (on Docker you're warned; a shell's JSON refuses it, since the JSON needs the value; see
[Sensitive values](../01-three-layers/07-sensitive-values.md)). When `${VAR}` and `file://` are worked out depends on where
the value lands:

| Where the value lands | `${VAR}` | `file://` |
| --- | --- | --- |
| An ordinary value on Docker / Podman | Written into `compose.yaml` as it is and expanded when `docker compose` starts; the CLI checks it's defined when generating, and stops if not | The CLI reads the contents and writes them, as a secret, into an env file with mode 0600 |
| A secret on Docker / Podman (`secret: true`) | Written into the 0600 env file as it is and expanded when `docker compose` starts; checked to be defined first, likewise | As above |
| Kubernetes | Expanded by the CLI when generating the manifests (`kubectl` substitutes nothing); stops if it can't be found | The CLI reads the contents |
| A shell's `BRICKKIT_SERVED_MEMBERS_CONFIG` | Expanded by the CLI when generating, and JSON-encoded; stops if it can't be found | The CLI reads the contents and JSON-encodes them |
| A `mode: local` process's environment | Expanded by the CLI before starting the process; refuses to start if it can't be found | The CLI reads the contents |
| `mode: debug`'s `local-debug.*.env` | Expanded by the CLI when generating; if it can't be found the placeholder stays, and the output names it | The CLI reads the contents |

A shell's JSON must be worked out beforehand: `docker compose`'s substitution is plain text that knows nothing about JSON,
and substituting a value with a quote or a newline breaks the JSON (see
[Special characters](../04-shell/06-special-characters.md)). Kubernetes must be worked out beforehand: `kubectl` doesn't
substitute at all.

In no case does a secret appear in plain text in `compose.yaml` or a Deployment: on Docker it's in the 0600 env file, on
Kubernetes it's in the generated Secret, referenced by the Deployment through `secretKeyRef` (see
[Sensitive values](../01-three-layers/07-sensitive-values.md)).

## Migration containers and processes on this machine get the same

A component's migration container gets exactly the main service's environment (the same inline variables, the same env
file). A `mode: debug` component has no container; the platform writes the variables it would have got into
`.brickkit/generated/local-debug.<service name>.env` for you to load in your IDE; in that file, dependency addresses are
`http://localhost:<port>`, reachable from this machine.

## What a `mode: local` process inherits

A container gets the variables on this page and whatever its image sets, nothing from your terminal. A `mode: local`
process, which `brickkit up` starts on this machine from the component's source, starts from the environment of the
terminal that ran `up` instead: a program on your machine needs your `PATH`, `HOME`, toolchain and proxy settings to run
at all.

The names the platform owns are not inherited. They're removed from the terminal's environment before the process starts,
so the only value that can reach the process is the platform's own:

| Removed from the terminal's environment | So that |
| --- | --- |
| `COMPONENT_ID`, `COMPONENT_VERSION`, `PORT`, `BRICKKIT_SERVED_MEMBERS`, `BRICKKIT_SERVED_MEMBERS_CONFIG`, every name ending in `_ENDPOINT` | When the platform doesn't set one this time (an optional dependency that isn't running), the process really has no such variable, exactly as in a container; an `export DEMO_BUS_ENDPOINT=…` left in your shell can't make a stopped dependency look present |
| The keys of the component's own `configSchema` | A config item's value comes only from the chain [above](#the-components-own-config); a stale `export DB_HOST=…` in your shell never stands in for it |

On top of that, the platform sets a few variables for the language it recognised in the source directory, or the
`language` written in the `local:` block of `component.yaml` (a hand-written `runCommand` without `language` gets none of
them):

| Language | Set | Why |
| --- | --- | --- |
| `java` (Spring Boot) | `SERVER_PORT=<the PORT value>` | Spring Boot takes its listening port from `SERVER_PORT` |
| `java` | `JAVA_TOOL_OPTIONS=` and `JDK_JAVA_OPTIONS=` (emptied) | A debug agent with `suspend=y` left in your shell would make the process wait for a debugger nobody attaches |
| `node` | `NODE_OPTIONS=` (emptied) | The same, for an `--inspect-brk` left in your shell |
| `python` (Django) | `PYTHONUNBUFFERED=1` | The process's output goes to a pipe, not a terminal, and Python would otherwise hold log lines back in large blocks |

A `mode: debug` component is started by you, in your IDE, so what it inherits is up to the IDE; the platform's part is the
`local-debug.<service name>.env` file described above.
