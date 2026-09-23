# override.yaml core mechanism — Implementation Plan (2a of 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `override.yaml` file format end to end (types, parsing, single-file and cross-file validation, drift detection, JSON schema) and wire it into `brickkit up`/`sync`/`status`, add the `brickkit override` command that creates/refreshes it, wire in `target: podman` engine selection (erroring clearly at real-run time since no Podman `engine.Engine` exists yet), and make `brickkit.yaml` itself reject `mode: debug` (only `override.yaml` may set it from here on).

**Architecture:** A new `internal/override` package owns the file's types, parsing (mirroring `internal/config`'s 4-step parse pipeline), single-file validation, cross-file validation against `*config.Config`, and drift detection. `internal/cli/topology.go` gains an `applyOverride` step — mutating the in-memory `*config.Config` before `resolveTopology` runs, exactly like the existing `clearServedBy` (`--ignore-served-by`) pattern — so `internal/cascade`/`internal/resolver`/`internal/compose`/`internal/k8s` need zero changes: they already only ever read `cfg.Deploy.Target`/`cfg.Components[i].Mode`, never caring where the value came from. `brickkit graph` is deliberately never touched (spec §9: it stays scoped to `brickkit.yaml` alone).

**Tech Stack:** Go, `gopkg.in/yaml.v3`, the existing `internal/clierr`/`internal/yamlcheck`/`internal/schemagen`/`internal/i18n`+`internal/msgid` machinery — no new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-23-deploy-config-override-design.md`. This plan implements: §2 (two-file architecture), §3 (identity/opt-in/gitignore), §4 (mode placement — `debug` only in override.yaml), §5 (target/engine selection, downgrade-only), §6.1/§6.3 (shell semantics — already built by the companion `shell-standalone-fallback` plan, merged as commit `579d0b9`; this plan only needs its existing behavior, not new work), §7 (staleness/drift detection — the pure detection functions; the `lint` *rule* that surfaces them is Plan 2b), §8 (schema), and the `up`/`sync`/`status`/`brickkit override` rows of §9's command table. Plan 2b (a separate, later plan) covers §9's `add`/`remove`/`lint`/`restore` rows plus documentation.

## Global Constraints

- **Exact field names** (spec §8): `target`, `targetBaseline` at the document root; `components[].id`, `.mode`, `.localPort`, `.baseline`, `.members[]` (recursively the same shape) per component. **No `version` field anywhere in `override.yaml`** — component entries are bare IDs (spec §6.1's last bullet: version-forced instances are fully automatic, never user-toggled).
- **`target` accepts exactly `docker`, `podman`, or `k8s`** — but only in the **downgrade** direction from `brickkit.yaml`'s own `deploy.target` (spec §5.2): `k8s` → `docker`/`podman` is always allowed; `docker`/`podman` → `k8s` is always rejected; `docker` ↔ `podman` is unrestricted either way.
- **`debug` may never appear in `brickkit.yaml` itself** (spec §4) — only in `override.yaml`. `local` remains legal in both files. `k8s` (as the *effective*, post-override target) forbids `local`/`debug`/any further target-override on any component (spec §4's second paragraph).
- **`override.yaml` is optional, gitignored by default, and silent-by-default**: a missing file means "defer entirely to `brickkit.yaml`" — never an error. An empty (0-byte or all-comments) file means the same thing — also never an error, matching "when present, is either silent on a topic or explicit about it" (spec §2).
- **Multi-environment guard (spec §10):** `override.yaml` only applies when the command runs against the *default* `brickkit.yaml` — no `--config`, or `--config` pointing explicitly at it. When `--config` points elsewhere and an `override.yaml` file exists on disk, the command must **warn that it's being ignored**, never silently apply it and never silently ignore it without saying so.
- **`brickkit graph` never reads `override.yaml`, and this plan makes zero changes to `internal/cli/graph.go`'s wiring** — its output must not vary by who generated it locally (spec §9). The *only* `graph.go` changes in this plan are the removal of dead code that becomes unreachable once `brickkit.yaml` can no longer contain `mode: debug` (see Task 7) — not new override-awareness.
- **Podman's actual `engine.Engine` implementation is out of scope** (spec §11 — a separate companion doc/plan). This plan's job is narrower: `target: podman` must parse, validate, and downgrade-check exactly like `docker`/`k8s`, and `brickkit up --dry-run` must succeed and generate an ordinary (engine-agnostic) compose file — but a real (non-dry-run) `up` with an effective `podman` target must fail with a clear "not implemented yet" error at the point it would otherwise try to invoke the engine, never silently falling back to Docker and never crashing unexpectedly.
- **Error code reuse, not new stable codes:** every new error/warning in this plan reuses `clierr.CodeConfigInvalid` (matching the established house convention — see `internal/compose/servedby.go`'s `fallbackStandaloneWarnings` and `internal/config/validate.go`'s own use of the same code for every brickkit.yaml validation problem). No new `clierr.Code` constant is minted.
- **i18n discipline unchanged:** every user-visible string goes through an `internal/msgid` constant with matching entries in both `internal/i18n/catalog_en.go` and `internal/i18n/catalog_zh.go` (verified by `go test ./tests/i18nguard/...`, part of `make check-i18n`).

## Review Focus

1. **A stale `override.yaml` entry naming a component ID no longer in `brickkit.yaml`.** Spec §7: "always an error." A user who deletes a component from `brickkit.yaml` but forgets to touch `override.yaml` should get a clear error from `up`/`sync`/`status`, not a silent no-op that leaves them wondering why their override isn't taking effect. (Task 3, Task 4)
2. **`--config brickkit.prod.yaml` with an `override.yaml` sitting in the same directory.** Spec §10's hard guard: the override must never silently apply to a non-default config, and its presence must never go unmentioned either. (Task 4)
3. **An attempted target *upgrade*** (`brickkit.yaml`'s own `deploy.target: docker` + `override.yaml`'s `target: k8s`). Spec §5.2: always rejected, with a clear error naming the disallowed direction — never silently clamped, never silently ignored. (Task 3, Task 4)
4. **`target: k8s` as the *effective* target (whether from `brickkit.yaml` directly, or because `override.yaml` set no `target` at all) combined with a component-level `mode: debug`/`mode: local` override.** Spec §4: k8s forbids all three (local/debug/target-override) — this is a distinct check from the docker-only rule already enforced for `brickkit.yaml` itself, because it has to see *both* files to know the effective target. (Task 3, Task 4)
5. **`target: podman` selected, then a real (non-dry-run) `brickkit up`.** Must fail with a clear, specific error at the engine-invocation step — not silently try Docker, not panic on a nil `engine.Engine`, and critically must **not** block `--dry-run` (which only needs to generate a docker-compose-shaped file, exactly as Podman itself consumes). (Task 5)

---

## Task 1: `internal/override` package — types, layout, parsing, single-file validation

**Files:**
- Create: `internal/override/override.go`
- Create: `internal/override/parse.go`
- Create: `internal/override/validate.go`
- Create: `internal/override/override_test.go`
- Modify: `internal/config/layout.go:12-35` (add `FileOverride` const), `internal/config/layout.go:104` (add `OverridePath()` method right after `GitignorePath()`)
- Modify: `internal/config/config.go:8-13` (add `TargetPodman` const next to `TargetDocker`/`TargetK8s`)
- Modify: `internal/config/scaffold.go:123-135` (`gitignoreSections()` — add an `override.yaml` section; purely additive, not consumed until Task 6)
- Create: `internal/msgid/override.go` (message IDs for the `internal/override` package itself)
- Modify: `internal/i18n/catalog_en.go`, `internal/i18n/catalog_zh.go` (new entries for the above)

**Interfaces:**
- Consumes: `config.TargetDocker`/`config.TargetK8s`/`config.TargetPodman` (constants), `config.ModeEnabled`/`ModeDisable`/`ModeDebug`/`ModeLocal` (constants), `clierr.NewProblemSet`/`clierr.New`/`ProblemSet.Add`/`.Missing`/`.Err`, `yamlcheck.Walk`.
- Produces (for later tasks in this plan and for Plan 2b):
  - `type Override struct { Target, TargetBaseline string; Components []ComponentOverride; Source string }`
  - `type ComponentOverride struct { ID, Mode string; LocalPort int; Baseline string; Members []ComponentOverride }`
  - `func ParseOverrideFile(path string) (*Override, error)` — missing file → `(nil, nil)`, never an error.
  - `func ParseOverride(data []byte, source string) (*Override, error)` — empty/blank content → `(&Override{Source: source}, nil)`, never an error.
  - `func (o *Override) Validate() error` — single-file rules only (shape, unknown fields, `Mode`/`Target` enums, duplicate component IDs). Cross-file rules are Task 3's job.
  - `config.Layout.OverridePath() string` — always `<Root>/override.yaml`, independent of `--config`/`ConfigFile` (spec §10: the file's identity is fixed; only *whether it's read* depends on `--config`).
  - `config.TargetPodman = "podman"` — **not** added to `validateDeploy`'s accepted case list in `internal/config/validate.go:49-56` (unchanged in this task); it exists purely as shared vocabulary for `internal/override` and later `internal/cli` code that reads an already-overridden `cfg.Deploy.Target` in memory.

- [ ] **Step 1: Write the failing test for the types + a valid-file round trip**

```go
// internal/override/override_test.go
package override

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOverrideMissingFileReturnsNilNoError(t *testing.T) {
	o, err := ParseOverrideFile("/nonexistent/override.yaml")
	require.NoError(t, err)
	assert.Nil(t, o)
}

func TestParseOverrideEmptyContentIsValidNoOverrides(t *testing.T) {
	o, err := ParseOverride([]byte(""), "override.yaml")
	require.NoError(t, err)
	require.NotNil(t, o)
	assert.Empty(t, o.Target)
	assert.Empty(t, o.Components)
}

func TestParseOverrideFullShapeRoundTrips(t *testing.T) {
	o, err := ParseOverride([]byte(`
target: podman
targetBaseline: docker
components:
  - id: department/tree
  - id: erp/backend
    members:
      - id: people/basic
      - id: auth/rbac
        mode: disable
  - id: infra/redis-event-bus
    mode: local
    localPort: 8082
  - id: payment/gateway
    mode: debug
    localPort: 9091
    baseline: local
`), "override.yaml")
	require.NoError(t, err)
	require.NotNil(t, o)

	assert.Equal(t, "podman", o.Target)
	assert.Equal(t, "docker", o.TargetBaseline)
	require.Len(t, o.Components, 4)

	assert.Equal(t, "department/tree", o.Components[0].ID)
	assert.Empty(t, o.Components[0].Mode)

	shell := o.Components[1]
	assert.Equal(t, "erp/backend", shell.ID)
	require.Len(t, shell.Members, 2)
	assert.Equal(t, "people/basic", shell.Members[0].ID)
	assert.Equal(t, "auth/rbac", shell.Members[1].ID)
	assert.Equal(t, "disable", shell.Members[1].Mode)

	redis := o.Components[2]
	assert.Equal(t, "local", redis.Mode)
	assert.Equal(t, 8082, redis.LocalPort)

	payment := o.Components[3]
	assert.Equal(t, "debug", payment.Mode)
	assert.Equal(t, 9091, payment.LocalPort)
	assert.Equal(t, "local", payment.Baseline)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/override/... -run TestParseOverride -v`
Expected: FAIL — build error, `undefined: ParseOverride`/`Override` (the package doesn't exist yet).

- [ ] **Step 3: Write the types**

```go
// internal/override/override.go
// Package override 实现 override.yaml——本地、可选、默认 gitignore 的部署覆盖文件
// （override.yaml 设计书）。它从不改动 brickkit.yaml，也从不参与 brickkit.yaml 自身的
// 校验：两份文件各自独立解析，跨文件的规则（target 降级方向、k8s 下 local/debug 不许
// 出现、baseline 漂移）单独在 CheckAgainst/Drift 里判断（本包另一个文件）。
package override

// Override 是 override.yaml 的完整结构（设计书 §8）。
type Override struct {
	// Target 覆盖 brickkit.yaml 的 deploy.target，只能是降级方向
	// （设计书 §5.2，跨文件规则见 CheckAgainst）。不写就跟着 deploy.target 走。
	Target string `yaml:"target,omitempty" jsonschema:"enum=docker|podman|k8s"`
	// TargetBaseline 记录上一次确认这份覆盖时 deploy.target 的取值（设计书 §7）。
	// Target 一旦写了，这个字段总会有值——deploy.target 本身是必填字段，
	// 不存在"没写"这回事，跟组件级 Baseline 不是同一种"缺省"。
	TargetBaseline string `yaml:"targetBaseline,omitempty"`
	// Components 是这份覆盖列出的组件条目，穷举式：项目当前有的每个组件都该有
	// 一行（哪怕只是裸 `- id: X`），生成/刷新逻辑负责维持这一点（brickkit override
	// 命令，另一个任务）。
	Components []ComponentOverride `yaml:"components,omitempty"`

	// Source 是该文件的来源路径，只用于错误提示，不参与结构比较。
	Source string `yaml:"-"`
}

// ComponentOverride 是 override.yaml 里一个组件的条目。
//
// 没有 Version 字段——精确到版本的实例是级联自动算出来的：外壳没提供的版本
// 实例强制独立部署，override.yaml 从不代表这件事（设计书 §6.1 最后一条）。
type ComponentOverride struct {
	// ID 是组件 ID（不带版本），必填。
	ID string `yaml:"id"`
	// Mode 覆盖这个组件在 brickkit.yaml 里的 mode。debug 只能在这里出现——
	// brickkit.yaml 自身从今往后拒绝这个取值（设计书 §4）。
	Mode string `yaml:"mode,omitempty" jsonschema:"enum=enabled|disable|debug|local"`
	// LocalPort 覆盖 brickkit.yaml 建议的默认端口，或者在 Mode 是 debug 时
	// 提供它唯一的落脚点——debug 从不出现在 brickkit.yaml，它的 localPort
	// 没有别的地方可以写（设计书 §8 结尾）。
	LocalPort int `yaml:"localPort,omitempty"`
	// Baseline 记录上一次确认这份覆盖时 brickkit.yaml 对这个组件声明的 mode
	// （设计书 §7）。只在 brickkit.yaml 当时**已经**显式写了取值时才落笔——
	// 多数组件在 brickkit.yaml 里没有 mode，这个字段因此常常留空，这是刻意的
	// 取舍（设计书 §7 的"Consequence, accepted deliberately"）。
	Baseline string `yaml:"baseline,omitempty"`
	// Members 是被这个组件（作为外壳）合并部署的成员，同一种形状递归嵌套——
	// 嵌套纯粹是给人看的分组，apply 时会整个展平（设计书 §8："避免重复声明
	// 我属于外壳 X"）。
	Members []ComponentOverride `yaml:"members,omitempty"`
}
```

- [ ] **Step 4: Write parsing**

```go
// internal/override/parse.go
package override

import (
	"os"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcheck"
)

// ParseOverrideFile 读取并解析 override.yaml。
//
// 文件不存在时返回 (nil, nil)——这份机制是选用的（设计书 §3），"没有 override.yaml"
// 与"brickkit.yaml 缺失"完全不是同一类事情，不能沿用 config.ParseConfigFile 那种
// "文件缺失就报错"的处理。
func ParseOverrideFile(path string) (*Override, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.OverrideReadFailed)).
			WithDetail(i18n.T(msgid.LabelPath), path).
			WithDetail(i18n.T(msgid.LabelReason), err.Error()).
			WithHint(i18n.T(msgid.ProblemHintCheckPermissions)).
			WithCause(err)
	}
	return ParseOverride(data, path)
}

