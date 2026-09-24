# Podman Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `override.yaml`'s `target: podman` actually run `up`/`down`/`status` against a real Podman engine, closing 005 §7.5's revival condition ② — without adding any new CI automation, environment detection, or portability guarantee beyond the one machine already verified.

**Architecture:** `internal/engine`'s `Compose` struct is already engine-agnostic (`name`/`bin`/`base`/`runner`); add `NewPodman()` as a second five-line constructor over it, exactly like `NewDocker()`. Two small, targeted additions ride on top: a bin-aware "install X" hint (currently hardcoded to Docker), and a pattern-matched translation of Podman's one known `down`-failure signature into a hint pointing at the existing environment checklist doc. Wire `internal/cli/up_k8s.go`'s `resolveEngineFor` to dispatch to this real engine instead of erroring.

**Tech Stack:** Go, existing `internal/engine`/`internal/cli`/`internal/i18n`/`internal/clierr` packages, `testify` for assertions, the repo's `tests/checklist/清单.tsv` convention.

**Spec:** [docs/superpowers/specs/2026-09-24-podman-engine-implementation-design.md](../specs/2026-09-24-podman-engine-implementation-design.md)

## Global Constraints

- No new CI workflow. `.github/workflows/release.yml` (tag-triggered only) is not touched. Test coverage for this plan is unit tests with a fake `runner`, mirroring the existing Docker test pattern — never a real `podman`/`docker` invocation.
- No OS/AppArmor/environment-fitness detection code anywhere in the CLI. The only new "awareness" of the environment is a post-hoc text-pattern match on a command's *output*, exactly like `imageError()` already does — never a pre-emptive check.
- `brickkit.yaml`'s own `deploy.target` never accepts `podman` — this validation is untouched. `podman` stays an `override.yaml`-only value.
- No new `clierr.Code`. Every new/changed error reuses `CodeEngineMissing` or `CodeEngineFailed`.
- Every new or changed `msgid` needs entries in **both** `internal/i18n/catalog_en.go` and `catalog_zh.go` in the same commit — `make check-i18n` and `make check-docs-bilingual` are lint gates, not compile errors, so a one-language addition builds and tests green and only fails later.
- Run `make fmt` before each commit that touches `.go` files. Run `make lint` (or at minimum `go vet ./... && go test ./... && make check-i18n`) before the final commit of any task that changes messages or docs.
- `docs/archive/**` is never edited and never newly linked from current docs — both archived Podman documents already declare themselves read-only historical records; that convention holds.
- Do not touch anything under `.claude/worktrees/` — it is a separate, intentionally-unmerged worktree from unrelated past work.

## Review Focus

- **Docker's existing missing-binary hint must not regress.** Making the hint bin-aware must leave Docker's exact existing wording ("Install Docker 20.10+ and retry") untouched — a careless refactor could silently drop the version-floor guidance for every Docker user while fixing it for Podman. Covered by keeping `TestMissingBinaryGivesInstallHint` unchanged and passing in Task 2.
- **The AppArmor-signature hint must not over-match.** A generic "permission denied" from an unrelated cause must not carry the checklist hint — over-matching sends users chasing the wrong fix. Covered by `TestGenericPermissionDeniedGetsNoApparmorHint` in Task 2.
- **`resolveEngineFor`'s K8s and default(Docker) branches must stay byte-identical.** The podman-branch edit sits inside the same function; a careless edit could perturb the shared `if cfg != nil` structure the K8s branch also depends on. Covered by running the full existing `internal/cli` test suite (not just the new tests) in Task 4.
- **An injected `opts.Engine` must always win, even for Podman.** Every CLI test that injects a fake engine (existing and new) must never accidentally construct a real `engine.NewPodman()` that shells out during `go test`. Covered explicitly by `TestResolveEngineForPodmanTargetDispatchesRealEngine` only ever checking the *no-injection* path, never calling `Up`/`Down`/`Status` on the real engine it gets back.
- **Bilingual parity for every new msgid.** A message added to `catalog_en.go` but not `catalog_zh.go` compiles and passes `go test ./...` silently; only `make check-i18n`/`check-docs-bilingual` catch it. Each task below that adds a msgid ends its steps with running that check, not just `go test`.

---

### Task 1: `Podman` engine constant + `NewPodman()`

**Files:**
- Modify: `internal/engine/engine.go:1-19`
- Modify: `internal/engine/compose.go:26-29`
- Test: `internal/engine/compose_test.go`

**Interfaces:**
- Consumes: `Compose` struct (`internal/engine/compose.go:17-24`), `run` function, existing `Docker`/`K8s` constants.
- Produces: `Podman` constant (value `"podman"`), `NewPodman() *Compose` — both consumed by Task 2 (same file), Task 3 (`Detect()`), and Task 4 (`resolveEngineFor`).

- [ ] **Step 1: Write the failing test**

Add to `internal/engine/compose_test.go`, near `dockerWith` (after line 56):

```go
func podmanWith(rec *recorder) *Compose {
	c := NewPodman()
	c.runner = rec.run
	return c
}

// bin 换成 podman 之后，实际调用的必须是 podman 二进制，不是 docker——
// Compose 结构体是共用的，唯一该变的只有 bin。
func TestPodmanUsesPodmanBinary(t *testing.T) {
	rec := newRecorder()

	require.NoError(t, podmanWith(rec).Up(context.Background(), UpRequest{
		File: "f.yaml", Project: "brickkit-my-erp",
	}))

	call := rec.lastCall(t)
	assert.Contains(t, call, "podman compose")
	assert.NotContains(t, call, "docker compose")
	assert.Equal(t, Podman, podmanWith(rec).Name())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/... -run TestPodmanUsesPodmanBinary -v`
Expected: FAIL — `NewPodman` and `Podman` are undefined.

- [ ] **Step 3: Implement**

In `internal/engine/engine.go`, replace lines 1-19:

```go
// Package engine 抽象底层容器引擎（Docker、Podman）与部署目标（Kubernetes）。
//
// CLI 自己不懂容器，它只做三件事：把 brickkit.yaml 翻译成部署文件、
// 决定谁该启动、然后把文件交给引擎。这一层的存在是为了让"决定"与"执行"
// 分开——命令层的逻辑因此可以在没有 Docker/Podman 的机器上被完整测试。
//
// Podman 只能通过 override.yaml 的 target: podman 显式选用——brickkit.yaml
// 自身的 deploy.target 不接受这个取值，`Detect` 在没有显式配置、且只装了
// Podman 时也不会把它当默认引擎（选中哪个引擎必须来自配置，不能来自"猜"），
// 而是提示如何显式启用它（见 podmanNotEnabled）。
package engine

import "context"

// 引擎名。与 compose 包的 EngineDocker/EnginePodman 取值一致。
const (
	Docker = "docker"
	Podman = "podman"
	// K8s 是 kubectl 引擎。它不是"另一种容器引擎"，而是另一种**部署目标**
	// （005 §5）：清单交给集群，跑不跑得起来是集群的事。
	K8s = "k8s"
)
```

