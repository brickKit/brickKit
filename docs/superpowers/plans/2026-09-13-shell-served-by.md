# servedBy（外壳合并部署）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `servedBy` field to `brickkit.yaml` so a component can declare "my workload is provided by another component (a shell)"; the platform skips generating that component's own container/Job but still computes correct `*_ENDPOINT` addresses for anything depending on it, on both Docker and K8s.

**Architecture:** New shared package `internal/shell` does all target-agnostic validation and merging once (existence/running-state/port-conflict checks, `*_ENDPOINT`+label merging with collision detection, `BRICKKIT_SERVED_MEMBERS` computation). `internal/compose` and `internal/k8s` each call it once inside their own `newPlan`, then do purely mechanical, target-specific rendering (Docker network aliases vs. K8s per-member Services). `cascade` and `inject.Build`'s main loop stay completely unaware of `servedBy`, exactly like they're unaware of `local: true` today.

**Tech Stack:** Go 1.22+, existing BrickKit internal packages (`config`, `manifest`, `resolver`, `cascade`, `inject`, `compose`, `k8s`, `clierr`, `yamlcheck`), `testify` for tests, plus the `market-server` Go module (separate module, shares the reserved-variable list by convention).

**Spec:** `docs/superpowers/specs/2026-09-13-shell-served-by-design.md` — read it before starting; this plan implements it section by section and calls out a few refinements found during implementation research (all noted inline where they occur).

## Global Constraints

- Exact versions only: `servedBy` values must be `<component-id>@<major.minor.patch>` — no bare IDs, no ranges (matches `manifest.IsExactVersion`, AGENTS.md §9.2).
- `local: true` is completely untouched by this feature — same field, same code paths, same behavior, before and after.
- `internal/shell.Resolve` is the *only* place that validates existence/running-state/port-conflicts/merge-collisions; `internal/compose` and `internal/k8s` must not duplicate any of that logic.
- Only `*_ENDPOINT`-class variables (`inject.Var{Source: inject.SourceEndpoint}`) and `labels` get merged into a shell's environment. `COMPONENT_ID`, `COMPONENT_VERSION`, `configSchema`-derived vars, and resource-connection vars (`DATABASE_*` etc.) are never merged — this is a hard rule, not a default that can be widened later without a new design discussion.
- v1 does not support `expose` / `exposePort` / `hostname` / `replicas` / `resources` / `serviceAccountName` on a `servedBy` component — these fields describe "how my own container is deployed," and a `servedBy` component has no container of its own. The platform warns (does not silently ignore, does not error) and otherwise proceeds normally.
- Commit after each task passes its tests (per repo convention), using `git commit` directly (no `cd` prefix) and ending messages with the attribution line from your dispatch instructions.

---

### Task 1: `servedBy` field + static validation

**Files:**
- Modify: `internal/config/config.go:232-274` (`Component` struct)
- Modify: `internal/config/validate.go:22-34` (`Validate`), and add a new function in the same file
- Test: `internal/config/config_test.go` (new test functions, appended at end of file)

**Interfaces:**
- Produces: `config.Component.ServedBy string` (yaml tag `servedBy,omitempty`) — a `<component-id>@<version>` string, empty when unset. Consumed by Task 2's `shell.Resolve` and Task 3/4's renderers via `entry.ServedBy`.

- [ ] **Step 1: Add the field**

In `internal/config/config.go`, inside the `Component` struct (around line 236-238, right after the existing `Enabled`/`Local`/`LocalPort` fields), add:

```go
	// ServedBy 表示这个组件的工作负载由另一个组件条目提供（外壳合并部署，
	// servedBy 设计书）。声明了它的组件不生成自己的容器/迁移 Job，但平台
	// 照常为依赖它的其它组件计算正确的 *_ENDPOINT——地址指向 servedBy
	// 指向的那个组件实际的位置，端口用这个组件自己声明的那个。
	ServedBy string `yaml:"servedBy,omitempty"`
```

Place it right after the `LocalPort` field so the three "does this component get its own container" fields (`Local`, `LocalPort`, `ServedBy`) sit together.

- [ ] **Step 2: Write the failing validation tests**

Append to `internal/config/config_test.go`:

```go
// ============================================================
// servedBy（外壳合并部署）
// ============================================================

func TestServedByFieldParsed(t *testing.T) {
	cfg, err := ParseConfig([]byte(baseConfig+`
components:
  - id: infra/shell-go-core
    version: 1.0.0
  - id: mdm/customer
    version: 1.0.7
    servedBy: infra/shell-go-core@1.0.0
`), "brickkit.yaml")
	require.NoError(t, err)
	require.Len(t, cfg.Components, 2)
	assert.Equal(t, "infra/shell-go-core@1.0.0", cfg.Components[1].ServedBy)
	assert.Empty(t, cfg.Components[0].ServedBy)
}

func TestServedByValidationErrors(t *testing.T) {
	cases := []struct {
		name     string
		yaml     string
		contains []string
	}{
		{"缺少版本号", baseConfig + `
components:
  - id: mdm/customer
    version: 1.0.7
    servedBy: infra/shell-go-core
`, []string{"components[0].servedBy", "id@version"}},
		{"版本号是范围约束", baseConfig + `
components:
  - id: mdm/customer
    version: 1.0.7
    servedBy: infra/shell-go-core@^1.0.0
`, []string{"components[0].servedBy", "精确版本"}},
		{"指向自己", baseConfig + `
components:
  - id: mdm/customer
    version: 1.0.7
    servedBy: mdm/customer@1.0.7
`, []string{"components[0].servedBy", "不能指向自己"}},
		{"链式嵌套", baseConfig + `
components:
  - id: infra/shell-a
    version: 1.0.0
    servedBy: infra/shell-b@1.0.0
  - id: infra/shell-b
    version: 1.0.0
    servedBy: infra/shell-c@1.0.0
`, []string{"components[0].servedBy", "链式嵌套"}},
		{"与 local 同时声明", baseConfig + `
components:
  - id: mdm/customer
    version: 1.0.7
    local: true
    servedBy: infra/shell-go-core@1.0.0
`, []string{"components[0].servedBy", "local: true"}},
		{"外壳自己是 local", baseConfig + `
components:
  - id: mdm/customer
    version: 1.0.7
    servedBy: infra/shell-go-core@1.0.0
  - id: infra/shell-go-core
    version: 1.0.0
    local: true
`, []string{"components[1].local", "servedBy 指向"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := ParseConfig([]byte(c.yaml), "brickkit.yaml")
			require.Error(t, err, "该配置应校验失败")
			assert.Nil(t, cfg)

			e := clierr.As(err)
			assert.Equal(t, clierr.CodeConfigInvalid, e.Code)
			out := e.Format()
			for _, want := range c.contains {
				assert.Contains(t, out, want)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/config/... -run TestServedBy -v`
Expected: `TestServedByFieldParsed` fails (unknown field `servedBy` rejected by `yamlcheck`), `TestServedByValidationErrors` fails to compile or fails (no `ServedBy` field yet, no validation rejects any of these).

- [ ] **Step 3: Implement — add the static validation function**

In `internal/config/validate.go`, change `Validate()` (currently lines 23-34) to add one call:

```go
// Validate 校验 brickkit.yaml 的全部字段，一次返回所有问题。
func (c *Config) Validate() error {
	p := newConfigProblems(c.Source)

	c.validateProject(p)
	c.validateDeploy(p)
	c.validateSources(p)
	c.validateComponents(p)
	c.validateServedBy(p)
	c.validateResources(p)
	c.validateResourceEnvCollisions(p)

	return p.Err()
}
```

Then add this new function anywhere below `validateComponents` in the same file:

```go
// validateServedBy 静态校验 servedBy 字段本身：格式对不对、有没有自引用、
// 有没有链式嵌套、有没有跟 local: true 打架——这些都不需要依赖图，在解析
// 阶段就能查完。存在性、运行态、端口冲突、合并后的环境变量/标签冲突需要
// 依赖图 + cascade 结果，在 internal/shell.Resolve 里查
// （servedBy 设计书 §5-§6）。
func (c *Config) validateServedBy(p *clierr.ProblemSet) {
	declaresServedBy := make(map[string]bool, len(c.Components))
	for _, item := range c.Components {
		if item.ServedBy != "" {
			declaresServedBy[item.Ref()] = true
		}
	}

	// 谁被谁 servedBy 指向：下面第二轮要反过来查目标是不是 local: true
	servedByTarget := make(map[string]bool, len(c.Components))

	for i, item := range c.Components {
		if item.ServedBy == "" {
			continue
		}
		field := indexed("components", i) + ".servedBy"

		if item.Local {
			p.Add(field, "不能跟 local: true 同时声明——local 是本机调试，"+
				"servedBy 是代码已经打进另一个外壳镜像，两者是矛盾的意图")
			continue
		}

		id, version, found := strings.Cut(item.ServedBy, "@")
		if !found || id == "" || version == "" {
			p.Addf(field, "必须是 <组件ID>@<精确版本>（id@version）形式（当前是 %s）", item.ServedBy)
			continue
		}
		if !manifest.IsExactVersion(version) {
			p.Addf(field, "版本必须是精确版本 major.minor.patch，不接受 ^ 或 ~ 等范围约束（当前是 %s）", version)
			continue
		}

		target := id + "@" + version
		if target == item.Ref() {
			p.Add(field, "不能指向自己")
			continue
		}
		if declaresServedBy[target] {
			p.Addf(field, "指向的 %s 自己也声明了 servedBy，不能链式嵌套"+
				"（一个外壳不能被另一个外壳收编）", target)
			continue
		}
		servedByTarget[target] = true
	}

	for i, item := range c.Components {
		if item.Local && servedByTarget[item.Ref()] {
			p.Addf(indexed("components", i)+".local",
				"%s 被别的组件 servedBy 指向，不能同时是 local: true"+
					"（外壳要能在集群/容器网络里被访问到，跑在开发者本机上做不到这件事）",
				item.Ref())
		}
	}
}
```

`strings` and `manifest` are already imported at the top of `validate.go` — no new imports needed.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/config/... -run TestServedBy -v`
Expected: PASS

- [ ] **Step 5: Run the whole package's tests to check for regressions**

Run: `go test ./internal/config/...`
Expected: PASS (no existing test should reference `servedBy` or be affected by the new field/validation)

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/validate.go internal/config/config_test.go
git commit -m "$(cat <<'EOF'
新增 servedBy 字段与静态校验（外壳合并部署第一步）

只做不需要依赖图就能查的部分：格式（id@version，精确版本）、禁止自指、
禁止链式嵌套、跟 local: true 互斥。存在性/运行态/端口冲突/合并冲突需要
依赖图，留给下一步的 internal/shell 包。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: `internal/shell` package (shared resolve/merge logic)

**Files:**
- Create: `internal/shell/shell.go`
- Modify: `internal/inject/reserved.go:16` (add `BRICKKIT_SERVED_MEMBERS` to `reservedExact`)
- Modify: `internal/inject/inject_test.go:622-642` (`TestReservedPatternsCoverPlatformAndResourceVariables`, extend with the new reserved var)
- Modify: `market-server/internal/validator/reserved.go:17` (same addition, separate Go module — see comment at `internal/inject/reserved.go:10-14` explaining why the two copies must stay in sync)
- Test: `internal/shell/shell_test.go`

**Interfaces:**
- Consumes: `config.Config`, `config.Component.ServedBy` (Task 1), `resolver.Graph`/`resolver.Ref`/`resolver.Node`, `cascade.Result`, `inject.Result`/`inject.Component`/`inject.Var`/`inject.SourceEndpoint`, `manifest.ServiceName`, `clierr.Error`/`clierr.New`/`clierr.Newf`/`clierr.CodeConfigInvalid`/`clierr.CodePortConflict`.
- Produces (consumed by Task 3 & 4):
  - `shell.EnvVarServedMembers string` (constant `"BRICKKIT_SERVED_MEMBERS"`)
  - `shell.ParseRef(raw string) (resolver.Ref, bool)`
  - `shell.Group{Shell resolver.Ref; Members []Member; Env []inject.Var; Labels map[string]string}`
  - `shell.Member{Ref resolver.Ref; Port int}`
  - `func (g Group) ServedMembers() string`
  - `func Apply(shellEnv []inject.Var, g Group) []inject.Var`
  - `func Resolve(cfg *config.Config, graph *resolver.Graph, states *cascade.Result, env *inject.Result) ([]Group, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/shell/shell_test.go`:

```go
// 本文件测试 internal/shell 的校验与合并逻辑——这是 servedBy 唯一的一份
// 目标无关逻辑，Docker（internal/compose）与 K8s（internal/k8s）都只消费
// 它的结果，不重新实现任何一条规则（servedBy 设计书 §3）。
package shell_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
)

type stubProvider map[string]*manifest.Manifest

func (p stubProvider) Manifest(_ context.Context, id, version string) (*manifest.Manifest, error) {
	m, ok := p[id+"@"+version]
	if !ok {
		return nil, errNotFound{id + "@" + version}
	}
	return m, nil
}

