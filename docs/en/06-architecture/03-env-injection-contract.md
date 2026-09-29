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

Beyond these, the platform injects nothing.

## Dependency addresses

The **name** is derived from the component ID: `/` and `-` become `_`, all uppercase, followed by `_ENDPOINT`. The name
**carries no version**.

The **value** does: `http://<versioned service name>:<port>`. The service name is the component ID with `/` and `.` turned
into `-`, all lowercase, followed by the exact version. This string is exactly the same on Docker and on Kubernetes.

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

## The component's own config

The keys of `configSchema` are the environment variable names, injected as they are. Where a value comes from follows one
chain (details in [Resolution order](../01-three-layers/08-resolution-priority.md)):

1. The value written in `config/<component>.yaml` (`config/<component>@<version>.yaml` for a compatibility version);
2. When it's written as `$var:NAME`, the current deploy file's `vars:` is checked first, then `config/vars.yaml`, and it's
   an error when neither has it;
3. Not written: the `default` from `configSchema`;
4. None of these: an optional item isn't injected; a required item makes `up` refuse to start.

Value types: scalars are turned into strings (`1.10` is injected as `"1.10"`, never as `1.1`); lists and maps are encoded
as one line of JSON.

### No implicit overrides

A variable of the same name that happens to be in the process environment doesn't override what's written in `config/`.
Environment variables take part only where you **explicitly** wrote `${VAR}`.

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

A config value can hold three kinds of reference: `$var:NAME` (a shared variable), `${VAR}` (the process environment or
`.env`), `file://path` (a file's contents). `$var:` is always replaced by the shared variable's value when the CLI loads the
project. When the other two are worked out depends on where the value lands:

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
