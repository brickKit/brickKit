# Shell Standalone Fallback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a `servedBy` member's shell isn't running this cycle, the member deploys as an
ordinary standalone component instead of `up` hard-erroring.

**Architecture:** Three call sites currently decide "does this component get its own workload or
does it defer to a shell" purely from `entry.ServedBy != ""`, with no awareness of whether the
target shell is actually running: `internal/shell.Resolve` (errors outright),
`internal/compose`'s classification loop, and `internal/k8s`'s identical classification loop. All
three get a second condition — the shell must also be running — added at the same decision point,
so a member whose shell isn't running falls through to the same code path an ordinary
(non-`servedBy`) component already uses.

**Tech Stack:** Go, testify (`assert`/`require`), the existing `internal/resolver` →
`internal/cascade` → `internal/inject` → `internal/shell`/`internal/compose`/`internal/k8s`
pipeline.

**Spec:** [docs/superpowers/specs/2026-09-23-deploy-config-override-design.md](../specs/2026-09-23-deploy-config-override-design.md)
§6.1 ("Shell not running → each member deploys per its own independent Manifest declaration").
This plan implements exactly that one rule and its tests — nothing else from the spec.

## Global Constraints

- Generation-time errors in this codebase are `*clierr.Error` built via `clierr.New`/`clierr.Newf`
  with `i18n.T(msgid...)` message lookups — never a bare `fmt.Errorf` (see existing
  `shellNotFoundError`/`shellNotRunningError` in `internal/shell/shell.go:422-438` for the shape).
- Generators report the *first* problem and stop, matching every other check in `internal/shell`,
  `internal/compose`, and `internal/k8s` (`internal/shell/shell.go:213-216`'s doc comment states
  this convention explicitly) — do not change that convention while making this fix.
- Every new user-visible string (error/warning text) goes through `internal/i18n` +
  `internal/msgid`, with both an English and Chinese catalog entry — never a hardcoded string
  literal shown to a user.
- Run `go test ./internal/shell/... ./internal/compose/... ./internal/k8s/...` after every task;
  all pre-existing tests in these three packages must stay green throughout (this is a behavior
  change to existing code paths, not new isolated code — regressions are the main risk).

## Review Focus

- A member whose shell is running normally, and stays running — must be completely unaffected;
  the fallback path must only ever trigger when the shell genuinely isn't running.
- A member referencing a shell that doesn't exist at all (typo'd `servedBy`, or the shell was
  removed from the project) — this is a different, pre-existing error case
  (`shellNotFoundError`) and must keep erroring, not silently fall back to standalone (a made-up
  shell reference is a real mistake to catch, not something to paper over).
- Two members of the same disabled shell, both falling back standalone in the same run — both
  must get independent, correct services/Deployments, not just the first one processed.
- A member with its own port conflicting against an *unrelated*, already-running standalone
  component (not the disabled shell) once it falls back and starts generating its own workload —
  the existing port-conflict detection for ordinary components must still catch this; falling back
  must not accidentally bypass checks ordinary components go through.
- `checkPortConflicts` in `internal/shell/shell.go:307-335` currently validates a member's port
  only *within its shell's group* — once a member is falling back and no longer enters any group,
  confirm it does NOT skip whatever the *ordinary* per-component port-conflict path already does
  for regular components (i.e., the ordinary path's own checks, wherever they live, must still run
  for a fallback member — this plan does not need to build new port-conflict logic, only confirm
  the existing ordinary-component path already covers it, since the member now enters that path).

---

## Task 1: `internal/shell.Resolve` stops erroring when a member's shell isn't running

**Files:**
- Modify: `internal/shell/shell.go:246-252`
- Test: `internal/shell/shell_test.go:112-123` (rename/rewrite the existing
  `TestResolveErrorsWhenShellIsDisabled`)

**Interfaces:**
- Consumes: nothing new — `Resolve`'s signature
  (`internal/shell/shell.go:217-219`) is unchanged: `func Resolve(cfg *config.Config, graph
  *resolver.Graph, states *cascade.Result, env *inject.Result) ([]Group, error)`.
