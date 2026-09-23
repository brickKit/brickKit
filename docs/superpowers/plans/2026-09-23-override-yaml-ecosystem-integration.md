# override.yaml Ecosystem Integration (Plan 2b) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish wiring `override.yaml` into the rest of the CLI's command surface — `add`, `remove`,
`lint`, `restore`, `status`, and `up`'s drift note — and give `brickkit override` a documented entry
in the CLI reference, closing out everything Plan 2a (the core mechanism) deliberately deferred.

**Architecture:** Plan 2a already built the whole mechanism this plan reuses: the `internal/override`
package (`ParseOverrideFile`, `Validate`, `CheckAgainst`, `Drift`), `internal/cli/override.go`
(`generateOverride`, `renderOverride`, `componentEntryFor`/`memberEntryFor`), and `internal/cli/topology.go`
(`loadOverride`, `applyOverride`, `isDefaultConfigFile`). Nothing here invents a new mechanism — every
task is "read or write `override.yaml` at one more command's touchpoint," using functions that already
exist and are already tested at the unit level.

**Tech Stack:** Go 1.22+, the existing `internal/override`/`internal/config`/`internal/clierr`/
`internal/i18n` packages, `gopkg.in/yaml.v3` (already a dependency).

**Spec:** `docs/superpowers/specs/2026-09-23-deploy-config-override-design.md` — specifically §7
(staleness/drift detection), §9 (command integration table), §10 (multi-environment safety, already
fully implemented by Plan 2a), and §12 (testing considerations). This plan implements exactly the rows
of §9's table that Plan 2a's own Global Constraints marked out of scope: `add`, `remove`, `lint`,
`restore`, and the "non-blocking drift note from `up`" line from §7. `status`'s override-yaml reading
was technically already wired by Plan 2a (via `loadProject`'s `applyOverride` call), but the *labeling*
§9 also asks for ("a component that isn't running because of a local `disable` is labeled as such, not
left unexplained") was not — that's this plan's Task 6.

## Global Constraints

- `override.yaml` is optional. Every touchpoint this plan adds must treat "the file doesn't exist" as
  the default, common, fully legal case — never an error, never a printed line that implies something
  is missing.
- `override.yaml` never gets merged field-by-field with `brickkit.yaml`. A topic is either silent here
  (defer entirely to `brickkit.yaml`) or explicit (fully replaces `brickkit.yaml`'s value for that one
  topic). No task in this plan invents a third state.
- Component entries in `override.yaml` use bare `id`, never `id@version` — one entry applies uniformly
  to every coexisting version of that ID (design spec §6.1's last bullet; already the shape
  `internal/override.ComponentOverride`/`MemberOverride` enforce).
- `mode: debug` can only ever be written in `override.yaml`. `brickkit.yaml` itself rejects it
  unconditionally, at parse time (`internal/config/validate.go`'s `validateComponentMode`) — nothing in
  this plan touches that rule, only its ecosystem.
- `--config` pointing at a non-default `brickkit.yaml` means `override.yaml` must be ignored, with a
  warning (`isDefaultConfigFile` in `internal/cli/topology.go`). Every new touchpoint in this plan reads
  `override.yaml` exclusively through `override.ParseOverrideFile`/`loadOverride`/the two write-path
  helpers this plan adds (§Task 1, §Task 2) — never a second, ad-hoc `os.ReadFile` of the path.
- Drift detection is content-based, never timestamp-based (`internal/override.Drift` already implements
  this — this plan only adds new call sites, never a new drift-detection mechanism).
- TDD is mandatory for every step, per `superpowers:test-driven-development` (loaded automatically by
  `executing-plans`/`subagent-driven-development` — do not skip it because "the change is small").
- No worktree. Per this repo's standing instruction (the sole maintainer works directly on `main`), the
  Setup step of whichever execution skill runs this plan must skip creating a worktree and note that in
  the ledger, exactly as Plan 2a's own ledger did.
- Every new user-visible string goes through `internal/msgid` + `internal/i18n/catalog_en.go` +
  `internal/i18n/catalog_zh.go` — never a hardcoded string in `internal/cli`. Verify with
  `go test ./tests/i18nguard/...` after every task.
- `internal/cli/podman_target_test.go`'s `runIn`/`runWith` helpers default to `logging.LevelOff` (quiet
  test output). Don't assert on the `error_code` JSON log line in this plan's tests — assert on the
  printed message text instead, matching every existing test in this package.

## Review Focus

- **`brickkit add`'s two regeneration paths and the boundary between them.** "Has no overrides beyond
  bare-id defaults" needs a precise, testable definition (any non-empty `Mode`/`LocalPort`/`Baseline`
  anywhere, including nested members, and any non-empty `Target`/`TargetBaseline`) — a reasonable user
  expects `add` to never silently discard a real customization, and expects it to *not* nag them with a
  reminder when there was nothing to lose. Test: `TestAddDoesNotTouchOverrideWithRealCustomizations`
  (Task 1) pins the "don't touch it" half; `TestAddRegeneratesDefaultOnlyOverrideForNewComponent` pins
  the "just refresh it" half.
- **`brickkit remove` promoting a shell's nested members must preserve their own customizations**, not
  just their ID — a member that had `mode: disable` in `override.yaml` still needs `mode: disable` after
  it's promoted to a top-level entry, or removing an unrelated shell silently re-enables a component the
  user had deliberately turned off. Test: `TestRemoveShellPromotesMembersToTopLevel` (Task 2) asserts the
  promoted member keeps its `mode: disable`, not just its `id`.
- **`brickkit lint --strict`'s severity split for the two staleness checks must go the right way**: a
  dangling entry (component removed from `brickkit.yaml`, still referenced in `override.yaml`) is an
  **error** even without `--strict` (matches `CheckAgainst`'s existing severity, already enforced at
  `up`/`sync`/`status`/`down`/`brickkit override` time); a stale `baseline` is a **warning**, only
  escalated to failure by `--strict` (spec §7's explicit instruction). Swapping these would either block
  `lint` on something the spec calls "worth reviewing, not broken," or silently let a real dangling
  reference through a non-strict CI gate. Tests: `TestLintCatchesDanglingOverrideEntry` (fails without
  `--strict`) and `TestLintWarnsOnStaleBaselineWithoutFailing` / `TestLintStrictFailsOnStaleBaseline`
  (Task 3) pin both halves.
- **`brickkit status`'s override-sourced label must fire for exactly the case the spec names — a
  component skipped because of a `mode: disable` set in `override.yaml`** — and must *not* fire for a
  component skipped because something upstream of it was disabled (that component's own ID never
  appears in `override.yaml`, so mislabeling it would point the user at the wrong file). In practice
  `mode: debug`/`mode: local` set via `override.yaml` never reach the "skipped" table at all — a
  `mode: debug` component is rendered in the separate local-debugging table (`renderLocalDebug`), and
  `mode: local` gets no table row at all (`degradedView`'s own comment explains why: it's covered by the
  session-lock hint instead). `labelIfOverridden`'s check ("did `override.yaml` set *any* mode for this
  ID") is written generically rather than special-cased to `disable` only because that's the simpler
  implementation, not because the other values are reachable through this code path today — don't read
  that as a claim this task tests `mode: debug`/`local` labeling, it doesn't, because there is currently
  nothing there to test. Test: `TestStatusLabelsComponentDisabledByOverride` (fires) and
  `TestStatusDoesNotLabelComponentDisabledInBrickkitYamlItself` (Task 6, doesn't fire) pin exactly the
  boundary the spec names.
- **The new CLI reference section (Task 7) must use real captured CLI output, not hand-written text.**
  `tests/docfields/outputlines_test.go` verifies every line beginning with a CLI symbol (✅❌⚠️📝…)
  against the live `internal/i18n` catalog — a single hand-typed word that drifts from the real message
  breaks `make lint` silently until someone runs it. This plan's Task 7 steps include running the real
  built CLI against a scratch project and pasting its actual stdout, mirroring how Plan 2a's own guide-
  tutorial fix (documented in that plan's SDD ledger) was verified.

---

## Task 1: `brickkit add` keeps `override.yaml` in sync

**Files:**
- Create: `internal/cli/add_override.go`
- Create: `internal/cli/add_override_test.go`
- Modify: `internal/cli/add.go:204-208` (call the new helper after `writeComponents` succeeds)
- Modify: `internal/cli/add_local.go:90-94` (call the same helper after `writeLocalComponents` succeeds)
- Modify: `internal/msgid/cli_add.go` (two new message IDs)
- Modify: `internal/i18n/catalog_en.go`, `internal/i18n/catalog_zh.go` (their text)

**Interfaces:**
- Consumes: `override.ParseOverrideFile(path string) (*override.Override, error)`,
  `override.Override{Target, TargetBaseline, Components []override.ComponentOverride, Source}`,
  `override.ComponentOverride{ID, Mode, LocalPort, Baseline, Members []override.MemberOverride}`,
  `override.MemberOverride{ID, Mode, LocalPort, Baseline}` (all `internal/override`, already defined by
  Plan 2a); `generateOverride(cfg *config.Config, existing *override.Override) *override.Override` and
  `renderOverride(o *override.Override) ([]byte, error)` (both already defined, unexported, in
  `internal/cli/override.go` — same package, directly callable); `config.Layout.OverridePath() string`,
  `config.Layout.ConfigPath() string`, `config.ParseConfigFile(path string) (*config.Config, error)`.
- Produces: `syncOverrideAfterAdd(opts *Options, layout config.Layout) error` — called by both `add.go`
  and `add_local.go` after they've written new components to `brickkit.yaml`. `isOverrideDefaultOnly(ov
  *override.Override) bool` — a pure function other tasks do not need, but keep it exported-within-package
  (lowercase, `internal/cli` only) in case a later task in this plan needs the same check (none currently
  does, but keeping it as a named, tested function rather than inlining it is what makes Review Focus
  item 1 testable in isolation).

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/add_override_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/override"
)

func TestIsOverrideDefaultOnlyTreatsNilAsDefaultOnly(t *testing.T) {
	assert.True(t, isOverrideDefaultOnly(nil))
}

func TestIsOverrideDefaultOnlyAcceptsBareIDsOnly(t *testing.T) {
	ov := &override.Override{Components: []override.ComponentOverride{
		{ID: "demo/hello"},
		{ID: "erp/backend", Members: []override.MemberOverride{{ID: "people/basic"}}},
	}}
	assert.True(t, isOverrideDefaultOnly(ov))
}

func TestIsOverrideDefaultOnlyRejectsTargetOverride(t *testing.T) {
	ov := &override.Override{Target: "podman"}
	assert.False(t, isOverrideDefaultOnly(ov))
}

func TestIsOverrideDefaultOnlyRejectsComponentMode(t *testing.T) {
	ov := &override.Override{Components: []override.ComponentOverride{{ID: "demo/hello", Mode: "debug"}}}
	assert.False(t, isOverrideDefaultOnly(ov))
}

func TestIsOverrideDefaultOnlyRejectsNestedMemberLocalPort(t *testing.T) {
	ov := &override.Override{Components: []override.ComponentOverride{
		{ID: "erp/backend", Members: []override.MemberOverride{{ID: "people/basic", LocalPort: 9001}}},
	}}
	assert.False(t, isOverrideDefaultOnly(ov))
}

// 新组件要出现在 override.yaml 里——它是穷举式的（AGENTS.md §7.1），新组件没有一行，
// 使用者会以为它没有覆盖开关，而不是"从来没人给它补线"。这份 override.yaml 只有裸
// id 默认行，没有真实覆盖，所以 add 该直接静默重新生成。
func TestAddRegeneratesDefaultOnlyOverrideForNewComponent(t *testing.T) {
	comps := []comp{
		{ID: "demo/hello", Version: "1.0.0"},
		{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
	}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
`)

	r := runIn(t, f.Dir, "add", "demo/caller@1.0.0", "--yes")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "- id: demo/caller")
}