type errNotFound struct{ ref string }

func (e errNotFound) Error() string { return "夹具里没有 " + e.ref }

func simple(id, version string, port int) *manifest.Manifest {
	return &manifest.Manifest{
		APIVersion: manifest.APIVersion, Kind: manifest.Kind,
		Metadata:   manifest.Metadata{ID: id, Name: id, Version: version},
		Deployment: manifest.Deployment{Type: manifest.DeploymentTypeContainer, Image: "x:" + version, Port: port},
		HealthCheck: manifest.HealthCheck{Type: manifest.HealthCheckHTTP, Path: "/healthz"},
	}
}

func dependsOn(m *manifest.Manifest, id, version string) *manifest.Manifest {
	if m.Dependencies == nil {
		m.Dependencies = &manifest.Dependencies{}
	}
	m.Dependencies.Components = append(m.Dependencies.Components, manifest.ComponentDep{ID: id, Version: version})
	return m
}

// resolveFixture 跑完整条链路（解析依赖图 → 级联 → 注入 → shell.Resolve），
// 供每个用例只关心自己要断言的那一小段。
func resolveFixture(t *testing.T, cfg *config.Config, manifests map[string]*manifest.Manifest) ([]shell.Group, error) {
	t.Helper()

	provider := stubProvider(manifests)
	var roots []resolver.Ref
	for _, c := range cfg.Components {
		roots = append(roots, resolver.Ref{ID: c.ID, Version: c.Version})
	}

	graph, err := resolver.New(provider).Resolve(context.Background(), roots...)
	require.NoError(t, err)

	states, err := cascade.Compute(cfg, graph)
	require.NoError(t, err)

	env, err := inject.Build(cfg, graph, states)
	require.NoError(t, err)

	return shell.Resolve(cfg, graph, states, env)
}

func comp(id, version, servedBy string) config.Component {
	return config.Component{ID: id, Version: version, ServedBy: servedBy}
}

// ---- 基本分组 ----

func TestResolveGroupsMemberUnderItsShell(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
	}}
	groups, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        simple("mdm/customer", "1.0.7", 8080),
	})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, resolver.Ref{ID: "infra/shell-go-core", Version: "1.0.0"}, groups[0].Shell)
	require.Len(t, groups[0].Members, 1)
	assert.Equal(t, resolver.Ref{ID: "mdm/customer", Version: "1.0.7"}, groups[0].Members[0].Ref)
	assert.Equal(t, 8080, groups[0].Members[0].Port)
}

// ---- 存在性 / 运行态 ----

func TestResolveErrorsWhenShellDoesNotExist(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
	}}
	_, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"mdm/customer@1.0.7": simple("mdm/customer", "1.0.7", 8080),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不存在")
}

func TestResolveErrorsWhenShellIsDisabled(t *testing.T) {
	disabled := false
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		{ID: "infra/shell-go-core", Version: "1.0.0", Enabled: &disabled},
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
	}}
	_, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        simple("mdm/customer", "1.0.7", 8080),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "没有在运行")
}

// ---- 端口冲突 ----

func TestResolveErrorsOnPortConflictWithinGroup(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
		comp("erp/sales", "1.0.0", "infra/shell-go-core@1.0.0"),
	}}
	_, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        simple("mdm/customer", "1.0.7", 8080),
		"erp/sales@1.0.0":           simple("erp/sales", "1.0.0", 8080), // 撞了 mdm/customer
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "8080")
}

// ---- 环境变量合并：范围只收 *_ENDPOINT ----

func TestResolveMergesOnlyEndpointVars(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
		comp("infra/database", "1.0.0", ""),
	}}
	member := dependsOn(simple("mdm/customer", "1.0.7", 8080), "infra/database", "1.0.0")
	member.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"pgSchema": {Type: "string", Default: "mdm_customer"},
	}}

	groups, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        member,
		"infra/database@1.0.0":      simple("infra/database", "1.0.0", 5432),
	})
	require.NoError(t, err)
	require.Len(t, groups, 1)

	env := groups[0].Env
	names := make([]string, 0, len(env))
	for _, v := range env {
		names = append(names, v.Name)
	}
	assert.Contains(t, names, "INFRA_DATABASE_ENDPOINT", "依赖端点必须被合并")
	assert.NotContains(t, names, "PG_SCHEMA", "组件自己的配置绝不能进合并范围")
	assert.NotContains(t, names, "COMPONENT_ID", "身份变量绝不能进合并范围")
	assert.NotContains(t, names, "COMPONENT_VERSION")
}

// ---- 环境变量合并：同名同值放过，同名不同值报错 ----

func TestResolveEndpointCollisionSameValueIsFine(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
		comp("erp/sales", "1.0.0", "infra/shell-go-core@1.0.0"),
		comp("infra/database", "1.0.0", ""),
	}}
	a := dependsOn(simple("mdm/customer", "1.0.7", 8080), "infra/database", "1.0.0")
	b := dependsOn(simple("erp/sales", "1.0.0", 8081), "infra/database", "1.0.0")

	groups, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        a,
		"erp/sales@1.0.0":           b,
		"infra/database@1.0.0":      simple("infra/database", "1.0.0", 5432),
	})
	require.NoError(t, err, "两个成员依赖同一个组件的同一个版本，端点值相同，不该报冲突")
	require.Len(t, groups, 1)
}

func TestResolveEndpointCollisionDifferentVersionErrors(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
		comp("erp/sales", "1.0.0", "infra/shell-go-core@1.0.0"),
		comp("infra/database", "1.0.0", ""),
		comp("infra/database", "2.0.0", ""),
	}}
	a := dependsOn(simple("mdm/customer", "1.0.7", 8080), "infra/database", "1.0.0")
	b := dependsOn(simple("erp/sales", "1.0.0", 8081), "infra/database", "2.0.0")

	_, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        a,
		"erp/sales@1.0.0":           b,
		"infra/database@1.0.0":      simple("infra/database", "1.0.0", 5432),
		"infra/database@2.0.0":      simple("infra/database", "2.0.0", 5432),
	})
	require.Error(t, err, "同一个变量名不可能同时指向两个不同版本的地址")
	assert.Contains(t, err.Error(), "INFRA_DATABASE_ENDPOINT")
}

// ---- BRICKKIT_SERVED_MEMBERS ----

func TestServedMembersFormatting(t *testing.T) {
	g := shell.Group{Members: []shell.Member{
		{Ref: resolver.Ref{ID: "erp/sales", Version: "1.0.0"}},
		{Ref: resolver.Ref{ID: "mdm/customer", Version: "1.0.7"}},
	}}
	assert.Equal(t, "erp-sales-1-0-0,mdm-customer-1-0-7", g.ServedMembers(),
		"按字典序排列，逗号分隔")
}

func TestServedMembersEmptyWhenNoMembers(t *testing.T) {
	assert.Equal(t, "", shell.Group{}.ServedMembers(),
		"零个成员时是空字符串——这个空字符串本身就是信号，不是变量缺失")
}

func TestApplyUpsertsEndpointsAndServedMembers(t *testing.T) {
	shellEnv := []inject.Var{
		{Name: "COMPONENT_ID", Value: "infra/shell-go-core", Source: inject.SourcePlatform},
	}
	g := shell.Group{
		Members: []shell.Member{{Ref: resolver.Ref{ID: "mdm/customer", Version: "1.0.7"}}},
		Env:     []inject.Var{{Name: "INFRA_DATABASE_ENDPOINT", Value: "http://infra-database-1-0-0:5432", Source: inject.SourceEndpoint}},
	}

	out := shell.Apply(shellEnv, g)

	byName := map[string]string{}
	for _, v := range out {
		byName[v.Name] = v.Value
	}
	assert.Equal(t, "infra/shell-go-core", byName["COMPONENT_ID"], "外壳自己的变量不受影响")
	assert.Equal(t, "http://infra-database-1-0-0:5432", byName["INFRA_DATABASE_ENDPOINT"])
	assert.Equal(t, "mdm-customer-1-0-7", byName[shell.EnvVarServedMembers])
}

// ---- 同一个外壳收编同一个组件的两个不同版本（版本迁移期间的真实用法：
// 一部分调用方还依赖旧版本，一部分已经切到新版本，两个版本同时活在
// 同一个外壳里）----

func TestResolveGroupsTwoVersionsOfSameComponentUnderOneShell(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
		comp("mdm/customer", "2.0.0", "infra/shell-go-core@1.0.0"),
	}}
	groups, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        simple("mdm/customer", "1.0.7", 8080),
		"mdm/customer@2.0.0":        simple("mdm/customer", "2.0.0", 8081),
	})
	require.NoError(t, err, "同一个逻辑组件的两个版本，只要端口不同，同一个外壳完全装得下")
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Members, 2)

	byVersion := map[string]shell.Member{}
	for _, m := range groups[0].Members {
		byVersion[m.Ref.Version] = m
	}
	assert.Equal(t, 8080, byVersion["1.0.7"].Port)
	assert.Equal(t, 8081, byVersion["2.0.0"].Port)
}

func TestResolveErrorsWhenTwoVersionsOfSameComponentSharePort(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
		comp("mdm/customer", "2.0.0", "infra/shell-go-core@1.0.0"),
	}}
	_, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        simple("mdm/customer", "1.0.7", 8080),
		"mdm/customer@2.0.0":        simple("mdm/customer", "2.0.0", 8080), // 忘了改端口
	})
	require.Error(t, err, "两份代码要在同一个进程里各自监听，端口不能一样，哪怕是同一个逻辑组件的两个版本")
	assert.Contains(t, err.Error(), "8080")
}

// ---- ParseRef ----

func TestParseRef(t *testing.T) {
	ref, ok := shell.ParseRef("infra/shell-go-core@1.0.0")
	require.True(t, ok)
	assert.Equal(t, resolver.Ref{ID: "infra/shell-go-core", Version: "1.0.0"}, ref)

	_, ok = shell.ParseRef("infra/shell-go-core")
	assert.False(t, ok, "没有 @ 就解析不出版本")
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/shell/... -v`
Expected: FAIL — `internal/shell` package doesn't exist yet (build failure).

- [ ] **Step 3: Implement `internal/shell/shell.go`**

```go
// Package shell 计算外壳合并部署（servedBy）分组。
//
// 这是 Docker（internal/compose）与 K8s（internal/k8s）两个渲染器共用的
// 唯一一份校验与合并逻辑——两者过去对结构相似的问题（local: true）各自
// 独立实现分支，而 servedBy 明确要求"两边逻辑一致"，同一份逻辑写两遍
// 只会悄悄跑偏，所以单独收进这个包，两边渲染器只消费它的结果。
//
// servedBy 字段本身的语法、自引用、链式嵌套、跟 local: true 互斥已经在
// config.Validate 里静态查过（不需要依赖图就能查），这里只做需要依赖图 +
// cascade 结果才能查出来的部分：目标存不存在、有没有在跑、端口撞不撞车、
// 合并后的环境变量/labels 撞不撞值（servedBy 设计书 §5-§7）。
package shell

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
)

// EnvVarServedMembers 是外壳容器上"当前实际收编了哪些成员"的保留变量名
// （设计书 §7）。它是精确匹配的保留变量，登记在
// internal/inject/reserved.go 与 market-server/internal/validator/reserved.go
// 的 reservedExact 里，任何组件的 configSchema 都不能声明出这个变量名。
const EnvVarServedMembers = "BRICKKIT_SERVED_MEMBERS"

// SourceServed 标记 BRICKKIT_SERVED_MEMBERS 这条变量的来源，
// 与 inject.SourceEndpoint 等常量同一用途（--verbose 输出、排障）。
const SourceServed = "外壳收编"

// Group 是一个外壳与它当前收编的成员。
type Group struct {
	Shell   resolver.Ref
	Members []Member
	// Env 是全部成员贡献的 *_ENDPOINT 类变量，已经和外壳自己的同类变量、
	// 以及成员相互之间做过合并与冲突检查（同名同值跳过，同名不同值报错）。
	Env []inject.Var
	// Labels 是全部成员贡献的透传标签，合并规则与 Env 相同。
	Labels map[string]string
}

// Member 是被外壳收编的一个组件。
type Member struct {
	Ref resolver.Ref
	// Port 是该组件自己 component.yaml 声明的 deployment.port。
	Port int
}