In `internal/engine/compose.go`, right after `NewDocker()` (after line 29):

```go

// NewPodman 返回 podman compose 引擎。
//
// podman compose 在已验证的机器上直接调用与 Docker 相同的 docker-compose
// 二进制，因此 Compose 结构体的其余行为（命令拼装、ps 输出解析、P27 的
// stdout/stderr 处理）全部原样适用，只有 bin 不同。
func NewPodman() *Compose {
	return &Compose{name: Podman, bin: "podman", base: []string{"compose"}, runner: run}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/... -run TestPodmanUsesPodmanBinary -v`
Expected: PASS

- [ ] **Step 5: Run the full package test suite**

Run: `go test ./internal/engine/...`
Expected: PASS (no existing test regresses)

- [ ] **Step 6: Commit**

```bash
make fmt
git add internal/engine/engine.go internal/engine/compose.go internal/engine/compose_test.go
git commit -m "feat: add NewPodman() engine constructor

Reuses the existing engine-agnostic Compose struct with bin: podman —
podman compose shells out to the same docker-compose binary on the
verified machine, so no other behavior needs to change."
```

---

### Task 2: Engine error handling — bin-aware install hint + known Podman down-failure hint

**Files:**
- Modify: `internal/engine/compose.go:189-203` (the `exec` method)
- Modify: `internal/msgid/engine.go:3-29`
- Modify: `internal/i18n/catalog_en.go` (around line 214)
- Modify: `internal/i18n/catalog_zh.go` (around line 208)
- Test: `internal/engine/compose_test.go`

**Interfaces:**
- Consumes: `podmanWith`/`dockerWith` from Task 1, `recorder` fixture (`compose_test.go:19-50`).
- Produces: `installHint(bin string) string` helper (package-private, used only within `compose.go`).

- [ ] **Step 1: Write the failing test for the bin-aware hint**

Add to `internal/engine/compose_test.go`, near `TestMissingBinaryGivesInstallHint`:

```go
// Podman 引擎缺二进制时不能还建议装 Docker——那是完全不同的两条路。
func TestPodmanMissingBinaryGivesInstallHint(t *testing.T) {
	rec := newRecorder()
	rec.fail["up"] = errors.New(`exec: "podman": executable file not found in $PATH`)

	err := podmanWith(rec).Up(context.Background(), UpRequest{File: "f.yaml", Project: "p"})

	require.Error(t, err)
	assert.Equal(t, clierr.CodeEngineMissing, clierr.As(err).Code)
	text := clierr.As(err).Format()
	assert.Contains(t, text, "Podman", "要说清装什么")
	assert.NotContains(t, text, "Install Docker", "podman 引擎缺二进制时不该建议装 Docker")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/... -run TestPodmanMissingBinaryGivesInstallHint -v`
Expected: FAIL — hint still says "Install Docker 20.10+ and retry" regardless of `c.bin`.

- [ ] **Step 3: Write the failing tests for the down-failure translation**

Add to `internal/engine/compose_test.go`:

```go
// 已知的 AppArmor 失败信号要指回环境检查清单，而不是留一句裸的
// permission denied 让人自己去猜。
func TestPodmanDownTranslatesKnownApparmorFailure(t *testing.T) {
	rec := newRecorder()
	rec.fail["down"] = errors.New("exit 1")
	rec.output["down"] = "Error: removing container ...: 1 error occurred:\n" +
		"\t* rootless netns: kill network process: permission denied\n"

	err := podmanWith(rec).Down(context.Background(), DownRequest{Project: "brickkit-demo"})

	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "11-podman-environment-checklist.md")
}

// 无关的 permission denied 不该套用这条提示——过度匹配会把人引向错误的修法。
func TestGenericPermissionDeniedGetsNoApparmorHint(t *testing.T) {
	rec := newRecorder()
	rec.fail["down"] = errors.New("exit 1")
	rec.output["down"] = "Error: removing container: permission denied\n"

	err := podmanWith(rec).Down(context.Background(), DownRequest{Project: "brickkit-demo"})

	require.Error(t, err)
	assert.NotContains(t, clierr.As(err).Format(), "11-podman-environment-checklist.md",
		"这条提示只匹配那一句具体的 kill network process 特征串，不是任何 permission denied")
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/engine/... -run 'TestPodmanDownTranslatesKnownApparmorFailure|TestGenericPermissionDeniedGetsNoApparmorHint' -v`
Expected: both FAIL — no such translation exists yet.

- [ ] **Step 5: Add the new msgids**

In `internal/msgid/engine.go`, replace line 14 (`EngineHintInstallDocker = "engine.hint.install_docker"`) with:

```go
	EngineHintInstallDocker             = "engine.hint.install_docker"
	EngineHintInstallPodman             = "engine.hint.install_podman"
	EnginePodmanDownBlockedByAppArmor   = "engine.podman_down_blocked_by_apparmor"
```

- [ ] **Step 6: Add the catalog entries**

In `internal/i18n/catalog_en.go`, right after the `msgid.EngineHintInstallDocker` line:

```go
	msgid.EngineHintInstallPodman:           "Install Podman and retry",
	msgid.EnginePodmanDownBlockedByAppArmor: "This is a known Ubuntu/Debian AppArmor gap blocking rootless Podman's network teardown (containers/podman#27372), not a BrickKit or Podman bug — see docs/en/07-patterns/11-podman-environment-checklist.md for the fix",
```

In `internal/i18n/catalog_zh.go`, right after the `msgid.EngineHintInstallDocker` line:

```go
	msgid.EngineHintInstallPodman:           "安装 Podman 后重试",
	msgid.EnginePodmanDownBlockedByAppArmor: "这是一个已知的 Ubuntu/Debian AppArmor 缺口，拦住了 rootless Podman 的网络拆卸（containers/podman#27372），不是 BrickKit 或 Podman 的 bug——修法见 docs/zh/07-patterns/11-podman-environment-checklist.md",
```

- [ ] **Step 7: Implement `exec()`**