- Produces: `Resolve` now returns a member whose shell isn't running as **absent from every
  `Group`** (not an error) — Task 2 and Task 3 rely on this: they must independently classify that
  same member into their own "ordinary component" bucket, since `Resolve`'s output alone no longer
  tells them anything about it.

- [ ] **Step 1: Write the failing test**

Replace `TestResolveErrorsWhenShellIsDisabled` (`internal/shell/shell_test.go:112-123`) with:

```go
func TestResolveSkipsMemberWhenShellIsDisabled(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		{ID: "infra/shell-go-core", Version: "1.0.0", Mode: config.ModeDisable},
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
	}}
	groups, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        simple("mdm/customer", "1.0.7", 8080),
	})
	require.NoError(t, err)
	assert.Empty(t, groups, "the shell isn't running, so no group should form for it at all")
}
```

Leave `TestResolveErrorsWhenShellDoesNotExist` (`internal/shell/shell_test.go:101-110`)
untouched — a nonexistent shell target must still error (Review Focus, point 2).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/shell/... -run TestResolveSkipsMemberWhenShellIsDisabled -v`
Expected: FAIL — current code returns `shellNotRunningError`, so `err` is non-nil and
`require.NoError(t, err)` fails.

- [ ] **Step 3: Write minimal implementation**

In `internal/shell/shell.go`, change lines 246-252 from:

```go
		targetNode := graph.Node(target)
		switch {
		case targetNode == nil:
			return nil, shellNotFoundError(ref, target)
		case !states.IsRunning(target):
			return nil, shellNotRunningError(ref, target)
		}
```

to:

```go
		targetNode := graph.Node(target)
		if targetNode == nil {
			return nil, shellNotFoundError(ref, target)
		}
		if !states.IsRunning(target) {
			// 外壳这次没跑：这个成员不属于任何 Group，交给调用方（compose/k8s）
			// 按普通组件生成——不是这个函数的错误分支，是 Task 2/3 要接住的地方。
			continue
		}
```

`shellNotRunningError` (`internal/shell/shell.go:429-438`) becomes unused by this change — leave
the function in place for now rather than deleting it; Task 4 in the companion "override.yaml"
plan may still need it for a different case (an explicit `--ignore-served-by`-style rejection),
and deleting-then-possibly-re-adding it is unnecessary churn. Confirm it still compiles: a lint or
`go vet` pass will flag it as unused only if nothing else in the file references it — check with:

Run: `go build ./internal/shell/...`
Expected: builds clean. If `go vet`/`golangci-lint` flags `shellNotRunningError` as unused, add a
`//nolint:unused` comment above its declaration rather than deleting it, noting it's kept for the
nonexistent-shell-vs-not-running distinction that a future caller may still need.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/shell/... -v`
Expected: PASS — every test in the package, not just the new one (this file's other tests must be
unaffected).

- [ ] **Step 5: Commit**

```bash
git add internal/shell/shell.go internal/shell/shell_test.go
git commit -m "fix: shell.Resolve skips (not errors) a member whose shell isn't running"
```

---

## Task 2: `internal/compose` falls back to a normal service for the member

**Files:**
- Modify: `internal/compose/compose.go:230-267`
- Test: `internal/compose/servedby_test.go`

**Interfaces:**
- Consumes: `states.IsRunning(ref resolver.Ref) bool` (already used identically in
  `internal/shell/shell.go`, part of `*cascade.Result`) and `shell.ParseRef(raw string)
  (resolver.Ref, bool)` (`internal/shell/shell.go:203-209`, already imported in this file — see
  `compose.go:249`).
- Produces: a member falling back now appears in `p.components` (the same slice ordinary
  components use) with `p.rendered[service] = true` set, instead of `p.served`.

- [ ] **Step 1: Write the failing test**

Add to `internal/compose/servedby_test.go` (same file/package as
`TestServedByComponentGeneratesNoContainer`, which this test is the direct inverse of):

```go
func TestServedByMemberFallsBackToStandaloneWhenShellIsDisabled(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000),
		config.Component{ID: "infra/shell-go-core", Version: "1.0.0", Mode: config.ModeDisable})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))
	// A second member of the same disabled shell (Review Focus: both must fall back
	// independently, not just the first one processed).
	b.component(simple("mdm/orders", "2.0.0", 8081), servedByEntry("infra/shell-go-core", "1.0.0"))

	services := servicesOf(t, b.parsed())
	assert.Contains(t, services, "mdm-customer-1-0-7",
		"the shell isn't running, so this member should generate its own service")
	assert.Contains(t, services, "mdm-orders-2-0-0",
		"the second member of the same disabled shell should also generate its own service")
	assert.NotContains(t, services, "infra-shell-go-core-1-0-0",
		"the disabled shell itself should not be generated")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/compose/... -run TestServedByMemberFallsBackToStandaloneWhenShellIsDisabled -v`
Expected: FAIL — `mdm-customer-1-0-7` is currently absent from `services` (classified into
`p.served`, never rendered), so `assert.Contains` fails.

- [ ] **Step 3: Write minimal implementation**

In `internal/compose/compose.go`, change lines 248-258 from:

```go
		if entry.ServedBy != "" {
			shellRef, ok := shell.ParseRef(entry.ServedBy)
			if !ok {
				continue // config.Validate 已经挡过格式问题
			}
			p.served = append(p.served, servedComponent{
				Ref: ref, Service: service, Manifest: node.Manifest,
				Entry: entry, Shell: shellRef,
			})
			continue
		}
		p.components = append(p.components, componentPlan{
			Ref:      ref,
			Service:  service,
			Manifest: node.Manifest,
			Entry:    entry,
			Env:      envByRef[ref],
		})
		p.rendered[service] = true