// ParseOverride 解析并校验 override.yaml 的单文件规则。source 用于错误提示。
//
// 跟 config.ParseConfig 同一个四步骨架（语法 → 形状+未知字段 → 解码 → 语义校验），
// 少一步：override.yaml 没有任何字段合理地需要 ${VAR} 展开（target/mode 是枚举，
// localPort 是数字，baseline 只是取值的镜像），所以不做 config.ExpandEnv 那一遍。
func ParseOverride(data []byte, source string) (*Override, error) {
	if source == "" {
		source = "override.yaml"
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.OverrideNotValidYAML)).
			WithDetail(i18n.T(msgid.LabelFile), source).
			WithDetail(i18n.T(msgid.LabelReason), cleanYAMLError(err)).
			WithHint(i18n.T(msgid.ProblemHintCheckSyntax)).
			WithCause(err)
	}
	if root.Kind == 0 || len(root.Content) == 0 {
		// 空文件等价于"这份覆盖什么都没说"——不是错误，与文件不存在同一个
		// 结论（override.yaml 设计书 §2："要么对某件事完全不吭声，要么明说"，
		// 空文件是"对所有事都不吭声"的极端情形，合法）。
		return &Override{Source: source}, nil
	}

	doc := root.Content[0]

	shape := newOverrideProblems(source)
	checkOverrideShapes(doc, shape)
	yamlcheck.Walk(doc, reflect.TypeOf(Override{}), shape)
	if shape.Len() > 0 {
		return nil, shape.Err()
	}

	var o Override
	if err := doc.Decode(&o); err != nil {
		p := newOverrideProblems(source)
		var typeErr *yaml.TypeError
		if te, ok := err.(*yaml.TypeError); ok {
			for _, msg := range te.Errors {
				p.Add(i18n.T(msgid.ProblemLabelTypeMismatch), msg)
			}
		} else {
			p.Add(i18n.T(msgid.ProblemLabelParseFailed), cleanYAMLError(err))
		}
		return nil, p.Err()
	}
	o.Source = source

	if err := o.Validate(); err != nil {
		return nil, err
	}
	return &o, nil
}

func newOverrideProblems(source string) *clierr.ProblemSet {
	if source == "" {
		source = "override.yaml"
	}
	return clierr.NewProblemSet(clierr.CodeConfigInvalid, i18n.T(msgid.OverrideValidationFailed)).
		WithSource(i18n.T(msgid.LabelFile), source)
}

func cleanYAMLError(err error) string {
	return strings.TrimSpace(strings.TrimPrefix(err.Error(), "yaml: "))
}

// checkOverrideShapes 在解码前检查节点形状：components 与嵌套的 members 都必须是数组，
// 数组里的每一项都必须是映射——跟 config.checkConfigShapes 同一个用意（003 §2 那条规则
// 的 override.yaml 版本），只是这份文件的字段少得多，不需要 configSequenceFields 那张表。
func checkOverrideShapes(doc *yaml.Node, p *clierr.ProblemSet) {
	if doc.Kind != yaml.MappingNode {
		p.Add("override.yaml", i18n.T(msgid.ProblemTopLevelMustBeMapping))
		return
	}
	if components := lookupOverrideNode(doc, "components"); components != nil && components.Tag != "!!null" {
		checkComponentsShape(components, "components", p)
	}
}

func checkComponentsShape(node *yaml.Node, path string, p *clierr.ProblemSet) {
	if node.Kind != yaml.SequenceNode {
		p.Add(path, i18n.T(msgid.ProblemMustBeArray, yamlcheck.KindName(node)))
		return
	}
	for i, item := range node.Content {
		itemPath := indexed(path, i)
		if item.Kind != yaml.MappingNode {
			p.Add(itemPath, i18n.T(msgid.OverrideComponentMustBeMapping))
			continue
		}
		if members := lookupOverrideNode(item, "members"); members != nil && members.Tag != "!!null" {
			checkComponentsShape(members, itemPath+".members", p)
		}
	}
}

func lookupOverrideNode(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func indexed(field string, i int) string {
	return field + "[" + itoa(i) + "]"
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
```

> **Note for the implementer:** `itoa`/`indexed` deliberately don't import `strconv`/reuse `internal/config`'s own unexported `indexed` (`internal/config/validate.go:584`, package-private, can't be imported) — this is the same small, deliberate duplication convention already established elsewhere in the codebase (e.g. `refText`-style helpers duplicated between `internal/compose` and `internal/k8s`). Feel free to use `strconv.Itoa` instead of the hand-rolled `itoa` above if you prefer — either is fine; the hand-rolled version above is written out in full only so this step has no placeholder.

- [ ] **Step 5: Run the test — it should still fail (Validate doesn't exist yet)**

Run: `go test ./internal/override/... -v`
Expected: FAIL — build error, `o.Validate undefined`.

- [ ] **Step 6: Write single-file validation**

```go
// internal/override/validate.go
package override

import (
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// Validate 校验 override.yaml 的单文件级规则：target/mode 取值、组件条目的形状、
// 组件 ID 不重复。跨文件规则（target 降级方向、k8s 下 local/debug/target 不许
// 出现、baseline 漂移）不在这里——那些需要 brickkit.yaml 才能判断，见 CheckAgainst
// 与 Drift（本包另一个文件，设计书 §5.2、§4、§7）。
func (o *Override) Validate() error {
	p := newOverrideProblems(o.Source)

	switch o.Target {
	case "", config.TargetDocker, config.TargetPodman, config.TargetK8s:
	default:
		p.Add("target", i18n.T(msgid.OverrideTargetInvalid, o.Target))
	}

	seen := map[string]bool{}
	for i, c := range o.Components {
		o.validateComponent(p, indexed("components", i), c, seen)
	}

	return p.Err()
}

func (o *Override) validateComponent(p *clierr.ProblemSet, field string, c ComponentOverride, seen map[string]bool) {
	switch {
	case c.ID == "":
		p.Missing(field + ".id")
	case seen[c.ID]:
		p.Add(field+".id", i18n.T(msgid.OverrideDuplicateComponent, c.ID))
	default:
		seen[c.ID] = true
	}

	switch c.Mode {
	case "", config.ModeEnabled, config.ModeDisable, config.ModeDebug, config.ModeLocal:
	default:
		p.Add(field+".mode", i18n.T(msgid.ConfigModeInvalid, c.Mode))
	}

	for j, m := range c.Members {
		o.validateComponent(p, field+".members["+itoa(j)+"]", m, seen)
	}
}
```

- [ ] **Step 7: Run the test — it should pass now**

Run: `go test ./internal/override/... -v`
Expected: PASS (3/3).

- [ ] **Step 8: Add negative-path tests for shape/unknown-field/enum/duplicate-ID rejection**

```go
// internal/override/override_test.go (append)

func TestParseOverrideRejectsUnknownField(t *testing.T) {
	_, err := ParseOverride([]byte(`
components:
  - id: demo/hello
    version: 1.0.0
`), "override.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version")
}

func TestParseOverrideRejectsComponentsNotArray(t *testing.T) {
	_, err := ParseOverride([]byte(`
components: demo/hello
`), "override.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "components")
}

func TestParseOverrideRejectsInvalidTarget(t *testing.T) {
	_, err := ParseOverride([]byte(`
target: swarm
`), "override.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target")
}

func TestParseOverrideAcceptsPodmanTarget(t *testing.T) {
	o, err := ParseOverride([]byte(`
target: podman
`), "override.yaml")
	require.NoError(t, err)
	assert.Equal(t, "podman", o.Target)
}

func TestParseOverrideRejectsMissingComponentID(t *testing.T) {
	_, err := ParseOverride([]byte(`
components:
  - mode: debug
`), "override.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "components[0].id")
}

func TestParseOverrideRejectsDuplicateComponentID(t *testing.T) {
	_, err := ParseOverride([]byte(`
components:
  - id: demo/hello
  - id: demo/hello
`), "override.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "demo/hello")
}

func TestParseOverrideRejectsDuplicateIDAcrossNestedMembers(t *testing.T) {
	_, err := ParseOverride([]byte(`
components:
  - id: erp/backend
    members:
      - id: people/basic
  - id: people/basic
`), "override.yaml")
	require.Error(t, err)
}

func TestParseOverrideRejectsInvalidMode(t *testing.T) {
	_, err := ParseOverride([]byte(`
components:
  - id: demo/hello
    mode: bogus
`), "override.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "components[0].mode")
}
```

- [ ] **Step 9: Run the full package test suite**

Run: `go test ./internal/override/... -v`
Expected: PASS (11/11).

- [ ] **Step 10: Add `Layout.OverridePath()` and `config.TargetPodman`**

Read `internal/config/layout.go:12-35` first. Add `FileOverride = "override.yaml"` to the existing `const` block, right after `FileGitignore`:

```go
	// FileGitignore 是项目的 .gitignore。
	FileGitignore = "gitignore"  // (unchanged — quoted here only to anchor the diff location)
	// FileOverride 是本地部署覆盖文件（override.yaml 设计书 §3）。
	// 固定名字、固定在项目根——跟 --config 指向哪份 brickkit.yaml 无关
	// （设计书 §10：这份机制只对默认 brickkit.yaml 生效，但文件本身的路径不随之改变）。
	FileOverride = "override.yaml"
```

(Leave `FileGitignore`'s own existing value exactly as it already is in the file — the snippet above only shows it for placement context; do not change its value.)

Add the accessor method right after `GitignorePath()` (`internal/config/layout.go:104`):

```go
// OverridePath 返回本地部署覆盖文件的路径。固定在项目根、固定叫 override.yaml，
// 不随 ConfigFile（--config）变化。
func (l Layout) OverridePath() string { return l.path(FileOverride) }
```

Read `internal/config/config.go:8-13` first. Add `TargetPodman` to the existing `const` block:

```go
// 部署目标（003 §3.2）。
const (
	TargetDocker = "docker"
	TargetK8s    = "k8s"
	// TargetPodman 是 override.yaml 才能选的取值（override.yaml 设计书 §5）——
	// brickkit.yaml 自身的 deploy.target 校验（validateDeploy）从不接受它；这个
	// 常量存在只是为了给 internal/override 与后续读取"已被覆盖过的 cfg.Deploy.Target"
	// 的代码一个共享的取值名字，避免到处写裸字符串 "podman"。
	TargetPodman = "podman"
	// PodSecurityRestricted 对应 K8s 官方 Pod Security Standards 的 restricted 级别。
	PodSecurityRestricted = "restricted"
)
```

- [ ] **Step 11: Add the gitignore section (inert until Task 6 wires the call site)**

Read `internal/config/scaffold.go:123-135` first, then add one entry to the slice `gitignoreSections()` returns:

```go
		{"# " + i18n.T(msgid.ConfigGitignoreComponents), []string{"components/"}},
		{"# " + i18n.T(msgid.ConfigGitignoreOverride), []string{"override.yaml"}},
		// 这两条**默认是注释掉的**：...
```

(Insert the new line between the existing `components/` entry and the commented-out caches entry, preserving everything else in the function unchanged.)

- [ ] **Step 12: Add every new msgid + catalog entry**

```go
// internal/msgid/override.go
package msgid

// internal/override 包自己的文案（override.yaml 的解析与单文件校验）。
const (
	OverrideReadFailed          = "override.read_failed"
	OverrideNotValidYAML        = "override.not_valid_yaml"
	OverrideValidationFailed    = "override.validation_failed"
	OverrideComponentMustBeMapping = "override.component_must_be_mapping"
	OverrideTargetInvalid       = "override.target_invalid"
	OverrideDuplicateComponent  = "override.duplicate_component"
)
```

Add to `internal/i18n/catalog_en.go` (anywhere among the other message entries — `gofmt -w` afterward fixes column alignment regardless of insertion point):

```go
	msgid.OverrideReadFailed:              "failed to read override.yaml",
	msgid.OverrideNotValidYAML:            "override.yaml is not valid YAML",
	msgid.OverrideValidationFailed:        "override.yaml validation failed",
	msgid.OverrideComponentMustBeMapping:  "must be a mapping (got %s)",
	msgid.OverrideTargetInvalid:           "must be one of docker/podman/k8s (or omitted), got %[1]q",
	msgid.OverrideDuplicateComponent:      "component %[1]q appears more than once in override.yaml",
```

Add to `internal/i18n/catalog_zh.go`:

```go
	msgid.OverrideReadFailed:              "override.yaml 读取失败",
	msgid.OverrideNotValidYAML:            "override.yaml 不是合法的 YAML",
	msgid.OverrideValidationFailed:        "override.yaml 校验失败",
	msgid.OverrideComponentMustBeMapping:  "必须是映射格式（当前是 %s）",
	msgid.OverrideTargetInvalid:           "只能是 docker/podman/k8s 之一（或不写），实际是 %[1]q",
	msgid.OverrideDuplicateComponent:      "组件 %[1]q 在 override.yaml 里出现了不止一次",
	msgid.ConfigGitignoreOverride:         "本地部署覆盖",
```

Add `ConfigGitignoreOverride = "config.gitignore_override"` to `internal/msgid/config.go`, right next to the existing `ConfigGitignoreComponents` constant (find its exact line with `grep -n "ConfigGitignoreComponents" internal/msgid/config.go` first), and its matching English catalog entry `msgid.ConfigGitignoreOverride: "local deployment overrides",` in `catalog_en.go`.

- [ ] **Step 13: Run `gofmt` and the full build+test**

Run: `gofmt -w internal/msgid/override.go internal/msgid/config.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go internal/config/layout.go internal/config/config.go internal/config/scaffold.go && go build ./... && go test ./internal/override/... ./internal/config/... ./tests/i18nguard/... -v`
Expected: build succeeds; all three packages' tests PASS; no i18nguard failures (every new msgid has both EN and ZH entries).

- [ ] **Step 14: Commit**

```bash
git add internal/override/ internal/config/layout.go internal/config/config.go internal/config/scaffold.go internal/msgid/override.go internal/msgid/config.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "$(cat <<'EOF'
feat: add internal/override package (override.yaml types, parsing, single-file validation)

Foundation for the override.yaml mechanism (design spec §2-§4, §8): a new,
independent package that parses and validates the file's own shape — no
brickkit.yaml awareness yet (that's cross-file validation, a later task).
A missing or empty file is always valid (opt-in, silent-by-default per §2/§3).

Also lands config.Layout.OverridePath() and config.TargetPodman (shared
vocabulary other tasks build on) and a gitignore section for override.yaml
that stays inert until brickkit override (a later task) actually calls it.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: JSON Schema generation for `override.yaml`

**Files:**
- Modify: `internal/schemagen/schemagen.go` (add `OverrideFile` const + `Override()` func + `Files()` entry)
- Modify: `internal/schemagen/schemas_test.go` (extend `documents`, `requiredGolden`, `constraintCases()`, `TestFilesNamesBothSchemas`)
- Modify: `cmd/gen-schemas/main.go` if it hardcodes anything beyond calling `schemagen.Files()` (verify — per the existing code it iterates the returned map, so it should need **no** change; confirm during Step 1's investigation, and if it does need a change, the diff is additive only)

**Interfaces:**
- Consumes: `override.Override`, `override.ComponentOverride` (Task 1).
- Produces: `schemagen.OverrideFile = "override.schema.json"`, `schemagen.Override() ([]byte, error)`, and `schemas/override.schema.json` on disk after `make generate-schemas`.

- [ ] **Step 1: Write the failing test**

Read `internal/schemagen/schemas_test.go:1-152` first (the `baselineComponent`/`baselineProject` constants and the `documents` map) to confirm exact insertion points, then add:

```go
// internal/schemagen/schemas_test.go (add near baselineProject, before the `document` type)

const baselineOverride = `
target: docker
targetBaseline: docker
components:
  - id: demo/hello
  - id: erp/backend
    members:
      - id: people/basic
      - id: auth/rbac
        mode: disable
  - id: infra/redis-event-bus
    mode: local
    localPort: 8082
  - id: payment/gateway
    mode: debug
    localPort: 9091
    baseline: local
`
```

Extend `documents` (`schemas_test.go:140-151`):

```go
var documents = map[string]document{
	"component": {
		baseline: baselineComponent,
		generate: Component,
		parse:    func(data []byte) error { _, err := manifest.Parse(data, "component.yaml"); return err },
	},
	"project": {
		baseline: baselineProject,
		generate: Project,
		parse:    func(data []byte) error { _, err := config.ParseConfig(data, "brickkit.yaml"); return err },
	},
	"override": {
		baseline: baselineOverride,
		generate: Override,
		parse:    func(data []byte) error { _, err := override.ParseOverride(data, "override.yaml"); return err },
	},
}
```

Add the import: `"github.com/brickkit/brickkit/internal/override"`.

Update `TestFilesNamesBothSchemas` (`schemas_test.go:414-418`) — rename and extend:

```go
func TestFilesNamesAllSchemas(t *testing.T) {
	files, err := Files()
	require.NoError(t, err)
	assert.Equal(t, []string{"brickkit.schema.json", "component.schema.json", "override.schema.json"}, sortedKeys(files))
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/schemagen/... -run TestFilesNamesAllSchemas -v`
Expected: FAIL — `undefined: Override` (compile error, since `Files()` doesn't produce a third entry yet and `Override` doesn't exist).

- [ ] **Step 3: Implement `Override()` in schemagen.go**

Read `internal/schemagen/schemagen.go` (already quoted in full in this plan's research — 85 lines) first, then:

```go
// internal/schemagen/schemagen.go

const (
	ComponentFile = "component.schema.json"
	ProjectFile   = "brickkit.schema.json"
	OverrideFile  = "override.schema.json"
)

// Override 返回 override.yaml 的 JSON Schema。
func Override() ([]byte, error) {
	return newGenerator(overrides()).document(reflect.TypeOf(override.Override{}), "BrickKit override.yaml")
}
```

Update `Files()`:

```go
func Files() (map[string][]byte, error) {
	component, err := Component()
	if err != nil {
		return nil, err
	}
	project, err := Project()
	if err != nil {
		return nil, err
	}
	overrideSchema, err := Override()
	if err != nil {
		return nil, err
	}
	return map[string][]byte{ComponentFile: component, ProjectFile: project, OverrideFile: overrideSchema}, nil
}
```

Add the import `"github.com/brickkit/brickkit/internal/override"`.

- [ ] **Step 4: Run the test — it should pass now**

Run: `go test ./internal/schemagen/... -run TestFilesNamesAllSchemas -v`
Expected: PASS.

- [ ] **Step 5: Run the full schemagen suite to find every other test that needs the third document**

Run: `go test ./internal/schemagen/... -v 2>&1 | tail -100`
Expected: several **new** failures — `TestRequiredFieldsMatchGoldenTable/override` (no golden entries yet, but the generated schema's `components[]` node requires `id`, so `got` will be non-empty while `want` is empty), `TestEveryConstrainedSchemaNodeHasAConstraintCase` (the generated `target`/`components[]/mode` enum nodes have no matching `constraintCases()` rows yet). Read the actual failure output — it will name the exact schema paths that need golden-table rows; that output is the authoritative list, more reliable than guessing here.

- [ ] **Step 6: Add the required-fields golden rows**

Read `internal/schemagen/schemas_test.go:434-455` (`requiredGolden`) first, then add:

```go
	{"override", "components[]", []string{"id"}, []any{"components", 0}},
	{"override", "components[]/members[]", []string{"id"}, []any{"components", 1, "members", 0}},
```

(`baselineOverride`'s `components[1]` is `erp/backend`, whose first member is `people/basic` — matching the `dataPath` used above; adjust the index if Step 1's baseline ordering differs from what actually landed in the file.)

- [ ] **Step 7: Add the constraint-case rows for `target` and `components[].mode`**

Read `internal/schemagen/schemas_test.go:592-669` (the `constraintCase` struct, `constraintCases()`, and the existing `deploy.target`/`components[0].mode` rows for the `project` doc — both already quoted in full in this plan's research) first, then add two new rows modeled directly on those two:

```go
		{
			name: "target", doc: "override", schemaPath: "target",
			dataPath: []any{"target"}, errField: "target",
			valid:   []any{config.TargetDocker, config.TargetPodman, config.TargetK8s, nil},
			invalid: []any{"swarm", "Docker", ""},
		},
		{
			name: "components[0].mode", doc: "override", schemaPath: "components[]/mode",
			dataPath: []any{"components", 3, "mode"}, errField: "components[3].mode",
			valid:   []any{config.ModeEnabled, config.ModeDisable, config.ModeDebug, config.ModeLocal, nil},
			invalid: []any{"Enabled", "disabled", "debugging", "docker"},
		},
```

(`components[3]` is `payment/gateway` in `baselineOverride` — the one entry that already carries a `mode` value, giving `mutate`'s required-field-fuzzing something real to overwrite; adjust the index to match wherever that entry actually landed if Step 1's baseline differs.)

Update the `doc` field's comment at `constraintCase.doc` (`schemas_test.go:594`) from `// "component" | "project"` to `// "component" | "project" | "override"`.

- [ ] **Step 8: Run the full schemagen suite**

Run: `go test ./internal/schemagen/... -v`
Expected: PASS, all tests green (including `TestEveryUnmarshalerTypeIsCoveredByAnOverride`, which needs no change — `Override`/`ComponentOverride` have no custom `UnmarshalYAML`, so the reflection-based generator handles them without an entry in `overrides()`).

- [ ] **Step 9: Generate the real schema file and verify `make check-schemas` (the CI drift guard) is happy**

Run: `go run ./cmd/gen-schemas && git status --short schemas/`
Expected: `schemas/override.schema.json` appears as a new untracked file; `schemas/component.schema.json` and `schemas/brickkit.schema.json` show **no** diff (this task must not change either existing schema's content).

Run: `make check-schemas`
Expected: passes (the freshly-generated files on disk already match what `schemagen.Files()` produces, since Step 9's `go run` just wrote them).

- [ ] **Step 10: Commit**

```bash
git add internal/schemagen/schemagen.go internal/schemagen/schemas_test.go schemas/override.schema.json
git commit -m "$(cat <<'EOF'
feat: generate a JSON Schema for override.yaml

Same reflection-based generator that already covers component.yaml and
brickkit.yaml (internal/schemagen), extended with a third document —
editors get the same completion/typo-detection for override.yaml that the
other two files already have. Cross-checked against internal/override's
own validator by the same golden-table machinery schemas_test.go already
runs for the other two documents, so the three can never silently drift
apart.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Cross-file validation and drift detection

**Files:**
- Create: `internal/override/check.go`
- Create: `internal/override/check_test.go`
- Modify: `internal/msgid/override.go` (more constants)
- Modify: `internal/i18n/catalog_en.go`, `internal/i18n/catalog_zh.go`

**Interfaces:**
- Consumes: `*config.Config` (from `internal/config`, already parsed+validated), `*override.Override` (Task 1, already single-file-validated).
- Produces (for Task 4's wiring and for Plan 2b's `lint`/`brickkit override` consumers):
  - `func CheckAgainst(cfg *config.Config, ov *Override) error` — nil when `ov` is nil or fully consistent; otherwise a single `*clierr.Error` covering every problem found in one pass (dangling entries, target-upgrade attempts, k8s-forbids-local/debug/target-override). Never mutates either argument.
  - `type DriftNote struct { Field, Message string }`
  - `func Drift(cfg *config.Config, ov *Override) []DriftNote` — never an error, always safe to ignore; empty/nil `ov` → empty slice.

- [ ] **Step 1: Write the failing tests for `CheckAgainst`**

```go
// internal/override/check_test.go
package override

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/config"
)

func dockerConfig(components ...config.Component) *config.Config {
	return &config.Config{
		Project: "p",
		Deploy:  config.Deploy{Target: config.TargetDocker},
		Components: components,
	}
}

func k8sConfig(components ...config.Component) *config.Config {
	return &config.Config{
		Project: "p",
		Deploy:  config.Deploy{Target: config.TargetK8s},
		Components: components,
	}
}

func TestCheckAgainstNilOverrideIsAlwaysValid(t *testing.T) {
	assert.NoError(t, CheckAgainst(dockerConfig(), nil))
}

func TestCheckAgainstRejectsDanglingComponentEntry(t *testing.T) {
	cfg := dockerConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Components: []ComponentOverride{{ID: "demo/gone"}}}

	err := CheckAgainst(cfg, ov)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "demo/gone")
}

func TestCheckAgainstRejectsDanglingNestedMember(t *testing.T) {
	cfg := dockerConfig(config.Component{ID: "erp/backend", Version: "1.0.0"})
	ov := &Override{Components: []ComponentOverride{
		{ID: "erp/backend", Members: []ComponentOverride{{ID: "demo/gone"}}},
	}}

	err := CheckAgainst(cfg, ov)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "demo/gone")
}

func TestCheckAgainstAllowsDowngradeFromK8sToDocker(t *testing.T) {
	cfg := k8sConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Target: config.TargetDocker, TargetBaseline: config.TargetK8s}

	assert.NoError(t, CheckAgainst(cfg, ov))
}

func TestCheckAgainstAllowsDowngradeFromK8sToPodman(t *testing.T) {
	cfg := k8sConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Target: config.TargetPodman, TargetBaseline: config.TargetK8s}

	assert.NoError(t, CheckAgainst(cfg, ov))
}

func TestCheckAgainstAllowsDockerToPodmanEitherDirection(t *testing.T) {
	assert.NoError(t, CheckAgainst(dockerConfig(), &Override{Target: config.TargetPodman}))

	podmanCfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetPodman}}
	assert.NoError(t, CheckAgainst(podmanCfg, &Override{Target: config.TargetDocker}))
}

func TestCheckAgainstRejectsUpgradeFromDockerToK8s(t *testing.T) {
	cfg := dockerConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Target: config.TargetK8s}

	err := CheckAgainst(cfg, ov)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "docker")
	assert.Contains(t, err.Error(), "k8s")
}

func TestCheckAgainstRejectsUpgradeFromPodmanToK8s(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetPodman}}
	ov := &Override{Target: config.TargetK8s}

	err := CheckAgainst(cfg, ov)

	require.Error(t, err)
}

func TestCheckAgainstRejectsDebugModeWhenEffectiveTargetIsK8s(t *testing.T) {
	cfg := k8sConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Components: []ComponentOverride{{ID: "demo/hello", Mode: config.ModeDebug}}}

	err := CheckAgainst(cfg, ov)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "demo/hello")
}

