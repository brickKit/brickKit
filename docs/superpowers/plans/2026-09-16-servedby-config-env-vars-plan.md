# servedBy 成员 config 值改走独立命名空间环境变量 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 `BRICKKIT_SERVED_MEMBERS_CONFIG` 把 servedBy 成员的 config 值打包进 JSON 内部、被 docker compose 自己的全文本 `${VAR}` 替换撑坏 JSON 这个 bug——不是打补丁，而是让每个成员自己的 config 值改走一条独立的、组件 ID 前缀命名的环境变量，`${VAR}` 占位符语义完全不变；`BRICKKIT_SERVED_MEMBERS_CONFIG` 的 JSON 从"携带值"变成"携带变量名的索引"。

**Architecture:** `internal/shell/shell.go` 的 `mergeGroup` 在现有"合并 `*_ENDPOINT` 类变量、同名同值放过/同名不同值报错"逻辑基础上，新增一个分支：把每个成员自己的 `SourceConfig`/`SourceOverride` 变量，用 `manifest.EnvPrefix(componentID) + "_" + 变量名` 重新命名后，走同一套碰撞检测，一并进入 `Group.Env`（外壳共享环境表）。`ServedMembersConfig()` 里的 `config` 字段改名 `configEnvVars`，值从原始配置值改成上面这条算出来的变量名。

**Tech Stack:** Go 1.22+，`encoding/json`，`github.com/stretchr/testify`（assert/require）。

**Spec:** `docs/superpowers/specs/2026-09-16-servedby-config-env-vars-design.md`

## Global Constraints

- 不给 `configSchema` 新增"这个字段是密钥"的声明能力——不在范围内。
- 不改变资源连接变量（`DATABASE_*` 等）在 servedBy 下的处理方式——只涉及 `component.config`。
- 不改 `internal/cli/up_secrets.go` 现有的明文密钥警告逻辑（`isHardcodedSecret`/`warnConfigSecrets`）——语义不变。
- `BRICKKIT_SERVED_MEMBERS_CONFIG` 才上线一天，没有外部依赖方接入旧形状——不需要兼容层、不需要迁移说明，直接改成新形状。
- 变量命名复用已有函数：`manifest.EnvPrefix(id)`（`internal/manifest/envvar.go`）与 `inject.EnvVarName(key)`（`internal/inject/reserved.go`）——不新造转换算法。
- 每处代码改动完成后运行 `go build ./...` 与相关包的 `go test`，全绿才进入下一个任务。

---

## Task 1: `mergeGroup` 把每个成员自己的 config 值合并成带前缀的独立变量

**Files:**
- Modify: `internal/shell/shell.go:344-384`（`mergeGroup` 函数）、文件末尾新增一个 `configVarCollisionError` 函数
- Test: `internal/shell/shell_test.go`（在 `TestResolveMergesOnlyEndpointVars` 之后、`TestResolveEndpointCollisionSameValueIsFine` 之前插入新测试；碰撞测试插在 `TestResolveEndpointCollisionDifferentVersionErrors` 之后）

**Interfaces:**
- Consumes：`inject.Var{Name, Value, Source, Key}`（已有）、`inject.SourceConfig`/`inject.SourceOverride`（已有常量）、`manifest.EnvPrefix(id string) string`（已有导出函数）、`clierr.Error`/`clierr.Newf`/`WithDetail`/`WithDetailf`/`WithHint`（已有）
- Produces：`mergeGroup` 的返回值 `[]inject.Var` 现在除了 `*_ENDPOINT` 类变量，还包含形如 `{EnvPrefix(componentID)}_{原变量名}` 的成员 config 变量；新增的 `configVarCollisionError(shellRef, firstOwner, secondOwner resolver.Ref, name, firstValue, secondValue string) *clierr.Error`，供 Task 2 之外的代码不需要调用（仅 `mergeGroup` 内部使用）

- [ ] **Step 1: 写失败测试——成员自己的 config 值要生成带前缀的独立变量**

在 `internal/shell/shell_test.go` 的 `TestResolveMergesOnlyEndpointVars` 函数之后插入：

```go
// ---- 环境变量合并：成员自己的 config 值改走带组件 ID 前缀的独立变量
// （brickKit 反馈：servedBy 的密钥类 config 值会被 docker-compose 撑坏
// JSON——密钥类 config 值必须继续走 "${VAR} 占位符 + docker compose
// 自己展开" 这条已证明安全的老路，不能被塞进 BRICKKIT_SERVED_MEMBERS_CONFIG
// 的 JSON 字符串内部，那样会被 docker compose 的全文本替换撑坏结构）----

func TestResolveMergesMemberConfigAsNamespacedVars(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		{ID: "erp/sales", Version: "1.0.0", ServedBy: "infra/shell-go-core@1.0.0",
			Config: map[string]any{"pgSchema": "sales"}},
	}}
	member := simple("erp/sales", "1.0.0", 8080)
	member.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"pgSchema": {Type: "string", Default: "public"},
	}}

	groups, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"erp/sales@1.0.0":           member,
	})
	require.NoError(t, err)
	require.Len(t, groups, 1)

	byName := map[string]string{}
	for _, v := range groups[0].Env {
		byName[v.Name] = v.Value
	}
	assert.Equal(t, "sales", byName["ERP_SALES_PG_SCHEMA"],
		"成员自己的 config 值要各自生成一条带组件 ID 前缀的独立变量，进外壳共享环境——"+
			"跟 *_ENDPOINT 用同一个前缀算法（manifest.EnvPrefix）")
}

// 同一个外壳收编同一组件的两个版本（TestResolveGroupsTwoVersionsOfSameComponentUnderOneShell
// 已证明是合法用法），这一项 config 值恰好相同时不该报冲突——跟 *_ENDPOINT 的
// "同名同值放过" 是同一条规则。
func TestResolveConfigVarCollisionSameValueIsFine(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		{ID: "mdm/customer", Version: "1.0.7", ServedBy: "infra/shell-go-core@1.0.0",
			Config: map[string]any{"pgSchema": "customer"}},
		{ID: "mdm/customer", Version: "2.0.0", ServedBy: "infra/shell-go-core@1.0.0",
			Config: map[string]any{"pgSchema": "customer"}},
	}}
	v1 := simple("mdm/customer", "1.0.7", 8080)
	v1.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{"pgSchema": {Type: "string"}}}
	v2 := simple("mdm/customer", "2.0.0", 8081)
	v2.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{"pgSchema": {Type: "string"}}}

	groups, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        v1,
		"mdm/customer@2.0.0":        v2,
	})
	require.NoError(t, err, "两个版本这一项 config 值相同，不该报冲突")
	require.Len(t, groups, 1)
}

// 同一个外壳收编同一组件的两个版本，这一项 config 值不同时必须报错并点名双方——
// 这条变量名不含版本号（跟 *_ENDPOINT 一致），两个不同版本给出不同值时，
// 同一个变量名不可能同时代表两个值。
func TestResolveConfigVarCollisionDifferentValueErrors(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		{ID: "mdm/customer", Version: "1.0.7", ServedBy: "infra/shell-go-core@1.0.0",
			Config: map[string]any{"pgSchema": "customer_v1"}},
		{ID: "mdm/customer", Version: "2.0.0", ServedBy: "infra/shell-go-core@1.0.0",
			Config: map[string]any{"pgSchema": "customer_v2"}},
	}}
	v1 := simple("mdm/customer", "1.0.7", 8080)
	v1.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{"pgSchema": {Type: "string"}}}
	v2 := simple("mdm/customer", "2.0.0", 8081)
	v2.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{"pgSchema": {Type: "string"}}}

	_, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        v1,
		"mdm/customer@2.0.0":        v2,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MDM_CUSTOMER_PG_SCHEMA")
}
```