// override.yaml 里有真实覆盖时，add 一个字节都不碰——静默重新生成有真的丢失自定义值的
// 风险（设计书 §9 自己的原话）。只打印提醒，把决定权留给使用者。
func TestAddDoesNotTouchOverrideWithRealCustomizations(t *testing.T) {
	comps := []comp{
		{ID: "demo/hello", Version: "1.0.0"},
		{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
	}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: debug
    localPort: 9001
`)

	r := runIn(t, f.Dir, "add", "demo/caller@1.0.0", "--yes")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "override.yaml")
	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	text := string(data)
	assert.NotContains(t, text, "demo/caller", "有真实覆盖时不该自动改写")
	assert.Contains(t, text, "mode: debug", "已有的覆盖必须原样保留")
}

// add --local 走的是 add_local.go 里完全不同的一份写配置代码，必须独立确认它也接上了
// 同一个同步逻辑——这不是重新测 syncOverrideAfterAdd 本身（上面两条测试已经测过），
// 是测 runAddLocal 真的调用了它。
func TestAddLocalRegeneratesDefaultOnlyOverride(t *testing.T) {
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps)
	f.writeConfig(t, `components: []
`)
	f.writeOverride(t, `components: []
`)

	r := runIn(t, f.Dir, "add", "--local", "--yes")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "- id: demo/hello")
}

// override.yaml 完全不存在时，add 什么都不该做——它是可选机制，"没有覆盖"本身就是
// 完全合法、最常见的状态，不该因为 add 就凭空生出一份文件。
func TestAddDoesNotCreateOverrideWhenNoneExists(t *testing.T) {
	comps := []comp{
		{ID: "demo/hello", Version: "1.0.0"},
	}
	f := addedProject(t, comps)

	r := runIn(t, f.Dir, "add", "demo/hello@1.0.0", "--yes")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NoFileExists(t, filepath.Join(f.Dir, "override.yaml"))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run 'TestIsOverrideDefaultOnly|TestAddRegeneratesDefaultOnlyOverrideForNewComponent|TestAddDoesNotTouchOverrideWithRealCustomizations|TestAddLocalRegeneratesDefaultOnlyOverride|TestAddDoesNotCreateOverrideWhenNoneExists' -v`

Expected: every `TestIsOverrideDefaultOnly*` test fails with `undefined: isOverrideDefaultOnly`; every
`TestAdd*` test fails to compile for the same reason (the test file won't build until the function
exists) — this is one combined compile failure, not five separate runtime failures. That's still a
genuine RED: the package does not build, which is the correct failure mode before Step 3.

- [ ] **Step 3: Write the implementation**

Create `internal/cli/add_override.go`:

```go
package cli

// 本文件让 brickkit add 与 brickkit add --local 在真的往 brickkit.yaml 里写了新组件
// 之后，把 override.yaml 也带上（override.yaml 设计书 §9）。override.yaml 是穷举式的
// （AGENTS.md §7.1：项目当前每个组件都该有一行），新组件不出现在里面，使用者会以为
// 它没有覆盖开关，其实只是从来没人给它补线。

import (
	"os"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/override"
)

// syncOverrideAfterAdd 在 add 成功写入新组件之后调用。
//
// override.yaml 不存在：什么都不做——这份机制完全可选，没有它是最常见、完全合法的
// 状态，add 不该替使用者决定要不要开始用它。
// override.yaml 存在、只有裸 id 默认行：直接重新生成，安全——没有真实覆盖可丢，
// 重新生成天然会让外壳嵌套跟着 brickkit.yaml 最新的 servedBy 关系对上，不需要手写
// "往哪一行插一条 id"的补丁逻辑（复用 generateOverride，跟 brickkit override 命令
// 自己刷新时完全同一条路径）。
// override.yaml 存在且有真实覆盖：一个字节都不碰，只打印提醒——静默重新生成有真的
// 丢失自定义值的风险（设计书 §9 自己的原话："couldn't safely handle the new
// component being a shell member without risking clobbering the user's own
// customizations"）。
func syncOverrideAfterAdd(opts *Options, layout config.Layout) error {
	existing, err := override.ParseOverrideFile(layout.OverridePath())
	if err != nil {
		return err
	}
	if existing == nil {
		return nil
	}

	if !isOverrideDefaultOnly(existing) {
		opts.Printf("%s\n", i18n.T(msgid.CliAddOverrideYamlNeedsUpdating))
		return nil
	}

	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return err
	}

	fresh := generateOverride(cfg, existing)
	data, err := renderOverride(fresh)
	if err != nil {
		return err
	}
	if err := os.WriteFile(layout.OverridePath(), data, 0o644); err != nil {
		return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliOverrideWriteFailed)).
			WithDetail(i18n.T(msgid.LabelPath), layout.OverridePath()).WithCause(err)
	}
	opts.Printf("%s\n", i18n.T(msgid.CliAddOverrideYamlRefreshed))
	return nil
}

// isOverrideDefaultOnly 判断这份 override.yaml 除了穷举式的裸 id 行之外，有没有任何
// 真实覆盖——target、或者任何一个组件/成员的 mode、localPort、baseline。
func isOverrideDefaultOnly(ov *override.Override) bool {
	if ov == nil {
		return true
	}
	if ov.Target != "" || ov.TargetBaseline != "" {
		return false
	}
	for _, c := range ov.Components {
		if c.Mode != "" || c.LocalPort != 0 || c.Baseline != "" {
			return false
		}
		for _, m := range c.Members {
			if m.Mode != "" || m.LocalPort != 0 || m.Baseline != "" {
				return false
			}
		}
	}
	return true
}
```

Add to `internal/msgid/cli_add.go`, inside the existing `const (...)` block, right before the closing
`)`:

```go
	CliAddOverrideYamlNeedsUpdating = "cli.add.override_yaml_needs_updating"
	CliAddOverrideYamlRefreshed     = "cli.add.override_yaml_refreshed"
```

Add to `internal/i18n/catalog_en.go`'s message map (anywhere inside the existing map literal — Go map
literals don't require ordering, but keep it near the other `CliAdd*` entries for readability):

```go
	msgid.CliAddOverrideYamlNeedsUpdating: "⚠️ override.yaml has real overrides in it and wasn't updated for the new component — back it up, then run `brickkit override` to refresh it (your existing overrides are preserved)",
	msgid.CliAddOverrideYamlRefreshed:     "📝 override.yaml refreshed to include the new component",
```

Add to `internal/i18n/catalog_zh.go`'s message map:

```go
	msgid.CliAddOverrideYamlNeedsUpdating: "⚠️ override.yaml 里有真实覆盖，没有跟着这次新增的组件更新——先备份一份，再跑 `brickkit override` 刷新（已有的覆盖会保留）",
	msgid.CliAddOverrideYamlRefreshed:     "📝 override.yaml 已刷新，补上了新增的组件",
```

Modify `internal/cli/add.go` — find this exact block (currently at lines 204-208):

```go
	artifacts := downloadArtifacts(ctx, client, graph)
	added, err := writeComponents(layout, graph)
	if err != nil {
		return err
	}
```

Change it to:

```go
	artifacts := downloadArtifacts(ctx, client, graph)
	added, err := writeComponents(layout, graph)
	if err != nil {
		return err
	}
	if len(added) > 0 {
		if err := syncOverrideAfterAdd(opts, layout); err != nil {
			return err
		}
	}
```

Modify `internal/cli/add_local.go` — find this exact block (currently at lines 89-93):

```go
	artifacts := downloadLocalArtifacts(ctx, client, graphs)
	added, err := writeLocalComponents(layout, graphs)
	if err != nil {
		return err
	}
```

Change it to:

```go
	artifacts := downloadLocalArtifacts(ctx, client, graphs)
	added, err := writeLocalComponents(layout, graphs)
	if err != nil {
		return err
	}
	if len(added) > 0 {
		if err := syncOverrideAfterAdd(opts, layout); err != nil {
			return err
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run 'TestIsOverrideDefaultOnly|TestAddRegeneratesDefaultOnlyOverrideForNewComponent|TestAddDoesNotTouchOverrideWithRealCustomizations|TestAddLocalRegeneratesDefaultOnlyOverride|TestAddDoesNotCreateOverrideWhenNoneExists' -v`

Expected: `PASS` for all nine tests (`TestIsOverrideDefaultOnly*` ×5, `TestAdd*` ×4).

- [ ] **Step 5: Run the i18n guard and the full add/add_local suites**

Run: `go test ./internal/cli/... ./tests/i18nguard/... -run 'TestAdd|TestIsOverrideDefaultOnly'`

Then run the whole package to catch any regression: `go test ./internal/cli/...`

Expected: all green, no regressions in `add_test.go`/`add_local_test.go`'s existing tests (they never
write `override.yaml`, so `syncOverrideAfterAdd` should be a no-op for every one of them — confirms the
"file absent → do nothing" branch doesn't accidentally create files for tests that never asked for one).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/add_override.go internal/cli/add_override_test.go internal/cli/add.go internal/cli/add_local.go internal/msgid/cli_add.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "feat: brickkit add keeps override.yaml in sync with new components"
```

---

## Task 2: `brickkit remove` cleans up `override.yaml`

**Files:**
- Create: `internal/cli/remove_override.go`
- Create: `internal/cli/remove_override_test.go`
- Modify: `internal/cli/remove.go:104-112` (call the new helper after the component's last version is
  removed and saved)
- Modify: `internal/msgid/cli_remove.go` (one new message ID)
- Modify: `internal/i18n/catalog_en.go`, `internal/i18n/catalog_zh.go`

**Interfaces:**
- Consumes: the same `internal/override` types Task 1 consumes, plus `renderOverride` (unexported,
  `internal/cli/override.go`).
- Produces: `removeFromOverride(layout config.Layout, id string) (changed bool, err error)` — called by
  `remove.go` right after the component's config edit is saved, only when it was the component's *last*
  remaining version.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/remove_override_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/override"
)

func TestRemoveComponentFromOverrideDeletesTopLevelEntry(t *testing.T) {
	entries := []override.ComponentOverride{{ID: "demo/hello", Mode: "debug"}, {ID: "demo/caller"}}

	changed, out := removeComponentFromOverride(entries, "demo/hello")

	assert.True(t, changed)
	require.Len(t, out, 1)
	assert.Equal(t, "demo/caller", out[0].ID)
}

func TestRemoveComponentFromOverrideDeletesNestedMember(t *testing.T) {
	entries := []override.ComponentOverride{
		{ID: "erp/backend", Members: []override.MemberOverride{
			{ID: "people/basic", Mode: "disable"},
			{ID: "auth/rbac"},
		}},
	}

	changed, out := removeComponentFromOverride(entries, "people/basic")

	assert.True(t, changed)
	require.Len(t, out, 1)
	require.Len(t, out[0].Members, 1)
	assert.Equal(t, "auth/rbac", out[0].Members[0].ID)
}

func TestRemoveComponentFromOverridePromotesRemainingMembers(t *testing.T) {
	entries := []override.ComponentOverride{
		{ID: "erp/backend", Members: []override.MemberOverride{
			{ID: "people/basic", Mode: "disable", LocalPort: 9001, Baseline: "local"},
		}},
	}

	changed, out := removeComponentFromOverride(entries, "erp/backend")

	assert.True(t, changed)
	require.Len(t, out, 1)
	promoted := out[0]
	assert.Equal(t, "people/basic", promoted.ID)
	assert.Equal(t, "disable", promoted.Mode, "被提升的成员要带着自己原来的覆盖值")
	assert.Equal(t, 9001, promoted.LocalPort)
	assert.Equal(t, "local", promoted.Baseline)
	assert.Empty(t, promoted.Members)
}

func TestRemoveComponentFromOverrideNoOpWhenIDNotPresent(t *testing.T) {
	entries := []override.ComponentOverride{{ID: "demo/hello"}}

	changed, out := removeComponentFromOverride(entries, "demo/gone")

	assert.False(t, changed)
	assert.Equal(t, entries, out)
}

func TestRemoveDeletesLineFromOverrideYaml(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: debug
    localPort: 9001
`)

	r := runIn(t, f.Dir, "remove", "demo/hello")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "override.yaml")
	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "demo/hello")
}