func TestCheckAgainstRejectsLocalModeWhenEffectiveTargetIsK8s(t *testing.T) {
	cfg := k8sConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Components: []ComponentOverride{{ID: "demo/hello", Mode: config.ModeLocal}}}

	err := CheckAgainst(cfg, ov)

	require.Error(t, err)
}

func TestCheckAgainstAllowsDebugModeWhenTargetDowngradedAwayFromK8s(t *testing.T) {
	// brickkit.yaml 自身是 k8s，但这份覆盖同时把 target 降级到 docker——
	// 那么"k8s 禁止 debug/local"这条规则不该再拦它：生效的目标已经不是 k8s 了。
	cfg := k8sConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{
		Target: config.TargetDocker, TargetBaseline: config.TargetK8s,
		Components: []ComponentOverride{{ID: "demo/hello", Mode: config.ModeDebug}},
	}

	assert.NoError(t, CheckAgainst(cfg, ov))
}

func TestCheckAgainstAllowsDebugModeUnderDockerTarget(t *testing.T) {
	cfg := dockerConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Components: []ComponentOverride{{ID: "demo/hello", Mode: config.ModeDebug}}}

	assert.NoError(t, CheckAgainst(cfg, ov))
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/override/... -run TestCheckAgainst -v`
Expected: FAIL — `undefined: CheckAgainst`.

- [ ] **Step 3: Implement `CheckAgainst`**

```go
// internal/override/check.go
package override

import (
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// CheckAgainst 校验 override.yaml 相对 brickkit.yaml 的跨文件规则（设计书 §5.2、§4、
// §7 的"悬空条目"）：target 只能降级、生效目标是 k8s 时不许再有 local/debug/target
// 覆盖、每个组件条目都必须能在 brickkit.yaml 里找到对应的组件。一次收集全部问题
// 报出，不遇错即返回——跟 config.Config.Validate() 同一个约定。
//
// ov 为 nil 时总是合法（没有覆盖，谈不上跟 brickkit.yaml 冲突）。
func CheckAgainst(cfg *config.Config, ov *Override) error {
	if ov == nil {
		return nil
	}

	p := clierr.NewProblemSet(clierr.CodeConfigInvalid, i18n.T(msgid.OverrideCheckFailed)).
		WithSource(i18n.T(msgid.LabelFile), ov.Source)

	effectiveTarget := cfg.Deploy.Target
	if ov.Target != "" {
		if !isDowngrade(cfg.Deploy.Target, ov.Target) {
			p.Add("target", i18n.T(msgid.OverrideTargetUpgradeRejected, cfg.Deploy.Target, ov.Target))
		}
		effectiveTarget = ov.Target
	}

	known := map[string]bool{}
	for _, c := range cfg.Components {
		known[c.ID] = true
	}

	checkOverrideComponents(p, ov.Components, known, effectiveTarget)

	return p.Err()
}

// isDowngrade 判断从 from 切到 to 是不是设计书 §5.2 允许的方向：
// k8s → docker/podman 总是允许；docker/podman → k8s 总是拒绝；
// docker ↔ podman 双向不受限（结构上都只生成 compose 文件，谁都不缺对方需要的字段）。
func isDowngrade(from, to string) bool {
	if to == config.TargetK8s {
		return from == config.TargetK8s
	}
	return true
}

func checkOverrideComponents(p *clierr.ProblemSet, entries []ComponentOverride, known map[string]bool, effectiveTarget string) {
	for _, c := range entries {
		if !known[c.ID] {
			p.Add(c.ID, i18n.T(msgid.OverrideDanglingComponent, c.ID))
		}
		if effectiveTarget == config.TargetK8s && (c.Mode == config.ModeDebug || c.Mode == config.ModeLocal) {
			p.Add(c.ID+".mode", i18n.T(msgid.OverrideModeK8sForbidden, c.Mode, c.ID))
		}
		checkOverrideComponents(p, c.Members, known, effectiveTarget)
	}
}
```

- [ ] **Step 4: Add the new msgids**

Add to `internal/msgid/override.go`:

```go
	OverrideCheckFailed          = "override.check_failed"
	OverrideTargetUpgradeRejected = "override.target_upgrade_rejected"
	OverrideDanglingComponent    = "override.dangling_component"
	OverrideModeK8sForbidden     = "override.mode_k8s_forbidden"
```

Add to `internal/i18n/catalog_en.go`:

```go
	msgid.OverrideCheckFailed:           "override.yaml is inconsistent with brickkit.yaml",
	msgid.OverrideTargetUpgradeRejected: "brickkit.yaml's own deploy.target is %[1]q — override.yaml may only downgrade from it, never upgrade to %[2]q. To use %[2]q, declare it directly in deploy.target",
	msgid.OverrideDanglingComponent:     "%[1]q is not one of brickkit.yaml's declared components — remove this entry, or re-run brickkit override to refresh the file",
	msgid.OverrideModeK8sForbidden:      "mode: %[1]q is not allowed while the effective target is k8s — a cluster Pod cannot run a bare process (component: %[2]q)",
```

Add to `internal/i18n/catalog_zh.go`:

```go
	msgid.OverrideCheckFailed:           "override.yaml 与 brickkit.yaml 对不上",
	msgid.OverrideTargetUpgradeRejected: "brickkit.yaml 自己的 deploy.target 是 %[1]q——override.yaml 只能从它往下降级，不能升级到 %[2]q。要用 %[2]q，得直接在 deploy.target 里声明它",
	msgid.OverrideDanglingComponent:     "%[1]q 不是 brickkit.yaml 里声明过的组件——删掉这一条，或者重新跑一遍 brickkit override 刷新这份文件",
	msgid.OverrideModeK8sForbidden:      "生效目标是 k8s 时不许写 mode: %[1]q——集群里的 Pod 没法跑一个裸进程（组件：%[2]q）",
```

- [ ] **Step 5: Run the tests — they should pass now**

Run: `go test ./internal/override/... -v`
Expected: PASS (all tests from Tasks 1-3 combined).

- [ ] **Step 6: Write the failing tests for `Drift`**

```go
// internal/override/check_test.go (append)

func TestDriftNilOverrideIsEmpty(t *testing.T) {
	assert.Empty(t, Drift(dockerConfig(), nil))
}

func TestDriftDetectsTargetBaselineMismatch(t *testing.T) {
	cfg := k8sConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Target: config.TargetDocker, TargetBaseline: config.TargetDocker} // brickkit.yaml 已经从 docker 变成了 k8s

	notes := Drift(cfg, ov)

	require.Len(t, notes, 1)
	assert.Equal(t, "target", notes[0].Field)
}

