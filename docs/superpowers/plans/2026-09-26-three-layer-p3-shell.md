# P3: Shell Mechanism Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the servedBy-era shell code (adapted to `members` in P2) into the shell mechanism of `new_plan/提案.md` §8 and Appendix A: CLI-evaluated JSON injection, `_ENDPOINT` rewrite to the shell, member migrations with the member's own image, declaration consistency checks, and member `mode: debug|local` (A18).

**Architecture:** One function decides "is this component hosted by a shell this run" (`cascade.Result.HostOf`) and every consumer (inject, shell, compose, k8s, graph) asks it. `inject` rewrites a hosted member's `*_ENDPOINT` to the shell's address. `shell.Resolve` validates the declarations, evaluates each member's environment into `BRICKKIT_SERVED_MEMBERS_CONFIG` and marks it secret, so the existing placement rules put it in a 0600 env file (Docker) or a Secret (K8s). Members keep a migration container / Job built from their own image.

**Tech Stack:** Go 1.22, gopkg.in/yaml.v3, testify. Existing packages `internal/{project,cascade,inject,shell,compose,k8s,cli,manifest,configdir,envref}`.

**Spec:** `new_plan/提案.md` §8 (8.1–8.9) and Appendix A (A3, A7, A10, A11, A18 are binding). Roadmap row P3 in `docs/superpowers/plans/2026-09-26-three-layer-refactor-roadmap.md`.

## Global Constraints

- Appendix A overrides the spec body.
- Every user-visible string goes through `internal/msgid` constants with both `catalog_en.go` and `catalog_zh.go` entries; `go test ./tests/i18nguard/...` must stay green; unused msgids are deleted.
- Code comments in Chinese, matching the surrounding style.
- A shell member is never written to `compose.yaml` / a Deployment in plaintext: the JSON is a secret value (A7).
- Commits go directly on main, message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Each task: `go build ./...`, the task's tests, `go test ./tests/i18nguard/...`, and `.tools/bin/golangci-lint run` on touched packages green before commit.
- Out of scope (later phases): `add`/`remove` maintaining `kind: shell` and `members` (P4), `upgrade` of shells (P6), `lint`'s offline shell checks (P7), docs (P9).

## Review Focus

1. A member value containing a multi-line PEM, `$`, quotes and backslashes arrives byte-exact inside the JSON on both Docker and K8s, and never appears in `compose.yaml` or a Deployment. (Task 3, Task 7 e2e)
2. A member config value `${VAR}` whose variable is unset fails `up --dry-run` naming component, key and variable — it must not end up as a literal `${VAR}` inside the JSON. (Task 3 `TestResolveMemberTemplateUnresolved`)
3. A component depending on a hosted member gets the shell's address; after the member switches to `mode: debug`, it gets the member's own (host-routed) address again and the member leaves `BRICKKIT_SERVED_MEMBERS`. (Task 1, Task 6)
4. A member with `migration` still migrates before the shell starts, with the member's image, on Docker and K8s. (Task 4, Task 5)
5. `kind: shell` in brickkit.yaml without `shell:` in component.yaml (and the reverse), and a member the shell never declared it can host, fail at `up --dry-run` with a message naming both sides. (Task 2)

---

### Task 1: One hosting decision; `_ENDPOINT` rewrite to the shell

**Files:**
- Create: `internal/cascade/host.go`, `internal/cascade/host_test.go`
- Modify: `internal/inject/inject.go` (`addEndpoints` gets the host), `internal/inject/inject_test.go`
- Modify: `internal/shell/shell.go` (delete `ShellRef`, use cascade), `internal/compose/compose.go`, `internal/compose/servedby.go`, `internal/k8s/k8s.go`, `internal/k8s/servedby.go`, `internal/cli/graph.go`

**Interfaces:**
- Produces:
  - `func ShellOf(p *project.Project, ref resolver.Ref) (resolver.Ref, bool)` — declared membership (deploy `members`), shell has exactly one version in brickkit.yaml; ignores run state and mode.
  - `func (r *Result) HostOf(p *project.Project, ref resolver.Ref) (resolver.Ref, bool)` — hosted *this run*: `ShellOf` true, the member is not a bare process (`mode: debug|local`), and `r.IsRunning(shell)`.
  - inject: a hosted dependency's `X_ENDPOINT` = `http://<shell service>:<member port>`, extra ports likewise with the member's extra port numbers.