// 多版本共存：只删一个版本，这个 ID 在 brickkit.yaml 里还在，override.yaml 的条目
// 不该动——它按 ID 索引，跟版本无关（AGENTS.md §7.1）。
func TestRemoveKeepsOverrideLineWhenOtherVersionRemains(t *testing.T) {
	f := addedProject(t, []comp{
		{ID: "demo/hello", Version: "1.0.0"},
		{ID: "demo/hello", Version: "2.0.0"},
	}, "demo/hello@1.0.0", "demo/hello@2.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
  - id: demo/hello
    version: 2.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: local
`)

	r := runIn(t, f.Dir, "remove", "demo/hello@1.0.0")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "- id: demo/hello")
	assert.Contains(t, string(data), "mode: local")
}

// 删掉的是外壳：override.yaml 里嵌在它下面的成员不会被一并删掉（它们本来就还在
// brickkit.yaml 里），而是被提升成顶层条目——注意这里 brickkit.yaml 本身没有声明
// servedBy：remove 只看 override.yaml 自己的嵌套结构，不需要（也不该）反过去确认
// brickkit.yaml 当时是不是真的这样声明过 servedBy——那是漂移检测的事，不是 remove
// 的事，两者刻意分开，见 Global Constraints。
func TestRemoveShellPromotesMembersToTopLevel(t *testing.T) {
	f := addedProject(t, []comp{
		{ID: "infra/shell-go-core", Version: "1.0.0"},
		{ID: "mdm/customer", Version: "1.0.0"},
	}, "infra/shell-go-core@1.0.0", "mdm/customer@1.0.0")
	f.writeConfig(t, `components:
  - id: infra/shell-go-core
    version: 1.0.0
  - id: mdm/customer
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: infra/shell-go-core
    members:
      - id: mdm/customer
        mode: disable
`)

	r := runIn(t, f.Dir, "remove", "infra/shell-go-core")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	text := string(data)
	assert.NotContains(t, text, "infra/shell-go-core")
	assert.Contains(t, text, "- id: mdm/customer")
	assert.Contains(t, text, "mode: disable", "成员自己的覆盖值要原样带过去")
	assert.NotContains(t, text, "members:", "提升之后不该再嵌套在任何外壳下面")
}

func TestRemoveSkipsOverrideCleanupWhenFileAbsent(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)

	r := runIn(t, f.Dir, "remove", "demo/hello")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "override.yaml")
	assert.NoFileExists(t, filepath.Join(f.Dir, "override.yaml"))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run 'TestRemoveComponentFromOverride|TestRemoveDeletesLineFromOverrideYaml|TestRemoveKeepsOverrideLineWhenOtherVersionRemains|TestRemoveShellPromotesMembersToTopLevel|TestRemoveSkipsOverrideCleanupWhenFileAbsent' -v`

Expected: compile failure — `undefined: removeComponentFromOverride` (the four pure-function tests) and
the package won't build until it exists, so every test in the run fails for that one reason.

- [ ] **Step 3: Write the implementation**

Create `internal/cli/remove_override.go`:

```go
package cli

// 本文件让 brickkit remove 在真的把一个组件 ID 的最后一个版本从 brickkit.yaml 里删掉
// 之后，把 override.yaml 也带上（override.yaml 设计书 §9）。留着一条指向不存在组件的
// 条目，下一次任何命令读 override.yaml 都会被 CheckAgainst 的悬空引用检查拦下
// （AGENTS.md §7.1）——remove 自己造成的问题不该留给下一条命令去发现。
//
// 这里做的是最小的、定向的编辑，不像 add 那样整个重新生成：override.yaml 里可能还有
// 别的组件带着真实自定义值，一次 remove 不该有牵连它们的风险（设计书 §9 把这条跟 add
// 的整体重生成分开论证过）。

import (
	"os"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/override"
)

// removeFromOverride 在 id 的最后一个版本被移出 brickkit.yaml 之后调用。返回 changed
// 表示文件是否被真的改写过（remove.go 用它决定要不要打印一句确认）。
func removeFromOverride(layout config.Layout, id string) (bool, error) {
	ov, err := override.ParseOverrideFile(layout.OverridePath())
	if err != nil {
		return false, err
	}
	if ov == nil {
		return false, nil
	}

	changed, next := removeComponentFromOverride(ov.Components, id)
	if !changed {
		return false, nil
	}
	ov.Components = next

	data, err := renderOverride(ov)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(layout.OverridePath(), data, 0o644); err != nil {
		return false, clierr.New(clierr.CodeInternal, i18n.T(msgid.CliOverrideWriteFailed)).
			WithDetail(i18n.T(msgid.LabelPath), layout.OverridePath()).WithCause(err)
	}
	return true, nil
}

// removeComponentFromOverride 从顶层条目列表里删掉 id 对应的那一条（若有），外壳类
// 条目的成员原样提升成顶层；也会到每个外壳的 Members 里找一遍，删掉嵌套的那份。
// id 到底嵌在哪一层，remove 自己不需要关心 brickkit.yaml 当时是怎么声明 servedBy
// 的——override.yaml 里现在长什么样才是唯一要看的事实。
func removeComponentFromOverride(
	entries []override.ComponentOverride, id string,
) (bool, []override.ComponentOverride) {
	changed := false
	out := make([]override.ComponentOverride, 0, len(entries))
	for _, c := range entries {
		if c.ID == id {
			changed = true
			for _, m := range c.Members {
				out = append(out, promoteMember(m))
			}
			continue
		}
		var kept []override.MemberOverride
		for _, m := range c.Members {
			if m.ID == id {
				changed = true
				continue
			}
			kept = append(kept, m)
		}
		c.Members = kept
		out = append(out, c)
	}
	return changed, out
}

// promoteMember 把一个嵌套成员条目转成顶层条目——两个类型字段完全一样，只是
// ComponentOverride 多一个 Members（schemagen 的反射生成器不支持自引用递归类型，
// 这是它们从一开始就是两个类型的原因，见 internal/override/override.go）。
func promoteMember(m override.MemberOverride) override.ComponentOverride {
	return override.ComponentOverride{ID: m.ID, Mode: m.Mode, LocalPort: m.LocalPort, Baseline: m.Baseline}
}
```

Add to `internal/msgid/cli_remove.go`, inside the existing `const (...)` block:

```go
	CliRemoveOverrideYamlUpdated = "cli.remove.override_yaml_updated"
```

Add to `internal/i18n/catalog_en.go`:

```go
	msgid.CliRemoveOverrideYamlUpdated: "📝 override.yaml updated (removed the entry for this component)",
```

Add to `internal/i18n/catalog_zh.go`:

```go
	msgid.CliRemoveOverrideYamlUpdated: "📝 override.yaml 已更新（删掉了这个组件的条目）",
```

Modify `internal/cli/remove.go` — find this exact block (currently at lines 104-112):

```go
	// 组件的最后一个版本也走了，指着它的资源绑定就是一条谁也用不上的配置。
	// 多版本共存时不动：绑定按组件 ID 记（003 §5.3），剩下的版本还要用它。
	var unbound []string
	if !edit.HasComponentID(target.ID) {
		unbound = edit.RemoveBindings(target.ID)
	}
	if err := edit.Save(); err != nil {
		return err
	}
```

Change it to:

```go
	// 组件的最后一个版本也走了，指着它的资源绑定就是一条谁也用不上的配置，
	// override.yaml 里指着它的条目也是——两者共用同一个判据：多版本共存时不动
	// （绑定按组件 ID 记，003 §5.3；override.yaml 条目同样按 ID 不按版本，
	// AGENTS.md §7.1），剩下的版本还要用它们。
	lastVersion := !edit.HasComponentID(target.ID)
	var unbound []string
	if lastVersion {
		unbound = edit.RemoveBindings(target.ID)
	}
	if err := edit.Save(); err != nil {
		return err
	}

	var overrideUpdated bool
	if lastVersion {
		overrideUpdated, err = removeFromOverride(layout, target.ID)
		if err != nil {
			return err
		}
	}
```

Then find the output-rendering block right after `cleanup, err := cleanupComponent(...)` (currently at
lines 114-137):

```go
	opts.Printf("%s\n", i18n.T(msgid.CliRemoveRemoved, target))
	if len(unbound) > 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliRemoveResourceBindingsDropped, strings.Join(unbound, i18n.T(msgid.ListSeparator))))
	}
```

Change it to add the new line right after the existing `unbound` line:

```go
	opts.Printf("%s\n", i18n.T(msgid.CliRemoveRemoved, target))
	if len(unbound) > 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliRemoveResourceBindingsDropped, strings.Join(unbound, i18n.T(msgid.ListSeparator))))
	}
	if overrideUpdated {
		opts.Printf("%s\n", i18n.T(msgid.CliRemoveOverrideYamlUpdated))
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run 'TestRemoveComponentFromOverride|TestRemoveDeletesLineFromOverrideYaml|TestRemoveKeepsOverrideLineWhenOtherVersionRemains|TestRemoveShellPromotesMembersToTopLevel|TestRemoveSkipsOverrideCleanupWhenFileAbsent' -v`

Expected: `PASS` for all eight tests.

- [ ] **Step 5: Run the full remove suite plus i18n guard**

Run: `go test ./internal/cli/... ./tests/i18nguard/... -run 'TestRemove'`

Then: `go test ./internal/cli/...`

Expected: all green, no regressions in `remove_test.go`'s existing tests (none of them write
`override.yaml`, so `removeFromOverride` should be a no-op for all of them).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/remove_override.go internal/cli/remove_override_test.go internal/cli/remove.go internal/msgid/cli_remove.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "feat: brickkit remove cleans up the corresponding override.yaml entry"
```

---

## Task 3: `brickkit up` prints a non-blocking drift note

**Files:**
- Modify: `internal/cli/up.go:1-29` (add `internal/override` import), `internal/cli/up.go:185-197`
  (print drift notes between `loadOverride` and `applyOverride`)
- Modify: `internal/cli/up_dryrun_test.go` or a new `internal/cli/up_override_drift_test.go` (create the
  latter — keeps this task's tests in their own file rather than growing an already-large existing one)

**Interfaces:**
- Consumes: `override.Drift(cfg *config.Config, ov *override.Override) []override.DriftNote` (already
  defined by Plan 2a, `internal/override/check.go`); `DriftNote{Field, Message string}`; the existing
  `msgid.CliOverrideDriftNote` (already defined and already used once, by `internal/cli/override.go`'s
  `runOverride` — this task's print statement must use the exact same message ID, so the wording is
  identical whether the note comes from `brickkit override` or from `brickkit up`).
- Produces: nothing new for later tasks — this is a leaf addition.

**Why the note must be printed *before* `applyOverride` runs:** `Drift` compares `override.yaml`'s
recorded `baseline`/`targetBaseline` against `brickkit.yaml`'s **current** value. `applyOverride`
mutates `cfg` in place, overwriting `cfg.Deploy.Target`/`cfg.Components[i].Mode` with the override's own
values. Calling `Drift` *after* `applyOverride` would compare the override's baseline against the
override's own already-applied value — always a match, silently disabling the entire check. `loadOverride`
already calls `override.CheckAgainst(cfg, ov)` before returning, for exactly this reason (see
`internal/cli/topology.go`) — this task follows the same ordering.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/up_override_drift_test.go`:

```go
package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// brickkit.yaml 的 mode 从 override.yaml 上次确认时记的 baseline 变了——drift 提示
// 该出现，但不阻断 up（设计书 §7：这是警告级的、非阻断的提示）。
func TestUpPrintsDriftNoteWhenComponentBaselineIsStale(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: debug
    baseline: enabled
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "Drift: demo/hello")
}

func TestUpPrintsDriftNoteWhenTargetBaselineIsStale(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `target: docker
targetBaseline: k8s
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "Drift: target")
}

