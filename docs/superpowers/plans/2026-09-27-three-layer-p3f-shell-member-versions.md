# P3f: The Shell Declares Which Member Versions It Contains

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A shell's `component.yaml` states the exact member versions compiled into it, e.g. `shell.members: [erp/api@1.2.0]`. `up` and `graph` check that every member the shell hosts this run is exactly that version. A mismatch fails loudly with three ways out. This is ruling A24, the last piece of the shell + multi-version logic.

**Architecture:** The three files keep their roles, and the platform checks rather than derives:
- brickkit.yaml plus the deploy file say what runs this time (A20/A21 unchanged: a nested member entry without a version means the default version).
- The shell's `component.yaml` says what the shell is.

Changes:
- `manifest.Shell.Members` items become `id@exact-version`.
- `(*Manifest).HostedVersion(id)` replaces `CanHost`.
- `shell.Check` compares the version each shell hosts this run (`cascade.Result.HostOf`) with the declared one.

**Tech Stack:** Go 1.22, testify; packages `internal/{manifest,shell,cli,compose,k8s,project/projecttest,schemagen}`.

**Spec:** `new_plan/提案.md` Appendix A — A24 (overrides §8.6 and the ID-only capability wording in §8.4), with A20/A21 unchanged.

## Global Constraints

- Messages go through `internal/msgid` and both catalogs. The i18n guard (including the message guard test) stays green, and unused msgids are removed.
- Chinese comments matching the surrounding style.
- Commit on main after each task. The message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- After each task, run all of the following:
  - `go build ./...`
  - `go vet ./...`
  - `go test -count=1 ./internal/... ./cmd/... ./tests/i18nguard/... ./tests/archguard/...`
  - `.tools/bin/golangci-lint run ./...`
  - `GOOS=windows` / `GOOS=darwin` builds
- Out of scope, per the roadmap:
  - `add` writing member versions from a shell's `component.yaml` (P4).
  - Member local repo version == declared version for bare-process shells (P4, with A22).
  - The image label safety net (P5).
  - Upgrading a shell (P6).
  - Docs and skill assets (P9 / P7).
  - The market server (A2).

## Review Focus

1. **Malformed `shell.members` entries:** an entry without a version, with a range (`^1.2.0`), naming the shell itself, or naming the same component ID twice (two versions of one ID) fails `component.yaml` validation at `shell.members[i]`. This includes `brickkit lint` on a local source.
2. **Nested bare `erp/api` where brickkit.yaml's default is 1.3.0 but the shell declares 1.2.0:** `up --dry-run` and `graph` fail. The error names the shell, the member, both versions and the deploy entry, and the three hints carry the concrete lines to write.
3. **Nested `erp/api@1.2.0`** (brickkit.yaml has 1.3.0 default and `1.2.0 requiredBy: [erp/shell]`), with the shell declaring 1.2.0: this passes.
   - The shell JSON holds 1.2.0.
   - 1.3.0 runs standalone.
   - Callers of each version reach the right address.
   - The two migrations are chained 1.2.0 → 1.3.0.
4. **Only members hosted this run are checked:**
   - A member in `mode: debug` / `local`, a member whose shell is `mode: disable`, and runs with `--ignore-shells` are not checked, since the shell does not load them this time.
   - A bare-process shell is checked. Its members do run inside it.
5. **Shell declares a member the project does not host this run** (no nested entry): this is fine. The declaration describes the image, not the project.

---

### Task 1: `shell.members` carries exact versions

**Files:**
- Modify: `internal/manifest/types.go` (doc comment of `Shell`), `internal/manifest/validate.go` (`validateShell`), `internal/manifest/image.go` (`CanHost` → `HostedVersion`).
- Test: `internal/manifest/validate_test.go` (or the file holding shell validation cases), `internal/manifest/image_test.go`.
- Regenerate: `schemas/component.schema.json` (`make generate-schemas`). Give `Shell.Members` items the same exact-ref pattern the string form of a dependency uses, if the schema generator supports a per-item pattern. Otherwise leave items as strings and record a ruling.
- Messages: `ManifestShellMemberNeedsVersion` ("write the exact version compiled into the shell, e.g. erp/api@1.2.0"), and `ManifestShellMemberTwoVersions` (one shell contains one version of a component).
- Adjust every constructor of `manifest.Shell` in tests and helpers to the `id@version` form:
  - `internal/project/projecttest/entries.go` (`FillShellCapability` appends `e.ID+"@"+e.Version`)
  - `internal/shell/shell_test.go` (`resolveFixture`), `internal/shell/check_test.go` (`shellManifest`)
  - `internal/compose/servedby_test.go:484`, `internal/k8s/servedby_test.go:355`
  - `internal/cli/testsupport_component_test.go` (`ShellMembers` written as given; callers pass `id@version`), `internal/cli/graph_test.go`
  - `internal/cli/testdata/three-layer-shell/shell/erp/shell/component.yaml` → `erp/api@1.0.0`, `erp/worker@1.0.0`

**Interfaces:**
- Produces:
  - `func (m *Manifest) HostedVersion(id string) (version string, ok bool)`: the version the shell declares for component `id`; `ok` is false when `m` is not a shell or does not contain `id`.
  - `CanHost` is removed. Its callers use `HostedVersion`.

