# P3b: Versioned Shell Members and Bare-Process Shells

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Two follow-ups the user asked for after P3: (A) a shell hosts one specific version of a member (`members: [erp/a@1.0.0]`) while other versions of that member run as ordinary components — multi-version compatibility; (B) a shell can run as a bare process (`mode: debug` / `mode: local`) with its members inside it — local development starts before containers.

**Architecture:** (A) Membership becomes version-aware: `project.Project` records the hosted version per member ID; `cascade.ShellOf` compares ID and version, so everything built on `HostOf` (inject endpoints, shell JSON, compose/k8s workloads, graph) follows. (B) When a shell is a bare process, its hosted members are host processes too: they take part in the local routing exactly like `mode: debug` components (their container dependencies are published to host ports and their addresses become `localhost:<port>`), the shell's reserved variables go into the shell's local env file, dependents reach members at `<shell service>:<member port>` (containers, via `extra_hosts`) or `localhost:<member port>` (bare processes), and member ports are claimed on the host. Member migrations stay containers and keep container addresses.

**Tech Stack:** Go 1.22, testify; packages `internal/{deployfile,project,cascade,shell,compose,cli}`.

**Spec:** `new_plan/提案.md` §8 (8.4 members, 8.6 single-version shell, 8.9.4 migrations), Appendix A (A18 extended by the user: shells, not only members, run as bare processes). User decisions in conversation: `members@version` approved; bare ID remains the normal case.

## Global Constraints

- Messages via `internal/msgid` + both catalogs; `go test ./tests/i18nguard/...` green; unused msgids removed.
- Chinese comments matching the surrounding style.
- Commit on main, message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Each task: `go build ./...`, task tests, i18n guard, golangci-lint on touched packages.
- K8s is unaffected by B (bare modes are rejected under k8s).

## Review Focus

1. `erp/a@1.0.0` hosted and `erp/a@2.0.0` standalone: a caller of 2.0.0 gets `erp-a-2-0-0`, a caller of 1.0.0 gets the shell; the shell JSON holds only 1.0.0; the two migrations are chained by version. (Task 1)
2. A bare shell's member that depends on a container gets `localhost:<published host port>` in the shell's local env JSON, and that port is published on the container. (Task 2)
3. The member migration container of a bare shell still receives container addresses (`http://<service>:<port>`), not localhost. (Task 2)
4. A container calling a member of a bare shell can resolve the shell's service name (`extra_hosts`) and reaches the member's port; a bare-process caller gets `localhost:<member port>`. (Task 2)
5. Two members of a bare shell declaring the same port, or a member port colliding with another host port, fail at generation. (Task 2)

---

### Task 1: Versioned members

**Files:** `internal/deployfile/validate.go` (+test), `internal/project/check.go`, `internal/project/load.go` (+test), `internal/cascade/host.go` (+test), `internal/shell/check.go` (+test), `internal/compose/servedby_test.go`, `internal/project/projecttest/entries.go` (ServedBy with a pinned member version), msgids/catalogs.

**Rules:**
- A `members` entry is `id` or `id@version` (exact version). Validation checks the ID part like before and the version format.
- `project.checkMembers`: the member ID must be declared; `id@version` must match a declared version; a bare `id` with more than one declared version → CONFIG_INVALID "write which version the shell hosts (id@version)"; a shell as a member and one ID in two shells stay rejected.
- `(*Project).ShellOf(id, version string) (string, bool)` replaces the ID-only lookup: true only for the hosted version.
- `cascade.ShellOf` passes the version; `shell.Check` compares `CanHost` against the ID part.

- [ ] Step 1: failing tests — `TestLoadMemberVersionPinned` (two versions, `members: [erp/a@1.0.0]` loads; `ShellOf("erp/a","2.0.0")` false), `TestLoadMemberBareWithMultipleVersionsRejected` (error mentions `erp/a@`), `TestLoadMemberVersionUndeclared`, `TestHostOfIsVersionAware` (cascade), `TestComposeHostsOneVersionAndRunsTheOtherStandalone` (compose: shell JSON has 1.0.0 only; service `erp-a-2-0-0` exists; caller of 2.0.0 gets `http://erp-a-2-0-0:8081`, caller of 1.0.0 gets the shell address; `erp-a-2-0-0-migration` depends on `erp-a-1-0-0-migration`).
- [ ] Step 2: run → FAIL. Step 3: implement. Step 4: `go test ./internal/deployfile/ ./internal/project/ ./internal/cascade/ ./internal/shell/ ./internal/compose/ ./internal/k8s/ ./internal/cli/` PASS.
- [ ] Step 5: commit `feat(shell): a shell hosts one version of a member; other versions run standalone`.

### Task 2: Bare-process shells (Docker)

**Files:** `internal/shell/check.go` (drop the bare-shell rejection + msgids), `internal/compose/{compose.go,servedby.go,local.go}`, tests `internal/compose/servedby_test.go`, `internal/shell/check_test.go`.

**Rules:**
- `applyShellGroups` applies to a shell in `p.locals` too: its local env file carries `BRICKKIT_SERVED_MEMBERS` and the evaluated `BRICKKIT_SERVED_MEMBERS_CONFIG`.
- Members hosted by a bare shell ("host members"): their environment is copied (so member migrations keep container addresses), their running dependencies are mapped to host ports like a local component's (`mapDependencyToHost`) and pointed at `localhost` (`pointDependenciesAtLocalhost` generalised to `(ref, vars)`); the copied environment is what `shell.Resolve` puts in the JSON.
- Their declared main and extra ports are claimed in the host port table (owner "member via shell"); a collision is a generation error.
- A container depending on a host member: endpoint stays `http://<shell service>:<member port>` (inject), `extra_hosts` gets `<shell service>:host-gateway` (host of the dependency is local). A bare process depending on a host member: `localhost:<member port>` (and extra ports).
- `mapDependencyToHost` skips dependencies that are host members (nothing to publish: they already listen on the host).

- [ ] Step 1: failing tests — `TestBareShellLocalEnvCarriesJSON`, `TestBareShellMemberReachesContainerViaLocalhost` (+ port published on the container), `TestBareShellMemberMigrationKeepsContainerAddresses`, `TestContainerCallsMemberOfBareShell` (extra_hosts shell:host-gateway, endpoint shell:memberPort), `TestLocalCallsMemberOfBareShell` (localhost:memberPort), `TestBareShellMemberPortCollision`; `TestCheckBareShellWithMembersRejected` is replaced by `TestCheckBareShellWithMembersAllowed`.
- [ ] Step 2: FAIL. Step 3: implement. Step 4: `go test ./internal/shell/ ./internal/compose/ ./internal/cli/` PASS.
- [ ] Step 5: commit `feat(compose): a shell can run as a bare process with its members`.

### Task 3: End-to-end and wording

**Files:** `internal/cli/shell_e2e_test.go` (bare-shell subtest: `deploy.local.yaml` with the shell `mode: debug`, local mode on → `local-debug.erp-shell-1-0-0.env` holds the JSON with the PEM, the member migration service remains, `erp/portal` gets `extra_hosts` for the shell), `internal/cli/testdata/three-layer-shell/` (versioned second member version for the multi-version check if useful), catalogs.

- [ ] Step 1: failing e2e. Step 2: FAIL. Step 3: fixture/code. Step 4: `go build ./... && go vet ./... && go test ./internal/... ./cmd/... ./tests/i18nguard/... ./tests/archguard/...` PASS; golangci-lint 0 issues.
- [ ] Step 5: commit `test(cli): bare-process shell and versioned members end to end`.
