# 密钥流向修正、existingSecret 引用与两份配方 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让"密钥值写到哪"在 Docker / K8s 两种目标下都符合平台自己文档写的设计（compose 文件永远没有密钥；K8s 里密钥进 Secret、不进 Deployment）；新增 `existingSecret` 引用机制，让资源密码与组件配置密钥都能指向外部系统（Vault Secrets Operator / External Secrets Operator / Sealed Secrets）已经建好的 K8s Secret，平台从头到尾不接触密钥值；并补上两份缺失的文档配方（契约先行、对比两份环境配置），把被否决的想法记进拒绝清单。

**Architecture:** 四处改动、一个原则——**每个值只在最终需要它的那一处求值，密钥性质由声明给出**。① `config.ParseConfig` 不再提前展开 `components[].config` 与 `resources[].password` 的 `${VAR}`，让 compose / K8s / local-debug 三个渲染器各自决定；② `configSchema` 属性新增 `secret: true`，注入层把它标成敏感变量，K8s 渲染器为它生成 `Secret/<服务名>-config-secret` 并在 Deployment 里用 `secretKeyRef`，servedBy 成员的 Secret 仍归成员；③ 新增 `resources[].existingSecret`（资源层）与配置密钥的 `{ existingSecret, key }` 写法（配置层），两者都只引用一个外部系统已经建好的 Secret，平台不生成、不接触值，语义与 `serviceAccountName`（"运维已建好，平台只引用"）同构；④ 文档与拒绝清单。不新增任何 CLI 命令，只新增字段与写法。

**Tech Stack:** Go 1.22，`testify`，YAML（`gopkg.in/yaml.v3`），Python 3（`scripts/check-guide-output.py`），Markdown 双语文档。

**Spec:** `docs/superpowers/specs/2026-09-19-secret-flow-and-proposal-review-design.md`（含五条提案的逐条评判、实测数据、设计取舍与暂缓项、`existingSecret` 的完整设计 §3.2；先读它）

## Global Constraints

- 代码注释、提交信息、文档正文用中文；标识符用英文（跟周边一致）。
- **不新增 CLI 命令或参数**（`existingSecret` 是字段/写法，不是命令）；不改 `deploy.target` 的含义；不新增 `brickkit diff`（暂缓，见 Spec §5）。
- 新的 Manifest 字段 `secret` 可选、默认 `false`；新的 `brickkit.yaml` 字段 `resources[].existingSecret` 可选、默认空：已发布的 Manifest 与已有的 `brickkit.yaml` 都不受影响。
- 每个任务结束前：完整 `make lint` 与 `go test ./... -count=1` 都要绿，再单独提交。**判断是否通过要看退出码**，
  这台机器是 zsh，`${PIPESTATUS[0]}` 不存在，`make lint | grep` 会把失败看成成功。写成：
  `make lint > "$SCRATCH/lint.log" 2>&1; echo "exit=$?"`，再 `tail` 日志。
- 提交命令从 `git` 开头，**不带 `cd` 前缀**；提交信息末尾加一行 `Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>`。
- zsh 会把 `--include=*.go` 当通配符展开，一律写成 `--include='*.go'`；不要 `echo "======"`。
- 不碰仓库根目录未跟踪的 `改进计划.md`。若 `make lint` 的 `check-docs` 被它拦住，临时 `mv` 到 scratchpad、跑完再 `mv` 回来，别改它、别提交它。
- 文档：`docs/en` 与 `docs/zh` 各写一份（独立撰写，不是互译），但**教程里的 CLI 输出块必须逐字取自真实输出，且两边一致**；
  面向用户的文字不预设读者背景（先大白话讲是什么，再讲好处与代价，最后讲 BrickKit 怎么对待）；不引用、不链接 `docs/archive/`。
  `AGENTS.md` / `AGENTS.zh.md` 是写给 AI 的压缩版，不受"不预设背景"约束。
- 凡是关于"解析器 / 渲染器会怎样"的说法，**先跑再写**（这个仓库里曾凭读代码说错过一次解析器行为；本计划的 `existingSecret` 部分也已经用一个独立小程序核实过 `gopkg.in/yaml.v3` 把嵌套映射解到 `interface{}` 时产出的是 `map[string]interface{}`，不是 `map[interface{}]interface{}`）。
- 新增/挪动文档要同步的地方见 Task 5 Step 10 的清单；所有写死文档路径的地方（`tests/docfields/*_test.go`、`internal/**` 的提示文案、`scripts/*.py`）改完要 `git grep` 确认没有残留。

## File Structure

| 文件 | 职责 | 任务 |
| --- | --- | --- |
| `internal/config/parse.go` | `expandEnvNode` 跳过 `deferredRefs` 路径；删掉 `passwordEnvRefs` / `configEnvRefs` | 1 |
| `internal/config/config.go` | 删除 `ConfigFromEnv` / `PasswordFromEnv` 两个冗余字段；新增 `Resource.ExistingSecret` | 1、4 |
| `internal/cli/up_secrets.go` | 用 `isEnvRef` 代替两个标志；`warnConfigSecrets` 认 `secret: true` 与 `existingSecret` 形状；新增 `warnExistingSecretConfigIssues` | 1、3、4 |
| `internal/manifest/types.go` | `ConfigProperty.Secret` | 2 |
| `internal/inject/inject.go` | `Var.Owner`；`addConfig` 为 `secret: true` 的项设 `SecretKey`；`Var.ExistingSecretRef`；`resourceVars`/`addConfig` 认 `existingSecret` | 2、4 |
| `internal/k8s/secret.go`、`deployment.go`、`k8s.go` | `secretRef` / `configSecretName`；两类 Secret 分文件落盘；`envDoc` 用 `secretRef`；`secretRef`/`collectSecrets` 认 `ExistingSecretRef` | 2、4 |
| `internal/compose/compose.go` | `environmentOf` 跳过空值变量 | 4 |
| `internal/config/validate.go`、`internal/config/secretref.go`（新建） | `existingSecret`/`password` 互斥校验；`ExistingSecretRef` 形状识别 | 4 |
| `internal/cli/k8s_cluster.go` | `resourceFields`；`describeUsers`/`fieldUse` 加 `noun`；`k8sOnlyFields` 收 `resources[].existingSecret` | 4 |
| `docs/en\|zh/06-architecture/*`、`AGENTS*.md`、`docs/en\|zh/07-patterns/*` | 参考行、骨架、密钥专文 `10-secrets.md`、环境变量文档、外壳指南更正、错误码表 | 5 |
| `docs/en\|zh/03-guide/07-consuming-artifacts.md`、`scripts/check-guide-output.py` | 契约先行一节 + 3 个可确定性复现的场景 + `!set-version` 步骤 | 6 |
| `docs/en\|zh/07-patterns/05-deployment-selection-guide.md`、`AGENTS*.md` §4.1/§10、`docs/en\|zh/06-architecture/00-overview.md` | 双环境对比配方；拒绝清单 4 条（含 existingSecret 的 carve-out 措辞） | 7 |

---

## Task 1: `${VAR}` 引用原样留给渲染器（修缺口 A）

**背景（实测，见 Spec §2.1）：** `config.ParseConfig` 在解析时就把整份 `brickkit.yaml` 的 `${VAR}` 用 `os.LookupEnv` 展开。
变量在进程环境里时，`docker-compose.yaml` 里出现 `DATABASE_PASSWORD=pw-from-env` 与 `API_KEY=key-from-env` 明文；
变量只在 `.env` 里时却保留 `${...}`。同一份配置两种结果。渲染器本来就都会自己求值，所以这是"改回文档写的样子"。

**Files:**
- Modify: `internal/config/parse.go`（`ParseConfig` 的展开步骤；`expandEnvNode`；删除 `passwordEnvRefs`、`configEnvRefs`）
- Modify: `internal/config/config.go`（删除 `Component.ConfigFromEnv`、`Resource.PasswordFromEnv` 及各自的注释块；给 `Component.Config`、`Resource.Password` 补一句"值里的 `${VAR}` 解析时不展开"）
- Modify: `internal/cli/up_secrets.go`（新增 `isEnvRef`；两处判断改用它）
- Modify: `internal/config/edge_test.go`（替换 `TestPasswordFromEnvRecordsTheReference` 与 `TestPasswordFromEnvWhenVariableMissing`，约 466–530 行）
- Create: `internal/config/deferred_refs_test.go`
- Create: `internal/cli/secret_flow_test.go`

**Interfaces:**
- Produces（Task 2、3、4 依赖）：`config.Component.Config` 与 `config.Resource.Password` 里的 `${VAR}` 保持原文；
  `cli.isEnvRef(v any) bool`（值是字符串且含 `${`）；测试辅助 `secretFlowComp`、`secretFlowEntry`、`secretFlowResources`、`dockerSecretFlowProject(t)`（`internal/cli/secret_flow_test.go`）。

- [ ] **Step 1: 写失败测试（config 包）**

新建 `internal/config/deferred_refs_test.go`：

```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 密钥候选值里的 ${VAR} 必须原样进入 Config，即使变量此刻就在进程环境里。
//
// 提前展开的后果：变量在进程环境里（CI 里最常见）时，明文在解析阶段就进了配置结构，
// docker compose 渲染器想保留占位符也保留不了；变量在 .env 里时却不会——
// 同一份配置、两种结果，取决于变量放在哪。
func TestSecretBearingFieldsKeepTheirReferences(t *testing.T) {
	t.Setenv("PG_PASSWORD", "pw-from-process-env")
	t.Setenv("API_TOKEN", "token-from-process-env")
	t.Setenv("PG_HOST", "db.internal")

	c, err := ParseConfig([]byte(`project: demo
deploy:
  target: docker
components:
  - id: people/basic
    version: 1.0.0
    config:
      apiToken: ${API_TOKEN}
      region: eu-west-1
resources:
  - kind: database
    engine: postgresql
    id: main-db
    host: ${PG_HOST}
    port: 5432
    password: ${PG_PASSWORD}
    bindings:
      - componentId: people/basic
        database: people
`), "brickkit.yaml")
	require.NoError(t, err)

	assert.Equal(t, "${PG_PASSWORD}", c.Resources[0].Password, "密码里的引用留给渲染器求值")
	assert.Equal(t, "${API_TOKEN}", c.Components[0].Config["apiToken"], "config 里的引用留给渲染器求值")
	assert.Equal(t, "eu-west-1", c.Components[0].Config["region"], "不是引用的值照旧")
	assert.Equal(t, "db.internal", c.Resources[0].Host, "CLI 自己要用的字段照常在解析时展开")
}

// 路径里的 `*` 是数组下标通配：第二个组件、第二个资源同样适用。
func TestDeferredRefsApplyToEveryArrayItem(t *testing.T) {
	t.Setenv("SECOND_TOKEN", "must-not-appear")
	t.Setenv("SECOND_PASSWORD", "must-not-appear")

	c, err := ParseConfig([]byte(`project: demo
deploy:
  target: docker
components:
  - id: people/basic
    version: 1.0.0
  - id: people/other
    version: 1.0.0
    config:
      apiToken: ${SECOND_TOKEN}
resources:
  - kind: cache
    engine: redis
    id: first
    host: redis-a.example.com
    port: 6379
    bindings:
      - componentId: people/basic
  - kind: cache
    engine: redis
    id: second
    host: redis-b.example.com
    port: 6379
    password: ${SECOND_PASSWORD}
    bindings:
      - componentId: people/other
`), "brickkit.yaml")
	require.NoError(t, err)

	assert.Equal(t, "${SECOND_TOKEN}", c.Components[1].Config["apiToken"])
	assert.Equal(t, "${SECOND_PASSWORD}", c.Resources[1].Password)
}

// 变量根本没配时，同样保留占位符（渲染器负责报"哪个变量没定义"）。
func TestUnsetReferenceStaysAsWritten(t *testing.T) {
	c, err := ParseConfig([]byte(`project: demo
deploy:
  target: docker
components:
  - id: people/basic
    version: 1.0.0
resources:
  - kind: database
    engine: postgresql
    id: main-db
    host: db.example.com
    port: 5432
    password: ${PG_PASSWORD_NOT_SET}
    bindings:
      - componentId: people/basic
        database: people
`), "brickkit.yaml")
	require.NoError(t, err)

	assert.Equal(t, "${PG_PASSWORD_NOT_SET}", c.Resources[0].Password)
}
```

- [ ] **Step 2: 跑测试，确认因为"已被展开"而失败**

Run: `go test ./internal/config -run 'TestSecretBearingFieldsKeepTheirReferences|TestDeferredRefsApplyToEveryArrayItem|TestUnsetReferenceStaysAsWritten' -count=1`
Expected: 前两个 FAIL，报 `expected: "${PG_PASSWORD}"  actual: "pw-from-process-env"` 一类；第三个 PASS（变量没配时本来就保留）。

- [ ] **Step 3: 实现——`expandEnvNode` 跳过两处字段**

`internal/config/parse.go`：把 `ParseConfig` 里展开那一段

```go
	doc := root.Content[0]
	// 展开**前**先记下哪些密码写的是 ${ENV_VAR}：展开之后就分不出
	// "使用者写死了密码" 与 "使用者写了引用、而这个变量恰好有值" 了
	cfgRefs := configEnvRefs(doc)
	envRefs := passwordEnvRefs(doc)
	expandEnvNode(doc)
```

改成

```go
	doc := root.Content[0]
	expandEnvNode(doc, nil)
```

删掉紧随 `c.Source = source` 之后的两个循环（给 `PasswordFromEnv` / `ConfigFromEnv` 赋值的那两个 `for`），只留 `c.Source = source`。
删掉 `passwordEnvRefs`、`configEnvRefs` 两个函数（连同各自的注释）。
把 `ParseConfig` 文档注释里第 2 步改成 `${ENV_VAR} 展开（只作用于值，不动 key；deferredRefs 里的字段除外）`。
把 `expandEnvNode` 整个换成：

```go
// deferredRefs 是"${VAR} 引用要原样留给渲染器"的字段路径，`*` 匹配数组下标。
//
// 这两处装的都是要进部署清单的密钥候选值，而"什么时候求值"只有渲染器知道：
//
//	docker compose  文件里保留 ${VAR}，由 docker compose 启动时自己从进程环境与 .env 求值——
//	                那份文件因此永远可以放心打开、diff
//	K8s             生成时求值，密钥进 Secret（0600），绝不进 Deployment
//	local-debug     生成时求值，IDE 不做变量替换
//
// 在这里提前展开，变量在进程环境里（CI 里最常见）时明文就会被写进 docker-compose.yaml，
// 变量在 .env 里时却不会——同一份配置、两种结果，取决于变量放在哪。
//
// 其余字段（sources[].url / authToken、deploy.*、资源的 host 等）是 CLI 自己要用的值，仍在解析时展开。
var deferredRefs = [][]string{
	{"components", "*", "config"},
	{"resources", "*", "password"},
}

// isDeferredRef 判断一条字段路径是不是 deferredRefs 里的一项。
func isDeferredRef(path []string) bool {
	for _, want := range deferredRefs {
		if len(want) != len(path) {
			continue
		}
		matched := true
		for i := range want {
			if want[i] != "*" && want[i] != path[i] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// expandEnvNode 递归展开节点中的 ${ENV_VAR}，path 是当前节点的字段路径。
//
// 只展开**值**：映射的 key 不展开，避免环境变量影响配置结构。
func expandEnvNode(node *yaml.Node, path []string) {
	if isDeferredRef(path) {
		return
	}
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			child := append(append([]string(nil), path...), node.Content[i].Value)
			expandEnvNode(node.Content[i+1], child)
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			child := append(append([]string(nil), path...), "*")
			expandEnvNode(item, child)
		}
	case yaml.ScalarNode:
		if node.Tag == "!!str" {
			node.Value = ExpandEnv(node.Value)
		}
	}
}
```

`internal/config/config.go`：删除 `ConfigFromEnv` 与 `PasswordFromEnv` 两个字段以及各自上方的整段注释；
在 `Config map[string]any` 的注释末尾补：

```go
	//
	// 值里的 ${VAR} **解析时不展开**（见 deferredRefs）：写进 compose 文件还是进 Secret 是渲染器的事。
	// 想知道"这一项写的是不是引用"，看值本身就是原文。这个位置也允许写成 existingSecret 引用
	// 的形状（见 ExistingSecretRef，Task 4），同样不受这里的展开逻辑影响。
```

在 `Password` 字段注释末尾补一句 `值里的 ${VAR} 解析时不展开，理由同 Component.Config。`。改完跑 `gofmt -w internal/config`。

`internal/cli/up_secrets.go`：新增