// ServedMembers 返回这个外壳该写进 BRICKKIT_SERVED_MEMBERS 的值：当前
// 收编成员的版本化服务名，逗号分隔、按字典序排列。零个成员时返回空字符
// 串——这个空字符串本身就是"零个成员激活"的信号，外壳读到空字符串必须
// 一个模块都不初始化，不能当成"变量不存在"去回退成全部启动（这两种语义
// 不能合并处理，设计书 §7）。
func (g Group) ServedMembers() string {
	names := make([]string, 0, len(g.Members))
	for _, m := range g.Members {
		names = append(names, manifest.ServiceName(m.Ref.ID, m.Ref.Version))
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// Apply 把这个外壳分组合并进外壳自己已经算好的环境变量：先放外壳自己的，
// 再把 Group.Env 与 BRICKKIT_SERVED_MEMBERS 逐个 upsert 进去（同名覆盖，
// 不存在则追加），按变量名排序返回，保证生成物稳定可比对。
//
// 两个渲染器（compose / k8s）都调用这一个函数，不各写一份"怎么把
// Group 摊平进环境变量"——这正是这个包存在的理由。
func Apply(shellEnv []inject.Var, g Group) []inject.Var {
	byName := make(map[string]inject.Var, len(shellEnv)+len(g.Env)+1)
	for _, v := range shellEnv {
		byName[v.Name] = v
	}
	for _, v := range g.Env {
		byName[v.Name] = v
	}
	byName[EnvVarServedMembers] = inject.Var{
		Name: EnvVarServedMembers, Value: g.ServedMembers(), Source: SourceServed,
	}

	out := make([]inject.Var, 0, len(byName))
	for _, v := range byName {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ParseRef 把 "id@version" 拆成 Ref。config.Validate 已经保证一个非空
// 的 servedBy 字符串必然是合法的 id@version（internal/config/validate.go
// 的 validateServedBy），但 ParseRef 本身不做这个假设，格式不对就返回
// false——调用方（Resolve、两个渲染器）据此决定要不要继续处理。
func ParseRef(raw string) (resolver.Ref, bool) {
	id, version, found := strings.Cut(raw, "@")
	if !found || id == "" || version == "" {
		return resolver.Ref{}, false
	}
	return resolver.Ref{ID: id, Version: version}, true
}

// Resolve 计算本次生成里全部的外壳分组。
//
// 出错即返回（不是一次性收集全部问题）：与本包同类的其它生成期校验
// （compose.checkExposePorts、k8s.checkHostnameUnique、
// k8s.localNotSupported）都是这个约定——使用者改一处、重新 up、再看下
// 一个问题，跟本项目"生成器只报第一个问题"的既有习惯保持一致。
func Resolve(
	cfg *config.Config, graph *resolver.Graph, states *cascade.Result, env *inject.Result,
) ([]Group, error) {
	if cfg == nil || graph == nil || states == nil || env == nil {
		return nil, nil
	}

	entries := make(map[resolver.Ref]config.Component, len(cfg.Components))
	for _, c := range cfg.Components {
		entries[resolver.Ref{ID: c.ID, Version: c.Version}] = c
	}
	envByRef := make(map[resolver.Ref]inject.Component, len(env.Components))
	for _, c := range env.Components {
		envByRef[c.Ref] = c
	}

	byShell := map[resolver.Ref][]Member{}
	var order []resolver.Ref

	for _, ref := range states.Running() {
		entry := entries[ref]
		if entry.ServedBy == "" {
			continue
		}
		target, ok := ParseRef(entry.ServedBy)
		if !ok {
			continue // config.Validate 已经挡过格式问题，这里只做防御
		}

		targetNode := graph.Node(target)
		switch {
		case targetNode == nil:
			return nil, shellNotFoundError(ref, target)
		case !states.IsRunning(target):
			return nil, shellNotRunningError(ref, target)
		}

		node := graph.Node(ref)
		if node == nil || node.Manifest == nil {
			continue
		}
		if _, seen := byShell[target]; !seen {
			order = append(order, target)
		}
		byShell[target] = append(byShell[target], Member{Ref: ref, Port: node.Manifest.Deployment.Port})
	}

	sort.Slice(order, func(i, j int) bool { return order[i].String() < order[j].String() })

	groups := make([]Group, 0, len(order))
	for _, shellRef := range order {
		members := byShell[shellRef]
		sort.Slice(members, func(i, j int) bool { return members[i].Ref.String() < members[j].Ref.String() })

		if err := checkPortConflicts(shellRef, graph.Node(shellRef), members); err != nil {
			return nil, err
		}
		mergedEnv, mergedLabels, err := mergeGroup(shellRef, envByRef[shellRef], members, envByRef)
		if err != nil {
			return nil, err
		}
		groups = append(groups, Group{Shell: shellRef, Members: members, Env: mergedEnv, Labels: mergedLabels})
	}
	return groups, nil
}

// checkPortConflicts 校验外壳自己的端口 + 全部成员的端口互不冲突——它们
// 最终都要在同一个容器/Pod 上监听。只查主端口（deployment.port），不查
// extraPorts：一个成员的额外端口撞车，后果是外壳进程自己在那个端口上
// bind 失败、当场崩溃退出——这本身就是一次响亮的失败，不是需要平台在
// 生成阶段replicate 一遍的静默失败，v1 不做这层校验（YAGNI）。
func checkPortConflicts(shellRef resolver.Ref, shellNode *resolver.Node, members []Member) *clierr.Error {
	claimed := map[int]string{}
	claim := func(port int, owner string) *clierr.Error {
		if port == 0 {
			return nil
		}
		if previous, taken := claimed[port]; taken && previous != owner {
			return clierr.Newf(clierr.CodePortConflict,
				"错误：外壳 %s 上有两个组件都要用端口 %d", shellRef.String(), port).
				WithDetail("占用方", previous).
				WithDetail("占用方", owner).
				WithHint("这几个组件最终都跑在同一个外壳容器/Pod 里，端口必须互不相同")
		}
		claimed[port] = owner
		return nil
	}

	if shellNode != nil && shellNode.Manifest != nil {
		if err := claim(shellNode.Manifest.Deployment.Port, "外壳自己"); err != nil {
			return err
		}
	}
	for _, m := range members {
		if err := claim(m.Port, "组件 "+m.Ref.String()); err != nil {
			return err
		}
	}
	return nil
}

// mergeGroup 合并外壳自己的端点变量/透传标签与全部成员的端点变量/标签，
// 同名同值跳过，同名不同值报错（设计书 §6）。
func mergeGroup(
	shellRef resolver.Ref, shellComponent inject.Component, members []Member,
	envByRef map[resolver.Ref]inject.Component,
) ([]inject.Var, map[string]string, *clierr.Error) {
	envValues := map[string]string{}
	envVars := map[string]inject.Var{}
	envOwner := map[string]resolver.Ref{}
	for _, v := range shellComponent.Env {
		if v.Source != inject.SourceEndpoint {
			continue
		}
		envValues[v.Name] = v.Value
		envVars[v.Name] = v
		envOwner[v.Name] = shellRef
	}

	labelValues := map[string]string{}
	labelOwner := map[string]resolver.Ref{}
	for key, value := range shellComponent.Labels {
		labelValues[key] = value
		labelOwner[key] = shellRef
	}

	for _, m := range members {
		mEnv := envByRef[m.Ref]
		for _, v := range mEnv.Env {
			if v.Source != inject.SourceEndpoint {
				continue
			}
			if existing, exists := envValues[v.Name]; exists {
				if existing == v.Value {
					continue
				}
				return nil, nil, endpointCollisionError(shellRef, envOwner[v.Name], m.Ref, v.Name, existing, v.Value)
			}
			envValues[v.Name] = v.Value
			envVars[v.Name] = v
			envOwner[v.Name] = m.Ref
		}
		for key, value := range mEnv.Labels {
			if existing, exists := labelValues[key]; exists {
				if existing == value {
					continue
				}
				return nil, nil, labelCollisionError(shellRef, labelOwner[key], m.Ref, key, existing, value)
			}
			labelValues[key] = value
			labelOwner[key] = m.Ref
		}
	}

	env := make([]inject.Var, 0, len(envVars))
	for _, v := range envVars {
		env = append(env, v)
	}
	sort.Slice(env, func(i, j int) bool { return env[i].Name < env[j].Name })

	var labels map[string]string
	if len(labelValues) > 0 {
		labels = labelValues
	}
	return env, labels, nil
}

func shellNotFoundError(member, target resolver.Ref) *clierr.Error {
	return clierr.New(clierr.CodeConfigInvalid, "错误：servedBy 指向的组件不存在").
		WithDetail("组件", member.String()).
		WithDetailf("servedBy 指向", "%s（当前项目的 components: 里没有这一条）", target.String()).
		WithHint("检查 servedBy 的值有没有写错组件 ID 或版本号")
}

func shellNotRunningError(member, target resolver.Ref) *clierr.Error {
	return clierr.New(clierr.CodeConfigInvalid, "错误：servedBy 指向的外壳当前没有在运行").
		WithDetail("组件", member.String()).
		WithDetail("servedBy 指向", target.String()).
		WithDetail("原因", "这个组件在跑，但它声明的外壳被禁用或没有启动，代码没有地方可以运行").
		WithHint(
			"确认外壳组件没有被 enabled: false 关掉",
			"或者去掉这个组件的 servedBy，让它照常独立部署",
		)
}

func endpointCollisionError(
	shellRef, firstOwner, secondOwner resolver.Ref, name, firstValue, secondValue string,
) *clierr.Error {
	return clierr.Newf(clierr.CodeConfigInvalid,
		"错误：外壳 %s 下两个成员对同一个环境变量给出了不同的值", shellRef.String()).
		WithDetail("变量名", name).
		WithDetailf(firstOwner.String(), "%s", firstValue).
		WithDetailf(secondOwner.String(), "%s", secondValue).
		WithDetail("原因", "这两个组件各自依赖同一个组件 ID 的不同精确版本——独立部署时互不冲突，"+
			"合并进同一个外壳的共享环境后，同一个变量名不可能同时指向两个地址").
		WithHint("让这两个成员依赖同一个精确版本，或者不要把它们放进同一个外壳")
}

func labelCollisionError(
	shellRef, firstOwner, secondOwner resolver.Ref, key, firstValue, secondValue string,
) *clierr.Error {
	return clierr.Newf(clierr.CodeConfigInvalid,
		"错误：外壳 %s 下两个成员对同一个透传标签给出了不同的值", shellRef.String()).
		WithDetail("标签键", key).
		WithDetailf(firstOwner.String(), "%s", firstValue).
		WithDetailf(secondOwner.String(), "%s", secondValue).
		WithHint("给其中一个成员改用不同的标签键，或者统一成同一个值")
}
```

- [ ] **Step 4: Run to verify the new package's tests pass**

Run: `go test ./internal/shell/... -v`
Expected: PASS on all tests listed in Step 1.

- [ ] **Step 5: Register `BRICKKIT_SERVED_MEMBERS` as a reserved variable (CLI side)**

In `internal/inject/reserved.go:16`, change:

```go
	reservedExact  = []string{"COMPONENT_ID", "COMPONENT_VERSION"}
```

to:

```go
	reservedExact  = []string{"COMPONENT_ID", "COMPONENT_VERSION", "BRICKKIT_SERVED_MEMBERS"}
```

Extend the existing coverage test in `internal/inject/inject_test.go` (currently lines 622-642, `TestReservedPatternsCoverPlatformAndResourceVariables`) — add one property and one assertion:

```go
func TestReservedPatternsCoverPlatformAndResourceVariables(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"componentId":         {Default: "冒充组件 ID"},
		"databaseHost":        {Default: "冒充数据库地址"},
		"redisPort":           {Default: 1234},
		"smtpUser":            {Default: "x"},
		"brickkitServedMembers": {Default: "冒充外壳收编列表"},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{})

	result := b.build()
	env := envOf(t, result, "people/basic")

	assert.Equal(t, "people/basic", env["COMPONENT_ID"], "平台值优先")
	assert.NotContains(t, env, "DATABASE_HOST", "没绑数据库就不该凭空出现这个变量")
	assert.NotContains(t, env, "REDIS_PORT")
	assert.NotContains(t, env, "SMTP_USER")
	assert.NotContains(t, env, "BRICKKIT_SERVED_MEMBERS", "组件自己的配置不能冒充这个平台保留变量")
	assert.Len(t, result.Warnings, 5, "五个冲突各有一条警告")
}
```

- [ ] **Step 6: Register the same reserved variable in `market-server`**

In `market-server/internal/validator/reserved.go:17`, change:

```go
	reservedExact = []string{"COMPONENT_ID", "COMPONENT_VERSION"}
```

to:

```go
	reservedExact = []string{"COMPONENT_ID", "COMPONENT_VERSION", "BRICKKIT_SERVED_MEMBERS"}
```

Check `market-server/internal/validator/reserved_test.go`'s `TestMatchReserved` (around line 53) — if it enumerates `reservedExact` members one by one, add a case for `"BRICKKIT_SERVED_MEMBERS"` following the exact same style as the existing `"COMPONENT_ID"`/`"COMPONENT_VERSION"` cases in that test.

- [ ] **Step 7: Run both modules' full test suites**

Run: `go test ./internal/... && cd market-server && go test ./... && cd ..`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/shell internal/inject/reserved.go internal/inject/inject_test.go market-server/internal/validator/reserved.go market-server/internal/validator/reserved_test.go
git commit -m "$(cat <<'EOF'
新增 internal/shell 包：servedBy 分组、校验与合并（外壳合并部署第二步）

Docker（internal/compose）与 K8s（internal/k8s）接下来共用这一份逻辑，
不再各写一份——校验存在性/运行态/端口冲突，合并范围只收 *_ENDPOINT 类
变量与 labels（同名同值跳过，同名不同值报错），COMPONENT_ID/
COMPONENT_VERSION/configSchema 派生变量/资源连接变量一律不进合并。
BRICKKIT_SERVED_MEMBERS 登记为平台保留变量（CLI 与 market-server 两处）。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Docker rendering (`internal/compose`)

**Files:**
- Create: `internal/compose/servedby.go`
- Modify: `internal/compose/compose.go:189-263` (`newPlan`), `:157-187` (`plan` struct), `:376-400` (`containerIDs`/`componentIDs`), `:406-442` (`componentService`)
- Test: `internal/compose/servedby_test.go`

**Interfaces:**
- Consumes: `internal/shell` (Task 2) — `shell.Resolve`, `shell.Group`, `shell.Apply`, `shell.ParseRef`.
- Produces: nothing new consumed outside this package; `compose.Generate`'s public signature is unchanged.

- [ ] **Step 1: Write the failing tests**

Create `internal/compose/servedby_test.go`:

```go
// 本文件测试 servedBy（外壳合并部署）在 Docker 目标下的渲染，覆盖
// servedBy 设计书 §6-§8。local: true 的既有行为不受影响，回归覆盖见
// TestLocalStillWorksAlongsideServedBy。
package compose_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/shell"
)

// servedByEntry 是一个 servedBy 组件的 brickkit.yaml 条目。
func servedByEntry(shellID, shellVersion string) config.Component {
	return config.Component{ServedBy: shellID + "@" + shellVersion}
}

// ---- 不生成自己的容器/迁移 ----

func TestServedByComponentGeneratesNoContainer(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(withMigration(simple("mdm/customer", "1.0.7", 8080)),
		servedByEntry("infra/shell-go-core", "1.0.0"))

	services := servicesOf(t, b.parsed())
	assert.NotContains(t, services, "mdm-customer-1-0-7", "servedBy 组件不该有自己的 service")
	assert.NotContains(t, services, "mdm-customer-1-0-7-migration", "也不该有自己的迁移 service")
	assert.Contains(t, services, "infra-shell-go-core-1-0-0", "外壳自己照常生成")
}

// ---- 网络别名 ----

func TestShellGetsNetworkAliasForEachMember(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))

	svc := serviceOf(t, b.parsed(), "infra-shell-go-core-1-0-0")
	networks, ok := svc["networks"].(map[string]any)
	require.True(t, ok, "有 servedBy 成员时，networks 必须是带别名的映射形式：%v", svc["networks"])
	net, ok := networks["brickkit-net"].(map[string]any)
	require.True(t, ok)
	aliases, ok := net["aliases"].([]any)
	require.True(t, ok)
	assert.Contains(t, aliases, "mdm-customer-1-0-7")
}