func TestDriftNoNoteWhenTargetBaselineMatchesCurrent(t *testing.T) {
	cfg := k8sConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Target: config.TargetDocker, TargetBaseline: config.TargetK8s}

	assert.Empty(t, Drift(cfg, ov))
}

func TestDriftNoNoteWhenTargetOverrideNotSet(t *testing.T) {
	// 没写 target 就没有 targetBaseline，也就没有可比的东西。
	cfg := k8sConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Components: []ComponentOverride{{ID: "demo/hello", Mode: config.ModeDebug}}}

	assert.Empty(t, Drift(cfg, ov))
}

func TestDriftDetectsComponentBaselineMismatch(t *testing.T) {
	cfg := dockerConfig(config.Component{ID: "payment/gateway", Version: "1.0.0", Mode: config.ModeLocal})
	ov := &Override{Components: []ComponentOverride{
		{ID: "payment/gateway", Mode: config.ModeDebug, Baseline: config.ModeEnabled},
	}}

	notes := Drift(cfg, ov)

	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Field, "payment/gateway")
}

func TestDriftNoNoteWhenComponentBaselineMatchesCurrent(t *testing.T) {
	cfg := dockerConfig(config.Component{ID: "payment/gateway", Version: "1.0.0", Mode: config.ModeLocal})
	ov := &Override{Components: []ComponentOverride{
		{ID: "payment/gateway", Mode: config.ModeDebug, Baseline: config.ModeLocal},
	}}

	assert.Empty(t, Drift(cfg, ov))
}

func TestDriftNoNoteWhenComponentBaselineNotRecorded(t *testing.T) {
	// baseline 只在 brickkit.yaml 当时已经显式写了取值时才落笔（设计书 §7）——
	// 没有 baseline 就没有"漂移"这回事，不是"缺失的旧值等于空字符串"。
	cfg := dockerConfig(config.Component{ID: "payment/gateway", Version: "1.0.0"})
	ov := &Override{Components: []ComponentOverride{{ID: "payment/gateway", Mode: config.ModeDebug}}}

	assert.Empty(t, Drift(cfg, ov))
}
```

- [ ] **Step 7: Run to verify failure**

Run: `go test ./internal/override/... -run TestDrift -v`
Expected: FAIL — `undefined: Drift`, `undefined: DriftNote`.

- [ ] **Step 8: Implement `Drift`**

```go
// internal/override/check.go (append)

// DriftNote 是一条非阻断的漂移提示（设计书 §7）：override.yaml 记录的 baseline
// 与 brickkit.yaml 的当前值对不上了，值得回头看看这份覆盖是否还合理，但从不
// 拦任何命令。
type DriftNote struct {
	// Field 是 "target" 或某个组件 ID，供调用方（brickkit override 的刷新提示、
	// 后续 lint 规则）拼自己的展示格式。
	Field string
	// Message 是面向使用者的一句话说明。
	Message string
}

// Drift 找出 override.yaml 记录的 baseline 与 brickkit.yaml 当前值不一致的地方。
// 从不返回 error——这是提示，不是校验失败。ov 为 nil 时返回空。
func Drift(cfg *config.Config, ov *Override) []DriftNote {
	if ov == nil {
		return nil
	}

	var notes []DriftNote
	if ov.TargetBaseline != "" && ov.TargetBaseline != cfg.Deploy.Target {
		notes = append(notes, DriftNote{
			Field: "target",
			Message: i18n.T(msgid.OverrideTargetDrift, ov.TargetBaseline, cfg.Deploy.Target),
		})
	}

	current := map[string]string{}
	for _, c := range cfg.Components {
		current[c.ID] = c.Mode
	}
	notes = append(notes, componentDrift(ov.Components, current)...)
	return notes
}

func componentDrift(entries []ComponentOverride, current map[string]string) []DriftNote {
	var notes []DriftNote
	for _, c := range entries {
		if c.Baseline != "" && c.Baseline != current[c.ID] {
			notes = append(notes, DriftNote{
				Field:   c.ID,
				Message: i18n.T(msgid.OverrideComponentDrift, c.ID, c.Baseline, current[c.ID]),
			})
		}
		notes = append(notes, componentDrift(c.Members, current)...)
	}
	return notes
}
```

Add to `internal/msgid/override.go`:

```go
	OverrideTargetDrift    = "override.target_drift"
	OverrideComponentDrift = "override.component_drift"
```

Add to `internal/i18n/catalog_en.go`:

```go
	msgid.OverrideTargetDrift:    "brickkit.yaml's deploy.target changed from %[1]q to %[2]q since this override was last confirmed — worth reviewing whether target in override.yaml still makes sense",
	msgid.OverrideComponentDrift: "%[1]q's mode in brickkit.yaml changed from %[2]q to %[3]q since this override was last confirmed",
```

Add to `internal/i18n/catalog_zh.go`:

```go
	msgid.OverrideTargetDrift:    "brickkit.yaml 的 deploy.target 从 %[1]q 变成了 %[2]q（相对这份覆盖上次确认时），值得回头看看 override.yaml 里的 target 还合不合适",
	msgid.OverrideComponentDrift: "%[1]q 在 brickkit.yaml 里的 mode 从 %[2]q 变成了 %[3]q（相对这份覆盖上次确认时）",
```

- [ ] **Step 9: Run the full package suite**

Run: `gofmt -w internal/override/ internal/msgid/override.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go && go build ./... && go test ./internal/override/... ./tests/i18nguard/... -v`
Expected: build succeeds, all tests PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/override/check.go internal/override/check_test.go internal/msgid/override.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "$(cat <<'EOF'
feat: cross-file validation and drift detection for override.yaml

CheckAgainst (design spec §5.2, §4, §7's dangling-entry rule) needs both
files to judge: target downgrade-only direction, k8s forbidding
local/debug/target-override once it's the *effective* target (whether
from brickkit.yaml directly or because override.yaml left target unset),
and every override.yaml component entry actually existing in
brickkit.yaml. Drift (§7's baseline-mismatch rule) is separate and never
blocking — it just flags when brickkit.yaml changed since an override was
last confirmed.

Both are pure functions with no CLI wiring yet — Task 4 wires CheckAgainst
into up/sync/status; a later plan (2b) wires Drift into brickkit override's
refresh output and a new lint rule.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Apply mechanism + wiring into `up`/`sync`/`status` + multi-environment guard

**Files:**
- Modify: `internal/cli/topology.go` (add `loadOverride`/`applyOverride`/`flattenOverrides`)
- Modify: `internal/cli/up.go:185-194` (call the new functions right after `ParseConfigFile`)
- Modify: `internal/cli/sync.go:67-73` (same)
- Modify: `internal/cli/lifecycle.go:80-93` (same, inside `loadProject`)
- Modify: `internal/cli/testsupport_component_test.go` (add `writeOverride` helper)
- Create: `internal/cli/override_apply_test.go`
- Modify: `internal/msgid/cli_override.go` (new file — CLI-layer messages)
- Modify: `internal/i18n/catalog_en.go`, `internal/i18n/catalog_zh.go`

**Interfaces:**
- Consumes: `override.ParseOverrideFile`, `override.CheckAgainst`, `override.Override`, `override.ComponentOverride` (Tasks 1 & 3); `config.Layout.OverridePath()` (Task 1); `Options.ConfigPath`, `DefaultConfigFile` (existing, `internal/cli/root.go:30,46`).
- Produces: `func loadOverride(opts *Options, layout config.Layout, cfg *config.Config) (*override.Override, error)`, `func applyOverride(cfg *config.Config, ov *override.Override)` — both in `internal/cli/topology.go`, used identically by `up.go`, `sync.go`, `lifecycle.go`.

- [ ] **Step 1: Write the failing test for the `writeOverride` test helper + a basic mode override taking effect via `up --dry-run`**

Read `internal/cli/testsupport_component_test.go:163-211` (`projectFixture`, `writeConfig`) first, then add:

```go
// internal/cli/testsupport_component_test.go (append, right after writeConfig)

// writeOverride 把 override.yaml 写进项目目录——跟 writeConfig 同一个手法，
// 但 override.yaml 没有 writeConfig 那份固定的 header/sources 前缀要重现
// （override.yaml 设计书 §8 的示例本身就是完整文件，没有任何隐藏结构）。
func (f *projectFixture) writeOverride(t *testing.T, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(f.Dir, "override.yaml"), []byte(body), 0o644))
}
```

```go
// internal/cli/override_apply_test.go
package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

func TestUpDryRunAppliesModeOverrideFromOverrideYAML(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: local
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	// mode: local 意味着不生成容器；services 段里因此不会出现 demo-hello。
	assert.NotContains(t, r.stdout, "demo-hello-1-0-0")
}

func TestUpDryRunIgnoresOverrideYamlWithoutIt(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	// 没有 override.yaml——跟今天完全一样的行为。

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

func TestUpBlocksOnDanglingOverrideEntry(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/gone
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "demo/gone")
}

func TestUpRejectsTargetUpgradeViaOverride(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`) // deploy.target: docker，来自 configHeader
	f.writeOverride(t, `target: k8s`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "k8s")
}

func TestUpWarnsAndIgnoresOverrideWhenConfigPointsElsewhere(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: local
`)
	// brickkit.other.yaml 是同一个项目的第二份配置文件（同一个 f.Dir，同一套
	// f.Sources，同一个 .brickkit/manifests/ 缓存）——只是换了个文件名，测的是
	// --config 指到非默认文件时 override.yaml 被忽略这件事本身。组装方式
	// 照抄 writeConfig 自己的拼法（configHeader + sources 块 + body），
	// 这样它才是一份真正能被 ParseConfigFile 解析、能把 demo/hello 解析到
	// 同一份缓存 Manifest 的项目文件，不是随手糊出来的半成品。
	var other strings.Builder
	other.WriteString(configHeader)
	if len(f.Sources) > 0 {
		other.WriteString("\nsources:\n")
		for _, s := range f.Sources {
			other.WriteString(s)
		}
	}
	other.WriteString("\ncomponents:\n  - id: demo/hello\n    version: 1.0.0\n")
	require.NoError(t, os.WriteFile(
		filepath.Join(f.Dir, "brickkit.other.yaml"), []byte(other.String()), 0o644))

	r := runIn(t, f.Dir, "up", "--dry-run", "--config", "brickkit.other.yaml")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "override.yaml")
	// 没生效：mode: local 没有被应用，demo-hello 仍然作为容器出现
	assert.Contains(t, r.stdout, "demo-hello-1-0-0")
}
```

> **Note:** `configHeader` and the `f.Sources` assembly above mirror `writeConfig`'s own exact logic verbatim (`internal/cli/testsupport_component_test.go:171-177,201-209`, already quoted in full earlier in this plan's research) — this is deliberate, not a shortcut: `brickkit.other.yaml` needs the same `sources:` block as the fixture's real `brickkit.yaml` so `demo/hello` resolves against the same cached Manifest, rather than failing for an unrelated reason (no source configured) that would make this test's actual failure message misleading. `override_apply_test.go` is a new file — give it its own import block including `"os"`, `"path/filepath"`, and `"strings"`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/... -run 'TestUpDryRunAppliesModeOverride|TestUpDryRunIgnoresOverrideYaml|TestUpBlocksOnDangling|TestUpRejectsTargetUpgrade|TestUpWarnsAndIgnores' -v`
Expected: FAIL — either a compile error (`writeOverride` undefined) if Step 1's helper addition to `testsupport_component_test.go` hasn't landed yet in the same commit, or (once that compiles) behavioral failures since `up` doesn't read `override.yaml` at all yet — `TestUpDryRunAppliesModeOverrideFromOverrideYAML` fails because `demo-hello-1-0-0` still appears (mode: local wasn't applied); `TestUpBlocksOnDanglingOverrideEntry` and `TestUpRejectsTargetUpgradeViaOverride` fail because `up` exits `ExitOK` instead of `ExitError` (the file is silently never read).

- [ ] **Step 3: Implement `loadOverride`/`applyOverride`/`flattenOverrides` in `topology.go`**

