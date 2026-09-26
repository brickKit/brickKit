# P3d: Deferred Minors from P2–P3c

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Clear every minor finding deferred by the P2, P3, P3b and P3c final reviews. The user asked for them to be fixed now rather than folded into later phases.

**Architecture:** No new concepts. Each fix makes an existing piece consistent with the three-layer model:
- generated output belongs to the current target only;
- lookups are keyed by `id@version`;
- nested member entries are reachable everywhere;
- messages name the file that actually holds a field.

**Tech Stack:** Go 1.22, testify.

**Spec:** `new_plan/提案.md` Appendix A (A7 secrets on disk, A20/A21 default version and nested members).

## Global Constraints

- Messages go through `internal/msgid` and both catalogs. The i18n guard stays green, and unused msgids are removed.
- Comments in Chinese, matching the surrounding style.
- Commit on main after each task. The message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- After each task: `go build ./...`, the full `go test ./internal/... ./cmd/... ./tests/i18nguard/... ./tests/archguard/...`, and `.tools/bin/golangci-lint run ./...`.
- Out of scope:
  - P2 minor "shell collision errors print secret values": the collision check was deleted in P3.
  - Long help texts of commands P7 rewrites (`lint`).
  - The market server (A2).

## Review Focus

1. **Switching target docker → k8s → docker:**
   - Moving to k8s leaves no `env/*.env` or `local-debug.*.env` behind.
   - Moving back to docker leaves no `k8s/secrets/*`.
   - A component that leaves `mode: debug` loses its `local-debug.*.env`.
2. **Every component moved to the host (`mode: local` / `debug`, or a bare shell):** containers from the previous run are stopped, volumes are kept. A machine without Docker still runs a pure-local project.
3. **`status` in local mode, with two versions whose modes differ between team and local files:** each version is labelled on its own.
4. **`up` start order with a running shell:** hosted members are not listed as services of their own.
5. **Editor:**
   - `DeleteField` keeps a comment attached to the deleted key's neighbours.
   - `Save` never leaves a half-written file.
   - `RemoveEntry` finds a nested member.

---

### Task 1: Generated output hygiene

**Files:** `internal/cli/up.go`, `internal/cli/up_k8s.go`, `internal/engine/compose.go` (+tests in `internal/cli`, `internal/engine`).

**Rules:**
- `pruneOtherTarget(layout, target)`:
  - the docker/podman path removes `generated/k8s/`;
  - the k8s path removes `generated/env/` and `generated/local-debug.*.env`.
  - It runs on every `up`, `--dry-run` included, right where each path writes its own files.
- `writeLocalEnvFiles` removes `local-debug.*.env` files that were not written this run.
- No containers this run:
  - `engine.Compose.Up` with no `Services`: when `PruneSelector` is set it runs `compose down --remove-orphans` (no `-v`) before the one-shots.
  - The `up` path with neither services nor one-shots asks the engine to `Down` when an engine resolves, and ignores the failure: no daemon means no containers.

- [ ] Step 1: write the failing tests:
  - `TestSwitchingTargetRemovesOtherTargetsSecrets`
  - `TestLeavingDebugRemovesItsEnvFile`
  - `TestUpWithOnlyOneShotsStopsOldContainers` (engine)
  - `TestAllOnHostStopsOldContainers` (cli, fake engine records a down)
- [ ] Step 2: run them and confirm FAIL. Step 3: implement. Step 4: run and confirm PASS. Step 5: commit `fix(up): generated output and containers belong to this run only`.

### Task 2: Per-version lookups and shells in CLI output

**Files:** `internal/cli/{status.go,lifecycle.go,up_local.go,render_order.go,up.go,graph.go}` (+tests).

**Rules:**
- `status`: `localModeDiffers(ref)` and `labelIfOverridden(ref, …)`; team modes keyed by `id@version`.
- `checkLocalSources`:
  - running refs are keyed by `id@version`;
  - the problem points at the deploy file entry (source = deploy path, field = that entry's `Field` from `All()`).
- The start order printout skips hosted members. They show under their shell as "in shell X".
- `graph` runs `shell.Check` after resolving, like `up`.

- [ ] Step 1: write the failing tests:
  - `TestStatusLabelsEachVersionOnItsOwn`
  - `TestLocalSourceMissingPointsAtDeployEntry`
  - `TestStartOrderSkipsHostedMembers`
  - `TestGraphRejectsInconsistentShell`
- [ ] Steps 2–4 as above. Step 5: commit `fix(cli): per-version lookups; hosted members shown under their shell`.

### Task 3: Editor and parser

**Files:** `internal/yamlfile/edit.go`, `internal/deployfile/parse.go` (+tests), msgids.

**Rules:**
- `DeleteField` moves the removed key's head comment onto the next key, or onto the entry when it was the last key.
- `Save` writes to a temp file in the same directory, then renames it over the target, keeping the permissions.
- `RemoveEntry` also removes a nested member entry.
- A member entry with its own `members:` reports `DeployfileMemberNested` ("members nest one level only") instead of the generic unknown-field error. The key is dropped from the node so the unknown-field walk does not report it twice.

- [ ] Step 1: write the failing tests:
  - `TestEditDeleteFieldKeepsNeighbourComment`
  - `TestEditSaveIsAtomic` (target unchanged when the rename fails; temp file cleaned up)
  - `TestEditRemoveNestedMember`
  - `TestThirdLevelMembersSaysOneLevel`
- [ ] Steps 2–4. Step 5: commit `fix(yamlfile): comments survive DeleteField, atomic Save, nested RemoveEntry`.

### Task 4: Messages and dead code

**Files:** `internal/i18n/catalog_{en,zh}.go`, `internal/shell/shell.go`, `internal/msgid/compose.go`.

**Rules:**
- **Deploy fields named in the wrong file:** hints that place a deploy field in brickkit.yaml now name the deploy file:
  - `HeaderOverwritten`, `ComposeEnvHeader`
  - `ComposeHintChangeExposePort`, `K8sHintAddHostname`, `K8sHintChangeHostname`
  - `K8sEnvVarsUndefined` → config/ or the deploy file's `vars`
  - the `mode: disable` sentences in the `up`/`down`/`sync` long help
- **servedBy wording:** `shellNotFoundError` wording loses servedBy (a member under a shell whose component.yaml was not resolved).
- **Dead code:** remove `shell.MemberLabels` and the empty const block in `internal/msgid/compose.go`.

- [ ] Step 1: write the failing test `TestMessagesNameTheFileThatHoldsTheField` (a table of msgids that must not say brickkit.yaml together with a deploy-field name). Step 2: FAIL. Step 3: fix. Step 4: PASS. Step 5: commit `fix(i18n): messages name the file that holds the field`.