```

to:

```go
		if entry.ServedBy != "" {
			shellRef, ok := shell.ParseRef(entry.ServedBy)
			if !ok {
				continue // config.Validate 已经挡过格式问题
			}
			if states.IsRunning(shellRef) {
				p.served = append(p.served, servedComponent{
					Ref: ref, Service: service, Manifest: node.Manifest,
					Entry: entry, Shell: shellRef,
				})
				continue
			}
			// 外壳这次没跑：退回普通组件生成路径，走下面这段——
			// 跟 internal/shell.Resolve 的回落决定必须是同一个判据
			// （states.IsRunning(shellRef)），两处一旦不一致就会出现
			// "这里生成了容器，internal/shell 那边又把它当成员处理"
			// 的双重归类。
		}
		p.components = append(p.components, componentPlan{
			Ref:      ref,
			Service:  service,
			Manifest: node.Manifest,
			Entry:    entry,
			Env:      envByRef[ref],
		})
		p.rendered[service] = true
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/compose/... -v`
Expected: PASS — the whole package, including `TestServedByComponentGeneratesNoContainer` (the
shell-*running* case must still generate no container for the member) and the new test.

- [ ] **Step 5: Commit**

```bash
git add internal/compose/compose.go internal/compose/servedby_test.go
git commit -m "fix: compose falls back to a standalone service when a servedBy member's shell isn't running"
```

---

## Task 3: `internal/k8s` falls back to a normal Deployment for the member

**Files:**
- Modify: `internal/k8s/k8s.go:348-375`
- Test: `internal/k8s/servedby_test.go`

**Interfaces:**
- Consumes: same `states.IsRunning`/`shell.ParseRef` as Task 2 — `k8s.go` already imports
  `internal/shell` (used at `k8s.go:358`).
- Produces: a member falling back now appears in `p.components` (the ordinary Deployment-plan
  slice) instead of `p.served`.

- [ ] **Step 1: Write the failing test**

Add to `internal/k8s/servedby_test.go` (mirrors `TestServedByComponentGeneratesOnlyAService`,
`internal/k8s/servedby_test.go:25-40`, reusing its exact `b.generate()`/`hasFile(result, path)`
pattern):

```go
func TestServedByMemberFallsBackToStandaloneDeploymentWhenShellIsDisabled(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000),
		config.Component{ID: "infra/shell-go-core", Version: "1.0.0", Mode: config.ModeDisable})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))

	result := b.generate()
	assert.True(t, hasFile(result, "deployments/mdm-customer-1-0-7.yaml"),
		"the shell isn't running, so the member should have its own Deployment")
	assert.False(t, hasFile(result, "deployments/infra-shell-go-core-1-0-0.yaml"),
		"the disabled shell itself should not generate a Deployment")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/k8s/... -run TestServedByMemberFallsBackToStandaloneDeploymentWhenShellIsDisabled -v`
Expected: FAIL — `mdm/customer` is currently classified into `p.served`, so no Deployment is
generated for it.

- [ ] **Step 3: Write minimal implementation**

In `internal/k8s/k8s.go`, change lines 357-367 from:

```go
		if entry.ServedBy != "" {
			shellRef, ok := shell.ParseRef(entry.ServedBy)
			if !ok {
				continue // config.Validate 已经挡过格式问题
			}
			p.served = append(p.served, servedPlan{
				Ref: ref, Service: manifest.ServiceName(ref.ID, ref.Version),
				Manifest: node.Manifest, Entry: entry, Shell: shellRef,
			})
			continue
		}
		p.components = append(p.components, componentPlan{
			Ref:      ref,
			Service:  manifest.ServiceName(ref.ID, ref.Version),
			Manifest: node.Manifest,
			Entry:    entry,
			Env:      envByRef[ref],
		})