- [ ] **Step 1: Write the failing tests.**
  - `TestValidateShellMembersNeedExactVersions`: a table with cases "bare id", "range", "self", "same id twice", and "valid `[erp/api@1.2.0, erp/worker@1.0.0]`". Each error case asserts the field `shell.members[i]`.
  - `TestHostedVersion`: a shell declaring `erp/api@1.2.0` gives `("1.2.0", true)` for `erp/api`, `("", false)` for `erp/other`, and `("", false)` on a non-shell manifest.
- [ ] **Step 2:** Run `go test ./internal/manifest/`. Expected: FAIL (`HostedVersion` undefined, and bare IDs accepted).
- [ ] **Step 3: Implement.**
  - `validateShell` splits each item on `@`. It rejects a missing version (`ManifestShellMemberNeedsVersion`), a non-exact version (the existing exact-version message), the shell's own ID (`DeployfileMemberSelf`) and a repeated ID (`ManifestShellMemberTwoVersions`). It keeps the ID format check.
  - Add `HostedVersion`. Update the `Shell` doc comment: "构建时编进外壳的成员及其精确版本（附录 A24）".
- [ ] **Step 4:** Update every helper and fixture listed above. Run the full suite: PASS.
- [ ] **Step 5:** Commit `feat(manifest): shell.members declares the exact member versions compiled in`.

### Task 2: `up` and `graph` check hosted versions against the declaration

**Files:**
- Modify: `internal/shell/check.go` (use the `states` parameter, compare versions, and put the member-hostable check on `HostedVersion`), msgids and catalogs.
- Test: `internal/shell/check_test.go`, `internal/cli/shell_e2e_test.go`.

**Rules:**
- For every running member this run where `states.HostOf(p, ref)` gives shell S (hosted this run): `declared, ok := S.Manifest.HostedVersion(ref.ID)`.
  - `!ok`: the existing `ShellMemberNotHostable`, reworded to "the shell does not contain it". Its detail lists the declared `id@version` items.
  - `declared != ref.Version`: `ShellMemberVersionMismatch`, e.g. "shell erp/shell@1.0.0 contains erp/api@1.2.0, but this run hosts erp/api@1.3.0".
    - Details: the file (deploy path) and the member entry field (`deployfile.File.EntryAt`).
    - Hints, with concrete values:
      1. `ShellHintUpgradeShell`: upgrade the shell to a version that contains erp/api@1.3.0.
      2. `ShellHintMoveMemberOut`: move erp/api's entry to the top level so it runs on its own.
      3. `ShellHintKeepBothVersions`: add `- {id: erp/api, version: 1.2.0, requiredBy: [erp/shell]}` to brickkit.yaml and write `- id: erp/api@1.2.0` under the shell. erp/api@1.3.0 then runs on its own.
- Members that are not hosted this run are not checked (Review Focus 4). The `kind: shell` ↔ `shell` block check (`checkKind`) and `checkSkipWaitFor` stay as they are.
- Check order: kind first, then hostable and version, then `skipWaitFor`.

- [ ] **Step 1: Write the failing tests.**
  - `TestCheckMemberVersionMismatch` (shell package): brickkit.yaml has erp/a 1.3.0 as default, the shell declares `erp/a@1.2.0`, and the nested entry is bare `erp/a`. The error mentions both versions, the member entry field, and each of the three hints.
  - `TestCheckMemberPinnedToDeclaredVersion`: brickkit.yaml has 1.3.0 default and `1.2.0 requiredBy: [erp/shell]`, the nested entry is `erp/a@1.2.0`, and the shell declares 1.2.0. No error, and `cascade.ShellOf` hosts 1.2.0 only.
  - `TestCheckSkipsMembersNotHostedThisRun`: the same mismatch passes when the member is in `mode: debug`, when the shell is `mode: disable`, and after `IgnoreShells()`.
  - `TestShellMemberVersionMismatchEndToEnd` (cli): in a copy of `three-layer-shell`, change the shell's `component.yaml` to `erp/api@0.9.0`. Both `up --dry-run` and `graph` exit with an error whose stderr contains the three hints.
  - `TestShellHostsDeclaredVersionWhileDefaultRunsStandalone` (cli, Review Focus 3): add an `erp/api` 1.1.0 component directory to the fixture, make 1.1.0 the default, and keep 1.0.0 as `requiredBy: [erp/shell]` nested as `erp/api@1.0.0`. Assert:
    - The compose file has `erp-api-1-1-0` and no `erp-api-1-0-0` service.
    - The shell env JSON holds version 1.0.0.
    - erp/portal (which depends on erp/api@1.0.0) gets the shell address.
    - `erp-api-1-1-0-migration` depends on `erp-api-1-0-0-migration`.
- [ ] **Step 2:** Run the tests. Expected: FAIL, because no version comparison exists yet.
- [ ] **Step 3: Implement** as in the Rules. Remove messages that are no longer used.
- [ ] **Step 4:** Run the full suite and all the checks from Global Constraints: PASS.
- [ ] **Step 5:** Commit `feat(shell): hosted member versions must match what the shell contains`.

### Task 3: Final review

- [ ] Run `review-package` over the plan's range. Dispatch the final reviewer (opus) with the Review Focus above, the ledger's `Ruling:` lines, and A24. Do one fix pass, verified by TDD. Report to the user in Chinese.