Read `internal/cli/topology.go` in full (already quoted in this plan's research — 50 lines) first, then add:

```go
// internal/cli/topology.go (append; add "os" and the override import to the existing import block)

// loadOverride 按 override.yaml 设计书 §10 的多环境护栏读取并校验 override.yaml：
// 只有针对**默认** brickkit.yaml 的这次运行才应用它——--config 指到别处时，
// 存在的 override.yaml 会被忽略，并且必须明说一声（既不能悄悄生效，也不能悄悄
// 不提，两种沉默都会让使用者对"这次到底生效了什么"产生错误的预期）。
//
// 文件不存在时返回 (nil, nil)：没有覆盖是完全合法、最常见的状态。
func loadOverride(opts *Options, layout config.Layout, cfg *config.Config) (*override.Override, error) {
	if opts.ConfigPath != "" && opts.ConfigPath != DefaultConfigFile {
		if _, err := os.Stat(layout.OverridePath()); err == nil {
			opts.Printf("%s\n", i18n.T(msgid.OverrideIgnoredNonDefaultConfig, layout.OverridePath()))
		}
		return nil, nil
	}

	ov, err := override.ParseOverrideFile(layout.OverridePath())
	if err != nil || ov == nil {
		return ov, err
	}
	if err := override.CheckAgainst(cfg, ov); err != nil {
		return nil, err
	}
	return ov, nil
}

// applyOverride 把 override.yaml 的取值写进内存里的 cfg，从不写回磁盘——
// 跟 clearServedBy 同一个手法：一次性改完，下游 resolver / cascade / compose /
// k8s 全部只读 cfg 本身的字段，不需要单独知道"这个值是不是被覆盖过"。
//
// ov 为 nil 时什么都不做（调用方在没有 override.yaml 时无条件调用它也是安全的）。
func applyOverride(cfg *config.Config, ov *override.Override) {
	if ov == nil {
		return
	}
	if ov.Target != "" {
		cfg.Deploy.Target = ov.Target
	}

	overrides := map[string]override.ComponentOverride{}
	flattenOverrides(ov.Components, overrides)
	for i := range cfg.Components {
		o, ok := overrides[cfg.Components[i].ID]
		if !ok {
			continue
		}
		if o.Mode != "" {
			cfg.Components[i].Mode = o.Mode
		}
		if o.LocalPort != 0 {
			cfg.Components[i].LocalPort = o.LocalPort
		}
	}
}

// flattenOverrides 把嵌套在外壳 members 下的条目展平成一份按组件 ID 索引的表——
// apply 只关心"这个 ID 有没有被覆盖"，不关心它在 override.yaml 里嵌在哪个外壳
// 下面（那层嵌套纯粹是给人看的分组，设计书 §8）。
func flattenOverrides(entries []override.ComponentOverride, out map[string]override.ComponentOverride) {
	for _, c := range entries {
		out[c.ID] = c
		flattenOverrides(c.Members, out)
	}
}
```

Add imports to `topology.go`'s existing import block: `"os"` and `"github.com/brickkit/brickkit/internal/i18n"`, `"github.com/brickkit/brickkit/internal/msgid"`, `"github.com/brickkit/brickkit/internal/override"`.

- [ ] **Step 4: Add the new msgid**

Create `internal/msgid/cli_override.go`:

```go
package msgid

// brickkit up / sync / status 读取 override.yaml 时用到的文案。
// brickkit override 命令自己的帮助文本（Short/Long/Example）在它自己的任务里
// 补，不在这里——这份文件只装"读取 override.yaml 这个动作本身"产生的文案。
const (
	OverrideIgnoredNonDefaultConfig = "cli.override.ignored_non_default_config"
)
```

Add to `internal/i18n/catalog_en.go`:

```go
	msgid.OverrideIgnoredNonDefaultConfig: "Note: override.yaml exists but is ignored — this run uses --config %[1]s, and override.yaml only ever applies to the default brickkit.yaml",
```

Add to `internal/i18n/catalog_zh.go`:

```go
	msgid.OverrideIgnoredNonDefaultConfig: "提示：override.yaml 存在，但这次没有生效——这次用的是 --config %[1]s，override.yaml 只对默认的 brickkit.yaml 生效",
```

- [ ] **Step 5: Wire `up.go`**

Read `internal/cli/up.go:185-196` first (already quoted in full in this plan's research), then edit:

```go
// internal/cli/up.go, inside buildUpPlan — replace the block from
// "cfg, err := config.ParseConfigFile(...)" through the `if flags.ignoreServedBy` block:

	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return nil, err
	}
	ov, err := loadOverride(opts, layout, cfg)
	if err != nil {
		return nil, err
	}
	applyOverride(cfg, ov)
	if flags.ignoreServedBy {
		clearServedBy(cfg)
		opts.Printf("%s\n", i18n.T(msgid.CliUpAllServedbyDeclarationsAreIgnored))
	}
```

(This must land **before** line 207's `opts.Printf(..., cfg.Deploy.Target)` banner and **before** line 216's `if cfg.Deploy.Target == config.TargetK8s` pre-flight check, both of which need to see the already-overridden target — inserting it immediately after the `ParseConfigFile` error check, as shown, satisfies both.)

- [ ] **Step 6: Wire `sync.go`**

Read `internal/cli/sync.go:61-79` first (already quoted in full), then edit:

```go
// internal/cli/sync.go, inside runSync:

	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return err
	}
	ov, err := loadOverride(opts, layout, cfg)
	if err != nil {
		return err
	}
	applyOverride(cfg, ov)

	keep, err := syncFocus(ctx, opts, layout, cfg)
```

- [ ] **Step 7: Wire `lifecycle.go` (`status` only — `down` stays untouched since it calls `loadConfig` directly, never `loadProject`)**

Read `internal/cli/lifecycle.go:80-93` first (already quoted in full), then edit `loadProject`:

```go
// internal/cli/lifecycle.go

func loadProject(ctx context.Context, opts *Options) (*project, error) {
	p, err := loadConfig(opts)
	if err != nil {
		return nil, err
	}
	ov, err := loadOverride(opts, p.layout, p.cfg)
	if err != nil {
		return nil, err
	}
	applyOverride(p.cfg, ov)
	if len(p.cfg.Components) == 0 {
		return p, nil
	}
	if err := p.resolve(ctx, opts); err != nil {
		p.graph, p.states, p.order = nil, nil, nil
		p.degraded = clierr.As(err)
	}
	return p, nil
}
```

- [ ] **Step 8: Run the Task 4 tests — they should pass now**

Run: `go test ./internal/cli/... -run 'TestUpDryRunAppliesModeOverride|TestUpDryRunIgnoresOverrideYaml|TestUpBlocksOnDangling|TestUpRejectsTargetUpgrade|TestUpWarnsAndIgnores' -v`
Expected: PASS (5/5).

- [ ] **Step 9: Write and run a `sync` and a `status` override-integration test**

```go
// internal/cli/override_apply_test.go (append)

func TestSyncAppliesModeOverrideFromOverrideYAML(t *testing.T) {
	f := addedProject(t, []comp{
		{ID: "demo/hello", Version: "1.0.0"},
		{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
	}, "demo/hello@1.0.0", "demo/caller@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
  - id: demo/caller
    version: 1.0.0
    mode: disable
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: debug
  - id: demo/caller
`)

	r := runIn(t, f.Dir, "sync")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	f.assertActive(t, "demo/hello")
	f.assertArchived(t, "demo/caller")
}

func TestStatusReflectsModeOverrideFromOverrideYAML(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: disable
`)

	r := runIn(t, f.Dir, "status")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "demo/hello")
}
```

> **Note:** `f.assertActive`/`f.assertArchived` are `syncFixture` methods (see `internal/cli/sync_test.go`), not `projectFixture` methods — `addedProject` returns a `*projectFixture`, so `TestSyncAppliesModeOverrideFromOverrideYAML` above needs whatever this repo's existing sync tests use instead (likely `newSyncFixture` rather than `addedProject`, since `newSyncFixture` is what wraps a `*projectFixture` into a `*syncFixture` with those two assertion helpers — see `internal/cli/sync_test.go:29-44`, quoted in full in this plan's research). Adjust this test to use `newSyncFixture` and its `*syncFixture` return type if `assertActive`/`assertArchived` don't resolve against a bare `*projectFixture`.

Run: `go test ./internal/cli/... -run 'TestSyncAppliesModeOverride|TestStatusReflectsModeOverride' -v`
Expected: FAIL first (behavior not wired for these two paths' actual assertions — though Step 6/7 already wired the code, so this may in fact PASS immediately; if it does, that's fine — it's still real proof, just proof that arrived a step early because Steps 6-7 already did the wiring these two paths needed). If it passes immediately, skip ahead; if a compile error surfaces from the `assertActive`/`assertArchived` note above, fix the fixture type first, then re-run.

- [ ] **Step 10: Run the full `internal/cli` suite**

Run: `go test ./internal/cli/... 2>&1 | tail -60`
Expected: `ok` — every pre-existing test in the package still passes (this task must not change behavior for any project without an `override.yaml`).

- [ ] **Step 11: Run `make check-i18n`**

Run: `go test ./tests/i18nguard/... -v`
Expected: PASS.

- [ ] **Step 12: Commit**

```bash
git add internal/cli/topology.go internal/cli/up.go internal/cli/sync.go internal/cli/lifecycle.go internal/cli/testsupport_component_test.go internal/cli/override_apply_test.go internal/msgid/cli_override.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "$(cat <<'EOF'
feat: wire override.yaml into up/sync/status

applyOverride mutates the in-memory *config.Config exactly like the
existing clearServedBy (--ignore-served-by) pattern — a single mutation
point before resolveTopology runs, so internal/cascade/resolver/compose/k8s
need zero changes: they already only read cfg.Deploy.Target/Mode, never
caring where the value came from.

loadOverride enforces the multi-environment guard (design spec §10): only
the default brickkit.yaml run applies override.yaml; --config pointing
elsewhere warns and ignores it, never silently either way. It also runs
CheckAgainst (Task 3) before anything is applied, so a dangling entry or an
attempted target upgrade blocks the command with a clear error rather than
silently no-opping.

status wires in via loadProject only, not loadConfig — down stays
untouched (design spec §9's table doesn't list it, and it never reads
per-component Mode for anything functional).

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: `target: podman` engine selection

**Files:**
- Modify: `internal/cli/up_k8s.go:157-171` (`resolveEngineFor`)
- Modify: `internal/cli/up.go:173`, `internal/cli/up.go:477-503` (`generate`), `internal/cli/up.go:800-822` (`resolveEngine`/`engineName`)
- Modify: `internal/compose/local.go:36-37` (`EngineDocker` — add sibling `EnginePodman`)
- Create: `internal/cli/podman_target_test.go`
- Modify: `internal/msgid/cli_override.go`
- Modify: `internal/i18n/catalog_en.go`, `internal/i18n/catalog_zh.go`

**Interfaces:**
- Consumes: `config.TargetPodman` (Task 1), `applyOverride`'s ability to set `cfg.Deploy.Target = "podman"` (Task 4).
- Produces: `resolveEngineFor` gains a third branch; `engineName(opts, cfg)` (signature change — was `engineName(opts)`) returns `compose.EnginePodman` when the effective target is podman.

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/podman_target_test.go
package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
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

// 真跑（非 --dry-run）时，target: podman 必须清楚地报错，而不是悄悄退回 docker、
// 也不是对着 nil 的 engine.Engine panic——005 §7 挪掉的那份 Podman engine.Engine
// 实现还没有回来（override.yaml 设计书 §11 明确排除在这份计划之外）。
func TestUpRealRunFailsClearlyWithPodmanTarget(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `target: podman`)

	r := runIn(t, f.Dir, "up")

	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "podman")
}
```

- [ ] **Step 2: Run the tests to verify their current behavior**

Run: `go test ./internal/cli/... -run 'TestUpDryRunSucceedsWithPodmanTarget|TestUpRealRunFailsClearlyWithPodmanTarget' -v`
Expected: `TestUpDryRunSucceedsWithPodmanTarget` likely already **passes** (dry-run's `generate()` branches only on `== config.TargetK8s`, so `podman` already falls into the ordinary compose-generation path with zero code changes — this is expected and fine, not a bug in the test). `TestUpRealRunFailsClearlyWithPodmanTarget` FAILS: today's `resolveEngine(opts)` (called unconditionally, target-blind) falls through to `engine.Detect()`, which picks whatever's actually installed on the test machine (in CI, neither docker nor podman binaries exist in `PATH` for this sandboxed test run — confirm the actual failure message before assuming; either way, the assertion `assert.Contains(t, r.stderr, "podman")` fails because nothing in today's code path ever mentions "podman" for this scenario).

- [ ] **Step 3: Add `EnginePodman` to `internal/compose`**

Read `internal/compose/local.go:33-37` first, then:

```go
// EngineDocker 与 EnginePodman 是 compose.Options.Engine 目前能取的两个值。
// 只影响生成文件里记录的引擎名（见 hostGateway 已经预留的、但目前对两者
// 一视同仁的引擎参数）——podman 自己真正的 engine.Engine 实现不在这里
// （override.yaml 设计书 §11，另一份计划的事）。
const (
	EngineDocker = "docker"
	EnginePodman = "podman"
)
```

- [ ] **Step 4: Update `engineName` to be target-aware**

Read `internal/cli/up.go:813-822` first (already quoted in full), then:

```go
// internal/cli/up.go

// engineName 是生成部署文件时记录的引擎名——注入的引擎优先，否则按生效的
// deploy.target 推断（override.yaml 能把它改成 podman，见 up_k8s.go 的
// resolveEngineFor）。
func engineName(opts *Options, cfg *config.Config) string {
	if opts.Engine != nil {
		return opts.Engine.Name()
	}
	if cfg != nil && cfg.Deploy.Target == config.TargetPodman {
		return compose.EnginePodman
	}
	return compose.EngineDocker
}
```

Update its one call site inside `generate()` (`internal/cli/up.go:493`):

```go
	result, err := compose.Generate(p.cfg, p.graph, p.states, env, compose.Options{
		Now:    opts.Now,
		Engine: engineName(opts, p.cfg),
		Lookup: envLookup(opts.WorkDir),
	})
```

- [ ] **Step 5: Update `resolveEngineFor` to handle `podman`**

Read `internal/cli/up_k8s.go:157-171` first (already quoted in full), then:

```go
// internal/cli/up_k8s.go

// resolveEngineFor 按部署目标选引擎——K8s 与 Docker/Podman 不是"同一类引擎的
// 两个牌子"，而是两种部署目标（见函数原有的这段说明，保留在下面）。
//
// podman 单列一支：它跟 k8s 一样不能直接走 resolveEngine 的 Docker 探测逻辑，
// 但原因不同——不是"选错了编排器"，是"这个引擎的 engine.Engine 实现还没有
// 落地"（005 §7、override.yaml 设计书 §11）。混进 resolveEngine 的话，一台
// 只装了 Podman 的机器上，target: podman 会先撞上 engine.Detect() 自己那句
// "检测到 Podman，暂不支持"，措辞对，但来源是"猜出来的"，不是"配置里选出来的"——
// 使用者分不清这次到底是环境问题还是他自己配错了。
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
		return nil, podmanTargetNotImplemented()
	}
	return resolveEngine(opts)
}

// podmanTargetNotImplemented 说清楚"选了 podman"和"这个引擎能不能真的启动"
// 是两回事——override.yaml 已经接受并校验了这个取值（跟 docker/k8s 同等对待，
// 见 internal/override.CheckAgainst），缺的只是 engine.Engine 的真实实现。
func podmanTargetNotImplemented() error {
	return clierr.New(clierr.CodeEngineMissing, i18n.T(msgid.CliUpPodmanTargetNotImplemented)).
		WithHint(i18n.T(msgid.CliUpPodmanTargetDryRunStillWorks))
}
```

- [ ] **Step 6: Route the real (non-dry-run) engine resolution through `resolveEngineFor`**

Read `internal/cli/up.go:163-181` first (already quoted in full — the `runUp` function's real-engine-invocation site), then:

```go
// internal/cli/up.go, inside runUp — replace:
//   eng, err := resolveEngine(opts)
// with:

	eng, err := resolveEngineFor(opts, plan.cfg)
```

(`plan.cfg` is already in scope at this point in `runUp` — it's the `*upPlan` field populated by `buildUpPlan`, already carrying the overridden `Deploy.Target` from Task 4's `applyOverride`.)

- [ ] **Step 7: Add the new msgids**

Add to `internal/msgid/cli_override.go`:

```go
	CliUpPodmanTargetNotImplemented   = "cli.up.podman_target_not_implemented"
	CliUpPodmanTargetDryRunStillWorks = "cli.up.podman_target_dry_run_still_works"
```

Add to `internal/i18n/catalog_en.go`:

```go
	msgid.CliUpPodmanTargetNotImplemented:   "target: podman is accepted and validated, but there is no Podman engine implementation to actually run it yet",
	msgid.CliUpPodmanTargetDryRunStillWorks: "brickkit up --dry-run still works — it generates an ordinary compose file, which is exactly what podman compose consumes",
```

Add to `internal/i18n/catalog_zh.go`:

```go
	msgid.CliUpPodmanTargetNotImplemented:   "target: podman 已经被接受并校验通过，但真正启动它的引擎实现还没有落地",
	msgid.CliUpPodmanTargetDryRunStillWorks: "brickkit up --dry-run 仍然能用——它生成的是一份普通的 compose 文件，正是 podman compose 会读的那一份",
```

- [ ] **Step 8: Run the Task 5 tests — they should pass now**

Run: `go test ./internal/cli/... -run 'TestUpDryRunSucceedsWithPodmanTarget|TestUpRealRunFailsClearlyWithPodmanTarget' -v`
Expected: PASS (2/2).

- [ ] **Step 9: Run the full `internal/cli` and `internal/compose` suites**

Run: `go test ./internal/cli/... ./internal/compose/... 2>&1 | tail -60`
Expected: `ok` for both — this task must not change any existing docker/k8s-target test's behavior.

- [ ] **Step 10: Run `make check-i18n`**

Run: `go test ./tests/i18nguard/... -v`
Expected: PASS.

- [ ] **Step 11: Commit**

```bash
git add internal/cli/up_k8s.go internal/cli/up.go internal/compose/local.go internal/cli/podman_target_test.go internal/msgid/cli_override.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "$(cat <<'EOF'
feat: target: podman engine selection (dry-run works, real run errors clearly)

override.yaml's target field already accepts and validates podman
(Tasks 1/3) exactly like docker/k8s — this task wires the actual engine
resolution to respect it, rather than silently falling through to
engine.Detect()'s docker-or-nothing logic (which would either silently run
under Docker despite the user's explicit choice, or produce a confusing
"detected Podman" message with no connection to the config that asked for
it).

Dry-run generation needed no changes — it already only branches on
target == k8s, so podman falls into the ordinary (engine-agnostic) compose
path, exactly matching what real podman compose consumes. Only the real
engine-invocation step (resolveEngineFor, now routed through instead of
the target-blind resolveEngine) needs a podman-specific branch, and it's a
deliberately narrow one: a clear "not implemented yet" error, not an
attempt to actually drive Podman (005 §7 / design spec §11 — the real
engine.Engine implementation is a separate, later piece of work).

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: `brickkit override` command

**Files:**
- Create: `internal/cli/override.go`
- Create: `internal/cli/override_test.go`
- Modify: `internal/cli/root.go:192-211` (register the command)
- Modify: `internal/msgid/cli_override.go`
- Modify: `internal/i18n/catalog_en.go`, `internal/i18n/catalog_zh.go`

**Interfaces:**
- Consumes: `override.Override`/`ComponentOverride` (Task 1), `override.Drift` (Task 3), `config.Layout.OverridePath()` (Task 1), `config.EnsureGitignore`/`gitignoreSections` (Task 1's addition), `resolveTopology`/`shell.ParseRef`-style shell detection (existing, for nesting members under their shell — mirrors `internal/cli/graph.go`'s own servedBy-grouping logic at `graph.go:196-223`, already quoted in full in this plan's research).
- Produces: `brickkit override` as a registered cobra command; `func newOverrideCommand(opts *Options) *cobra.Command`; `func runOverride(opts *Options) error`. No `context.Context` parameter — unlike `restore`/`sync`/`up`, this command never resolves the dependency graph or talks to a source client (it only parses `brickkit.yaml` and `override.yaml` and writes a local file), so it has nothing to pass a context through to.

- [ ] **Step 1: Write the failing test for first-run generation**

```go
// internal/cli/override_test.go
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

func TestOverrideCreatesFileOnFirstRun(t *testing.T) {
	f := addedProject(t, []comp{
		{ID: "demo/hello", Version: "1.0.0"},
		{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
	}, "demo/hello@1.0.0", "demo/caller@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
  - id: demo/caller
    version: 1.0.0
`)

	r := runIn(t, f.Dir, "override")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	path := filepath.Join(f.Dir, "override.yaml")
	require.FileExists(t, path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "- id: demo/hello")
	assert.Contains(t, string(data), "- id: demo/caller")
}

func TestOverrideAddsFileToGitignore(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)

	r := runIn(t, f.Dir, "override")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	data, err := os.ReadFile(filepath.Join(f.Dir, ".gitignore"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "override.yaml")
}

func TestOverrideNestsMembersUnderTheirShell(t *testing.T) {
	f := addedProject(t, []comp{
		{ID: "infra/shell-go-core", Version: "1.0.0"},
		{ID: "mdm/customer", Version: "1.0.0"},
	}, "infra/shell-go-core@1.0.0", "mdm/customer@1.0.0")
	f.writeConfig(t, `components:
  - id: infra/shell-go-core
    version: 1.0.0
  - id: mdm/customer
    version: 1.0.0
    servedBy: infra/shell-go-core@1.0.0
`)

	r := runIn(t, f.Dir, "override")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	text := string(data)
	assert.Contains(t, text, "- id: infra/shell-go-core")
	assert.Contains(t, text, "members:")
	assert.Contains(t, text, "- id: mdm/customer")
}

func TestOverrideRefreshPreservesExistingCustomizations(t *testing.T) {
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

	r := runIn(t, f.Dir, "override")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	text := string(data)
	assert.Contains(t, text, "mode: debug")
	assert.Contains(t, text, "9001")
}

func TestOverrideRefreshAddsLineForNewComponent(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
`)
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
  - id: demo/caller
    version: 1.0.0
`)

	r := runIn(t, f.Dir, "override")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "- id: demo/caller")
}

func TestOverrideRefreshDropsLineForRemovedComponent(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
  - id: demo/gone
`)

	r := runIn(t, f.Dir, "override")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "demo/gone")
}

func TestOverridePrintsDriftNote(t *testing.T) {
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

	r := runIn(t, f.Dir, "override")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "demo/hello")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/... -run TestOverride -v`
Expected: FAIL — `unknown command "override"` (cobra reports it, since the command isn't registered).

- [ ] **Step 3: Implement `newOverrideCommand`/`runOverride`**

Read `internal/cli/restore.go:20-39` (the closest structural analog, already quoted in full) and `internal/cli/graph.go:196-223` (the existing shell-grouping logic to mirror, already quoted in full) first, then:

```go
// internal/cli/override.go
package cli

import (
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/override"
	"github.com/brickkit/brickkit/internal/shell"
	"gopkg.in/yaml.v3"
)

func newOverrideCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:     "override",
		Short:   i18n.T(msgid.CliOverrideShort),
		GroupID: groupProject,
		Long:    i18n.T(msgid.CliOverrideLong),
		Example: i18n.T(msgid.CliOverrideExample),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOverride(opts)
		},
	}
}