Replace `internal/engine/compose.go:189-203` with:

```go
func (c *Compose) exec(ctx context.Context, args ...string) ([]byte, error) {
	out, err := c.runner(ctx, c.bin, args...)
	if err == nil {
		return out, nil
	}
	if isMissingBinary(err) {
		return out, clierr.New(clierr.CodeEngineMissing, i18n.T(msgid.EngineBinaryMissing, c.bin)).
			WithHint(installHint(c.bin)).
			WithCause(err)
	}

	failure := clierr.New(clierr.CodeEngineFailed, i18n.T(msgid.EngineExecFailed, c.bin)).
		WithDetail(i18n.T(msgid.LabelCommand), c.bin+" "+strings.Join(args, " ")).
		WithDetail(i18n.T(msgid.LabelOutput), tail(string(out), 3)).
		WithCause(err)
	if strings.Contains(string(out), "kill network process: permission denied") {
		failure = failure.WithHint(i18n.T(msgid.EnginePodmanDownBlockedByAppArmor))
	}
	return out, failure
}

// installHint 按缺失的二进制给出对应的安装建议——Podman 引擎缺 podman 时
// 不能还建议装 Docker，那是完全不同的两条路。
func installHint(bin string) string {
	if bin == "podman" {
		return i18n.T(msgid.EngineHintInstallPodman)
	}
	return i18n.T(msgid.EngineHintInstallDocker)
}
```

- [ ] **Step 8: Run all four tests to verify they pass**

