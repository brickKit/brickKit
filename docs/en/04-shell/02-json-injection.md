# Config as JSON

Deployed on their own, members each get their own environment variables in their own container. Compiled into a shell
they share one process and one environment — so what happens when both members have a `LOG_LEVEL`? The platform's
answer: **members' config isn't spread into the shell's environment; it's packed into one JSON value**, one item per
member, and the items don't touch each other.

## Two reserved variables

The shell's container gets two extra variables:

| Variable | Holds |
| --- | --- |
| `BRICKKIT_SERVED_MEMBERS` | The service names of the members hosted this time, comma-separated: `shop-cart-0-1-0,shop-stock-0-1-0` |
| `BRICKKIT_SERVED_MEMBERS_CONFIG` | A JSON array with one item per member: its config and ports |

The `BRICKKIT_SERVED_MEMBERS_CONFIG` that `shop/shell` gets (formatted):

```json
[
  {
    "componentId": "shop/cart",
    "version": "0.1.0",
    "httpPort": 8081,
    "extraPorts": [],
    "config": {
      "SHOP_STOCK_ENDPOINT": "http://shop-shell-0-1-0:8082"
    }
  },
  {
    "componentId": "shop/stock",
    "version": "0.1.0",
    "httpPort": 8082,
    "extraPorts": [],
    "config": {
      "STOCK_WAREHOUSE": "Main warehouse"
    }
  }
]
```

## The fields of each item

| Field | Meaning |
| --- | --- |
| `componentId`, `version` | Which member, which version |
| `httpPort` | The main port in the member's own `component.yaml`: the shell listens on it for the member |
| `extraPorts` | The member's extra ports: `[{"name": "grpc", "port": 9090}]` |
| `config` | The environment variables the member would get deployed on its own: its config items, plus the addresses it depends on (`*_ENDPOINT`). `COMPONENT_ID` and `COMPONENT_VERSION` aren't here — they are `componentId` and `version` |

The values in `config` are **already evaluated**: `$var:` has been replaced by the shared variable's value, `${VAR}`
expanded, `file://` read into the file's contents. The shell uses them as they are, with no further substitution (why they
are evaluated beforehand: [Special characters](06-special-characters.md)).

## How the shell uses it

1. Read `BRICKKIT_SERVED_MEMBERS_CONFIG` and parse it into an array.
2. For each item, find the module compiled in for it and initialise it with that item's `config` — not with the shell's
   own environment.
3. Listen on `httpPort` (and each of `extraPorts`) for it.

```go
type member struct {
	ComponentID string            `json:"componentId"`
	Version     string            `json:"version"`
	HTTPPort    int               `json:"httpPort"`
	Config      map[string]string `json:"config"`
}

var members []member
if err := json.Unmarshal([]byte(os.Getenv("BRICKKIT_SERVED_MEMBERS_CONFIG")), &members); err != nil {
	log.Fatalf("BRICKKIT_SERVED_MEMBERS_CONFIG: %v", err)
}
```

A module that used to read its config with `os.Getenv("STOCK_WAREHOUSE")` reads it from the table handed to it once it's
compiled into a shell. The least work is for the module to take a config table from the start: deployed on its own,
`main` fills the table from environment variables; inside a shell, the shell fills it from the JSON.

## Zero members

A shell may host no members at all this time (they were all moved out). `BRICKKIT_SERVED_MEMBERS` is then the **empty
string**, and `BRICKKIT_SERVED_MEMBERS_CONFIG` is `[]` — "the variable exists, and is empty". The shell should then
initialise no modules. Never treat it as "the variable isn't there" and fall back to "start every module compiled in": the
two cases mean opposite things.

## Why nothing collides

Each member's config sits in its own item: `shop/cart`'s `LOG_LEVEL` and `shop/stock`'s `LOG_LEVEL` are two keys in two
different objects, and neither can overwrite the other. The shell's own config stays ordinary environment variables on the
shell's container (see [The shell's own config](08-shell-config.md)), and doesn't collide with the JSON either.

## Where it's kept

The JSON holds members' whole config, possibly including secrets, so it's treated as a secret: on Docker it's written to
`.brickkit/generated/env/<shell service name>.env` (file mode 0600, referenced through `env_file`); on Kubernetes it goes
into the generated Secret. It never appears in plain text in `compose.yaml` or a Deployment.
