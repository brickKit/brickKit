# P3e: `skipWaitFor`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merging members into a container shell can create a start-order cycle that Docker Compose refuses (`dependency cycle detected`, verified on v5.3.1). The user decided:
- The default stays an error, with a warning that names the dependencies and the ways out.
- A deploy entry may declare `skipWaitFor: [<component ids>]` to start without waiting for those required dependencies. The user accepts the cost that the component must tolerate them not being ready yet. The cost is documented (P9).

**Architecture:** `skipWaitFor` changes **waiting** only: compose `depends_on` and the printed start order. It does not change **reaching**: network policy egress/ingress and `extra_hosts` still follow the full dependency set, because skipping a wait must not make the dependency unreachable.
- `shell.WaitFor(p, graph, states, ref)` is `Dependencies` minus the IDs each source entry skips. The source is the component itself, or each hosted member for a shell.
- `componentDependsOn` and `Workloads` use `WaitFor`.
- `shell.Check` validates that every skipped ID is a required dependency of that component.

**Tech Stack:** Go 1.22, testify.

**Spec:** `new_plan/提案.md` Appendix A; this plan adds ruling A23.

## Global Constraints

- Messages go through `internal/msgid` and both catalogs, and the i18n guard stays green.
- Chinese comments. Commit on main with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Every task runs the full suite, golangci-lint, and a cross-build.

## Review Focus

1. **Member entry `erp/worker` with `skipWaitFor: [erp/pay]`, in the P3d cycle topology:**
   - Docker `up --dry-run` succeeds.
   - The shell has no `depends_on` on `erp/pay`.
   - `erp/pay` still depends on the shell.
   - The shell is still on the network path to `erp/pay` (k8s egress keeps `erp/pay`).
2. **`skipWaitFor` naming something that is not a required dependency of that component** (typo, an optional dependency, the component itself): generation fails and names the entry.
3. **Target k8s:** a warning says the field has no effect, and nothing else changes.
4. **The entry is a bare process, or a member of a bare-process shell:** a warning says the field has no effect.
5. **The start order line** for the workload says which dependencies it does not wait for.
6. **The cycle error** gains a third suggestion naming the concrete `skipWaitFor` line to write.

---

### Task 1: The field and its checks

**Files:** `internal/deployfile/{deployfile.go,validate.go}`, `internal/shell/{check.go,deps.go}` (+tests), `schemas/deploy.schema.json`, msgids.

**Rules:**
- `Entry.SkipWaitFor []string` (`yaml:"skipWaitFor,omitempty"`).
- Single-file checks: every item is a valid component ID, has no `@`, is not a duplicate, and is not the entry's own ID. The k8s target warning lists the field.
- `shell.Check`, via `checkSkipWaitFor`: every item is the ID of a required dependency of that component version, per its Manifest. The error names the deploy entry field and lists the required dependencies.

- [ ] Step 1: failing tests:
  - `TestSkipWaitForValidation` (deployfile)
  - `TestSkipWaitForMustNameARequiredDependency` (shell)
  - `TestSkipWaitForIgnoredOnK8sWarns` (deployfile)
- [ ] Step 2: FAIL. Step 3: implement. Step 4: PASS. Step 5: commit `feat(deploy): skipWaitFor field and its checks`.

### Task 2: Waiting follows `skipWaitFor`

**Files:** `internal/shell/deps.go`, `internal/compose/{compose.go,servedby.go}`, `internal/cli/{up.go,render_order.go}` (+tests), msgids.

**Rules:**
- `shell.WaitFor` feeds `componentDependsOn` and `Workloads`.
- `Dependencies` stays the full set, for egress and `extra_hosts`.
- Compose warns when `skipWaitFor` sits on a bare-process entry or on a member of a bare-process shell.
- The start-order line appends "(does not wait for X)".
- `MergeCycleError` adds a hint with a concrete line: `skipWaitFor: [<y>] on <x>'s entry`, where x → y is the first edge from inside a shell to outside it.

- [ ] Step 1: failing tests:
  - `TestSkipWaitForBreaksMergeCycle` (cli e2e on the P3d topology, docker and k8s)
  - `TestSkipWaitForKeepsReachability` (k8s egress still lists the dependency)
  - `TestSkipWaitForOnBareProcessWarns`
  - `TestStartOrderSaysWhatItDoesNotWaitFor`
  - `TestMergeCycleSuggestsSkipWaitFor`
- [ ] Steps 2–4. Step 5: commit `feat(compose): skipWaitFor drops the wait, not the connection`.

### Task 3: Spec ruling

**Files:** `new_plan/提案.md` Appendix A (A23), roadmap P9 note.

- [ ] Write A23: the field, its scope (docker/podman only), the default error, the cost and the documentation duty. Commit `docs(spec): A23 skipWaitFor`.
