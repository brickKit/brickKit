# 11. Build Your First Component From Scratch

Every earlier article installed something that already existed. This one writes a genuinely new component — a minimal HTTP counter, deliberately small enough to type out in full — and gets it running the exact same way as everything else in this series: no special onboarding path, just a `component.yaml` and an image. Typing the Manifest out by hand here is deliberate, to show there's no hidden magic in it; for real use, `brickkit new my-scope/counter` generates the same shape of file, already valid, with the fields you'd otherwise be typing left as TODOs.

## The whole thing, four files

```go
// main.go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
)

var (
	mu    sync.Mutex
	count int
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	startAt, _ := strconv.Atoi(envOr("START_AT", "0"))
	count = startAt

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/api/v1/count", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		count++
		current := count
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{
			"component": envOr("COMPONENT_ID", "tutorial/counter"),
			"version":   envOr("COMPONENT_VERSION", "1.0.0"),
			"count":     current,
		})
	})
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

```
# go.mod
module tutorial/counter

go 1.22
```

```dockerfile
# Dockerfile — the same multi-stage shape as every fixture used earlier in this series
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/counter .

FROM alpine:3.20
RUN adduser -D -u 10001 brickkit
COPY --from=build /out/counter /app/counter
USER 10001
EXPOSE 8080
ENTRYPOINT ["/app/counter"]
```

```yaml
# component.yaml
apiVersion: brickkit/v1
kind: Component

metadata:
  id: tutorial/counter
  name: 教程计数器
  version: 1.0.0
  description: 教程用的最小组件，每次调用 /api/v1/count 就加一

configSchema:
  type: object
  properties:
    startAt:
      type: integer
      default: 0
      description: 计数器起始值，注入为环境变量 START_AT

deployment:
  type: container
  image: tutorial/counter:1.0.0
  port: 8080

healthCheck:
  type: http
  path: /healthz
```

No `deploy.target`-specific file anywhere in this component's own directory — that generation step is entirely the CLI's job, for whichever target the *project* using this component chooses (AGENTS.md §5.5).

## Build it, add it, run it — exactly like everything else in this series

```bash
docker build -t tutorial/counter:1.0.0 .

mkdir -p components/tutorial/counter
cp component.yaml components/tutorial/counter/
brickkit add --local
```
```
🔍 Found 1 component in local install source: local-dev
📦 Adding tutorial/counter@1.0.0
   └── Manifest ✅
✅ Written to brickkit.yaml (1 component)
```

```yaml
components:
  - id: tutorial/counter
    version: 1.0.0
    expose: true
    exposePort: 8085
```

```bash
brickkit up
curl http://localhost:8085/api/v1/count
curl http://localhost:8085/api/v1/count
```
```json
{"component":"tutorial/counter","count":1,"version":"1.0.0"}
{"component":"tutorial/counter","count":2,"version":"1.0.0"}
```

A brand new component, never seen by this platform before, went from four files to a running, incrementing, curl-able service using the exact same `add`/`up` this whole series has used on `demo/hello` from Article 1 onward. Nothing about the platform's own mechanics cared that this one didn't exist an hour ago.

## Config override works exactly the same way too

```yaml
components:
  - id: tutorial/counter
    version: 1.0.0
    expose: true
    exposePort: 8085
    config:
      startAt: 100
```
```bash
brickkit up
curl http://localhost:8085/api/v1/count
```
```json
{"component":"tutorial/counter","count":101,"version":"1.0.0"}
```

`startAt: 100` became `START_AT=100` in the container's real environment (AGENTS.md §5.2's camelCase-to-`UPPER_SNAKE_CASE` rule), and the handler picked it up on first read — the same configSchema override mechanism used against `demo/hello`'s `greeting` back in Article 1.

## From here, everything else in this series already applies

This component didn't need anything special to plug into the rest of what this series covers, because none of it was ever specific to `demo/hello` or `demo/caller` in the first place:

- [Debug a component locally](03-local-debugging.md) works the same way — `local: true`, run the binary on your own machine, breakpoints and all.
- [Publish and install from a marketplace](09-marketplace.md) and [Sign and verify components](10-signing.md) work the same way — `brickkit publish --path ./components/tutorial/counter`, sign it, install it somewhere else.

Nothing in this article introduced a new mechanism — the point was confirming that writing a component from nothing really does land you in the exact same platform every other article in this series already walked through.

---

Next: [Network policy and least privilege](12-network-policy.md) — locking down which components can actually talk to which, on Kubernetes.