func TestUpPrintsNoDriftNoteWhenBaselineMatches(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "Drift:")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run 'TestUpPrintsDriftNote|TestUpPrintsNoDriftNote' -v`

Expected: `TestUpPrintsDriftNoteWhenComponentBaselineIsStale` and
`TestUpPrintsDriftNoteWhenTargetBaselineIsStale` FAIL (stdout doesn't contain "Drift:" — nothing prints
it yet); `TestUpPrintsNoDriftNoteWhenBaselineMatches` PASSES already (there's nothing to print either way
— this one exists to prove Step 3 doesn't start printing drift notes unconditionally).

- [ ] **Step 3: Write the implementation**

Modify `internal/cli/up.go`'s import block — find:

```go
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/procsup"
```

Change to:

```go
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/override"
	"github.com/brickkit/brickkit/internal/procsup"
```

Modify `internal/cli/up.go`'s `buildUpPlan` — find this exact block (currently at lines 191-197):

```go
	ov, err := loadOverride(opts, layout, cfg)
	if err != nil {
		return nil, err
	}
	if err := applyOverride(cfg, ov); err != nil {
		return nil, err
	}
```

Change it to:

```go
	ov, err := loadOverride(opts, layout, cfg)
	if err != nil {
		return nil, err
	}
	for _, note := range override.Drift(cfg, ov) {
		opts.Printf("%s\n", i18n.T(msgid.CliOverrideDriftNote, note.Field, note.Message))
	}
	if err := applyOverride(cfg, ov); err != nil {
		return nil, err
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run 'TestUpPrintsDriftNote|TestUpPrintsNoDriftNote' -v`

Expected: `PASS` for all three tests.

- [ ] **Step 5: Run the full up suite plus i18n guard**

Run: `go test ./internal/cli/... ./tests/i18nguard/...`

Expected: all green — in particular, every existing test that writes an `override.yaml` without a
`baseline`/`targetBaseline` field (all of Plan 2a's own override-application tests) must still show no
drift output, since `Drift` only fires when a baseline is recorded and mismatches.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/up.go internal/cli/up_override_drift_test.go
git commit -m "feat: brickkit up prints a non-blocking override.yaml drift note"
```

---

## Task 4: `brickkit lint` gains the two staleness checks

**Files:**
- Modify: `internal/cli/lint.go:16-30` (add `override` import), `internal/cli/lint.go:87-127`
  (`lintProject` — declare `files` earlier, insert the new `override.yaml` check)
- Create: `internal/cli/lint_override_test.go`

**Interfaces:**
- Consumes: `override.ParseOverrideFile`, `override.CheckAgainst`, `override.Drift` (all Plan 2a),
  `clierr.As(err error) *clierr.Error`, `clierr.New(code, message).WithDetail(...)`.
- Produces: `lintOverride(opts *Options, layout config.Layout, cfg *config.Config) *lintFile` — returns
  `nil` when `override.yaml` doesn't exist (caller must check for `nil` and skip appending).

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/lint_override_test.go`:

```go
package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// 悬空条目——override.yaml 指着一个 brickkit.yaml 里已经不存在的组件——是错误，
// 不需要 --strict 也该失败：它跟 up/sync/status/down/brickkit override 走的是
// 同一条 CheckAgainst 校验，lint 不该是唯一放行它的地方。
func TestLintCatchesDanglingOverrideEntry(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/gone
`)

	r := runIn(t, f.Dir, "lint")

	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "override.yaml")
	assert.Contains(t, r.stdout, "demo/gone")
}

// baseline 漂移是警告，不阻断——设计书 §7 明说"a warning, not an error"，跟
// lintManifest 里拼错的 configSchema 键同一个待遇。
func TestLintWarnsOnStaleBaselineWithoutFailing(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: debug
    baseline: enabled
`)

	r := runIn(t, f.Dir, "lint")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "Drift: demo/hello")
}