func TestShellWithoutMembersUsesPlainNetworkList(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})

	svc := serviceOf(t, b.parsed(), "infra-shell-go-core-1-0-0")
	_, isList := svc["networks"].([]any)
	assert.True(t, isList, "没有 servedBy 成员时，普通组件的 networks 渲染形式不该变")
}

// ---- 合并环境变量 + BRICKKIT_SERVED_MEMBERS ----

func TestShellEnvGetsMergedEndpointsAndServedMembers(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(dependsOn(simple("mdm/customer", "1.0.7", 8080), "infra/database", "1.0.0"),
		servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(simple("infra/database", "1.0.0", 5432), config.Component{})

	env := envOf(t, serviceOf(t, b.parsed(), "infra-shell-go-core-1-0-0"))
	assert.Equal(t, "http://infra-database-1-0-0:5432", env["INFRA_DATABASE_ENDPOINT"])
	assert.Equal(t, "mdm-customer-1-0-7", env[shell.EnvVarServedMembers])
}

func TestShellServedMembersIsEmptyStringWhenMemberNotRunning(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	disabled := false
	b.component(simple("mdm/customer", "1.0.7", 8080),
		config.Component{ServedBy: "infra/shell-go-core@1.0.0", Enabled: &disabled})

	env := envOf(t, serviceOf(t, b.parsed(), "infra-shell-go-core-1-0-0"))
	value, ok := env[shell.EnvVarServedMembers]
	require.True(t, ok, "外壳一直在跑，变量必须存在，即使是空字符串")
	assert.Equal(t, "", value)
}

// ---- 迁移 / 健康检查 / 不支持字段的警告 ----

func TestServedByMigrationWarns(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(withMigration(simple("mdm/customer", "1.0.7", 8080)), servedByEntry("infra/shell-go-core", "1.0.0"))

	result, err := b.build(compose.Options{})
	require.NoError(t, err)
	require.Len(t, result.Warnings, 1)
	assert.Equal(t, clierr.CodeMigrationSkipped, result.Warnings[0].Code)
	assert.Contains(t, result.Warnings[0].Format(), "外壳")
}

func TestServedByHealthCheckWarns(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))

	result, err := b.build(compose.Options{})
	require.NoError(t, err)
	found := false
	for _, w := range result.Warnings {
		if w.Code == clierr.CodeConfigInvalid && strings.Contains(w.Format(), "健康检查") {
			found = true
		}
	}
	assert.True(t, found, "应该有一条关于健康检查不生效的警告：%+v", result.Warnings)
}

func TestServedByUnsupportedFieldsWarn(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	entry := servedByEntry("infra/shell-go-core", "1.0.0")
	entry.Expose = true
	entry.Hostname = "mdm.example.com"
	b.component(simple("mdm/customer", "1.0.7", 8080), entry)

	result, err := b.build(compose.Options{})
	require.NoError(t, err)
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w.Format(), "expose") {
			found = true
		}
	}
	assert.True(t, found, "应该警告 expose 不生效：%+v", result.Warnings)
}

// ---- local: true 回归：完全不受影响 ----

func TestLocalStillWorksAlongsideServedBy(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(simple("erp/backend", "1.0.0", 8080), config.Component{Local: true, LocalPort: 8888})

	doc := b.parsed()
	services := servicesOf(t, doc)
	assert.NotContains(t, services, "erp-backend-1-0-0", "local: true 组件依旧不生成容器")
	assert.NotContains(t, services, "mdm-customer-1-0-7")
}

// ---- 版本迁移期间的混合场景：旧调用方依赖的旧版本独立部署，
// 新调用方依赖的新版本收编进外壳，互不干扰、都不用感知对方存在 ----

func TestOldCallerAndNewCallerGetDifferentAddressesForDifferentVersions(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.7", 8080), config.Component{}) // 旧版本，独立部署
	b.component(simple("mdm/customer", "2.0.0", 8081), servedByEntry("infra/shell-go-core", "1.0.0")) // 新版本，收编
	b.component(dependsOn(simple("erp/legacy-caller", "1.0.0", 8080), "mdm/customer", "1.0.7"), config.Component{})
	b.component(dependsOn(simple("erp/new-caller", "1.0.0", 8080), "mdm/customer", "2.0.0"), config.Component{})

	doc := b.parsed()
	legacyEnv := envOf(t, serviceOf(t, doc, "erp-legacy-caller-1-0-0"))
	newEnv := envOf(t, serviceOf(t, doc, "erp-new-caller-1-0-0"))

	// 两边调用方拿到的都是各自依赖版本**自己的**版本化服务名——`inject.Build`
	// 完全不知道 servedBy 存在，从不改写地址值；地址真正指向外壳，靠的是
	// 外壳容器挂上 mdm-customer-2-0-0 这个网络别名（TestShellGetsNetworkAliasForEachMember
	// 已经验证过这一半），不是靠改写调用方的环境变量值。这正是"调用方永远
	// 不需要知道对方是不是被收编"这条设计承诺的字面体现：地址字符串本身
	// 与独立部署时一模一样，只是它现在解析到别处。
	assert.Equal(t, "http://mdm-customer-1-0-7:8080", legacyEnv["MDM_CUSTOMER_ENDPOINT"],
		"旧调用方依赖的旧版本自己独立部署，地址指向它自己的 service")
	assert.Equal(t, "http://mdm-customer-2-0-0:8081", newEnv["MDM_CUSTOMER_ENDPOINT"],
		"新调用方依赖的新版本被收编，地址值依然是它自己的版本化服务名——"+
			"重定向发生在网络层（外壳的别名），不是在这个环境变量的值上")
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/compose/... -run TestServedBy -v` and `go test ./internal/compose/... -run TestShell -v` and `go test ./internal/compose/... -run TestLocalStillWorks -v`
Expected: FAIL (compile errors — `config.Component.ServedBy` exists from Task 1, but no `servedby.go` logic wires it in yet; `mdm-customer-1-0-7` would currently render as an ordinary service).

- [ ] **Step 3: Implement — `internal/compose/servedby.go`**

```go
package compose

// 本文件实现 servedBy（外壳合并部署，servedBy 设计书）。
//
// local: true 与 servedBy 结构相似（都是"在依赖图里存在但不生成工作
// 负载"），但语义完全独立，实现也刻意不共享代码路径——local: true 是
// 本机调试，servedBy 是代码已经打进另一个外壳镜像，混在一起维护迟早
// 出现"改 local 的逻辑却影响了 servedBy"这种事故。

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
)

// servedComponent 是一个 servedBy 组件：不生成自己的容器/迁移，代码跑
// 在 Shell 那个组件的容器里。
type servedComponent struct {
	Ref      resolver.Ref
	Service  string
	Manifest *manifest.Manifest
	Entry    config.Component
	Shell    resolver.Ref
}

// applyShellGroups 把每个外壳分组合并进外壳自己的环境变量/labels，并
// 记下它要挂哪些网络别名（供 componentService 渲染 networks 段用）。
func (p *plan) applyShellGroups(groups []shell.Group) {
	byShell := make(map[resolver.Ref]shell.Group, len(groups))
	for _, g := range groups {
		byShell[g.Shell] = g
	}
	for i := range p.components {
		g, ok := byShell[p.components[i].Ref]
		if !ok {
			continue
		}
		p.components[i].Env.Env = shell.Apply(p.components[i].Env.Env, g)
		p.components[i].Env.Labels = manifest.MergeLabels(p.components[i].Env.Labels, g.Labels)

		aliases := make([]string, 0, len(g.Members))
		for _, m := range g.Members {
			aliases = append(aliases, manifest.ServiceName(m.Ref.ID, m.Ref.Version))
		}
		sort.Strings(aliases)
		p.shellAliases[p.components[i].Service] = aliases
	}
}

// servedMigrationWarnings 提醒"servedBy 组件的迁移由外壳自己负责编排"
// ——责任主体与 local: true 的对应警告（localMigrationWarnings）不同：
// 那边是调试者本人要手动执行，这边是外壳作者的编排责任。
func (p *plan) servedMigrationWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, s := range p.served {
		if s.Manifest == nil || s.Manifest.Migration == nil {
			continue
		}
		out = append(out, clierr.Warn(clierr.CodeMigrationSkipped,
			"提示：servedBy 组件的数据库迁移不会自动执行").
			WithDetail("组件", refText(s.Ref)).
			WithDetail("外壳", refText(s.Shell)).
			WithDetail("原因", "servedBy 的组件不生成容器，它的迁移容器也一并跳过").
			WithHint(
				"确保外壳 "+s.Shell.ID+" 自己的启动逻辑覆盖了这个组件的迁移，"+
					"并按各模块真实的依赖顺序执行",
				"迁移命令："+strings.Join(s.Manifest.Migration.Command, " "),
			))
	}
	return out
}

// servedHealthCheckWarnings 提醒"servedBy 组件自己的健康检查不会独立
// 生效"——它没有自己的容器，健康检查完全是外壳实现者自己的责任，平台
// 不做任何聚合、也不替外壳生成任何健康检查逻辑。
func (p *plan) servedHealthCheckWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, s := range p.served {
		if s.Manifest == nil || s.Manifest.HealthCheck.Type == manifest.HealthCheckNone {
			continue
		}
		out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
			"提示：servedBy 组件自己的健康检查不会独立生效").
			WithDetail("组件", refText(s.Ref)).
			WithDetail("外壳", refText(s.Shell)).
			WithDetail("原因", "它没有自己的容器，健康检查完全是外壳实现者自己的责任，"+
				"平台不做任何聚合、也不替外壳生成任何健康检查逻辑"))
	}
	return out
}