Run: `go test ./internal/engine/... -run 'TestPodmanMissingBinaryGivesInstallHint|TestPodmanDownTranslatesKnownApparmorFailure|TestGenericPermissionDeniedGetsNoApparmorHint|TestMissingBinaryGivesInstallHint' -v`
Expected: all PASS — including the pre-existing `TestMissingBinaryGivesInstallHint`, unchanged (Review Focus #1).

- [ ] **Step 9: Run the bilingual guard**

Run: `make check-i18n`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
make fmt
git add internal/engine/compose.go internal/engine/compose_test.go internal/msgid/engine.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "feat: bin-aware install hint + translate Podman's known down-failure signature

Missing-binary hint now names the actual missing engine instead of
always suggesting Docker. A down failure matching Podman's documented
AppArmor signature (containers/podman#27372) gets a hint pointing at
the existing environment checklist instead of a bare permission denied."
```

---

### Task 3: `Detect()` / `podmanNotEnabled()` reword

**Files:**
- Modify: `internal/engine/compose.go:369-404` (`Detect` and `podmanNotSupported`)
- Modify: `internal/msgid/engine.go` (remove 5 stale msgids, add 2)
- Modify: `internal/i18n/catalog_en.go` / `catalog_zh.go`
- Test: `internal/engine/detect_test.go`, `internal/engine/compose_test.go:414-428`

**Interfaces:**
- Consumes: nothing new from earlier tasks.
- Produces: `podmanNotEnabled() error` (renamed from `podmanNotSupported`), still called only from `Detect()`.

This is a *different* code path from Task 4's `resolveEngineFor`: it fires when nothing in config
asked for Podman, but Docker isn't on `PATH` and Podman is. It must keep erroring (never silently
switch engines — explicit-over-implicit), but the wording changes: Podman is no longer "stuck",
just "not enabled."

- [ ] **Step 1: Write the failing tests**

In `internal/engine/detect_test.go`, replace `TestDetectReportsPodmanSpecifically` (the whole function, including its doc comment) with:

```go
// 只有 Podman 时不是"找不到引擎"，而是"检测到了但没启用"——两者该做的下一步
// 完全不同：后者只需要显式配置去用它，前者才是真的什么都没装。
func TestDetectReportsPodmanSpecifically(t *testing.T) {
	pathWith(t, "podman")

	_, err := Detect()

	require.Error(t, err)
	text := clierr.As(err).Format()
	assert.Contains(t, text, "Podman is installed, but not enabled", "34.11：%s", text)
	assert.Contains(t, text, "target: podman", "34.11：要指出怎么显式启用它")
	assert.NotContains(t, text, "no usable container engine found",
		"34.11：不能报成笼统的'找不到引擎'——那会让人以为装了也没用，其实装了就能用")
}
```

Also update `TestDetectPrefersDocker`'s doc comment (the line `// 有 Docker 就用 Docker。`) to:

```go
// 有 Docker 就用 Docker——没有显式配置时不会自动选 Podman。
```

In `internal/engine/compose_test.go`, replace `TestPodmanOnlyMachineGetsSpecificError` (lines 414-428, including its doc comment) with:

```go
// 只装了 Podman、没装 Docker、也没有显式配置时，报的必须是"检测到但没启用"，
// 而不是笼统的"找不到容器引擎"，也不能悄悄把 Podman 当默认引擎用——
// 选中哪个引擎必须来自配置，不能来自"猜"。
func TestPodmanOnlyMachineGetsEnableHint(t *testing.T) {
	err := podmanNotEnabled()

	text := clierr.As(err).Format()
	assert.Contains(t, text, "Podman is installed, but not enabled")
	assert.Contains(t, text, "override")
	assert.Contains(t, text, "target: podman")
	assert.Contains(t, text, "--dry-run", "生成文件不需要引擎，这条出路要给出来")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/engine/... -run 'TestDetectReportsPodmanSpecifically|TestPodmanOnlyMachineGetsEnableHint' -v`
Expected: both FAIL — `podmanNotEnabled` doesn't exist yet, old wording doesn't match.

- [ ] **Step 3: Update the msgids**

In `internal/msgid/engine.go`, remove these five lines entirely: `EnginePodmanUnsupported`,
`EngineLabelStuckAt`, `EnginePodmanStuckDetail`, `EngineLabelWhyNotHalf`, `EnginePodmanWhyDetail`.
Keep `EngineLabelDetected`, `EnginePodmanDetectedDetail`, `EngineHintDryRunNoEngine` — they're still
accurate. Add two new ones in their place:

```go
	EnginePodmanNotEnabled = "engine.podman_not_enabled"
	EngineHintEnablePodman = "engine.hint.enable_podman"
```

- [ ] **Step 4: Update the catalogs**

In `internal/i18n/catalog_en.go`, remove the five lines for the msgids deleted in Step 3, keep the
three kept above, and add:

```go
	msgid.EnginePodmanNotEnabled: "Error: Podman is installed, but not enabled",
	msgid.EngineHintEnablePodman: "Run brickkit override and set target: podman in override.yaml to use it — see docs/en/07-patterns/11-podman-environment-checklist.md for the environment prerequisite",
```

In `internal/i18n/catalog_zh.go`, same removals, and add:

```go
	msgid.EnginePodmanNotEnabled: "错误：检测到 Podman，但尚未启用",
	msgid.EngineHintEnablePodman: "运行 brickkit override，在 override.yaml 里把 target 设成 podman 才会真正启用——环境前提见 docs/zh/07-patterns/11-podman-environment-checklist.md",
```

- [ ] **Step 5: Implement**

Replace `internal/engine/compose.go:369-404` (from the `Detect` doc comment through the end of
`podmanNotSupported`) with:

```go
// Detect 挑选可用的容器引擎（005 §7.4）。
//
// 没有显式配置时只在 Docker/Podman 之间按 PATH 挑：Docker 优先，只有 Podman
// 也不会把它悄悄当默认——选中哪个引擎必须来自配置，不能来自"猜"（这条线
// resolveEngineFor 的 target: podman 分支也在守）。只有 Podman 时如实提示
// 怎么显式启用它，而不是把它当"找不到引擎"那样笼统报错。
//
// 只有真正要启动时才该调用它；只生成文件不需要引擎。
func Detect() (Engine, error) {
	if _, err := exec.LookPath("docker"); err == nil {
		return NewDocker(), nil
	}
	if _, err := exec.LookPath("podman"); err == nil {
		return nil, podmanNotEnabled()
	}
	return nil, clierr.New(clierr.CodeEngineMissing, i18n.T(msgid.EngineNoneFound)).
		WithDetail(i18n.T(msgid.EngineLabelTried), "docker compose").
		WithHint(
			i18n.T(msgid.EngineHintInstallDocker),
			i18n.T(msgid.EngineHintDryRunOnly),
		)
}

// podmanNotEnabled 在没有显式选择 podman、但机器上只装了 podman 时如实说明现状。
//
// 与"没找到引擎"分开报，是因为下一步不同：前者装个 Docker（或者显式选
// podman）就好；这里则是"你其实可以用它，只是还没告诉配置去用"。也不能因为
// 只有 Podman 就悄悄拿它当默认引擎——那等于让平台替使用者做了一次没人
// 要求过的选择。
func podmanNotEnabled() error {
	return clierr.New(clierr.CodeEngineMissing, i18n.T(msgid.EnginePodmanNotEnabled)).
		WithDetail(i18n.T(msgid.EngineLabelDetected), i18n.T(msgid.EnginePodmanDetectedDetail)).
		WithHint(
			i18n.T(msgid.EngineHintEnablePodman),
			i18n.T(msgid.EngineHintDryRunNoEngine),
		)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/engine/...`
Expected: PASS, all tests including `TestDetectPrefersDocker`, `TestDetectWithoutAnyEngine`.

- [ ] **Step 7: Run the bilingual guard**

Run: `make check-i18n`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
make fmt
git add internal/engine/compose.go internal/engine/detect_test.go internal/engine/compose_test.go internal/msgid/engine.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "fix: reword Detect()'s podman-only message from 'stuck' to 'not enabled'

Podman genuinely works now when explicitly configured, so the old
'built, ran partway, permanently stuck' framing is no longer accurate.
Detect() still never auto-selects Podman without explicit config —
only the wording changes, pointing at override.yaml's target: podman."
```

---

### Task 4: CLI wiring — `resolveEngineFor` real dispatch

**Files:**
- Modify: `internal/cli/up_k8s.go:157-192`
- Modify: `internal/msgid/cli_override.go:7-9`
- Modify: `internal/i18n/catalog_en.go` / `catalog_zh.go` (remove the two now-dead entries)
- Test: `internal/cli/podman_target_test.go` (full rewrite)

**Interfaces:**
- Consumes: `engine.NewPodman()` (Task 1), `engine.Podman` (Task 1), `newFakeEngine()` /
  `runWithEngine()` (`internal/cli/testsupport_engine_test.go`, pre-existing).
- Produces: nothing new consumed by later tasks — this is the last code change.

- [ ] **Step 1: Write the failing tests**

Replace the entire contents of `internal/cli/podman_target_test.go` with:

```go
package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/engine"
)

// --dry-run 只需要生成一份 compose 文件——engine-agnostic，podman 消费的是
// 同一份 docker-compose.yaml。target: podman 不该拦住它。
func TestUpDryRunSucceedsWithPodmanTarget(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `target: podman`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

// 真跑（非 --dry-run）时，target: podman 现在必须真的把工作交给 Podman
// 引擎——005 §7 挪掉的那个 engine.Engine 实现已经回来了。这里注入假引擎，
// 验证的是"分发对了"，不实际调用真 podman 二进制。
func TestUpRealRunSucceedsWithPodmanTarget(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `target: podman`)
	eng := newFakeEngine()
	eng.name = engine.Podman

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotEmpty(t, eng.ups, "target: podman 时 up 必须真的调用引擎，不能再报'还没实现'")
}

// down 与 status 走的是同一个 resolveEngineFor，同源但独立的调用路径。
func TestDownSucceedsWithPodmanTarget(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `target: podman`)
	eng := newFakeEngine()
	eng.name = engine.Podman

	r := runWithEngine(t, eng, f.Dir, "down")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotEmpty(t, eng.downs)
}

func TestStatusSucceedsWithPodmanTarget(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `target: podman`)
	eng := newFakeEngine()
	eng.name = engine.Podman

	r := runWithEngine(t, eng, f.Dir, "status")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