func TestLintStrictFailsOnStaleBaseline(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: debug
    baseline: enabled
`)

	r := runIn(t, f.Dir, "lint", "--strict")

	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
}

func TestLintSkipsOverrideCheckWhenFileAbsent(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)

	r := runIn(t, f.Dir, "lint")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "override.yaml")
}

func TestLintPassesOnCleanOverride(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
`)

	r := runIn(t, f.Dir, "lint")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "override.yaml")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run 'TestLintCatchesDanglingOverrideEntry|TestLintWarnsOnStaleBaselineWithoutFailing|TestLintStrictFailsOnStaleBaseline|TestLintSkipsOverrideCheckWhenFileAbsent|TestLintPassesOnCleanOverride' -v`

Expected: `TestLintCatchesDanglingOverrideEntry` FAILS (exit code is `ExitOK`, not `ExitError` — lint
today never reads `override.yaml` at all, so a dangling entry goes unnoticed).
`TestLintWarnsOnStaleBaselineWithoutFailing` FAILS (stdout doesn't contain "Drift:").
`TestLintStrictFailsOnStaleBaseline` FAILS (exit code is `ExitOK`).
`TestLintSkipsOverrideCheckWhenFileAbsent` PASSES already (nothing to skip yet).
`TestLintPassesOnCleanOverride` FAILS (stdout doesn't contain "override.yaml" — lint doesn't print a
line for it at all today).

- [ ] **Step 3: Write the implementation**

Modify `internal/cli/lint.go`'s import block — find:

```go
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/skills"
```

Change to:

```go
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/override"
	"github.com/brickkit/brickkit/internal/skills"
```

Modify `internal/cli/lint.go`'s `lintProject` function. Find this exact block (currently the whole
function body):

```go
func lintProject(opts *Options, layout config.Layout) ([]lintFile, []string) {
	head := lintFile{path: displayPath(opts.WorkDir, layout.ConfigPath())}

	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		head.errors = append(head.errors, clierr.As(err))
		return []lintFile{head}, []string{
			i18n.T(msgid.CliLintBrickkitYamlDidNotPass, manifest.FileName),
		}
	}

	// 直接 source.New，不走 newSourceClient：后者会先去读 installer.publicKeys 指向的公钥文件，
	// 而公钥缺失是 up / add 该报的事，不该让一条"离线校验 YAML"的命令因此失败。
	// source.New 本身不联网——三种安装源都是惰性的，只有真去取 Manifest 才会碰网络，lint 从不取。
	//
	// 这是整个 CLI 里唯一不经 newSourceClient（也就是不带签名策略）的取源客户端。这个例外
	// **只安全在** lint 只调 LocalManifestFiles、从不取 Manifest：将来谁在这里加一次
	// client.Manifest，就等于悄悄绕过验签，还会写 .brickkit/manifests/ 缓存、可能联网，
	// "纯只读、不联网"的承诺当场破功。
	client, err := source.New(layout, cfg, source.Options{})
	if err != nil {
		head.errors = append(head.errors, clierr.As(err))
		return []lintFile{head}, []string{localSkippedNote()}
	}
	defer func() { _ = client.Close() }()

	found, err := client.LocalManifestFiles()
	if err != nil {
		// 本地源的根目录不存在之类：那是 brickkit.yaml 里 sources[].path 配错了。
		// 枚举遇到第一个出错的源就整体失败，别的本地源里的组件因此一份也没查——汇总里的
		// 文件数会被低估，必须说出来，否则使用者改好 path 之前不知道还有文件没被检查
		head.errors = append(head.errors, clierr.As(err))
		return []lintFile{head}, []string{localSkippedNote()}
	}

	files := []lintFile{head}
	for _, f := range found {
		files = append(files, lintManifest(opts, f.Path, f.ID))
	}
	return files, nil
}
```

Change it to (the only changes: `files` is declared right after `cfg` parses successfully instead of
just before the manifest loop, the `override.yaml` check is inserted there, and every `return
[]lintFile{head}, ...` on the two later error paths becomes `return files, ...` — those two paths run
*before* the override check would ever be reached, so `files` still equals `[]lintFile{head}` at that
point; this is a pure rename, not a behavior change, and keeps a single source of truth for "the list of
files so far" instead of two):

```go
func lintProject(opts *Options, layout config.Layout) ([]lintFile, []string) {
	head := lintFile{path: displayPath(opts.WorkDir, layout.ConfigPath())}

	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		head.errors = append(head.errors, clierr.As(err))
		return []lintFile{head}, []string{
			i18n.T(msgid.CliLintBrickkitYamlDidNotPass, manifest.FileName),
		}
	}

	files := []lintFile{head}
	if ovFile := lintOverride(opts, layout, cfg); ovFile != nil {
		files = append(files, *ovFile)
	}

	// 直接 source.New，不走 newSourceClient：后者会先去读 installer.publicKeys 指向的公钥文件，
	// 而公钥缺失是 up / add 该报的事，不该让一条"离线校验 YAML"的命令因此失败。
	// source.New 本身不联网——三种安装源都是惰性的，只有真去取 Manifest 才会碰网络，lint 从不取。
	//
	// 这是整个 CLI 里唯一不经 newSourceClient（也就是不带签名策略）的取源客户端。这个例外
	// **只安全在** lint 只调 LocalManifestFiles、从不取 Manifest：将来谁在这里加一次
	// client.Manifest，就等于悄悄绕过验签，还会写 .brickkit/manifests/ 缓存、可能联网，
	// "纯只读、不联网"的承诺当场破功。
	client, err := source.New(layout, cfg, source.Options{})
	if err != nil {
		files = append(files, lintFile{path: head.path, errors: []*clierr.Error{clierr.As(err)}})
		return files, []string{localSkippedNote()}
	}
	defer func() { _ = client.Close() }()

	found, err := client.LocalManifestFiles()
	if err != nil {
		// 本地源的根目录不存在之类：那是 brickkit.yaml 里 sources[].path 配错了。
		// 枚举遇到第一个出错的源就整体失败，别的本地源里的组件因此一份也没查——汇总里的
		// 文件数会被低估，必须说出来，否则使用者改好 path 之前不知道还有文件没被检查
		head.errors = append(head.errors, clierr.As(err))
		return files, []string{localSkippedNote()}
	}

	for _, f := range found {
		files = append(files, lintManifest(opts, f.Path, f.ID))
	}
	return files, nil
}

// lintOverride 检查 override.yaml——文件不存在时完全合法，什么也不查（override.yaml
// 是可选机制）。检查设计书 §7 的两类过期性：悬空引用（错误，跟 up/sync/status/down/
// brickkit override 用的是同一个 CheckAgainst）与 baseline 漂移（警告，不阻断，
// --strict 才会让它计入失败——待遇跟 lintManifest 里 configSchema 拼写警告完全一样，
// 见 reportLint 里 warned 的计数方式）。
func lintOverride(opts *Options, layout config.Layout, cfg *config.Config) *lintFile {
	if _, err := os.Stat(layout.OverridePath()); err != nil {
		return nil
	}

	f := &lintFile{path: displayPath(opts.WorkDir, layout.OverridePath())}

	ov, err := override.ParseOverrideFile(layout.OverridePath())
	if err != nil {
		f.errors = append(f.errors, clierr.As(err))
		return f
	}
	if ov == nil {
		return f
	}

	if err := override.CheckAgainst(cfg, ov); err != nil {
		f.errors = append(f.errors, clierr.As(err))
	}
	for _, note := range override.Drift(cfg, ov) {
		f.warnings = append(f.warnings, clierr.New(clierr.CodeConfigInvalid,
			i18n.T(msgid.CliOverrideDriftNote, note.Field, note.Message)).
			WithDetail(i18n.T(msgid.LabelFile), f.path))
	}
	return f
}
```

**Important:** there's a pre-existing bug you'll notice in the `client, err := source.New(...)` error
path above — the original code did `head.errors = append(head.errors, clierr.As(err))` and then
`return []lintFile{head}, ...`, discarding whatever was already appended to `files` (in the original
code that was only ever `head` itself, so it didn't matter; now that `files` can also contain the
`override.yaml` entry, discarding it would be a real regression, silently dropping the override.yaml
lint result whenever the source client fails to construct). The rewrite above fixes this by building a
fresh `lintFile{path: head.path, errors: ...}` and appending it to the existing `files` slice instead of
discarding it — this is not optional polish, it's required for `TestLintSkipsOverrideCheckWhenFileAbsent`
and friends to keep passing once a `sources[].path` error is also present (no test in this task exercises
that specific combination, but do not reintroduce the discard — it would silently break the invariant
"every file lint looked at appears in the report").

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run 'TestLintCatchesDanglingOverrideEntry|TestLintWarnsOnStaleBaselineWithoutFailing|TestLintStrictFailsOnStaleBaseline|TestLintSkipsOverrideCheckWhenFileAbsent|TestLintPassesOnCleanOverride' -v`

