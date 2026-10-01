# Writing a shell

## Start from the skeleton

```bash
brickkit new shop/shell --shell
```

```text
✅ Component skeleton generated: shop/shell
   📄 shell/shop/shell/component.yaml
   📄 shell/shop/shell/BRICKKIT.md
   📄 shell/shop/shell/AGENTS.md
   📄 shell/shop/shell/CLAUDE.md
   📄 shell/shop/shell/README.md
```

A shell is written under the project's `shell/` (the local install source `local-shells`): a shell is usually a project's
own code, deciding "which components this project merges", and it's committed with the project. The skeleton's
`shell.members` carries a placeholder member to replace:

```yaml
shell:
  # TODO: the members this shell compiles in, each at its exact version (replace the placeholder)
  members:
    - example/member@0.1.0
```

Replace it with the members really compiled in:

```yaml
shell:
  members:
    - shop/cart@0.1.0
    - shop/stock@0.1.0
```

The shell itself is an ordinary component: it has its own `deployment`, `healthCheck`, image and config.

## The start-up code

When a shell starts it does four things: read `BRICKKIT_SERVED_MEMBERS_CONFIG`, find the module compiled in for each item,
initialise it with that item's config, and listen on that item's port for it. The whole of `shop/shell`:

```go
// shop/shell: a shell that compiles shop/cart and shop/stock into one process.
//
// A real shell would import the members' code; to keep it short, the two modules' handlers are written here in the shell.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
)

// member is one item of BRICKKIT_SERVED_MEMBERS_CONFIG.
type member struct {
	ComponentID string            `json:"componentId"`
	Version     string            `json:"version"`
	HTTPPort    int               `json:"httpPort"`
	Config      map[string]string `json:"config"`
}

// modules are the modules compiled into this shell: component ID → build its HTTP handler from the member's own config.
var modules = map[string]func(cfg map[string]string) http.Handler{
	"shop/stock": func(cfg map[string]string) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/stock", func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"sku": "A1", "count": 42, "warehouse": cfg["STOCK_WAREHOUSE"]})
		})
		return mux
	},
	"shop/cart": func(cfg map[string]string) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/cart", func(w http.ResponseWriter, _ *http.Request) {
			var s map[string]any
			if resp, err := http.Get(cfg["SHOP_STOCK_ENDPOINT"] + "/api/v1/stock"); err == nil {
				_ = json.NewDecoder(resp.Body).Decode(&s)
				resp.Body.Close()
			}
			json.NewEncoder(w).Encode(map[string]any{"items": []string{"A1"}, "stock": s})
		})
		return mux
	},
}

func main() {
	var members []member
	if err := json.Unmarshal([]byte(os.Getenv("BRICKKIT_SERVED_MEMBERS_CONFIG")), &members); err != nil {
		log.Fatalf("BRICKKIT_SERVED_MEMBERS_CONFIG: %v", err)
	}
	for _, m := range members {
		build, ok := modules[m.ComponentID]
		if !ok {
			log.Fatalf("this shell has no module %s", m.ComponentID) // not among the members compiled in: fail loudly
		}
		addr := fmt.Sprintf(":%d", m.HTTPPort) // listen on the member's own port for it
		go func(h http.Handler) { log.Fatal(http.ListenAndServe(addr, h)) }(build(m.Config))
		log.Printf("serving %s@%s on %s", m.ComponentID, m.Version, addr)
	}
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	log.Fatal(http.ListenAndServe(":8080", nil))
}
```

Built and started, the shell logs:

```text
shop-shell-0-1-0-1  | 2026/09/29 22:58:47 serving shop/cart@0.1.0 on :8081
shop-shell-0-1-0-1  | 2026/09/29 22:58:47 serving shop/stock@0.1.0 on :8082
```

A few things to do exactly this way:

- **Each module uses its own item's `config`**, never the shell process's environment variables — members' config isn't
  there.
- **Fail at once when the JSON names a member the shell didn't compile in.** It means the declaration and the image
  disagree; skip it quietly, and callers get an address pointing at a service that doesn't exist.
- **The health check checks only the shell's process itself.** A module that fails to initialise should make the whole
  shell fail to start, rather than report healthy with a dead module inside.

## How addresses are pointed at it

Callers don't know the other end is a shell. The platform connects the addresses for it in two places:

**The addresses others get point at the shell.** `shop/order`, outside the shell, depends on `shop/stock` and gets:

```text
      - SHOP_STOCK_ENDPOINT=http://shop-shell-0-1-0:8082
```

The same holds between members inside the shell: the `SHOP_STOCK_ENDPOINT` that `shop/cart` gets in the JSON is also
`http://shop-shell-0-1-0:8082`. The port is the member's own port — exactly where the shell listens for it.

**Members' service names resolve to the shell too.** On Docker the shell's container carries every member's service name
as a network alias:

```yaml
    networks:
      brickkit-net:
        aliases:
          - shop-cart-0-1-0
          - shop-stock-0-1-0
```

**How requests are routed inside the process is the shell's own business.** Which module a request gets once it reaches
one of the shell's ports, whether to split further by path or host name — the platform stays out of it. The platform only
guarantees a request reaches the right port of the shell; whether the shell uses one port per module or one port routed by
path is the shell author's decision.

**Ports must not clash.** The shell's own port and every member's port end up listening in the same container (or Pod), so
the platform checks them before generating anything:

```text
❌ Error: two components on shell shop/shell@0.2.0 both want port 8082
   Held by: Component shop/cart@0.1.0
   Held by: Component shop/stock@0.2.0
   Suggestion: These components all end up running in the same shell container/Pod, so ports must not collide
```

## Members' migrations don't run in the shell

When a member declares `migration.command`, the platform runs its migration separately, with **the member's own image** and
the member's own config, and starts the shell only once it succeeded:

```yaml
  shop-shell-0-1-0:
    depends_on:
      shop-stock-0-1-0-migration:
        condition: service_completed_successfully
```

So the shell needn't know how to migrate each member; and every member **must have its own image** (`deployment.image` or
`deployment.build`), even if it always runs in a shell. More in
[Migrations and shells](../05-migration/04-shell-interaction.md).

## Developing it locally

A shell is a component too and is developed the same way: create a workbench with `brickkit init` in the shell's
directory, or write the shell's entry in the project as `mode: debug` / `mode: local` so it runs as a process on this
machine (see [Developing inside a component](../03-component-guide/05-local-dev-fractal.md)).

A shell running as a process on this machine loads the members' **code in their local repositories** (the clones under
`components/`). So `up` checks a chain: the member versions the shell hosts this time equal the versions the shell's
`component.yaml` declares as compiled in; when a member has a local repository, the version in that repository's
`component.yaml` must be exactly that one too, and it must be the member's default version. When they don't match it fails,
suggesting `brickkit upgrade <member>@<repository version>` or checking out the matching tag in the repository — rather
than letting you debug code that doesn't match the declaration.