// runOverride 创建/刷新 override.yaml（override.yaml 设计书 §3）：首次运行时按
// brickkit.yaml 当前的组件列表生成穷举式的骨架，之后每次运行都是"刷新"——
// 已有的覆盖原样保留，新组件补一条裸 `- id:`，被删掉的组件那一行也跟着消失，
// 同时把漂移提示（Task 3 的 Drift）打印出来。这就是重置/修复操作本身，没有
// 单独的第二个命令（设计书 §3）。
func runOverride(opts *Options) error {
	if opts.ConfigPath != "" && opts.ConfigPath != DefaultConfigFile {
		return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliOverrideRefusesNonDefaultConfig)).
			WithDetail(i18n.T(msgid.LabelPath), opts.ConfigPath)
	}

	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return err
	}

	existing, err := override.ParseOverrideFile(layout.OverridePath())
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

	if updated, err := config.EnsureGitignore(layout.GitignorePath()); err == nil && updated {
		opts.Printf("%s\n", i18n.T(msgid.CliOverrideGitignoreUpdated))
	}

	opts.Printf("%s\n", i18n.T(msgid.CliOverrideWritten, layout.OverridePath()))

	for _, note := range override.Drift(cfg, fresh) {
		opts.Printf("%s\n", i18n.T(msgid.CliOverrideDriftNote, note.Field, note.Message))
	}

	return nil
}

// generateOverride 按 brickkit.yaml 的当前状态重建覆盖树：已有条目（按组件 ID
// 匹配）原样保留它的 Mode/LocalPort/Baseline，新组件补一条裸 id，不在
// brickkit.yaml 里的旧条目被丢弃。外壳分组每次都重新计算，而不是从旧文件
// 继承——servedBy 关系随时可能变，旧的嵌套结构不该假设还对。
func generateOverride(cfg *config.Config, existing *override.Override) *override.Override {
	byID := map[string]override.ComponentOverride{}
	if existing != nil {
		flattenExisting(existing.Components, byID)
	}

	members := map[string][]config.Component{}
	var standalone []config.Component
	for _, c := range cfg.Components {
		if c.ServedBy != "" {
			if ref, ok := shell.ParseRef(c.ServedBy); ok {
				members[ref.ID] = append(members[ref.ID], c)
				continue
			}
		}
		standalone = append(standalone, c)
	}

	var out []override.ComponentOverride
	for _, c := range standalone {
		entry := entryFor(c, byID)
		if group, ok := members[c.ID]; ok {
			sort.Slice(group, func(i, j int) bool { return group[i].ID < group[j].ID })
			for _, m := range group {
				entry.Members = append(entry.Members, entryFor(m, byID))
			}
		}
		out = append(out, entry)
	}

	fresh := &override.Override{Components: out}
	if existing != nil {
		fresh.Target = existing.Target
		fresh.TargetBaseline = existing.TargetBaseline
	}
	return fresh
}

func entryFor(c config.Component, byID map[string]override.ComponentOverride) override.ComponentOverride {
	if prev, ok := byID[c.ID]; ok {
		return override.ComponentOverride{ID: c.ID, Mode: prev.Mode, LocalPort: prev.LocalPort, Baseline: prev.Baseline}
	}
	return override.ComponentOverride{ID: c.ID}
}

func flattenExisting(entries []override.ComponentOverride, out map[string]override.ComponentOverride) {
	for _, c := range entries {
		out[c.ID] = c
		flattenExisting(c.Members, out)
	}
}

// renderOverride 序列化成 override.yaml——固定的说明性头注释 + yaml.Marshal 的
// 结构体输出。不像 config.Edit 那样做节点级手术保留原有格式/注释：override.yaml
// 是 gitignore、本地生成的文件，不进 code review，每次刷新整份重写没有代价
// （跟 brickkit.yaml 的场景完全不同，那边保留格式是因为它要进 git diff）。
func renderOverride(o *override.Override) ([]byte, error) {
	const header = `# override.yaml — local deployment overrides on top of brickkit.yaml.
# Generated/refreshed by ` + "`brickkit override`" + `. Not authoritative — brickkit.yaml
# stays the source of truth for everything not listed here.

`
	body, err := yaml.Marshal(o)
	if err != nil {
		return nil, clierr.New(clierr.CodeInternal, i18n.T(msgid.CliOverrideWriteFailed)).WithCause(err)
	}
	return append([]byte(header), body...), nil
}
```

- [ ] **Step 4: Register the command**

Read `internal/cli/root.go:192-211` first, then add `newOverrideCommand(opts)` to the `root.AddCommand(...)` call — insert it right after `newRestoreCommand(opts)`, matching the command table's own logical grouping (both are project-maintenance commands with no positional args):

```go
	root.AddCommand(
		newInitCommand(opts), newSkillsCommand(opts), newGraphCommand(opts), newLintCommand(opts),
		newNewCommand(opts), newAddCommand(opts), newRemoveCommand(opts), newFetchCommand(opts),
		newSyncCommand(opts), newRestoreCommand(opts), newOverrideCommand(opts), newUpCommand(opts), newDownCommand(opts),
		newStatusCommand(opts), newLoginCommand(opts), newLogoutCommand(opts), newPublishCommand(opts),
		newVersionCommand(opts), newLangCommand(opts),
	)
```

- [ ] **Step 5: Add the new msgids**

Add to `internal/msgid/cli_override.go`:

```go
	CliOverrideShort                     = "cli.override.short"
	CliOverrideLong                      = "cli.override.long"
	CliOverrideExample                   = "cli.override.example"
	CliOverrideRefusesNonDefaultConfig   = "cli.override.refuses_non_default_config"
	CliOverrideWriteFailed               = "cli.override.write_failed"
	CliOverrideGitignoreUpdated          = "cli.override.gitignore_updated"
	CliOverrideWritten                   = "cli.override.written"
	CliOverrideDriftNote                 = "cli.override.drift_note"
```

Add to `internal/i18n/catalog_en.go`:

```go
	msgid.CliOverrideShort:                   "Create or refresh override.yaml (local deployment overrides)",
	msgid.CliOverrideLong: "Creates override.yaml on first run and refreshes it on later runs: adds a bare\n" +
		"line for any new component, drops the line for any removed component, and\n" +
		"reports drift between brickkit.yaml's current values and what this override\n" +
		"was last confirmed against. Re-running this command is the reset/repair\n" +
		"operation — there is no separate command for it.",
	msgid.CliOverrideExample:                 "  brickkit override",
	msgid.CliOverrideRefusesNonDefaultConfig: "override.yaml only ever applies to the default brickkit.yaml — refusing to generate one keyed off %[1]s",
	msgid.CliOverrideWriteFailed:             "failed to write override.yaml",
	msgid.CliOverrideGitignoreUpdated:        "Added override.yaml to .gitignore",
	msgid.CliOverrideWritten:                 "Wrote %[1]s",
	msgid.CliOverrideDriftNote:               "Drift: %[1]s — %[2]s",
```

Add to `internal/i18n/catalog_zh.go`:

```go
	msgid.CliOverrideShort:                   "创建或刷新 override.yaml（本地部署覆盖）",
	msgid.CliOverrideLong: "首次运行时创建 override.yaml，之后每次运行都是刷新：给新组件补一条裸\n" +
		"条目，把被删掉的组件那一行去掉，并且报出 brickkit.yaml 当前值与这份覆盖\n" +
		"上次确认时的记录之间的漂移。重新跑这条命令本身就是重置/修复操作——没有\n" +
		"另一个单独的命令做这件事。",
	msgid.CliOverrideExample:                 "  brickkit override",
	msgid.CliOverrideRefusesNonDefaultConfig: "override.yaml 只对默认的 brickkit.yaml 生效——拒绝生成一份挂在 %[1]s 上的",
	msgid.CliOverrideWriteFailed:             "override.yaml 写入失败",
	msgid.CliOverrideGitignoreUpdated:        "已把 override.yaml 加进 .gitignore",
	msgid.CliOverrideWritten:                 "已写入 %[1]s",
	msgid.CliOverrideDriftNote:               "漂移：%[1]s —— %[2]s",
```

- [ ] **Step 6: Run the Task 6 tests**

Run: `go test ./internal/cli/... -run TestOverride -v`
Expected: PASS (7/7). If `TestOverrideNestsMembersUnderTheirShell` fails because `yaml.Marshal`'s default field order or indentation doesn't literally contain the substring `"members:"` the way the test expects, read the actual output and adjust either the test's assertion or `ComponentOverride`'s struct field order (`Members` should stay last, matching the spec §8 example's visual shape) — do not weaken the test to stop checking nesting occurred.

- [ ] **Step 7: Run the full `internal/cli` suite and the CLI reference/command-count doc guards**

Run: `go test ./internal/cli/... 2>&1 | tail -40`
Expected: `ok`.

Run: `go build ./... && go run ./cmd/brickkit override --help`
Expected: prints the command's help text with no error — a quick manual sanity check that `Short`/`Long`/`Example` render correctly.

Run: `make check-cli-docs`
Expected: this will very likely **fail** — it's the doc-consistency guard mentioned in `make lint`'s command reference ("命令数目：文档与实现一致" / "帮助文本提到的命令都存在") and this plan does not update `docs/en/06-architecture/09-cli-reference.md` or AGENTS.md §8's command table. **This is expected and acceptable for this task** — full CLI documentation for `brickkit override` (the reference doc, AGENTS.md's §8 table, the guide walkthrough) is explicitly Plan 2b's job (it documents the whole feature together, once `add`/`remove`/`lint`/`restore` integration also exists, rather than documenting the command twice). Note the exact failing check names in the commit message so Plan 2b's author knows what's still outstanding; do not attempt to silence or work around this check in this task.

- [ ] **Step 8: Run `make check-i18n`**

Run: `go test ./tests/i18nguard/... -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/cli/override.go internal/cli/override_test.go internal/cli/root.go internal/msgid/cli_override.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "$(cat <<'EOF'
feat: add the brickkit override command

Creates override.yaml on first run, refreshes it on later runs — the same
command is the reset/repair operation too (design spec §3), no separate
command for that. Refresh preserves existing customizations (matched by
component ID), adds a bare line for any new component, drops the line for
any removed one, re-nests servedBy members under their shell fresh every
time (relationships can change, so old nesting is never trusted), and
prints Task 3's Drift notes.