Expected: `PASS` for all five tests.

- [ ] **Step 5: Run the full lint suite**

Run: `go test ./internal/cli/... -run TestLint`

Then: `go test ./internal/cli/...`

Expected: all green, no regressions in the existing `lint_test.go` suite (standalone-component-repo mode
never calls `lintProject`/`lintOverride` at all — confirm no existing standalone-repo test starts
failing because it now expects an `override.yaml` line; it shouldn't, since `scope ==
skills.ScopeComponent` skips `lintProject` entirely).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/lint.go internal/cli/lint_override_test.go
git commit -m "feat: brickkit lint checks override.yaml for dangling entries and stale baselines"
```

---

## Task 5: `brickkit restore` suggests re-running `brickkit override`

**Files:**
- Modify: `internal/cli/restore.go:1-17` (add `os` import if not already present — it is not currently
  imported by this file), `internal/cli/restore.go:109-160` (`runRestore` — call the new hint after
  `applyWorkspacePlan` succeeds)
- Modify: `internal/msgid/cli_restore.go` (one new message ID)
- Modify: `internal/i18n/catalog_en.go`, `internal/i18n/catalog_zh.go`
- Modify: `internal/cli/restore_test.go` (add the two new tests to the existing file — this task's
  addition is small enough, and closely enough related to the existing restore tests' fixtures, that a
  separate file would just duplicate the `newSyncFixture`/`gitProject`/`gitDo` setup for no benefit)

**Interfaces:**
- Consumes: `os.Stat` (standard library — same pattern `loadOverride` already uses in
  `internal/cli/topology.go` to check `override.yaml`'s existence without needing to fully parse it just
  to decide whether to print a hint).
- Produces: nothing new for later tasks — leaf addition.

- [ ] **Step 1: Write the failing tests**

Add to `internal/cli/restore_test.go`, right after `TestRestoreRestoresModeAndMovesSourceBack` (so it
sits next to the test whose fixture it borrows):

```go
// restore 刚把 mode 还原到 HEAD 那份状态——override.yaml 记的 baseline 多半是刷新时
// brickkit.yaml 的旧状态，restore 一还原，那些 baseline 多半就过期了。不主动说，
// 使用者要等到下一次 brickkit override 或 lint 才会看到一堆漂移提示，却不知道
// 是这次 restore 造成的（设计书 §9："prints a hint afterward suggesting `brickkit
// override` be re-run"）。
func TestRestoreSuggestsRerunningOverrideWhenModeChanged(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	f.writeConfig(t, helloDisabled)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
	f.writeOverride(t, `components:
  - id: demo/hello
`)

	r := runIn(t, f.Dir, "restore")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "brickkit override")
}

// 没有 override.yaml：这条提示压根没意义，不该出现——override.yaml 是可选机制，
// 没用上它的项目不该被一句跟自己无关的提示打扰。
func TestRestoreSkipsOverrideHintWhenFileAbsent(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	f.writeConfig(t, helloDisabled)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)

	r := runIn(t, f.Dir, "restore")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "brickkit override")
}

// mode 一个都没变（工作区已经跟 HEAD 一致）：没有什么"过期"可言，同样不该提示。
func TestRestoreSkipsOverrideHintWhenNothingChanged(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")
	f.writeOverride(t, `components:
  - id: demo/hello
`)

	r := runIn(t, f.Dir, "restore")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "brickkit override")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run 'TestRestoreSuggestsRerunningOverrideWhenModeChanged|TestRestoreSkipsOverrideHintWhenFileAbsent|TestRestoreSkipsOverrideHintWhenNothingChanged' -v`

Expected: `TestRestoreSuggestsRerunningOverrideWhenModeChanged` FAILS (stdout doesn't contain "brickkit
override" — nothing prints it yet). The other two PASS already (nothing to print, correctly).

- [ ] **Step 3: Write the implementation**

Modify `internal/cli/restore.go`'s import block — find:

```go
import (
	"context"

	"github.com/spf13/cobra"
```

Change to:

```go
import (
	"context"
	"os"

	"github.com/spf13/cobra"
```

Modify `internal/cli/restore.go`'s `runRestore` — find this exact block (the function's final two
statements, currently at the end of the function):

```go
	// 被覆盖的旧值必须在落盘之前印出来：restore 不可逆，旧的 mode 没有
	// 第二份副本，如实汇报是唯一的缓解措施。这两句之间如果被杀掉进程
	// （OOM、SIGKILL、断电），旧值不能既从磁盘上没了、又从未被报告过。
	printModeChanges(opts, layout, changes, untouched)
	if err := writeMode(layout, changes); err != nil {
		return err
	}

	return applyWorkspacePlan(opts, layout, planSync(layout, work, f))
}
```

Change it to:

```go
	// 被覆盖的旧值必须在落盘之前印出来：restore 不可逆，旧的 mode 没有
	// 第二份副本，如实汇报是唯一的缓解措施。这两句之间如果被杀掉进程
	// （OOM、SIGKILL、断电），旧值不能既从磁盘上没了、又从未被报告过。
	printModeChanges(opts, layout, changes, untouched)
	if err := writeMode(layout, changes); err != nil {
		return err
	}

	if err := applyWorkspacePlan(opts, layout, planSync(layout, work, f)); err != nil {
		return err
	}
	suggestOverrideRefresh(opts, layout, changes)
	return nil
}

// suggestOverrideRefresh 在 restore 真的动过 mode 之后提醒一句：override.yaml 里的
// baseline 记的是 restore 之前那个 brickkit.yaml 状态，restore 一还原，那些 baseline
// 多半就过期了——不主动说，使用者要等到下一次 brickkit override 或 lint 才会看到
// 一堆"漂移"提示，却不知道是这次 restore 造成的。
func suggestOverrideRefresh(opts *Options, layout config.Layout, changes []modeChange) {
	if len(changes) == 0 {
		return
	}
	if _, err := os.Stat(layout.OverridePath()); err != nil {
		return
	}
	opts.Printf("%s\n", i18n.T(msgid.CliRestoreSuggestRerunningOverride))
}
```

Add to `internal/msgid/cli_restore.go`, inside the existing `const (...)` block:

```go
	CliRestoreSuggestRerunningOverride = "cli.restore.suggest_rerunning_override"
```

Add to `internal/i18n/catalog_en.go`:

```go
	msgid.CliRestoreSuggestRerunningOverride: "💡 mode changed — override.yaml's recorded baselines may now be stale. Run `brickkit override` to refresh them",
```

Add to `internal/i18n/catalog_zh.go`:

```go
	msgid.CliRestoreSuggestRerunningOverride: "💡 mode 变了——override.yaml 记的 baseline 可能已经过期，跑一次 `brickkit override` 刷新一下",
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run 'TestRestoreSuggestsRerunningOverrideWhenModeChanged|TestRestoreSkipsOverrideHintWhenFileAbsent|TestRestoreSkipsOverrideHintWhenNothingChanged' -v`

Expected: `PASS` for all three tests.

- [ ] **Step 5: Run the full restore suite plus i18n guard**

Run: `go test ./internal/cli/... -run TestRestore`

Then: `go test ./internal/cli/... ./tests/i18nguard/...`

Expected: all green, no regressions in the existing `restore_test.go` suite (none of them write
`override.yaml`, so the new hint should never fire for any of them).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/restore.go internal/cli/restore_test.go internal/msgid/cli_restore.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "feat: brickkit restore suggests refreshing override.yaml after a mode change"
```

---

## Task 6: `brickkit status` labels components disabled by `override.yaml`

**Files:**
- Modify: `internal/cli/lifecycle.go:39-52` (`project` struct — add a field),
  `internal/cli/lifecycle.go:80-100` (`loadProject` — populate it before `applyOverride` mutates `cfg`)
- Modify: `internal/cli/status.go:128-193` (`resolvedView`/`degradedView` — apply the label)
- Modify: `internal/msgid/cli_status.go` (one new message ID, in the "手工处理的条目" block)
- Modify: `internal/i18n/catalog_en.go`, `internal/i18n/catalog_zh.go`
- Modify: `internal/cli/status_test.go` (add the new tests next to `TestStatusShowsSkippedComponentsWithReason`)

**Interfaces:**
- Consumes: `flattenOverrideValues(entries []override.ComponentOverride) map[string]overrideValues`
  (already defined, unexported, in `internal/cli/topology.go` — same package, directly callable);
  `overrideValues{Mode, LocalPort}` (same file).