// servedUnsupportedFieldWarnings 提醒"这些字段对 servedBy 组件不生效"。
//
// expose / exposePort / hostname / replicas / resources /
// serviceAccountName 描述的都是"我自己这个容器该怎么部署"——而 servedBy
// 组件没有自己的容器，这些字段天然没有对象可以落地（v1 范围裁剪，见
// 设计书实施记录）。
func (p *plan) servedUnsupportedFieldWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, s := range p.served {
		var fields []string
		if s.Entry.Expose {
			fields = append(fields, "expose")
		}
		if s.Entry.ExposePort != 0 {
			fields = append(fields, "exposePort")
		}
		if s.Entry.Hostname != "" {
			fields = append(fields, "hostname")
		}
		if s.Entry.Replicas != nil {
			fields = append(fields, "replicas")
		}
		if s.Entry.Resources != nil {
			fields = append(fields, "resources")
		}
		if s.Entry.ServiceAccountName != "" {
			fields = append(fields, "serviceAccountName")
		}
		if len(fields) == 0 {
			continue
		}
		out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
			"提示：servedBy 组件上，"+strings.Join(fields, "/")+" 本次不生效").
			WithDetail("组件", refText(s.Ref)).
			WithDetail("原因", "这些字段描述的是它自己这个容器该怎么部署，而它没有自己的容器"+
				"（代码跑在外壳 "+s.Shell.ID+" 里）").
			WithHint("要单独部署这个组件，去掉它的 servedBy"))
	}
	return out
}
```

- [ ] **Step 4: Wire it into `newPlan` and `componentService`**

In `internal/compose/compose.go`, add a field to the `plan` struct (currently lines 157-187), right after `locals`:

```go
	// served 是 servedBy 的组件：不生成自己的容器/迁移，但要走它专属的
	// 几条提醒（见 servedby.go）。
	served []servedComponent
	// shellAliases 是外壳的服务名 → 它要挂的额外网络别名（收编成员的
	// 版本化服务名），供 componentService 渲染 networks 段。
	shellAliases map[string][]string
```

In `newPlan` (currently lines 189-263), initialize the new map alongside the others (in the `p := &plan{...}` literal):

```go
	p := &plan{
		cfg: cfg, graph: graph, states: states, engine: engine,
		rendered:       map[string]bool{},
		migrationAfter: map[string]string{},
		localPort:      map[string]int{},
		exposedPort:    map[string]int{},
		debugPort:      map[string]int{},
		debugExtraPort: map[string]map[int]int{},
		shellAliases:   map[string][]string{},
	}