- [ ] **Step 2: 运行测试，确认按预期失败**

```bash
go test ./internal/shell/... -run 'TestResolveMergesMemberConfigAsNamespacedVars|TestResolveConfigVarCollisionSameValueIsFine|TestResolveConfigVarCollisionDifferentValueErrors' -v
```

期望：`TestResolveMergesMemberConfigAsNamespacedVars` 失败（`byName["ERP_SALES_PG_SCHEMA"]` 是空字符串，因为现在还没有任何代码生成这条变量）；`TestResolveConfigVarCollisionDifferentValueErrors` 失败（`require.Error` 拿到 nil，因为现在两个版本的 config 值根本不会被拿来比较）；`TestResolveConfigVarCollisionSameValueIsFine` 应该已经通过（这条在改动前后都不该报错，是个回归防护，不是新行为）——如果这条也失败，先停下来看是不是测试本身写错了。

- [ ] **Step 3: 实现——`mergeGroup` 新增成员 config 合并分支**

打开 `internal/shell/shell.go`，把 `mergeGroup` 函数体里下面这一段（第 360-376 行左右）：

```go
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
				return nil, endpointCollisionError(shellRef, envOwner[v.Name], m.Ref, v.Name, existing, v.Value)
			}
			envValues[v.Name] = v.Value
			envVars[v.Name] = v
			envOwner[v.Name] = m.Ref
		}
	}
```

替换成：

```go
	for _, m := range members {
		mEnv := envByRef[m.Ref]
		prefix := manifest.EnvPrefix(m.Ref.ID)
		for _, v := range mEnv.Env {
			switch v.Source {
			case inject.SourceEndpoint:
				if existing, exists := envValues[v.Name]; exists {
					if existing == v.Value {
						continue
					}
					return nil, endpointCollisionError(shellRef, envOwner[v.Name], m.Ref, v.Name, existing, v.Value)
				}
				envValues[v.Name] = v.Value
				envVars[v.Name] = v
				envOwner[v.Name] = m.Ref
			case inject.SourceConfig, inject.SourceOverride:
				// 每个成员自己的 config 值各自生成一条带组件 ID 前缀的
				// 独立变量（跟 *_ENDPOINT 用同一个前缀算法），${VAR} 占位符
				// 语义完全不变，交给 docker compose 自己展开——不摊平进
				// 不带前缀的共享键（那是被否决过的方案，两个模块用同一个
				// 通用 key 名会撞车），也不塞进 BRICKKIT_SERVED_MEMBERS_CONFIG
				// 的 JSON 内部（那正是密钥类 ${VAR} 占位符撑坏 JSON 的根源，
				// 见 docs/superpowers/specs/2026-09-16-servedby-config-env-vars-design.md）。
				name := prefix + "_" + v.Name
				if existing, exists := envValues[name]; exists {
					if existing == v.Value {
						continue
					}
					return nil, configVarCollisionError(shellRef, envOwner[name], m.Ref, name, existing, v.Value)
				}
				v.Name = name
				envValues[name] = v.Value
				envVars[name] = v
				envOwner[name] = m.Ref
			}
		}
	}
```

然后在文件末尾（`endpointCollisionError` 函数之后）新增：

```go

// configVarCollisionError 生成"两个成员的 config 值算出了同一个环境变量名，
// 但值不同"的错误——跟 endpointCollisionError 同一个报错形状，原因文案不同：
// 这条变量名由组件 ID 与 config 项名拼出来、不含版本号，最常见的成因是同一个
// 组件 ID 的两个不同版本被同一个外壳收编，且这一项 config 的值不一样。
func configVarCollisionError(
	shellRef, firstOwner, secondOwner resolver.Ref, name, firstValue, secondValue string,
) *clierr.Error {
	return clierr.Newf(clierr.CodeConfigInvalid,
		"错误：外壳 %s 下两个成员的 config 算出了同一个环境变量名，但值不同", shellRef.String()).
		WithDetail("变量名", name).
		WithDetailf(firstOwner.String(), "%s", firstValue).
		WithDetailf(secondOwner.String(), "%s", secondValue).
		WithDetail("原因", "这条变量名由组件 ID 与 config 项名拼出来，不含版本号——最常见的成因是"+
			"同一个组件 ID 的两个不同版本被同一个外壳收编，且这一项 config 的值不一样").
		WithHint("让这两个成员这一项 config 的值保持一致，或者不要把它们放进同一个外壳")
}
```

- [ ] **Step 4: 运行测试，确认通过**

```bash
go test ./internal/shell/... -v
```