- [ ] **Step 1: failing tests** — `internal/cascade/host_test.go`:

```go
func TestHostOf(t *testing.T) {
	p := projecttest.Build(t, projecttest.Spec{Entries: []projecttest.Entry{
		{ID: "erp/shell", Version: "1.0.0"},
		{ID: "erp/a", Version: "1.0.0", ServedBy: "erp/shell@1.0.0"},
		{ID: "erp/b", Version: "1.0.0", ServedBy: "erp/shell@1.0.0", Mode: deployfile.ModeLocal},
		{ID: "erp/solo", Version: "1.0.0"},
	}})
	running := cascade.NewResult(/* all four running */)
	shell := resolver.Ref{ID: "erp/shell", Version: "1.0.0"}
	got, ok := running.HostOf(p, resolver.Ref{ID: "erp/a", Version: "1.0.0"})
	assert.True(t, ok); assert.Equal(t, shell, got)
	_, ok = running.HostOf(p, resolver.Ref{ID: "erp/b", Version: "1.0.0"})
	assert.False(t, ok, "裸进程成员这次不进外壳（A18）")
	_, ok = running.HostOf(p, resolver.Ref{ID: "erp/solo", Version: "1.0.0"})
	assert.False(t, ok)
	// 外壳没跑：成员回落独立部署
	stopped := cascade.NewResult(/* erp/a running, erp/shell not */)
	_, ok = stopped.HostOf(p, resolver.Ref{ID: "erp/a", Version: "1.0.0"})
	assert.False(t, ok)
}
```