```go
// isEnvRef 判断一个配置值写的是不是 ${ENV_VAR} 引用。
//
// config.deferredRefs 让这两处字段解析时不展开，所以值本身就是原文——
// 不再需要"展开前先记下"的补丁字段。
func isEnvRef(v any) bool {
	s, ok := v.(string)
	return ok && strings.Contains(s, "${")
}
```

`warnHardcodedPasswords` 里 `if r.PasswordFromEnv || strings.TrimSpace(r.Password) == "" {` 改成
`if isEnvRef(r.Password) || strings.TrimSpace(r.Password) == "" {`；
`warnConfigSecrets` 里把 `for name := range c.Config {` 与随后那条 `if c.ConfigFromEnv[name] || !secretishKey(name) {` 改成

```go
		for name, value := range c.Config {
			// 写成 ${ENV_VAR} 就是做对了。解析时这处不展开（config.deferredRefs），
			// 所以值本身就是原文
			if isEnvRef(value) || !secretishKey(name) {
				continue
			}
```

（Task 3、4 还会再改这个循环一次，加上 `existingSecret` 形状与 `declaredSecretKeys` 的判断——这里先只处理 Task 1 的范围。）

- [ ] **Step 4: 修旧测试并跑 config 与 cli 包**

`internal/config/edge_test.go` 里 `TestPasswordFromEnvRecordsTheReference`（断言 `Password == "resolved-value"` 与两个 `PasswordFromEnv`）与
`TestPasswordFromEnvWhenVariableMissing` 整个替换成一个：

```go
// 写了引用的密码与写死的密码，解析后仍然分得出来：引用保留原文，写死的就是写死的。
// 008 要求的"密码不写进 brickkit.yaml"因此仍然查得出来。
func TestPasswordReferenceIsDistinguishableFromLiteral(t *testing.T) {
	t.Setenv("PG_PASSWORD", "resolved-value")

	c, err := ParseConfig([]byte(`project: demo
deploy:
  target: docker
components:
  - id: people/basic
    version: 1.0.0
resources:
  - kind: database
    engine: postgresql
    id: from-env
    host: db.example.com
    port: 5432
    password: ${PG_PASSWORD}
    bindings:
      - componentId: people/basic
        database: people
  - kind: cache
    engine: redis
    id: hardcoded
    host: redis.example.com
    port: 6379
    password: written-in-plain-text
    bindings:
      - componentId: people/basic
`), "brickkit.yaml")

	require.NoError(t, err)
	require.Len(t, c.Resources, 2)
	assert.Equal(t, "${PG_PASSWORD}", c.Resources[0].Password, "引用保留原文")
	assert.Equal(t, "written-in-plain-text", c.Resources[1].Password)
}
```

Run: `go build ./... && go test ./internal/config/... ./internal/cli/... -count=1`
Expected: PASS（`git grep -n 'PasswordFromEnv\|ConfigFromEnv' -- 'internal/*.go'` 应无结果；`tests/components/*/…_test.go` 里的 `TestConfigFromEnv` 是另一回事，不用管）。
若 `internal/cli` 里有别的用例依赖"进程环境里有值就展开进 compose"，它们现在应该改成断言占位符——逐个读、按新语义改，别放宽断言。

- [ ] **Step 5: 写端到端测试（Docker 与 K8s 两个目标）**

新建 `internal/cli/secret_flow_test.go`：

```go
package cli

// 本文件钉住"密钥值写到哪"（Spec 2026-09-19 §3.1）：
//
//	Docker  compose 文件里永远没有密钥，占位符留给 docker compose 启动时求值
//	K8s     生成时求值，资源密码进 Secret、Deployment 里只有 secretKeyRef
//
// 以前变量在**进程环境**里（CI 里最常见）时，compose 文件里会出现明文，
// 变量在 .env 里时却不会——同一份配置、两种结果。
//
// Task 4 会在这份文件里继续加 existingSecret 的端到端用例。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// secretFlowComp 是带一个配置项、一个数据库依赖的组件。
var secretFlowComp = comp{
	ID: "demo/hello", Version: "1.0.0",
	ConfigSchema: []string{"apiToken:"},
	ResourceDeps: []string{"database:postgresql"},
}

const secretFlowEntry = `    config:
      apiToken: ${HELLO_TOKEN}
`

const secretFlowResources = `
resources:
  - kind: database
    engine: postgresql
    id: main-db
    host: db.example.internal
    port: 5432
    username: app
    password: ${HELLO_DB_PASSWORD}
    bindings:
      - componentId: demo/hello
        database: hello
`

// dockerSecretFlowProject 造一个 deploy.target: docker 的项目（写法照 k8sProjectWith）。
func dockerSecretFlowProject(t *testing.T) *projectFixture {
	t.Helper()

	f := addedProject(t, []comp{secretFlowComp}, secretFlowComp.ref())

	var b strings.Builder
	b.WriteString(configHeader)
	b.WriteString("\nsources:\n")
	for _, s := range f.Sources {
		b.WriteString(s)
	}
	b.WriteString("\ncomponents:\n  - id: demo/hello\n    version: 1.0.0\n")
	b.WriteString(secretFlowEntry)
	b.WriteString(secretFlowResources)
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(), []byte(b.String()), 0o644))
	return f
}

// 变量在进程环境里时，compose 文件里也不能出现明文。
func TestComposeNeverHoldsSecretValuesEvenWhenProcessEnvHasThem(t *testing.T) {
	t.Setenv("HELLO_TOKEN", "sk-live-TOKEN-VALUE")
	t.Setenv("HELLO_DB_PASSWORD", "pw-DB-VALUE")
	f := dockerSecretFlowProject(t)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)

	raw, err := os.ReadFile(filepath.Join(f.Layout.GeneratedDir(), composeFileName))
	require.NoError(t, err)
	text := string(raw)

	assert.NotContains(t, text, "sk-live-TOKEN-VALUE", "compose 文件会被人打开看、进 CI 产物，明文进去就是泄露")
	assert.NotContains(t, text, "pw-DB-VALUE")
	assert.Contains(t, text, "${HELLO_TOKEN}", "占位符留给 docker compose 启动时求值")
	assert.Contains(t, text, "${HELLO_DB_PASSWORD}")
}

// K8s 目标：变量在进程环境里，生成时求值——资源密码进 Secret，Deployment 里没有它。
func TestK8sResourcePasswordStaysInSecretNotDeployment(t *testing.T) {
	t.Setenv("HELLO_TOKEN", "sk-live-TOKEN-VALUE")
	t.Setenv("HELLO_DB_PASSWORD", "pw-DB-VALUE")
	f := k8sProjectWith(t, secretFlowComp, secretFlowEntry, secretFlowResources)

	r := runWithEngine(t, newK8sEngine(), f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)

	secrets, err := os.ReadFile(filepath.Join(k8sDir(f), "secrets", "resource-secrets.yaml"))
	require.NoError(t, err)
	deployment, err := os.ReadFile(filepath.Join(k8sDir(f), "deployments", "demo-hello-1-0-0.yaml"))
	require.NoError(t, err)

	assert.Contains(t, string(secrets), "pw-DB-VALUE", "K8s 没有变量替换，Secret 必须在生成时求值")
	assert.NotContains(t, string(deployment), "pw-DB-VALUE")
}
```

Run: `go test ./internal/cli -run 'TestComposeNeverHoldsSecretValuesEvenWhenProcessEnvHasThem|TestK8sResourcePasswordStaysInSecretNotDeployment' -count=1 -v`
Expected: 两个都 PASS（Step 3 已实现）。**为确认第一个测试真的守着这件事**：临时把 `parse.go` 里 `deferredRefs` 清空再跑，
应看到它 FAIL 并报 `Contains ... "sk-live-TOKEN-VALUE"`，再还原。

- [ ] **Step 6: 全量验证并提交**

Run: `go test ./... -count=1` 与 `make lint > "$SCRATCH/lint.log" 2>&1; echo "exit=$?"`
Expected: 全绿、`exit=0`。

```bash
git add internal/config internal/cli
git commit -m "$(cat <<'EOF'
config：密钥候选值里的 ${VAR} 不再在解析时提前展开

components[].config 与 resources[].password 里的 ${VAR} 原样进入 Config，
由三个渲染器各自求值（compose 交给 docker、K8s 生成时求值、local-debug 生成时求值）。

以前解析时就用进程环境展开：变量在进程环境里（CI 里最常见）时，明文会被写进
docker-compose.yaml，变量在 .env 里时却不会——同一份配置两种结果，
与 compose.go 里"compose 文件刻意保留占位符"的设计直接矛盾。

同时删除因此失去存在理由的 ConfigFromEnv / PasswordFromEnv 两个补丁字段。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: 配置项 `secret: true`——K8s 下进 Secret、不进 Deployment（修缺口 B）

**背景（实测）：** 只有资源连接变量带敏感标记；组件自己的配置项没有"敏感"这个概念，
`config: { apiKey: ${THIRD_PARTY_KEY} }` 在 K8s 上会以 `env: - name: API_KEY value: sk-live-…` 明文写进 Deployment。
CLI 自己的警告文案（`up_secrets.go`）建议的写法恰恰是这个。

**Files:**
- Modify: `internal/manifest/types.go`（`ConfigProperty.Secret`）
- Modify: `internal/inject/inject.go`（`Var.Owner`；`envBuilder.service`；`addConfig`）
- Modify: `internal/k8s/secret.go`（`configSecretName`、`secretRef`、`secretPlan.Config`、`collectSecrets`、`secretDocs(config bool)`）
- Modify: `internal/k8s/deployment.go`（`envDoc` 用 `secretRef`）
- Modify: `internal/k8s/k8s.go`（`Generate` 里两类 Secret 分文件 emit）
- Modify: `internal/manifest/lint_test.go`、`internal/inject/inject_test.go`、`internal/k8s/k8s_test.go`、`internal/k8s/servedby_test.go`、`internal/cli/testsupport_component_test.go`、`internal/cli/secret_flow_test.go`

**Interfaces:**
- Consumes：Task 1 的 `secretFlowComp`、`secretFlowEntry`、`secretFlowResources`（`internal/cli/secret_flow_test.go`）。
- Produces（Task 3、4 依赖）：
  `manifest.ConfigProperty.Secret bool`（yaml `secret`）；
  `inject.Var.Owner string`——所有 `SourceConfig` / `SourceOverride` 变量都带，值是该组件的版本化服务名（如 `people-basic-1-0-0`），servedBy 改名时原样带着；
  声明了 `secret: true` 的配置类变量 `SecretKey` = 它自己的变量名（如 `API_KEY`）、`IsSecret()` 为 true；
  `k8s.secretRef(v inject.Var) (name, key string)`（Task 4 会在它内部再插入一条 `ExistingSecretRef` 分支）；Secret 文件 `secrets/config-secrets.yaml`；
  cli 测试夹具 `comp.SecretConfig []string`。

- [ ] **Step 1: 写失败测试——manifest 认识 `secret`**

`internal/manifest/lint_test.go` 的 `TestPropertyKeyWarningsQuietForKnownKeys` 里，在 `hosts:` 属性之后加一个属性：

```go
    apiKey:
      type: string
      secret: true
```

再在 `internal/manifest/manifest_test.go` 末尾加：

```go
// secret: true 是说明书上的一栏：被解析、存下来，不校验任何值。
func TestConfigPropertySecretIsParsed(t *testing.T) {
	m, err := Parse([]byte(minimalYAML+`
configSchema:
  type: object
  properties:
    apiKey:
      type: string
      secret: true
    region:
      type: string
`), "component.yaml")

	require.NoError(t, err)
	assert.True(t, m.ConfigSchema.Properties["apiKey"].Secret)
	assert.False(t, m.ConfigSchema.Properties["region"].Secret, "不写就是 false")
}
```

（若 `manifest_test.go` 没有 `assert` / `require` 的导入，按文件已有的导入补齐。）

Run: `go test ./internal/manifest -run 'TestPropertyKeyWarningsQuietForKnownKeys|TestConfigPropertySecretIsParsed' -count=1`
Expected: FAIL（编译错误 `Secret undefined`，或 lint 报 `secret` 是未知键）。

- [ ] **Step 2: 加字段**

`internal/manifest/types.go` 的 `ConfigProperty` 末尾（`Pattern` 之后）加：

```go
	// Secret 声明这一项的值是凭据（API 密钥、令牌……）。
	//
	// 它和 Enum、Pattern 一样是说明书上的一栏：平台不用它校验任何值、不拒绝任何输入
	// （AGENTS.md §9.12）。不同的是它决定**值写到哪里**——K8s 目标下这一项进平台生成的
	// Secret，Deployment 里只留 secretKeyRef，而不是把值明文写进 env。
	//
	// 是不是凭据只有组件作者最清楚，所以由 Manifest 声明，平台不按名字去猜
	// （名字启发式只配拿来发警告，见 internal/cli/up_secrets.go）。
	Secret bool `yaml:"secret,omitempty"`
```

Run: `go test ./internal/manifest -count=1` → PASS。

- [ ] **Step 3: 写失败测试——注入层标记敏感变量**

`internal/inject/inject_test.go` 在 `TestConfigVarRecordsOriginalKeyForDefault` 之后加：

```go
// configSchema 里声明了 secret: true 的配置项，注入结果里要标成敏感变量，
// 并记着它属于哪个组件（K8s 据此起 Secret 名）。
func TestSecretConfigVarIsMarkedSensitive(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"apiKey": {Type: "string", Secret: true},
		"region": {Type: "string", Default: "eu-west-1"},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{Config: map[string]any{"apiKey": "${THIRD_PARTY_KEY}"}})
	result := b.build()

	key := varOf(t, result, "people/basic", "API_KEY")
	assert.True(t, key.IsSecret(), "声明了 secret: true")
	assert.Equal(t, "API_KEY", key.SecretKey, "变量名就是它在 Secret 里的 key")
	assert.Equal(t, "people-basic-1-0-0", key.Owner)
	assert.Equal(t, "${THIRD_PARTY_KEY}", key.Value, "值原样保留，求值是渲染器的事")

	region := varOf(t, result, "people/basic", "REGION")
	assert.False(t, region.IsSecret(), "没声明 secret 的照常明文")
	assert.Equal(t, "people-basic-1-0-0", region.Owner, "所有配置类变量都带 Owner")
}

// 非配置来源的变量没有 Owner——它们不属于任何"某个组件的配置项"。
func TestNonConfigVarsHaveNoOwner(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	assert.Empty(t, varOf(t, b.build(), "people/basic", "COMPONENT_ID").Owner)
}
```

Run: `go test ./internal/inject -run 'TestSecretConfigVarIsMarkedSensitive|TestNonConfigVarsHaveNoOwner' -count=1`
Expected: FAIL（编译错误 `Owner undefined`）。

- [ ] **Step 4: 实现注入层**

`internal/inject/inject.go`：

`Var` 结构体末尾（`Key` 之后）加：

```go
	// Owner 是配置类变量（SourceConfig / SourceOverride）来自哪个组件，写成它的版本化服务名。
	// K8s 据此给声明了 secret: true 的配置项起 Secret 名。servedBy 合并把成员变量改名
	// （加组件 ID 前缀）时，Owner 原样带着——所以 Secret 仍归成员，外壳的 Deployment 只是引用它。
	Owner string
```

`envBuilder` 加字段 `service string`；`buildComponent` 里构造 builder 时补 `service: manifest.ServiceName(node.Ref.ID, node.Ref.Version),`。
`addConfig` 里把最后的 `b.set(Var{Name: name, Value: formatValue(value), Source: source, Key: key})` 换成：

```go
		v := Var{Name: name, Value: formatValue(value), Source: source, Key: key, Owner: b.service}
		if property.Secret {
			// 值是凭据：变量名本身就是它在 Secret 里的 key（K8s 渲染器据此引用）
			v.SecretKey = name
		}
		b.set(v)
```

同时把 `Var.SecretKey` 的注释里"只有资源连接变量有值"的隐含前提改掉——在 `SecretKey` 注释末尾补一句：
`声明了 secret: true 的配置项也带（值是它自己的变量名），此时 Owner 指明 Secret 归哪个组件。`

Run: `go test ./internal/inject -count=1` → PASS。

- [ ] **Step 5: 写失败测试——K8s 渲染**

`internal/k8s/k8s_test.go` 在 `TestNoSecretFileWithoutSensitiveVars` 之后加：

```go
// ============================================================
// 声明了 secret: true 的配置项（Spec 2026-09-19 §3.1）
// ============================================================

func secretConfigManifest() *manifest.Manifest {
	m := simple("acme/hello", "0.1.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"apiKey": {Type: "string", Secret: true},
		"region": {Type: "string"},
	}}
	return m
}