期望：全部通过，包括 Step 1 新增的三条和文件里原有的全部测试（尤其确认 `TestResolveMergesOnlyEndpointVars`、`TestResolveEndpointCollisionSameValueIsFine`、`TestResolveEndpointCollisionDifferentVersionErrors`、`TestResolveGroupsTwoVersionsOfSameComponentUnderOneShell` 这几条既有测试没有被这次改动带崩）。

- [ ] **Step 5: 提交**

```bash
git add internal/shell/shell.go internal/shell/shell_test.go
git commit -m "$(cat <<'EOF'
修复：servedBy 成员自己的 config 值改走带组件 ID 前缀的独立环境变量

BRICKKIT_SERVED_MEMBERS_CONFIG 把成员 config 值打包进 JSON 字符串内部，
撞上了 docker compose 自己对生成文件做无结构感知全文本 ${VAR} 替换这件
事：密钥类 config 值只在 .env、没 export 到 CLI 进程时会以占位符原样
留在 JSON 里，docker compose 运行时把真实多行密钥糊进去，撑坏 JSON。

mergeGroup 现在给每个成员自己的每个 config 值，各自生成一条独立的、
manifest.EnvPrefix(componentID) 前缀命名的环境变量，跟 *_ENDPOINT 走
同一套碰撞检测（同名同值放过、同名不同值报错，覆盖"同一个外壳收编同一
组件两个版本"这个已有测试证明合法的场景）。${VAR} 占位符语义完全不变，
继续交给 docker compose 自己展开——这条变量本身还是一行独立的标量赋值，
不再嵌在任何结构化字符串内部。

见 docs/superpowers/specs/2026-09-16-servedby-config-env-vars-design.md。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: `ServedMembersConfig()` 的 `config` 字段改名 `configEnvVars`，携带变量名而不是值

**Files:**
- Modify: `internal/shell/shell.go`（`servedMemberConfigEntry` 结构体、`ServedMembersConfig()` 函数、`EnvVarServedMembersConfig` 常量的文档注释）
- Test: `internal/shell/shell_test.go`（修改既有的 `TestServedMembersConfigFormatting`，新增一条测试）

**Interfaces:**
- Consumes：Task 1 未改变 `Member.Config`（`map[string]string`，原始 key → 值）的形状，本任务继续读它；`inject.EnvVarName(key string) string`（已有导出函数）
- Produces：`servedMemberConfigEntry.ConfigEnvVars map[string]string`（`json:"configEnvVars"`），值是 `manifest.EnvPrefix(componentID) + "_" + inject.EnvVarName(key)` 算出来的变量名

- [ ] **Step 1: 修改既有测试，反映新字段名与新语义**

打开 `internal/shell/shell_test.go`，找到 `TestServedMembersConfigFormatting`（当前内容）：

```go
func TestServedMembersConfigFormatting(t *testing.T) {
	g := shell.Group{Members: []shell.Member{
		{
			Ref: resolver.Ref{ID: "erp/sales", Version: "1.0.0"}, Port: 8080,
			ExtraPorts: []manifest.ExtraPort{{Name: "grpc", Port: 9090}},
			Config:     map[string]string{"pgSchema": "sales"},
		},
		{
			Ref: resolver.Ref{ID: "mdm/customer", Version: "1.0.7"}, Port: 8081,
			Config: map[string]string{"pgSchema": "customer"},
		},
	}}

	var entries []map[string]any
	require.NoError(t, json.Unmarshal([]byte(g.ServedMembersConfig()), &entries))
	require.Len(t, entries, 2, "按 componentId 字典序排列")

	assert.Equal(t, "erp/sales", entries[0]["componentId"])
	assert.Equal(t, "1.0.0", entries[0]["version"])
	assert.Equal(t, float64(8080), entries[0]["httpPort"])
	assert.Equal(t, []any{map[string]any{"name": "grpc", "port": float64(9090)}}, entries[0]["extraPorts"])
	assert.Equal(t, map[string]any{"pgSchema": "sales"}, entries[0]["config"])

	assert.Equal(t, "mdm/customer", entries[1]["componentId"])
}
```

整个替换成：

```go
func TestServedMembersConfigFormatting(t *testing.T) {
	g := shell.Group{Members: []shell.Member{
		{
			Ref: resolver.Ref{ID: "erp/sales", Version: "1.0.0"}, Port: 8080,
			ExtraPorts: []manifest.ExtraPort{{Name: "grpc", Port: 9090}},
			Config:     map[string]string{"pgSchema": "sales"},
		},
		{
			Ref: resolver.Ref{ID: "mdm/customer", Version: "1.0.7"}, Port: 8081,
			Config: map[string]string{"pgSchema": "customer"},
		},
	}}

	var entries []map[string]any
	require.NoError(t, json.Unmarshal([]byte(g.ServedMembersConfig()), &entries))
	require.Len(t, entries, 2, "按 componentId 字典序排列")

	assert.Equal(t, "erp/sales", entries[0]["componentId"])
	assert.Equal(t, "1.0.0", entries[0]["version"])
	assert.Equal(t, float64(8080), entries[0]["httpPort"])
	assert.Equal(t, []any{map[string]any{"name": "grpc", "port": float64(9090)}}, entries[0]["extraPorts"])
	assert.Equal(t, map[string]any{"pgSchema": "ERP_SALES_PG_SCHEMA"}, entries[0]["configEnvVars"],
		"携带的是算出来的变量名，不是原始值——外壳去读那条独立变量，不从 JSON 里抠值")

	assert.Equal(t, "mdm/customer", entries[1]["componentId"])
	assert.Equal(t, map[string]any{"pgSchema": "MDM_CUSTOMER_PG_SCHEMA"}, entries[1]["configEnvVars"])
}