- Produces: `project.overriddenMode map[string]bool` — a new field other future work could read, though
  nothing in this plan needs to; `(*project).labelIfOverridden(id, text string) string`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/cli/status_test.go`, right after `TestStatusShowsSkippedComponentsWithReason`:

```go
// override.yaml 里的 mode: disable 让这个组件这次不跑——status 得说清楚这是本地
// override.yaml 造成的，不是 brickkit.yaml 自己写的，否则使用者会去改 brickkit.yaml
// 却怎么也改不动结果（设计书 §9："a component that isn't running because of a
// local disable is labeled as such, not left unexplained"）。
func TestStatusLabelsComponentDisabledByOverride(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
  - id: erp/backend
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: erp/backend
    mode: disable
`)
	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up", "--dry-run").code)

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "erp/backend")
	assert.Contains(t, r.stdout, "override.yaml")
}

// mode: disable 是 brickkit.yaml 自己写的（没有 override.yaml 介入）：不该出现
// override.yaml 这个词——那会让使用者去一份完全无关的文件里找一个根本不存在的设置。
func TestStatusDoesNotLabelComponentDisabledInBrickkitYamlItself(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
  - id: erp/backend
    version: 1.0.0
    mode: disable
`)
	eng := newFakeEngine()

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "erp/backend")
	assert.NotContains(t, r.stdout, "override.yaml")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run 'TestStatusLabelsComponentDisabledByOverride|TestStatusDoesNotLabelComponentDisabledInBrickkitYamlItself' -v`

Expected: `TestStatusLabelsComponentDisabledByOverride` FAILS (stdout doesn't contain "override.yaml").
`TestStatusDoesNotLabelComponentDisabledInBrickkitYamlItself` PASSES already (nothing labels anything
yet, so of course it doesn't contain the word — this one exists to catch Step 3 over-labeling).

- [ ] **Step 3: Write the implementation**

Modify `internal/cli/lifecycle.go`'s `project` struct — find this exact block (currently at lines
39-52):

```go
type project struct {
	layout config.Layout
	cfg    *config.Config
	graph  *resolver.Graph
	states *cascade.Result
	// order 是启动顺序（停止时倒着来）。
	order []resolver.Ref
	// degraded 非 nil 表示**依赖图没解析出来**，本次只能给出部分结论。
	//
	// 它是一个结论，不是一个错误：读不到 Manifest 并不妨碍回答
	// "现在什么在跑"——那个答案只来自引擎。谁需要依赖图、需要它回答哪一句，
	// 由各个渲染函数自己决定（见 status.go 的 buildView）。
	degraded *clierr.Error
}
```

Change it to:

```go
type project struct {
	layout config.Layout
	cfg    *config.Config
	graph  *resolver.Graph
	states *cascade.Result
	// order 是启动顺序（停止时倒着来）。
	order []resolver.Ref
	// degraded 非 nil 表示**依赖图没解析出来**，本次只能给出部分结论。
	//
	// 它是一个结论，不是一个错误：读不到 Manifest 并不妨碍回答
	// "现在什么在跑"——那个答案只来自引擎。谁需要依赖图、需要它回答哪一句，
	// 由各个渲染函数自己决定（见 status.go 的 buildView）。
	degraded *clierr.Error
	// overriddenMode 是 override.yaml 明确写了 mode 的组件 ID 集合——在 applyOverride
	// 把值合并进 cfg、抹掉"这个 mode 来自哪个文件"这个信息**之前**捕获（见
	// loadProject）。status 用它把"因为本地 override.yaml 而没跑"和"brickkit.yaml
	// 自己写的"分开说清楚（override.yaml 设计书 §9）。
	overriddenMode map[string]bool
}
```

Modify `internal/cli/lifecycle.go`'s `loadProject` — find this exact block (currently at lines 80-100):

```go
func loadProject(ctx context.Context, opts *Options) (*project, error) {
	p, err := loadConfig(opts)
	if err != nil {
		return nil, err
	}
	ov, err := loadOverride(opts, p.layout, p.cfg)
	if err != nil {
		return nil, err
	}
	if err := applyOverride(p.cfg, ov); err != nil {
		return nil, err
	}
```

Change it to:

```go
func loadProject(ctx context.Context, opts *Options) (*project, error) {
	p, err := loadConfig(opts)
	if err != nil {
		return nil, err
	}
	ov, err := loadOverride(opts, p.layout, p.cfg)
	if err != nil {
		return nil, err
	}
	p.overriddenMode = overriddenModeIDs(ov)
	if err := applyOverride(p.cfg, ov); err != nil {
		return nil, err
	}
```

Add this new function to `internal/cli/lifecycle.go`, right after `loadProject`:

```go
// overriddenModeIDs 返回 override.yaml 里明确写了 mode 的组件 ID 集合——只看 Mode
// 是否非空，不管具体取值：不只是 disable 需要说明来源，debug/local 同样是本地
// override.yaml 造成的、brickkit.yaml 里看不出来的事实。
func overriddenModeIDs(ov *override.Override) map[string]bool {
	if ov == nil {
		return nil
	}
	ids := map[string]bool{}
	for id, v := range flattenOverrideValues(ov.Components) {
		if v.Mode != "" {
			ids[id] = true
		}
	}
	return ids
}
```

This requires `internal/override` to be imported by `lifecycle.go`. Check its current import block —
it does not yet import `internal/override` (only `internal/config` for the `config.Layout`/`config.Config`
types it already uses). Modify the import block — find:

```go
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/msgid"
```

Change to:

```go
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/override"
```

Now modify `internal/cli/status.go`. Find `resolvedView`'s skip-row construction (currently):

```go
	if p.states != nil {
		for _, c := range p.states.Components {
			if c.State != cascade.StateRunning {
				v.skipped = append(v.skipped, statusRow{ref: c.Ref, text: c.Reason})
			}
		}
	}
```

Change it to:

```go
	if p.states != nil {
		for _, c := range p.states.Components {
			if c.State != cascade.StateRunning {
				v.skipped = append(v.skipped, statusRow{ref: c.Ref, text: p.labelIfOverridden(c.Ref.ID, c.Reason)})
			}
		}
	}
```

Find `degradedView`'s disabled case (currently):

```go
		case c.IsDisabled():
			v.skipped = append(v.skipped, statusRow{ref: ref, text: reasonDisabled()})
```

Change it to:

```go
		case c.IsDisabled():
			v.skipped = append(v.skipped, statusRow{ref: ref, text: p.labelIfOverridden(ref.ID, reasonDisabled())})
```

Add this method to `internal/cli/status.go`, right after the `statusRow` type definition:

```go
// labelIfOverridden 给"没跑"的原因文案加一个 override.yaml 出处标记——不然使用者
// 会去改 brickkit.yaml 却怎么也改不动结果，因为真正生效的 mode 来自本地的
// override.yaml，从来不在 brickkit.yaml 里（override.yaml 设计书 §9）。
func (p *project) labelIfOverridden(id, text string) string {
	if p.overriddenMode[id] {
		return text + i18n.T(msgid.CliStatusViaOverrideYaml)
	}
	return text
}
```

Add to `internal/msgid/cli_status.go`, inside the existing "手工处理的条目" `const (...)` block:

```go
	CliStatusViaOverrideYaml = "cli.status.via_override_yaml"
```

Add to `internal/i18n/catalog_en.go`:

```go
	msgid.CliStatusViaOverrideYaml: " (override.yaml)",
```

Add to `internal/i18n/catalog_zh.go`:

```go
	msgid.CliStatusViaOverrideYaml: "（override.yaml）",
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run 'TestStatusLabelsComponentDisabledByOverride|TestStatusDoesNotLabelComponentDisabledInBrickkitYamlItself' -v`

Expected: `PASS` for both tests.

- [ ] **Step 5: Run the full status suite plus i18n guard**

Run: `go test ./internal/cli/... -run TestStatus`

Then: `go test ./internal/cli/... ./tests/i18nguard/...`

Expected: all green — in particular `TestStatusShowsSkippedComponentsWithReason` (the pre-existing test
this task's tests sit next to) must still pass unchanged, since neither of its two components has an
`override.yaml` entry.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/lifecycle.go internal/cli/status.go internal/cli/status_test.go internal/msgid/cli_status.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "feat: brickkit status labels components disabled via override.yaml"
```

---

## Task 7: `brickkit override` in the CLI reference doc

**Files:**
- Modify: `docs/en/06-architecture/09-cli-reference.md` (new `## brickkit override` section, inserted
  between `## brickkit sync` and `## brickkit restore` — matching §8's command-table ordering in
  `AGENTS.md`, which this plan's Task 1-6 work doesn't change)
- Modify: `docs/zh/06-architecture/09-cli-reference.md` (the same section, Chinese)

**Interfaces:** None — this is a documentation-only task with no code interfaces. Its "test" is
`tests/docfields/outputlines_test.go` (already exists, already runs in `make check-doc-fields`) plus the
real command output captured in Step 1.

- [ ] **Step 1: Capture real CLI output**

Build the CLI and set up a scratch project (do this in a scratch directory outside the repo, e.g.
`/tmp/bk-override-doc-scratch/` — never inside `/home/zhijie/Desktop/github/brickKit` itself):

```bash
go build -o /tmp/bk-override-doc-scratch/brickkit ./cmd/brickkit
mkdir -p /tmp/bk-override-doc-scratch/proj
cd /tmp/bk-override-doc-scratch/proj
/tmp/bk-override-doc-scratch/brickkit init demo-shop --no-skills
mkdir -p components/demo
cp -r /home/zhijie/Desktop/github/brickKit/tests/components/demo-hello components/demo/hello
cp -r /home/zhijie/Desktop/github/brickKit/tests/components/demo-caller components/demo/caller
/tmp/bk-override-doc-scratch/brickkit add --local --yes
/tmp/bk-override-doc-scratch/brickkit override
cat override.yaml
```

Expected output for the `brickkit override` line: `Wrote override.yaml`. Expected `override.yaml`
content:

```
# override.yaml — local deployment overrides on top of brickkit.yaml.
# Generated/refreshed by `brickkit override`. Not authoritative — brickkit.yaml
# stays the source of truth for everything not listed here.

components:
    - id: demo/hello
    - id: demo/caller
```

Then capture the refresh-with-drift scenario:

```bash
cd /tmp/bk-override-doc-scratch/proj
sed -i 's/    - id: demo\/hello/    - id: demo\/hello\n      mode: debug\n      localPort: 9001\n      baseline: enabled/' override.yaml
/tmp/bk-override-doc-scratch/brickkit override
cat override.yaml
```

Expected output:

```
Wrote override.yaml
Drift: demo/hello — "demo/hello"'s mode in brickkit.yaml changed from "enabled" to "" since this override was last confirmed
```

Clean up: `rm -rf /tmp/bk-override-doc-scratch`.

If any captured line differs even slightly from what's quoted above (this doc was written against a
specific commit — the executor's checkout may have since changed `msgid.CliOverrideWritten` or
`msgid.CliOverrideDriftNote`'s exact wording), use the **real** output you just captured, not what's
written here. This is the entire point of Review Focus item 5 — do not hand-type these blocks.

- [ ] **Step 2: Write the English section**

`docs/en/06-architecture/09-cli-reference.md`'s `## brickkit sync` section currently ends (confirmed by
reading the file directly) with this exact sequence, right before `## brickkit restore` begins:

```
A hands-on walkthrough with real output: [Manage component source](../03-guide/09-component-source.md) — archiving, activating, and how it works with `mode`.

**Example**

```
$ brickkit sync
📂 The workspace needs no tidying
   There is no component source under components to archive or activate
```

---

## brickkit restore
```

Find that exact `---` blank-line-`## brickkit restore` sequence and insert the new section between the
`---` and `## brickkit restore`, so the result reads:

```
---

## brickkit override

**Syntax:** `brickkit override`

Creates `override.yaml` on first run, refreshes it on every later run — refreshing *is* the reset/repair
operation, there is no separate second command. Every component currently in `brickkit.yaml` gets a
line: an unoverridden one gets a bare `- id: <id>`, a `servedBy` member gets nested under its shell's own
entry. Existing customizations (`mode`, `localPort`, `baseline`, `target`, `targetBaseline`) are
preserved across a refresh; a component removed from `brickkit.yaml` loses its line; a new one gets a
fresh bare entry. Prints any drift notes (AGENTS.md §7.1) after writing, the same non-blocking check
`brickkit up` and `brickkit lint` also run.

Refuses to run when `--config` points at anything other than the default `brickkit.yaml` — `override.yaml`
only ever applies to a run against the default file, so generating one keyed off a different project file
would be scoped to the wrong `brickkit.yaml` from the start (AGENTS.md §7.1's multi-environment guard).

`override.yaml` is gitignored by default; `brickkit init` already seeds `.gitignore` with an entry for
it, and this command also calls the same `EnsureGitignore` check `init` does, in case the line was
removed by hand.

A hands-on walkthrough of the full mechanism — the schema, `mode: debug`'s override.yaml-only rule, the
downgrade-only `target` field: root `AGENTS.md` §7.1.

**Example**

```
$ brickkit override
Wrote override.yaml
```

```
$ cat override.yaml
# override.yaml — local deployment overrides on top of brickkit.yaml.
# Generated/refreshed by `brickkit override`. Not authoritative — brickkit.yaml
# stays the source of truth for everything not listed here.

components:
    - id: demo/hello
    - id: demo/caller
```

Refreshing after hand-editing in a `mode: debug` override, with `brickkit.yaml` having since changed
that component's own `mode` since the override's `baseline` was last confirmed:

```
$ brickkit override
Wrote override.yaml
Drift: demo/hello — "demo/hello"'s mode in brickkit.yaml changed from "enabled" to "" since this override was last confirmed
```

---

## brickkit restore
```

(`## brickkit restore` and everything after it is completely unchanged — only insert the new section
*before* it, reusing the `---` that already existed.)

- [ ] **Step 3: Write the Chinese section**

`docs/zh/06-architecture/09-cli-reference.md`'s `## brickkit sync` section ends (confirmed by reading the
file directly) with this exact sequence, right before `## brickkit restore` begins:

```
带真实输出、一步一步走的上手教程：[管理组件源码](../03-guide/09-component-source.md)——归档、激活、和它与 `mode` 的配合都在里面。

**示例**

```
$ brickkit sync
📂 工作区无需整理
   components 下没有需要归档或激活的组件源码
```

---

## brickkit restore
```

Find that exact sequence and insert the new section between the `---` and `## brickkit restore`. Note
that the Chinese doc tree quotes CLI output captured with `BRICKKIT_LANG=zh` set (confirmed by comparing
`## brickkit sync`'s two language sections — the English one shows "The workspace needs no tidying", the
Chinese one shows "工作区无需整理", not the same English text copy-pasted twice), so this section's
example block uses the Chinese-language output captured in Step 1, not a translation of Step 2's English
block. The `# override.yaml — ...` header comment inside the file itself stays in English in both
language trees — confirmed by capturing it under `BRICKKIT_LANG=zh` in Step 1: `renderOverride`'s header
is a fixed literal, never routed through `i18n.T`, so there is nothing to translate there; do not
"fix" it to Chinese.

```
---

## brickkit override

**用法：** `brickkit override`

首次运行时创建 `override.yaml`，之后每次运行都是刷新——刷新**就是**它的重置/修复
操作，没有单独的第二个命令。`brickkit.yaml` 里当前的每个组件都会有一行：没有覆盖
的组件是裸的 `- id: <id>`，`servedBy` 成员嵌在它所属外壳的条目下面。刷新时已有的
自定义值（`mode`、`localPort`、`baseline`、`target`、`targetBaseline`）原样保留；
从 `brickkit.yaml` 里删掉的组件那一行也跟着消失；新组件补一条裸条目。写完之后打印
漂移提示（AGENTS.zh.md §7.1）——跟 `brickkit up`、`brickkit lint` 用的是同一套
非阻断检查。

`--config` 指到默认 `brickkit.yaml` 以外的文件时拒绝运行——`override.yaml` 只对
针对默认文件的运行生效，生成一份挂在别的项目文件上的覆盖从一开始就是范围错了
（AGENTS.zh.md §7.1 的多环境护栏）。

`override.yaml` 默认进 `.gitignore`；`brickkit init` 已经在 `.gitignore` 里写好了
这一行，这条命令自己也会跑一遍 `init` 用的同一个 `EnsureGitignore` 检查，防止那一行
被手动删掉。

完整机制的上手细节——schema、`mode: debug` 只能写在这里这条规则、只许降级的
`target` 字段：见根目录 `AGENTS.zh.md` §7.1。

**示例**

```
$ brickkit override
已写入 override.yaml
```

```
$ cat override.yaml
# override.yaml — local deployment overrides on top of brickkit.yaml.
# Generated/refreshed by `brickkit override`. Not authoritative — brickkit.yaml
# stays the source of truth for everything not listed here.

components:
    - id: demo/hello
    - id: demo/caller
```

手改过一条 `mode: debug` 覆盖之后再刷新，而 `brickkit.yaml` 自己这个组件的 mode
在这份覆盖的 `baseline` 上次确认之后已经变了：

```
$ brickkit override
已写入 override.yaml
漂移：demo/hello —— "demo/hello" 在 brickkit.yaml 里的 mode 从 "enabled" 变成了 ""（相对这份覆盖上次确认时）
```

---

## brickkit restore
```

- [ ] **Step 4: Verify the doc checks**

Run: `make check-docs-bilingual`

Expected: `✅ docs/en ↔ docs/zh 镜像完整...`

Run: `go test ./tests/docfields/...`

Expected: `PASS` — every line beginning with a CLI symbol in the new section matches a real catalog
message (this is why Step 1's captured output must be pasted verbatim, not retyped).

Run: `make check-docs`

Expected: `✅ 悬空小节引用：无` and no new failures — confirms the new section didn't break any existing
`§N`-style cross-reference elsewhere in the doc tree (this section introduces none itself, but the check
covers the whole tree and any accidental heading-level mistake would show up here).

- [ ] **Step 5: Commit**

```bash
git add docs/en/06-architecture/09-cli-reference.md docs/zh/06-architecture/09-cli-reference.md
git commit -m "docs: add brickkit override to the CLI reference"
```

---

## Final note for the executor

After Task 7's commit, run the full `make lint` once (per this repo's established convention — see Plan
2a's own SDD ledger for why: it catches cross-cutting checks no single task's own test run exercises,
such as `check-cli-docs`'s command-count/flag-existence scan and `golangci-lint`). Treat any finding the
same way Plan 2a's own execution did: a real, unanticipated finding gets fixed with the same rigor as an
in-scope task (a failing test first, not a guessed patch), not silently absorbed into whichever task's
commit happens to be open at the time — ledger it as its own fix, separately.

Also run `go test ./...` (the whole repository, not just `internal/cli`) once, at the very end — this
plan's tasks only ever modify files under `internal/cli/`, `internal/msgid/`, `internal/i18n/`, and
`docs/`, so no other package's tests should be affected, but confirming that is cheap and is exactly the
kind of assumption worth checking once rather than trusting.