```

to:

```go
		if entry.ServedBy != "" {
			shellRef, ok := shell.ParseRef(entry.ServedBy)
			if !ok {
				continue // config.Validate 已经挡过格式问题
			}
			if states.IsRunning(shellRef) {
				p.served = append(p.served, servedPlan{
					Ref: ref, Service: manifest.ServiceName(ref.ID, ref.Version),
					Manifest: node.Manifest, Entry: entry, Shell: shellRef,
				})
				continue
			}
			// 外壳这次没跑：退回普通组件生成路径，走下面这段——
			// 判据必须跟 internal/shell.Resolve、internal/compose 保持一致。
		}
		p.components = append(p.components, componentPlan{
			Ref:      ref,
			Service:  manifest.ServiceName(ref.ID, ref.Version),
			Manifest: node.Manifest,
			Entry:    entry,
			Env:      envByRef[ref],
		})
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/k8s/... -v`
Expected: PASS — the whole package.

- [ ] **Step 5: Commit**

```bash
git add internal/k8s/k8s.go internal/k8s/servedby_test.go
git commit -m "fix: k8s falls back to a standalone Deployment when a servedBy member's shell isn't running"
```

---

## Task 4: End-to-end CLI verification

**Files:**
- Test: `internal/cli/up_test.go` (new test function)

**Interfaces:**
- Consumes: `runIn(t, dir, args...)` (`internal/cli/init_test.go:20`). **Deliberately does not use
  the `comp{}`/`newProjectFixtureAt`/`oneLocalSource` fixture DSL**
  (`internal/cli/testsupport_component_test.go`) — checked during this plan's research and
  confirmed `comp` (line 27) has no `Mode` or `ServedBy` field, only
  ID/Version/Requires/Optional/Artifacts/Migration/Image/ConfigSchema/CPU/Memory/ResourceDeps/
  SecretConfig/Port. Extending that shared struct is out of scope for this plan (it's used by many
  other tests) — this test instead writes `brickkit.yaml` and the two components' `component.yaml`
  files directly to a temp directory, the same way BrickKit's own local-source layout expects
  (`<sources-path>/<scope>/<name>/component.yaml`), matching the manual fixture approach already
  proven to work earlier in this project's own investigation work.
- Produces: nothing consumed by later tasks — this is the plan's final, whole-pipeline check.

- [ ] **Step 1: Write the failing test**

```go
func TestUpDryRunFallsBackServedByMemberWhenShellDisabled(t *testing.T) {
	dir := t.TempDir()
	srcRoot := filepath.Join(dir, "local-src")
	shellDir := filepath.Join(srcRoot, "infra", "shell-go-core")
	memberDir := filepath.Join(srcRoot, "mdm", "customer")
	require.NoError(t, os.MkdirAll(shellDir, 0o755))
	require.NoError(t, os.MkdirAll(memberDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(shellDir, "component.yaml"), []byte(`
apiVersion: brickkit/v1
kind: Component
metadata:
  id: infra/shell-go-core
  name: Shell
  version: 1.0.0
  description: test shell
deployment:
  type: container
  image: registry.example.com/infra-shell-go-core:1.0.0
  port: 9000
healthCheck:
  type: http
  path: /healthz
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(memberDir, "component.yaml"), []byte(`
apiVersion: brickkit/v1
kind: Component
metadata:
  id: mdm/customer
  name: Customer
  version: 1.0.7
  description: test member
deployment:
  type: container
  image: registry.example.com/mdm-customer:1.0.7
  port: 8080
healthCheck:
  type: http
  path: /healthz
`), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "brickkit.yaml"), []byte(`
project: fallback-test
deploy:
  target: docker
sources:
  - id: local-dev
    type: local
    path: `+srcRoot+`
    enabled: true
components:
  - id: infra/shell-go-core
    version: 1.0.0
    mode: disable
  - id: mdm/customer
    version: 1.0.7
    servedBy: infra/shell-go-core@1.0.0
`), 0o644))

	res := runIn(t, dir, "up", "--dry-run")
	require.Equal(t, 0, res.ExitCode, res.Stderr)
	composeContent, err := os.ReadFile(filepath.Join(dir, ".brickkit", "generated", "docker-compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(composeContent), "mdm-customer-1-0-7",
		"the shell isn't running, so the member should have its own generated service")
	assert.NotContains(t, string(composeContent), "infra-shell-go-core-1-0-0",
		"the disabled shell itself should not appear")
}
```

Add `"os"` and `"path/filepath"` to this test file's imports if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run TestUpDryRunFallsBackServedByMemberWhenShellDisabled -v`
Expected: FAIL before Tasks 1-3 land, or if this task is done after them (as this plan orders it),
verify it already PASSES — Tasks 1-3 together should already make this true; this task exists to
prove the whole pipeline end-to-end, not to add new production code. If it passes immediately upon
writing, that's the expected outcome — record it and move directly to Step 4 without a code change.

- [ ] **Step 3: (only if Step 2 failed) Investigate why the CLI layer doesn't reflect Tasks 1-3**

If the generated compose file still doesn't show `mdm-customer-1-0-7`, something in
`internal/cli/up.go`'s `buildUpPlan` (`internal/cli/up.go:184`) is calling into compose/k8s
generation in a way Tasks 1-3 didn't cover, or caching/short-circuiting before reaching it. Read
`buildUpPlan` directly at that point to find the gap — do not guess; this step exists specifically
to catch an integration gap Tasks 1-3's unit-level tests couldn't see.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -v`
Expected: PASS — the whole package.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/up_test.go
git commit -m "test: end-to-end coverage for servedBy member standalone fallback via brickkit up --dry-run"
```

---

## Explicitly out of scope for this plan

- **Resource-binding gap for a standalone-fallback member** (design spec §6.2). Investigated
  during this plan's research: `internal/inject.resourceBindings` (`internal/inject/inject.go:534`)
  is unexported and its exact interaction with `servedBy`'s "satisfied via the shell's binding"
  rule lives inside `inject.Build` itself, which this plan's research did not trace in enough
  depth to write real, non-placeholder tasks for. This needs its own focused investigation into
  `internal/inject/inject.go`'s `Build` function before it can be planned — deliberately left out
  here rather than guessed at, per this plan's own no-placeholder requirement.
- Everything else in the companion "override.yaml" design (the file format, `brickkit override`
  command, `mode: debug` removal from `brickkit.yaml`, and the other six commands' integration
  changes) — a separate plan, sequenced after this one per the spec's own dependency note.