// 直接的回归测试：就算某个成员的 config 值本身还是未展开的 ${VAR} 占位符
// （brickkit up 那个进程查不到这个环境变量、只在 .env 里有时，就会是这个
// 样子——见 config.ExpandEnv），BRICKKIT_SERVED_MEMBERS_CONFIG 的 JSON 里
// 也不该出现这段文本——这正是撑坏 JSON 那个 bug 的根源。
func TestServedMembersConfigNeverEmbedsRawPlaceholderText(t *testing.T) {
	g := shell.Group{Members: []shell.Member{
		{
			Ref: resolver.Ref{ID: "infra/iam-casdoor", Version: "1.0.0"}, Port: 8080,
			Config: map[string]string{"appTokenSigningKeyPem": "${APP_TOKEN_SIGNING_KEY_PEM}"},
		},
	}}

	out := g.ServedMembersConfig()

	assert.NotContains(t, out, "${",
		"值本身是不是 ${VAR} 占位符不该影响 JSON 是否合法——JSON 里现在只装变量名")

	var entries []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &entries))
	configEnvVars, ok := entries[0]["configEnvVars"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "INFRA_IAM_CASDOOR_APP_TOKEN_SIGNING_KEY_PEM", configEnvVars["appTokenSigningKeyPem"])
}
```

- [ ] **Step 2: 运行测试，确认按预期失败**

```bash
go test ./internal/shell/... -run 'TestServedMembersConfigFormatting|TestServedMembersConfigNeverEmbedsRawPlaceholderText' -v
```

期望：两条都失败——`TestServedMembersConfigFormatting` 报 `entries[0]["configEnvVars"]` 是 `nil`（当前 JSON 字段还叫 `config`）；`TestServedMembersConfigNeverEmbedsRawPlaceholderText` 里 `configEnvVars` 断言同样拿到 `nil`（`assert.NotContains` 那一条这时候应该已经过——当前实现下 `${APP_TOKEN_SIGNING_KEY_PEM}` 本来就会原样出现在 `config` 字段里，如果这一条现在就失败了，说明沿用的还是旧字段名，先确认没有别的地方漏改）。

- [ ] **Step 3: 实现——重命名字段、改写 `ServedMembersConfig()`**

打开 `internal/shell/shell.go`，把 `servedMemberConfigEntry` 结构体（当前）：

```go
type servedMemberConfigEntry struct {
	ComponentID string                  `json:"componentId"`
	Version     string                  `json:"version"`
	HTTPPort    int                     `json:"httpPort"`
	ExtraPorts  []servedMemberExtraPort `json:"extraPorts"`
	Config      map[string]string       `json:"config"`
}
```

改成：

```go
type servedMemberConfigEntry struct {
	ComponentID   string                  `json:"componentId"`
	Version       string                  `json:"version"`
	HTTPPort      int                     `json:"httpPort"`
	ExtraPorts    []servedMemberExtraPort `json:"extraPorts"`
	ConfigEnvVars map[string]string       `json:"configEnvVars"`
}
```

这个结构体上方的文档注释（"刻意不包含的两类数据"那一段）保留，在末尾补一句说明新字段的语义——把：

```go
//   - 资源连接变量（DATABASE_* 等）——这些可能标了 inject.Var.SecretKey，
//     该走 K8s Secret（005 §5.6），混进这条明文 JSON 会绕开那层处理。
//     而且提案本身要的也只是"把已经算好的 config 数据交出来"。
type servedMemberConfigEntry struct {
```

改成：

```go
//   - 资源连接变量（DATABASE_* 等）——这些可能标了 inject.Var.SecretKey，
//     该走 K8s Secret（005 §5.6），混进这条明文 JSON 会绕开那层处理。
//     而且提案本身要的也只是"把已经算好的 config 数据交出来"。
//
// ConfigEnvVars 携带的是变量名，不是值——member.Ref.ID 与原始 config key
// 拼出来的那条独立环境变量（mergeGroup 已经把它并入 Group.Env）才是真正
// 的值所在，外壳读这份 JSON 拿变量名，再去自己的进程环境读值。这样即使
// 某个成员的 config 值本身还是未展开的 ${VAR} 占位符（密钥类配置的标准
// 写法），也不会被塞进这条 JSON 字符串内部——那正是 docker compose 自己
// 的全文本 ${VAR} 替换会撑坏 JSON 结构的根源，见
// docs/superpowers/specs/2026-09-16-servedby-config-env-vars-design.md。
type servedMemberConfigEntry struct {
```

然后把 `ServedMembersConfig()` 函数体（当前）：

```go
func (g Group) ServedMembersConfig() string {
	entries := make([]servedMemberConfigEntry, 0, len(g.Members))
	for _, m := range g.Members {
		ports := make([]servedMemberExtraPort, 0, len(m.ExtraPorts))
		for _, p := range m.ExtraPorts {
			ports = append(ports, servedMemberExtraPort{Name: p.Name, Port: p.Port})
		}
		cfg := m.Config
		if cfg == nil {
			cfg = map[string]string{}
		}
		entries = append(entries, servedMemberConfigEntry{
			ComponentID: m.Ref.ID, Version: m.Ref.Version, HTTPPort: m.Port,
			ExtraPorts: ports, Config: cfg,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ComponentID < entries[j].ComponentID })

	// entries 全部由 string/int/slice/map[string]string 拼成，没有 channel、
	// func 这类 json 编不了的类型，也没有自定义 MarshalJSON 会出岔子——
	// 这里的 err 结构性地不可能非 nil。
	out, _ := json.Marshal(entries)
	return string(out)
}
```

改成：

```go
func (g Group) ServedMembersConfig() string {
	entries := make([]servedMemberConfigEntry, 0, len(g.Members))
	for _, m := range g.Members {
		ports := make([]servedMemberExtraPort, 0, len(m.ExtraPorts))
		for _, p := range m.ExtraPorts {
			ports = append(ports, servedMemberExtraPort{Name: p.Name, Port: p.Port})
		}
		prefix := manifest.EnvPrefix(m.Ref.ID)
		configEnvVars := make(map[string]string, len(m.Config))
		for key := range m.Config {
			configEnvVars[key] = prefix + "_" + inject.EnvVarName(key)
		}
		entries = append(entries, servedMemberConfigEntry{
			ComponentID: m.Ref.ID, Version: m.Ref.Version, HTTPPort: m.Port,
			ExtraPorts: ports, ConfigEnvVars: configEnvVars,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ComponentID < entries[j].ComponentID })

	// entries 全部由 string/int/slice/map[string]string 拼成，没有 channel、
	// func 这类 json 编不了的类型，也没有自定义 MarshalJSON 会出岔子——
	// 这里的 err 结构性地不可能非 nil。
	out, _ := json.Marshal(entries)
	return string(out)
}
```

最后把 `EnvVarServedMembersConfig` 常量上方的文档注释（当前）：

```go
// EnvVarServedMembersConfig 是外壳容器上"当前实际收编的每个成员，完整的
// componentId/version/端口/合并后 config"的保留变量名（brickKit 反馈：
// 两个降低 servedBy 运维摩擦的架构提案，提案一）。跟 BRICKKIT_SERVED_MEMBERS
// （只有一份名字列表）互补：外壳实现者从这里能拿到装配每个模块所需的
// 全部数据，不需要再自己维护一份容易过期的手工 JSON（brickkit.yaml 一改
// 版本号/config/servedBy 归属，那份手工数据就得跟着重新生成，忘了就是
// 外壳真机启动时才炸）。
const EnvVarServedMembersConfig = "BRICKKIT_SERVED_MEMBERS_CONFIG"
```

改成：

```go
// EnvVarServedMembersConfig 是外壳容器上"当前实际收编的每个成员，
// componentId/version/端口，以及每一项 config 对应的环境变量名"的保留
// 变量名（brickKit 反馈：两个降低 servedBy 运维摩擦的架构提案，提案一；
// JSON 形状定稿见 docs/superpowers/specs/2026-09-16-servedby-config-env-vars-design.md）。
// 跟 BRICKKIT_SERVED_MEMBERS（只有一份名字列表）互补：外壳实现者从这里
// 能拿到装配每个模块所需的索引信息，不需要再自己维护一份容易过期的手工
// JSON。**这份 JSON 不携带 config 的值本身**——每个值都在外壳环境里
// 独立成一条带组件 ID 前缀的变量（mergeGroup 负责合并），这里只给"这个
// key 对应哪个变量名"，避免密钥类 config 值（常以未展开的 ${VAR} 占位符
// 形式存在）被塞进这条 JSON 字符串内部、被 docker compose 自己的全文本
// ${VAR} 替换撑坏结构。
const EnvVarServedMembersConfig = "BRICKKIT_SERVED_MEMBERS_CONFIG"
```

- [ ] **Step 4: 运行测试，确认通过**

```bash
go test ./internal/shell/... -v
```

期望：全部通过。

- [ ] **Step 5: 全仓库构建与测试**

```bash
go build ./...
go test ./...
```

期望：全绿（`internal/shell` 是唯一改动的包，但要确认没有其它包间接依赖 `servedMemberConfigEntry`/`Config` 这个未导出字段——它是包内私有类型，正常情况下不会有）。

- [ ] **Step 6: 提交**

```bash
git add internal/shell/shell.go internal/shell/shell_test.go
git commit -m "$(cat <<'EOF'
修复：BRICKKIT_SERVED_MEMBERS_CONFIG 的 config 字段改名 configEnvVars

字段语义从"key → 原始配置值"变成"key → Task 1 里 mergeGroup 已经并入
Group.Env 的那条独立环境变量的名字"——JSON 现在只是一份索引，不再携带
任何可能是 ${VAR} 占位符的原始文本，从根上不会再被 docker compose 自己
的全文本替换撑坏结构。

见 docs/superpowers/specs/2026-09-16-servedby-config-env-vars-design.md。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: 更新 `shell-implementers-guide.md`（中英文）

**Files:**
- Modify: `docs/zh/patterns/shell-implementers-guide.md`
- Modify: `docs/en/patterns/shell-implementers-guide.md`

**Interfaces:** 无代码接口，纯文档。

- [ ] **Step 1: 改中文版**

打开 `docs/zh/patterns/shell-implementers-guide.md`，把这一行（当前第 15 行）：

```
- **`BRICKKIT_SERVED_MEMBERS_CONFIG`**——上面那份名单的详细版：一个 JSON 数组，当前被收编的每个成员各占一个元素，字段是 `componentId`/`version`/`httpPort`/`extraPorts`/`config`（`config` 是该成员合并后的自身配置，键是原始 configSchema key——驼峰形式，不是转换后的大写下划线环境变量名）。零个成员时是 `[]`，不是不存在这个变量。这条是给真要把每个模块装配进壳的外壳作者用的：不用再自己写一个命令行工具、在装配前手算一份 JSON、手工贴进 `brickkit.yaml` 某个 `configSchema` 字符串配置项里——那种手工维护的数据只要组件版本号、config 值或收编关系一变就会过期，平台不会报错，只会在外壳真机启动时才炸（要么装错模块，要么直接 crash-loop）。
```

改成：

```
- **`BRICKKIT_SERVED_MEMBERS_CONFIG`**——上面那份名单的详细版：一个 JSON 数组，当前被收编的每个成员各占一个元素，字段是 `componentId`/`version`/`httpPort`/`extraPorts`/`configEnvVars`（`configEnvVars` 把该成员自己的 configSchema key——驼峰形式，不是转换后的大写下划线名——映射到*你自己进程环境里*那条独立变量的名字，真正的值就在那条变量里）。零个成员时是 `[]`，不是不存在这个变量。这条是给真要把每个模块装配进壳的外壳作者用的：不用再自己写一个命令行工具、在装配前手算一份 JSON、手工贴进 `brickkit.yaml` 某个 `configSchema` 字符串配置项里——那种手工维护的数据只要组件版本号、config 值或收编关系一变就会过期，平台不会报错，只会在外壳真机启动时才炸（要么装错模块，要么直接 crash-loop）。
```

然后把"平台刻意不会往那里放什么"这一整节（当前第 16-19 行）：

```
## 平台刻意不会往那里放什么

一个被收编组件自己的 `configSchema` 配置值，不会像 `*_ENDPOINT` 那样被摊平合并进外壳共享的操作系统环境变量表——这不是遗漏，是一条硬边界。`*_ENDPOINT` 这类变量名是从组件 ID 算出来的，天然在整个系统里唯一；而 `pgSchema` 这样的配置键不是——两个各自独立开发的模块完全可能都用这个通用名字表示两个完全不同的值，摊平合并进同一个共享的进程环境，只会悄悄让一个覆盖另一个。这些值仍然拿得到，只是换了一种不会撞车的形状：上面的 `BRICKKIT_SERVED_MEMBERS_CONFIG` 把它们打包进一个按 `componentId` 分开的 JSON 数组，不是摊平进一张扁平的键值表。资源连接变量（`DATABASE_*` 之类）则完全不在这条 JSON 里——它们可能标了本该走 K8s Secret 的密钥身份，混进一条明文变量会绕开那层处理；给每个模块自己那份独立的资源连接，仍然是你的活，不是平台的——见下面第 5 条。同样的道理，平台也从不会为除外壳自己以外的任何东西写 `COMPONENT_ID` 或 `COMPONENT_VERSION`：这两个变量名是固定的，一旦你收编超过一个组件就必然撞车。
```

整节改成：

```
## 哪些会合并进来，哪些不会

一个被收编组件自己的 `configSchema` 配置值，**会**合并进外壳共享的操作系统环境变量表——但从来不用组件自己那个不带前缀的名字。每一条都落在 `{EnvPrefix(componentId)}_{独立部署时会用的那个大写下划线名}` 这个名字下——跟 `*_ENDPOINT` 变量用的是同一条前缀规则。`pgSchema` 这样的配置键单独拿出来不保证唯一（两个各自独立开发的模块完全可能都用这个通用名字表示两个完全不同的值），这正是它从不以不带前缀的形式出现的原因；加上组件 ID 前缀之后，结构性地不可能跟另一个成员的同名 key 相撞。上面的 `BRICKKIT_SERVED_MEMBERS_CONFIG` 是这些变量的一份索引，不是它们值的第二份拷贝——读 `configEnvVars[key]` 拿到变量*名*，再去自己的环境里读那条变量拿到真正的值。这层间接不是随手加的：`brickkit.yaml` 里成员的 config 值经常是指向密钥的 `${VAR}` 引用，而 brickKit 生成 Docker Compose 文件时刻意从不自己展开这类引用（生成的文件本该能安全地被人打开看、进 git diff）——占位符要留到 `docker compose` 运行时才展开。如果把这类占位符的*值*打包进一段 JSON 字符串，一旦它展开成带引号、反斜杠或换行符的内容（比如一份多行 PEM 密钥），docker compose 自己那套无结构感知的全文本替换就会把这段 JSON 撑坏——把每个值留在自己独立的、单一用途的环境变量里，跟独立部署组件自己的 config 完全同构，就彻底避开了这一类问题。

资源连接变量（`DATABASE_*` 之类）依然完全不会代替成员合并进外壳环境——它们可能标了本该走 K8s Secret 的密钥身份，混进来会绕开那层处理；给每个模块自己那份独立的资源连接，仍然是你的活，不是平台的——见下面第 5 条。同样的道理，平台也从不会为除外壳自己以外的任何东西写 `COMPONENT_ID` 或 `COMPONENT_VERSION`：这两个变量名是固定的，一旦你收编超过一个组件就必然撞车。
```

- [ ] **Step 2: 改英文版**

打开 `docs/en/patterns/shell-implementers-guide.md`，把这一段（当前第 63-76 行）：

```
- **`BRICKKIT_SERVED_MEMBERS_CONFIG`** — the detailed counterpart to the
  list above: a JSON array with one element per currently-absorbed member,
  each carrying `componentId`/`version`/`httpPort`/`extraPorts`/`config`
  (`config` is that member's own merged configuration, keyed by the
  original configSchema key — camelCase, not the converted uppercase
  environment-variable name). Zero members still gives you `[]`, not a
  missing variable. This one is for shell authors who actually have to wire
  each module up: no more hand-writing a CLI tool that computes a JSON blob
  before every deployment and pastes it into a string field under some
  shell component's own `configSchema` in `brickkit.yaml` — that kind of
  hand-maintained data goes stale the instant a member's version, config
  values, or `servedBy` membership changes, and the platform won't warn
  you; it just crash-loops (or wires up the wrong module) the next time the
  shell actually starts.
```

改成：

```
- **`BRICKKIT_SERVED_MEMBERS_CONFIG`** — the detailed counterpart to the
  list above: a JSON array with one element per currently-absorbed member,
  each carrying `componentId`/`version`/`httpPort`/`extraPorts`/`configEnvVars`
  (`configEnvVars` maps that member's own configSchema key — camelCase, not
  the converted uppercase environment-variable name — to the name of the
  independent environment variable, in your own process environment, where
  its actual merged value lives). Zero members still gives you `[]`, not a
  missing variable. This one is for shell authors who actually have to wire
  each module up: no more hand-writing a CLI tool that computes a JSON blob
  before every deployment and pastes it into a string field under some
  shell component's own `configSchema` in `brickkit.yaml` — that kind of
  hand-maintained data goes stale the instant a member's version, config
  values, or `servedBy` membership changes, and the platform won't warn
  you; it just crash-loops (or wires up the wrong module) the next time the
  shell actually starts.
```

然后把"What the platform deliberately does not put there"这一整节（当前第 78-99 行）：

```
## What the platform deliberately does not put there

An absorbed component's own `configSchema`-derived configuration values are
never flattened into your shell's shared OS environment the way
`*_ENDPOINT` variables are, and this is not an oversight to work around —
it's a hard boundary. `*_ENDPOINT` variable names are derived from a
component ID, so they're guaranteed unique across your whole system; a
config key like `pgSchema` is not — two independently-authored modules can
easily reuse the same generic name for two entirely different values, and
flattening those into one shared process environment would silently let one
overwrite the other. The values are still available, just in a shape that
can't collide: `BRICKKIT_SERVED_MEMBERS_CONFIG` above packages them into a
JSON array keyed by `componentId`, not a flat table of environment
variables. Resource-connection variables (`DATABASE_*` and friends) never
appear in that JSON at all — they can carry a secret identity that's
supposed to go through a K8s Secret, and folding them into a plaintext
variable would route around that handling. Giving each of your absorbed
modules its own isolated resource connections is still squarely your job,
not the platform's — see property 5 below. The platform also never puts
`COMPONENT_ID` or `COMPONENT_VERSION` for anything but the shell itself
into that environment, for the same reason: those variable names are fixed
and would collide the instant you absorb more than one component.
```

整节改成：

```
## What gets merged in, and what doesn't

An absorbed component's own `configSchema`-derived configuration values
*do* get merged into your shell's shared OS environment — just never under
the component's own, unprefixed name. Each one lands under
`{EnvPrefix(componentId)}_{the same uppercase name a standalone deployment
would use}` — the identical prefix rule `*_ENDPOINT` variables already use.
A config key like `pgSchema` on its own is not guaranteed unique (two
independently-authored modules can easily reuse the same generic name for
two entirely different values), which is exactly why it never appears
unprefixed; prefixed by component ID, it structurally cannot collide with
another member's same-named key. `BRICKKIT_SERVED_MEMBERS_CONFIG` above is
an index into these variables, not a second copy of their values — read
`configEnvVars[key]` to get the variable *name*, then read that variable
from your own environment to get the real value. This indirection is
deliberate: a member's config value in `brickkit.yaml` is frequently a
`${VAR}` reference to a secret, and brickKit's Docker Compose generation
deliberately never resolves those itself (the generated file is meant to
stay safe to open and diff) — the placeholder is left for `docker compose`
to expand only at container-start time. Packing such a placeholder's
*value* into a JSON string would let `docker compose`'s own blind,
structure-unaware text substitution corrupt that JSON the moment the
placeholder expands into something containing a quote, backslash, or
newline (a multi-line PEM key, say); keeping each value in its own
single-purpose environment variable — exactly like a standalone
component's own config — avoids that entirely.

Resource-connection variables (`DATABASE_*` and friends) still never merge
in on a member's behalf — they can carry a secret identity that's supposed
to go through a K8s Secret, and folding them in would route around that
handling. Giving each of your absorbed modules its own isolated resource
connections is still squarely your job, not the platform's — see property
5 below. The platform also never puts `COMPONENT_ID` or `COMPONENT_VERSION`
for anything but the shell itself into that environment, for the same
reason: those variable names are fixed and would collide the instant you
absorb more than one component.
```

- [ ] **Step 3: 跑文档一致性检查**

```bash
python3 scripts/check-docs.py
python3 scripts/check-docs-bilingual.py
```

期望：两个都输出全部 ✅（这一步不改变任何小节标题的锚点被外部引用的情况——Task 4 开始前已经确认过没有别的文档链接指向这两个标题）。

- [ ] **Step 4: 提交**

```bash
git add docs/zh/patterns/shell-implementers-guide.md docs/en/patterns/shell-implementers-guide.md
git commit -m "$(cat <<'EOF'
更新：造壳指南同步 BRICKKIT_SERVED_MEMBERS_CONFIG 的新形状

configEnvVars 字段携带的是变量名，不是值——每个成员自己的 config 值现在
各自并入外壳环境、带组件 ID 前缀，JSON 只是一份指向这些变量的索引。
补充说明了为什么必须这样设计：密钥类 config 值常是未展开的 ${VAR} 占位
符，塞进 JSON 字符串内部会被 docker compose 自己的全文本替换撑坏结构。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: 更新 `AGENTS.md` 与 `AGENTS.zh.md`

**Files:**
- Modify: `AGENTS.md`
- Modify: `AGENTS.zh.md`

**Interfaces:** 无代码接口，纯文档。

- [ ] **Step 1: 改英文版**

打开 `AGENTS.md`，把 §5.7 里这一段（当前）：

```
- Merges only `*_ENDPOINT`-class variables into the shell's shared,
  flat OS environment — never `COMPONENT_ID`/`COMPONENT_VERSION`, never a
  component's own `configSchema`-derived config, never resource-connection
  variables, and (see below) never `labels`. None of these are namespaced
  by component ID, so two independently-authored modules could easily
  reuse the same name; giving each module its own isolated configuration
  is the shell author's job, not the platform's. (A member's own merged
  config is still reachable, just not by flat-merging it — see
  `BRICKKIT_SERVED_MEMBERS_CONFIG` two bullets down.)
```

改成：

```
- Merges `*_ENDPOINT`-class variables into the shell's shared OS
  environment unprefixed (component IDs already make these names unique),
  and merges each member's own `configSchema`-derived config values in
  too — but always under a component-ID prefix
  (`{EnvPrefix(componentId)}_{name}`, the same prefix `*_ENDPOINT`
  variables use), so two independently-authored modules reusing the same
  config key can never collide. Never merges `COMPONENT_ID`/
  `COMPONENT_VERSION`, resource-connection variables, or (see below)
  `labels` on a member's behalf — those stay the shell's own, single set
  of platform/resource variables. `BRICKKIT_SERVED_MEMBERS_CONFIG` (two
  bullets down) is an index into the prefixed config variables, not a
  second copy of their values.
```

然后把这一段（当前）：

```
- Also writes `BRICKKIT_SERVED_MEMBERS_CONFIG` (a reserved variable): a JSON
  array, one element per member on that same list, each carrying
  `componentId`/`version`/`httpPort`/`extraPorts`/`config` (`config` is that
  member's own merged configuration, keyed by the original configSchema key,
  not the converted `*_ENDPOINT`-style variable name). This is the data a
  shell author actually needs to wire each module up — without it, the only
  option was hand-computing an equivalent JSON blob before every deployment
  and pasting it into a string field, which goes stale the moment a member's
  version, config, or `servedBy` membership changes and only surfaces as a
  crash-loop at the next real startup. Empty deployments still get `[]`, not
  a missing variable.
```

改成：

```
- Also writes `BRICKKIT_SERVED_MEMBERS_CONFIG` (a reserved variable): a JSON
  array, one element per member on that same list, each carrying
  `componentId`/`version`/`httpPort`/`extraPorts`/`configEnvVars`
  (`configEnvVars` maps that member's own original configSchema key to the
  name of the prefixed environment variable above where its actual value
  lives — not the value itself). This is the index a shell author actually
  needs to wire each module up — without it, the only option was
  hand-computing an equivalent JSON blob before every deployment and
  pasting it into a string field, which goes stale the moment a member's
  version, config, or `servedBy` membership changes and only surfaces as a
  crash-loop at the next real startup. Carrying only variable names (never
  values) is deliberate, not incidental: a member's config value is often a
  `${VAR}` reference to a secret that brickKit's Docker Compose generation
  deliberately leaves unresolved (so the generated file stays safe to open
  and diff) — embedding such a placeholder's eventual value inside a JSON
  string would let `docker compose`'s own blind, structure-unaware
  variable substitution corrupt that JSON the moment the value contains a
  quote, backslash, or newline. Empty deployments still get `[]`, not a
  missing variable.
```

- [ ] **Step 2: 改中文版**

打开 `AGENTS.zh.md`，把这一段（当前）：

```
- 只把 `*_ENDPOINT` 一类变量摊平合并进外壳共享的操作系统环境——绝不
  合并 `COMPONENT_ID` / `COMPONENT_VERSION`，绝不合并某个组件自己
  `configSchema` 生成的配置，绝不合并资源连接变量，（见下文）也绝不合并
  `labels`。这些东西本来就不按组件 ID 做命名空间隔离，两个各自独立开发
  的模块很容易撞同一个名字；给每个模块做好隔离是外壳作者自己的事，
  不是平台的事。（一个成员合并后的自身配置仍然拿得到，只是不走摊平合并
  这条路——见下面两条之后的 `BRICKKIT_SERVED_MEMBERS_CONFIG`。）
```

改成：

```
- 把 `*_ENDPOINT` 一类变量不带前缀地合并进外壳共享的操作系统环境
  （组件 ID 本身已经让这些名字唯一），也把每个成员自己 `configSchema`
  生成的配置合并进来——但一律带组件 ID 前缀
  （`{EnvPrefix(组件ID)}_{名字}`，跟 `*_ENDPOINT` 用同一个前缀），两个
  各自独立开发的模块就算用了同一个配置项名字也不可能撞车。绝不会替
  成员合并 `COMPONENT_ID` / `COMPONENT_VERSION`、资源连接变量，（见下文）
  也不会合并 `labels`——这些始终只是外壳自己那一套平台/资源变量，不按
  成员各来一份。`BRICKKIT_SERVED_MEMBERS_CONFIG`（下面两条之后）是这些
  带前缀变量的一份索引，不是它们值的第二份拷贝。
```

然后把这一段（当前）：

```
- 还会写入 `BRICKKIT_SERVED_MEMBERS_CONFIG`（另一个保留变量）：一个
  JSON 数组，跟上面那份名单里的每个成员一一对应，各自带上
  `componentId`/`version`/`httpPort`/`extraPorts`/`config`（`config` 是
  该成员合并后的自身配置，键是原始 configSchema key，不是转换后的
  `*_ENDPOINT` 风格变量名）。这才是外壳作者真正要装配每个模块所需的
  数据——没有它，唯一的办法是在每次部署前手算一份等价的 JSON、贴进某个
  字符串配置项，而这份手工数据只要成员的版本号、config 或 `servedBy`
  归属一变就会过期，平台不会提醒，只会在下一次真机启动时才表现成
  crash-loop。零个成员时是 `[]`，不是变量缺失。
```

改成：

```
- 还会写入 `BRICKKIT_SERVED_MEMBERS_CONFIG`（另一个保留变量）：一个
  JSON 数组，跟上面那份名单里的每个成员一一对应，各自带上
  `componentId`/`version`/`httpPort`/`extraPorts`/`configEnvVars`
  （`configEnvVars` 把该成员原始的 configSchema key 映射到上面那条带
  前缀变量的名字——不是值本身）。这是外壳作者真正要装配每个模块所需的
  索引——没有它，唯一的办法是在每次部署前手算一份等价的 JSON、贴进某个
  字符串配置项，而这份手工数据只要成员的版本号、config 或 `servedBy`
  归属一变就会过期，平台不会提醒，只会在下一次真机启动时才表现成
  crash-loop。只携带变量名、从不携带值是故意的：成员的 config 值经常是
  指向密钥的 `${VAR}` 引用，brickKit 生成 Docker Compose 文件时刻意不
  自己展开这类引用（好让生成的文件能安全地打开看、进 git diff）——把这
  类占位符最终会展开成的值塞进一段 JSON 字符串，一旦这个值带引号、反
  斜杠或换行符，就会被 docker compose 自己那套无结构感知的变量替换撑
  坏这段 JSON。零个成员时是 `[]`，不是变量缺失。
```

- [ ] **Step 3: 跑文档一致性检查**

```bash
python3 scripts/check-docs.py
python3 scripts/check-docs-bilingual.py
```

期望：两个都输出全部 ✅。

- [ ] **Step 4: 提交**

```bash
git add AGENTS.md AGENTS.zh.md
git commit -m "$(cat <<'EOF'
更新：AGENTS.md/AGENTS.zh.md 同步 BRICKKIT_SERVED_MEMBERS_CONFIG 新形状

跟 shell-implementers-guide 那次更新同一个内容：成员自己的 config 值
现在带组件 ID 前缀合并进外壳环境，JSON 里的 configEnvVars 字段只携带
变量名，不携带值——避免密钥类 ${VAR} 占位符被塞进 JSON 内部撑坏结构。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: 全仓库最终验证

**Files:** 无新改动，只验证。

- [ ] **Step 1: 全仓库构建**

```bash
go build ./...
```

期望：无输出，无错误。

- [ ] **Step 2: 全仓库测试**

```bash
go test ./...
```

期望：全部 `ok`，没有 `FAIL`。

- [ ] **Step 3: market-server 子模块构建与测试**（这次改动没有碰 market-server，但按既有惯例每次改完都确认一遍没有连带影响）

```bash
cd market-server && go build ./... && go test ./...
```

期望：全部 `ok`。

- [ ] **Step 4: `gofmt` 检查改动过的文件**

```bash
gofmt -l internal/shell/shell.go internal/shell/shell_test.go
```

期望：无输出（有输出说明需要 `gofmt -w` 对应文件，再重新跑一遍测试确认没有破坏什么）。

- [ ] **Step 5: 文档一致性检查（最后再跑一遍，确认前面几个任务的改动叠加起来仍然全绿）**

```bash
python3 scripts/check-docs.py
python3 scripts/check-docs-bilingual.py
```

期望：全部 ✅。

（这一步不需要单独提交——前面每个任务已经各自提交过，这里只是确认状态。）