```

Change the loop that currently reads:

```go
	for _, ref := range states.Running() {
		entry := entries[ref]
		node := graph.Node(ref)
		if node == nil {
			continue
		}
		service := manifest.ServiceName(ref.ID, ref.Version)

		if entry.Local {
			// 12.7 / 13.1：local: true 的组件在宿主机（IDE）里跑，不生成容器，
			// 但它仍然是"启动中"的组件——依赖方要能找到它
			p.locals = append(p.locals, localComponent{
				Ref: ref, Service: service, Manifest: node.Manifest,
				Entry: entry, Env: envByRef[ref],
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
	}
```

to:

```go
	for _, ref := range states.Running() {
		entry := entries[ref]
		node := graph.Node(ref)
		if node == nil {
			continue
		}
		service := manifest.ServiceName(ref.ID, ref.Version)

		if entry.Local {
			// 12.7 / 13.1：local: true 的组件在宿主机（IDE）里跑，不生成容器，
			// 但它仍然是"启动中"的组件——依赖方要能找到它
			p.locals = append(p.locals, localComponent{
				Ref: ref, Service: service, Manifest: node.Manifest,
				Entry: entry, Env: envByRef[ref],
			})
			continue
		}
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
	}
```

Right after the existing `sort.Slice(p.locals, ...)` line, add:

```go
	sort.Slice(p.served, func(i, j int) bool { return p.served[i].Service < p.served[j].Service })
```

Then, right after the existing `p.rewriteEndpointsForLocalDependencies()` call and before the warning-collection block, add:

```go
	groups, err := shell.Resolve(cfg, graph, states, env)
	if err != nil {
		return nil, err
	}
	p.applyShellGroups(groups)
```

Extend the existing warning-collection block (currently):

```go
	p.warnings = append(p.warnings, p.localMigrationWarnings()...)
	p.warnings = append(p.warnings, p.localExposeWarnings()...)
	p.warnings = append(p.warnings, p.localLabelWarnings()...)
	p.warnings = append(p.warnings, p.serviceNameResourceWarnings()...)
```

to also include:

```go
	p.warnings = append(p.warnings, p.localMigrationWarnings()...)
	p.warnings = append(p.warnings, p.localExposeWarnings()...)
	p.warnings = append(p.warnings, p.localLabelWarnings()...)
	p.warnings = append(p.warnings, p.servedMigrationWarnings()...)
	p.warnings = append(p.warnings, p.servedHealthCheckWarnings()...)
	p.warnings = append(p.warnings, p.servedUnsupportedFieldWarnings()...)
	p.warnings = append(p.warnings, p.serviceNameResourceWarnings()...)
```

Now update `componentIDs`/`containerIDs` (currently lines 376-400) so servedBy members count as "running" (need their own resources) and as "in a container" (they run inside the shell's container, so they use container-network addressing, not localhost):

```go
// containerIDs 是本次**会生成容器**的组件 ID（含 servedBy：它们的代码
// 跑在外壳容器里，用的是容器网络寻址，不是本地调试的宿主机寻址；
// 不含 local: true）。
func (p *plan) containerIDs() []string {
	out := make([]string, 0, len(p.components)+len(p.served))
	for _, c := range p.components {
		out = append(out, c.Ref.ID)
	}
	for _, s := range p.served {
		out = append(out, s.Ref.ID)
	}
	return out
}

// componentIDs 是本次会跑起来的组件 ID。
//
// local 与 servedBy 组件都算：它们都不生成自己的容器，但都照样要连
// 自己的库。
func (p *plan) componentIDs() []string {
	out := make([]string, 0, len(p.components)+len(p.locals)+len(p.served))
	for _, c := range p.components {
		out = append(out, c.Ref.ID)
	}
	for _, l := range p.locals {
		out = append(out, l.Ref.ID)
	}
	for _, s := range p.served {
		out = append(out, s.Ref.ID)
	}
	return out
}
```

Finally, update `componentService` (currently lines 406-442) to render `networks` conditionally. Change:

```go
	svc := map[string]any{
		"image":    c.Manifest.Deployment.Image,
		"networks": []any{networkAlias},
		"restart":  "unless-stopped", // 12.10
	}
```

to:

```go
	svc := map[string]any{
		"image":   c.Manifest.Deployment.Image,
		"restart": "unless-stopped", // 12.10
	}
	if aliases := p.shellAliases[c.Service]; len(aliases) > 0 {
		aliasList := make([]any, len(aliases))
		for i, a := range aliases {
			aliasList[i] = a
		}
		svc["networks"] = map[string]any{networkAlias: map[string]any{"aliases": aliasList}}
	} else {
		svc["networks"] = []any{networkAlias}
	}
```

Add the import `"github.com/brickkit/brickkit/internal/shell"` to `internal/compose/compose.go`'s import block (already has `resolver`, `manifest`, etc. — just add `shell` alphabetically among them).

- [ ] **Step 5: Run to verify tests pass**

Run: `go test ./internal/compose/... -v`
Expected: PASS on every test in `servedby_test.go`, and no regression in any pre-existing test (`local_test.go`, `compose_test.go`, `multiversion_test.go`, `upgrade_isolation_test.go`, `labels_test.go`).

- [ ] **Step 6: Commit**

```bash
git add internal/compose
git commit -m "$(cat <<'EOF'
Docker 渲染支持 servedBy（外壳合并部署第三步）

servedBy 组件不生成自己的 service/迁移；外壳挂上每个成员的版本化
服务名作为网络别名，合并端点变量+labels+BRICKKIT_SERVED_MEMBERS
写进外壳自己的环境。迁移/健康检查/不支持字段各有专属警告，措辞
区分外壳作者与本机调试者两种不同的责任主体。local: true 的既有
路径完全不受影响。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: K8s rendering (`internal/k8s`)

**Files:**
- Create: `internal/k8s/servedby.go`
- Modify: `internal/k8s/k8s.go:295-368` (`plan` struct, `newPlan`), `:145-225` (`Generate`), `:423-430` (`componentIDs`)
- Modify: `internal/k8s/deployment.go:370-402` (`privilegedPortWarnings`)
- Test: `internal/k8s/servedby_test.go`

**Interfaces:**
- Consumes: `internal/shell` (Task 2).
- Produces: nothing new consumed outside this package; `k8s.Generate`'s public signature is unchanged.

- [ ] **Step 1: Write the failing tests**

This package already has the fixtures this task needs, all in `internal/k8s/k8s_test.go` and `internal/k8s/manifests_test.go` (same `k8s_test` package, so they're directly callable from a new file without redefinition): `newBuilder(t)`, `b.component(m, entry)`, `b.build() (*k8s.Result, error)`, `b.generate() *k8s.Result`, `b.doc(path string) map[string]any` (reads one generated file by its **exact** `Files[].Path` and parses it), `b.container(service string) map[string]any` (digs into `deployments/<service>.yaml`'s single container), `dig(t, doc, path...) any`, `envOf(t, container) map[string]any`, `pathsOf(r *k8s.Result) []string`, `hasFile(r *k8s.Result, path string) bool`, `simple(id, version, port)`, `dependsOn(m, id, version)`, and `migrating(m)` (sets `m.Migration`, defined in `manifests_test.go:204-207`). Do not redefine any of these.

Create `internal/k8s/servedby_test.go`:

```go
// 本文件测试 servedBy（外壳合并部署）在 K8s 目标下的渲染，覆盖 servedBy
// 设计书 §6-§9。local: true 在 K8s 下依旧照常拒绝，回归覆盖见
// TestLocalStillRejectedAlongsideServedBy。
package k8s_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/shell"
)

func servedByEntry(shellID, shellVersion string) config.Component {
	return config.Component{ServedBy: shellID + "@" + shellVersion}
}

// ---- 不生成 Deployment/Job，只生成一个指向外壳 Pod 的 Service ----

func TestServedByComponentGeneratesOnlyAService(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(migrating(simple("mdm/customer", "1.0.7", 8080)),
		servedByEntry("infra/shell-go-core", "1.0.0"))

	result := b.generate()
	assert.False(t, hasFile(result, "deployments/mdm-customer-1-0-7.yaml"),
		"servedBy 组件不该有自己的 Deployment")
	assert.True(t, hasFile(result, "services/mdm-customer-1-0-7.yaml"),
		"但要有一个 Service 让它自己的服务名能被解析")
	assert.False(t, hasFile(result, "migrations/mdm-customer-1-0-7-migration.yaml"),
		"也不该有自己的迁移 Job")
	assert.True(t, hasFile(result, "deployments/infra-shell-go-core-1-0-0.yaml"),
		"外壳自己照常生成 Deployment")
}

func TestServedByServiceSelectsShellPod(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))

	doc := b.doc("services/mdm-customer-1-0-7.yaml")
	assert.Equal(t, "infra-shell-go-core-1-0-0", dig(t, doc, "spec", "selector", "app"),
		"selector 要指向外壳的 Pod，不是它自己（它没有自己的 Pod）")

	ports, ok := dig(t, doc, "spec", "ports").([]any)
	require.True(t, ok)
	require.Len(t, ports, 1)
	assert.Equal(t, 8080, ports[0].(map[string]any)["targetPort"], "端口是它自己声明的端口")
}

// ---- 合并环境变量 + BRICKKIT_SERVED_MEMBERS ----

func TestShellDeploymentGetsMergedEndpointsAndServedMembers(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(dependsOn(simple("mdm/customer", "1.0.7", 8080), "infra/database", "1.0.0"),
		servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(simple("infra/database", "1.0.0", 5432), config.Component{})

	env := envOf(t, b.container("infra-shell-go-core-1-0-0"))
	assert.Equal(t, "http://infra-database-1-0-0:5432", env["INFRA_DATABASE_ENDPOINT"])
	assert.Equal(t, "mdm-customer-1-0-7", env[shell.EnvVarServedMembers])
}

// ---- 孤儿清理：member Service 出现在 Desired 里 ----

func TestServedByServiceIsInDesired(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))

	result := b.generate()
	assert.Contains(t, result.Desired, "service/mdm-customer-1-0-7",
		"P38 孤儿清理靠 Desired 判断该留还是该删——servedBy 撤销之后这条要能被识别成孤儿")
}

// ---- 迁移警告 ----

func TestServedByMigrationWarnsInK8s(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(migrating(simple("mdm/customer", "1.0.7", 8080)), servedByEntry("infra/shell-go-core", "1.0.0"))

	result, err := b.build()
	require.NoError(t, err)
	require.Len(t, result.Warnings, 1)
	assert.Equal(t, clierr.CodeMigrationSkipped, result.Warnings[0].Code)
}

// ---- local: true 回归：K8s 下依旧照常拒绝 ----

func TestLocalStillRejectedAlongsideServedBy(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(simple("erp/backend", "1.0.0", 8080), config.Component{Local: true})

	_, err := b.build()
	require.Error(t, err, "local: true 在 K8s 下必须依旧被拒绝，不受 servedBy 存在与否影响")
	assert.Contains(t, err.Error(), "local: true 只能在 deploy.target: docker 下使用")
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/k8s/... -run TestServedBy -v` and `go test ./internal/k8s/... -run TestShellDeployment -v` and `go test ./internal/k8s/... -run TestLocalStillRejected -v`
Expected: FAIL (compile errors or wrong output — `mdm/customer` currently renders as an ordinary Deployment+Service+Job).

- [ ] **Step 3: Implement — `internal/k8s/servedby.go`**

```go
package k8s

// 本文件实现 servedBy（外壳合并部署）在 K8s 下的渲染。
//
// local: true 在 K8s 下完全不支持（localNotSupported，本文件不改动这条
// 校验），servedBy 是完全独立的新代码路径。

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
)

// servedPlan 是一个 servedBy 组件：只生成一个指向外壳 Pod 的 Service，
// 不生成 Deployment/Job——它没有自己的工作负载。
type servedPlan struct {
	Ref      resolver.Ref
	Service  string
	Manifest *manifest.Manifest
	Entry    config.Component
	Shell    resolver.Ref
}

// applyShellGroups 把每个外壳分组合并进外壳自己的环境变量/labels。
func (p *plan) applyShellGroups(groups []shell.Group) {
	byShell := make(map[resolver.Ref]shell.Group, len(groups))
	for _, g := range groups {
		byShell[g.Shell] = g
	}
	for i := range p.components {
		g, ok := byShell[p.components[i].Ref]
		if !ok {
			continue
		}
		p.components[i].Env.Env = shell.Apply(p.components[i].Env.Env, g)
		p.components[i].Env.Labels = manifest.MergeLabels(p.components[i].Env.Labels, g.Labels)
	}
}

// servedServiceDoc 渲染一个 servedBy 组件的 Service：selector 指向外壳
// 的 Pod（labelApp: 外壳的服务名），而不是它自己——它没有自己的
// Deployment，这个 Service 存在的唯一目的是让它自己的版本化服务名解析
// 到外壳的 Pod（servedBy 设计书 §9）。
func (p *plan) servedServiceDoc(m servedPlan) map[string]any {
	shellService := manifest.ServiceName(m.Shell.ID, m.Shell.Version)
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":      m.Service,
			"namespace": p.namespace,
			"labels": map[string]any{
				labelComponent:        containerName(m.Ref.ID),
				labelComponentVersion: m.Ref.Version,
				labelProject:          p.cfg.Project,
			},
			"annotations": map[string]any{annotationComponentID: m.Ref.ID},
		},
		"spec": map[string]any{
			"selector": map[string]any{labelApp: shellService},
			"ports":    servicePorts(m.Manifest),
			"type":     "ClusterIP",
		},
	}
}

// servedMigrationWarnings 与 compose 侧同名函数职责相同——责任主体是
// 外壳作者，不是本机调试者。
func (p *plan) servedMigrationWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, s := range p.served {
		if s.Manifest == nil || s.Manifest.Migration == nil {
			continue
		}
		out = append(out, clierr.Warn(clierr.CodeMigrationSkipped,
			"提示：servedBy 组件的数据库迁移不会自动执行").
			WithDetail("组件", s.Ref.String()).
			WithDetail("外壳", s.Shell.String()).
			WithDetail("原因", "servedBy 的组件不生成 Deployment/Job，它的迁移 Job 也一并跳过").
			WithHint(
				"确保外壳 "+s.Shell.ID+" 自己的启动逻辑覆盖了这个组件的迁移，"+
					"并按各模块真实的依赖顺序执行",
				"迁移命令："+strings.Join(s.Manifest.Migration.Command, " "),
			))
	}
	return out
}

func (p *plan) servedHealthCheckWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, s := range p.served {
		if s.Manifest == nil || s.Manifest.HealthCheck.Type == manifest.HealthCheckNone {
			continue
		}
		out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
			"提示：servedBy 组件自己的健康检查不会独立生效").
			WithDetail("组件", s.Ref.String()).
			WithDetail("外壳", s.Shell.String()).
			WithDetail("原因", "它没有自己的 Pod，健康检查完全是外壳实现者自己的责任，"+
				"平台不做任何聚合、也不替外壳生成任何探针"))
	}
	return out
}

func (p *plan) servedUnsupportedFieldWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, s := range p.served {
		var fields []string
		if s.Entry.Expose {
			fields = append(fields, "expose")
		}
		if s.Entry.Hostname != "" {
			fields = append(fields, "hostname")
		}
		if s.Entry.Replicas != nil {
			fields = append(fields, "replicas")
		}
		if s.Entry.Resources != nil {
			fields = append(fields, "resources")
		}
		if s.Entry.ServiceAccountName != "" {
			fields = append(fields, "serviceAccountName")
		}
		if len(fields) == 0 {
			continue
		}
		out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
			"提示：servedBy 组件上，"+strings.Join(fields, "/")+" 本次不生效").
			WithDetail("组件", s.Ref.String()).
			WithDetail("原因", "这些字段描述的是它自己这个 Pod 该怎么部署，而它没有自己的 Pod"+
				"（代码跑在外壳 "+s.Shell.ID+" 里）").
			WithHint("要单独部署这个组件，去掉它的 servedBy"))
	}
	return out
}

// servedComponentIDs 是 servedBy 组件的组件 ID，供 componentIDs 复用。
func servedComponentIDs(served []servedPlan) []string {
	out := make([]string, 0, len(served))
	for _, s := range served {
		out = append(out, s.Ref.ID)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Wire it into `plan`, `newPlan`, `Generate`, `componentIDs`, `privilegedPortWarnings`**

In `internal/k8s/k8s.go`, add a field to the `plan` struct (currently lines 296-308):

```go
	// components 按服务名排序。
	components []componentPlan
	// served 是 servedBy 的组件：只生成一个指向外壳 Pod 的 Service。
	served []servedPlan
	// secrets 按 Secret 名排序。
	secrets []secretPlan
```

Change the loop inside `newPlan` (currently lines 330-350):

```go
	var locals []resolver.Ref
	for _, ref := range states.Running() {
		node := graph.Node(ref)
		if node == nil {
			continue
		}
		if entries[ref].Local {
			locals = append(locals, ref)
			continue
		}
		p.components = append(p.components, componentPlan{
			Ref:      ref,
			Service:  manifest.ServiceName(ref.ID, ref.Version),
			Manifest: node.Manifest,
			Entry:    entries[ref],
			Env:      envByRef[ref],
		})
	}
	if len(locals) > 0 {
		return nil, localNotSupported(locals)
	}

	sort.Slice(p.components, func(i, j int) bool { return p.components[i].Service < p.components[j].Service })
```

to:

```go
	var locals []resolver.Ref
	for _, ref := range states.Running() {
		node := graph.Node(ref)
		if node == nil {
			continue
		}
		entry := entries[ref]
		if entry.Local {
			locals = append(locals, ref)
			continue
		}
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
	}
	if len(locals) > 0 {
		return nil, localNotSupported(locals)
	}

	sort.Slice(p.components, func(i, j int) bool { return p.components[i].Service < p.components[j].Service })
	sort.Slice(p.served, func(i, j int) bool { return p.served[i].Service < p.served[j].Service })

	groups, err := shell.Resolve(cfg, graph, states, env)
	if err != nil {
		return nil, err
	}
	p.applyShellGroups(groups)
```

Extend the warnings block right after (currently):

```go
	p.warnings = append(p.warnings, p.privilegedPortWarnings()...)
	// K8s 下没有 local: true（上面已经拦下），所以全部组件都是容器组件
	p.warnings = append(p.warnings, deploy.LocalhostResourceWarnings(
		cfg, p.componentIDs(), config.TargetK8s)...)
	return p, nil
}
```

to:

```go
	p.warnings = append(p.warnings, p.privilegedPortWarnings()...)
	p.warnings = append(p.warnings, p.servedMigrationWarnings()...)
	p.warnings = append(p.warnings, p.servedHealthCheckWarnings()...)
	p.warnings = append(p.warnings, p.servedUnsupportedFieldWarnings()...)
	// K8s 下没有 local: true（上面已经拦下），所以全部组件都是容器组件
	p.warnings = append(p.warnings, deploy.LocalhostResourceWarnings(
		cfg, p.componentIDs(), config.TargetK8s)...)
	return p, nil
}
```

Add the import `"github.com/brickkit/brickkit/internal/shell"` to `internal/k8s/k8s.go`'s import block.

In `Generate` (currently lines 146-225), add a new loop right after the existing `for _, c := range p.components { ... }` block and before `result.MigrationGroups = p.migrationGroups()`:

```go
	for _, m := range p.served {
		if err := p.emit(result, cfg, now,
			dirServices+"/"+m.Service+".yaml", p.servedServiceDoc(m)); err != nil {
			return nil, err
		}
	}
	result.MigrationGroups = p.migrationGroups()
```

Update `componentIDs` (currently lines 423-430):

```go
// componentIDs 是本次会跑起来的组件 ID（含 servedBy：它没有自己的
// Pod，但照样要连自己的资源）。
func (p *plan) componentIDs() []string {
	out := make([]string, 0, len(p.components)+len(p.served))
	for _, c := range p.components {
		out = append(out, c.Ref.ID)
	}
	out = append(out, servedComponentIDs(p.served)...)
	return out
}
```

In `internal/k8s/deployment.go`, extend `privilegedPortWarnings` (currently lines 370-402) to also cover `p.served` (its code runs inside the shell's Pod, whose `securityContext` — if `podSecurity: restricted` — applies to it exactly the same way):

```go
func (p *plan) privilegedPortWarnings() []*clierr.Error {
	if p.cfg.Deploy.PodSecurity != config.PodSecurityRestricted {
		return nil
	}

	var out []*clierr.Error
	check := func(m *manifest.Manifest, refText string) {
		if m == nil {
			return
		}
		ports := []int{m.Deployment.Port}
		for _, extra := range m.Deployment.ExtraPorts {
			ports = append(ports, extra.Port)
		}
		for _, port := range ports {
			if port >= 1024 {
				continue
			}
			out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
				fmt.Sprintf("组件监听特权端口 %d，但 podSecurity: restricted 下绑不了", port)).
				WithDetail("组件", refText).
				WithDetailf("端口", "%d", port).
				WithDetail("原因", "restricted 会 drop 掉全部 capabilities，包括 NET_BIND_SERVICE").
				WithHint(
					"让组件监听 1024 以上的端口（对外端口由 Service / Ingress 映射，不必是 80）",
					"或去掉 deploy.podSecurity: restricted",
				))
		}
	}

	for _, c := range p.components {
		check(c.Manifest, c.Ref.ID+"@"+c.Ref.Version)
	}
	for _, s := range p.served {
		check(s.Manifest, s.Ref.ID+"@"+s.Ref.Version+"（servedBy "+s.Shell.String()+"）")
	}
	return out
}
```

- [ ] **Step 5: Run to verify tests pass**

Run: `go test ./internal/k8s/... -v`
Expected: PASS on every test in `servedby_test.go`, and no regression in any pre-existing test.

- [ ] **Step 6: Commit**

```bash
git add internal/k8s
git commit -m "$(cat <<'EOF'
K8s 渲染支持 servedBy（外壳合并部署第四步）

servedBy 组件不生成 Deployment/Job，只生成一个 selector 指向外壳 Pod
的 Service——版本化服务名照常能被解析到，孤儿清理（P38）不需要任何
改动，Desired 记账天然覆盖这个新 Service 类型。合并端点变量+labels+
BRICKKIT_SERVED_MEMBERS 写进外壳的 Deployment。local: true 在 K8s 下
依旧照常拒绝，不受 servedBy 是否存在影响。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Shell implementer's guide (bilingual patterns doc)

**Files:**
- Create: `docs/en/patterns/shell-implementers-guide.md`
- Create: `docs/zh/patterns/shell-implementers-guide.md`
- Modify: `llms.txt`

**Interfaces:**
- None (pure documentation) — consumed only by human/AI readers, and by `scripts/check-docs-bilingual.py`'s mirror check (already generic, needs no changes: it globs `docs/en/**/*.md` vs `docs/zh/**/*.md`, so adding one file to each side keeps it symmetric automatically).

- [ ] **Step 1: Write `docs/en/patterns/shell-implementers-guide.md`**

Read `docs/en/architecture/overview.md` first for the established tone, heading style, and its convention of grounding claims in real code — mirror that, not a generic-tutorial voice.