// 没有注入假引擎时，target: podman 必须真的分发到 Podman 引擎（Name() 是
// "podman"），而不是当年那个"还没实现"的错误——这是唯一断言"真调用路径
// 选对了引擎"的用例，只检查 resolveEngineFor 的返回值，不调用它的任何方法，
// 所以永远不会真的去 exec 一个 podman 二进制。
func TestResolveEngineForPodmanTargetDispatchesRealEngine(t *testing.T) {
	cfg := &config.Config{Deploy: config.Deploy{Target: config.TargetPodman}}

	eng, err := resolveEngineFor(&Options{}, cfg)

	require.NoError(t, err)
	assert.Equal(t, engine.Podman, eng.Name())
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run 'PodmanTarget|SucceedsWithPodmanTarget' -v`
Expected: FAIL — the real (non-dry-run) tests currently expect an error and get one from
`podmanTargetNotImplemented`; the new dispatch test fails because that function still returns an
error instead of an engine.

- [ ] **Step 3: Implement**

Replace `internal/cli/up_k8s.go:157-192` (from the `resolveEngineFor` doc comment through the end
of `podmanTargetNotImplemented`) with:

```go
// resolveEngineFor 按部署目标选引擎。
//
// K8s 与 Docker/Podman 不是"同一类引擎的两个牌子"，而是两种部署目标：
// 前者把清单交给集群，后两者在本机起容器。选错的后果在 Step 16 之前撞到过一次——
// 一个 target: k8s 的项目被按 Docker 处理，文件生成了、命令也成功了，
// 只是整个项目跑在了错误的编排器上。
//
// target: podman 走的是同一个 Compose 结构体，只是 bin 换成了 podman
// （engine.NewPodman）——这条路径**显式**来自配置，跟"没有配置、只是环境里
// 只装了 Podman"（engine.Detect 的 podmanNotEnabled 分支）是两回事，两者
// 不能混在一起判：前者是使用者自己选的，后者是平台替他猜的，猜出来的选择
// 不该被当成配置里选出来的。
func resolveEngineFor(opts *Options, cfg *config.Config) (engine.Engine, error) {
	if cfg != nil && cfg.Deploy.Target == config.TargetK8s {
		if opts.Engine != nil {
			return opts.Engine, nil
		}
		return engine.NewKubectl(), nil
	}
	if cfg != nil && cfg.Deploy.Target == config.TargetPodman {
		if opts.Engine != nil {
			return opts.Engine, nil
		}
		return engine.NewPodman(), nil
	}
	return resolveEngine(opts)
}
```

- [ ] **Step 4: Remove the dead msgids**

In `internal/msgid/cli_override.go`, remove these two lines:

```go
	CliUpPodmanTargetNotImplemented   = "cli.up.podman_target_not_implemented"
	CliUpPodmanTargetDryRunStillWorks = "cli.up.podman_target_dry_run_still_works"
```

In `internal/i18n/catalog_en.go`, remove:

```go
	msgid.CliUpPodmanTargetNotImplemented:   "target: podman is accepted and validated, but there is no Podman engine implementation to actually run it yet",
	msgid.CliUpPodmanTargetDryRunStillWorks: "brickkit up --dry-run still works — it generates an ordinary compose file, which is exactly what podman compose consumes",
```

In `internal/i18n/catalog_zh.go`, remove the matching two lines.

- [ ] **Step 5: Run the new tests**

Run: `go test ./internal/cli/... -run 'PodmanTarget|SucceedsWithPodmanTarget' -v`
Expected: PASS

- [ ] **Step 6: Run the full `internal/cli` suite (Review Focus #3)**

Run: `go test ./internal/cli/...`
Expected: PASS — no existing K8s/Docker-path test regresses from the `resolveEngineFor` edit.

- [ ] **Step 7: Run the bilingual guard**

Run: `make check-i18n`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
make fmt
git add internal/cli/up_k8s.go internal/cli/podman_target_test.go internal/msgid/cli_override.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "feat: dispatch target: podman to a real engine.Engine

resolveEngineFor now returns engine.NewPodman() instead of erroring —
override.yaml's target: podman actually runs up/down/status against
Podman. Deletes the now-dead podmanTargetNotImplemented and its two
messages; a real missing-binary or down failure produces its own
correct error via the engine layer (Task 2)."
```

---

### Task 5: `tests/checklist/清单.tsv` rows

**Files:**
- Modify: `tests/checklist/清单.tsv`

**Interfaces:**
- Consumes: every test function name from Tasks 1-4.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Add the rows**

Append these lines to `tests/checklist/清单.tsv`, immediately after the existing `34.12` row
(matching its exact tab-separated 7-field shape: category, id, description, `test`, `.`, package,
function):

```
compat	34.13	NewPodman 调用的是 podman 二进制，不是 docker	test	.	./internal/engine	TestPodmanUsesPodmanBinary
compat	34.14	引擎二进制缺失时的安装建议按引擎名走，不永远建议装 Docker	test	.	./internal/engine	TestPodmanMissingBinaryGivesInstallHint
compat	34.15	down 已知的 AppArmor 失败信号带上环境检查清单的提示	test	.	./internal/engine	TestPodmanDownTranslatesKnownApparmorFailure
compat	34.16	无关的 permission denied 不套用 AppArmor 提示	test	.	./internal/engine	TestGenericPermissionDeniedGetsNoApparmorHint
compat	34.17	只装 Podman、未显式启用时提示怎么启用	test	.	./internal/engine	TestPodmanOnlyMachineGetsEnableHint
compat	34.18	resolveEngineFor 对 target: podman 真实分发到 Podman 引擎	test	.	./internal/cli	TestResolveEngineForPodmanTargetDispatchesRealEngine
compat	34.19	brickkit up 对 target: podman 真的调用引擎，不再报未实现	test	.	./internal/cli	TestUpRealRunSucceedsWithPodmanTarget
compat	34.20	brickkit down 对 target: podman 真的调用引擎	test	.	./internal/cli	TestDownSucceedsWithPodmanTarget
compat	34.21	brickkit status 对 target: podman 真的调用引擎	test	.	./internal/cli	TestStatusSucceedsWithPodmanTarget
```

- [ ] **Step 2: Verify the checklist self-check passes**

Run: `make test-compat`
Expected: PASS — this both re-verifies the checklist rows point at real, passing tests and runs
every `compat`-tagged test, including the ones from Tasks 1-4.

- [ ] **Step 3: Commit**

```bash
git add tests/checklist/清单.tsv
git commit -m "test: register the new Podman engine tests in the compat checklist"
```

---

### Task 6: Documentation

**Files:**
- Modify: `AGENTS.md`
- Modify: `AGENTS.zh.md`
- Modify: `docs/en/06-architecture/00-overview.md`
- Modify: `docs/zh/06-architecture/00-overview.md`
- Modify: `docs/en/06-architecture/10-error-codes.md`
- Modify: `docs/zh/06-architecture/10-error-codes.md`
- Modify: `docs/en/07-patterns/11-podman-environment-checklist.md`
- Modify: `docs/zh/07-patterns/11-podman-environment-checklist.md`

No tests — this is prose only. Each step below gives the exact old and new text so there's nothing
to improvise.

- [ ] **Step 1: `AGENTS.md` §4.1 row**

Replace line 204 (the entire `| Podman as a real, running deploy target | ... |` row) with:

```
| Automatically verifying a machine's Podman environment can tear down cleanly, or running that verification in CI | `override.yaml`'s `target` field (§7.1) runs a real `engine.Engine` implementation for Podman — `up`, `down`, and `status` all work once the host prerequisite is met. The platform never probes for that prerequisite itself (verify it yourself with `scripts/podman/check-environment.sh`); a `down` that hits the one known failure signature still gets a translated hint pointing back at the [environment checklist](docs/en/07-patterns/11-podman-environment-checklist.md) instead of a bare `permission denied`. See §5.10 below for the full picture, including what's still explicitly not done (portability beyond the one verified environment, automated real-lifecycle CI) |
```

- [ ] **Step 2: `AGENTS.md` new §5.10**

Insert, right before the `## 6. \`component.yaml\` (Manifest) field skeleton` heading (line 619):

```markdown
### 5.10 Podman as a deploy engine

`override.yaml`'s `target: podman` (§7.1) is a real, working deploy engine — not just a
validated-but-inert config value. Once set, `up`, `down`, and `status` all run against Podman
instead of Docker, using the exact same generated `docker-compose.yaml` `podman compose` already
consumes identically to Docker.

**The one prerequisite:** rootless Podman's network-teardown helper, `pasta`, needs a `SIGTERM`
from `podman` to tear a deployment's network down cleanly on `down`. Most Ubuntu/Debian installs'
default AppArmor policy blocks that signal — a confirmed distro packaging gap
([containers/podman#27372](https://github.com/containers/podman/issues/27372)), not a BrickKit or
Podman bug. [The environment checklist](docs/en/07-patterns/11-podman-environment-checklist.md)
has the diagnostic (`scripts/podman/check-environment.sh`) and the fix
(`scripts/podman/fix-apparmor.sh`).

**What the CLI does and doesn't do about this:** it never probes for that prerequisite itself —
doing so would mean guessing at environment fitness instead of the project explicitly declaring
it (§4's "explicit over implicit"). What it does do: if a real `down`/`up` hits that exact known
failure signature, the error's hint points back at the checklist instead of leaving a bare
`permission denied` for the user to puzzle over.

**What's still explicitly out of scope:** any portability guarantee beyond the one environment
this was verified on (other distributions, other AppArmor configurations), and any automated
real-container-lifecycle CI check — this repository's test suite verifies the Podman engine the
same way it verifies Docker's: unit tests against a fake runner, not a real container.

---

```

- [ ] **Step 3: `AGENTS.md` §11.2 index row**

Insert, right after the row `| Whether and how to declare \`servedBy\` on your own project | ... |`
(line 1335):

```
| How Podman works as a deploy engine, and the one environment prerequisite it needs | `docs/en/07-patterns/11-podman-environment-checklist.md` (swap `en` for `zh`) |
```

- [ ] **Step 4: `AGENTS.zh.md` — mirror all three edits**

Replace line 184 with:

```
| 自动验证某台机器的 Podman 环境是否真的能干净拆卸，或者把这份验证接进 CI | `override.yaml` 的 `target` 字段（§7.1）现在跑的是一个真正的 Podman `engine.Engine` 实现——满足宿主前提后，`up`、`down`、`status` 全部生效。平台自己从不去探测这个前提是否满足（用 `scripts/podman/check-environment.sh` 自己验证）；真的撞上那个已知的失败信号时，`down` 报错里的建议仍会指回[环境检查清单](docs/zh/07-patterns/11-podman-environment-checklist.md)，而不是留一句裸的 `permission denied`。完整情况见下面的 §5.10，包括明确还没做的部分（这台机器之外的可移植性、自动化的真实生命周期 CI） |
```

Insert, right before `## 6. \`component.yaml\`（Manifest）字段骨架` (line 530):

```markdown
### 5.10 把 Podman 当部署引擎用

`override.yaml` 的 `target: podman`（§7.1）是一个真正能跑的部署引擎——不是"校验通过但什么都不做"
的配置项。设了它之后，`up`、`status`、`down` 都会真的对 Podman 生效，用的是与 Docker
完全同一份生成出来的 `docker-compose.yaml`——`podman compose` 消费它的方式和 Docker 一模一样。

**唯一的前提条件：** rootless Podman 的网络拆卸辅助进程 `pasta`，需要收到 `podman` 发来的
`SIGTERM` 才能在 `down` 时把这次部署的网络干净拆掉。大多数 Ubuntu/Debian 默认的 AppArmor
策略会拦下这个信号——这是一个已确认的发行版打包缺口
（[containers/podman#27372](https://github.com/containers/podman/issues/27372)），不是 BrickKit
或 Podman 自己的 bug。[环境检查清单](docs/zh/07-patterns/11-podman-environment-checklist.md)
给了诊断脚本（`scripts/podman/check-environment.sh`）和修法（`scripts/podman/fix-apparmor.sh`）。

**CLI 对这件事做了什么、没做什么：** 它自己从不去探测这个前提条件是否满足——那样做等于让平台
去猜环境是否合适，而不是由项目显式声明（§4"显式优于隐式"）。它确实做的是：如果一次真实的
`down`/`up` 撞上这个已知的、特征明确的失败信号，报错里的建议会指回这份检查清单，而不是留一句
裸的 `permission denied` 让用户自己去猜。

**明确还没做的：** 这台机器之外的可移植性保证（其它发行版、其它 AppArmor 配置），以及任何
自动化的"真实容器生命周期" CI 检查——这个仓库验证 Podman 引擎的方式和验证 Docker 引擎完全一样：
对着假 runner 的单元测试，不是真容器。

---

```

Insert, right after the row `| 要不要在自己项目里声明 \`servedBy\`、怎么声明 | ... |` (line 1156):

```
| Podman 怎么当部署引擎用，以及它唯一的环境前提 | `docs/zh/07-patterns/11-podman-environment-checklist.md`（英文版把 `zh` 换 `en`） |
```

- [ ] **Step 5: `docs/en/06-architecture/00-overview.md` entry 16**

Replace the whole `### 16. Podman as a deploy target` section (lines 298-301) with:

```markdown
### 16. Podman as a deploy target

- **What it is:** deploying with Podman, another container engine, instead of Docker.
- **Why it doesn't, unconditionally:** the platform does no OS-level detection of whether a given
  machine's Podman rootless networking can actually tear down cleanly, gives no portability
  guarantee beyond the one environment this was verified on, and has no automated
  real-container-lifecycle CI check for it — building any of those would mean the platform
  guessing at environment fitness instead of the project explicitly declaring it, the same
  "auto-guessed and uncontrollable" shape rejected elsewhere.
- **What actually works:** `override.yaml`'s `target: podman` runs a real `engine.Engine`
  implementation — `up`, `down` and `status` all work, once the host prerequisite is met. The
  blocker this entry used to describe (rootless Podman's `down` failing because its
  network-teardown helper, `pasta`, can't receive the signal that tears it down cleanly — a distro
  AppArmor packaging gap, not a BrickKit or Podman bug) is resolved on a verified environment; see
  the [Podman environment checklist](../07-patterns/11-podman-environment-checklist.md) for the
  prerequisite and how to check for it. A known failure matching that exact signature still gets a
  translated hint pointing back at that checklist, rather than a bare `permission denied`.
- **What to do instead, on an unverified machine:** use Docker, or run
  `scripts/podman/check-environment.sh` first to find out where you stand.
```

And replace the summary table row (near the top of the file, the row starting
`| [16. Podman as a deploy target]`) with:

```
| [16. Podman as a deploy target](#16-podman-as-a-deploy-target)<br>Deploying with Podman instead of Docker | `override.yaml`'s `target: podman` runs a real engine now — the platform still doesn't auto-detect environment fitness, guarantee portability beyond one verified machine, or run automated real-lifecycle CI for it | Set `target: podman` in `override.yaml` once the [environment checklist](../07-patterns/11-podman-environment-checklist.md) prerequisite is met |
```

- [ ] **Step 6: `docs/zh/06-architecture/00-overview.md` entry 16 (mirror)**

Replace the whole `### 16. Podman 作为部署目标` section (lines 308-311) with:

```markdown
### 16. Podman 作为部署目标

- **它是什么：** 用 Podman（另一种容器引擎）代替 Docker 来部署。
- **不做到什么程度：** 平台自己从不去探测某台机器的 Podman rootless 网络是否真的能干净拆卸，
  不保证除了这一台已验证的机器之外的可移植性，也没有自动化的"真实容器生命周期" CI 检查——
  做这几件事里的任何一件，都等于让平台去猜环境是否合适，而不是由项目显式声明，跟别处已经拒绝的
  "猜出来、不可控"是同一类问题。
- **现在真能用的部分：** `override.yaml` 的 `target: podman` 跑的是一个真正的 `engine.Engine`
  实现——满足宿主前提后，`up`、`down`、`status` 全部生效。这一条从前描述的那个卡点（rootless
  Podman 的 `down` 会失败，因为它的网络拆卸辅助进程 `pasta` 收不到能让它干净拆卸的信号——这是
  发行版打包的缺口，不是 BrickKit 或 Podman 的 bug）在已验证的环境上已经解决；前提条件和自查
  方法见[Podman 环境检查清单](../07-patterns/11-podman-environment-checklist.md)。真的撞上这个
  具体特征串的失败时，报错仍会带上指回那份清单的提示，而不是留一句裸的 `permission denied`。
- **在没验证过的机器上该怎么办：** 用 Docker，或者先跑一遍
  `scripts/podman/check-environment.sh` 看看自己这台机器到底是什么情况。
```

And replace the summary table row at line 166 with:

```
| [16. Podman 作为部署目标](#16-podman-作为部署目标)<br>用 Podman 代替 Docker 部署 | `override.yaml` 的 `target: podman` 现在跑的是真引擎——平台仍然不会自动探测环境是否合适、不保证这台机器之外的可移植性、也没有自动化的真实生命周期 CI | 满足[环境检查清单](../07-patterns/11-podman-environment-checklist.md)的前提后，在 override.yaml 里设 target: podman |
```

- [ ] **Step 7: `docs/en/06-architecture/10-error-codes.md`**

In the `### ENGINE_MISSING` table, replace these two rows:

```
| `Error: container engine <engine> not found` | The engine binary isn't installed | Install Docker 20.10+ |
| `Error: no usable container engine found` | No engine found at all | Install Docker 20.10+ — or use `brickkit up --dry-run` to generate the files without one |
| `Error: Podman isn't supported yet — please use Docker` | Only Podman is installed. Podman support was built and then withdrawn: `down` fails on rootless Podman, and a project that can't be torn down is worse than one that never came up | Install Docker |
```

with:

```
| `Error: container engine <engine> not found` | The engine binary isn't installed | Install Docker 20.10+, or Podman if `target: podman` |
| `Error: no usable container engine found` | No engine found at all | Install Docker 20.10+ — or use `brickkit up --dry-run` to generate the files without one |
| `Error: Podman is installed, but not enabled` | Podman is on `PATH` but nothing in config asked for it — the platform won't pick an engine it wasn't explicitly told to use | Run `brickkit override` and set `target: podman` in `override.yaml`, or install Docker |
```

In the `### ENGINE_FAILED` table, add one row at the end:

```
| `Error: <command> failed to run` (Podman, output mentioning `kill network process: permission denied`) | Rootless Podman's `down` blocked by a host AppArmor policy — a distro packaging gap, not a BrickKit or Podman bug | Follow the hint to `docs/en/07-patterns/11-podman-environment-checklist.md` |
```

- [ ] **Step 8: `docs/zh/06-architecture/10-error-codes.md` (mirror)**

In the `### ENGINE_MISSING` table, replace:

```
| `错误：找不到容器引擎 <engine>` | 没装这个引擎的可执行文件 | 安装 Docker 20.10+ |
| `错误：没有找到可用的容器引擎` | 一个引擎都没找到 | 安装 Docker 20.10+——或用 `brickkit up --dry-run`，不需要引擎就能生成文件 |
| `错误：暂不支持 Podman，请使用 Docker` | 只装了 Podman。Podman 支持写过、后来撤回了：rootless Podman 上 `down` 会失败，而一个停不掉的项目比根本起不来的更糟 | 安装 Docker |
```

with:

```
| `错误：找不到容器引擎 <engine>` | 没装这个引擎的可执行文件 | 安装 Docker 20.10+；如果 target 是 podman，装 Podman |
| `错误：没有找到可用的容器引擎` | 一个引擎都没找到 | 安装 Docker 20.10+——或用 `brickkit up --dry-run`，不需要引擎就能生成文件 |
| `错误：检测到 Podman，但尚未启用` | Podman 在 PATH 里，但配置里没有谁要求用它——平台不会替你选一个没被显式要求的引擎 | 运行 `brickkit override`，在 override.yaml 里把 target 设成 podman；或者安装 Docker |
```

In the `### ENGINE_FAILED` table, add:

```
| `错误：<command> 执行失败`（Podman，输出里带 `kill network process: permission denied`） | rootless Podman 的 `down` 被宿主机的 AppArmor 策略拦住——是发行版打包的缺口，不是 BrickKit 或 Podman 的 bug | 按提示去看 `docs/zh/07-patterns/11-podman-environment-checklist.md` |
```

- [ ] **Step 9: `docs/en/07-patterns/11-podman-environment-checklist.md` disclaimer**

Replace the opening blockquote (lines 3-7):

```
> **This is not about BrickKit choosing an engine.** As of today, `internal/engine` only
> implements Docker, and `deploy.target` only accepts `docker` and `k8s` — there is no way to
> tell BrickKit to run compose through Podman instead. This page is a prerequisite checklist for
> anyone who wants to run rootless Podman itself on Ubuntu/Debian (independent of BrickKit, and
> useful groundwork if Podman support is ever added) — not a feature this CLI has today.
```

with:

```
> **This is now a real BrickKit deploy option, with one prerequisite.** `override.yaml`'s
> `target: podman` runs a real `engine.Engine` implementation — `up`, `down`, and `status` all
> work through it. What this page still describes is the one environment prerequisite that has to
> be true first: rootless Podman's `down` needs AppArmor to allow its `pasta` helper to receive
> the signal that tears its network down cleanly, which most Ubuntu/Debian installs block by
> default. Run this page's checklist before setting `target: podman`; if `brickkit down` still
> fails with the exact error this page describes, the fix below is what to apply.
```

- [ ] **Step 10: `docs/zh/07-patterns/11-podman-environment-checklist.md` disclaimer (mirror)**

Replace the opening blockquote (lines 3-7):

```
> **这篇不是在说 BrickKit 支持选择哪个容器引擎。** 目前 `internal/engine` 只实现了 Docker，
> `deploy.target` 也只接受 `docker` 和 `k8s`——没有任何方式能让 BrickKit 改用 Podman 来跑 compose。
> 这篇是给任何想在 Ubuntu/Debian 上独立跑通 rootless Podman 本身的人准备的前置检查清单
> （跟 BrickKit 无关，如果将来真的要接入 Podman 支持，这也是有用的前期准备）——不是这个 CLI
> 现在就有的功能。
```

with:

```
> **这现在是一个真实的 BrickKit 部署选项，只有一个前提条件。** `override.yaml` 的
> `target: podman` 跑的是一个真正的 `engine.Engine` 实现——`up`、`down`、`status` 全都能用。
> 这篇文档现在讲的是那唯一的环境前提：rootless Podman 的 `down` 需要 AppArmor 允许它的
> `pasta` 辅助进程收到能干净拆掉网络的信号，而大多数 Ubuntu/Debian 默认会拦下这个信号。
> 设 `target: podman` 之前先跑一遍这篇的检查清单；如果 `brickkit down` 依然报出这篇描述的
> 那个具体错误，下面的修法就是该用的那个。
```

- [ ] **Step 11: Verify the doc lint gates**

Run: `make check-docs check-docs-bilingual check-doc-fields`
Expected: PASS — no dangling links, both language trees still mirror each other structurally.

- [ ] **Step 12: Commit**

```bash
git add AGENTS.md AGENTS.zh.md docs/en/06-architecture/00-overview.md docs/zh/06-architecture/00-overview.md docs/en/06-architecture/10-error-codes.md docs/zh/06-architecture/10-error-codes.md docs/en/07-patterns/11-podman-environment-checklist.md docs/zh/07-patterns/11-podman-environment-checklist.md
git commit -m "docs: describe Podman as a real, working deploy engine

AGENTS.md/AGENTS.zh.md's rejection-list row and a new §5.10 section,
00-overview.md's catalog entry 16, error-codes.md's ENGINE_MISSING/
ENGINE_FAILED rows, and the environment checklist's opening disclaimer
all move from 'built, ran, pulled' framing to describing what target:
podman actually does today, and the one environment prerequisite and
CI/portability gaps that remain."
```

---

### Task 7: Close out the paused working notes + one-time manual re-verification

**Files:**
- Modify: `docs/superpowers/specs/2026-09-23-podman-engine-support-notes.md`

No new tests — this task is a documentation close-out plus a manual (not automated) verification
step.

- [ ] **Step 1: Mark the working notes resolved**

At the top of `docs/superpowers/specs/2026-09-23-podman-engine-support-notes.md`, replace the first
line (`# Podman engine support — working notes (paused)`) and its status line with:

```markdown
# Podman engine support — working notes (resolved)

**Status: resolved.** Superseded by
[2026-09-24-podman-engine-implementation-design.md](2026-09-24-podman-engine-implementation-design.md)
and its implementation plan. The "still open" items below were answered there: the field shape
question by the already-shipped `override.yaml` `target` field, the CI guard question by choosing
unit tests over new CI automation, and the `internal/engine` implementation by `NewPodman()`. This
file is kept as historical record of the investigation that led there, not as an open task list.
```

- [ ] **Step 2: One-time manual full-lifecycle re-verification**

This step has no automated test — it is the human acceptance step that closes 005 §7.5's revival
condition ② against the *shipped code*, not just the environment (the 2026-09 investigation
verified the environment and a lifecycle by hand with raw `podman` commands; this repeats it
through the real CLI, after Tasks 1-4 land). On this machine, with the code from Tasks 1-6 built:

```bash
make build-cli
REPO="$(pwd)"
mkdir -p /tmp/podman-verify && cd /tmp/podman-verify
"$REPO/bin/brickkit" init podman-verify
cd podman-verify
# add a component with a health check and a dependency, e.g. one of tests/components/
"$REPO/bin/brickkit" add demo/hello@1.0.0 --local
"$REPO/bin/brickkit" override
# edit override.yaml: target: podman
"$REPO/bin/brickkit" up
"$REPO/bin/brickkit" status
curl http://localhost:<exposed-port>/healthz   # or whatever the test component exposes
"$REPO/bin/brickkit" up      # idempotent rerun
"$REPO/bin/brickkit" down
podman ps -a                 # confirm no residue
podman network ls            # confirm no leftover network
```

Expected: every command succeeds, `status` shows the component healthy, the request succeeds, the
rerun is a no-op success, `down` exits 0, and neither `podman ps -a` nor `podman network ls` show
anything left over. If any step fails, stop and treat it as a bug in Tasks 1-4, not a documentation
issue.

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/specs/2026-09-23-podman-engine-support-notes.md
git commit -m "docs: close out the paused podman-engine-support-notes

Superseded by the 2026-09-24 design + implementation plan; the manual
full-lifecycle re-verification through the real CLI passed on this
machine."
```