// 值进 Secret，Deployment 里只留引用：Secret 与 Deployment 在 K8s 里是分开授权的，
// 密钥明文写进 Deployment，能读 Deployment 的人就都读得到。
func TestSecretConfigGoesThroughSecret(t *testing.T) {
	b := newBuilder(t)
	b.component(secretConfigManifest(), config.Component{Config: map[string]any{
		"apiKey": "${THIRD_PARTY_KEY}", "region": "eu-west-1",
	}})
	b.env["THIRD_PARTY_KEY"] = "sk-live-SECRET123"

	doc := b.doc("secrets/config-secrets.yaml")
	env := envOf(t, b.container("acme-hello-0-1-0"))
	deployment := string(b.file("deployments/acme-hello-0-1-0.yaml").YAML)

	assert.Equal(t, "acme-hello-0-1-0-config-secret", dig(t, doc, "metadata", "name"),
		"<版本化服务名>-config-secret，与资源 Secret（<资源ID>-secret）永不撞名")
	assert.Equal(t, "sk-live-SECRET123", dig(t, doc, "stringData", "API_KEY"),
		"${VAR} 在生成时求值——kubectl 不做变量替换")
	assert.Equal(t, map[string]any{"secretKeyRef": map[string]any{
		"name": "acme-hello-0-1-0-config-secret", "key": "API_KEY",
	}}, env["API_KEY"])
	assert.Equal(t, "eu-west-1", env["REGION"], "没声明 secret 的配置项照常明文")
	assert.NotContains(t, deployment, "sk-live-SECRET123", "密钥绝不能出现在 Deployment 里")
}

// 没有声明 secret 的配置项时不生成 config-secrets 文件（空文件只会让人以为漏了什么）。
func TestNoConfigSecretFileWithoutDeclaredSecrets(t *testing.T) {
	b := newBuilder(t)
	m := simple("acme/hello", "0.1.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"region": {Type: "string"},
	}}
	b.component(m, config.Component{Config: map[string]any{"region": "eu-west-1"}})

	assert.False(t, hasFile(b.generate(), "secrets/config-secrets.yaml"))
}

// 值文件权限同样是 0600：Secret 目录整个是。
func TestConfigSecretFileIsNotWorldReadable(t *testing.T) {
	b := newBuilder(t)
	b.component(secretConfigManifest(), config.Component{Config: map[string]any{"apiKey": "${THIRD_PARTY_KEY}"}})
	b.env["THIRD_PARTY_KEY"] = "sk-live-SECRET123"

	dir := t.TempDir()
	require.NoError(t, k8s.WriteFiles(dir, b.generate().Files))

	info, err := os.Stat(filepath.Join(dir, "secrets", "config-secrets.yaml"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// 变量没定义要报错，与资源密码同一条规则：放过去就是把字面量 "${THIRD_PARTY_KEY}" 当密钥部署上去。
func TestUnresolvedSecretConfigIsAnError(t *testing.T) {
	b := newBuilder(t)
	b.component(secretConfigManifest(), config.Component{Config: map[string]any{"apiKey": "${THIRD_PARTY_KEY}"}})

	_, err := b.build()

	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)
	assert.Contains(t, err.Error(), "THIRD_PARTY_KEY")
}
```

`internal/k8s/k8s_test.go` 顶部导入按需补 `os`、`path/filepath`（`require`、`clierr`、`k8s`、`manifest`、`config` 该文件已用）。

`internal/k8s/servedby_test.go` 末尾加（并在导入里补 `"github.com/brickkit/brickkit/internal/manifest"`）：

```go
// servedBy 成员声明了 secret: true 的配置项：Secret 仍归成员，
// 外壳的 Deployment 里带前缀的那个变量是指向成员 Secret 的 secretKeyRef。
func TestServedByMemberSecretConfigStaysWithMemberSecret(t *testing.T) {
	member := simple("mdm/customer", "1.0.7", 8080)
	member.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"apiKey": {Type: "string", Secret: true},
	}}
	entry := servedByEntry("infra/shell-go-core", "1.0.0")
	entry.Config = map[string]any{"apiKey": "${CUSTOMER_KEY}"}

	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(member, entry)
	b.env["CUSTOMER_KEY"] = "sk-customer"

	env := envOf(t, b.container("infra-shell-go-core-1-0-0"))
	secret := b.doc("secrets/config-secrets.yaml")
	deployment := string(b.file("deployments/infra-shell-go-core-1-0-0.yaml").YAML)

	assert.Equal(t, map[string]any{"secretKeyRef": map[string]any{
		"name": "mdm-customer-1-0-7-config-secret", "key": "API_KEY",
	}}, env["MDM_CUSTOMER_API_KEY"], "外壳里带前缀的变量指向成员自己的 Secret")
	assert.Equal(t, "mdm-customer-1-0-7-config-secret", dig(t, secret, "metadata", "name"))
	assert.Equal(t, "sk-customer", dig(t, secret, "stringData", "API_KEY"))
	assert.NotContains(t, deployment, "sk-customer")
}
```

Run: `go test ./internal/k8s -run 'TestSecretConfigGoesThroughSecret|TestNoConfigSecretFileWithoutDeclaredSecrets|TestConfigSecretFileIsNotWorldReadable|TestUnresolvedSecretConfigIsAnError|TestServedByMemberSecretConfigStaysWithMemberSecret' -count=1`
Expected: FAIL（`secrets/config-secrets.yaml` 找不到；明文出现在 Deployment 里）。

- [ ] **Step 6: 实现 K8s 渲染**

`internal/k8s/secret.go`：导入补 `"github.com/brickkit/brickkit/internal/inject"`。

`secretPlan` 加字段：

```go
	// Config 为 true 表示这是组件配置项的 Secret（configSchema 的 secret: true），
	// 否则是资源连接的 Secret。两类分文件落盘，文件名一眼看出归属。
	Config bool
```

在 `secretName` 之后加：

```go
// configSecretName 是某个组件的配置类 Secret 名：<版本化服务名>-config-secret。
//
// 与资源 Secret 分开命名：资源 ID 是使用者起的，可能恰好等于某个服务名；
// 多一个 -config- 才保证两类 Secret 永远不会撞名。
func configSecretName(service string) string { return sanitizeName(service) + "-config-secret" }

// secretRef 返回一条敏感变量在 K8s Secret 里的位置：Secret 名 + key。
//
// Task 4 会在这里插入第一条分支：ExistingSecretRef 非空时直接引用那个名字，
// 不生成任何平台自己的 Secret。
func secretRef(v inject.Var) (name, key string) {
	if v.Source == inject.SourceResource {
		return secretName(v.ResourceID), v.SecretKey
	}
	return configSecretName(v.Owner), v.SecretKey
}
```

`collectSecrets` 里把前半段（`byName` 到 `p.secrets = append(...)` 与排序）换成：

```go
	type slot struct {
		config bool
		data   map[string]string
	}
	byName := map[string]*slot{}

	for _, c := range p.components {
		for _, v := range c.Env.Env {
			if !v.IsSecret() {
				continue
			}
			name, key := secretRef(v)
			s := byName[name]
			if s == nil {
				s = &slot{config: v.Source != inject.SourceResource, data: map[string]string{}}
				byName[name] = s
			}
			s.data[key] = p.expand.value(v.Value)
		}
	}

	for name, s := range byName {
		p.secrets = append(p.secrets, secretPlan{Name: name, Data: s.data, Config: s.config})
	}
	sort.Slice(p.secrets, func(i, j int) bool { return p.secrets[i].Name < p.secrets[j].Name })