Write `docs/en/patterns/shell-implementers-guide.md` with this exact content. `docs/en/patterns/` and `docs/en/architecture/` are sibling directories, so the `overview.md` link below resolves correctly as written; the `AGENTS.md` reference is prose, not a markdown link (AGENTS.md sits three directories up, at the repo root, and per the existing convention in this repo's docs, cross-references to it are stated by section number in prose, not linked):

```markdown
# Building a Qualified Shell

> Prerequisite: read [Architecture overview](overview.md) first if you
> haven't — this guide assumes you already understand versioned service
> names and environment-variable injection.

`servedBy` (see AGENTS.md §5.7) lets a component declare that its workload
runs inside another component's container instead of its own. This guide is
for whoever builds that other component — the "shell." It has two jobs:
say precisely what the platform hands your shell and what it doesn't, and
lay out the nine correctness properties every real merged process ends up
needing, regardless of language or framework.

## What `servedBy` does and doesn't ask of the platform

A `servedBy` component's code runs inside your shell's single container or
Pod. The platform generates the address routing for it — a Docker network
alias or a Kubernetes Service, depending on target — and merges the right
`*_ENDPOINT` variables into your shell's environment, so anything that
depends on a component you've absorbed still resolves it correctly. That is
the entire platform contract. It never needs to know, and this guide never
asks you to tell it, what language your shell is written in, how many
processes or threads it runs, which concurrency primitives it uses, or what
base image it starts from. Two real shells built for the same platform can
look nothing alike internally — one might orchestrate goroutines with
`errgroup`, another might run an `asyncio` event loop — and both are equally
valid, because the platform's contract stops at "which address points
where."

## What the platform puts in your shell's environment

- **Every `*_ENDPOINT` variable an absorbed component would have received
  had it been deployed on its own.** These are merged directly into your
  shell's real OS environment — not relayed through some intermediate
  config object your own code has to construct. If two absorbed components
  need different values under the same variable name (the same component ID
  at two different exact versions, say), that is a genuine conflict the
  platform catches and refuses to generate, rather than silently picking
  one.
- **`BRICKKIT_SERVED_MEMBERS`** — a comma-separated list of the versioned
  service names of the components currently absorbed into this deployment.
  A shell image is typically compiled with more modules than any single
  deployment actually needs active; this variable tells you, at that
  specific deployment's startup, which of your compiled-in modules should
  actually run right now. Two values matter, and they mean opposite things:
  an **empty string** means zero members are currently active — a
  compliant shell must initialize none of them. The variable being
  **entirely absent** means this container isn't running under a
  brickKit-generated deployment at all (you started the binary by hand for
  local debugging, say) — what you do in that case is entirely up to you.
  Don't collapse these two cases into one fallback path. The recommended
  pattern: at startup, compute each compiled-in module's own versioned
  service name using the same rule the platform uses (component ID with
  `/` and `.` replaced by `-`, all lowercase, plus the exact version with
  `.` replaced by `-`), and skip initializing — including skipping its
  migrations and skipping opening its resource connections — any module not
  on the list.

## What the platform deliberately does not put there

An absorbed component's own `configSchema`-derived configuration values and
its resource-connection variables (`DATABASE_*` and friends) are never
merged into your shell's environment, and this is not an oversight to work
around — it's a hard boundary. `*_ENDPOINT` variable names are derived from
a component ID, so they're guaranteed unique across your whole system; a
config key like `pgSchema` is not — two independently-authored modules can
easily reuse the same generic name for two entirely different values, and
merging those into one shared process environment would silently let one
overwrite the other. Giving each of your absorbed modules its own isolated
view of its configuration and its own resource connections is squarely your
job, not the platform's — see property 5 below. The platform also never
puts `COMPONENT_ID` or `COMPONENT_VERSION` for anything but the shell
itself into that environment, for the same reason: those variable names
are fixed and would collide the instant you absorb more than one component.

## Nine properties a merged process must satisfy

Every one of these was found the hard way, by teams who actually shipped a
shell. None of them are specific to any one language, database, or
framework — only the concrete illustrations are, and they're marked as
such.

1. **A health check must reflect only the merged unit itself, never a
   downstream dependency.** This is the same rule the platform's own
   `/healthz` guidance already states for a single component — but merging
   raises the stakes: one absorbed module's downstream hiccup can now take
   down the health signal for every module sharing that process, and a
   restart takes all of them with it, not just the one that was actually
   unwell.
2. **Verify your health check against every HTTP method your framework
   routes differently — don't assume the documented behavior survives
   contact with reality.** One real example: a popular Python ASGI
   framework's routing layer does not automatically answer `HEAD` requests
   for a route only declared with `GET`, despite what its underlying
   toolkit's documentation implies — a health checker that sends `HEAD`
   (as `wget --spider` does) gets a 404 against a route that answers `GET`
   perfectly normally. The lesson generalizes past this one framework:
   whatever health-check mechanism your deployment target actually uses,
   test it against your real merged binary once, don't reason about it
   from documentation.
3. **One module's unexpected error must not take down its siblings.** A
   standalone deployment gives each component a blast radius of one; a
   naively merged one can silently turn that into "the whole shell." The
   concrete trap: orchestration primitives that share a single cancellation
   signal across independent tasks (Go's `errgroup.WithContext` is one
   example — the first goroutine to return a non-nil error cancels the
   shared context for every other goroutine in the group) are built for
   "these things should live and die together," which is exactly the wrong
   shape for modules that are supposed to stay independently fault-isolated.
   Whatever your language's equivalent primitive is, check what it actually
   does on a single failure before you build your module orchestration on it.
4. **Any stateful resource shared across modules — a connection pool, a
   client — must have its per-borrow identity or scope cleared before the
   next borrow, or the next borrower silently inherits it.** This is the
   most dangerous property on this list, because violating it produces no
   crash and no error message — only quietly wrong data, read or written
   under the wrong identity. How you satisfy it is entirely stack-specific
   (a transaction-scoped `SET LOCAL ROLE`/`SET LOCAL search_path` in
   PostgreSQL, reset automatically on commit or rollback, is one real fix;
   a different database or a different pooling library will need a
   different mechanism), but the underlying rule — never set identity or
   scope on a shared resource outside of a request- or transaction-scoped
   boundary — is universal to sharing any stateful resource pool at all.
5. **Process-global state initializes exactly once for the whole process,
   not once per absorbed module.** Signal handlers, tracer/logger/metrics
   registry setup, and any framework-level global configuration all fall
   under this — and so, per the two sections above, does giving each module
   its own configuration and resource connections, since the platform
   deliberately doesn't do that merging for you. Whichever module happens
   to initialize last tends to silently win, usually with no warning that
   it happened.
6. **Whatever mechanism your independently-deployed code uses to discover a
   dependency's address must still work once merged.** If your code (or a
   shared SDK you maintain) reads dependency addresses straight out of the
   process's real environment variables — exactly the mechanism this guide
   describes above — this property holds automatically, because the
   platform already writes those variables into your shell container's
   real OS environment, the same way it would for any independently
   deployed component. The trap only appears if some layer in your own
   stack relays or re-derives that address through an intermediate step
   (a per-module config object your shell launcher builds and hands to
   each module, say) and that relay step is the one place nobody actually
   wires the real environment through — a bug that stays invisible until
   the first time a code path that depends on it actually runs.
7. **Migration and schema-creation ordering becomes entirely your
   responsibility, derived from your modules' real dependency order.** The
   platform runs no migration container at all for a `servedBy` component
   (it warns you at generation time, precisely so this doesn't surprise you
   at runtime) — your shell's own startup code has to run each absorbed
   module's migrations in an order that actually respects any cross-module
   foreign keys or references, not an arbitrary or assumed-idempotent one.
8. **Any process-global, register-once-by-name mechanism needs a
   duplicate-registration check once modules are merged into one process.**
   This is not specific to any one serialization format. The canonical
   trap in Go: two modules that each vendor a copy of the same third
   component's generated gRPC/protobuf client code (to avoid directly
   importing each other's repositories) will, once compiled into the same
   binary, each try to register the same `.proto` file path in
   `google.golang.org/protobuf`'s process-global type registry — and the
   second registration panics, with an error message that gives no hint
   the actual cause is a merged deployment. The fix that actually works is
   a deliberate, narrow exception to "components never import each other":
   a package that contains *only* generated message types and client stubs
   — no hand-written business logic — can be published as an independent,
   shared module that every component importing that contract depends on
   directly, instead of each vendoring its own copy. Sharing pure
   generated-contract code this way doesn't compromise component autonomy
   the way sharing real business logic would, and it guarantees there is
   only ever one compiled instance of that contract's types in a merged
   process, no matter how the modules end up grouped. If your own
   toolchain has an equivalent global registry-by-name pattern (not every
   language does), the same exception — and the same underlying fix — likely
   applies.
9. **There is no way to externally verify that a shell image's declared
   version matches what's actually compiled inside it.** An image is an
   opaque reference to the platform; keeping that promise is entirely on
   your own team's discipline (a version ledger you maintain by hand is a
   reasonable, if unglamorous, answer). This is an honest, permanent
   limitation of merging code into one artifact — not a gap a future
   platform version is expected to close.

## Multiple versions, mixed deployment shapes

A real migration is never a clean cutover — some callers upgrade before
others, and a shell's own contents change over time. `servedBy` doesn't add
a new axis of complexity here; every scenario below already falls out of
BrickKit's existing exact-version-pinning and versioned-service-naming
rules (AGENTS.md §5.1/§9.2/§9.3), because `servedBy` is just one more field
on one more ordinary `components:` entry.

**Old callers on the old version, new callers on the new, one version
merged and the other standalone — no coordination required between them.**

```yaml
components:
  - id: infra/shell-go-core
    version: 1.0.0
    image: brickenterprise/be-shell-go:1.0.0
    healthCheck: { path: /healthz }
    deployment: { port: 9000 }

  - id: mdm/customer
    version: 1.0.7                       # not yet migrated; still its own container
    deployment: { port: 8080 }

  - id: mdm/customer
    version: 2.0.0                       # migrated; lives inside the shell
    servedBy: infra/shell-go-core@1.0.0
    deployment: { port: 8081 }
```

A component still declaring `mdm/customer@1.0.7` as a dependency gets
`MDM_CUSTOMER_ENDPOINT=http://mdm-customer-1-0-7:8080` — that version's own
independent service, exactly as before this feature existed. A component
declaring `mdm/customer@2.0.0` gets
`MDM_CUSTOMER_ENDPOINT=http://mdm-customer-2-0-0:8081` — note that this is
still `mdm/customer`'s *own* versioned service name and its *own* declared
port, identical in shape to what an independently-deployed version would
produce. What's different is invisible at this layer: the platform makes
that name resolve to the shell instead of a container of its own (a Docker
network alias, or a dedicated Kubernetes Service selecting the shell's
Pods — see below). Neither caller's own dependency declaration, nor the
address it computes, ever needs to change based on where its dependency
happens to physically live — that indirection is the entire point of the
platform's address injection.

**The same shell can absorb two versions of the same logical component at
once** — this is exactly the state a migration passes through while some
callers are on the old version and some are on the new, and both versions
are already merged for footprint reasons:

```yaml
  - id: mdm/customer
    version: 1.0.7
    servedBy: infra/shell-go-core@1.0.0
    deployment: { port: 8080 }
  - id: mdm/customer
    version: 2.0.0
    servedBy: infra/shell-go-core@1.0.0
    deployment: { port: 8081 }           # must differ from 1.0.7's port
```

Give each version its own port and the platform treats them as two entirely
independent members of the same group — merged endpoints, network
routing, and `BRICKKIT_SERVED_MEMBERS` all handle this with no special
casing, the same way they'd handle two unrelated components sharing a
shell. The only thing you must get right yourself: your shell's own module
registry needs to be keyed by the full versioned service name
(`mdm-customer-1-0-7` vs. `mdm-customer-2-0-0`), not by the bare component
ID — if it's keyed by ID alone, the second version silently overwrites the
first in your registry, with no error from the platform (which never
inspects your registry) and no error from the merge (which validated the
*addresses*, not your shell's internal bookkeeping).

**What you cannot do: give one exact version two deployment shapes at
once.**

```yaml
  - id: mdm/customer
    version: 1.0.7
    servedBy: infra/shell-go-core@1.0.0
  - id: mdm/customer
    version: 1.0.7                       # rejected: duplicate id+version
```

This isn't a `servedBy`-specific restriction — a project config can never
declare the same exact `(id, version)` twice, with or without `servedBy`
(AGENTS.md §9.2's "one component ID appears once" rule, applied literally:
`servedBy` is just one more field on that one entry, not a second axis a
duplicate could vary on). If you genuinely need "most callers share one
merged instance, one caller needs an independently-scaled instance of what
is conceptually the same code," the answer is to publish that code under a
second version number — even a trivial one — and give the two versions
their two different deployment shapes. A single version number cannot mean
two different things at once; that would be exactly the kind of implicit,
unguessable state this platform's "explicit over implicit" principle exists
to rule out.

## What this guide deliberately never asks of you

Nothing here requires disclosing your shell's language, process count,
concurrency model, or base image to the platform, and nothing in the
platform's own generation logic branches on any of those either. That's not
an oversight — it's the entire point of `servedBy`: the platform's job stops
at correctly answering "which address points where," and everything past
that line is yours to build however fits your stack.
```

- [ ] **Step 2: Write `docs/zh/patterns/shell-implementers-guide.md`**

Independently written, native Chinese — not a translation pass, matching the symmetric-bilingual convention already established for `docs/zh/architecture/overview.md` and `docs/zh/patterns/testing.md`. Cover the exact same section structure and make every one of the same substantive claims as the English version above (the "什么给、什么不给" boundary, all nine properties including the explicit protobuf/generated-code-sharing exception in point 8, the "multiple versions, mixed deployment shapes" section with its three worked YAML examples — old/new version split, one shell absorbing two versions at once, and the one-version-can't-have-two-shapes hard rule — and the closing non-scope statement) — read `docs/zh/architecture/overview.md` first for tone before writing.

- [ ] **Step 3: Update `llms.txt`**

Add one line to the `## English documentation` section (after the existing testing.md entry) and one to `## 中文文档` (same position), following the exact format of the existing entries:

```
- [Building a qualified shell](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/patterns/shell-implementers-guide.md): What a component's `servedBy` field asks of the shell that hosts it — the nine correctness properties a merged process must satisfy, and exactly what the platform does and doesn't inject into a shell's environment.
```

```
- [合格外壳该满足什么](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/zh/patterns/shell-implementers-guide.md): `servedBy` 字段对收编它的外壳提出了什么要求——合并进程必须满足的九条性质，以及平台到底会不会把哪些东西注入外壳的环境。
```

- [ ] **Step 4: Verify the bilingual mirror check passes**

Run: `python3 scripts/check-docs-bilingual.py`
Expected: `✅ docs/en ↔ docs/zh 镜像完整，README/AGENTS 双语齐全，llms.txt 全部链接可解析`

- [ ] **Step 5: Commit**

```bash
git add docs/en/patterns/shell-implementers-guide.md docs/zh/patterns/shell-implementers-guide.md llms.txt
git commit -m "$(cat <<'EOF'
新增外壳实现者指南（英文/中文对等），llms.txt 收录

把 be-assembly-standard 反馈里九条实现者指南改写成语言/框架无关的
通用版本，加上 BRICKKIT_SERVED_MEMBERS 的完整语义说明与平台合并
边界（只收 *_ENDPOINT，不收组件自己的配置/身份变量）。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: `AGENTS.md` / `AGENTS.zh.md` updates

**Files:**
- Modify: `AGENTS.md` (§4.1 rejection-list entry, §5.2 reserved-variable table, §7 schema skeleton, §9.21, §10 table, §11.1 repo map, §11.2 dig-deeper table)
- Modify: `AGENTS.zh.md` (same sections, native Chinese)

**Interfaces:** None (documentation only) — but `tests/docfields/docfields_test.go`'s `TestDocSkeletonsUseOnlyKnownFields` DOES parse any YAML block in these two files that looks like a `brickkit.yaml`/`component.yaml` skeleton and rejects unknown fields, so the new `servedBy:` line added to §7's skeleton must be spelled exactly `servedBy` (matching the real yaml tag from Task 1) or that test will fail.

- [ ] **Step 1: §4.1 rejection-list row**

In `AGENTS.md`, find the row:

```
| Consolidated deployment / monolithic shell (multiple components running as one instance) | The platform provides no command for this, but **the path is open**: address injection means a caller has no way to know the shape of the other end, so a user can build their own shell. See *Merged Component Deployment* and §2.21 of the architecture rationale |
```

Replace it with:

```
| Consolidated deployment / monolithic shell — the platform does not, and will not, ship its own shell scaffolding or process supervisor | But a small, structural piece **is** built in: `servedBy` lets a component declare "my workload is provided by another component," and the platform correctly wires `*_ENDPOINT` addresses to it on both Docker and K8s — without ever needing to understand what's inside the shell. See §5.9 below and *Merged Component Deployment* / §2.21 of the architecture rationale for the full boundary |
```

Do the equivalent edit in `AGENTS.zh.md`'s matching row (same table, native Chinese wording — read the surrounding rows for tone before writing).

- [ ] **Step 2: New §5.9 "Consolidated deployment (`servedBy`)"**

Insert a new subsection in `AGENTS.md` §5 (right after the existing §5.6 "Local debugging (`local: true`)", renumbering nothing else — subsequent §5.x entries keep their numbers since this becomes §5.7, pushing "§5.7 Component source workspace" to §5.8 and "§5.8 Marketplace..." to §5.9; check the actual current numbering in the file before inserting, since the read-in copy shown to you may already have shifted by the time you do this task — search for the literal heading text, not the number):

```markdown
### 5.7 Consolidated deployment (`servedBy`)

A component can declare `servedBy: <shell-component-id>@<version>` to mean
"my workload is provided by that other component" — the shell it names is an
ordinary component, with its own image, port, and health check. The platform:

- Skips generating a workload (container/Deployment) and a migration
  container/Job for the `servedBy` component.
- Still computes a correct `*_ENDPOINT` for anything depending on it — the
  address points at the shell's actual location, using the `servedBy`
  component's own declared port.
- Merges only `*_ENDPOINT`-class variables and `labels` into the shell's
  environment — never `COMPONENT_ID`/`COMPONENT_VERSION`, never a component's
  own `configSchema`-derived config, never resource-connection variables.
  Those aren't namespaced by component ID, so two independently-authored
  modules could easily reuse the same name; giving each module its own
  isolated configuration is the shell author's job, not the platform's.
- Writes `BRICKKIT_SERVED_MEMBERS` (a reserved variable) into the shell's
  environment: a comma-separated list of the versioned service names
  currently part of the deployment. A compliant shell may read it to skip
  initializing (including skipping migrations and resource connections for)
  any compiled-in module not on the list — this is optional; the platform
  never checks whether a shell actually honors it.

`local: true` is untouched by this — same field, same meaning, same code
paths as always; `servedBy` is a wholly separate, independent mechanism that
happens to share the same "in the dependency graph but generates no workload"
shape.

**What a `servedBy` component's own `expose`/`exposePort`/`hostname`/
`replicas`/`resources`/`serviceAccountName` do:** nothing — the platform
warns, it doesn't error and doesn't silently ignore. These fields describe
"how my own container/Pod is deployed," and a `servedBy` component has none.

Full field-level detail, the validation rules, and what a shell implementation
itself must get right: [Building a qualified shell](docs/en/patterns/shell-implementers-guide.md).
```

Write the equivalent section in `AGENTS.zh.md` (native Chinese, matching the surrounding §5.6 "本地调试" section's tone), inserted at the same relative position.

- [ ] **Step 3: §7 schema skeleton**

In `AGENTS.md`'s `## 7. `brickkit.yaml` (project config) field skeleton` code block, inside the `components:` entry example, add a line after `localPort`:

```yaml
    localPort: 8081              # host port when local: true
    servedBy: <id>@<version>     # optional, this component's workload is provided by that other component
```

Same addition to `AGENTS.zh.md`'s equivalent skeleton (translate the inline comment naturally, not word-for-word).

**This is the field `tests/docfields/docfields_test.go` will check** — after this edit, run:

Run: `go test ./tests/docfields/... -v`
Expected: PASS (the new line must classify as a `brickkit.yaml` skeleton fragment already, since `components:` is a top-level key `config.Config` recognizes — confirm no new failures; if it fails, the skeleton fragment's YAML isn't parseable or `servedBy` was misspelled, fix it).

- [ ] **Step 4: §5.2 reserved-variable table note**

In `AGENTS.md` §5.2, right after the existing "Reserved-variable protection (two layers of defense)" paragraph (the one listing `COMPONENT_ID`, `COMPONENT_VERSION`, `*_ENDPOINT`, etc.), add `BRICKKIT_SERVED_MEMBERS` to the exact-match list in that same sentence:

Change:
```
**Reserved-variable protection (two layers of defense):** `COMPONENT_ID`, `COMPONENT_VERSION`
(exact match), `*_ENDPOINT` (suffix match), ...
```
to:
```
**Reserved-variable protection (two layers of defense):** `COMPONENT_ID`, `COMPONENT_VERSION`,
`BRICKKIT_SERVED_MEMBERS` (exact match), `*_ENDPOINT` (suffix match), ...
```

Same edit in `AGENTS.zh.md`'s equivalent sentence.

- [ ] **Step 5: §9.21 rewrite**

Replace the existing §9.21 body in `AGENTS.md` (currently arguing purely "why the platform doesn't do this") with a version that states what's now built while preserving the argument for why the platform still refuses to go further:

```markdown
**9.21 Why doesn't the platform offer a full consolidated-deployment
command, when it does offer `servedBy`?**
The need is real: a JVM component's memory floor is 200–450MB, so 20 of them
is 4–9G, and that genuinely might not fit during a private on-prem delivery.
The platform has **already done the hardest half of this for free** — a
caller only reads `*_ENDPOINT`; whether the other end is 10 containers, 1
container, or 10 modules inside one JVM is something it has no way to know.
`servedBy` (§5.7) closes the one gap that was actually the platform's own —
correctly routing addresses to a merged unit without borrowing `local: true`
and without the K8s-side gap that had no equivalent at all. Everything past
that (avoid port collisions inside the merged process, take over health
checks and migrations, isolate each module's config) still lives entirely in
the shell author's own code — not one more line of platform code is needed
there, and the platform still never has to understand which things *can* be
merged, what supervisor manages them, or how any given framework starts
multiple listeners. Building a full `--consolidated` command would still
mean starting to understand "the shell" in exactly the way §2.21 of the
architecture rationale argues against — `servedBy` is deliberately the
smallest structural piece that helps, not a step toward that larger, rejected
command.
```

Write the equivalent updated §9.21 in `AGENTS.zh.md`.

- [ ] **Step 6: §10 table row**

In `AGENTS.md` §10's table, update the row:

```
| A user asks "can I merge multiple components into one instance to save memory" | **Don't shoot it down, and don't say the platform supports it.** ... ⚠️ `enabled: false` **cannot** be used as a "I'm taking this over myself" switch (a required dependency being off means whatever depends on it stops too); there's no equivalent switch on K8s |
```

to:

```
| A user asks "can I merge multiple components into one instance to save memory" | First ask if it's JVM (20 Go/Rust components are only 0.4G, not worth it); then suggest GraalVM native images and on-demand activation (§2.15 of the architecture rationale). If they still want to merge: **`servedBy` (§5.7) is the supported path** — it handles address routing correctly on both Docker and K8s; everything else (module isolation, config, migrations ordering inside the shell) is still their own code, see the shell implementer's guide. `enabled: false` is unrelated to this — it still can't be used as a "I'm taking this over myself" switch |
```

Same update to `AGENTS.zh.md`'s equivalent row.

- [ ] **Step 7: §11.1 repo map and §11.2 dig-deeper table**

In `AGENTS.md` §11.1's code block, add `internal/shell/` to the `internal/` tree, right after `resolver/`:

```
  ├── resolver/           dependency resolution, topological sort
  ├── shell/              servedBy grouping/merging, shared by compose and k8s renderers
  ├── cascade/            cascade decision: figures out who actually starts this time (follows the top)
```

In §11.2's table, add a row (near the "Testing patterns" row):

```
| How to build a shell that qualifies for `servedBy` | `docs/en/patterns/shell-implementers-guide.md` (swap `en` for `zh`) |
```

Same edits to `AGENTS.zh.md`.

- [ ] **Step 8: Run the full doc-consistency check suite**

Run: `python3 scripts/check-docs-bilingual.py && go test ./tests/docfields/...`
Expected: both pass.

- [ ] **Step 9: Commit**

```bash
git add AGENTS.md AGENTS.zh.md
git commit -m "$(cat <<'EOF'
AGENTS.md/AGENTS.zh.md：记录 servedBy 已落地

新增 §5.7 说明 servedBy 的完整行为边界，更新 §4.1 拒绝清单、§5.2
保留变量表、§7 字段骨架、§9.21、§10 讨论指引、§11 仓库地图/深挖表，
全部改成反映"servedBy 已经落地"而不是"平台完全不做，只给 DIY 路径"。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Notes for whoever executes this plan

- Tasks 1→2→(3,4 can run in parallel)→(5,6 can run in parallel, both depend on 3&4 being done since they document real behavior). If using `subagent-driven-development`, keep this dependency order even though some tasks could theoretically parallelize — a fresh subagent doing Task 3 needs Task 2's `internal/shell` package to already exist and be committed.
- Every task's tests are written to fail first (real assertions, not `t.Skip`) — if a task's Step 1 test file doesn't fail for the expected reason in Step 2, stop and re-examine before writing the implementation; a test that already passes before the implementation exists means either the test is wrong or something in a prior task's step was skipped.
- `internal/k8s`'s test file (Task 4) explicitly calls out checking for existing test helpers before adding duplicates — `internal/k8s/*_test.go` is a large, multi-file test package and this plan was written without exhaustively reading every existing helper function in it; the implementer must grep first.
- After all six tasks: run `go test ./... && cd market-server && go test ./... && cd ..` once more as a final check, and re-run `python3 scripts/check-docs-bilingual.py`.
