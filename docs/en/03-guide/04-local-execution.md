# 4. Run a Component Locally, Hands-Off

`mode: debug` ([Article 3](03-local-debugging.md)) is "you start the process yourself — in an IDE, with breakpoints — and BrickKit only routes to it." `mode: local` is the other half of the same "bare process, no container" idea: BrickKit detects how to start the component, launches it itself, and supervises it — no IDE, no breakpoints, nothing to run by hand. This article proves that loop for real: `demo/hello` runs as a plain OS process that BrickKit itself launched, visible from another terminal, and stopped the same way you'd stop any foreground command.

## The setup

A fresh project, one component, real Go source — no compiled binary, no Dockerfile, just a `go.mod` and a `main.go` that reads `PORT` and listens:

```go
package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	http.HandleFunc("/api/v1/hello", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"message":"Hello, I'm demo/hello"}`)
	})
	fmt.Printf("demo/hello listening on :%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
```

`brickkit new demo/hello` generates the skeleton `component.yaml`, `brickkit add --local` writes it into `brickkit.yaml`, and one field turns it into a locally-managed component — the same shape `mode: debug` uses, a different value:

```yaml
components:
  - id: demo/hello
    version: 0.1.0
    mode: local
```

Nothing else about the Manifest changed. `deployment.image` and `deployment.port` are still there, still valid — and, exactly like `mode: debug`, never used: a `mode: local` component generates no container at all (AGENTS.md §5.4).

## Detecting the command

```bash
brickkit up --dry-run
```

```
🚀 Starting project hello-local (deploy.target: docker)
📋 Component state calculation:
   ✅ demo/hello@0.1.0  starting (top-level)

📋 Start order (topological sort):
   1. demo-hello-0-1-0  no dependencies

Can start on their own: demo-hello-0-1-0 (no dependencies)
📄 Generated: .brickkit/generated/docker-compose.yaml

The following mode: local component(s) would start:
   demo/hello@0.1.0  go run .

💡 --dry-run only generates the files and starts no component
   View it: cat .brickkit/generated/docker-compose.yaml
```

`--dry-run` still means "show what would happen, start nothing" — for a `mode: local` component that includes the one thing there's no deployment file to inspect: the command it detected. Nothing in `component.yaml` said `go run .` anywhere. BrickKit found `go.mod` and a root-level `package main` in the component's source directory and worked the rest out on its own (AGENTS.md §5.6 covers the full detection table — Node, Java, Python, and a handful of others each have their own marker file). A compose file still gets generated — a project can mix `mode: local` components with ordinary containers, and this one just happens to have zero of the latter.

## Actually running it

```bash
brickkit up
```

```
Starting 1 local component(s) — press Ctrl+C to stop
demo-hello-0-1-0 | demo/hello listening on :8080
demo-hello-0-1-0  listening on port 8080
```

This run doesn't return — `up` stays in the foreground, the same way `docker compose up` (without `-d`) does, streaming the process's own output with its service name prefixed on every line. The first `demo-hello-0-1-0 |` line is `main.go`'s own `fmt.Printf`, forwarded exactly as written; the second, unprefixed line is BrickKit's own: it probed port 8080 until something started answering there, the same startup check a container's health check would perform.

## Checking in from another terminal

`demo/hello` isn't running in any engine BrickKit can query — there's no `docker ps` for a plain OS process. So from a second terminal, in the same project directory:

```bash
brickkit status
```

```
📊 Project status: hello-local (deploy.target: docker)

⬜ No components need to be started as containers this run


💡 This project has a local session running (PID 966478) — go to that terminal, or Ctrl+C it there
```

```bash
brickkit graph
```

```
graph TD
    demo_hello_0_1_0["demo/hello@0.1.0<br/>managed locally"]
    classDef managed fill:#e6ffe6,stroke:#2e8b57;
    class demo_hello_0_1_0 managed
```

```bash
brickkit down
```

```
🛑 Stopping project hello-local
📋 This project has no containers running right now (the engine has none at all)
   Start it with brickkit up

💡 This project has a local session running (PID 966478) — go to that terminal, or Ctrl+C it there
```

Three different commands, the same hint, for the same reason: a small lock file under `.brickkit/` — created the moment `up` starts supervising a `mode: local` component, released when it stops — is the only thing that makes a *second* terminal aware a local session exists at all. `status` doesn't list `demo/hello` as a container (there isn't one), but it doesn't report it as down either — the hint says exactly where to look. `graph` marks it with its own label and color, pinned and never greyed out, the same visual treatment a running `mode: debug` component gets. `down` is the most important of the three: it stops every container this project generated, but it does not, and cannot, reach across into another terminal's process tree — the hint tells you to go there yourself and press Ctrl+C, rather than silently doing nothing and leaving you to wonder why `demo/hello` is still listening.

## Stopping it

Back in the first terminal, `Ctrl+C`:

```
{"time":"...","level":"INFO","message":"Command finished","command":"brickkit up","elapsed_ms":29288,"exit_code":0}
```

No extra "shutting down" banner, no crash report — because nothing crashed. `demo/hello` was asked to stop, stopped, and `up` exited `0`. That silence is deliberate: a crash summary exists specifically to surface *unexpected* exits (the next section), and printing one here, for a component that did exactly what it was asked, would just be noise. A `mode: local` component's process tree is also scoped to that one terminal session on purpose — it never tries to outlive the `up` that started it (unlike a container, which keeps running after the CLI that started it exits), so there's nothing left to clean up afterward, and no equivalent of `docker ps -a` showing a stopped-but-still-there entry.

## If it crashes instead

None of the above happens if the process dies on its own — a panic, an unhandled signal, exiting non-zero before `down`/Ctrl+C ever gets involved. That path prints a crash summary with the exit reason and the process's own recent output, and `--crash-lines` controls how much of that output is kept (`0` for the exit reason alone, no output lines at all). Actually producing that crash and reading the summary belongs in [Troubleshooting](../08-troubleshooting.md), not here — this article's whole point was watching the hands-off path work exactly as promised.

---

Next: [Deploy to Kubernetes](05-kubernetes.md) — the same shape of project, deployed to Kubernetes instead of Docker.