```

（保留其后"明文变量里的 `${VAR}` 同样要求值"那一段与 `return p.expand.check()` 不动。Task 4 会在这个循环的 `if !v.IsSecret() {` 那一行加一个"引用外部 Secret 的变量跳过"的条件。）

`secretDocs` 改签名并过滤：

```go
// secretDocs 渲染一类 Secret：config 为 true 是组件配置项的，否则是资源连接的。
func (p *plan) secretDocs(config bool) []map[string]any {
	out := make([]map[string]any, 0, len(p.secrets))
	for _, s := range p.secrets {
		if s.Config != config {
			continue
		}
		// ……其余循环体不变
```

`internal/k8s/deployment.go` 的 `envDoc` 里把

```go
					"secretKeyRef": map[string]any{
						"name": secretName(v.ResourceID),
						"key":  v.SecretKey,
					},
```

所在的 `if v.IsSecret() {` 分支改成：

```go
		if v.IsSecret() {
			name, key := secretRef(v)
			out = append(out, map[string]any{
				"name": v.Name,
				"valueFrom": map[string]any{
					"secretKeyRef": map[string]any{"name": name, "key": key},
				},
			})
			continue
		}
```

`internal/k8s/k8s.go` 的 `Generate` 里把

```go
	if docs := p.secretDocs(); len(docs) > 0 {
		if err := p.emitAll(result, cfg, now,
			dirSecrets+"/resource-secrets.yaml", docs); err != nil {
			return nil, err
		}
	}
```

换成

```go
	for _, group := range []struct {
		file   string
		config bool
	}{
		{"resource-secrets.yaml", false},
		{"config-secrets.yaml", true},
	} {
		if docs := p.secretDocs(group.config); len(docs) > 0 {
			if err := p.emitAll(result, cfg, now, dirSecrets+"/"+group.file, docs); err != nil {
				return nil, err
			}
		}
	}
```

Run: `go test ./internal/k8s ./internal/inject ./internal/shell -count=1` → PASS。
再确认 `Desired`（孤儿清理）会带上新 Secret：`git grep -n 'Desired' internal/k8s/*.go` 看 `emitAll` 是否统一登记；
若有专门的"期望清单"测试（`desired_test.go`），加一个断言：声明了 `secret: true` 时 `Desired` 含 `secret/acme-hello-0-1-0-config-secret`。

- [ ] **Step 7: 端到端（cli）与夹具**

`internal/cli/testsupport_component_test.go`：`comp` 结构体加

```go
	// SecretConfig 是 ConfigSchema 里声明了 secret: true 的键名。
	SecretConfig []string
```

渲染 `configSchema` 的循环（约 96–102 行）里，`fmt.Fprintf(&b, "    %s:\n      type: string\n      default: \"%s\"\n", name, def)` 之后加：

```go
			if slices.Contains(c.SecretConfig, name) {
				b.WriteString("      secret: true\n")
			}
```

导入补 `"slices"`。

`internal/cli/secret_flow_test.go` 末尾加：

```go
// 组件声明了 secret: true：K8s 下配置密钥与资源密码一样进 Secret，Deployment 里两者都没有。
func TestK8sConfigSecretGoesToSecretNotDeployment(t *testing.T) {
	t.Setenv("HELLO_TOKEN", "sk-live-TOKEN-VALUE")
	t.Setenv("HELLO_DB_PASSWORD", "pw-DB-VALUE")
	declared := secretFlowComp
	declared.SecretConfig = []string{"apiToken"}
	f := k8sProjectWith(t, declared, secretFlowEntry, secretFlowResources)

	r := runWithEngine(t, newK8sEngine(), f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)

	secrets, err := os.ReadFile(filepath.Join(k8sDir(f), "secrets", "config-secrets.yaml"))
	require.NoError(t, err)
	deployment, err := os.ReadFile(filepath.Join(k8sDir(f), "deployments", "demo-hello-1-0-0.yaml"))
	require.NoError(t, err)

	assert.Contains(t, string(secrets), "sk-live-TOKEN-VALUE")
	assert.NotContains(t, string(deployment), "sk-live-TOKEN-VALUE", "配置密钥不再明文进 Deployment")
	assert.NotContains(t, string(deployment), "pw-DB-VALUE")
}
```

Run: `go test ./internal/cli -run 'TestK8sConfigSecretGoesToSecretNotDeployment' -count=1` → PASS。

- [ ] **Step 8: 文档守卫会要求补参考文档——先看它报什么**

Run: `go test ./tests/docfields/... -count=1`
Expected: FAIL，指出 `configSchema.properties.<key>.secret` 在 component.yaml 参考文档（中英）里缺失。**这是预期**，Task 5 补；
本任务先不提交文档，所以到 Step 9 之前先把参考文档那两行加上（内容见 Task 5 Step 1），让守卫变绿，再提交。

- [ ] **Step 9: 全量验证并提交**

Run: `go test ./... -count=1` 与 `make lint > "$SCRATCH/lint.log" 2>&1; echo "exit=$?"`
Expected: 全绿、`exit=0`。

```bash
git add internal docs
git commit -m "$(cat <<'EOF'
configSchema 新增 secret: true：K8s 下配置密钥进 Secret，不再明文进 Deployment

以前只有资源连接变量带敏感标记，组件自己的配置项没有"敏感"这个概念：
config: { apiKey: ${KEY} } 在 K8s 上会以 env value 明文写进 Deployment，
而 CLI 自己的警告文案建议的写法恰恰是这个。

声明了 secret: true 的配置项：Secret/<版本化服务名>-config-secret（secrets/config-secrets.yaml，0600），
Deployment 里是 secretKeyRef。servedBy 成员的 Secret 仍归成员，外壳里带前缀的变量引用它。
secret 不校验任何值，只决定值写到哪（§9.12 不变）。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: 明文密钥告警认 `secret: true`

**Files:**
- Modify: `internal/cli/up_secrets.go`（`warnConfigSecrets` 加 `graph` 参数；新增 `declaredSecretKeys`）
- Modify: `internal/cli/up.go`（调用处传 `plan.graph`，约第 210 行 `warnConfigSecrets(opts, cfg)`）
- Modify: `internal/cli/config_secret_test.go`

**Interfaces:**
- Consumes：Task 2 的 `manifest.ConfigProperty.Secret`、`comp.SecretConfig`。
- Produces（Task 4 依赖）：`declaredSecretKeys(graph *resolver.Graph, c config.Component) map[string]bool`；
  `warnConfigSecrets(opts *Options, cfg *config.Config, graph *resolver.Graph)`。

- [ ] **Step 1: 写失败测试**

`internal/cli/config_secret_test.go` 末尾加：

```go
// 组件亲口声明了 secret: true 的配置项，不必名字长得像密钥也要警告：
// 名字启发式宁可漏报，而组件作者的声明比它准得多。
func TestDeclaredSecretConfigWarnsRegardlessOfName(t *testing.T) {
	f := addedProject(t,
		[]comp{{ID: "demo/hello", Version: "1.0.0",
			ConfigSchema: []string{"webhookUrl:"}, SecretConfig: []string{"webhookUrl"}}},
		"demo/hello@1.0.0")
	body := configHeader + `
components:
  - id: demo/hello
    version: 1.0.0
    config:
      webhookUrl: "https://hooks.example.com/services/T000/B000/XXXX"
`
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(), []byte(body), 0o644))

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, "是警告不是错误：%s", r.stderr)
	out := r.stdout + r.stderr
	assert.Contains(t, out, "webhookUrl", "要点名是哪个配置项：%s", out)
	assert.NotContains(t, out, "hooks.example.com", "绝不能把值本身打出来")
}

// 声明了 secret: true 又写成 ${VAR} 引用，就是做对了，不警告。
func TestDeclaredSecretConfigWithReferenceDoesNotWarn(t *testing.T) {
	f := addedProject(t,
		[]comp{{ID: "demo/hello", Version: "1.0.0",
			ConfigSchema: []string{"webhookUrl:"}, SecretConfig: []string{"webhookUrl"}}},
		"demo/hello@1.0.0")
	body := configHeader + `
components:
  - id: demo/hello
    version: 1.0.0
    config:
      webhookUrl: ${HELLO_WEBHOOK}
`
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(), []byte(body), 0o644))

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.NotContains(t, r.stdout+r.stderr, "明文密钥")
}
```

Run: `go test ./internal/cli -run 'TestDeclaredSecretConfig' -count=1 -v`
Expected: 第一个 FAIL（没有警告——`webhookUrl` 名字不像密钥），第二个 PASS。

- [ ] **Step 2: 实现**

`internal/cli/up_secrets.go`：导入补 `"github.com/brickkit/brickkit/internal/resolver"`。
新增：

```go
// declaredSecretKeys 返回组件在 configSchema 里声明了 secret: true 的配置项。
//
// 名字启发式（secretishKey）宁可漏报也不误报，只配拿来发警告；
// 组件作者亲口声明的事实比它准得多，所以两者取并集。
// graph 里没有这个组件（还没 add、Manifest 读不到）时返回 nil，退回按名字判断。
func declaredSecretKeys(graph *resolver.Graph, c config.Component) map[string]bool {
	if graph == nil {
		return nil
	}
	node := graph.Node(resolver.Ref{ID: c.ID, Version: c.Version})
	if node == nil || node.Manifest == nil || node.Manifest.ConfigSchema == nil {
		return nil
	}
	out := map[string]bool{}
	for key, property := range node.Manifest.ConfigSchema.Properties {
		if property.Secret {
			out[key] = true
		}
	}
	return out
}
```

`warnConfigSecrets` 签名改为 `(opts *Options, cfg *config.Config, graph *resolver.Graph)`，循环改成：

```go
	for _, c := range cfg.Components {
		declared := declaredSecretKeys(graph, c)
		for name, value := range c.Config {
			// 写成 ${ENV_VAR} 就是做对了。解析时这处不展开（config.deferredRefs），
			// 所以值本身就是原文
			if isEnvRef(value) || !(declared[name] || secretishKey(name)) {
				continue
			}
			offenders = append(offenders, c.Ref()+" → "+name)
		}
	}
```

（Task 4 会在这个循环里再插入一条 `existingSecret` 形状的豁免——`config.ExistingSecretRef` 到那时才存在，这里先不用管它。）

把函数注释里"判据只看名字"的说法改成"判据是：组件声明了 `secret: true`，或名字长得像密钥（只看名字，不看值）"；
警告里 `WithHint` 的第三条 `"确实不是密钥的话可以忽略这条——判据只看名字，不看值"` 保持不变（对名字启发式那部分仍然成立）。

`internal/cli/up.go` 里 `warnConfigSecrets(opts, cfg)` 改成 `warnConfigSecrets(opts, cfg, plan.graph)`。

Run: `go test ./internal/cli -count=1` → PASS。

- [ ] **Step 3: 全量验证并提交**

Run: `go test ./... -count=1` 与 `make lint > "$SCRATCH/lint.log" 2>&1; echo "exit=$?"` → 全绿。

```bash
git add internal/cli
git commit -m "$(cat <<'EOF'
明文密钥告警认 configSchema 的 secret: true

名字启发式宁可漏报（apiKey 是密钥，sortKey 不是），只配拿来发警告；
组件声明了 secret: true、brickkit.yaml 里却写了字面值，就警告，不论名字长什么样。
写成 ${VAR} 引用永远不警告。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `existingSecret`——引用外部系统已经建好的 Secret（仅 K8s）

**背景（第二次评判改判的一条，见 Spec §2.1、§3.2）：** 提案 1 的两个真实例子——数据库密码、第三方 API 密钥——
都是"运维/安全团队已经用 Vault Secrets Operator / External Secrets Operator / Sealed Secrets 之类的工具，
在集群里建好了一个 K8s Secret"，平台需要做的只是"引用它，别自己生成一份"，与 `serviceAccountName`
（"运维已建好，平台只引用"）同一个模式，Helm 生态里也是被验证过的成熟写法。这不是"代取值"（已否决，见 §2.1），
CLI 从头到尾不接触密钥的值。本任务分两层：**资源层**（`resources[].existingSecret`，一个真实的结构体字段）
与**配置层**（声明了 `secret: true` 的配置项，值可以写成 `{ existingSecret, key }` 两键对象，不是新字段，
是同一个位置的另一种形状）。

**Files:**
- Modify: `internal/config/config.go`（`Resource.ExistingSecret` 字段）
- Modify: `internal/config/validate.go`（`existingSecret`/`password` 互斥报错）
- Create: `internal/config/secretref.go`（`ExistingSecretRef` 形状识别）
- Create: `internal/config/secretref_test.go`
- Modify: `internal/inject/inject.go`（`Var.ExistingSecretRef`；`resourceVars` 认 `r.ExistingSecret`；`addConfig` 认 `existingSecret` 形状）
- Modify: `internal/inject/inject_test.go`
- Modify: `internal/k8s/secret.go`（`secretRef`/`collectSecrets` 认 `ExistingSecretRef`）
- Modify: `internal/k8s/k8s_test.go`
- Modify: `internal/compose/compose.go`（`environmentOf` 跳过空值变量）
- Modify: `internal/compose/compose_test.go`
- Modify: `internal/cli/k8s_cluster.go`（`fieldUse`/`describeUsers` 加 `noun`；新增 `resourceField`/`resourceFields`；`k8sOnlyFields` 收 `existingSecret`）
- Modify: `internal/cli/up_secrets.go`（`warnConfigSecrets` 豁免 `existingSecret` 形状；新增 `warnExistingSecretConfigIssues`）
- Modify: `internal/cli/up.go`（调用 `warnExistingSecretConfigIssues`）
- Modify: `internal/cli/secret_flow_test.go`
- Modify: `docs/en\|zh/06-architecture/08-brickkit-yaml-reference.md`（`Resource.ExistingSecret` 会触发 `tests/docfields` 的完整性检查）

**Interfaces:**
- Consumes：Task 1、2 的 `secretFlowComp`/`secretFlowEntry`/`secretFlowResources`、`k8sProjectWith`/`dockerSecretFlowProject`、`k8s.secretRef`/`collectSecrets`、`inject.Var`；Task 3 的 `declaredSecretKeys`、`warnConfigSecrets(opts, cfg, plan.graph)` 已经接进 `up.go`。
- Produces：
  `config.Resource.ExistingSecret string`（yaml `existingSecret`）；
  `config.ExistingSecretRef(v any) (secretName, key string, ok bool)`；
  `inject.Var.ExistingSecretRef string`；
  `cli.warnExistingSecretConfigIssues(opts *Options, cfg *config.Config, graph *resolver.Graph)`；
  `cli.resourceFields(cfg *config.Config, fields []resourceField) []fieldUse`；
  `fieldUse.noun string`（"组件"/"资源"，供 `describeUsers` 措辞）。

- [ ] **Step 1: 写失败测试——config 层（字段、互斥校验、形状识别）**

新建 `internal/config/secretref_test.go`：

```go
package config

import "testing"

// ExistingSecretRef 只认恰好两个键、都是非空字符串的对象；其它任何形状都当"不是"处理，
// 落回原来的标量/引用路径（inject.addConfig 据此判断要不要拦截 formatValue）。
func TestExistingSecretRefRecognizesTheExactShape(t *testing.T) {
	cases := []struct {
		name string
		v    any
		ok   bool
	}{
		{"两键对象", map[string]any{"existingSecret": "vault-synced", "key": "api-key"}, true},
		{"标量字符串", "sk-live-plain", false},
		{"${VAR} 引用", "${API_TOKEN}", false},
		{"只有 existingSecret 没有 key", map[string]any{"existingSecret": "vault-synced"}, false},
		{"多了一个键", map[string]any{"existingSecret": "vault-synced", "key": "api-key", "extra": "x"}, false},
		{"existingSecret 是空字符串", map[string]any{"existingSecret": "", "key": "api-key"}, false},
		{"key 不是字符串", map[string]any{"existingSecret": "vault-synced", "key": 1}, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, ok := ExistingSecretRef(c.v)
			if ok != c.ok {
				t.Fatalf("%s: got ok=%v, want %v", c.name, ok, c.ok)
			}
		})
	}
}

func TestExistingSecretRefReturnsTheTwoValues(t *testing.T) {
	name, key, ok := ExistingSecretRef(map[string]any{"existingSecret": "vault-synced", "key": "api-key"})
	if !ok || name != "vault-synced" || key != "api-key" {
		t.Fatalf("got name=%q key=%q ok=%v", name, key, ok)
	}
}
```

新建 `internal/config/existingsecret_validate_test.go`：

```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// resources[].existingSecret 是真实的字段，能被解析、能被读出来。
func TestResourceExistingSecretIsParsed(t *testing.T) {
	c, err := ParseConfig([]byte(`project: demo
deploy:
  target: k8s
components:
  - id: people/basic
    version: 1.0.0
resources:
  - kind: database
    engine: postgresql
    id: main-db
    host: db.example.com
    port: 5432
    existingSecret: acme-db-vault-synced
    bindings:
      - componentId: people/basic
        database: people
`), "brickkit.yaml")
	require.NoError(t, err)
	assert.Equal(t, "acme-db-vault-synced", c.Resources[0].ExistingSecret)
	assert.Empty(t, c.Resources[0].Password, "没写 password 是合法的——密码在外部已建好的 Secret 里")
}

// existingSecret 与 password 同时写是矛盾意图：到底信哪个？必须报错，不能悄悄选一个。
func TestResourceExistingSecretConflictsWithPassword(t *testing.T) {
	_, err := ParseConfig([]byte(`project: demo
deploy:
  target: k8s
components:
  - id: people/basic
    version: 1.0.0
resources:
  - kind: database
    engine: postgresql
    id: main-db
    host: db.example.com
    port: 5432
    password: ${PG_PASSWORD}
    existingSecret: acme-db-vault-synced
    bindings:
      - componentId: people/basic
        database: people
`), "brickkit.yaml")

	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "existingSecret")
	assert.Contains(t, clierr.As(err).Format(), "password")
}
```

Run: `go test ./internal/config -run 'TestExistingSecretRef|TestResourceExistingSecret' -count=1`
Expected: FAIL（编译错误 `ExistingSecretRef undefined` / `ExistingSecret undefined`）。

- [ ] **Step 2: 实现 config 层**

新建 `internal/config/secretref.go`：

```go
package config

// ExistingSecretRef 识别一个 config 值是不是"引用外部系统已建好的 Secret"这个形状：
// { existingSecret: <名>, key: <Secret 里的 key> }，且只有这两个键、都是非空字符串。
//
// 用在 configSchema 声明了 secret: true 的配置项上（由调用方判断，这里只管认形状）——
// 与 resources[].existingSecret 是同一个模式的另一层：数据库密码这类"资源"密钥用真实的
// 结构体字段（平台自己定好了 password/secret-key 这个 key 名，引用外部 Secret 时约定它
// 也叫这个名字），组件自己的 API 密钥这类"配置"密钥没有对应的资源可以挂，只能用值的形状
// 表达——而且外部同步过来的 Secret 未必用组件作者起的配置项名当 key，所以必须显式写 key。
//
// gopkg.in/yaml.v3 把嵌套映射解到 interface{} 时给的是 map[string]interface{}（已用一个
// 独立小程序核实过），不是 map[interface{}]interface{}，所以这里的类型断言是安全的。
func ExistingSecretRef(v any) (secretName, key string, ok bool) {
	m, isMap := v.(map[string]any)
	if !isMap || len(m) != 2 {
		return "", "", false
	}
	secretName, hasSecret := m["existingSecret"].(string)
	key, hasKey := m["key"].(string)
	if !hasSecret || !hasKey || secretName == "" || key == "" {
		return "", "", false
	}
	return secretName, key, true
}
```

`internal/config/config.go`：`Resource` 结构体里 `Password` 字段（Task 1 已经在它注释末尾补了一句）之后加：

```go
	// ExistingSecret 是这个资源的密钥字段（database/cache/mq/smtp 的 password，
	// storage 的 secret-key）该引用的 K8s Secret 名，而不是由平台生成一份——
	// 用在运维已经用 Vault Secrets Operator / External Secrets Operator / Sealed Secrets
	// 之类的工具把密钥同步进集群的场景，CLI 从头到尾不接触值本身（仅 K8s，语义与
	// ServiceAccountName 一致：只引用、不生成）。与 Password 二选一，两者都写会报错——
	// 已经有一个外部管理的 Secret 时，Password 是多余的、也可能对不上。
	ExistingSecret string `yaml:"existingSecret,omitempty"`
```

`internal/config/validate.go` 的 `validateResources` 循环里，`if r.Host == "" { p.Missing(field + ".host") }` 之后加：

```go
		if r.ExistingSecret != "" && r.Password != "" {
			p.Addf(field+".existingSecret",
				"不能同时写 password——已经有一个外部系统管理的 Secret 时，"+
					"password 是多余的、也可能对不上")
		}
```

Run: `go test ./internal/config -count=1` → PASS。

- [ ] **Step 3: 写失败测试——inject 层（资源层 + 配置层）**

`internal/inject/inject_test.go` 在 `TestSecretConfigVarIsMarkedSensitive` 之后加（该测试与其配套的 `TestNonConfigVarsHaveNoOwner` 是 Task 2 加的）：

```go
// 资源声明了 existingSecret 而不是 password：密钥变量仍要产出（K8s 才有东西可以挂
// secretKeyRef），但值留空——existingSecret 场景下压根没有值可言。
func TestResourceExistingSecretProducesVarWithoutValue(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(config.Resource{
		Kind: config.ResourceKindDatabase, Engine: "postgresql", ID: "main-db",
		Host: "pg.infra.svc", Port: 5432, Username: "app",
		ExistingSecret: "acme-db-vault-synced",
		Bindings:       []config.Binding{{ComponentID: "people/basic", Database: "people"}},
	})

	pw := varOf(t, b.build(), "people/basic", "DATABASE_PASSWORD")
	assert.True(t, pw.IsSecret())
	assert.Empty(t, pw.Value, "existingSecret 场景下没有值可言")
	assert.Equal(t, "acme-db-vault-synced", pw.ExistingSecretRef)
	assert.Equal(t, "main-db", pw.ResourceID)
}

// 既没写 password 也没写 existingSecret：跟今天一样，字段没配就不注入（不是本次改动的范围，
// 只是确认没有被新代码影响）。
func TestResourceWithNeitherPasswordNorExistingSecretStillSkipped(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(config.Resource{
		Kind: config.ResourceKindDatabase, Engine: "postgresql", ID: "main-db",
		Host: "pg.infra.svc", Port: 5432, Username: "app",
		Bindings: []config.Binding{{ComponentID: "people/basic", Database: "people"}},
	})

	env := envOf(t, b.build(), "people/basic")
	_, exists := env["DATABASE_PASSWORD"]
	assert.False(t, exists)
}

// 声明了 secret: true 的配置项写成 existingSecret 形状：产出的变量没有值，
// ExistingSecretRef/SecretKey 分别是 Secret 名与 Secret 里的 key（不是环境变量名）。
func TestConfigExistingSecretShapeProducesVarWithoutValue(t *testing.T) {
	m := simple("acme/hello", "0.1.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"apiKey": {Type: "string", Secret: true},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{Config: map[string]any{
		"apiKey": map[string]any{"existingSecret": "acme-hello-vault-synced", "key": "api-key"},
	}})

	v := varOf(t, b.build(), "acme/hello", "API_KEY")
	assert.True(t, v.IsSecret())
	assert.Empty(t, v.Value)
	assert.Equal(t, "acme-hello-vault-synced", v.ExistingSecretRef)
	assert.Equal(t, "api-key", v.SecretKey, "Secret 里的 key 是使用者自己写的，不是转换后的环境变量名")
}

// 写了 existingSecret 形状，但这个配置项没有声明 secret: true：不注入（不能把这个对象
// 糊成 Go 的 map[...] 字符串塞给组件），交给 cli 层的警告说清楚原因（Task 4 Step 7）。
func TestConfigExistingSecretShapeWithoutDeclaredSecretIsNotInjected(t *testing.T) {
	m := simple("acme/hello", "0.1.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"apiKey": {Type: "string"}, // 没有 Secret: true
	}}

	b := newBuilder(t)
	b.component(m, config.Component{Config: map[string]any{
		"apiKey": map[string]any{"existingSecret": "acme-hello-vault-synced", "key": "api-key"},
	}})

	env := envOf(t, b.build(), "acme/hello")
	_, exists := env["API_KEY"]
	assert.False(t, exists)
}
```

Run: `go test ./internal/inject -run 'TestResourceExistingSecret|TestResourceWithNeither|TestConfigExistingSecret' -count=1`
Expected: FAIL（`ExistingSecretRef` 字段不存在；或者行为不对——existingSecret 场景下变量整个没产出）。

- [ ] **Step 4: 实现 inject 层**

`internal/inject/inject.go`：`Var` 结构体里 `SecretKey` 字段之后加：

```go
	// ExistingSecretRef 非空表示这条敏感变量不该由平台生成 Secret，而是引用外部系统
	// （Vault Secrets Operator / External Secrets Operator / Sealed Secrets……）已经建好的
	// 这个名字的 K8s Secret；值是那个 Secret 的名字。只在 IsSecret() 为 true 时有意义。
	// K8s 渲染器（secretRef）优先看它；Docker 没有对应概念，这类变量的 Value 始终是空串，
	// 由 compose 的 environmentOf 跳过（表现成"没配"）。
	ExistingSecretRef string
```

`resourceVars` 里把末尾的循环

```go
	out := make([]Var, 0, len(pairs))
	for _, pair := range pairs {
		if pair.value == "" {
			// 没配的字段不注入空值：组件据此判断"这项没提供"
			continue
		}
		out = append(out, Var{
			Name: prefix + pair.name, Value: pair.value, Source: SourceResource,
			ResourceID: r.ID, SecretKey: pair.secretKey,
		})
	}
	return out
}
```

换成：

```go
	out := make([]Var, 0, len(pairs))
	for _, pair := range pairs {
		usesExistingSecret := pair.secretKey != "" && r.ExistingSecret != ""
		if pair.value == "" && !usesExistingSecret {
			// 没配的字段不注入空值：组件据此判断"这项没提供"。
			//
			// 例外：existingSecret 场景下密钥字段压根没有值可言（值在外部已经建好的
			// Secret 里），但这条变量仍然要存在——K8s 才有东西可以挂 secretKeyRef；
			// Docker 没有"引用外部 Secret"这个概念，由 compose 的 environmentOf
			// 在写文件那一步跳过空值变量，表现成"没配"，与其它未配置字段一致。
			continue
		}
		v := Var{
			Name: prefix + pair.name, Value: pair.value, Source: SourceResource,
			ResourceID: r.ID, SecretKey: pair.secretKey,
		}
		if usesExistingSecret {
			v.ExistingSecretRef = r.ExistingSecret
		}
		out = append(out, v)
	}
	return out
}
```

`addConfig` 里，在 `value, source := property.Default, SourceConfig` 与 `if override, ok := entry.Config[key]; ok { value, source = override, SourceOverride }` 之后、`if value == nil {` 之前，插入 existingSecret 形状的识别（这一步必须先于 `missing`/`required` 判断，也必须先于 `formatValue`）：

```go
		if secretName, secretKeyInRef, ok := config.ExistingSecretRef(value); ok {
			if !property.Secret {
				// 没有声明 secret: true，existingSecret 写法不会生效——
				// 不能把这个对象糊成 Go 的 map[...] 字符串塞给组件，交给 cli 层的
				// 警告说清楚原因（up_secrets.go 的 warnExistingSecretConfigIssues）
				continue
			}
			name := EnvVarName(key)
			if pattern, hit := b.matchReserved(name); hit {
				warnings = append(warnings, reservedConflictWarning(b.componentID, key, name, pattern))
				continue
			}
			b.set(Var{
				Name: name, Source: source, Key: key, Owner: b.service,
				SecretKey: secretKeyInRef, ExistingSecretRef: secretName,
			})
			continue
		}
```

（`config` 是 `internal/config` 包，`addConfig` 所在文件已经 `import "github.com/brickkit/brickkit/internal/config"`——用的是这份文件里已有的 `config.Component`/`config.Resource` 等类型，不需要新增导入。）

Run: `go test ./internal/inject -count=1` → PASS。

- [ ] **Step 5: 写失败测试——K8s 渲染 + compose 空值跳过**

`internal/k8s/k8s_test.go` 在 `TestServedByMemberSecretConfigStaysWithMemberSecret`（Task 2 加的）之后加：

```go
// ============================================================
// existingSecret：引用外部已建好的 Secret（Spec 2026-09-19 §3.2）
// ============================================================

// 资源声明了 existingSecret：Deployment 里的 secretKeyRef 直接指向那个名字，
// 平台不生成任何 Secret 条目——resource-secrets.yaml 里完全找不到这个资源的痕迹。
func TestResourceExistingSecretReferencesGivenName(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(config.Resource{
		Kind: config.ResourceKindDatabase, Engine: "postgresql", ID: "main-db",
		Host: "pg.infra.svc", Port: 5432, Username: "app",
		ExistingSecret: "acme-db-vault-synced",
		Bindings:       []config.Binding{{ComponentID: "people/basic", Database: "people"}},
	})

	env := envOf(t, b.container("people-basic-1-0-0"))

	assert.Equal(t, map[string]any{"secretKeyRef": map[string]any{
		"name": "acme-db-vault-synced", "key": "password",
	}}, env["DATABASE_PASSWORD"])
	assert.False(t, hasFile(b.generate(), "secrets/resource-secrets.yaml"),
		"这个资源的值不该由平台知道，也就没有 Secret 可生成")
}

// 组件声明的配置密钥写成 existingSecret 形状：secretKeyRef 指向使用者给的名字与 key，
// 不是平台按 <服务名>-config-secret 算出来的那个。
func TestConfigExistingSecretReferencesGivenNameAndKey(t *testing.T) {
	m := secretConfigManifest() // Task 2 加的：apiKey 声明了 secret: true
	b := newBuilder(t)
	b.component(m, config.Component{Config: map[string]any{
		"apiKey": map[string]any{"existingSecret": "acme-hello-vault-synced", "key": "api-key"},
	}})

	env := envOf(t, b.container("acme-hello-0-1-0"))

	assert.Equal(t, map[string]any{"secretKeyRef": map[string]any{
		"name": "acme-hello-vault-synced", "key": "api-key",
	}}, env["API_KEY"])
	assert.False(t, hasFile(b.generate(), "secrets/config-secrets.yaml"),
		"值在外部已建好的 Secret 里，平台没有值可以生成一份自己的 Secret")
}
```

`internal/compose/compose_test.go` 在 `TestSecretsStayAsEnvironmentReferences` 之后加：

```go
// existingSecret 场景下密钥变量没有值——Docker 没有"引用外部 Secret"这个概念，
// 这条变量必须表现成"没配"（不能写出 DATABASE_PASSWORD= 这种空字符串，那正是
// §9.13 反对的"注入空值"）。
func TestExistingSecretVarsAreOmittedUnderDocker(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(config.Resource{
		Kind: config.ResourceKindDatabase, Engine: "postgresql", ID: "main-db",
		Host: "pg.infra.svc", Port: 5432, Username: "app",
		ExistingSecret: "acme-db-vault-synced",
		Bindings:       []config.Binding{{ComponentID: "people/basic", Database: "people"}},
	})

	svc := serviceOf(t, b.parsed(), "people-basic-1-0-0")
	raw := svc["environment"].([]any)
	for _, item := range raw {
		assert.NotContains(t, item.(string), "DATABASE_PASSWORD=",
			"existingSecret 在 Docker 下没有意义，这个变量不该出现，哪怕是空值")
	}
}
```

Run: `go test ./internal/k8s ./internal/compose -run 'TestResourceExistingSecret|TestConfigExistingSecret|TestExistingSecretVarsAreOmittedUnderDocker' -count=1`
Expected: FAIL（`env["DATABASE_PASSWORD"]` 的引用不对；或者 Docker 侧真的写出了 `DATABASE_PASSWORD=`）。

- [ ] **Step 6: 实现 K8s 渲染 + compose 空值跳过**

`internal/k8s/secret.go`：`secretRef` 换成（在最前面插入一条分支）：

```go
// secretRef 返回一条敏感变量在 K8s Secret 里的位置：Secret 名 + key。
func secretRef(v inject.Var) (name, key string) {
	if v.ExistingSecretRef != "" {
		// 引用外部系统已经建好的 Secret，不生成任何平台自己的 Secret
		return v.ExistingSecretRef, v.SecretKey
	}
	if v.Source == inject.SourceResource {
		return secretName(v.ResourceID), v.SecretKey
	}
	return configSecretName(v.Owner), v.SecretKey
}
```

`collectSecrets` 里 `if !v.IsSecret() {` 改成 `if !v.IsSecret() || v.ExistingSecretRef != "" {`（引用外部 Secret 的变量没有值可存，不该出现在平台生成的 Secret 里）。

`internal/compose/compose.go` 的 `environmentOf` 换成：

```go
// environmentOf 把注入结果渲染成 compose 的 environment 列表。
//
// 用 `KEY=value` 的列表而不是 map：列表保序，生成的文件可比对
// （map 在 YAML 里会被重排，每次 diff 都是噪音）。
//
// 跳过值为空的变量：今天所有会产生空值的路径都在生成 Var 之前就被挡掉了
// （§9.13：缺失就不注入，绝不注入空值），唯一的例外是 resources[].existingSecret /
// 配置密钥的 existingSecret 写法——那条密钥字段的值本来就不存在（值在外部已建好的
// Secret 里），K8s 侧靠 secretKeyRef 引用它，Docker 没有对应概念，这里让它表现成
// "没配"，与其它未配置字段一致。
func environmentOf(c inject.Component) []string {
	out := make([]string, 0, len(c.Env))
	for _, v := range c.Env {
		if v.Value == "" {
			continue
		}
		out = append(out, v.Name+"="+v.Value)
	}
	return out
}
```

Run: `go test ./internal/k8s ./internal/compose -count=1` → PASS。

- [ ] **Step 7: 写失败测试——cli 层警告（K8s-only + 未声明 secret）**

`internal/cli/k8s_cluster.go` 的改动先写测试。加进 `internal/cli/secret_flow_test.go`（复用 Task 1 已经定义的 `secretFlowComp`，
它的 Manifest 已经声明了 `dependencies.resources: [{kind: database, engine: postgresql}]`，`addedProject` 用它建出的项目
可以直接绑一个 `existingSecret` 资源）：

```go
// resources[].existingSecret 只在 K8s 生效；Docker 下要警告，不是错误——existingSecret
// 对 Docker 没有意义，跟 replicas/tlsSecret 那批"只在 K8s 生效"的字段同一条路。
func TestExistingSecretWarnsUnderDocker(t *testing.T) {
	f := addedProject(t, []comp{secretFlowComp}, secretFlowComp.ref())

	var b strings.Builder
	b.WriteString(configHeader)
	b.WriteString("\nsources:\n")
	for _, s := range f.Sources {
		b.WriteString(s)
	}
	b.WriteString(`
components:
  - id: demo/hello
    version: 1.0.0
resources:
  - kind: database
    engine: postgresql
    id: main-db
    host: db.example.com
    port: 5432
    existingSecret: acme-db-vault-synced
    bindings:
      - componentId: demo/hello
        database: hello
`)
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(), []byte(b.String()), 0o644))

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, "是警告不是错误：%s", r.stderr)
	out := r.stdout + r.stderr
	assert.Contains(t, out, "只对 K8s 生效")
	assert.Contains(t, out, "existingSecret")
	assert.Contains(t, out, "main-db", "要点名是哪个资源，不能只说组件")
}
```

`internal/cli/secret_flow_test.go` 末尾加（配置层的两个警告）：

```go
// existingSecret 形状但配置项没有声明 secret: true：警告，且不能把值糊成
// map[...] 字符串塞给组件——inject 层已经确保它不被注入，这里确认使用者能看懂为什么。
func TestConfigExistingSecretWithoutDeclaredSecretWarns(t *testing.T) {
	declared := secretFlowComp
	declared.SecretConfig = nil // apiToken 没有声明 secret: true
	f := k8sProjectWith(t, declared, `    config:
      apiToken:
        existingSecret: acme-hello-vault-synced
        key: api-key
`, "")

	r := runWithEngine(t, newK8sEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, "是警告不是错误：%s%s", r.stdout, r.stderr)
	out := r.stdout + r.stderr
	assert.Contains(t, out, "apiToken")
	assert.Contains(t, out, "existingSecret")
	assert.Contains(t, out, "secret: true")
}

// existingSecret 形状但目标是 docker：警告，且不出现在生成的 compose 里。
func TestConfigExistingSecretUnderDockerWarns(t *testing.T) {
	declared := secretFlowComp
	declared.SecretConfig = []string{"apiToken"}
	f := addedProject(t, []comp{declared}, declared.ref())

	var b strings.Builder
	b.WriteString(configHeader)
	b.WriteString("\nsources:\n")
	for _, s := range f.Sources {
		b.WriteString(s)
	}
	b.WriteString("\ncomponents:\n  - id: demo/hello\n    version: 1.0.0\n    config:\n" +
		"      apiToken:\n        existingSecret: acme-hello-vault-synced\n        key: api-key\n")
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(), []byte(b.String()), 0o644))

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	out := r.stdout + r.stderr
	assert.Contains(t, out, "existingSecret")
	assert.Contains(t, out, "只")
	assert.Contains(t, out, "K8s")
}
```

（这两个测试里的 `k8sProjectWith(t, declared, "    config:\n...", "")` 用法要对着 `internal/cli/up_k8s_test.go` 的 `k8sProjectWith` 签名核实
`entryLines`/`extra` 两个参数各自拼在哪——如果与本计划写法不完全一致，按签名实际拼法调整，不要改测试意图。）

同时，把 `TestConfigSecretWarningExplainsWhy`（Task 1 之前就有的旧测试，见 `internal/cli/config_secret_test.go`）附近读一遍：
确认 `warnConfigSecrets` 对 existingSecret 形状不会误报"明文密钥"——补一个防回归用例：

```go
// existingSecret 形状是"做对了"，不该被明文密钥警告误伤——它不是字面密钥，是一个引用。
func TestExistingSecretShapeDoesNotTriggerPlaintextWarning(t *testing.T) {
	declared := secretFlowComp
	declared.SecretConfig = []string{"apiToken"}
	f := k8sProjectWith(t, declared, `    config:
      apiToken:
        existingSecret: acme-hello-vault-synced
        key: api-key
`, "")

	r := runWithEngine(t, newK8sEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.NotContains(t, r.stdout+r.stderr, "明文密钥")
}
```

Run: `go test ./internal/cli -run 'TestExistingSecretWarnsUnderDocker|TestConfigExistingSecretWithoutDeclaredSecretWarns|TestConfigExistingSecretUnderDockerWarns|TestExistingSecretShapeDoesNotTriggerPlaintextWarning' -count=1`
Expected: FAIL（还没有对应的警告；`TestExistingSecretShapeDoesNotTriggerPlaintextWarning` 这一个如果不改 `warnConfigSecrets` 也会 FAIL，因为 `value` 是 map、`isEnvRef` 返回 false、`declared["apiToken"]` 为 true → 会被误判成明文密钥）。

- [ ] **Step 8: 实现 cli 层警告**

`internal/cli/k8s_cluster.go`：

`fieldUse` 加字段：

```go
	// noun 是 describeUsers 该怎么称呼 components 里的这些名字："组件"还是"资源"；
	// 空值按"组件"处理（历史上所有调用方都是组件级字段）。
	noun string
```

`describeUsers` 签名与实现：

```go
// describeUsers 说清是哪几个组件/资源写了它。
func describeUsers(components []string, noun string) string {
	if noun == "" {
		noun = "组件"
	}
	if len(components) <= maxListedComponents {
		return fmt.Sprintf("%d 个%s：%s", len(components), noun, strings.Join(components, "、"))
	}
	return fmt.Sprintf("%d 个%s：%s 等",
		len(components), noun, strings.Join(components[:maxListedComponents], "、"))
}
```

`warnFields` 里唯一的调用点改成 `describeUsers(f.components, f.noun)`。

在 `componentFields` 之后加：

```go
// resourceField 是一个资源级字段与"它写了没有"的判断，与 componentField 同构。
type resourceField struct {
	name string
	set  func(config.Resource) bool
}

func resourceFields(cfg *config.Config, fields []resourceField) []fieldUse {
	var out []fieldUse
	for _, f := range fields {
		var users []string
		for _, r := range cfg.Resources {
			if f.set(r) {
				users = append(users, r.ID)
			}
		}
		if len(users) > 0 {
			out = append(out, fieldUse{name: "resources[]." + f.name, components: users, noun: "资源"})
		}
	}
	return out
}
```

`k8sOnlyFields` 末尾 `return append(out, componentFields(...)...)` 改成：

```go
	out = append(out, componentFields(cfg, []componentField{
		{"replicas", func(c config.Component) bool { return c.Replicas != nil }},
		{"hostname", func(c config.Component) bool { return c.Hostname != "" }},
		{"tlsSecret", func(c config.Component) bool { return c.TLSSecret != "" }},
		{"serviceAccountName", func(c config.Component) bool { return c.ServiceAccountName != "" }},
	})...)
	return append(out, resourceFields(cfg, []resourceField{
		{"existingSecret", func(r config.Resource) bool { return r.ExistingSecret != "" }},
	})...)
```

`internal/cli/up_secrets.go`：`warnConfigSecrets` 的循环体加一条豁免（existingSecret 形状是"做对了"，不是明文）：

```go
		for name, value := range c.Config {
			// 写成 ${ENV_VAR} 就是做对了，existingSecret 引用也是——解析时这两处
			// 都不展开/不改写（config.deferredRefs），所以值本身就是原文/原形状
			_, _, isExistingSecretRef := config.ExistingSecretRef(value)
			if isEnvRef(value) || isExistingSecretRef || !(declared[name] || secretishKey(name)) {
				continue
			}
			offenders = append(offenders, c.Ref()+" → "+name)
		}
```

在 `warnConfigSecrets` 之后新增：

```go
// warnExistingSecretConfigIssues 检查 component.config 里写了 existingSecret 形状的两类问题：
//
//	① 配置项没有声明 secret: true——这个形状不会生效（见 inject.addConfig），
//	   使用者会以为自己配好了，实际这条变量根本不会被注入；
//	② deploy.target 是 docker——existingSecret 是 K8s 专属概念，Docker 没有
//	   "引用外部已建好的 Secret"这回事，同样不会生效。
//
// 与 declaredSecretKeys 用同一份"这个组件的 configSchema 怎么说"的读取方式：graph 里
// 没有这个组件（还没 add、Manifest 读不到）时什么都不查，等 add 完、下一次 up 自然查得到。
func warnExistingSecretConfigIssues(opts *Options, cfg *config.Config, graph *resolver.Graph) {
	var notDeclaredSecret, dockerOnly []string
	for _, c := range cfg.Components {
		declared := declaredSecretKeys(graph, c)
		for name, value := range c.Config {
			if _, _, ok := config.ExistingSecretRef(value); !ok {
				continue
			}
			ref := c.Ref() + " → " + name
			if !declared[name] {
				notDeclaredSecret = append(notDeclaredSecret, ref)
			}
			if cfg.Deploy.Target != config.TargetK8s {
				dockerOnly = append(dockerOnly, ref)
			}
		}
	}

	if len(notDeclaredSecret) > 0 {
		sort.Strings(notDeclaredSecret)
		renderWarnings(opts, []*clierr.Error{
			clierr.Warn(clierr.CodeConfigInvalid, "existingSecret 写法不会生效：配置项没有声明 secret: true").
				WithDetail("配置项", strings.Join(notDeclaredSecret, "、")).
				WithHint("给对应的 configSchema.properties.<key> 加一行 secret: true，或者把这个值改回普通写法"),
		})
	}
	if len(dockerOnly) > 0 {
		sort.Strings(dockerOnly)
		renderWarnings(opts, []*clierr.Error{
			clierr.Warn(clierr.CodeConfigInvalid, "existingSecret 只在 K8s 生效，当前是 docker 目标").
				WithDetail("配置项", strings.Join(dockerOnly, "、")).
				WithHint("Docker 下没有\"引用外部已建好的 Secret\"这个概念，请直接给这个配置项写字面值或 ${VAR} 引用"),
		})
	}
}
```

导入按需补 `"sort"`（若该文件还没有）。

`internal/cli/up.go` 里 `warnConfigSecrets(opts, cfg, plan.graph)`（Task 3 已经加了这一行）之后补：

```go
	warnExistingSecretConfigIssues(opts, cfg, plan.graph)
```

Run: `go test ./internal/cli -count=1` → PASS。

- [ ] **Step 9: 参考文档补 `existingSecret` 一行（触发的是 08，不是 07）**

`Resource.ExistingSecret` 是 `config.Config` 下的真实结构体字段，`tests/docfields/reference_test.go` 会要求
`docs/{en,zh}/06-architecture/08-brickkit-yaml-reference.md` 里有它（**不是** component.yaml 参考文档——配置层的
`{ existingSecret, key }` 写法不是新增结构体字段，`Config` 仍是 `map[string]any`，不会被这个结构性检查捕获，
但同样值得写清楚，一起放进这一行附近）：

`docs/en/06-architecture/08-brickkit-yaml-reference.md`，`resources[].password` 那一行之后加：

```markdown
| `resources[].existingSecret` | string | no | K8s only. References a Secret an external system (Vault Secrets Operator, External Secrets Operator, Sealed Secrets, …) already put in the cluster, instead of the platform generating one from `password`. Mutually exclusive with `password` — writing both errors. The platform never reads or writes the value; it only points the `secretKeyRef` at this name, using the same key (`password` or `secret-key`) it would use for a generated Secret. Ignored (with a warning) under `docker`. A `configSchema` property declared `secret: true` can use the same idea for a component's own config value: write `{ existingSecret: <name>, key: <key-in-secret> }` in place of a scalar — see [Secrets](../07-patterns/10-secrets.md). |
```

`docs/zh/06-architecture/08-brickkit-yaml-reference.md` 同位置：

```markdown
| `resources[].existingSecret` | string | 否 | 仅 K8s。引用外部系统（Vault Secrets Operator、External Secrets Operator、Sealed Secrets……）已经放进集群的 Secret，而不是让平台从 `password` 生成一份。与 `password` 互斥——两个都写会报错。平台从不读写这个值，只是把 `secretKeyRef` 指向这个名字，用的 key 与平台自己生成时会用的一样（`password` 或 `secret-key`）。`docker` 下被忽略（有警告）。声明了 `secret: true` 的 `configSchema` 属性也能用同一个想法：把标量值换成 `{ existingSecret: <名>, key: <Secret 里的 key> }`——见[密钥](../07-patterns/10-secrets.md)。 |
```

Run: `go test ./tests/docfields/... -count=1` → PASS。

- [ ] **Step 10: 全量验证并提交**

Run: `go test ./... -count=1` 与 `make lint > "$SCRATCH/lint.log" 2>&1; echo "exit=$?"`
Expected: 全绿、`exit=0`。

```bash
git add internal docs
git commit -m "$(cat <<'EOF'
新增 existingSecret：引用外部系统已经建好的 Secret（仅 K8s），CLI 不接触值

提案 1（外部密钥集成）的两个真实例子——数据库密码、第三方 API 密钥——都是"运维已经用
Vault Secrets Operator / External Secrets Operator / Sealed Secrets 把密钥同步进集群"，
不是"CLI 自己去问 Vault 要值"。这与平台已有的 serviceAccountName 模式（运维已建好，
平台只引用、不生成）同构，Helm 生态里也是被验证过的成熟写法。

资源层：resources[].existingSecret（与 password 互斥，仅 K8s，Docker 下警告忽略）。
配置层：声明了 secret: true 的配置项，值可以写成 { existingSecret, key } 两键对象，
不占用新字段，是 config.<key> 同一个位置的另一种形状。

CLI 从头到尾不读写密钥的值：既不像"平台代取值"（已否决，见 §2.1）那样调任何 SDK，
也不生成任何 Secret 内容——只是把 secretKeyRef 指向使用者给的名字。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: 文档——把"密钥怎么流"写清楚（含 existingSecret）

**Files:**
- Modify: `docs/en/06-architecture/07-component-yaml-reference.md`、`docs/zh/…`（`pattern` 行之后加 `secret` 行）
- Modify: `AGENTS.md`、`AGENTS.zh.md`（§6/§7 骨架、§5.2 加一段、§5.6 一处更正）
- Modify: `docs/en|zh/06-architecture/04-environment-variables.md`（🔒 说明处，约第 62 行）
- Modify: `docs/en|zh/06-architecture/03-deployment-generation.md`（Secret 那条要点，约第 93 行）
- Modify: `docs/en|zh/07-patterns/07-shell-implementers-guide.md`（约 110–127 行那两段）
- Modify: `docs/en|zh/06-architecture/10-error-codes.md`（第 405 行那一行）
- Create: `docs/en/07-patterns/10-secrets.md`、`docs/zh/07-patterns/10-secrets.md`
- Modify（同步清单，见 Step 10）：各级 README、`llms.txt`、`llms.zh.txt`、AGENTS §11.2 等

- [ ] **Step 1: 参考文档补 `secret` 一行（Task 2 Step 8 需要它先绿）**

`docs/en/06-architecture/07-component-yaml-reference.md`，紧跟 `configSchema.properties.<key>.pattern` 那一行之后：

```markdown
| `configSchema.properties.<key>.secret` | boolean | no | Marks the value as a credential. Unlike `enum` / `minimum` / `pattern` it does change what the platform does — but only *where the value is written*, never whether a value is accepted (`configSchema` stays a spec sheet, AGENTS.md §9.12). On `deploy.target: k8s` the value goes into a generated `Secret/<versioned-service-name>-config-secret` and the Deployment carries only a `secretKeyRef`; on `docker` nothing changes (the `${VAR}` placeholder is already left for `docker compose` to resolve). A literal value written in `brickkit.yaml` for a `secret: true` property triggers the plaintext-secret warning whatever the key is called. In place of a scalar, its `brickkit.yaml` value can instead be `{ existingSecret: <name>, key: <key-in-secret> }`, referencing a Secret an external system already created. See [Secrets](../07-patterns/10-secrets.md). |
```

`docs/zh/06-architecture/07-component-yaml-reference.md` 同位置：

```markdown
| `configSchema.properties.<key>.secret` | boolean | 否 | 声明这一项的值是凭据。它和 `enum` / `minimum` / `pattern` 不同，确实会改变平台的行为——但只改变**值写到哪里**，从不决定值收不收（`configSchema` 仍然是说明书，AGENTS.md §9.12）。`deploy.target: k8s` 下，这一项的值进平台生成的 `Secret/<版本化服务名>-config-secret`，Deployment 里只有 `secretKeyRef`；`docker` 下没有变化（`${VAR}` 占位符本来就留给 `docker compose` 自己求值）。给 `secret: true` 的配置项在 `brickkit.yaml` 里写了字面值，不论名字叫什么都会触发明文密钥警告。它在 `brickkit.yaml` 里的值也可以不写标量，改写成 `{ existingSecret: <名>, key: <Secret 里的 key> }`，引用外部系统已经建好的 Secret。见[密钥](../07-patterns/10-secrets.md)。 |
```

Run: `go test ./tests/docfields/... -count=1`
Expected: 参考文档那一条转绿；AGENTS 骨架若也有对照测试则下一步补。

- [ ] **Step 2: AGENTS 骨架、§5.2、§5.6**

`AGENTS.md` §6 骨架里，`pattern: <regex>` 那一行之后加：

```
      secret: true               # optional — a credential: on K8s its value goes through a generated
                                 #   Secret (secretKeyRef), never plaintext env. Not validated (§5.2)
```

`AGENTS.zh.md` 对应处（`pattern: <正则>` 之后）：

```
      secret: true               # 可选——声明这是凭据：K8s 下值走平台生成的 Secret（secretKeyRef），
                                 #   绝不明文进 env。不校验任何值（§5.2）
```

`AGENTS.md` §7 骨架（`brickkit.yaml` 字段清单）里，`password: ${DB_PASSWORD}     # must be referenced via an env var` 那一行之后加：

```
    existingSecret: <K8s Secret name>  # optional, K8s only, mutually exclusive with password —
                                       #   reference a Secret already created by ops/Vault/ESO instead
```

`AGENTS.zh.md` 对应处（`password: ${DB_PASSWORD}     # 必须通过环境变量引用` 之后）：

```
    existingSecret: <K8s Secret 名>  # 可选，仅 K8s，与 password 互斥——
                                    #   引用运维/Vault/ESO 已经建好的 Secret，而不是让平台生成
```

`AGENTS.md` §5.2，紧接"A third, distinct failure mode…"那一段之后、"The table above states the naming *shape*"之前，加：

```markdown
**Secrets take a different road from ordinary values.** Resource passwords (`DATABASE_PASSWORD`, …)
are always secrets; a component's own config item is a secret only if its `configSchema` says
`secret: true` (the platform never guesses from the name). On `deploy.target: k8s` a secret goes into
a generated `Secret` (file mode 0600) and the Deployment holds a `secretKeyRef`; everything else is a
plain `env` value. On Docker, `${VAR}` in `config` and `resources[].password` is **never** resolved by
the CLI when it writes `docker-compose.yaml` — `docker compose` resolves it at start (process
environment first, `.env` second). `brickkit.yaml` only ever holds the reference. There is no
"fetch from Vault" built in and there won't be (§4.1): anything that can put the value in the process
environment works — and for a Secret an external system (Vault Secrets Operator, External Secrets
Operator, Sealed Secrets, …) already created in the cluster, `resources[].existingSecret` and a
`secret: true` config value's `{ existingSecret, key }` form reference it directly, K8s only; the
platform never reads or writes the value either way. Details: [Secrets](docs/en/07-patterns/10-secrets.md).
```

`AGENTS.zh.md` §5.2 同位置：

```markdown
**密钥与普通值走不同的路。** 资源密码（`DATABASE_PASSWORD` 等）一律是密钥；组件自己的配置项只有在
`configSchema` 里写了 `secret: true` 才算（平台从不按名字猜）。`deploy.target: k8s` 下，密钥进平台生成的
`Secret`（文件权限 0600），Deployment 里只有 `secretKeyRef`；其余都是明文 `env`。Docker 下，`config` 与
`resources[].password` 里的 `${VAR}` 在 CLI 写 `docker-compose.yaml` 时**从不**求值——由 `docker compose`
启动时求值（先进程环境、后 `.env`）。`brickkit.yaml` 里永远只有引用。平台没有内置"去 Vault 取值"，
以后也不会有（§4.1）：任何能把值放进进程环境的工具都行——而对于外部系统（Vault Secrets Operator、
External Secrets Operator、Sealed Secrets……）已经在集群里建好的 Secret，`resources[].existingSecret`
与 `secret: true` 配置项的 `{ existingSecret, key }` 写法能直接引用它，仅 K8s，平台从不读写那个值。
详见[密钥](docs/zh/07-patterns/10-secrets.md)。
```

§5.6（本地调试）里那句"a `${VAR}` config value is looked up from the project root's `.env` file (`config.envLookup` — process env first, `.env` second)"：
先 `git grep -n 'envLookup' AGENTS.md AGENTS.zh.md` 定位，把它改准确——那是 **K8s 与 local-debug 的查找函数**（`internal/cli/up_k8s.go` 的 `envLookup`），
Docker 的 compose 文件由 `docker compose` 自己按同样顺序求值。改动只限这一句的归属，不动其余内容。

- [ ] **Step 3: 环境变量文档与部署生成文档**

`docs/en/06-architecture/04-environment-variables.md` 第 62 行附近那句 `🔒 marks a secret: on deploy.target: k8s …` 之后补：

```markdown
A component's own config variable gets the 🔒 too when its `configSchema` property says `secret: true`. Its Secret is `Secret/<versioned-service-name>-config-secret` (key = the variable's name), separate from the resource Secrets, and under `servedBy` it stays with the *member* — the shell's prefixed variable (e.g. `MDM_CUSTOMER_API_KEY`) is a `secretKeyRef` into the member's Secret. Either kind of secret can instead reference a Secret an external system already created (`resources[].existingSecret`, or a `secret: true` config value written as `{ existingSecret, key }`) — the platform then generates no Secret of its own for that one variable.
```

`docs/zh/…/04-environment-variables.md` 同位置：

```markdown
组件自己的配置变量，只要它在 `configSchema` 里写了 `secret: true`，也带 🔒。它的 Secret 是 `Secret/<版本化服务名>-config-secret`（key 是变量名），与资源 Secret 分开；`servedBy` 下它仍归**成员**——外壳里带前缀的变量（如 `MDM_CUSTOMER_API_KEY`）是指向成员 Secret 的 `secretKeyRef`。两类密钥都可以改成引用外部系统已经建好的 Secret（`resources[].existingSecret`，或 `secret: true` 的配置值写成 `{ existingSecret, key }`）——这种情况下平台不再为这一条变量生成自己的 Secret。
```

`docs/{en,zh}/06-architecture/03-deployment-generation.md`：读第 83–93 行讲 `Secret/main-db-secret` 的那条要点，在其后加两条对称的说明（中英各写各的）：
① 组件声明 `secret: true` 的配置项同理，多一份 `secrets/config-secrets.yaml`，文件权限同为 0600；
② 写了 `existingSecret` 的资源或配置项，两份生成物里都不出现——`secretKeyRef` 直接指向使用者给的名字；
并把该文档里若有写死"唯一一份 Secret 文件"的措辞改准确。

- [ ] **Step 4: 外壳指南更正**

`docs/{en,zh}/07-patterns/07-shell-implementers-guide.md`（约 110–127 行）：那段"brickKit's Docker Compose generation deliberately never resolves those itself"
在 Task 1 之后**才**名副其实（此前进程环境里有值时并不成立），措辞不用改。在那两段之后补一小段（中英各写各的）：

> On `deploy.target: k8s`, a member's config value whose `configSchema` property says `secret: true` is not a plain `env` entry in your shell's Pod: it lives in that member's own `Secret/<versioned-service-name>-config-secret`, and the prefixed variable in your shell's environment is a `secretKeyRef` into it — or, if that member's config value was written as `{ existingSecret, key }`, straight into the Secret named there instead. You read it exactly like any other environment variable either way.

- [ ] **Step 5: 错误码表**

`docs/{en,zh}/06-architecture/10-error-codes.md` 里 `brickkit.yaml 的 config 里可能写了明文密钥` 那一行，"Meaning"列改成：
en：`A `config` key declared `secret: true` in the component's `configSchema`, or merely named like a secret, has a literal value (an `existingSecret` reference is not a literal and never triggers this). For a name-only match the check goes on the name alone, never the value; if it isn't a secret, ignore it`；
zh：`config 里一个在组件 configSchema 里声明了 secret: true、或者只是名字像密钥的项写了字面值（`existingSecret` 引用不是字面值，不会触发这条）。只是名字像的，判据只看名字、不看值，确实不是密钥的话可以忽略`。

再新增两行（`existingSecret` 的两个警告）：
en：`| `existingSecret 写法不会生效：配置项没有声明 secret: true` | `CONFIG_INVALID` | A config value written as `{ existingSecret, key }` on a property that isn't declared `secret: true` — the shape is silently not injected, this names it |`
`| `existingSecret 只在 K8s 生效，当前是 docker 目标` | `CONFIG_INVALID` | Docker has no concept of referencing an externally-created Secret; write the value directly (literal or `${VAR}`) |`
zh 对应两行同理，措辞照抄 CLI 实际打出的标题。

- [ ] **Step 6: 用真实输出准备新文档的素材（密钥流向部分）**

在 scratchpad 里搭一个项目（步骤都是本次评判时真跑过的）：

```bash
S="$SCRATCH"; rm -rf "$S/secrets-demo" && mkdir -p "$S/secrets-demo" && cd "$S/secrets-demo"
go build -o "$S/brickkit" /home/zhijie/Desktop/github/brickKit/cmd/brickkit
"$S/brickkit" init demo --no-skills
"$S/brickkit" new acme/hello
# 编辑 components/acme/hello/component.yaml：
#   在 deployment: 之前加
#     dependencies:
#       resources:
#         - kind: database
#           engine: postgresql
#     configSchema:
#       type: object
#       properties:
#         apiKey:
#           type: string
#           secret: true
# 编辑 brickkit.yaml：components 里 acme/hello 加 config: { apiKey: ${THIRD_PARTY_KEY} }；
#   resources 里加一个 database 资源（id: main-db，password: ${PG_PASSWORD}），绑定 acme/hello
printf 'PG_PASSWORD=pw-from-dotenv\nTHIRD_PARTY_KEY=key-from-dotenv\n' > .env
"$S/brickkit" add --local
```

然后**分别**跑并留下输出（文档里用）：
① Docker、值在**进程环境**：`PG_PASSWORD=pw-from-env THIRD_PARTY_KEY=key-from-env "$S/brickkit" up --dry-run`，再 `grep -n "PASSWORD\|API_KEY" .brickkit/generated/docker-compose.yaml`——应看到 `${PG_PASSWORD}` / `${THIRD_PARTY_KEY}` 占位符，看不到 `pw-from-env`。
② K8s（把 `deploy.target` 改成 `k8s`）：`env -u PG_PASSWORD -u THIRD_PARTY_KEY "$S/brickkit" up --dry-run`，再
`ls -l .brickkit/generated/k8s/secrets`（应是 `-rw-------`）、`cat .brickkit/generated/k8s/secrets/config-secrets.yaml`、`grep -n -B1 -A4 "API_KEY" .brickkit/generated/k8s/deployments/acme-hello-0-1-0.yaml`（应是 `secretKeyRef`）。
③ 确认 `docker compose config` 是否展开占位符：`docker compose -f .brickkit/generated/docker-compose.yaml --project-directory . config | grep -n "API_KEY"`（本机有 Docker 才跑；**按实测写**，别凭印象）。
④ **existingSecret**：把 `brickkit.yaml` 里的 `password: ${PG_PASSWORD}` 换成 `existingSecret: acme-db-vault-synced`，
把 `config.apiKey` 换成两键对象写法，`"$S/brickkit" up --dry-run`，确认 `.brickkit/generated/k8s/secrets/` 下两份 Secret
文件都不再包含这两条（`grep -rn "vault-synced\|api-key" .brickkit/generated/k8s/secrets/` 应该找不到值），
`grep -n -B1 -A4 "API_KEY\|DATABASE_PASSWORD" .brickkit/generated/k8s/deployments/acme-hello-0-1-0.yaml` 里的 `secretKeyRef.name`
应该分别是 `acme-db-vault-synced` 与使用者给的名字。

- [ ] **Step 7: 写 `docs/en/07-patterns/10-secrets.md`**

标题 `# Secrets: where they live, where they end up, and how a secret manager plugs in`。按下面的节写，每节的要求都是必须有的内容：

1. **What a secret is, and why it's kept apart** —— 大白话：钥匙和门牌号的区别；密钥一旦进了 Git 历史就删不掉；分开对待的意思是"值只出现在需要它的那一处"。
2. **Where a secret lives in BrickKit** —— 一张表：`resources[].password`（一律是密钥）、组件 `configSchema` 里写了 `secret: true` 的配置项、`sources[].authToken` 等 CLI 自己用的（不进部署文件）。
   `brickkit.yaml` 里永远只放 `${VAR}` 引用（或 `existingSecret` 引用）；真值在进程环境或项目根的 `.env`（进程环境优先，`.env` 其次；`.env` 必须在 `.gitignore` 里）。
3. **Where it ends up** —— Docker / K8s / local-debug 三行表 + Step 6 ①② 的真实输出块。要点：Docker 的 compose 文件里永远是占位符，
   由 `docker compose` 启动时求值；K8s 生成时求值、进 Secret（0600，在 `.brickkit/generated/` 下，可再生、在 `.gitignore` 里）、Deployment 只有 `secretKeyRef`；
   local-debug 文件是给 IDE 读的，本来就是明文。Step 6 ③ 的实测结论如实写（比如"`docker compose config` 会把占位符展开打印出来，别把它的输出贴进工单"，若实测如此）。
4. **Two ways to plug in a secret manager** ——
   ① **Put the value in the process environment, then `brickkit up`.** 机制只有一句：**把值放进进程环境，再 `brickkit up`**。
   用一条真能跑的写法演示，如 `PG_PASSWORD="$(cat /run/secrets/pg-password)" brickkit up`；然后说明"任何能把值放进环境变量的工具都行，BrickKit 不认识其中任何一个"。
   **不写没跑过的具体工具命令。**
   ② **`existingSecret`：reference a Secret something else already created.** 用 Step 6 ④ 的真实输出演示 `resources[].existingSecret` 与
   `{ existingSecret, key }` 两种写法；说明它与①的区别——①是"把值交给 `brickkit up`"，②是"把整个 Secret 对象交给 K8s，CLI 从不接触值"；
   两者都不需要 CLI 认识 Vault/AWS 是什么。
   再讲 BrickKit 为什么不自己去调 SDK 取值（链接 `../06-architecture/00-overview.md` 的第 18 条）：一种存储一个 SDK、`--dry-run` 也得带着凭据联网、它是"配置中心"的邻居。
5. **Honest boundaries** —— ① CLI 生成的 Secret 清单是明文落盘（0600、可再生），CI 里别把 `.brickkit/generated/` 当产物上传；
   ② `existingSecret` 不校验指向的 Secret 在集群里真的存在、真的有那个 key——那是 K8s 自己在 Pod 启动时的事；
   ③ 写字面值会被警告，`secret: true` 的项不论名字都会；④ Docker 下 `secret: true`/`existingSecret` 都不改变行为（前者因为占位符本来就不进文件，后者因为 Docker 没有对应概念，会警告并当作未配置）；
   ⑤ 不支持"整份 `config` 从一个 Secret 灌入"（`envFrom` 语义）——逐项引用保留了"变量名双向可推导"这条设计。

- [ ] **Step 8: 写 `docs/zh/07-patterns/10-secrets.md`**

同样的五节，独立撰写（不是逐句互译），标题 `# 密钥：住在哪、最后落在哪、怎么接密钥管理器`。同样不预设读者背景、同样只写实测过的说法；输出块与英文版**逐字相同**（CLI 打印的本来就是中文）。

- [ ] **Step 9: 同步清单（新增一篇 patterns 文档要动的地方）**

用 `git grep -n -E "08-shared-connection-pools|shared-connection-pools" -- ':!docs/archive' ':!docs/superpowers'` 找出每一处列了 patterns 文档的地方，在 `09-deployment` 之前（编号顺序）加上 `10-secrets`。预期至少有：
`docs/{en,zh}/07-patterns/README.md`、`docs/{en,zh}/README.md`、`docs/README.md`、`llms.txt`、`llms.zh.txt`、`AGENTS.md` §11.2 与 `AGENTS.zh.md` 对应表、根 `README.md` / `README.zh.md` 的"接下来去哪"表（若列了 patterns）。
另外在 `AGENTS.md` §11.2 的表里加一行（`AGENTS.zh.md` 同）：

```
| Where secrets live and end up on each deploy target, the two ways a secret manager plugs in (process environment vs. `existingSecret`), and the honest limits | `docs/en/07-patterns/10-secrets.md` (swap `en` for `zh`) |
```

之后 `git grep -n "10-secrets"` 应在上面每处都命中；`make check-docs`、`make check-docs-bilingual` 会替你兜底。

- [ ] **Step 10: 全量验证并提交**

Run: `go test ./... -count=1` 与 `make lint > "$SCRATCH/lint.log" 2>&1; echo "exit=$?"` → 全绿。

```bash
git add -A docs AGENTS.md AGENTS.zh.md README.md README.zh.md llms.txt llms.zh.txt
git commit -m "$(cat <<'EOF'
文档：密钥怎么流——新增《密钥》专文，补 secret / existingSecret 的参考与骨架

- 07-patterns/10-secrets.md（中英）：密钥住在哪、三个落点各自怎么处理、两种接密钥管理器的方式
  （进程环境 vs. existingSecret）、如实交代的边界
- component.yaml / brickkit.yaml 参考与 AGENTS 骨架补 secret / existingSecret；
  AGENTS §5.2 补"密钥走不同的路"
- 环境变量、部署生成、外壳指南、错误码表按新行为更正

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: 契约先行配方（指南 07 新增一节）

**背景（实测，见 Spec §2.5）：** "上游还没做好"用现有零件就能接通：`brickkit new <id> --contract openapi` 立桩 →
`add --local` → 桩标 `local: true` + `localPort` → 消费方拿到 `ERP_BACKEND_ENDPOINT=http://erp-backend-0-1-0:18081` 与
`extra_hosts: erp-backend-0-1-0:host-gateway`，主机上任何 mock 工具监听该端口即可。缺的只是文档。

**Files:**
- Modify: `docs/en/03-guide/07-consuming-artifacts.md`、`docs/zh/03-guide/07-consuming-artifacts.md`（在"下一篇"链之前加一节）
- Modify: `scripts/check-guide-output.py`（新增 `!set-version` 步骤 + 3 个场景）
- Modify: `AGENTS.md`、`AGENTS.zh.md` 的 §10 表（加一行）

**Interfaces:**
- Consumes：`brickkit new … --contract openapi`、`!copy-into`、`!local-debug`（脚本里已有）。
- Produces：脚本步骤 `!set-version <目录> <版本>`。

- [ ] **Step 1: 脚本加步骤与场景（先于文档，让它因"找不到锚点"而硬失败）**

`scripts/check-guide-output.py`：在 `def disable(...)` 之前加：

```python
def set_version(proj, dir_rel, version):
    """把 <proj>/<dir_rel>/component.yaml 里的 version 改掉（brickkit new 出来的骨架默认是 0.1.0）。"""
    path = os.path.join(proj, dir_rel, "component.yaml")
    s = open(path, encoding="utf-8").read()
    old = "  version: 0.1.0\n"
    if old not in s:
        sys.exit(f"❌ {path} 里找不到 version: 0.1.0，无法改成 {version}")
    open(path, "w", encoding="utf-8").write(s.replace(old, f"  version: {version}\n", 1))
```

`main()` 里的步骤分发（`elif step.startswith("!disable ")` 之前）加：

```python
                elif step.startswith("!set-version "):
                    _, dir_rel, version = step.split(None, 2)
                    set_version(proj, dir_rel, version)
```

`CASES` 里，紧跟 `"07 fetch 只下产物、不动配置"` 那个场景之后加三个：

```python
    {
        "what": "07 上游还没好：new 出桩",
        "reset": True,
        "run": ["init hello-world --no-skills", "!copy-into components/demo/caller demo-caller"],
        "file": "07-consuming-artifacts.md",
        "check": ("new demo/hello --contract openapi", "✅ 已生成组件骨架：demo/hello", 0),
    },
    {
        "what": "07 桩与消费方一起 add --local",
        "run": ["!set-version components/demo/hello 1.0.0"],
        "file": "07-consuming-artifacts.md",
        "check": ("add --local", "🔍 从本地安装源 local-dev 扫到 2 个组件", 0),
    },
    {
        "what": "07 桩接成 local 之后的 dry-run",
        "run": ["!local-debug demo/hello 18081"],
        "file": "07-consuming-artifacts.md",
        "check": ("up --dry-run", "🚀 启动项目 hello-world（deploy.target: docker）", 0),
    },
```

同时把该脚本文件头说明里 `07（部分）` 的措辞补上"（含契约先行一节）"。

Run: `make build-cli && python3 scripts/check-guide-output.py`
Expected: 硬失败（退出码 2），报第一个新场景"找不到锚点 `✅ 已生成组件骨架：demo/hello`"——这证明脚本确实会盯着它。

- [ ] **Step 2: 真跑一遍，拿到输出**

```bash
S="$SCRATCH"; rm -rf "$S/contract-first" && mkdir -p "$S/contract-first" && cd "$S/contract-first"
"$S/brickkit" init hello-world --no-skills
cp -r /home/zhijie/Desktop/github/brickKit/tests/components/demo-caller components/demo/caller 2>/dev/null || { mkdir -p components/demo && cp -r /home/zhijie/Desktop/github/brickKit/tests/components/demo-caller components/demo/caller; }
"$S/brickkit" new demo/hello --contract openapi
sed -i 's/^  version: 0.1.0$/  version: 1.0.0/' components/demo/hello/component.yaml
"$S/brickkit" add --local
# 在 brickkit.yaml 里给 demo/hello 加 local: true 与 localPort: 18081（与 !local-debug 做的事一致）
"$S/brickkit" up --dry-run
grep -n "DEMO_HELLO_ENDPOINT\|extra_hosts\|demo-hello-1-0-0" .brickkit/generated/docker-compose.yaml
```

（脚本里的 `copy_into` 用 `tests/components/<slug>` 拷到 `components/demo/caller`，上面 `cp` 与之等价。）
把三条命令的输出**逐字**留着，写文档用。

- [ ] **Step 3: 写英文与中文的新一节**

`docs/en/03-guide/07-consuming-artifacts.md` 在文末"下一篇"链之前加一节 `## When the upstream isn't ready yet: stand in a stub`
（`docs/zh/…` 写 `## 上游还没做好：先立一个桩`），独立撰写，内容要求：

1. 场景（大白话）：你的组件依赖 `demo/hello@1.0.0`，但那个组件还没做好/没发布。两个团队先约定了接口契约，你不想干等。
2. `brickkit new demo/hello --contract openapi` ——它按 `component.yaml` 骨架 + `api/openapi.yaml` 占位契约立一个**桩**（stub），并把契约登记进 `artifacts`（贴 Step 2 的真实输出）。
   桩的 `version` 要改成消费方依赖的那个版本（示例里从 `0.1.0` 改成 `1.0.0`）。
3. `brickkit add --local`——桩和消费方一起被加进来（贴真实输出）。
4. 在 `brickkit.yaml` 里给桩加 `local: true` 与 `localPort: 18081`（贴 YAML 片段），意思是"这个组件不生成容器，我自己在主机上跑它"；
   `brickkit up --dry-run` 的真实输出里能看到"不生成容器；请在 IDE 里启动它，监听 localhost:18081"（贴，可用 `...` 省略无关行）。
5. 在主机上的 18081 起任意一个能按契约返回假数据的 mock 工具。**这一步 BrickKit 不管**——说清楚为什么：平台不解析契约内容，
   `artifacts.format` 只是一个字符串；把"按契约做 mock"做进平台，就得懂 OpenAPI、protobuf、gRPC，永远追不完。
6. 消费方看到什么：`grep` 生成的 `docker-compose.yaml`，贴 `DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:18081` 与 `extra_hosts: - demo-hello-1-0-0:host-gateway`
   （**这一块脚本核对不了**——它不是 brickkit 命令的输出；文中写明"这一步的输出取自本机真实运行"）。
7. 想跑端到端（消费方容器真的打到主机上的 mock）需要 Docker：**本机有 Docker 就真跑一次**，按指南 03 的做法起 `demo/caller` 的镜像、
   在 18081 起一个返回固定 JSON 的 `python3` 小服务，抄真实输出；没有 Docker 就只写脚本能核对的部分，并在文中明说"这一步要 Docker"。
8. 两句话说清**为什么没有 `brickkit mock` 命令、也没有 `up --with-mocks`**：平台不解析契约；自动把缺失的强依赖换成替身违反
   "强依赖缺失就阻断"与"显式优于隐式"（一次误用就把替身部署上生产）；mock 起在别的名字下也接不到流量，因为注入的地址指向真实组件的版本化服务名。
9. 收尾一句：真的上游好了怎么换回来——删掉 `components/demo/hello/` 那份桩与 `local: true`，`brickkit add demo/hello@1.0.0` 从真实来源装。

- [ ] **Step 4: 跑脚本，做变异测试**

Run: `python3 scripts/check-guide-output.py`
Expected: `✅ 教程里的 CLI 输出：N 个场景逐行一致，docker/en 与 docs/zh 抄的是同一份`（N 比之前多 3）。

变异测试（**必做**，脚本的价值就在它真会失败）：
① 在 `docs/en/03-guide/07-consuming-artifacts.md` 的新一节里把某个输出块里的一行中文改一个字，跑脚本，应报"教程预期输出对不上"，再还原；
② 只改 `docs/zh/…` 那一份的同一处，跑脚本，应报"docs/en 与 docs/zh 抄的不是同一份输出"，再还原。
`git diff --stat` 确认还原后文档只有新增的那一节。

- [ ] **Step 5: AGENTS §10 加一行（AI 读者）**

`AGENTS.md` §10 的表里（`A user asks "how do I run everything locally without Docker/K8s at all"` 那一行之前）加：

```
| A user's upstream component isn't built or published yet and they ask for a mock, or for the CLI to substitute one | Not a platform feature — the platform never parses contracts and never swaps in a stand-in for a missing required dependency (§4.1). The path that already works: `brickkit new <id> --contract openapi` for a stub carrying the agreed contract, `add --local`, `local: true` + `localPort` on the stub, and any mock tool listening on that port. Walkthrough with real output: `docs/en/03-guide/07-consuming-artifacts.md` |
```

`AGENTS.zh.md` §10 同位置：

```
| 用户的上游组件还没做好/没发布，问能不能给个 mock、或让 CLI 自动替换一个 | 不是平台功能——平台从不解析契约，也从不给缺失的强依赖换上替身（§4.1）。现成能走通的路：`brickkit new <id> --contract openapi` 立一个带约定契约的桩、`add --local`、给桩加 `local: true` + `localPort`、主机上任意 mock 工具监听那个端口。带真实输出的演示：`docs/zh/03-guide/07-consuming-artifacts.md` |
```

- [ ] **Step 6: 全量验证并提交**

Run: `go test ./... -count=1` 与 `make lint > "$SCRATCH/lint.log" 2>&1; echo "exit=$?"` → 全绿（`make lint` 含 `check-guide-output`、`check-cli-docs`）。
`check-cli-docs` 会看同一行里 `brickkit <命令>` 之后的 `--参数`：`brickkit new demo/hello --contract openapi` 是合法的（`new` 有 `--contract`）；别的工具的参数单独放一行。

```bash
git add docs scripts AGENTS.md AGENTS.zh.md
git commit -m "$(cat <<'EOF'
指南 07 新增"上游还没做好"一节：用 new --contract 立桩 + local: true 接 mock

需求用现有零件就能满足，缺的是文档：brickkit new <id> --contract openapi 立桩、
add --local、给桩加 local: true + localPort，消费方拿到指向桩的 *_ENDPOINT，
主机上任何 mock 工具监听该端口即可。确定性的三步（new / add --local / up --dry-run）
进 check-guide-output.py，并新增 !set-version 步骤。

同时在 AGENTS §10 记一行：平台不做 mock 生成，也不自动替换缺失的强依赖。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: 双环境对比配方 + 把被否决的想法记进拒绝清单

**Files:**
- Modify: `docs/en/07-patterns/05-deployment-selection-guide.md`、`docs/zh/…`（多环境那条，英文版约第 731 行）
- Modify: `AGENTS.md`、`AGENTS.zh.md`（§4.1 拒绝清单各加 4 行，"平台代取外部密钥"这一行要带 `existingSecret` 的 carve-out）
- Modify: `docs/en/06-architecture/00-overview.md`、`docs/zh/…`（"刻意不做"：索引表 + 编号详解 18–21；第 8 条"What to do instead"补一句指路）

- [ ] **Step 1: 跑出双环境对比的真实输出**

```bash
S="$SCRATCH"; rm -rf "$S/two-envs" && mkdir -p "$S/two-envs" && cd "$S/two-envs"
"$S/brickkit" init shop --no-skills
"$S/brickkit" new erp/backend --contract openapi
"$S/brickkit" new acme/web
# 编辑 components/acme/web/component.yaml：在 deployment: 之前加
#   dependencies:
#     components:
#       - erp/backend@0.1.0
"$S/brickkit" add --local
cp brickkit.yaml brickkit.dev.yaml
# 生成 brickkit.prod.yaml：deploy.target 改 k8s，erp/backend 加 enabled: false
diff -u brickkit.dev.yaml brickkit.prod.yaml
diff <("$S/brickkit" up --dry-run --config brickkit.dev.yaml 2>&1 | grep -v '^{') \
     <("$S/brickkit" up --dry-run --config brickkit.prod.yaml 2>&1 | grep -v '^{')
```

（评判时真跑过：第二条 `diff` 的输出里出现 `⬜ acme/web@0.1.0  不启动（强依赖 erp/backend 不启动）`——**后果**，而不只是那行 `enabled: false`。）
把两条 `diff` 的输出留着。注意 `grep -v '^{'` 是滤掉 stderr 上的 JSON 日志行；命令行写进文档时把这个细节讲清。

- [ ] **Step 2: 部署选型指南补配方**

`docs/en/07-patterns/05-deployment-selection-guide.md` 的 "Multiple environments (say, a `brickkit.prod.yaml` …)" 那条要点之后，加一小段
（`docs/zh/…` 同位置写中文，独立撰写）：

- 一句话：每份环境文件自包含，所以对比它们不需要任何专门命令——两步就够。
- ① `diff -u brickkit.dev.yaml brickkit.prod.yaml` 看**你写了什么不同**。
- ② `diff <(brickkit up --dry-run --config brickkit.dev.yaml) <(brickkit up --dry-run --config brickkit.prod.yaml)` 看**这些不同的后果**——
  哪些组件因此不启动、为什么（贴 Step 1 的真实输出）。
- 提醒两点（都是实测出来的）：`--dry-run` 每次会覆盖 `.brickkit/generated/`，所以想留下某一份就最后再跑那一份；
  `grep -v '^{'` 是为了滤掉 stderr 上的 JSON 日志行。
- 一句诚实的话：这只比"启动决策"与文本，不比渲染出来的部署文件——dev 是 docker、prod 是 k8s 时那两份文件本来就不可比。
  这里没有专门的 `diff` 命令，是有意的暂缓，不是遗漏。

- [ ] **Step 3: AGENTS §4.1 加 4 行（AI 读者，英文版）**

`AGENTS.md` §4.1 表格末尾（Podman 那行之后）加：

```
| Fetching secrets from an external store (Vault / AWS Secrets Manager SDKs) on the platform's behalf | `${VAR}` is looked up in the process environment first, `.env` second — anything that can put the value in the environment works today with zero platform code. Built in, it would mean an SDK per store, store credentials and network access on every `up` (`--dry-run` included), and a neighbour of the rejected "config center". **What is supported:** `resources[].existingSecret` and a `secret: true` config value written as `{ existingSecret, key }` reference a Secret an external system (Vault Secrets Operator, External Secrets Operator, Sealed Secrets, …) already put in the cluster — the platform never reads or writes the value either way, K8s only (§5.2) |
| Engine plugins / third-party deploy targets (`brickkit up --engine nomad`) | A target's `Down`/`Status`/orphan-pruning guarantees are what make "a project that can be torn down" true; a plugin would own them while the CLI reported success on its behalf — the same reason Podman was pulled. `deploy.target` in `brickkit.yaml` stays the declaration, never a CLI flag. New targets are built in-tree, with the full test guard set. (`engine.Engine` is already an interface; this is about who guarantees its semantics, not about code layout) |
| Incremental generation cache (`.brickkit/` hash state) | Nothing to speed up: generating 50 components through the whole pipeline takes about 2 ms (`tests/perf`), and the time users wait on is `docker compose up` / `kubectl apply`, which already touch only what changed. A cache adds state whose staleness silently produces wrong deployment files |
| Mock generation from contracts (`brickkit mock`) and auto-substituting missing required dependencies (`up --with-mocks`) | The platform never parses contracts (`artifacts.format` is a free string); a stand-in swapped in for a missing required dependency contradicts "missing required dependency blocks startup" and could be deployed by mistake; a mock under another name receives no traffic because injected addresses point at the real component's versioned service name. What works today: `brickkit new <id> --contract openapi` + `local: true` + any mock tool (`docs/en/03-guide/07-consuming-artifacts.md`) |
```

`AGENTS.zh.md` §4.1 同位置加对应 4 行（中文独立撰写，含义一致；引用链接指向 `docs/zh/…`）：

```
| 平台代为从外部密钥存储（Vault / AWS Secrets Manager 的 SDK）取值 | `${VAR}` 先查进程环境、再查 `.env`——任何能把值放进环境的工具今天就能接入，平台零代码。内置的话，每接一种存储就多一个 SDK，每次 `up`（含 `--dry-run`）都要带存储凭据并联网，还是被否决的"配置中心"的邻居。**已经支持的：** `resources[].existingSecret` 与 `secret: true` 配置项写成 `{ existingSecret, key }`，引用外部系统（Vault Secrets Operator、External Secrets Operator、Sealed Secrets……）已经放进集群的 Secret——两种写法平台都不读写值本身，仅 K8s（§5.2） |
| 引擎插件 / 第三方部署目标（`brickkit up --engine nomad`） | 一个目标的 `Down` / `Status` / 孤儿清理保证，才让"一个能拆干净的项目"成立；插件要自己担保它们，而 CLI 会替它报"成功"——撤掉 Podman 的同一个理由。`deploy.target` 是 `brickkit.yaml` 里的声明，绝不变成命令行参数。新目标在仓库内实现，带全套测试守卫。（`engine.Engine` 本来就是接口；这里说的是谁来担保它的语义，不是代码怎么分层） |
| 增量生成缓存（`.brickkit/` 里存哈希状态） | 没有可加速的东西：50 个组件走完整条链路约 2 ms（`tests/perf`），使用者真正在等的是 `docker compose up` / `kubectl apply`，而它们本来就只动有变化的。缓存要维护状态，过期时静默产出错误的部署文件 |
| 按契约生成 mock（`brickkit mock`）、自动替换缺失的强依赖（`up --with-mocks`） | 平台从不解析契约（`artifacts.format` 只是个字符串）；给缺失的强依赖换上替身，违反"强依赖缺失就阻断启动"，还可能被误部署；mock 起在另一个名字下接不到流量，因为注入的地址指向真实组件的版本化服务名。现在就能用的：`brickkit new <id> --contract openapi` + `local: true` + 任意 mock 工具（`docs/zh/03-guide/07-consuming-artifacts.md`） |
```

- [ ] **Step 4: 概览文档的"刻意不做"补 18–21**

`docs/en/06-architecture/00-overview.md`：
① 在索引表里，按主题放进现有分组——18 放 **D. Security and distribution**，19、20、21 放 **E. Deployment shape and scope**（放在 `17` 那行之前也可以，编号保持 18–21 不动、不重排现有 1–17，避免改坏页外锚点）；每行格式照现有行（`[N. 标题](#N-锚点)<br>一句话是什么 | 为什么不做 | 改用什么`）。
② 在第 17 条详解之后按现有条目的三段格式（`What it is` / `Why it doesn't` / `What to do instead`）追加四条详解：

- `### 18. Fetching secrets from an external store` —— *What it is:* the CLI calling Vault / AWS Secrets Manager SDKs at `up` time to fetch values and turn them into Secrets. *Why it doesn't:* an SDK and its credentials per store, network access on every `up` including `--dry-run`, and a neighbour of the rejected config center; `${VAR}` already reads the process environment first. *What to do instead:* put the value in the environment with whatever tool you use, then `brickkit up`; declare `secret: true` in `configSchema` so a config credential stays out of Deployments; or, for a Secret an external system already created, reference it directly with `resources[].existingSecret` or a `secret: true` config value's `{ existingSecret, key }` form — see [Secrets](../07-patterns/10-secrets.md).
- `### 19. Engine plugins and third-party deploy targets` —— *What it is:* an interface external programs implement so `brickkit up --engine nomad` deploys somewhere the platform doesn't know. *Why it doesn't:* a target's tear-down, status and orphan-cleanup guarantees are the reason "a project can be torn down" is true, and a plugin would own them while the CLI reported success (the Podman lesson, entry 16); a CLI flag would also bypass `deploy.target`, the declaration. *What to do instead:* build a new target in-tree with the full test guard set.
- `### 20. An incremental generation cache` —— *What it is:* remembering hashes under `.brickkit/` so `up` regenerates only what changed. *Why it doesn't:* generating 50 components takes about 2 ms; what users wait for is `docker compose up` / `kubectl apply`, which already touch only what changed; a cache that goes stale silently produces wrong deployment files. *What to do instead:* nothing — there is nothing to speed up. Measure first (`go test ./tests/perf -bench .`); reopen it only if a real project shows generation above ~100 ms.
- `### 21. Generated mocks and substituting missing dependencies` —— *What it is:* `brickkit mock` building a fake server from a component's contract, and `up --with-mocks` swapping it in for a required dependency that isn't there. *Why it doesn't:* the platform never parses contracts; a stand-in swapped in silently contradicts "a missing required dependency blocks startup"; and a mock under its own name gets no traffic because injected addresses point at the real component's versioned service name. *What to do instead:* `brickkit new <id> --contract openapi` for a stub, `local: true` + `localPort`, and any mock tool listening on that port — [walkthrough](../03-guide/07-consuming-artifacts.md).

第 8 条（Multi-environment overlays）的 *What to do instead* 末尾补一句：`To see how two environment files differ — and what the differences do — see the two-line recipe in [Choosing a deployment shape](../07-patterns/05-deployment-selection-guide.md).`

`docs/zh/06-architecture/00-overview.md` 同样处理（中文独立撰写，格式照该文件现有的中文条目；含义一致，第 18 条同样要带 `existingSecret` 的 carve-out）。

- [ ] **Step 5: 校验并提交**

Run: `make check-docs check-docs-bilingual check-cli-docs`（页内锚点、双语镜像、命令引用），再 `go test ./tests/docfields/... -count=1`。
然后 `git grep -n -E "#(18|19|20|21)-" docs/en/06-architecture/00-overview.md` 确认索引表里的每个锚点都在正文里有对应标题（GitHub slug：小写、去标点、空格变连字符）。
Run: `go test ./... -count=1` 与 `make lint > "$SCRATCH/lint.log" 2>&1; echo "exit=$?"` → 全绿。

```bash
git add docs AGENTS.md AGENTS.zh.md
git commit -m "$(cat <<'EOF'
文档：双环境对比配方；把四条被否决的想法记进拒绝清单

- 部署选型指南：对比两份环境配置只要 diff -u 加两份 up --dry-run 输出的 diff，
  后者能直接看到"prod 里误关一个组件、谁跟着停了"
- AGENTS §4.1 与概览"刻意不做"新增 18–21：平台代取外部密钥（附 existingSecret 的
  carve-out）、引擎插件、增量生成缓存、契约 mock 生成/自动替换缺失依赖——每条都带着
  实测依据，免得下一个提案再被重新论证一遍

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: 收尾核对

- [ ] **Step 1: 对照 Spec 逐条打勾**

- §3.1 缺口 A → Task 1；缺口 B → Task 2、3。
- §3.2 `existingSecret` → Task 4。
- §3.3 密钥专文 → Task 5。
- §3.4 两份配方 → Task 6、7。
- §3.5 记录被否决的想法 → Task 7。
- §4 范围之外：确认**没有**新增 CLI 命令/参数（`git diff <起点>..HEAD -- internal/cli/*.go | grep -n 'Flags()\|AddCommand'` 应无新增）；
  确认**没有**顺手重构那约 10 处 `Deploy.Target ==` 分支（那是"观察但不处理"，见 Spec §4）。

- [ ] **Step 2: 全量回归与实测复核**

Run: `go test ./... -count=1`；`make lint > "$SCRATCH/lint.log" 2>&1; echo "exit=$?"`；`python3 scripts/check-guide-output.py`。
再把评判时的几个实测场景各跑一遍确认修复：
- Docker + 进程环境（compose 里应是占位符，Spec §2.1 缺口 A）；
- K8s + `.env`（组件声明 `secret: true` 后 `config-secrets.yaml` 里有值、Deployment 里只有 `secretKeyRef`，缺口 B）；
- K8s + `existingSecret`（资源与配置密钥都指向使用者给的名字，平台生成的两份 Secret 文件里都不出现这两条的值）。

- [ ] **Step 3: 收尾**

`git status` 应只剩根目录未跟踪的 `改进计划.md`（不属于本计划，由用户自己删除）。
把 Spec 与本计划留在 `docs/superpowers/` 下（与既有的计划/设计成对，已提交）。