override.yaml is regenerated wholesale on every write rather than
node-level-edited like brickkit.yaml's own config.Edit — deliberately:
it's gitignored, local-only, never reviewed, so losing comments/formatting
on refresh costs nothing, unlike brickkit.yaml where config.Edit's
surgical editing exists specifically to protect a file that IS reviewed.

Known gap, intentionally left for Plan 2b: docs (CLI reference, AGENTS.md
§8's command table, the guide walkthrough) aren't updated here — make
check-cli-docs currently fails as a result. Documenting this command
happens once alongside its add/remove/lint/restore integration, not twice.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: `brickkit.yaml` rejects `mode: debug`, and the full test migration

**Files:**
- Modify: `internal/config/validate.go:222-238` (`validateComponentMode`)
- Modify: `internal/config/config.go:262` (jsonschema tag)
- Modify: `internal/msgid/config.go` (new constant; `ConfigModeInvalid`'s catalog text changes)
- Modify: `internal/i18n/catalog_en.go`, `internal/i18n/catalog_zh.go`
- Modify: `internal/schemagen/schemas_test.go:657-669` (the `components[0].mode` constraint case for the `project` doc)
- Modify: `internal/config/testdata/erp-project.yaml`, `internal/config/config_test.go` (the golden-fixture test + `TestModeFiveStates` + `TestValidateComponentMode`)
- Modify: `internal/config/edge_test.go` (delete `TestDebugWithoutPortIsValid`)
- Modify: `internal/config/replicas_test.go` (delete `TestDebugWithReplicasIsAnError`; simplify `validateReplicas`'s now-unreachable debug branch if present)
- Modify: `internal/k8s/k8s_test.go` (rename/repurpose `TestModeDebugWithK8sTargetRejectedAtParseNotGeneration`)
- Modify: `internal/cli/up_k8s_test.go` (delete `TestUpK8sRejectsDebugComponent`)
- Modify: `internal/cli/graph.go` (remove the now-unreachable `mode: debug` rendering branch and the orphaned `classLocal` styling)
- Modify: `internal/cli/graph_test.go` (delete the three `mode: debug`-specific tests; keep the `mode: local` ones unchanged)
- Modify: `internal/cli/up_dryrun_test.go` (migrate `localDebugProject` and the multiline-`.env` test to `override.yaml`)
- Modify: `internal/cli/sync_test.go` (migrate the two `17.8` tests to `override.yaml`)

**Interfaces:**
- Consumes: nothing new — this task only removes a previously-legal value and updates every place that exercised it.
- Produces: `internal/config.validateComponentMode` now rejects `debug` unconditionally in `brickkit.yaml`, independent of `deploy.target`.

- [ ] **Step 1: Write the failing test for the new unconditional rejection**

Read `internal/config/config_test.go:960-996` (`TestValidateComponentMode`, already quoted in full) first, then replace its `cases` slice:

```go
// internal/config/config_test.go — TestValidateComponentMode's cases slice, replaced:

	cases := []struct {
		name    string
		yaml    string
		wantErr []string // 期望错误信息里包含的子串，nil 表示应该通过
	}{
		{
			name:    "mode 非法取值报错",
			yaml:    "project: p\ndeploy:\n  target: docker\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    mode: bogus\n",
			wantErr: []string{"mode", "enabled", "disable", "local"},
		},
		{
			name:    "mode: debug 在 brickkit.yaml 里报错（配 docker）——只能写进 override.yaml",
			yaml:    "project: p\ndeploy:\n  target: docker\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    mode: debug\n",
			wantErr: []string{"mode", "override.yaml"},
		},
		{
			name:    "mode: debug 在 brickkit.yaml 里报错（配 k8s）——同一条规则，不是 k8s 专属的",
			yaml:    "project: p\ndeploy:\n  target: k8s\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    mode: debug\n",
			wantErr: []string{"mode", "override.yaml"},
		},
		{
			name:    "mode: enabled 配 k8s 合法",
			yaml:    "project: p\ndeploy:\n  target: k8s\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    mode: enabled\n",
			wantErr: nil,
		},
		{
			name:    "mode: local 配 docker 合法",
			yaml:    "project: p\ndeploy:\n  target: docker\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    mode: local\n",
			wantErr: nil,
		},
		{
			name:    "mode: local 配 k8s 报错",
			yaml:    "project: p\ndeploy:\n  target: k8s\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    mode: local\n",
			wantErr: []string{"mode", "k8s"},
		},
	}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config/... -run TestValidateComponentMode -v`
Expected: FAIL — the two new "报错" cases currently get `wantErr: nil`'s treatment reversed (today's code accepts `mode: debug` under docker, so `require.Error` fails for that case; the k8s case fails on message content, since today's message doesn't mention "override.yaml").

- [ ] **Step 3: Update `validateComponentMode`**

Read `internal/config/validate.go:222-238` first (already quoted in full), then:

```go
// internal/config/validate.go

// validateComponentMode 校验 mode 的取值合法性。debug 从今往后在 brickkit.yaml
// 里无条件非法——它只能写进 override.yaml（override.yaml 设计书 §4）：这个组件
// 此刻"钉在谁的 IDE 里跑"是本机、此刻的状态，不该出现在跟团队共享、进 code
// review 的 brickkit.yaml 里。mode: local 配 deploy.target: k8s 的组合检查不变
// （以前这条检查在 K8s 生成阶段才报，internal/k8s/k8s.go 的 localNotSupported，
// 挪到解析阶段是为了让 brickkit lint 也能拿到这个错误，不用等生成部署文件）。
func (c *Config) validateComponentMode(p *clierr.ProblemSet, field string, item Component) {
	switch item.Mode {
	case "", ModeEnabled, ModeDisable, ModeLocal:
		// 合法取值
	case ModeDebug:
		p.Add(field+".mode", i18n.T(msgid.ConfigModeDebugNotInBrickkitYaml))
		return
	default:
		p.Add(field+".mode", i18n.T(msgid.ConfigModeInvalid, item.Mode))
		return
	}
	if item.Mode == ModeLocal && c.Deploy.Target == TargetK8s {
		p.Add(field+".mode", i18n.T(msgid.ConfigModeK8sUnsupported, item.Mode))
	}
}
```

- [ ] **Step 4: Update the jsonschema tag and the two catalog entries**

Read `internal/config/config.go:249-262` first (already quoted in full), then update the `Mode` field's tag and its explanatory comment:

```go
	// Mode 取代了 Enabled/Local 两个字段（mode 字段迁移设计）：
	// ""（未写）= 跟随上层，走容器；"enabled" = 钉住，走容器；
	// "disable" = 钉住不跑；"local" = 裸进程，brickkit 自己拉起。
	// "debug"（裸进程，用户自己启动）只能写进 override.yaml，brickkit.yaml
	// 自身从这里就无条件拒绝它（override.yaml 设计书 §4）。
	// 校验器负责按 deploy.target 决定这四个取值里哪些合法（k8s 下只认前三个）。
	Mode      string `yaml:"mode,omitempty" jsonschema:"enum=enabled|disable|local"`
```

Add `ConfigModeDebugNotInBrickkitYaml` to `internal/msgid/config.go`, right next to `ConfigModeInvalid`/`ConfigModeK8sUnsupported` (`internal/msgid/config.go:84-85`):

```go
	ConfigModeInvalid                = "config.mode_invalid"
	ConfigModeDebugNotInBrickkitYaml = "config.mode_debug_not_in_brickkit_yaml"
	ConfigModeK8sUnsupported         = "config.mode_k8s_unsupported"
```

Update `internal/i18n/catalog_en.go:620-621` (`ConfigModeInvalid`'s text loses "debug"; new entry added):

```go
	msgid.ConfigModeInvalid:                 "must be one of enabled/disable/local (or omitted), got %[1]q",
	msgid.ConfigModeDebugNotInBrickkitYaml:  "mode: debug can only be set in override.yaml, not brickkit.yaml — it's local, machine-specific state (which IDE, which debugger), not something to share and code-review",
	msgid.ConfigModeK8sUnsupported:          "%[1]q is only supported with deploy.target: docker (got k8s) — a cluster Pod cannot reach a process on the developer's own machine",
```

Update `internal/i18n/catalog_zh.go:614-615` the same way:

```go
	msgid.ConfigModeInvalid:                 "只能是 enabled/disable/local 之一（或不写），实际是 %[1]q",
	msgid.ConfigModeDebugNotInBrickkitYaml:  "mode: debug 只能写在 override.yaml 里，不能写进 brickkit.yaml——这是本机、此刻的状态（哪个 IDE、哪个调试器），不该拿去团队共享、进 code review",
	msgid.ConfigModeK8sUnsupported:          "%[1]q 只支持 deploy.target: docker（现在是 k8s）——集群里的 Pod 连不到开发者自己机器上的进程",
```

Also update `ConfigLocalPortNeedsLocal`'s text (`internal/i18n/catalog_en.go:596`, `internal/i18n/catalog_zh.go:590` — quoted in full in this plan's research) so its "declare mode: debug" suggestion doesn't mislead a brickkit.yaml editor into writing something that will itself be rejected:

```go
// catalog_en.go
	msgid.ConfigLocalPortNeedsLocal: "only takes effect with mode: local in brickkit.yaml, or mode: debug in override.yaml; declare one of them, or remove this field",
// catalog_zh.go
	msgid.ConfigLocalPortNeedsLocal: "只在 brickkit.yaml 里写 mode: local，或者在 override.yaml 里写 mode: debug 时才生效，请一并声明其中之一或删除该字段",
```

- [ ] **Step 5: Run the test — it should pass now**

Run: `go test ./internal/config/... -run TestValidateComponentMode -v`
Expected: PASS (6/6).

- [ ] **Step 6: Run the full `internal/config` suite to find every other break**

Run: `go test ./internal/config/... -v 2>&1 | tail -150`
Expected: several failures — read the actual output; it will name the exact tests. The ones this plan already knows about (fix each as follows):

  - `TestParseConfigFileFullProject` — fails on `people.Mode`/`people.LocalPort` assertions. Read `internal/config/testdata/erp-project.yaml:31-38` (already quoted in full), remove lines `    # 没写 mode → 默认开启，但可被级联关闭`, `    mode: debug`, `    localPort: 8081`, leaving:
    ```yaml
      - id: people/basic
        version: 1.0.0
        resources:                  # 可选，覆盖组件 Manifest 中的推荐资源配额
          limits:
            memory: "1Gi"           # 生产环境调大内存
    ```
    Read `internal/config/config_test.go:63-68` (already quoted in full), remove lines 64-65 (`assert.Equal(t, ModeDebug, people.Mode, ...)` and the `LocalPort` assertion), keeping the `Resources`/`Limits` assertions on lines 66-68 unchanged (renumber the trailing comment reference if the test's own numbering scheme — "5.22" — is now orphaned; check whether "5.22" is referenced elsewhere before renumbering anything).

  - `TestModeFiveStates` — fails: `ParseConfig` now errors on the `d/debug` component, so `require.NoError(t, err)` at line 228 fails. Read `internal/config/config_test.go:206-253` (already quoted in full) and rewrite:
    ```go
    func TestModeFourStatesInBrickkitYaml(t *testing.T) {
    	c, err := ParseConfig([]byte(`
    project: my-project
    deploy:
      target: docker
    components:
      - id: a/pinned
        version: 1.0.0
        mode: enabled
      - id: b/default
        version: 1.0.0
      - id: c/disabled
        version: 1.0.0
        mode: disable
      - id: e/local
        version: 1.0.0
        mode: local
    resources: []
    `), "brickkit.yaml")
    	require.NoError(t, err)

    	pinned, dflt, disabled, local :=
    		c.Components[0], c.Components[1], c.Components[2], c.Components[3]

    	assert.Equal(t, ModeEnabled, pinned.Mode)
    	assert.True(t, pinned.IsPinned(), "mode: enabled → 一定跑")
    	assert.False(t, pinned.IsDisabled())

    	assert.Equal(t, "", dflt.Mode, "不写 mode → 空字符串，不是 disable")
    	assert.False(t, dflt.IsDisabled(), "没写不等于关掉")
    	assert.False(t, dflt.IsPinned(), "没写不等于钉住")

    	assert.Equal(t, ModeDisable, disabled.Mode)
    	assert.True(t, disabled.IsDisabled(), "mode: disable → 一定不跑")

    	assert.Equal(t, ModeLocal, local.Mode)
    	assert.True(t, local.IsPinned(), "mode: local → 一定跑（brickkit 自己拉起）")
    	assert.False(t, local.IsDisabled())
    }

    // mode: debug 只能来自 override.yaml，在内存里由 apply 直接写进 Component.Mode
    // （从不经过 ParseConfig/Validate，见 internal/cli/topology.go 的 applyOverride）——
    // IsPinned/IsDisabled 这两个方法本身不关心 Mode 的取值是从哪份文件来的，这里
    // 直接构造 Go 结构体验证它们对 debug 仍然正确，不必（也不能）经过 YAML 解析。
    func TestModeDebugIsPinnedRegardlessOfSource(t *testing.T) {
    	debug := Component{Mode: ModeDebug}
    	assert.True(t, debug.IsPinned(), "mode: debug → 一定跑（要盯着它调试）")
    	assert.False(t, debug.IsDisabled())
    }
    ```
    (Also update the section header comment at `config_test.go:202-204` — `"5.4 / 5.5 / 5.6 mode 四种写法"` already said "four", oddly matching the pre-existing five-case test's title mismatch; leave the numbering itself alone, just make sure the comment above `TestModeFourStatesInBrickkitYaml` accurately describes what it now covers.)

  - `TestDebugWithoutPortIsValid` (`internal/config/edge_test.go:402-412`, already quoted in full) — its premise ("debug without localPort is legal in brickkit.yaml") is now false. **Delete this test entirely.** (The equivalent "debug without localPort is legal" concept belongs to `internal/override`'s own parser — confirm it's already covered there; if not, this is a gap worth a quick follow-up test in `internal/override/override_test.go`, but do not add scope here beyond noting it.)

  - `TestDebugWithReplicasIsAnError` (`internal/config/replicas_test.go:108-118`, already quoted in full) — its scenario (`mode: debug` + `replicas: 3` in brickkit.yaml text) is now unreachable for a more fundamental reason (debug itself is rejected first). **Delete this test.** Read `internal/config/validate.go`'s `validateReplicas` function (search for `func validateReplicas` — referenced near `validate.go:588` in this plan's research but not fully quoted; read it now) and check whether it has its own `item.Mode == ModeDebug` branch producing the "debug" substring this deleted test was asserting on. If it does, that branch is now unreachable via `ParseConfig` (though still technically callable if some other code path calls `Validate()` directly on a hand-built `Config` with `Mode: "debug"` — none exists today per this plan's research, `Validate()` has exactly one call site, `parse.go:113`). Leave `validateReplicas`'s debug-specific branch in place rather than removing it — it's harmless defensive code, and removing it isn't necessary for correctness (unlike `graph.go`'s dead branch below, which actively misleads a reader into thinking a scenario is reachable in production, not just in a hypothetical direct `Validate()` call).

- [ ] **Step 7: Run the `internal/config` suite again**

Run: `go test ./internal/config/... -v 2>&1 | tail -60`
Expected: `ok` — all failures from Step 6 resolved.

- [ ] **Step 8: Fix `internal/schemagen`**

Read `internal/schemagen/schemas_test.go:657-669` (already quoted in full) first, then update:

```go
		{
			// mode 不必填，schema 的 enum 因此带着 null（显式写 null 等于没写）。
			// "" 在 yaml 里与没写无法区分，校验器放行它，而 schema 不该把 "" 当成一个
			// 可选的取值推荐给人——所以点名成 validatorOnly。
			// local 只在 docker 下合法，基准里的 k8s 要换掉，component 上也得有一行
			// mode 才有落脚点。debug 从今往后在 brickkit.yaml 里无条件非法（override.yaml
			// 设计书 §4），因此不在 schema 的 enum 里，也不在校验器接受的取值里——
			// 点名进 invalid，证明两边确实一致地拒绝它。
			name: "components[0].mode", doc: "project", schemaPath: "components[]/mode",
			baseline: strings.Replace(strings.Replace(baselineProject, "target: k8s", "target: docker", 1),
				"    version: 1.0.0\n", "    version: 1.0.0\n    mode: enabled\n", 1),
			dataPath: []any{"components", 0, "mode"}, errField: "components[0].mode",
			valid:         []any{config.ModeEnabled, config.ModeDisable, config.ModeLocal, nil},
			validatorOnly: []any{""},
			invalid:       []any{"Enabled", "disabled", "docker", config.ModeDebug},
		},
```

Run: `go test ./internal/schemagen/... -v`
Expected: PASS.

- [ ] **Step 9: Fix `internal/k8s/k8s_test.go`**

Read `internal/k8s/k8s_test.go:800-825` first (find the exact current line numbers with `grep -n "TestModeDebugWithK8sTargetRejectedAtParseNotGeneration" internal/k8s/k8s_test.go`, since Plan 1's earlier work may have shifted these line numbers slightly from what this plan's research quoted), then rename and update the test's comment to reflect the now-unconditional rule:

```go
// mode: debug 在 brickkit.yaml 里要报错——解析阶段就拦下，指到出问题的那一行，
// 而不是等到生成部署文件时才发现。这条检查不再是 k8s 专属的（override.yaml
// 设计书 §4：debug 无论配哪个 target 都非法），这条测试留着是为了继续证明
// k8s.Generate 自己不需要重新实现这个检查——它完全在解析阶段就已经被挡住了。
func TestModeDebugRejectedAtParseNotGeneration(t *testing.T) {
	yaml := "project: p\ndeploy:\n  target: k8s\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    mode: debug\n"
	_, err := config.ParseConfig([]byte(yaml), "brickkit.yaml")
	require.Error(t, err)
}
```

(Keep the rest of the test function body exactly as it already is beyond the rename/comment — this plan's research did not capture its full body beyond the YAML literal; read the actual current implementation before editing, and only change the name/comment/assertion text needed to match the new unconditional framing, not its underlying mechanics.)

Also read `internal/k8s/servedby_test.go:199-204` (already quoted in full) and update its comment's reference from `TestModeDebugWithK8sTargetRejectedAtParseNotGeneration` to `TestModeDebugRejectedAtParseNotGeneration`, matching the rename.

Run: `go test ./internal/k8s/... -v 2>&1 | tail -40`
Expected: `ok`.

- [ ] **Step 10: Delete the now-redundant CLI-level k8s+debug test**

Read `internal/cli/up_k8s_test.go:238-250` (already quoted in full) — **delete** `TestUpK8sRejectsDebugComponent` entirely (its scenario — `mode: debug` reaching `config.ParseConfigFile` under a k8s-target project — duplicates what `internal/config`'s and `internal/k8s`'s own tests, fixed in Steps 6 and 9, already cover; keeping a third copy that asserts stale message text ("deploy.target: docker") adds nothing and would need its own separate fix for no distinct benefit). Also remove its now-orphaned section comment (lines 238-239) if nothing else follows it into the same comment block — check the surrounding context before deleting the comment itself.

Run: `go test ./internal/cli/... -run TestUpK8s -v 2>&1 | tail -40`
Expected: `ok` (with one fewer test than before).

- [ ] **Step 11: Remove the dead `mode: debug` rendering branch from `graph.go`**

Read `internal/cli/graph.go:94-194` in full (already quoted in this plan's research) first. Once `brickkit.yaml` can never contain `mode: debug` (Step 3 above) and `graph.go` deliberately never reads `override.yaml` (Global Constraints, unchanged in this plan), `renderMermaid`'s `entry.Mode == config.ModeDebug` branch (lines 164-178) becomes permanently unreachable — `entries` is built straight from `cfg.Components` (line 141-144), and `cfg` here is always the result of `config.ParseConfigFile` on `brickkit.yaml` alone, which by this point in the plan can never produce `Mode == "debug"`. Leaving it in place is misleading dead code, not a harmless defensive check (unlike `internal/config/validate.go`'s `validateReplicas`, which stays reachable from any direct `Validate()` call on a hand-built `Config` — `graph.go`'s branch has no such path, since nothing ever constructs a `Config` with `Mode: "debug"` and feeds it to `renderMermaid` outside of a test).

Remove the branch:

```go
	declare := func(indent string, ref resolver.Ref) {
		running := states.IsRunning(ref)
		label := ref.String()
		if entry := entries[ref]; entry.Mode == config.ModeLocal {
			label += i18n.T(msgid.CliGraphBrManagedLocally)
			if entry.LocalPort > 0 {
				// mode: local 也接受 localPort 作为"固定端口"的手动覆盖
				// （005 §5：默认自动分配，只有想固定端口时才手动指定）
				label += fmt.Sprintf(" :%d", entry.LocalPort)
			}
			if running {
				tag(classManaged, ref)
			}
		}
		if !running {
			tag(classDisabled, ref)
		}
		fmt.Fprintf(&b, "%s%s[\"%s\"]\n", indent, mermaidID(ref), label)
	}
```

Remove the now-unused `classLocal` constant and its `mermaidClassDefs` entry (`graph.go:96-116`, already quoted in full) — after the branch above is deleted, nothing calls `tag(classLocal, ref)` anywhere, so both become dead:

```go
// 节点样式类。
const (
	classDisabled = "disabled"
	classManaged  = "managed"
	classMissing  = "missing"
)

// mermaidClassDefs 按输出顺序列出样式类。用 classDef + class 而不是逐节点 style：
// 一处改样式，全图跟着变。
var mermaidClassDefs = []struct{ name, style string }{
	{classDisabled, "fill:#eee,stroke:#999,color:#999"},
	{classManaged, "fill:#e6ffe6,stroke:#2e8b57"},
	{classMissing, "fill:#fff4e5,stroke:#c77700,stroke-dasharray:4 3"},
}
```

(The historical-naming comment that used to sit above the `const` block — explaining why `classLocal` was never renamed despite being a confusing leftover name — no longer applies to anything, since the constant itself is gone; do not carry that comment forward.)

Find and remove the now-orphaned `msgid.CliGraphBrLocalDebug` constant and its two catalog entries (`grep -rn "CliGraphBrLocalDebug" internal/msgid/*.go internal/i18n/*.go` to find the exact lines).

- [ ] **Step 12: Delete the three `mode: debug`-specific graph tests**

Read `internal/cli/graph_test.go:136-168` and `:219-...` (`TestGraphMarksLocalDebugComponent`, `TestGraphLocalDebugWithoutLocalPortShowsNoPort`, `TestGraphDebugComponentIsPinnedAndNeverGreyedOut` — the first two fully quoted in this plan's research, the third partially) — **delete all three entirely**. Their scenario (a `mode: debug` component reaching `brickkit graph`'s rendering) is now permanently impossible: `brickkit.yaml` can't contain the value, and `graph` never reads `override.yaml`. Keep `TestGraphMarksModeLocalComponent` and `TestGraphModeLocalComponentIsPinnedAndNeverGreyedOut` (`graph_test.go:173-217`, already quoted in full) completely unchanged — `mode: local` remains fully legal in `brickkit.yaml` and fully renderable by `graph`, and their own assertions (`assert.NotContains(t, r.stdout, "local debug", ...)`) already don't depend on a debug component being present in the fixture — they were always testing `mode: local`'s own label text, not a comparison against a sibling debug node.

- [ ] **Step 13: Run the full `internal/cli` graph test suite**

Run: `go test ./internal/cli/... -run TestGraph -v`
Expected: `ok` (three fewer tests than before, the remaining ones green).

- [ ] **Step 14: Migrate the `up_dryrun_test.go` local-debug fixtures to `override.yaml`**

Read `internal/cli/up_dryrun_test.go:140-158` (`localDebugProject`, already quoted in full) and `:225-241` (`TestUpDryRunLocalDebugEnvResolvesMultilineDotEnvValue`, already quoted in full) first, then:

```go
// internal/cli/up_dryrun_test.go

func localDebugProject(t *testing.T) *projectFixture {
	t.Helper()

	comps := []comp{
		{ID: "people/basic", Version: "1.0.0", Requires: []string{"department/tree@1.0.0"}},
		{ID: "department/tree", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "people/basic@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
  - id: department/tree
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: people/basic
    mode: debug
    localPort: 8081
  - id: department/tree
`)
	return f
}
```

```go
func TestUpDryRunLocalDebugEnvResolvesMultilineDotEnvValue(t *testing.T) {
	comps := []comp{
		{ID: "infra/iam-casdoor", Version: "1.0.0", ConfigSchema: []string{"appTokenSigningKeyPem:"}},
	}
	f := addedProject(t, comps, "infra/iam-casdoor@1.0.0")
	f.writeConfig(t, `components:
  - id: infra/iam-casdoor
    version: 1.0.0
    config:
      appTokenSigningKeyPem: "${APP_TOKEN_SIGNING_KEY_PEM}"
`)
	f.writeOverride(t, `components:
  - id: infra/iam-casdoor
    mode: debug
    localPort: 8081
`)
	pem := "-----BEGIN PRIVATE KEY-----\n" +
		"MIIBVQIBADANBgkqhkiG9w0BAQEFAASCAT8wggE7AgEAAkEA\n" +
		"-----END PRIVATE KEY-----"
	dotenv := `APP_TOKEN_SIGNING_KEY_PEM="` + pem + "\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(f.Dir, ".env"), []byte(dotenv), 0o600))

	r := runIn(t, f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	path := filepath.Join(f.Dir, ".brickkit", "generated", "local-debug.infra-iam-casdoor-1-0-0.env")
	got := sourceLocalDebugEnvVar(t, path, "APP_TOKEN_SIGNING_KEY_PEM")
	assert.Equal(t, pem, got, "brickKit 反馈：local-debug.*.env 多行值截断")
}
```

- [ ] **Step 15: Run the affected `up_dryrun_test.go` and `up_test.go`/`status_test.go` tests (all consumers of `localDebugProject`)**

Run: `go test ./internal/cli/... -run 'TestUpDryRun|TestStatus|TestUp' -v 2>&1 | tail -150`
Expected: `ok` for every test that calls `localDebugProject(t)` (five call sites per this plan's research: `status_test.go:145,159`, `up_dryrun_test.go:185,255`, `up_test.go:448`) — since the helper itself now correctly writes both files, every caller should pass unchanged.

- [ ] **Step 16: Migrate the `sync_test.go` `17.8` tests to `override.yaml`**

Read `internal/cli/sync_test.go:226-268` (both tests, already quoted in full) first, then:

```go
// internal/cli/sync_test.go

func TestSyncKeepsDebugComponentActiveEvenWhenNothingNeedsIt(t *testing.T) {
	f := newSyncFixture(t, `components:
  - id: demo/hello
    version: 1.0.0
  - id: demo/caller
    version: 1.0.0
    mode: disable
resources: []
`, "demo/hello", "demo/caller")
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: debug
  - id: demo/caller
`)

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)

	f.assertActive(t, "demo/hello")
	f.assertArchived(t, "demo/caller")
}

func TestSyncRejectsDebugComponentWhoseRequiredDependencyIsDisabled(t *testing.T) {
	f := newSyncFixture(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: disable
  - id: demo/caller
    version: 1.0.0
resources: []
`, "demo/hello", "demo/caller")
	f.writeOverride(t, `components:
  - id: demo/hello
  - id: demo/caller
    mode: debug
`)

	r := runIn(t, f.Dir, "sync")

	require.Equal(t, clierr.ExitError, r.code, "%s%s", r.stdout, r.stderr)
	assert.Contains(t, r.stderr, "mode: debug")
	f.assertActive(t, "demo/hello")
	f.assertActive(t, "demo/caller")
}
```

- [ ] **Step 17: Run the full `internal/cli` sync test suite**

Run: `go test ./internal/cli/... -run TestSync -v`
Expected: `ok`.

- [ ] **Step 18: Run every affected package's full suite, plus `gofmt`**

Run: `gofmt -w internal/config/ internal/cli/ internal/k8s/ internal/schemagen/ internal/msgid/ internal/i18n/ && go build ./... && go test ./internal/config/... ./internal/cli/... ./internal/k8s/... ./internal/schemagen/... ./internal/compose/... ./tests/i18nguard/... -v 2>&1 | tail -200`
Expected: `ok` across the board.

- [ ] **Step 19: Run the whole repo's test suite and `golangci-lint`**

Run: `go test ./... 2>&1 | tail -60`
Expected: `ok` for every package.

Run: `.tools/bin/golangci-lint run ./...` (install first with `make tools-lint` if `.tools/bin/golangci-lint` doesn't exist in this worktree)
Expected: `0 issues`.

- [ ] **Step 20: Commit**

```bash
git add internal/config/validate.go internal/config/config.go internal/msgid/config.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go internal/schemagen/schemas_test.go internal/config/testdata/erp-project.yaml internal/config/config_test.go internal/config/edge_test.go internal/config/replicas_test.go internal/k8s/k8s_test.go internal/k8s/servedby_test.go internal/cli/up_k8s_test.go internal/cli/graph.go internal/cli/graph_test.go internal/cli/up_dryrun_test.go internal/cli/sync_test.go
git commit -m "$(cat <<'EOF'
feat!: brickkit.yaml no longer accepts mode: debug

BREAKING: mode: debug can now only be set in override.yaml (design spec
§4) — brickkit.yaml's own validator rejects it unconditionally,
independent of deploy.target (previously it was legal under docker,
illegal only under k8s). No migration path is provided or needed: this
codebase has exactly one current user (design spec §11).

This is the last piece that makes override.yaml's core promise real: a
component's "which IDE, which debugger, which port on this machine"
state can no longer land in the shared, code-reviewed brickkit.yaml at
all, by construction — not just by convention.

Full test migration across every affected package:
- internal/config: the parse-level rejection test rewritten to prove it's
  unconditional (not k8s-specific), the golden full-project fixture and
  the five-states test updated, two tests whose whole premise ("debug is
  legal in brickkit.yaml, just needs care") is now false deleted outright
- internal/schemagen: the components[].mode constraint case moves debug
  from valid to invalid, keeping the JSON schema's enum and the validator
  in lockstep (schemas_test.go's own cross-check)
- internal/k8s + internal/cli/up_k8s_test.go: the k8s-specific "debug
  rejected" tests renamed/deleted now that the rule isn't k8s-specific —
  config-level coverage already proves it
- internal/cli/graph.go: removed the mode: debug rendering branch and its
  now-orphaned classLocal styling — this scenario is now permanently
  unreachable (brickkit.yaml can't contain the value, and graph
  deliberately never reads override.yaml, per design spec §9), so the
  code was actively misleading dead code, not harmless
- internal/cli/{up_dryrun,sync}_test.go: every CLI-level test that needs a
  real mode: debug component to exercise generation/cascade behavior now
  gets it from override.yaml instead, proving the exact same production
  code path Task 4 wired up

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Final note for the executor

After Task 7's commit, run the **whole** `make lint` target once (not just `go test`/`go vet`) before considering this plan done — it exercises `check-docs`, `check-schemas`, `check-cli-docs`, `check-i18n`, `check-cross-build`, `cover-check`, and `golangci-lint` together, several of which this plan's individual task steps only partially cover. `make check-cli-docs` is **expected to still fail** after Task 6 and Task 7 (documented explicitly in Task 6's commit message) — that gap is deliberately left for Plan 2b, which documents `brickkit override` together with its `add`/`remove`/`lint`/`restore` integration rather than twice. Every other `make lint` check should be green; if one isn't, treat it as a real finding, not something to route around.
