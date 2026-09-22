# 4. Run a Component Locally, Hands-Off

`mode: debug` ([Article 3](03-local-debugging.md)) is "you start the process yourself — in an IDE, with breakpoints — and BrickKit only routes to it." `mode: local` is the other half of the same "bare process, no container" idea: BrickKit detects how to start the component, launches it itself, and supervises it — no IDE, no breakpoints, nothing to run by hand. This article proves that loop for real: [`demo/hello`](../../../tests/components/demo-hello/) — the same fixture Article 1 built into a container — runs instead as a plain OS process that BrickKit itself launched, visible from another terminal, and stopped the same way you'd stop any foreground command.

## The setup

A fresh project, one component — this time the component's *source* has to actually be present, not just its Manifest: `mode: local` reads `go.mod` and `main.go` off disk to work out how to start it, so a copy of `component.yaml` alone (enough for the Docker-based articles, since a prebuilt image is all `up` ever needed there) wouldn't be enough here.

```bash
mkdir hello-local && cd hello-local
brickkit init hello-local

mkdir -p components/demo
cp -r ../tests/components/demo-hello components/demo/hello
brickkit add --local
```

```
🔍 Found 1 component in local install source: local-dev
📦 Adding demo/hello@1.0.0
   ├── Manifest ✅
   └── artifacts ✅ (1 file)
✅ Written to brickkit.yaml (1 component)
```

One field turns it into a locally-managed component — the same shape `mode: debug` uses, a different value:

```yaml
components:
  - id: demo/hello
    version: 1.0.0
    mode: local
```

Nothing else about the Manifest changed. `deployment.image` and `deployment.port` are still there, still valid — and, exactly like `mode: debug`, never used: a `mode: local` component generates no container at all (AGENTS.md §5.4).

## Detecting the command

```bash
brickkit up --dry-run
```

```
📋 Component state calculation:
   ✅ demo/hello@1.0.0  starting (top-level)

📋 Start order (topological sort):
   1. demo-hello-1-0-0  no dependencies

Can start on their own: demo-hello-1-0-0 (no dependencies)
📄 Generated: .brickkit/generated/docker-compose.yaml

The following mode: local component(s) would start:
   demo/hello@1.0.0  go run .

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
demo-hello-1-0-0 | {"time":"...","level":"INFO","msg":"component started","component":"demo/hello","version":"1.0.0","addr":":8080"}
demo-hello-1-0-0  listening on port 8080
```

This run doesn't return — `up` stays in the foreground, the same way `docker compose up` (without `-d`) does, streaming the process's own output with its service name prefixed on every line. The first `demo-hello-1-0-0 |` line is `main.go`'s own structured log line, forwarded byte for byte, JSON and all — BrickKit doesn't parse or reformat a local component's output any more than it would a container's; the platform's own logging convention (AGENTS.md §6: JSON to stdout) is the component's choice to follow, not something enforced on it. The second, unprefixed line is BrickKit's own: it probed port 8080 until something started answering there, the same startup check a container's health check would perform.

## Checking in from another terminal

`demo/hello` isn't running in any engine BrickKit can query — there's no `docker ps` for a plain OS process. So from a second terminal, in the same project directory:

```bash
brickkit status
```

```
📊 Project status: hello-local (deploy.target: docker)

⬜ No components need to be started as containers this run


💡 This project has a local session running (PID 1071938) — go to that terminal, or Ctrl+C it there
```

```bash
brickkit graph
```

```
graph TD
    demo_hello_1_0_0["demo/hello@1.0.0<br/>managed locally"]
    classDef managed fill:#e6ffe6,stroke:#2e8b57;
    class demo_hello_1_0_0 managed
```

```bash
brickkit down
```

```
🛑 Stopping project hello-local
📋 This project has no containers running right now (the engine has none at all)
   Start it with brickkit up

💡 This project has a local session running (PID 1071938) — go to that terminal, or Ctrl+C it there
```

Three different commands, the same hint, for the same reason: a small lock file under `.brickkit/` — created the moment `up` starts supervising a `mode: local` component, released when it stops — is the only thing that makes a *second* terminal aware a local session exists at all. `status` doesn't list `demo/hello` as a container (there isn't one), but it doesn't report it as down either — the hint says exactly where to look. `graph` marks it with its own label and color, pinned and never greyed out, the same visual treatment a running `mode: debug` component gets. `down` is the most important of the three: it stops every container this project generated, but it does not, and cannot, reach across into another terminal's process tree — the hint tells you to go there yourself and press Ctrl+C, rather than silently doing nothing and leaving you to wonder why `demo/hello` is still listening.

## Stopping it

Back in the first terminal, `Ctrl+C`:

```
demo-hello-1-0-0 | {"time":"...","level":"INFO","msg":"component exited"}
```

`demo/hello`'s own last line comes from its signal handling (`main.go` catches `SIGINT`/`SIGTERM` and shuts its HTTP server down cleanly, logging `component exited` once it has) — BrickKit asked it to stop the same way `docker stop` would ask a container, and it did. No extra "shutting down" banner from BrickKit itself, no crash report — because nothing crashed, and `up` exited `0`. That silence is deliberate: a crash summary exists specifically to surface *unexpected* exits (the next section), and printing one here, for a component that did exactly what it was asked, would just be noise. A `mode: local` component's process tree is also scoped to that one terminal session on purpose — it never tries to outlive the `up` that started it (unlike a container, which keeps running after the CLI that started it exits), so there's nothing left to clean up afterward, and no equivalent of `docker ps -a` showing a stopped-but-still-there entry.

## If it crashes instead

None of the above happens if the process dies on its own — a panic, an unhandled signal, exiting non-zero before `down`/Ctrl+C ever gets involved. That path prints a crash summary with the exit reason and the process's own recent output, and `--crash-lines` controls how much of that output is kept (`0` for the exit reason alone, no output lines at all). Actually producing that crash and reading the summary belongs in [Troubleshooting](../08-troubleshooting.md), not here — this article's whole point was watching the hands-off path work exactly as promised.

---

Next: [Deploy to Kubernetes](05-kubernetes.md) — the same shape of project, deployed to Kubernetes instead of Docker.