(use the cascade package's real constructor for results, or compute `cascade.Compute(p, graph)` on a small graph if no constructor exists), and in `internal/inject/inject_test.go`:

```go
// 依赖一个被外壳承载的成员：地址指向外壳（提案 §8.8），端口仍是成员自己的端口。
func TestEndpointOfHostedMemberPointsAtShell(t *testing.T) { ... assert env["ERP_A_ENDPOINT"] == "http://erp-shell-1-0-0:8081" }
// 成员以裸进程运行（A18）或外壳没跑：地址指向成员自己。
func TestEndpointOfUnhostedMemberPointsAtMember(t *testing.T) { ... }
```

- [ ] **Step 2: run** `go test ./internal/cascade/ ./internal/inject/ -run 'HostOf|EndpointOf'` → FAIL (undefined / old address).
- [ ] **Step 3: implement** `host.go`; inject `buildComponent` passes the host (`states.HostOf(p, dep)`) to `addEndpoints`, which uses the host's service name with the member's ports. Replace every `shell.ShellRef` call with `cascade.ShellOf` (declared) or `states.HostOf` (effective): `shell.Resolve`, compose `newPlan`/`applyShellGroups`/`fallbackStandaloneWarnings`, k8s `newPlan`/`applyShellGroups`/`fallbackStandaloneWarnings`, cli `graph.go`.
- [ ] **Step 4:** `go test ./internal/cascade/ ./internal/inject/ ./internal/shell/ ./internal/compose/ ./internal/k8s/ ./internal/cli/` → PASS (existing tests that asserted `http://<member>:port` for hosted members change to the shell address).
- [ ] **Step 5: commit** `feat(shell): one hosting decision; member endpoints point at the shell`.

---

### Task 2: Declaration consistency

**Files:**
- Create: `internal/shell/check.go`, `internal/shell/check_test.go`
- Modify: `internal/shell/shell.go` (`Resolve` calls `Check` first), `internal/msgid/shell.go`, catalogs

**Interfaces:**
- Produces: `func Check(p *project.Project, graph *resolver.Graph, states *cascade.Result) error` — for every brickkit.yaml component whose node is in the graph:
  1. `kind: shell` ⇔ `manifest.IsShell()` (A11). Either direction mismatching → CONFIG_INVALID naming the component, what brickkit.yaml says and what component.yaml says, hint "add/remove `kind: shell`".
  2. every deploy `members` entry of a shell is in the shell manifest's `shell.members` (`CanHost`) — else CONFIG_INVALID naming shell and member, hint listing what the shell can host.
  3. a shell running as a bare process (`mode: debug|local`) while it hosts at least one member → CONFIG_INVALID (ruling: members live inside the shell's process; container-less shells with members are a follow-up), hint "run the shell in a container, or move the members out".
- Rule "member has a standalone image" needs no runtime check: manifest validation already requires `deployment.image` or `deployment.build` for every component (spec §8.1 rule 1 holds by construction; `add`'s check is P4).

- [ ] **Step 1: failing tests** in `check_test.go`, one per rule plus a clean case:

```go
func TestCheckKindWithoutShellBlock(t *testing.T)    { /* decl kind: shell, manifest no shell → error mentions "kind: shell" and component id */ }
func TestCheckShellBlockWithoutKind(t *testing.T)    { /* manifest shell.members, decl no kind → error */ }
func TestCheckMemberNotHostable(t *testing.T)        { /* members: [erp/x], manifest shell.members: [erp/a] → error names erp/x and erp/shell */ }
func TestCheckBareShellWithMembersRejected(t *testing.T) { /* shell mode: local + running member → error */ }
func TestCheckConsistentProject(t *testing.T)        { /* no error */ }
```

- [ ] **Step 2:** `go test ./internal/shell/ -run TestCheck` → FAIL.
- [ ] **Step 3:** implement; new msgids `ShellKindWithoutBlock`, `ShellBlockWithoutKind`, `ShellMemberNotHostable`, `ShellBareWithMembers` + hints, en/zh.
- [ ] **Step 4:** shell, compose, k8s, cli tests PASS (fixtures that relied on unchecked shells gain `ShellMembers` in their test manifests).
- [ ] **Step 5: commit** `feat(shell): kind/shell block/members consistency checked before generation`.

---

### Task 3: `BRICKKIT_SERVED_MEMBERS_CONFIG` with evaluated values

**Files:**
- Modify: `internal/shell/shell.go`, `internal/shell/shell_test.go`, `internal/msgid/shell.go`, catalogs
- Modify callers: `internal/compose/compose.go`, `internal/k8s/k8s.go` (pass the lookup)

**Interfaces:**
- `Resolve(p, graph, states, env, lookup func(string) (string, bool)) ([]Group, error)`
- `Member.Config map[string]string` = the member's evaluated environment exactly as it would receive it standalone, minus `COMPONENT_ID`/`COMPONENT_VERSION` (those are the JSON's `componentId`/`version`): its config keys and its `*_ENDPOINT` variables.
- JSON element (spec §8.2): `{"componentId","version","httpPort","extraPorts":[{"name","port"}],"config":{KEY: value}}`, sorted by componentId; `[]` for zero members.
- `Apply` writes `BRICKKIT_SERVED_MEMBERS` (plain literal) and `BRICKKIT_SERVED_MEMBERS_CONFIG` as `inject.Var{Secret: true, Key: "BRICKKIT_SERVED_MEMBERS_CONFIG", Value: inject.Literal(json)}`; member config no longer spills into the shell's environment (no prefixed variables, no endpoint merge, no collision errors).
- Evaluation (spec §8.3 scene A): literal as is; `file://` read; `${VAR}` strictly — every referenced name must resolve through `lookup`, else CONFIG_INVALID naming member, key and variable; `{existingSecret, key}` → CONFIG_INVALID (the CLI cannot read it; the shell JSON needs the value); a value that is not valid UTF-8 → CONFIG_INVALID (JSON would silently replace bytes).

- [ ] **Step 1: failing tests** in `shell_test.go`:

```go
func TestServedMembersConfigCarriesEvaluatedValues(t *testing.T) {
	// member config: PEM via file:// (contains $ " \ and newlines), a ${TOKEN} template, a literal,
	// and a dependency endpoint; lookup TOKEN=t$k
	// assert: JSON decodes to config{"CERT": pem, "TOKEN": "t$k", "MODE": "x", "DEP_ENDPOINT": "http://..."}
	// assert: Group env has no ERP_A_* prefixed variables
	// assert: the SERVED_MEMBERS_CONFIG var has Secret == true
}
func TestResolveMemberTemplateUnresolved(t *testing.T) { /* ${MISSING} → error names erp/a, key, MISSING */ }
func TestResolveMemberExistingSecretRejected(t *testing.T) { /* error mentions existingSecret */ }
func TestResolveMemberInvalidUTF8Rejected(t *testing.T) { /* file:// with \xff → error */ }
func TestServedMembersConfigEmptyIsArray(t *testing.T) { /* shell with zero running members → "[]" */ }
```

- [ ] **Step 2:** `go test ./internal/shell/` → FAIL.
- [ ] **Step 3:** implement; delete `mergeGroup`, `endpointCollisionError`, `configVarCollisionError`, `shellNotRunningError`, `memberConfig`'s old shape and their msgids.
- [ ] **Step 4:** `go test ./internal/shell/ ./internal/compose/ ./internal/k8s/` → PASS (compose/k8s tests asserting prefixed member variables are rewritten to assert the JSON).
- [ ] **Step 5: commit** `feat(shell): member config reaches the shell as evaluated JSON, placed as a secret`.

---

### Task 4: Docker — member migrations and JSON placement

**Files:**
- Modify: `internal/compose/compose.go`, `internal/compose/servedby.go`, `internal/compose/servedby_test.go`, msgids/catalogs

**Interfaces:**
- A hosted member with `migration` gets service `<member-service>-migration`: member image (`manifest.ImageRef`), member `migration.command` (entrypoint/command split as for ordinary components), the member's own environment placed by the normal rules (its own env file when it has secrets), `restart: "no"`.
- Member migrations join `chainMigrations` (versions of one component ID in order).
- The shell's main service `depends_on` every hosted member's migration with `service_completed_successfully`.
- `servedMigrationWarnings` and its msgids are deleted.
- `BRICKKIT_SERVED_MEMBERS_CONFIG` lands in the shell's env file (it is a secret literal: `$` doubled, escaped) — no code change expected beyond Task 3; asserted here.

- [ ] **Step 1: failing tests** in `servedby_test.go`:

```go
func TestHostedMemberMigrationUsesMemberImage(t *testing.T) {
	// member erp/a with migration ["/app/a","migrate"], image registry/erp-a:1.0.0, secret config
	// assert services["erp-a-1-0-0-migration"].image == "registry/erp-a:1.0.0", entrypoint ["/app/a"]
	// assert it has env_file .brickkit/generated/env/erp-a-1-0-0.env
	// assert services["erp-shell-1-0-0"].depends_on["erp-a-1-0-0-migration"].condition == "service_completed_successfully"
	// assert no "erp-a-1-0-0" main service
}
func TestShellJSONGoesToEnvFile(t *testing.T) {
	// assert compose.yaml has no BRICKKIT_SERVED_MEMBERS_CONFIG= line and no member secret text
	// assert shell env file contains BRICKKIT_SERVED_MEMBERS_CONFIG="[{..." with $ doubled
}
```

- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** `go test ./internal/compose/` PASS.
- [ ] **Step 5: commit** `feat(compose): hosted members migrate with their own image before the shell starts`.

---

### Task 5: K8s — member migration Jobs, JSON Secret, migration image

**Files:**
- Modify: `internal/k8s/k8s.go`, `internal/k8s/servedby.go`, `internal/k8s/job.go`, `internal/k8s/secret.go`, tests `internal/k8s/servedby_test.go`, msgids/catalogs

**Interfaces:**
- A hosted member with `migration` gets a Job `<member-service>-migration` (member image via `manifest.ImageRef`, member env via `envDoc`, member secrets in `<member-service>-config-secret`), included in `Result.MigrationGroups` (grouped by component ID, versions ascending).
- `migrationContainerDoc` uses `manifest.ImageRef(c.Manifest)` (it used `Deployment.Image`, wrong for untagged or build-only manifests).
- `BRICKKIT_SERVED_MEMBERS_CONFIG` is a `secretKeyRef` into the shell's config Secret; the Secret holds the JSON byte-exact.
- `servedMigrationWarnings` and its msgids are deleted.

- [ ] **Step 1: failing tests**:

```go
func TestK8sHostedMemberMigrationJob(t *testing.T) { /* Job file exists, image == member ImageRef, MigrationGroups contains it */ }
func TestK8sShellJSONInSecret(t *testing.T)        { /* shell env SERVED_MEMBERS_CONFIG is secretKeyRef; Secret stringData holds JSON with PEM intact */ }
func TestK8sMigrationUsesImageRef(t *testing.T)    { /* untagged image → image:version in the Job */ }
```

- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** `go test ./internal/k8s/` PASS.
- [ ] **Step 5: commit** `feat(k8s): hosted members get migration Jobs; shell JSON in a Secret`.

---

### Task 6: Members in `mode: debug` / `mode: local` (A18)

**Files:**
- Modify: `internal/compose/local.go` (a local component depending on a hosted member), `internal/compose/local_test.go`, `internal/compose/servedby_test.go`

**Semantics (ruling for A18's open part):** a member whose entry is `mode: debug|local` is not hosted this run (`HostOf` false): it runs as a bare process with its standalone environment in its local env file; it is absent from the shell's `BRICKKIT_SERVED_MEMBERS` and JSON; the shell keeps running with the remaining members; dependents reach the member at its own service name, which the existing debug routing maps to the host. Mode `disable` on a member just removes it (cascade).

- [ ] **Step 1: failing tests**:

```go
func TestDebugMemberLeavesTheShell(t *testing.T) {
	// erp/a debug (deploy.local.yaml), erp/b hosted, caller depends on erp/a
	// assert SERVED_MEMBERS == "erp-b-1-0-0", JSON has only erp/b
	// assert caller's ERP_A_ENDPOINT == "http://erp-a-1-0-0:<localPort>" and caller has extra_hosts erp-a-1-0-0:host-gateway
	// assert local env file local-debug.erp-a-1-0-0.env exists with erp/a's config
}
func TestLocalComponentDependingOnHostedMember(t *testing.T) {
	// local debug caller depends on hosted erp/b: its local env ERP_B_ENDPOINT == "http://localhost:<host port>" reaching the shell's published member port
}
```

- [ ] **Step 2:** FAIL (if only the second fails, the first is already satisfied by Task 1 — keep it as a regression test and record it). **Step 3:** implement. **Step 4:** `go test ./internal/compose/` PASS.
- [ ] **Step 5: commit** `feat(shell): members can run as bare processes (A18)`.

---

### Task 7: Wording, graph, CLI, end-to-end

**Files:**
- Modify: catalogs/msgids containing "servedBy" (shell/member wording; `--ignore-served-by` help says "ignore every shell's members"), `internal/compose/servedby.go`/`internal/k8s/servedby.go` comments, `internal/cli/graph.go`, `internal/cli/*_test.go`
- Create: `internal/cli/testdata/three-layer-shell/` (brickkit.yaml with `kind: shell`, a shell component with `shell.members`, two members — one with migration and a `file://` PEM secret containing `$` and `"`, one depending on the other), `internal/cli/shell_e2e_test.go`

- [ ] **Step 1: failing e2e test** `TestShellDryRunDockerAndK8s`: Docker `up --dry-run` → no member container, member migration service with the member image, shell env file holds the JSON whose `config.CERT` decodes byte-exact to the PEM, compose.yaml free of the PEM and of `BRICKKIT_SERVED_MEMBERS_CONFIG=`; a caller outside the shell gets `http://<shell>:<member port>`; `-f deploy.k8s.yaml` → member Job, shell JSON in Secret, Deployment free of PEM.
- [ ] **Step 2:** FAIL until fixture. **Step 3:** fixture + wording + graph fixes. **Step 4:** `go build ./... && go vet ./... && go test ./internal/... ./cmd/... ./tests/i18nguard/... ./tests/archguard/...` PASS; golangci-lint `./internal/... ./cmd/...` 0 issues.
- [ ] **Step 5: commit** `test(cli): shell end-to-end dry run for docker and k8s; shell wording`.

---

## Hand-off to P4

P4 (`add`/`remove`, git sources) maintains `kind: shell` and `members` automatically (spec §8.5, §8.7), using `shell.Check`'s rules as its validation and `yamlfile.Edit` for the file edits.
