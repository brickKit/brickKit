# Three-layer refactor P1 — Data model & loader — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Archive the old documentation, then build the complete in-memory model of a three-layer BrickKit project (`brickkit.yaml` + `deploy*.yaml` + `config/`) — types, parsing, validation, value references, conflict detection, config resolution, config skeletons, and a loader that enforces every cross-file rule — as new packages that later phases wire into the commands.

**Architecture:** Six new packages, none imported by existing command code yet (P2 does the switch-over and deletes `internal/config` / `internal/override`):

| Package | Owns |
| --- | --- |
| `internal/envref` | `${VAR}` detection / expansion (strings and YAML node trees) |
| `internal/yamlfile` | the read → YAML → top-level-mapping → decode pipeline and its standard errors, shared by every layer file |
| `internal/projfile` | `brickkit.yaml` v2: project, sources, components (`id`/`version`/`kind`/`source`), installer; project-name rule (moved here from `internal/config`) |
| `internal/deployfile` | `deploy.yaml` / `deploy.local.yaml` / `-f` files: target, `k8s:` block, `vars:`, component deploy entries incl. `members`; single-file validation per **role** (team vs local) |
| `internal/configdir` | `config/` directory: file naming, `Value` model (literal / `$var:` / `${VAR}` / `file://` / existingSecret), flat-file parsing with duplicate-key **conflict** detection and the marker format P6 writes, precedence resolution against a `configSchema`, skeleton generation |
| `internal/project` | directory `Layout`, local-mode on/off state, `Load` (deploy-file selection strategy + all cross-file consistency checks) producing one `*project.Project` aggregate |

Design patterns used deliberately: **Aggregate/Facade** (`project.Project` is the single object every later command consumes — no command re-reads layer files itself), **Strategy** (deploy-file selection: explicit `-f` > local mode > team file, isolated in one function), **Value Object** (`configdir.Value` carries *what kind of reference* a value is, so renderers in P2 decide per target when to evaluate it instead of the parser guessing).

**Tech Stack:** Go 1.22+, `gopkg.in/yaml.v3`, `github.com/stretchr/testify`, existing `internal/clierr` / `internal/i18n` + `internal/msgid` / `internal/yamlcheck` / `internal/yamlcomment` / `internal/manifest`.

**Spec:** `new_plan/提案.md` §4–§7, §8.4 (members as the single source), §12.3 (conflict marker), §9.3 (component `source`), Appendix A (A1, A4, A5, A8, A10, A11, A15, A16 bind this plan). Roadmap: `docs/superpowers/plans/2026-09-26-three-layer-refactor-roadmap.md`.

## Global Constraints

- **File names (fixed):** `brickkit.yaml`, `deploy.yaml`, `deploy.local.yaml`, `deploy.local.yaml.bak`, `config/`, `config/vars.yaml`, `config/.archive/`, `.brickkit/local-mode`, project `BRICKKIT.md`, `shell/`.
- **`brickkit.yaml` v2 fields only:** `project`, `sources[] {name, type: market|git|local, url (market), baseUrl (git), path (local), authToken, enabled}`, `components[] {id, version, kind: shell (optional), source {type: git|local, repo (git), path (local)}}`, `installer {requireSignature, publicKeys}`. `version` is always an exact `major.minor.patch` — local sources too (A8).
- **Deploy file fields only:** `target: docker|podman|k8s`, `k8s {context, namespace, createNamespace, podSecurity: restricted, imagePullSecrets, ingressClass, ingressAnnotations, networkPolicy, serviceAccount}`, `vars {NAME: value}`, `components[] {id, mode: enabled|disable|local|debug, localPort, expose, exposePort, hostname, tlsSecret, replicas, serviceAccountName, resources, labels, members []}`. A component entry `id` is either a bare ID (applies to every version of that ID without its own entry) or `id@version`.
- **`mode: debug` is legal only in `deploy.local.yaml`** (role local). `deploy.yaml` and any `-f` file reject it.
- **Deploy/declaration consistency is exact** (spec §6.3): every `brickkit.yaml` component is covered by exactly one deploy entry, and no deploy entry covers nothing. Any mismatch is a hard error with `clierr.CodeDeployInconsistent`.
- **Config values:** a value is *unset* when it is YAML null or the empty string — unset never injects anything and a required key that is unset is missing. `$var:NAME` must be the whole value; `vars.yaml` and deploy `vars:` may not contain `$var:` (no chaining). Deploy `vars:` override `vars.yaml` entries of the same name, and only for `$var:` lookups — they never override a component key directly.
- **Config file for a component with several versions in `brickkit.yaml`:** `config/<base>@<version>.yaml` if present; the unversioned `config/<base>.yaml` is used only when that ID has exactly one version — with several versions an unversioned file is an ambiguity error (A5).
- **Conflict marker (the contract P6's writer must produce):** duplicate keys, each line ending in a comment containing `brickkit:conflict <side> <version>` with side `current` or `proposed`. Duplicate keys with or without the marker are always a hard error (`clierr.CodeConfigConflict`).
- **i18n discipline:** every user-visible string is an `internal/msgid` constant with an entry in **both** `internal/i18n/catalog_en.go` and `internal/i18n/catalog_zh.go`. No Chinese string literals in production code (comments are fine — the codebase comments in Chinese). `go test ./tests/i18nguard/...` must pass after every task.
- **Do not modify** `internal/config`, `internal/override`, or any command code in this plan, except the one mechanical move in Task 3 (project-name rule) which keeps `internal/config` compiling through thin wrappers.
- **Formatting:** run `gofmt -w` on every Go file a task touches (the msgid const blocks and catalog entries in this plan are not column-aligned; gofmt aligns them).
- **Per-task verification** (collateral failures elsewhere are allowed by the roadmap rules): `go build ./...`, `go vet ./internal/<pkg>/...`, `go test ./internal/<pkg>/...`, `.tools/bin/golangci-lint run ./internal/<pkg>/...`, `go test ./tests/i18nguard/...`.
- **Commits** go straight to `main`; every message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **A teammate adds a component to `brickkit.yaml` while you have local mode on.** `project.Load` must fail loudly naming the missing component and offering the three fixes (`local refresh` / edit by hand / `local off`), never silently start with a stale `deploy.local.yaml`. (Task 8, `TestLoadLocalFileStale`)
2. **`$var:` referencing something defined nowhere, and `deploy.yaml` `vars:` overriding `vars.yaml`.** Undefined → one error listing every dangling reference with its file and key; defined in both → the deploy file's value wins. (Task 6 `TestResolveVarPrecedence`, Task 8 `TestLoadUndefinedVarRef`)
3. **A config file with a duplicated key the user typed by accident (no marker)** must fail as loudly as a BrickKit-written conflict, and the error must name the file, key and both lines. (Task 5, `TestParseComponentFileUnmarkedDuplicate`)
4. **A component with two versions in `brickkit.yaml` and only `config/<base>.yaml`** must be an ambiguity error, not silently feed the same file to both versions. (Task 8, `TestLoadConfigAmbiguousAcrossVersions`)
5. **The skeleton's `KEY: ""` for a required key left untouched** must count as missing (blocks startup) — not inject an empty string. (Task 6, `TestResolveEmptyRequiredIsMissing`)

---

## Task 1: Archive the old documentation, create the new skeleton

**Files:**
- Move: `docs/archive/` → `archive/v1/`
- Move: `docs/en/` → `archive/v2/en/`, `docs/zh/` → `archive/v2/zh/`
- Create: `archive/README.md`
- Create: `docs/en/README.md`, `docs/zh/README.md`, and `docs/{en,zh}/<module>/.gitkeep` for the twelve modules
- Create: `tutorials/en/.gitkeep`, `tutorials/zh/.gitkeep`
- Modify: `AGENTS.md`, `AGENTS.zh.md` (a refactor banner at the very top)
- Add to git: `new_plan/` (the spec travels with the plans from now on)

`docs/superpowers/` is the development-process folder (plans/specs) — it is **not** documentation and is **not** moved.

**Interfaces:** none (no code).

- [ ] **Step 1: Move the trees with git so history follows them**

```bash
mkdir -p archive/v2
git mv docs/archive archive/v1
git mv docs/en archive/v2/en
git mv docs/zh archive/v2/zh
```

- [ ] **Step 2: Write `archive/README.md`**

```markdown
# Archive / 归档

Historical material. **Not current documentation** — nothing here is kept in sync with the CLI, and current
documents must not link here.

仅供查阅的历史资料，**不是现行文档**：这里的内容不再与 CLI 保持同步，现行文档也不得链接到这里。

| Path | What it is |
| --- | --- |
| `v1/` | the former `docs/archive/`: design books, decision records, planning notes |
| `v2/en/`, `v2/zh/` | the documentation as it was before the three-layer refactor (2026-09-26) |
```

- [ ] **Step 3: Create the new skeleton directories**

```bash
for lang in en zh; do
  for m in 00-intro 01-three-layers 02-project-guide 03-component-guide 04-shell 05-migration \
           06-architecture 07-cli-reference 08-ai-guide 09-patterns 10-troubleshooting 11-reference; do
    mkdir -p docs/$lang/$m && touch docs/$lang/$m/.gitkeep
  done
  mkdir -p tutorials/$lang && touch tutorials/$lang/.gitkeep
done
```

`docs/en/README.md`:

```markdown
# BrickKit documentation

Being rewritten for the three-layer architecture (`brickkit.yaml` / `deploy.yaml` / `config/`).
The previous documentation is in `archive/v2/en/` for reference only.
```

`docs/zh/README.md`:

```markdown
# BrickKit 文档

正在按三层文件架构（`brickkit.yaml` / `deploy.yaml` / `config/`）重写。
旧版文档在 `archive/v2/zh/`，仅供查阅。
```

- [ ] **Step 4: Put a refactor banner at the top of `AGENTS.md` and `AGENTS.zh.md`**

Insert as the first lines of `AGENTS.md` (before `# BrickKit · AI Reading Guide`):

```markdown
> ⚠️ **Refactor in progress (from 2026-09-26).** This file still describes the *old* architecture
> (single `brickkit.yaml`, `resources`, `servedBy`, `override.yaml`). The target design is
> `new_plan/提案.md` (its Appendix A overrides the body); the phase plan is
> `docs/superpowers/plans/2026-09-26-three-layer-refactor-roadmap.md`. Where they disagree, the new design wins.

```

Insert as the first lines of `AGENTS.zh.md`:

```markdown
> ⚠️ **重构进行中（自 2026-09-26 起）。** 本文件描述的仍是*旧*架构（单一 `brickkit.yaml`、`resources`、
> `servedBy`、`override.yaml`）。目标设计以 `new_plan/提案.md` 为准（其附录 A 优先于正文），分阶段计划见
> `docs/superpowers/plans/2026-09-26-three-layer-refactor-roadmap.md`。两者冲突时以新设计为准。

```

- [ ] **Step 5: Verify the move is complete**

Run: `ls docs docs/en archive archive/v2 && git status --short | head -20`
Expected: `docs/` contains only `en/`, `zh/`, `superpowers/`; `docs/en` contains `README.md` and the twelve module dirs; `archive/` contains `README.md`, `v1/`, `v2/`.

- [ ] **Step 6: Commit**

```bash
git add -A archive docs tutorials AGENTS.md AGENTS.zh.md new_plan
git commit -m "docs: archive pre-refactor documentation, add new doc skeleton and the refactor spec

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

## Task 2: Foundations — error code, shared messages, `envref`, `yamlfile`, `project.Layout`, local-mode state

**Files:**
- Modify: `internal/clierr/clierr.go` (one new code next to `CodeConfigConflict`)
- Create: `internal/msgid/layer.go`
- Modify: `internal/i18n/catalog_en.go`, `internal/i18n/catalog_zh.go`
- Create: `internal/envref/envref.go`, `internal/envref/envref_test.go`
- Create: `internal/yamlfile/yamlfile.go`, `internal/yamlfile/yamlfile_test.go`
- Create: `internal/project/layout.go`, `internal/project/localmode.go`, `internal/project/layout_test.go`

**Interfaces:**
- Consumes: `clierr.New/.WithDetail/.WithHint/.WithCause`, `clierr.ProblemSet`, `i18n.T`.
- Produces:
  - `clierr.CodeDeployInconsistent Code = "DEPLOY_INCONSISTENT"`
  - `envref.Has(s string) bool`, `envref.Names(s string) []string`, `envref.Expand(s string, lookup func(string) (string, bool)) string`, `envref.ExpandOS(s string) string`, `envref.ExpandNode(node *yaml.Node, skip func(path []string) bool)`
  - `yamlfile.Read(path string) ([]byte, error)` — a missing file returns an error satisfying `errors.Is(err, fs.ErrNotExist)`
  - `yamlfile.Document(data []byte, source string, allowEmpty bool) (*yaml.Node, error)` — returns the top-level **mapping** node, or `(nil, nil)` for an empty document when `allowEmpty`
  - `yamlfile.Decode(doc *yaml.Node, out any, p *clierr.ProblemSet) bool`
  - `yamlfile.Lookup(node *yaml.Node, path ...string) *yaml.Node`
  - `yamlfile.RequireSequence(node *yaml.Node, field string, p *clierr.ProblemSet)`, `yamlfile.RequireMapping(node *yaml.Node, field string, p *clierr.ProblemSet)` — `node` may be nil (absent field → no problem); `field` is the full path used in the report
  - `yamlfile.Indexed(field string, i int) string`, `yamlfile.CleanError(err error) string`
  - `project.Layout{Root string}`, `project.NewLayout(root string) Layout` and path methods listed in Step 7
  - `project.LocalModeOn(l Layout) (bool, error)`, `project.SetLocalMode(l Layout, on bool) error`

- [ ] **Step 1: Add the error code**

In `internal/clierr/clierr.go`, directly after `CodeConfigConflict Code = "CONFIG_CONFLICT"`:

```go
	// CodeDeployInconsistent 是"部署文件（deploy.yaml / deploy.local.yaml / -f 指定的文件）
	// 与 brickkit.yaml 的组件集合对不上"：多了、少了都算。
	CodeDeployInconsistent Code = "DEPLOY_INCONSISTENT"
```

- [ ] **Step 2: Add the shared layer messages**

`internal/msgid/layer.go`:

```go
package msgid

// 三层文件（brickkit.yaml / deploy*.yaml / config/）共用的读取与解析文案。
const (
	LayerReadFailed   = "layer.read_failed"
	LayerNotValidYAML = "layer.not_valid_yaml"
	LayerEmpty        = "layer.empty"
)
```

Append to the `en` map in `internal/i18n/catalog_en.go` (before the closing `}`):

```go
	msgid.LayerReadFailed:   "Error: failed to read %[1]s",
	msgid.LayerNotValidYAML: "Error: %[1]s is not valid YAML",
	msgid.LayerEmpty:        "Error: %[1]s is empty",
```

Append to the `zh` map in `internal/i18n/catalog_zh.go`:

```go
	msgid.LayerReadFailed:   "错误：读取 %[1]s 失败",
	msgid.LayerNotValidYAML: "错误：%[1]s 不是合法的 YAML",
	msgid.LayerEmpty:        "错误：%[1]s 是空文件",
```

- [ ] **Step 3: Write the failing `envref` test**

`internal/envref/envref_test.go`:

```go
package envref_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/envref"
)

func lookup(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) { v, ok := values[name]; return v, ok }
}

func TestExpandKeepsUnknownReferences(t *testing.T) {
	got := envref.Expand("pg://${USER}:${MISSING}@db", lookup(map[string]string{"USER": "app"}))
	assert.Equal(t, "pg://app:${MISSING}@db", got)
}

func TestNamesDeduplicatesInOrder(t *testing.T) {
	assert.Equal(t, []string{"B", "A"}, envref.Names("${B}-${A}-${B}"))
	assert.False(t, envref.Has("$var:X"))
	assert.True(t, envref.Has("x${Y}"))
}

func TestExpandNodeHonoursSkip(t *testing.T) {
	t.Setenv("HOST_X", "h")
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: ${HOST_X}\nvars:\n  P: ${HOST_X}\nlist: [\"${HOST_X}\"]\n"), &root))
	envref.ExpandNode(&root, func(path []string) bool { return len(path) > 0 && path[0] == "vars" })

	var out map[string]any
	require.NoError(t, root.Decode(&out))
	assert.Equal(t, "h", out["a"])
	assert.Equal(t, "${HOST_X}", out["vars"].(map[string]any)["P"])
	assert.Equal(t, []any{"h"}, out["list"])
}
```

- [ ] **Step 4: Run it to see it fail**

Run: `go test ./internal/envref/...`
Expected: FAIL — package `envref` does not exist.

- [ ] **Step 5: Implement `envref`**

`internal/envref/envref.go`:

```go
// Package envref 识别并展开值里的 ${VAR} 引用。
//
// 三层文件都用同一种写法引用进程环境变量；"是不是引用、引用了谁、要不要现在展开"
// 在这一处定义，免得 brickkit.yaml、deploy 文件、config/ 各长一份略有出入的正则。
package envref

import (
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// refRe 匹配 ${NAME}：字母或下划线开头，后接字母、数字、下划线（shell 惯例）。
var refRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Has 报告 s 里是否含有 ${VAR} 引用。
func Has(s string) bool { return refRe.MatchString(s) }

// Names 按出现顺序返回 s 里引用到的变量名，去重。
func Names(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range refRe.FindAllStringSubmatch(s, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// Expand 用 lookup 替换 s 里的 ${VAR}。lookup 找不到的引用**原样保留**：
// 生成物里留着 ${VAR} 一眼就能看出漏配了哪个，换成空串则无从查起。
func Expand(s string, lookup func(string) (string, bool)) string {
	if !Has(s) {
		return s
	}
	return refRe.ReplaceAllStringFunc(s, func(match string) string {
		if v, ok := lookup(match[2 : len(match)-1]); ok {
			return v
		}
		return match
	})
}

// ExpandOS 用进程环境展开 s。
func ExpandOS(s string) string { return Expand(s, os.LookupEnv) }

// ExpandNode 递归展开 YAML 树里字符串标量的 ${VAR}：只动值、不动键。
//
// skip 对某条字段路径返回 true 时，整棵子树原样保留（那些值要留给渲染器决定何时求值）。
// 路径里映射用键名，数组下标一律写作 "*"。
func ExpandNode(node *yaml.Node, skip func(path []string) bool) {
	expandNode(node, nil, skip)
}

func expandNode(node *yaml.Node, path []string, skip func([]string) bool) {
	if node == nil || (skip != nil && skip(path)) {
		return
	}
	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			expandNode(child, path, skip)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			expandNode(node.Content[i+1], appendPath(path, node.Content[i].Value), skip)
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			expandNode(item, appendPath(path, "*"), skip)
		}
	case yaml.ScalarNode:
		if node.Tag == "!!str" {
			node.Value = ExpandOS(node.Value)
		}
	}
}

func appendPath(path []string, segment string) []string {
	return append(append([]string(nil), path...), segment)
}
```

- [ ] **Step 6: Run the `envref` tests**

Run: `go test ./internal/envref/...`
Expected: PASS.

- [ ] **Step 7: Write the failing `yamlfile` and `project` layout tests**

`internal/yamlfile/yamlfile_test.go`:

```go
package yamlfile_test

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

func TestReadMissingIsNotExist(t *testing.T) {
	_, err := yamlfile.Read(filepath.Join(t.TempDir(), "deploy.yaml"))
	assert.True(t, errors.Is(err, fs.ErrNotExist))
}

func TestDocumentInvalidYAML(t *testing.T) {
	_, err := yamlfile.Document([]byte("a: [1"), "deploy.yaml", false)
	e := clierr.As(err)
	require.NotNil(t, e)
	assert.Equal(t, i18n.T(msgid.LayerNotValidYAML, "deploy.yaml"), e.Message)
}

func TestDocumentEmpty(t *testing.T) {
	doc, err := yamlfile.Document([]byte("# only a comment\n"), "vars.yaml", true)
	require.NoError(t, err)
	assert.Nil(t, doc)

	_, err = yamlfile.Document([]byte(""), "deploy.yaml", false)
	require.Error(t, err)
	assert.Equal(t, i18n.T(msgid.LayerEmpty, "deploy.yaml"), clierr.As(err).Message)
}

func TestDocumentMustBeMapping(t *testing.T) {
	_, err := yamlfile.Document([]byte("- a\n- b\n"), "deploy.yaml", false)
	require.Error(t, err)
}

func TestRequireSequenceAndMapping(t *testing.T) {
	doc, err := yamlfile.Document([]byte("components: {a: 1}\nvars: [1]\nk8s: null\n"), "deploy.yaml", false)
	require.NoError(t, err)
	p := clierr.NewProblemSet(clierr.CodeConfigInvalid, "x")
	yamlfile.RequireSequence(yamlfile.Lookup(doc, "components"), "components", p)
	yamlfile.RequireMapping(yamlfile.Lookup(doc, "vars"), "vars", p)
	yamlfile.RequireMapping(yamlfile.Lookup(doc, "k8s"), "k8s", p)         // null 放行
	yamlfile.RequireMapping(yamlfile.Lookup(doc, "absent"), "absent", p)   // 不存在放行
	require.Equal(t, 2, p.Len())
	assert.Equal(t, "components", p.Items()[0].Field)
	assert.Equal(t, "vars", p.Items()[1].Field)
}
```

`internal/project/layout_test.go`:

```go
package project_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/project"
)

func TestLayoutPaths(t *testing.T) {
	l := project.NewLayout("/p")
	assert.Equal(t, "/p/brickkit.yaml", l.DeclPath())
	assert.Equal(t, "/p/deploy.yaml", l.DeployPath())
	assert.Equal(t, "/p/deploy.local.yaml", l.DeployLocalPath())
	assert.Equal(t, "/p/deploy.local.yaml.bak", l.DeployLocalBackupPath())
	assert.Equal(t, "/p/config", l.ConfigDir())
	assert.Equal(t, "/p/config/vars.yaml", l.VarsPath())
	assert.Equal(t, "/p/config/.archive", l.ConfigArchiveDir())
	assert.Equal(t, "/p/.brickkit/local-mode", l.LocalModePath())
	assert.Equal(t, "/p/shell", l.ShellDir())
	assert.Equal(t, "/p/deploy.prod.yaml", l.Resolve("deploy.prod.yaml"))
	assert.Equal(t, "/abs/d.yaml", l.Resolve("/abs/d.yaml"))
	assert.Equal(t, ".", project.NewLayout("").Root)
}

func TestLocalModeToggle(t *testing.T) {
	l := project.NewLayout(t.TempDir())
	on, err := project.LocalModeOn(l)
	require.NoError(t, err)
	assert.False(t, on)

	require.NoError(t, project.SetLocalMode(l, true))
	on, err = project.LocalModeOn(l)
	require.NoError(t, err)
	assert.True(t, on)
	assert.FileExists(t, filepath.Join(l.Root, ".brickkit", "local-mode"))

	require.NoError(t, project.SetLocalMode(l, false))
	require.NoError(t, project.SetLocalMode(l, false)) // 关两次不报错
	on, err = project.LocalModeOn(l)
	require.NoError(t, err)
	assert.False(t, on)
}
```

- [ ] **Step 8: Run them to see them fail**

Run: `go test ./internal/yamlfile/... ./internal/project/...`
Expected: FAIL — packages do not exist.

- [ ] **Step 9: Implement `yamlfile`**

`internal/yamlfile/yamlfile.go`:

```go
// Package yamlfile 是三层文件（brickkit.yaml / deploy*.yaml / config/*.yaml）共用的
// "读文件 → 解析 YAML → 顶层必须是映射 → 解码"流水线，以及它们统一的报错。
package yamlfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcheck"
)

// Read 读取文件。文件不存在时原样返回满足 errors.Is(err, fs.ErrNotExist) 的错误——
// 缺了算不算错、该怎么提示，只有调用方知道（deploy.yaml 缺了要 init，vars.yaml 缺了无所谓）。
func Read(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return data, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return nil, clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.LayerReadFailed, filepath.Base(path))).
		WithDetail(i18n.T(msgid.LabelPath), path).
		WithDetail(i18n.T(msgid.LabelReason), err.Error()).
		WithHint(i18n.T(msgid.ProblemHintCheckPermissions)).
		WithCause(err)
}

// Document 把 data 解析成顶层映射节点。
//
// allowEmpty 为 true 时，空文件（或只有注释）返回 (nil, nil)：vars.yaml、组件配置文件
// 允许是空的；brickkit.yaml 与 deploy 文件不允许。
func Document(data []byte, source string, allowEmpty bool) (*yaml.Node, error) {
	name := filepath.Base(source)
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.LayerNotValidYAML, name)).
			WithDetail(i18n.T(msgid.LabelFile), source).
			WithDetail(i18n.T(msgid.LabelReason), CleanError(err)).
			WithHint(i18n.T(msgid.ProblemHintCheckSyntax)).
			WithCause(err)
	}
	if root.Kind == 0 || len(root.Content) == 0 || root.Content[0].Tag == "!!null" {
		if allowEmpty {
			return nil, nil
		}
		return nil, clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.LayerEmpty, name)).
			WithDetail(i18n.T(msgid.LabelFile), source)
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil, clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.ProblemValidationFailed, name)).
			WithDetail(i18n.T(msgid.LabelFile), source).
			WithDetail(name, i18n.T(msgid.ProblemTopLevelMustBeMapping))
	}
	return doc, nil
}

// Decode 把 doc 解码进 out。类型不匹配逐条记进 p，返回 false。
func Decode(doc *yaml.Node, out any, p *clierr.ProblemSet) bool {
	err := doc.Decode(out)
	if err == nil {
		return true
	}
	var typeErr *yaml.TypeError
	if errors.As(err, &typeErr) {
		for _, msg := range typeErr.Errors {
			p.Add(i18n.T(msgid.ProblemLabelTypeMismatch), msg)
		}
	} else {
		p.Add(i18n.T(msgid.ProblemLabelParseFailed), CleanError(err))
	}
	return false
}

// Lookup 沿键路径取子节点，任何一段不存在都返回 nil。
func Lookup(node *yaml.Node, path ...string) *yaml.Node {
	current := node
	for _, key := range path {
		if current == nil || current.Kind != yaml.MappingNode {
			return nil
		}
		var next *yaml.Node
		for i := 0; i+1 < len(current.Content); i += 2 {
			if current.Content[i].Value == key {
				next = current.Content[i+1]
				break
			}
		}
		current = next
	}
	return current
}

// RequireSequence 要求 node（存在且不是 null 时）是数组；field 是报错里的完整字段路径。
func RequireSequence(node *yaml.Node, field string, p *clierr.ProblemSet) {
	if node != nil && node.Tag != "!!null" && node.Kind != yaml.SequenceNode {
		p.Add(field, i18n.T(msgid.ProblemMustBeArray, yamlcheck.KindName(node)))
	}
}

// RequireMapping 要求 node（存在且不是 null 时）是映射；field 是报错里的完整字段路径。
func RequireMapping(node *yaml.Node, field string, p *clierr.ProblemSet) {
	if node != nil && node.Tag != "!!null" && node.Kind != yaml.MappingNode {
		p.Add(field, i18n.T(msgid.ProblemMustBeMapping, yamlcheck.KindName(node)))
	}
}

// Indexed 把数组下标拼进字段路径：components[2]。
func Indexed(field string, i int) string { return fmt.Sprintf("%s[%d]", field, i) }

// CleanError 去掉 yaml.v3 报错前面的 "yaml: "。
func CleanError(err error) string {
	return strings.TrimSpace(strings.TrimPrefix(err.Error(), "yaml: "))
}
```

- [ ] **Step 10: Implement `project.Layout` and local-mode state**

`internal/project/layout.go`:

```go
// Package project 把一个三层文件项目（brickkit.yaml + deploy*.yaml + config/）装载成
// 一个 Project：之后所有命令只认这一个对象，谁也不再自己去读分层文件。
package project

import "path/filepath"

// 项目目录里的固定名字（提案 §4.1）。
const (
	FileDecl              = "brickkit.yaml"
	FileDeploy            = "deploy.yaml"
	FileDeployLocal       = "deploy.local.yaml"
	FileDeployLocalBackup = "deploy.local.yaml.bak"
	FileGitignore         = ".gitignore"
	FileProjectDoc        = "BRICKKIT.md"
	DirConfig             = "config"
	FileVars              = "vars.yaml"
	DirConfigArchive      = ".archive"
	DirBrickkit           = ".brickkit"
	DirManifests          = "manifests"
	DirArtifacts          = "artifacts"
	DirGenerated          = "generated"
	DirComponents         = "components"
	DirShell              = "shell"
	DirArchived           = ".archived"
	FileCredentials       = "credentials"
	FileSkillsLock        = "skills.lock"
	FileSessionLock       = "session.lock"
	// FileLocalMode 存在即"本地模式已开启"（附录 A16）：local off 只删它，
	// 不删 deploy.local.yaml，再开时本地改动还在。
	FileLocalMode = "local-mode"
)

// Layout 描述项目目录布局。所有路径都由 Root 推导，不依赖进程当前目录。
type Layout struct {
	Root string
}

// NewLayout 构建布局；root 为空时用当前目录。
func NewLayout(root string) Layout {
	if root == "" {
		root = "."
	}
	return Layout{Root: root}
}

func (l Layout) path(parts ...string) string {
	return filepath.Join(append([]string{l.Root}, parts...)...)
}

// Resolve 把使用者给的路径（如 -f deploy.prod.yaml）按项目根解析；绝对路径原样返回。
func (l Layout) Resolve(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return l.path(p)
}

func (l Layout) DeclPath() string              { return l.path(FileDecl) }
func (l Layout) DeployPath() string            { return l.path(FileDeploy) }
func (l Layout) DeployLocalPath() string       { return l.path(FileDeployLocal) }
func (l Layout) DeployLocalBackupPath() string { return l.path(FileDeployLocalBackup) }
func (l Layout) GitignorePath() string         { return l.path(FileGitignore) }
func (l Layout) ProjectDocPath() string        { return l.path(FileProjectDoc) }
func (l Layout) ConfigDir() string             { return l.path(DirConfig) }
func (l Layout) VarsPath() string              { return l.path(DirConfig, FileVars) }
func (l Layout) ConfigArchiveDir() string      { return l.path(DirConfig, DirConfigArchive) }
func (l Layout) BrickkitDir() string           { return l.path(DirBrickkit) }
func (l Layout) ManifestsDir() string          { return l.path(DirBrickkit, DirManifests) }
func (l Layout) ArtifactsDir() string          { return l.path(DirBrickkit, DirArtifacts) }
func (l Layout) GeneratedDir() string          { return l.path(DirBrickkit, DirGenerated) }
func (l Layout) CredentialsPath() string       { return l.path(DirBrickkit, FileCredentials) }
func (l Layout) SkillsLockPath() string        { return l.path(DirBrickkit, FileSkillsLock) }
func (l Layout) SessionLockPath() string       { return l.path(DirBrickkit, FileSessionLock) }
func (l Layout) LocalModePath() string         { return l.path(DirBrickkit, FileLocalMode) }
func (l Layout) ComponentsDir() string         { return l.path(DirComponents) }
func (l Layout) ShellDir() string              { return l.path(DirShell) }
func (l Layout) ArchivedDir() string           { return l.path(DirComponents, DirArchived) }
```

`internal/project/localmode.go`:

```go
package project

import (
	"errors"
	"io/fs"
	"os"
)

// LocalModeOn 报告本地模式是否开启（.brickkit/local-mode 是否存在）。
func LocalModeOn(l Layout) (bool, error) {
	_, err := os.Stat(l.LocalModePath())
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}

// SetLocalMode 打开或关闭本地模式。关闭一个本来就关着的开关不算错。
func SetLocalMode(l Layout, on bool) error {
	if !on {
		err := os.Remove(l.LocalModePath())
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(l.BrickkitDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(l.LocalModePath(), []byte("on\n"), 0o644)
}
```

- [ ] **Step 11: Run all Task 2 tests and checks**

Run: `go build ./... && go vet ./internal/envref/... ./internal/yamlfile/... ./internal/project/... && go test ./internal/envref/... ./internal/yamlfile/... ./internal/project/... ./tests/i18nguard/... && .tools/bin/golangci-lint run ./internal/envref/... ./internal/yamlfile/... ./internal/project/...`
Expected: all PASS, no lint findings.

- [ ] **Step 12: Commit**

```bash
git add internal/clierr/clierr.go internal/msgid/layer.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go internal/envref internal/yamlfile internal/project
git commit -m "feat(project): foundations for the three-layer model — envref, yamlfile, layout, local-mode state

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

## Task 3: `internal/projfile` — `brickkit.yaml` v2

**Files:**
- Create: `internal/projfile/projfile.go` (types), `internal/projfile/parse.go`, `internal/projfile/validate.go`, `internal/projfile/projectname.go`, `internal/projfile/projfile_test.go`
- Modify: `internal/config/project.go` (the name rule moves out; thin wrappers stay so `internal/config` keeps compiling until P2 deletes it)
- Create: `internal/msgid/projfile.go`
- Modify: `internal/i18n/catalog_en.go`, `internal/i18n/catalog_zh.go`

**Interfaces:**
- Consumes: Task 2's `yamlfile.*`, `envref.ExpandNode`; `manifest.ComponentIDProblem`, `manifest.IsExactVersion`; `yamlcheck.Walk`.
- Produces:
  - `const projfile.FileName = "brickkit.yaml"`, `SourceTypeMarket/Git/Local`, `KindShell = "shell"`
  - `type File struct { Project string; Sources []Source; Components []Component; Installer *Installer; Source string }`
  - `type Source struct { Name, Type, URL, BaseURL, Path, AuthToken string; Enabled *bool }` + `IsEnabled() bool`
  - `type Component struct { ID, Version, Kind string; Source *ComponentSource }` + `Ref() string` (`id@version`), `IsShell() bool`
  - `type ComponentSource struct { Type, Repo, Path string }`
  - `type Installer struct { RequireSignature *bool; PublicKeys map[string]string }`
  - `(*File).RequireSignature() bool`, `PublicKeys() map[string]string`, `EnabledSources() []Source`, `ComponentsByID(id string) []Component`, `Versions(id string) []string`, `IDs() []string` (distinct, declaration order), `IsShellID(id string) bool`
  - `projfile.ParseFile(path string) (*File, error)` (missing → `clierr.CodeProjectMissing`), `projfile.Parse(data []byte, source string) (*File, error)`, `(*File).Validate() error`
  - `projfile.ProjectNameProblem(name string) string`, `ProjectNameRule() string`, `ValidateProjectName(name string) error`, `SuggestProjectName(name string) string`, `MaxProjectNameLen = 54`

- [ ] **Step 1: Move the project-name rule**

Create `internal/projfile/projectname.go` by moving, unchanged in behaviour, from `internal/config/project.go`: `projectNameRe`, `MaxProjectNameLen`, `ProjectNameRule`, `ValidateProjectName`, `projectNameProblem` (renamed to the exported `ProjectNameProblem`), `hasUpper`, `SuggestProjectName`. Package clause `package projfile`; imports `regexp`, `strings`, `unicode`, `clierr`, `i18n`, `msgid`. Inside `ValidateProjectName` replace the call `projectNameProblem(name)` with `ProjectNameProblem(name)`.

Then in `internal/config/project.go` delete those declarations and add, keeping every existing caller in `internal/config` compiling:

```go
// 项目名规则已移到 internal/projfile（三层文件重构 P1）；这几个包装在 P2 随本包一起删除。
const MaxProjectNameLen = projfile.MaxProjectNameLen

func ProjectNameRule() string                { return projfile.ProjectNameRule() }
func ValidateProjectName(name string) error   { return projfile.ValidateProjectName(name) }
func SuggestProjectName(name string) string   { return projfile.SuggestProjectName(name) }
func projectNameProblem(name string) string  { return projfile.ProjectNameProblem(name) }
```

(add the import `github.com/brickkit/brickkit/internal/projfile`; remove imports that became unused). `internal/config` has other users of `hasUpper`? Run `grep -n "hasUpper" internal/config/*.go` — if any remain outside `project.go`, keep a private copy of `hasUpper` in `internal/config/project.go`.

Run: `go build ./... && go test ./internal/config/...`
Expected: PASS (pure move).

- [ ] **Step 2: Add the `projfile` messages**

`internal/msgid/projfile.go`:

```go
package msgid

// internal/projfile（brickkit.yaml v2）的文案。
const (
	ProjfileSourceNameDuplicate        = "projfile.source_name_duplicate"
	ProjfileFieldRequiredFor           = "projfile.field_required_for"
	ProjfileKindInvalid                = "projfile.kind_invalid"
	ProjfileShellSingleVersion         = "projfile.shell_single_version"
	ProjfileKindInconsistent           = "projfile.kind_inconsistent"
	ProjfileComponentSourceTypeInvalid = "projfile.component_source_type_invalid"
)
```

`catalog_en.go`:

```go
	msgid.ProjfileSourceNameDuplicate:        "duplicates %[1]s.name (install source names must be unique)",
	msgid.ProjfileFieldRequiredFor:           "missing (an install source of type %[2]s must declare %[1]s)",
	msgid.ProjfileKindInvalid:                "must be shell or omitted (currently %[1]q)",
	msgid.ProjfileShellSingleVersion:         "shell %[1]s may only have one version in a project, and %[2]s already declares a different one",
	msgid.ProjfileKindInconsistent:           "%[1]s is declared with a different kind in %[2]s; every version of one component ID must have the same kind",
	msgid.ProjfileComponentSourceTypeInvalid: "must be git or local (currently %[1]q)",
```

`catalog_zh.go`:

```go
	msgid.ProjfileSourceNameDuplicate:        "与 %[1]s.name 重复（安装源名称必须唯一）",
	msgid.ProjfileFieldRequiredFor:           "缺失（type 为 %[2]s 的安装源必须声明 %[1]s）",
	msgid.ProjfileKindInvalid:                "只能是 shell 或不写（当前是 %[1]q）",
	msgid.ProjfileShellSingleVersion:         "外壳 %[1]s 在一个项目里只能有一个版本，%[2]s 已经声明了另一个版本",
	msgid.ProjfileKindInconsistent:           "%[1]s 在 %[2]s 里声明的 kind 与这里不同；同一个组件 ID 的所有版本 kind 必须一致",
	msgid.ProjfileComponentSourceTypeInvalid: "只能是 git 或 local（当前是 %[1]q）",
```

- [ ] **Step 3: Write the failing tests**

`internal/projfile/projfile_test.go`:

```go
package projfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/projfile"
)

const valid = `project: my-shop
sources:
  - name: default
    type: git
    baseUrl: https://github.com/myorg/
  - name: dev
    type: local
    path: ./components
  - name: market
    type: market
    url: https://market.example.com/api/v1
    authToken: ${MARKET_TOKEN}
components:
  - id: erp/shell
    version: 1.0.0
    kind: shell
  - id: erp/backend
    version: 2.0.0
  - id: erp/backend
    version: 1.0.0
  - id: third-party/payment
    version: 1.0.0
    source:
      type: git
      repo: https://github.com/other-org/payment-gateway
  - id: infra/logger
    version: 0.3.0
    source:
      type: local
      path: ./components/infra-logger
installer:
  requireSignature: false
`

func fields(err error) []string {
	e := clierr.As(err)
	if e == nil {
		return nil
	}
	var out []string
	for _, d := range e.Details {
		out = append(out, d.Key)
	}
	return out
}

func TestParseValid(t *testing.T) {
	t.Setenv("MARKET_TOKEN", "tok")
	f, err := projfile.Parse([]byte(valid), "brickkit.yaml")
	require.NoError(t, err)
	assert.Equal(t, "my-shop", f.Project)
	assert.Equal(t, "tok", f.Sources[2].AuthToken)
	assert.True(t, f.Components[0].IsShell())
	assert.True(t, f.IsShellID("erp/shell"))
	assert.Equal(t, []string{"2.0.0", "1.0.0"}, f.Versions("erp/backend"))
	assert.Equal(t, []string{"erp/shell", "erp/backend", "third-party/payment", "infra/logger"}, f.IDs())
	assert.False(t, f.RequireSignature())
	assert.Equal(t, "erp/backend@2.0.0", f.Components[1].Ref())
}

func TestParseRejectsOldFields(t *testing.T) {
	_, err := projfile.Parse([]byte("project: p\ndeploy:\n  target: docker\nresources: []\n"), "brickkit.yaml")
	require.Error(t, err)
	assert.Contains(t, fields(err), "deploy")
	assert.Contains(t, fields(err), "resources")
}

func TestValidateComponents(t *testing.T) {
	cases := map[string]struct {
		yaml  string
		field string
	}{
		"range version":        {"project: p\ncomponents:\n  - id: a/b\n    version: ^1.0.0\n", "components[0].version"},
		"literal local":        {"project: p\ncomponents:\n  - id: a/b\n    version: local\n", "components[0].version"},
		"duplicate":            {"project: p\ncomponents:\n  - {id: a/b, version: 1.0.0}\n  - {id: a/b, version: 1.0.0}\n", "components[1]"},
		"bad kind":             {"project: p\ncomponents:\n  - {id: a/b, version: 1.0.0, kind: connector}\n", "components[0].kind"},
		"two shell versions":   {"project: p\ncomponents:\n  - {id: a/s, version: 1.0.0, kind: shell}\n  - {id: a/s, version: 2.0.0, kind: shell}\n", "components[1]"},
		"kind inconsistent":    {"project: p\ncomponents:\n  - {id: a/s, version: 1.0.0, kind: shell}\n  - {id: a/s, version: 2.0.0}\n", "components[1].kind"},
		"git source no repo":   {"project: p\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    source: {type: git}\n", "components[0].source.repo"},
		"local source no path": {"project: p\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    source: {type: local}\n", "components[0].source.path"},
		"bad source type":      {"project: p\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    source: {type: market}\n", "components[0].source.type"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := projfile.Parse([]byte(tc.yaml), "brickkit.yaml")
			require.Error(t, err)
			assert.Contains(t, fields(err), tc.field)
		})
	}
}

func TestValidateSources(t *testing.T) {
	_, err := projfile.Parse([]byte(`project: p
sources:
  - {name: a, type: git}
  - {name: a, type: local}
  - {name: m, type: market}
  - {name: x, type: svn}
`), "brickkit.yaml")
	require.Error(t, err)
	got := fields(err)
	assert.Contains(t, got, "sources[0].baseUrl")
	assert.Contains(t, got, "sources[1].name")
	assert.Contains(t, got, "sources[1].path")
	assert.Contains(t, got, "sources[2].url")
	assert.Contains(t, got, "sources[3].type")
}

func TestParseFileMissing(t *testing.T) {
	_, err := projfile.ParseFile(filepath.Join(t.TempDir(), "brickkit.yaml"))
	require.Error(t, err)
	assert.Equal(t, clierr.CodeProjectMissing, clierr.As(err).Code)
}

func TestParseFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "brickkit.yaml")
	require.NoError(t, os.WriteFile(path, []byte("project: p\n"), 0o644))
	f, err := projfile.ParseFile(path)
	require.NoError(t, err)
	assert.Equal(t, path, f.Source)
	assert.True(t, f.RequireSignature())
}
```

- [ ] **Step 4: Run to see failure**

Run: `go test ./internal/projfile/...`
Expected: FAIL — `projfile.Parse` and friends undefined (only `projectname.go` exists).

- [ ] **Step 5: Implement the types**

`internal/projfile/projfile.go`:

```go
// Package projfile 负责 brickkit.yaml（三层文件的"组件声明"层）：项目名、安装源、
// 有哪些组件与版本、谁是外壳、组件从哪来、信任哪些发布者公钥。
//
// 部署怎么做（deploy.yaml）与组件要读的环境变量（config/）都不在这里——提案 §4.2。
package projfile

// FileName 是声明层文件名。
const FileName = "brickkit.yaml"

// 安装源类型。market 继续可用（附录 A14），git 按组件一仓库推导地址，local 是开发态目录。
const (
	SourceTypeMarket = "market"
	SourceTypeGit    = "git"
	SourceTypeLocal  = "local"
)

// KindShell 标记外壳组件（附录 A11：由 CLI 维护，只作识别用，成员关系只看 deploy 文件的 members）。
const KindShell = "shell"

// File 是 brickkit.yaml 的完整结构。
type File struct {
	Project    string      `yaml:"project"`
	Sources    []Source    `yaml:"sources,omitempty"`
	Components []Component `yaml:"components,omitempty"`
	Installer  *Installer  `yaml:"installer,omitempty"`

	// Source 是文件路径，只用于报错。
	Source string `yaml:"-"`
}

// Source 是一个安装源（提案 §9.3）。
type Source struct {
	Name string `yaml:"name"`
	Type string `yaml:"type" jsonschema:"enum=market|git|local"`
	// URL 是 market 源的 API 地址。
	URL string `yaml:"url,omitempty"`
	// BaseURL 是 git 源的组织地址：组件 erp/backend 推导为 <BaseURL>erp-backend。
	BaseURL string `yaml:"baseUrl,omitempty"`
	// Path 是 local 源扫描的目录。
	Path      string `yaml:"path,omitempty"`
	AuthToken string `yaml:"authToken,omitempty"`
	// Enabled 缺省为 true。
	Enabled *bool `yaml:"enabled,omitempty"`
}

// IsEnabled 返回该源是否启用（缺省 true）。
func (s Source) IsEnabled() bool { return s.Enabled == nil || *s.Enabled }

// Component 是一条组件声明。
//
// Version 永远是精确版本——本地源也写 component.yaml 里的真实版本（附录 A8），
// 否则依赖方的精确匹配会落空，多版本共存会再从远端拉一份。
type Component struct {
	ID      string           `yaml:"id"`
	Version string           `yaml:"version" jsonschema:"pattern=^[0-9]+[.][0-9]+[.][0-9]+$"`
	Kind    string           `yaml:"kind,omitempty" jsonschema:"enum=shell"`
	Source  *ComponentSource `yaml:"source,omitempty"`
}

// Ref 返回 <id>@<version>。
func (c Component) Ref() string { return c.ID + "@" + c.Version }

// IsShell 报告它是不是外壳。
func (c Component) IsShell() bool { return c.Kind == KindShell }

// ComponentSource 显式覆盖一个组件的来源（提案 §9.3、§9.6）。
type ComponentSource struct {
	Type string `yaml:"type" jsonschema:"enum=git|local"`
	// Repo 是 git 仓库地址（映射冲突或第三方组织时用）。
	Repo string `yaml:"repo,omitempty"`
	// Path 是本地源目录，相对项目根。
	Path string `yaml:"path,omitempty"`
}

// Installer 是安装器行为配置（签名校验，附录 A14）。
type Installer struct {
	RequireSignature *bool             `yaml:"requireSignature,omitempty"`
	PublicKeys       map[string]string `yaml:"publicKeys,omitempty"`
}

// RequireSignature 返回是否强制签名校验（缺省 true）。
func (f *File) RequireSignature() bool {
	if f.Installer == nil || f.Installer.RequireSignature == nil {
		return true
	}
	return *f.Installer.RequireSignature
}

// PublicKeys 返回信任的发布者公钥，未配置时为 nil。
func (f *File) PublicKeys() map[string]string {
	if f.Installer == nil {
		return nil
	}
	return f.Installer.PublicKeys
}

// EnabledSources 返回启用中的安装源，保持声明顺序（即优先级）。
func (f *File) EnabledSources() []Source {
	var out []Source
	for _, s := range f.Sources {
		if s.IsEnabled() {
			out = append(out, s)
		}
	}
	return out
}

// ComponentsByID 返回某个 ID 的全部版本条目。
func (f *File) ComponentsByID(id string) []Component {
	var out []Component
	for _, c := range f.Components {
		if c.ID == id {
			out = append(out, c)
		}
	}
	return out
}

// Versions 返回某个 ID 的全部版本，按声明顺序。
func (f *File) Versions(id string) []string {
	var out []string
	for _, c := range f.ComponentsByID(id) {
		out = append(out, c.Version)
	}
	return out
}

// IDs 返回去重后的组件 ID，按首次出现的顺序。
func (f *File) IDs() []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range f.Components {
		if !seen[c.ID] {
			seen[c.ID] = true
			out = append(out, c.ID)
		}
	}
	return out
}

// IsShellID 报告某个 ID 是否被标成外壳。
func (f *File) IsShellID(id string) bool {
	for _, c := range f.Components {
		if c.ID == id && c.IsShell() {
			return true
		}
	}
	return false
}
```

- [ ] **Step 6: Implement parsing and validation**

`internal/projfile/parse.go`:

```go
package projfile

import (
	"errors"
	"io/fs"
	"reflect"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/envref"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcheck"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// ParseFile 读取并解析 brickkit.yaml。文件不存在按"这里不是 BrickKit 项目"报。
func ParseFile(path string) (*File, error) {
	data, err := yamlfile.Read(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, clierr.New(clierr.CodeProjectMissing, i18n.T(msgid.ProjectMissing)).
			WithDetail(i18n.T(msgid.LabelPath), path).
			WithHint(i18n.T(msgid.ProjectMissingHintInit)).
			WithCause(err)
	}
	if err != nil {
		return nil, err
	}
	return Parse(data, path)
}

// Parse 解析并校验 brickkit.yaml：YAML → ${VAR} 展开 → 形状与未知字段 → 解码 → 语义校验。
//
// 这一层没有密钥候选值（密钥都在 config/），所以 ${VAR} 一律在解析时展开。
func Parse(data []byte, source string) (*File, error) {
	if source == "" {
		source = FileName
	}
	doc, err := yamlfile.Document(data, source, false)
	if err != nil {
		return nil, err
	}
	envref.ExpandNode(doc, nil)

	shape := newProblems(source)
	yamlfile.RequireSequence(yamlfile.Lookup(doc, "sources"), "sources", shape)
	yamlfile.RequireSequence(yamlfile.Lookup(doc, "components"), "components", shape)
	yamlcheck.Walk(doc, reflect.TypeOf(File{}), shape)
	if shape.Len() > 0 {
		return nil, shape.Err()
	}

	var f File
	decode := newProblems(source)
	if !yamlfile.Decode(doc, &f, decode) {
		return nil, decode.Err()
	}
	f.Source = source
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

func newProblems(source string) *clierr.ProblemSet {
	if source == "" {
		source = FileName
	}
	return clierr.NewProblemSet(clierr.CodeConfigInvalid, i18n.T(msgid.ProblemValidationFailed, FileName)).
		WithSource(i18n.T(msgid.LabelFile), source)
}
```

`internal/projfile/validate.go`:

```go
package projfile

import (
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// Validate 校验全部字段，一次报出所有问题。
func (f *File) Validate() error {
	p := newProblems(f.Source)
	f.validateProject(p)
	f.validateSources(p)
	f.validateComponents(p)
	return p.Err()
}

func (f *File) validateProject(p *clierr.ProblemSet) {
	if f.Project == "" {
		p.Missing("project")
		return
	}
	if reason := ProjectNameProblem(f.Project); reason != "" {
		p.Add("project", i18n.T(msgid.ConfigProjectNameProblemWithRule, reason, ProjectNameRule()))
	}
}

func (f *File) validateSources(p *clierr.ProblemSet) {
	seen := map[string]int{}
	for i, s := range f.Sources {
		field := yamlfile.Indexed("sources", i)
		if s.Name == "" {
			p.Missing(field + ".name")
		} else if prev, ok := seen[s.Name]; ok {
			p.Add(field+".name", i18n.T(msgid.ProjfileSourceNameDuplicate, yamlfile.Indexed("sources", prev)))
		} else {
			seen[s.Name] = i
		}

		switch s.Type {
		case "":
			p.Missing(field + ".type")
		case SourceTypeMarket:
			requireFor(p, field, "url", s.URL, s.Type)
		case SourceTypeGit:
			requireFor(p, field, "baseUrl", s.BaseURL, s.Type)
		case SourceTypeLocal:
			requireFor(p, field, "path", s.Path, s.Type)
		default:
			p.Add(field+".type", i18n.T(msgid.ProblemMustBeOneOfThree,
				SourceTypeMarket, SourceTypeGit, SourceTypeLocal, s.Type))
		}
	}
}

func requireFor(p *clierr.ProblemSet, field, key, value, typ string) {
	if value == "" {
		p.Add(field+"."+key, i18n.T(msgid.ProjfileFieldRequiredFor, key, typ))
	}
}

func (f *File) validateComponents(p *clierr.ProblemSet) {
	seen := map[string]int{}
	firstOfID := map[string]int{}
	for i, c := range f.Components {
		field := yamlfile.Indexed("components", i)

		if c.ID == "" {
			p.Missing(field + ".id")
		} else if reason := manifest.ComponentIDProblem(c.ID); reason != "" {
			p.Add(field+".id", reason)
		}
		switch {
		case c.Version == "":
			p.Missing(field + ".version")
		case !manifest.IsExactVersion(c.Version):
			p.Add(field+".version", i18n.T(msgid.ConfigVersionNotExactRange, c.Version))
		}
		if c.ID != "" && c.Version != "" {
			if prev, ok := seen[c.Ref()]; ok {
				p.Add(field, i18n.T(msgid.ConfigComponentDuplicate, yamlfile.Indexed("components", prev), c.Ref()))
			} else {
				seen[c.Ref()] = i
			}
		}

		switch c.Kind {
		case "", KindShell:
		default:
			p.Add(field+".kind", i18n.T(msgid.ProjfileKindInvalid, c.Kind))
		}

		if c.ID != "" {
			if prev, ok := firstOfID[c.ID]; ok {
				first := f.Components[prev]
				prevField := yamlfile.Indexed("components", prev)
				switch {
				case first.Kind != c.Kind:
					p.Add(field+".kind", i18n.T(msgid.ProjfileKindInconsistent, c.ID, prevField))
				case c.IsShell() && first.Version != c.Version:
					p.Add(field, i18n.T(msgid.ProjfileShellSingleVersion, c.ID, prevField))
				}
			} else {
				firstOfID[c.ID] = i
			}
		}

		validateComponentSource(p, field, c.Source)
	}
}

func validateComponentSource(p *clierr.ProblemSet, field string, s *ComponentSource) {
	if s == nil {
		return
	}
	field += ".source"
	switch s.Type {
	case "":
		p.Missing(field + ".type")
	case SourceTypeGit:
		requireFor(p, field, "repo", s.Repo, s.Type)
	case SourceTypeLocal:
		requireFor(p, field, "path", s.Path, s.Type)
	default:
		p.Add(field+".type", i18n.T(msgid.ProjfileComponentSourceTypeInvalid, s.Type))
	}
}
```

Note: in `TestValidateComponents`, the case "kind inconsistent" expects `components[1].kind`; "two shell versions" (both `kind: shell`) expects `components[1]` — the switch order above produces exactly these.

- [ ] **Step 7: Run tests and checks**

Run: `go build ./... && go vet ./internal/projfile/... && go test ./internal/projfile/... ./internal/config/... ./tests/i18nguard/... && .tools/bin/golangci-lint run ./internal/projfile/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/projfile internal/config/project.go internal/msgid/projfile.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "feat(projfile): brickkit.yaml v2 — declaration layer types, parsing and validation

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
## Task 4: `internal/deployfile` — `deploy.yaml` / `deploy.local.yaml` / `-f` files

**Files:**
- Create: `internal/deployfile/deployfile.go` (types), `internal/deployfile/parse.go`, `internal/deployfile/validate.go`, `internal/deployfile/deployfile_test.go`
- Create: `internal/msgid/deployfile.go`
- Modify: `internal/i18n/catalog_en.go`, `internal/i18n/catalog_zh.go`

**Interfaces:**
- Consumes: `yamlfile.*`, `envref.ExpandNode`, `projfile.ProjectNameProblem`, `manifest.ComponentIDProblem`, `manifest.IsExactVersion`, `manifest.ValidateLabels`, `manifest.Resources`, `yamlcheck.Walk`, `yamlcheck.CheckStringValues`.
- Produces:
  - constants `TargetDocker/TargetPodman/TargetK8s`, `ModeEnabled/ModeDisable/ModeLocal/ModeDebug`, `PodSecurityRestricted`, `FileTeam = "deploy.yaml"`, `FileLocal = "deploy.local.yaml"`, `MinPort = 1`, `MaxPort = 65535`
  - `type Role int` with `RoleTeam`, `RoleLocal`
  - `type File struct { Target string; K8s *K8s; Vars map[string]any; Components []Component; Source string }`
  - `type K8s struct { Context, Namespace string; CreateNamespace *bool; PodSecurity string; ImagePullSecrets []string; IngressClass string; IngressAnnotations map[string]string; NetworkPolicy *NetworkPolicy; ServiceAccount *ServiceAccount }` and `NetworkPolicy`, `IngressControllerSource`, `AllowFromSource`, `Egress`, `AllowToTarget` (no `Resource` field — A15), `ServiceAccount`
  - `type Component struct { ID, Mode string; LocalPort int; Expose bool; ExposePort int; Hostname, TLSSecret string; Replicas *int; ServiceAccountName string; Resources *manifest.Resources; Labels map[string]string; Members []string }` + `Key() (id, version string)`, `ReplicaCount() int`, `IsDisabled() bool`, `IsPinned() bool`, `IsBareProcess() bool`
  - `(*File).Entry(id, version string) (Component, bool)` — exact `id@version` entry first, then the bare-ID entry
  - `(*File).Settings() K8s` (zero value when no `k8s:` block), `ShouldCreateNamespace() bool`, `NetworkPolicyEnabled() bool`, `EgressEnabled() bool`, `ServiceAccountEnabled() bool`
  - `deployfile.ParseFile(path string, role Role) (*File, []*clierr.Error, error)` — a missing file returns the raw `fs.ErrNotExist` error (the loader words it)
  - `deployfile.Parse(data []byte, source string, role Role) (*File, []*clierr.Error, error)`, `(*File).Validate(role Role) ([]*clierr.Error, error)` — the slice is non-blocking warnings

- [ ] **Step 1: Add the messages**

`internal/msgid/deployfile.go`:

```go
package msgid

// internal/deployfile（deploy.yaml / deploy.local.yaml / -f 指定的部署文件）的文案。
const (
	DeployfileComponentMustBeMapping = "deployfile.component_must_be_mapping"
	DeployfileVarNameInvalid         = "deployfile.var_name_invalid"
	DeployfileEntryDuplicate         = "deployfile.entry_duplicate"
	DeployfileDebugOnlyLocal         = "deployfile.debug_only_local"
	DeployfileModeInvalid            = "deployfile.mode_invalid"
	DeployfileLocalPortNeedsMode     = "deployfile.local_port_needs_mode"
	DeployfileMemberMustBeBareID     = "deployfile.member_must_be_bare_id"
	DeployfileMemberSelf             = "deployfile.member_self"
	DeployfileMemberDuplicate        = "deployfile.member_duplicate"
	DeployfileFieldIgnoredForTarget  = "deployfile.field_ignored_for_target"
)
```

`catalog_en.go`:

```go
	msgid.DeployfileComponentMustBeMapping: "each entry must be a mapping (id: …, mode: …)",
	msgid.DeployfileVarNameInvalid:         "must be a valid environment variable name: letters, digits and underscores, not starting with a digit",
	msgid.DeployfileEntryDuplicate:         "duplicates %[1]s (%[2]s has two entries)",
	msgid.DeployfileDebugOnlyLocal:         "mode: debug can only be written in deploy.local.yaml: it records that you are debugging this component on your machine right now, which is not a team decision. Run brickkit local on and set it there",
	msgid.DeployfileModeInvalid:            "must be one of enabled/disable/local/debug (or omitted), got %[1]q",
	msgid.DeployfileLocalPortNeedsMode:     "only takes effect with mode: local or mode: debug; declare one of them, or remove this field",
	msgid.DeployfileMemberMustBeBareID:     "list members by component ID without a version (got %[1]s): each member has exactly one version in brickkit.yaml",
	msgid.DeployfileMemberSelf:             "a shell cannot list itself as a member",
	msgid.DeployfileMemberDuplicate:        "%[1]s is listed twice",
	msgid.DeployfileFieldIgnoredForTarget:  "%[1]s has no effect with target: %[2]s and is ignored",
```

`catalog_zh.go`:

```go
	msgid.DeployfileComponentMustBeMapping: "每个条目都必须是映射（id: …、mode: …）",
	msgid.DeployfileVarNameInvalid:         "必须是合法的环境变量名：字母、数字、下划线，不能以数字开头",
	msgid.DeployfileEntryDuplicate:         "与 %[1]s 重复（%[2]s 有两个条目）",
	msgid.DeployfileDebugOnlyLocal:         "mode: debug 只能写在 deploy.local.yaml 里：它记录的是\"我此刻在本机调试这个组件\"，不是团队决策。先 brickkit local on，再到那里设置",
	msgid.DeployfileModeInvalid:            "只能是 enabled/disable/local/debug 之一（或不写），当前是 %[1]q",
	msgid.DeployfileLocalPortNeedsMode:     "只在 mode: local 或 mode: debug 下生效；声明其中之一，或删掉这个字段",
	msgid.DeployfileMemberMustBeBareID:     "成员只写组件 ID、不带版本（当前是 %[1]s）：每个成员在 brickkit.yaml 里只有一个版本",
	msgid.DeployfileMemberSelf:             "外壳不能把自己列为成员",
	msgid.DeployfileMemberDuplicate:        "%[1]s 被列了两次",
	msgid.DeployfileFieldIgnoredForTarget:  "%[1]s 在 target: %[2]s 下不起作用，已忽略",
```

- [ ] **Step 2: Write the failing tests**

`internal/deployfile/deployfile_test.go`:

```go
package deployfile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
)

const team = `target: docker
vars:
  DB_PASSWORD: "${PROD_DB_PASSWORD}"
components:
  - id: erp/shell
    expose: true
    exposePort: 8080
    members: [erp/backend]
  - id: erp/backend
    mode: enabled
  - id: people/basic@1.0.0
    mode: local
    localPort: 9001
  - id: people/basic
`

func fields(err error) []string {
	e := clierr.As(err)
	if e == nil {
		return nil
	}
	var out []string
	for _, d := range e.Details {
		out = append(out, d.Key)
	}
	return out
}

func parse(t *testing.T, yaml string, role deployfile.Role) (*deployfile.File, []*clierr.Error, error) {
	t.Helper()
	return deployfile.Parse([]byte(yaml), "deploy.yaml", role)
}

func TestParseTeamFile(t *testing.T) {
	t.Setenv("PROD_DB_PASSWORD", "leak")
	f, warnings, err := parse(t, team, deployfile.RoleTeam)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	// vars 的值留给渲染器求值，解析时绝不展开
	assert.Equal(t, "${PROD_DB_PASSWORD}", f.Vars["DB_PASSWORD"])

	e, ok := f.Entry("people/basic", "1.0.0")
	require.True(t, ok)
	assert.Equal(t, 9001, e.LocalPort)
	e, ok = f.Entry("people/basic", "2.0.0")
	require.True(t, ok)
	assert.Equal(t, "people/basic", e.ID)
	id, version := f.Components[2].Key()
	assert.Equal(t, "people/basic", id)
	assert.Equal(t, "1.0.0", version)
	assert.Equal(t, []string{"erp/backend"}, f.Components[0].Members)
	assert.True(t, f.Components[1].IsPinned())
	assert.Equal(t, 1, f.Components[1].ReplicaCount())
}

func TestDebugOnlyInLocalRole(t *testing.T) {
	doc := "target: docker\ncomponents:\n  - id: a/b\n    mode: debug\n    localPort: 9000\n"
	_, _, err := parse(t, doc, deployfile.RoleTeam)
	require.Error(t, err)
	assert.Contains(t, fields(err), "components[0].mode")

	_, _, err = parse(t, doc, deployfile.RoleLocal)
	require.NoError(t, err)
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]struct {
		yaml  string
		field string
	}{
		"missing target":         {"components: []\n", "target"},
		"bad target":             {"target: nomad\n", "target"},
		"old top-level context":  {"target: k8s\ncontext: prod\n", "context"},
		"local on k8s":           {"target: k8s\ncomponents:\n  - {id: a/b, mode: local}\n", "components[0].mode"},
		"k8s expose no hostname": {"target: k8s\ncomponents:\n  - {id: a/b, expose: true}\n", "components[0].hostname"},
		"localPort without mode": {"target: docker\ncomponents:\n  - {id: a/b, localPort: 9000}\n", "components[0].localPort"},
		"localPort conflict":     {"target: docker\ncomponents:\n  - {id: a/b, mode: local, localPort: 9000}\n  - {id: a/c, mode: local, localPort: 9000}\n", "components[1].localPort"},
		"duplicate entry":        {"target: docker\ncomponents:\n  - {id: a/b}\n  - {id: a/b}\n", "components[1].id"},
		"bad versioned id":       {"target: docker\ncomponents:\n  - {id: a/b@latest}\n", "components[0].id"},
		"member with version":    {"target: docker\ncomponents:\n  - {id: a/s, members: [a/b@1.0.0]}\n", "components[0].members[0]"},
		"member self":            {"target: docker\ncomponents:\n  - {id: a/s, members: [a/s]}\n", "components[0].members[0]"},
		"bad var name":           {"target: docker\nvars:\n  1BAD: x\n", "vars.1BAD"},
		"replicas zero":          {"target: k8s\ncomponents:\n  - {id: a/b, replicas: 0}\n", "components[0].replicas"},
		"exposePort w/o expose":  {"target: docker\ncomponents:\n  - {id: a/b, exposePort: 8080}\n", "components[0].exposePort"},
		"egress both":            {"target: k8s\nk8s:\n  networkPolicy:\n    enabled: true\n    egress:\n      enabled: true\n      allowTo:\n        - {name: db, namespace: x, cidr: 10.0.0.0/8}\n", "k8s.networkPolicy.egress.allowTo[0]"},
		"entry not mapping":      {"target: docker\ncomponents:\n  - a/b\n", "components[0]"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := parse(t, tc.yaml, deployfile.RoleLocal)
			require.Error(t, err)
			assert.Contains(t, fields(err), tc.field)
		})
	}
}

func TestWarningsForTarget(t *testing.T) {
	_, warnings, err := parse(t, "target: docker\nk8s:\n  namespace: shop\ncomponents:\n  - {id: a/b, replicas: 2}\n", deployfile.RoleTeam)
	require.NoError(t, err)
	assert.Len(t, warnings, 2)

	_, warnings, err = parse(t, "target: k8s\ncomponents:\n  - {id: a/b, expose: true, hostname: a.example.com, exposePort: 8080}\n", deployfile.RoleTeam)
	require.NoError(t, err)
	assert.Len(t, warnings, 1)
}

func TestSettingsDefaults(t *testing.T) {
	f, _, err := parse(t, "target: k8s\n", deployfile.RoleTeam)
	require.NoError(t, err)
	assert.True(t, f.ShouldCreateNamespace())
	assert.False(t, f.NetworkPolicyEnabled())
	assert.False(t, f.ServiceAccountEnabled())
}
```

- [ ] **Step 3: Run to see failure**

Run: `go test ./internal/deployfile/...`
Expected: FAIL — package does not exist.

- [ ] **Step 4: Implement the types**

`internal/deployfile/deployfile.go`:

```go
// Package deployfile 负责部署层文件：团队的 deploy.yaml、个人的 deploy.local.yaml、
// 以及 -f 显式指定的环境文件（deploy.prod.yaml 之类）。三者结构完全相同，只有"角色"不同——
// mode: debug 只允许出现在个人文件里（提案 §6.4）。
package deployfile

import (
	"strings"

	"github.com/brickkit/brickkit/internal/manifest"
)

const (
	TargetDocker = "docker"
	TargetPodman = "podman"
	TargetK8s    = "k8s"

	ModeEnabled = "enabled"
	ModeDisable = "disable"
	ModeLocal   = "local"
	ModeDebug   = "debug"

	PodSecurityRestricted = "restricted"

	FileTeam  = "deploy.yaml"
	FileLocal = "deploy.local.yaml"

	MinPort = 1
	MaxPort = 65535
)

// Role 是这份部署文件的角色。
type Role int

const (
	// RoleTeam：deploy.yaml 或 -f 指定的文件，进 Git、给团队评审。
	RoleTeam Role = iota
	// RoleLocal：deploy.local.yaml，个人本地文件，不进 Git。
	RoleLocal
)

// File 是一份部署文件的完整结构。
type File struct {
	Target string `yaml:"target" jsonschema:"enum=docker|podman|k8s"`
	// K8s 收拢所有只在 target: k8s 下有意义的项目级设置；其它 target 下写了会警告。
	K8s *K8s `yaml:"k8s,omitempty"`
	// Vars 覆盖 config/vars.yaml 里的同名公共变量，只作用于 $var: 查找（附录 A、提案 §6.1）。
	// 值里的 ${VAR} 解析时不展开——它们多半是密钥，何时求值由渲染器决定。
	Vars       map[string]any `yaml:"vars,omitempty"`
	Components []Component    `yaml:"components,omitempty"`

	// Source 是文件路径，只用于报错。
	Source string `yaml:"-"`
}

// K8s 是 K8s 专属的项目级设置（字段语义与旧 brickkit.yaml 的 deploy.* 相同）。
type K8s struct {
	Context            string            `yaml:"context,omitempty"`
	Namespace          string            `yaml:"namespace,omitempty"`
	CreateNamespace    *bool             `yaml:"createNamespace,omitempty"`
	PodSecurity        string            `yaml:"podSecurity,omitempty" jsonschema:"enum=restricted"`
	ImagePullSecrets   []string          `yaml:"imagePullSecrets,omitempty"`
	IngressClass       string            `yaml:"ingressClass,omitempty"`
	IngressAnnotations map[string]string `yaml:"ingressAnnotations,omitempty"`
	NetworkPolicy      *NetworkPolicy    `yaml:"networkPolicy,omitempty"`
	ServiceAccount     *ServiceAccount   `yaml:"serviceAccount,omitempty"`
}

// NetworkPolicy 按依赖图生成网络策略的开关与补充规则。
type NetworkPolicy struct {
	Enabled           bool                     `yaml:"enabled"`
	IngressController *IngressControllerSource `yaml:"ingressController,omitempty"`
	AllowFrom         []AllowFromSource        `yaml:"allowFrom,omitempty"`
	Egress            *Egress                  `yaml:"egress,omitempty"`
}

// IngressControllerSource 定位 ingress controller 的 Pod。
type IngressControllerSource struct {
	Namespace   string            `yaml:"namespace"`
	PodSelector map[string]string `yaml:"podSelector,omitempty"`
}

// AllowFromSource 是依赖图之外的合法入站来源（监控、备份……）。
type AllowFromSource struct {
	Name        string            `yaml:"name"`
	Namespace   string            `yaml:"namespace"`
	PodSelector map[string]string `yaml:"podSelector,omitempty"`
	Ports       []int             `yaml:"ports,omitempty"`
}

// Egress 是出站白名单。
type Egress struct {
	Enabled bool            `yaml:"enabled"`
	AllowTo []AllowToTarget `yaml:"allowTo,omitempty"`
}

// AllowToTarget 是一个出站目标：集群内写 Namespace，集群外写 CIDR。
//
// 旧版的 resource 写法随 resources 一起废除（附录 A15）：数据库地址现在只是某个组件
// config 里的一串字符，平台不再知道它在哪，只能由使用者直接写位置与端口。
type AllowToTarget struct {
	Name        string            `yaml:"name"`
	Namespace   string            `yaml:"namespace,omitempty"`
	PodSelector map[string]string `yaml:"podSelector,omitempty"`
	CIDR        string            `yaml:"cidr,omitempty"`
	Ports       []int             `yaml:"ports,omitempty"`
}

// ServiceAccount 是"每个组件一个不挂令牌的 SA"开关。
type ServiceAccount struct {
	Enabled bool `yaml:"enabled"`
}

// Component 是一个组件的部署条目。
//
// ID 是裸 ID（覆盖该 ID 所有没有专属条目的版本）或 id@version（只覆盖那一个版本）。
// 外壳条目用 Members 列出实际收编的成员——这是成员关系的唯一来源（提案 §8.4）。
type Component struct {
	ID                 string              `yaml:"id"`
	Mode               string              `yaml:"mode,omitempty" jsonschema:"enum=enabled|disable|local|debug"`
	LocalPort          int                 `yaml:"localPort,omitempty"`
	Expose             bool                `yaml:"expose,omitempty"`
	ExposePort         int                 `yaml:"exposePort,omitempty"`
	Hostname           string              `yaml:"hostname,omitempty"`
	TLSSecret          string              `yaml:"tlsSecret,omitempty"`
	Replicas           *int                `yaml:"replicas,omitempty"`
	ServiceAccountName string              `yaml:"serviceAccountName,omitempty"`
	Resources          *manifest.Resources `yaml:"resources,omitempty"`
	Labels             map[string]string   `yaml:"labels,omitempty"`
	Members            []string            `yaml:"members,omitempty"`
}

// Key 把条目 ID 拆成组件 ID 与版本（裸 ID 时版本为空）。
func (c Component) Key() (id, version string) {
	if i := strings.LastIndex(c.ID, "@"); i >= 0 {
		return c.ID[:i], c.ID[i+1:]
	}
	return c.ID, ""
}

// ReplicaCount 返回副本数，未写时为 1。
func (c Component) ReplicaCount() int {
	if c.Replicas == nil {
		return 1
	}
	return *c.Replicas
}

// IsDisabled 表示钉死不跑。
func (c Component) IsDisabled() bool { return c.Mode == ModeDisable }

// IsPinned 表示钉死要跑（enabled / local / debug）。
func (c Component) IsPinned() bool {
	return c.Mode == ModeEnabled || c.Mode == ModeLocal || c.Mode == ModeDebug
}

// IsBareProcess 表示以裸进程运行，不生成容器（local / debug）。
func (c Component) IsBareProcess() bool { return c.Mode == ModeLocal || c.Mode == ModeDebug }

// Entry 返回覆盖 id@version 的条目：专属条目优先，其次是裸 ID 条目。
func (f *File) Entry(id, version string) (Component, bool) {
	var bare *Component
	for i := range f.Components {
		entryID, entryVersion := f.Components[i].Key()
		if entryID != id {
			continue
		}
		if entryVersion == version {
			return f.Components[i], true
		}
		if entryVersion == "" {
			bare = &f.Components[i]
		}
	}
	if bare != nil {
		return *bare, true
	}
	return Component{}, false
}

// Settings 返回 K8s 设置；没写 k8s: 时是零值。
func (f *File) Settings() K8s {
	if f.K8s == nil {
		return K8s{}
	}
	return *f.K8s
}

// ShouldCreateNamespace 返回是否由 CLI 创建命名空间（缺省 true）。
func (f *File) ShouldCreateNamespace() bool {
	s := f.Settings()
	return s.CreateNamespace == nil || *s.CreateNamespace
}

// NetworkPolicyEnabled 返回是否生成 NetworkPolicy。
func (f *File) NetworkPolicyEnabled() bool {
	np := f.Settings().NetworkPolicy
	return np != nil && np.Enabled
}

// EgressEnabled 返回是否生成出站策略。
func (f *File) EgressEnabled() bool {
	return f.NetworkPolicyEnabled() && f.K8s.NetworkPolicy.Egress != nil && f.K8s.NetworkPolicy.Egress.Enabled
}

// ServiceAccountEnabled 返回是否为每个组件生成 ServiceAccount。
func (f *File) ServiceAccountEnabled() bool {
	sa := f.Settings().ServiceAccount
	return sa != nil && sa.Enabled
}
```

- [ ] **Step 5: Implement parsing**

`internal/deployfile/parse.go`:

```go
package deployfile

import (
	"path/filepath"
	"reflect"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/envref"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcheck"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// ParseFile 读取并解析一份部署文件。文件不存在时原样返回 fs.ErrNotExist——
// 缺的是 deploy.yaml、deploy.local.yaml 还是 -f 给的文件，提示完全不同，由装载器决定。
func ParseFile(path string, role Role) (*File, []*clierr.Error, error) {
	data, err := yamlfile.Read(path)
	if err != nil {
		return nil, nil, err
	}
	return Parse(data, path, role)
}

// Parse 解析并校验部署文件，返回不阻断的警告。
func Parse(data []byte, source string, role Role) (*File, []*clierr.Error, error) {
	doc, err := yamlfile.Document(data, source, false)
	if err != nil {
		return nil, nil, err
	}
	envref.ExpandNode(doc, func(path []string) bool { return len(path) > 0 && path[0] == "vars" })

	shape := newProblems(source)
	checkShapes(doc, shape)
	yamlcheck.Walk(doc, reflect.TypeOf(File{}), shape)
	if shape.Len() > 0 {
		return nil, nil, shape.Err()
	}

	var f File
	decode := newProblems(source)
	if !yamlfile.Decode(doc, &f, decode) {
		return nil, nil, decode.Err()
	}
	f.Source = source
	warnings, err := f.Validate(role)
	if err != nil {
		return nil, nil, err
	}
	return &f, warnings, nil
}

func checkShapes(doc *yaml.Node, p *clierr.ProblemSet) {
	yamlfile.RequireMapping(yamlfile.Lookup(doc, "k8s"), "k8s", p)
	yamlfile.RequireMapping(yamlfile.Lookup(doc, "vars"), "vars", p)
	components := yamlfile.Lookup(doc, "components")
	yamlfile.RequireSequence(components, "components", p)
	if components == nil || components.Kind != yaml.SequenceNode {
		return
	}
	for i, item := range components.Content {
		field := yamlfile.Indexed("components", i)
		if item.Kind != yaml.MappingNode {
			p.Add(field, i18n.T(msgid.DeployfileComponentMustBeMapping))
			continue
		}
		labels := yamlfile.Lookup(item, "labels")
		yamlfile.RequireMapping(labels, field+".labels", p)
		if labels != nil && labels.Kind == yaml.MappingNode {
			yamlcheck.CheckStringValues(labels, field+".labels", p.Add)
		}
		yamlfile.RequireMapping(yamlfile.Lookup(item, "resources"), field+".resources", p)
		yamlfile.RequireSequence(yamlfile.Lookup(item, "members"), field+".members", p)
	}
}

func newProblems(source string) *clierr.ProblemSet {
	return clierr.NewProblemSet(clierr.CodeConfigInvalid, i18n.T(msgid.ProblemValidationFailed, filepath.Base(source))).
		WithSource(i18n.T(msgid.LabelFile), source)
}
```

- [ ] **Step 6: Implement validation**

`internal/deployfile/validate.go`:

```go
package deployfile

import (
	"regexp"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

var varNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Validate 校验整份部署文件；返回的切片是不阻断的警告（某字段在当前 target 下不起作用）。
func (f *File) Validate(role Role) ([]*clierr.Error, error) {
	p := newProblems(f.Source)
	f.validateTarget(p)
	f.validateK8s(p)
	f.validateVarNames(p)
	f.validateComponents(p, role)
	if err := p.Err(); err != nil {
		return nil, err
	}
	return f.targetWarnings(), nil
}

func (f *File) validateTarget(p *clierr.ProblemSet) {
	switch f.Target {
	case "":
		p.Missing("target")
	case TargetDocker, TargetPodman, TargetK8s:
	default:
		p.Add("target", i18n.T(msgid.ProblemMustBeOneOfThree, TargetDocker, TargetPodman, TargetK8s, f.Target))
	}
}

func (f *File) validateK8s(p *clierr.ProblemSet) {
	k := f.K8s
	if k == nil {
		return
	}
	switch k.PodSecurity {
	case "", PodSecurityRestricted:
	default:
		p.Add("k8s.podSecurity", i18n.T(msgid.ConfigPodSecurityOnly, PodSecurityRestricted, k.PodSecurity))
	}
	if k.Namespace != "" {
		if reason := projfile.ProjectNameProblem(k.Namespace); reason != "" {
			p.Add("k8s.namespace", i18n.T(msgid.ConfigNamespaceSameRule, reason))
		}
	}
	validateNetworkPolicy(p, k.NetworkPolicy)
}

func validateNetworkPolicy(p *clierr.ProblemSet, np *NetworkPolicy) {
	if np == nil {
		return
	}
	const base = "k8s.networkPolicy"
	if np.IngressController != nil && np.IngressController.Namespace == "" {
		p.Missing(base + ".ingressController.namespace")
	}
	for i, source := range np.AllowFrom {
		field := yamlfile.Indexed(base+".allowFrom", i)
		if source.Name == "" {
			p.Missing(field + ".name")
		}
		if source.Namespace == "" {
			p.Add(field+".namespace", i18n.T(msgid.ConfigAllowFromNamespaceMissing, entryLabel(source.Name, i)))
		}
		validatePortList(p, field+".ports", source.Ports)
	}
	if np.Egress == nil {
		return
	}
	for i, target := range np.Egress.AllowTo {
		field := yamlfile.Indexed(base+".egress.allowTo", i)
		if target.Name == "" {
			p.Missing(field + ".name")
		}
		label := entryLabel(target.Name, i)
		switch {
		case target.Namespace != "" && target.CIDR != "":
			p.Add(field, i18n.T(msgid.ConfigEgressBothNamespaceAndCIDR, label))
		case target.Namespace == "" && target.CIDR == "":
			p.Add(field, i18n.T(msgid.ConfigEgressNoTarget, label))
		}
		validatePortList(p, field+".ports", target.Ports)
	}
}

func validatePortList(p *clierr.ProblemSet, field string, ports []int) {
	for _, port := range ports {
		if port < MinPort || port > MaxPort {
			p.Add(field, i18n.T(msgid.ConfigPortInvalid, port))
		}
	}
}

func entryLabel(name string, i int) string {
	if name != "" {
		return name
	}
	return i18n.T(msgid.ConfigEntryOrdinal, i+1)
}

func (f *File) validateVarNames(p *clierr.ProblemSet) {
	names := make([]string, 0, len(f.Vars))
	for name := range f.Vars {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !varNameRe.MatchString(name) {
			p.Add("vars."+name, i18n.T(msgid.DeployfileVarNameInvalid))
		}
	}
}

func (f *File) validateComponents(p *clierr.ProblemSet, role Role) {
	seen := map[string]int{}
	localPorts := map[int]int{}
	exposePorts := map[int]int{}
	for i, c := range f.Components {
		field := yamlfile.Indexed("components", i)
		id, version := c.Key()

		if c.ID == "" {
			p.Missing(field + ".id")
		} else {
			if reason := manifest.ComponentIDProblem(id); reason != "" {
				p.Add(field+".id", reason)
			}
			if strings.Contains(c.ID, "@") && !manifest.IsExactVersion(version) {
				p.Add(field+".id", i18n.T(msgid.ConfigVersionNotExactRange, version))
			}
			if prev, ok := seen[c.ID]; ok {
				p.Add(field+".id", i18n.T(msgid.DeployfileEntryDuplicate, yamlfile.Indexed("components", prev), c.ID))
			} else {
				seen[c.ID] = i
			}
		}

		f.validateMode(p, field, c, role)
		validatePorts(p, field, i, c, localPorts, exposePorts)
		if c.TLSSecret != "" && !c.Expose {
			p.Add(field+".tlsSecret", i18n.T(msgid.ConfigTLSSecretNeedsExpose))
		}
		if c.Expose && c.Hostname == "" && f.Target == TargetK8s {
			p.Add(field+".hostname", i18n.T(msgid.ConfigHostnameMissing))
		}
		validateReplicas(p, field, c)
		manifest.ValidateLabels(c.Labels, field+".labels", p.Add)
		validateMembers(p, field, id, c.Members)
	}
}

func (f *File) validateMode(p *clierr.ProblemSet, field string, c Component, role Role) {
	switch c.Mode {
	case "", ModeEnabled, ModeDisable, ModeLocal:
	case ModeDebug:
		if role != RoleLocal {
			p.Add(field+".mode", i18n.T(msgid.DeployfileDebugOnlyLocal))
			return
		}
	default:
		p.Add(field+".mode", i18n.T(msgid.DeployfileModeInvalid, c.Mode))
		return
	}
	if c.IsBareProcess() && f.Target == TargetK8s {
		p.Add(field+".mode", i18n.T(msgid.ConfigModeK8sUnsupported, c.Mode))
	}
}

func validatePorts(p *clierr.ProblemSet, field string, index int, c Component, localPorts, exposePorts map[int]int) {
	if c.LocalPort != 0 {
		switch {
		case !c.IsBareProcess():
			p.Add(field+".localPort", i18n.T(msgid.DeployfileLocalPortNeedsMode))
		case c.LocalPort < MinPort || c.LocalPort > MaxPort:
			p.Add(field+".localPort", i18n.T(msgid.ProblemPortOutOfRange, MinPort, MaxPort, c.LocalPort))
		default:
			if prev, ok := localPorts[c.LocalPort]; ok {
				p.Add(field+".localPort", i18n.T(msgid.ConfigLocalPortConflict, yamlfile.Indexed("components", prev), c.LocalPort))
			} else {
				localPorts[c.LocalPort] = index
			}
		}
	}
	if c.ExposePort != 0 {
		switch {
		case !c.Expose:
			p.Add(field+".exposePort", i18n.T(msgid.ConfigExposePortNeedsExpose))
		case c.ExposePort < MinPort || c.ExposePort > MaxPort:
			p.Add(field+".exposePort", i18n.T(msgid.ProblemPortOutOfRange, MinPort, MaxPort, c.ExposePort))
		default:
			if prev, ok := exposePorts[c.ExposePort]; ok {
				p.Add(field+".exposePort", i18n.T(msgid.ConfigExposePortConflict, yamlfile.Indexed("components", prev), c.ExposePort))
			} else {
				exposePorts[c.ExposePort] = index
			}
		}
	}
}

func validateReplicas(p *clierr.ProblemSet, field string, c Component) {
	if c.Replicas == nil {
		return
	}
	if *c.Replicas < 1 {
		p.Add(field+".replicas", i18n.T(msgid.ConfigReplicasTooSmall, *c.Replicas))
		return
	}
	if c.IsBareProcess() {
		p.Add(field+".replicas", i18n.T(msgid.ConfigReplicasWithLocal))
	}
}

func validateMembers(p *clierr.ProblemSet, field, ownID string, members []string) {
	seen := map[string]bool{}
	for i, member := range members {
		memberField := yamlfile.Indexed(field+".members", i)
		switch {
		case member == "":
			p.Missing(memberField)
		case strings.Contains(member, "@"):
			p.Add(memberField, i18n.T(msgid.DeployfileMemberMustBeBareID, member))
		case member == ownID:
			p.Add(memberField, i18n.T(msgid.DeployfileMemberSelf))
		case seen[member]:
			p.Add(memberField, i18n.T(msgid.DeployfileMemberDuplicate, member))
		default:
			if reason := manifest.ComponentIDProblem(member); reason != "" {
				p.Add(memberField, reason)
			}
		}
		seen[member] = true
	}
}

// targetWarnings 提醒"这个字段在当前 target 下不起作用"：不阻断，但绝不静默忽略。
func (f *File) targetWarnings() []*clierr.Error {
	var out []*clierr.Error
	warn := func(field string) {
		out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
			i18n.T(msgid.DeployfileFieldIgnoredForTarget, field, f.Target)).
			WithDetail(i18n.T(msgid.LabelFile), f.Source))
	}
	if f.Target == TargetK8s {
		for i, c := range f.Components {
			if c.ExposePort != 0 {
				warn(yamlfile.Indexed("components", i) + ".exposePort")
			}
		}
		return out
	}
	if f.K8s != nil {
		warn("k8s")
	}
	for i, c := range f.Components {
		field := yamlfile.Indexed("components", i)
		if c.Replicas != nil {
			warn(field + ".replicas")
		}
		if c.ServiceAccountName != "" {
			warn(field + ".serviceAccountName")
		}
		if c.TLSSecret != "" {
			warn(field + ".tlsSecret")
		}
		if c.Hostname != "" {
			warn(field + ".hostname")
		}
	}
	return out
}
```

- [ ] **Step 7: Run tests and checks**

Run: `go build ./... && go vet ./internal/deployfile/... && go test ./internal/deployfile/... ./tests/i18nguard/... && .tools/bin/golangci-lint run ./internal/deployfile/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/deployfile internal/msgid/deployfile.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "feat(deployfile): deploy.yaml / deploy.local.yaml types, parsing and role-aware validation

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

## Task 5: `internal/configdir` — naming, value references, parsing, conflicts

**Files:**
- Create: `internal/configdir/name.go`, `internal/configdir/value.go`, `internal/configdir/parse.go`, `internal/configdir/conflict.go`, `internal/configdir/configdir_test.go`
- Create: `internal/msgid/configdir.go`
- Modify: `internal/i18n/catalog_en.go`, `internal/i18n/catalog_zh.go`

**Interfaces:**
- Consumes: `yamlfile.Document`, `yamlfile.CleanError`, `envref.Has`, `manifest.IsExactVersion`, `yamlcomment.Block`.
- Produces:
  - `configdir.VarsFile = "vars.yaml"`, `ArchiveDir = ".archive"`, `VarRefPrefix = "$var:"`, `FileRefPrefix = "file://"`
  - `FileBase(id string) string`, `FileName(id, version string) string`, `ParseFileName(name string) (base, version string, ok bool)`
  - `type Kind int` = `KindLiteral`, `KindVarRef`, `KindEnvTemplate`, `KindFileRef`, `KindSecretRef`
  - `type Value struct { Kind Kind; Text, Name, Path, SecretName, SecretKey string }` + `IsUnset() bool`, `String() string`
  - `IsValidName(s string) bool`, `ParseValue(raw any) (Value, error)`
  - `type Entry struct { Key string; Value Value; Line int }`, `type File struct { Path string; Entries []Entry }` + `Lookup(key) (Value, bool)`, `Map() map[string]Value`, `Keys() []string`
  - `ParseComponentFile(data []byte, source string) (*File, error)`, `ParseVarsFile(data []byte, source string) (*File, error)`, `ParseVarsMap(raw map[string]any, source string) (map[string]Value, error)`
  - `ConflictTag = "brickkit:conflict"`, `SideCurrent = "current"`, `SideProposed = "proposed"`
  - `type ConflictLine struct { Line int; Value, Side, Version string }`, `type ConflictKey struct { Key string; Lines []ConflictLine }`, `type FileConflict struct { Path string; Keys []ConflictKey }`, `type ConflictError struct { Files []FileConflict }` + `Error() string`, `Render() *clierr.Error`, `Merge(other *ConflictError)`
  - `ScalarYAML(v any) string`, `ConflictBlock(key, currentYAML, currentVersion, proposedYAML, proposedVersion string) string` (the writer P6 uses)

- [ ] **Step 1: Add the messages**

`internal/msgid/configdir.go` (the resolve/skeleton IDs are added in Tasks 6–7):

```go
package msgid

// internal/configdir（config/ 目录：公共变量、组件配置、冲突标记、解析、骨架）的文案。
const (
	ConfigdirValueUnencodable    = "configdir.value_unencodable"
	ConfigdirVarRefBadName       = "configdir.var_ref_bad_name"
	ConfigdirFileRefEmpty        = "configdir.file_ref_empty"
	ConfigdirKeyInvalid          = "configdir.key_invalid"
	ConfigdirVarsNoChain         = "configdir.vars_no_chain"
	ConfigdirConflictTitle       = "configdir.conflict_title"
	ConfigdirConflictLineLabel   = "configdir.conflict_line_label"
	ConfigdirConflictCurrent     = "configdir.conflict_current"
	ConfigdirConflictProposed    = "configdir.conflict_proposed"
	ConfigdirConflictHintEdit    = "configdir.conflict_hint_edit"
	ConfigdirConflictHintNoFormat = "configdir.conflict_hint_no_format"
	ConfigdirConflictTipEditor   = "configdir.conflict_tip_editor"
	ConfigdirConflictBlockNote   = "configdir.conflict_block_note"
)
```

`catalog_en.go`:

```go
	msgid.ConfigdirValueUnencodable:     "the value cannot be encoded: %[1]s",
	msgid.ConfigdirVarRefBadName:        "%[1]q is not a valid $var: reference; write $var:NAME, where NAME uses letters, digits and underscores",
	msgid.ConfigdirFileRefEmpty:         "file:// needs a path after it (relative to the project root)",
	msgid.ConfigdirKeyInvalid:           "must be a valid environment variable name: letters, digits and underscores, not starting with a digit",
	msgid.ConfigdirVarsNoChain:          "a shared variable cannot reference another one with $var: — only ${ENV_VAR} and file:// are allowed here",
	msgid.ConfigdirConflictTitle:        "Error: unresolved configuration conflicts",
	msgid.ConfigdirConflictLineLabel:    "Line %[1]d",
	msgid.ConfigdirConflictCurrent:      "%[1]s (your previous value, %[2]s)",
	msgid.ConfigdirConflictProposed:     "%[1]s (suggested by %[2]s)",
	msgid.ConfigdirConflictHintEdit:     "Open the file in a plain text editor, keep the line you want, delete the other one and the comment above them, then run the command again",
	msgid.ConfigdirConflictHintNoFormat: "Do not run yq or your editor's Format Document on this file: they silently drop one of the duplicate keys, and the conflict disappears without being resolved",
	msgid.ConfigdirConflictTipEditor:    "Your editor may mark this file as invalid YAML. That is expected: BrickKit wrote the duplicate key on purpose so the conflict cannot be missed",
	msgid.ConfigdirConflictBlockNote:    "⚠️ Config conflict: upgrading to %[1]s changed the suggested value of this key.\nKeep one line, delete the other and this comment; until then brickkit refuses to start.",
```

`catalog_zh.go`:

```go
	msgid.ConfigdirValueUnencodable:     "这个值无法编码：%[1]s",
	msgid.ConfigdirVarRefBadName:        "%[1]q 不是合法的 $var: 引用；写成 $var:NAME，NAME 由字母、数字、下划线组成",
	msgid.ConfigdirFileRefEmpty:         "file:// 后面要跟一个路径（相对项目根）",
	msgid.ConfigdirKeyInvalid:           "必须是合法的环境变量名：字母、数字、下划线，不能以数字开头",
	msgid.ConfigdirVarsNoChain:          "公共变量不能再用 $var: 引用别的公共变量——这里只允许 ${ENV_VAR} 与 file://",
	msgid.ConfigdirConflictTitle:        "错误：检测到未解决的配置冲突",
	msgid.ConfigdirConflictLineLabel:    "第 %[1]d 行",
	msgid.ConfigdirConflictCurrent:      "%[1]s（你之前的值，%[2]s）",
	msgid.ConfigdirConflictProposed:     "%[1]s（%[2]s 的建议值）",
	msgid.ConfigdirConflictHintEdit:     "用纯文本编辑器打开文件，保留你要的那一行，删掉另一行和上方的注释，然后重新执行命令",
	msgid.ConfigdirConflictHintNoFormat: "不要对这个文件用 yq 或编辑器的\"格式化文档\"：它们会悄悄丢掉其中一个重复键，冲突没解决就消失了",
	msgid.ConfigdirConflictTipEditor:    "编辑器可能把这个文件标成非法 YAML——这是正常的：BrickKit 故意写了重复键，让冲突不可能被忽略",
	msgid.ConfigdirConflictBlockNote:    "⚠️ 配置冲突：升级到 %[1]s 时这个键的建议值变了。\n保留一行、删掉另一行和本注释；在那之前 brickkit 拒绝启动。",
```

- [ ] **Step 2: Write the failing tests**

`internal/configdir/configdir_test.go`:

```go
package configdir_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
)

func TestFileNaming(t *testing.T) {
	assert.Equal(t, "erp-backend.yaml", configdir.FileName("erp/backend", ""))
	assert.Equal(t, "erp-backend@1.0.0.yaml", configdir.FileName("erp/backend", "1.0.0"))

	cases := map[string]struct {
		base, version string
		ok            bool
	}{
		"erp-backend.yaml":       {"erp-backend", "", true},
		"erp-backend@1.0.0.yaml": {"erp-backend", "1.0.0", true},
		"vars.yaml":              {"", "", false},
		"x@latest.yaml":          {"", "", false},
		"notes.txt":              {"", "", false},
		".yaml":                  {"", "", false},
	}
	for name, want := range cases {
		base, version, ok := configdir.ParseFileName(name)
		assert.Equal(t, want.ok, ok, name)
		assert.Equal(t, want.base, base, name)
		assert.Equal(t, want.version, version, name)
	}
}

func TestParseValue(t *testing.T) {
	cases := []struct {
		raw  any
		kind configdir.Kind
		text string
	}{
		{nil, configdir.KindLiteral, ""},
		{"", configdir.KindLiteral, ""},
		{"abc", configdir.KindLiteral, "abc"},
		{5, configdir.KindLiteral, "5"},
		{2.5, configdir.KindLiteral, "2.5"},
		{float64(20), configdir.KindLiteral, "20"},
		{true, configdir.KindLiteral, "true"},
		{[]any{"a", "b"}, configdir.KindLiteral, `["a","b"]`},
		{map[string]any{"a": 1}, configdir.KindLiteral, `{"a":1}`},
		{"pg://${USER}@h", configdir.KindEnvTemplate, "pg://${USER}@h"},
	}
	for _, tc := range cases {
		v, err := configdir.ParseValue(tc.raw)
		require.NoError(t, err, tc.raw)
		assert.Equal(t, tc.kind, v.Kind, tc.raw)
		assert.Equal(t, tc.text, v.Text, tc.raw)
	}

	v, err := configdir.ParseValue("$var:DB_HOST")
	require.NoError(t, err)
	assert.Equal(t, configdir.Value{Kind: configdir.KindVarRef, Name: "DB_HOST"}, v)

	v, err = configdir.ParseValue("file://secrets/key.pem")
	require.NoError(t, err)
	assert.Equal(t, configdir.Value{Kind: configdir.KindFileRef, Path: "secrets/key.pem"}, v)

	v, err = configdir.ParseValue(map[string]any{"existingSecret": "db", "key": "password"})
	require.NoError(t, err)
	assert.Equal(t, configdir.Value{Kind: configdir.KindSecretRef, SecretName: "db", SecretKey: "password"}, v)

	_, err = configdir.ParseValue("$var:bad name")
	assert.Error(t, err)
	_, err = configdir.ParseValue("file://")
	assert.Error(t, err)

	assert.True(t, configdir.Value{}.IsUnset())
	assert.Equal(t, "$var:X", configdir.Value{Kind: configdir.KindVarRef, Name: "X"}.String())
}

func TestParseComponentFile(t *testing.T) {
	f, err := configdir.ParseComponentFile([]byte("# Component: erp/backend@1.0.0\nDB_HOST: $var:ERP_DB_HOST\nDB_PORT: 5432\nEMPTY: \"\"\n"), "config/erp-backend.yaml")
	require.NoError(t, err)
	assert.Equal(t, []string{"DB_HOST", "DB_PORT", "EMPTY"}, f.Keys())
	assert.Equal(t, 2, f.Entries[0].Line)
	v, ok := f.Lookup("DB_PORT")
	require.True(t, ok)
	assert.Equal(t, "5432", v.Text)
	assert.True(t, f.Map()["EMPTY"].IsUnset())
}

func TestParseEmptyFiles(t *testing.T) {
	f, err := configdir.ParseComponentFile([]byte("# nothing yet\n"), "config/a-b.yaml")
	require.NoError(t, err)
	assert.Empty(t, f.Entries)
}

func TestParseComponentFileBadKey(t *testing.T) {
	_, err := configdir.ParseComponentFile([]byte("db-host: x\n"), "config/a-b.yaml")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)
}

func TestParseVarsRejectsChain(t *testing.T) {
	_, err := configdir.ParseVarsFile([]byte("A: $var:B\n"), "config/vars.yaml")
	require.Error(t, err)

	f, err := configdir.ParseVarsFile([]byte("A: ${SECRET}\nB: file://k.pem\n"), "config/vars.yaml")
	require.NoError(t, err)
	assert.Equal(t, configdir.KindEnvTemplate, f.Map()["A"].Kind)

	_, err = configdir.ParseVarsMap(map[string]any{"A": "$var:B"}, "deploy.yaml")
	require.Error(t, err)
	m, err := configdir.ParseVarsMap(map[string]any{"A": "x", "N": 3}, "deploy.yaml")
	require.NoError(t, err)
	assert.Equal(t, "3", m["N"].Text)
}

func TestParseComponentFileUnmarkedDuplicate(t *testing.T) {
	_, err := configdir.ParseComponentFile([]byte("A: 1\nB: 2\nA: 3\n"), "config/a-b.yaml")
	var conflict *configdir.ConflictError
	require.True(t, errors.As(err, &conflict))
	require.Len(t, conflict.Files, 1)
	key := conflict.Files[0].Keys[0]
	assert.Equal(t, "A", key.Key)
	assert.Equal(t, []configdir.ConflictLine{{Line: 1, Value: "1"}, {Line: 3, Value: "3"}}, key.Lines)
	assert.Equal(t, clierr.CodeConfigConflict, conflict.Render().Code)
}

func TestConflictBlockRoundTrip(t *testing.T) {
	block := configdir.ConflictBlock("LOG_LEVEL",
		configdir.ScalarYAML("debug"), "1.0.0", configdir.ScalarYAML("warn"), "2.0.0")
	_, err := configdir.ParseComponentFile([]byte("X: 1\n"+block), "config/erp-backend.yaml")

	var conflict *configdir.ConflictError
	require.True(t, errors.As(err, &conflict))
	lines := conflict.Files[0].Keys[0].Lines
	require.Len(t, lines, 2)
	assert.Equal(t, configdir.ConflictLine{Line: lines[0].Line, Value: "debug", Side: configdir.SideCurrent, Version: "1.0.0"}, lines[0])
	assert.Equal(t, configdir.ConflictLine{Line: lines[1].Line, Value: "warn", Side: configdir.SideProposed, Version: "2.0.0"}, lines[1])

	merged := &configdir.ConflictError{}
	merged.Merge(conflict)
	merged.Merge(conflict)
	assert.Len(t, merged.Files, 2)
}

func TestScalarYAML(t *testing.T) {
	assert.Equal(t, "debug", configdir.ScalarYAML("debug"))
	assert.Equal(t, `"5"`, configdir.ScalarYAML("5"))
	assert.Equal(t, "5", configdir.ScalarYAML(5))
	assert.Equal(t, `"a\nb"`, configdir.ScalarYAML("a\nb"))
}
```

- [ ] **Step 3: Run to see failure**

Run: `go test ./internal/configdir/...`
Expected: FAIL — package does not exist.

- [ ] **Step 4: Implement naming and values**

`internal/configdir/name.go`:

```go
// Package configdir 负责 config/ 目录——三层文件的"业务配置"层：每个组件一份环境变量文件、
// 一份项目级公共变量 vars.yaml，以及两者之间显式的 $var: 引用（提案 §7）。
package configdir

import (
	"strings"

	"github.com/brickkit/brickkit/internal/manifest"
)

const (
	// VarsFile 是公共变量文件名。
	VarsFile = "vars.yaml"
	// ArchiveDir 是归档目录名（config/.archive/，不进 Git）。
	ArchiveDir = ".archive"
	ext        = ".yaml"
)

// FileBase 把组件 ID 变成文件名主干：erp/backend → erp-backend。
func FileBase(id string) string { return strings.ReplaceAll(id, "/", "-") }

// FileName 返回组件的配置文件名；version 为空时是跟随当前版本的无版本文件。
func FileName(id, version string) string {
	if version == "" {
		return FileBase(id) + ext
	}
	return FileBase(id) + "@" + version + ext
}

// ParseFileName 反解 config/ 下的文件名。vars.yaml、非 .yaml 文件、版本不是精确版本的都不算。
func ParseFileName(name string) (base, version string, ok bool) {
	if name == VarsFile || !strings.HasSuffix(name, ext) {
		return "", "", false
	}
	stem := strings.TrimSuffix(name, ext)
	if i := strings.LastIndex(stem, "@"); i >= 0 {
		base, version = stem[:i], stem[i+1:]
		if base == "" || !manifest.IsExactVersion(version) {
			return "", "", false
		}
		return base, version, true
	}
	if stem == "" {
		return "", "", false
	}
	return stem, "", true
}
```

`internal/configdir/value.go`:

```go
package configdir

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/brickkit/brickkit/internal/envref"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// 引用写法的前缀（提案 §7.2.3、§7.4）。
const (
	VarRefPrefix  = "$var:"
	FileRefPrefix = "file://"
)

// Kind 是一个配置值"是什么"：字面量，还是某种引用。
//
// 解析时只认形状、不求值——什么时候求值取决于部署目标（附录 A6/A7），那是 P2 渲染器的事。
type Kind int

const (
	// KindLiteral：写死的值（数字、布尔、列表/映射按 JSON 编码成一行）。
	KindLiteral Kind = iota
	// KindVarRef：$var:NAME，从 deploy 文件的 vars: 或 config/vars.yaml 取。
	KindVarRef
	// KindEnvTemplate：含 ${VAR} 的字符串，从进程环境 / .env 求值。
	KindEnvTemplate
	// KindFileRef：file://path，读本地文件内容（路径相对项目根）。
	KindFileRef
	// KindSecretRef：{ existingSecret, key }，引用集群里已有的 K8s Secret。
	KindSecretRef
)

// Value 是一个配置值。
type Value struct {
	Kind Kind
	// Text：KindLiteral 的值，或 KindEnvTemplate 的原文。
	Text string
	// Name：KindVarRef 引用的变量名。
	Name string
	// Path：KindFileRef 的文件路径。
	Path string
	// SecretName / SecretKey：KindSecretRef 引用的 Secret 与其中的 key。
	SecretName string
	SecretKey  string
}

// IsUnset 表示"没给值"：YAML 的 null 或空串。没给值的键绝不注入空串；
// 必填键没给值就是缺失（骨架里的 KEY: "" 正是这种）。
func (v Value) IsUnset() bool { return v.Kind == KindLiteral && v.Text == "" }

// String 把值还原成使用者写的样子，用于提示。
func (v Value) String() string {
	switch v.Kind {
	case KindVarRef:
		return VarRefPrefix + v.Name
	case KindFileRef:
		return FileRefPrefix + v.Path
	case KindSecretRef:
		return fmt.Sprintf("{existingSecret: %s, key: %s}", v.SecretName, v.SecretKey)
	default:
		return v.Text
	}
}

var nameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// IsValidName 报告 s 是否是合法的变量名（配置键、公共变量名都用这条规则）。
func IsValidName(s string) bool { return nameRe.MatchString(s) }

// ParseValue 把 YAML 解码出来的原始值归类成 Value。
func ParseValue(raw any) (Value, error) {
	switch v := raw.(type) {
	case nil:
		return literal(""), nil
	case string:
		return parseString(v)
	case bool:
		return literal(strconv.FormatBool(v)), nil
	case int:
		return literal(strconv.Itoa(v)), nil
	case int64:
		return literal(strconv.FormatInt(v, 10)), nil
	case uint64:
		return literal(strconv.FormatUint(v, 10)), nil
	case float64:
		return literal(formatFloat(v)), nil
	case map[string]any:
		if name, key, ok := secretRef(v); ok {
			return Value{Kind: KindSecretRef, SecretName: name, SecretKey: key}, nil
		}
		return jsonLiteral(v)
	default:
		return jsonLiteral(v)
	}
}

func literal(text string) Value { return Value{Kind: KindLiteral, Text: text} }

func parseString(s string) (Value, error) {
	switch {
	case strings.HasPrefix(s, VarRefPrefix):
		name := strings.TrimPrefix(s, VarRefPrefix)
		if !IsValidName(name) {
			return Value{}, errors.New(i18n.T(msgid.ConfigdirVarRefBadName, s))
		}
		return Value{Kind: KindVarRef, Name: name}, nil
	case strings.HasPrefix(s, FileRefPrefix):
		path := strings.TrimPrefix(s, FileRefPrefix)
		if path == "" {
			return Value{}, errors.New(i18n.T(msgid.ConfigdirFileRefEmpty))
		}
		return Value{Kind: KindFileRef, Path: path}, nil
	case envref.Has(s):
		return Value{Kind: KindEnvTemplate, Text: s}, nil
	default:
		return literal(s), nil
	}
}

func secretRef(m map[string]any) (name, key string, ok bool) {
	if len(m) != 2 {
		return "", "", false
	}
	name, nameOK := m["existingSecret"].(string)
	key, keyOK := m["key"].(string)
	if !nameOK || !keyOK || name == "" || key == "" {
		return "", "", false
	}
	return name, key, true
}

// jsonLiteral 把列表 / 映射编码成一行 JSON：环境变量只能是字符串，
// JSON 是组件最容易原样解析回来的写法。
func jsonLiteral(v any) (Value, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return Value{}, errors.New(i18n.T(msgid.ConfigdirValueUnencodable, err.Error()))
	}
	return literal(string(data)), nil
}

// formatFloat 让 YAML 里被解成 float64 的整数不带小数点：20 而不是 20.000000。
func formatFloat(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}
```

- [ ] **Step 5: Implement flat-file parsing and conflicts**

`internal/configdir/parse.go`:

```go
package configdir

import (
	"path/filepath"
	"sort"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// Entry 是配置文件里的一行。
type Entry struct {
	Key   string
	Value Value
	Line  int
}

// File 是一份扁平的配置文件（组件配置或 vars.yaml），保持文件里的顺序。
type File struct {
	Path    string
	Entries []Entry
}

// Lookup 取一个键的值。
func (f *File) Lookup(key string) (Value, bool) {
	if f == nil {
		return Value{}, false
	}
	for _, e := range f.Entries {
		if e.Key == key {
			return e.Value, true
		}
	}
	return Value{}, false
}

// Map 把条目转成 map。
func (f *File) Map() map[string]Value {
	out := map[string]Value{}
	if f == nil {
		return out
	}
	for _, e := range f.Entries {
		out[e.Key] = e.Value
	}
	return out
}

// Keys 按文件顺序返回全部键。
func (f *File) Keys() []string {
	if f == nil {
		return nil
	}
	out := make([]string, 0, len(f.Entries))
	for _, e := range f.Entries {
		out = append(out, e.Key)
	}
	return out
}

// ParseComponentFile 解析 config/<id>.yaml。有重复键时返回 *ConflictError（大声失败，提案 §12.3）。
func ParseComponentFile(data []byte, source string) (*File, error) {
	return parseFlat(data, source, false)
}

// ParseVarsFile 解析 config/vars.yaml：不允许 $var: 链式引用（提案 §7.2.4）。
func ParseVarsFile(data []byte, source string) (*File, error) {
	return parseFlat(data, source, true)
}

// ParseVarsMap 按 vars.yaml 的同一套规则检查部署文件里的 vars:。
func ParseVarsMap(raw map[string]any, source string) (map[string]Value, error) {
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	p := newProblems(source)
	out := make(map[string]Value, len(raw))
	for _, key := range keys {
		field := "vars." + key
		v, err := ParseValue(raw[key])
		if err != nil {
			p.Add(field, err.Error())
			continue
		}
		if v.Kind == KindVarRef {
			p.Add(field, i18n.T(msgid.ConfigdirVarsNoChain))
			continue
		}
		out[key] = v
	}
	if err := p.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func parseFlat(data []byte, source string, isVars bool) (*File, error) {
	doc, err := yamlfile.Document(data, source, true)
	if err != nil {
		return nil, err
	}
	f := &File{Path: source}
	if doc == nil {
		return f, nil
	}
	// 冲突优先：文件处在冲突状态时，别的问题都等冲突解决之后再说
	if conflict := findConflicts(doc, source); conflict != nil {
		return nil, conflict
	}

	p := newProblems(source)
	for i := 0; i+1 < len(doc.Content); i += 2 {
		keyNode, valueNode := doc.Content[i], doc.Content[i+1]
		key := keyNode.Value
		if !IsValidName(key) {
			p.Add(key, i18n.T(msgid.ConfigdirKeyInvalid))
			continue
		}
		var raw any
		if err := valueNode.Decode(&raw); err != nil {
			p.Add(key, yamlfile.CleanError(err))
			continue
		}
		v, err := ParseValue(raw)
		if err != nil {
			p.Add(key, err.Error())
			continue
		}
		if isVars && v.Kind == KindVarRef {
			p.Add(key, i18n.T(msgid.ConfigdirVarsNoChain))
			continue
		}
		f.Entries = append(f.Entries, Entry{Key: key, Value: v, Line: keyNode.Line})
	}
	if err := p.Err(); err != nil {
		return nil, err
	}
	return f, nil
}

func newProblems(source string) *clierr.ProblemSet {
	return clierr.NewProblemSet(clierr.CodeConfigInvalid, i18n.T(msgid.ProblemValidationFailed, filepath.Base(source))).
		WithSource(i18n.T(msgid.LabelFile), source)
}
```

`internal/configdir/conflict.go`:

```go
package configdir

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcomment"
)

// 冲突标记（提案 §12.3、附录 A4）：升级时 brickkit 把同一个键写两遍，每行行尾注释带
// "brickkit:conflict <side> <version>"。YAML 解析器遇到重复键会拒绝，所以冲突不可能被忽略；
// 这里认出标记，是为了把冷冰冰的"mapping key already defined"换成能直接照做的提示。
const (
	ConflictTag  = "brickkit:conflict"
	SideCurrent  = "current"
	SideProposed = "proposed"
)

// ConflictLine 是重复键的其中一行。Side 为空表示这一行不是 brickkit 写的（使用者手滑重复了）。
type ConflictLine struct {
	Line    int
	Value   string
	Side    string
	Version string
}

// ConflictKey 是一个出现了不止一次的键。
type ConflictKey struct {
	Key   string
	Lines []ConflictLine
}

// FileConflict 是一份文件里的全部冲突。
type FileConflict struct {
	Path string
	Keys []ConflictKey
}

// ConflictError 汇总一个或多个文件的冲突，一次全部报出。
type ConflictError struct {
	Files []FileConflict
}

func (e *ConflictError) Error() string { return e.Render().Error() }

// Merge 把另一组冲突并进来。
func (e *ConflictError) Merge(other *ConflictError) {
	if other != nil {
		e.Files = append(e.Files, other.Files...)
	}
}

// Render 把冲突变成给使用者看的错误。
func (e *ConflictError) Render() *clierr.Error {
	err := clierr.New(clierr.CodeConfigConflict, i18n.T(msgid.ConfigdirConflictTitle))
	for _, file := range e.Files {
		for _, key := range file.Keys {
			err = err.WithDetail(i18n.T(msgid.LabelFile), file.Path).
				WithDetail(i18n.T(msgid.LabelConfigKey), key.Key)
			for _, line := range key.Lines {
				err = err.WithDetail(i18n.T(msgid.ConfigdirConflictLineLabel, line.Line), lineText(line))
			}
		}
	}
	return err.
		WithHint(i18n.T(msgid.ConfigdirConflictHintEdit), i18n.T(msgid.ConfigdirConflictHintNoFormat)).
		WithTip(i18n.T(msgid.ConfigdirConflictTipEditor))
}

func lineText(l ConflictLine) string {
	switch l.Side {
	case SideCurrent:
		return i18n.T(msgid.ConfigdirConflictCurrent, l.Value, l.Version)
	case SideProposed:
		return i18n.T(msgid.ConfigdirConflictProposed, l.Value, l.Version)
	default:
		return l.Value
	}
}

// findConflicts 找出顶层映射里出现不止一次的键；没有时返回 nil。
func findConflicts(doc *yaml.Node, source string) *ConflictError {
	lines := map[string][]ConflictLine{}
	var order []string
	for i := 0; i+1 < len(doc.Content); i += 2 {
		keyNode, valueNode := doc.Content[i], doc.Content[i+1]
		if _, seen := lines[keyNode.Value]; !seen {
			order = append(order, keyNode.Value)
		}
		side, version := parseMarker(valueNode.LineComment)
		if side == "" {
			side, version = parseMarker(keyNode.LineComment)
		}
		lines[keyNode.Value] = append(lines[keyNode.Value], ConflictLine{
			Line: keyNode.Line, Value: displayValue(valueNode), Side: side, Version: version,
		})
	}
	var keys []ConflictKey
	for _, key := range order {
		if len(lines[key]) > 1 {
			keys = append(keys, ConflictKey{Key: key, Lines: lines[key]})
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return &ConflictError{Files: []FileConflict{{Path: source, Keys: keys}}}
}

func parseMarker(comment string) (side, version string) {
	i := strings.Index(comment, ConflictTag)
	if i < 0 {
		return "", ""
	}
	fields := strings.Fields(comment[i+len(ConflictTag):])
	if len(fields) < 2 || (fields[0] != SideCurrent && fields[0] != SideProposed) {
		return "", ""
	}
	return fields[0], fields[1]
}

func displayValue(v *yaml.Node) string {
	if v.Kind == yaml.ScalarNode {
		return v.Value
	}
	data, err := yaml.Marshal(v)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// ScalarYAML 把一个值写成能放在 "KEY: " 后面的单行 YAML。
// 多行字符串写成 JSON 双引号形式（它同时是合法的 YAML 双引号标量），保证一行一个键。
func ScalarYAML(v any) string {
	if s, ok := v.(string); ok && strings.ContainsAny(s, "\n\r") {
		data, _ := json.Marshal(s)
		return string(data)
	}
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimRight(string(data), "\n")
}

// ConflictBlock 生成一处冲突：说明注释 + 同一个键的两行（旧值在前，新建议值在后）。
// currentYAML / proposedYAML 用 ScalarYAML 生成。
func ConflictBlock(key, currentYAML, currentVersion, proposedYAML, proposedVersion string) string {
	var b strings.Builder
	b.WriteString(yamlcomment.Block("", i18n.T(msgid.ConfigdirConflictBlockNote, proposedVersion)))
	fmt.Fprintf(&b, "%s: %s  # %s %s %s\n", key, currentYAML, ConflictTag, SideCurrent, currentVersion)
	fmt.Fprintf(&b, "%s: %s  # %s %s %s\n", key, proposedYAML, ConflictTag, SideProposed, proposedVersion)
	return b.String()
}
```

- [ ] **Step 6: Run tests and checks**

Run: `go build ./... && go vet ./internal/configdir/... && go test ./internal/configdir/... ./tests/i18nguard/... && .tools/bin/golangci-lint run ./internal/configdir/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/configdir internal/msgid/configdir.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "feat(configdir): config/ naming, value references, flat-file parsing and conflict markers

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
## Task 6: `configdir.Resolve` — precedence, `$var:` lookup, missing and unknown keys

**Files:**
- Create: `internal/configdir/resolve.go`, `internal/configdir/resolve_test.go`
- Modify: `internal/msgid/configdir.go`, `internal/i18n/catalog_en.go`, `internal/i18n/catalog_zh.go`

**Interfaces:**
- Consumes: Task 5's `Value`, `File`, `ParseValue`; `manifest.ConfigSchema`, `manifest.ConfigProperty`; `yamlcheck.Closest`.
- Produces (P2's inject consumes exactly these):
  - `type Origin string` = `OriginFile`, `OriginVar`, `OriginDefault`
  - `type Resolved struct { Key string; Value Value; Origin Origin; VarName string; Secret bool }` — `Value.Kind` is never `KindVarRef`; `Key` **is** the environment variable name (A10)
  - `type Input struct { ComponentID, Version string; Schema *manifest.ConfigSchema; File *File; Vars, DeployVars map[string]Value }`
  - `type Result struct { Values []Resolved; Missing []string; Warnings []*clierr.Error }` + `Get(key string) (Resolved, bool)`
  - `configdir.Resolve(in Input) (*Result, error)` — error only for dangling `$var:` references
  - `configdir.LookupVar(name string, deployVars, vars map[string]Value) (Value, bool)` — deploy `vars:` first, then `vars.yaml`

Precedence per key (spec §7.5 as corrected by Appendix A): a written, non-unset value in the component file (a `$var:` resolves through `LookupVar`) → the schema `default` → nothing (required ⇒ missing). A `$var:` whose target is unset counts as not given.

- [ ] **Step 1: Add the messages**

Append to the const block in `internal/msgid/configdir.go`:

```go
	ConfigdirUnknownKey         = "configdir.unknown_key"
	ConfigdirUnknownKeyGuess    = "configdir.unknown_key_guess"
	ConfigdirLabelDeclared      = "configdir.label_declared"
	ConfigdirNoSchema           = "configdir.no_schema"
	ConfigdirLabelIgnoredKeys   = "configdir.label_ignored_keys"
	ConfigdirSecretRefNotSecret = "configdir.secret_ref_not_secret"
	ConfigdirUndefinedVar       = "configdir.undefined_var"
	ConfigdirLabelUndefinedRef  = "configdir.label_undefined_ref"
	ConfigdirHintDefineVar      = "configdir.hint_define_var"
```

`catalog_en.go`:

```go
	msgid.ConfigdirUnknownKey:         "%[1]s: %[2]s is not declared in the component's configSchema, so it has no effect",
	msgid.ConfigdirUnknownKeyGuess:    "Did you mean %[1]s?",
	msgid.ConfigdirLabelDeclared:      "Declared keys",
	msgid.ConfigdirNoSchema:           "%[1]s declares no configSchema, so everything in its config file is ignored",
	msgid.ConfigdirLabelIgnoredKeys:   "Ignored keys",
	msgid.ConfigdirSecretRefNotSecret: "%[1]s: %[2]s uses the { existingSecret, key } form, but the component does not declare it secret: true, so it is skipped",
	msgid.ConfigdirUndefinedVar:       "Error: %[1]s references shared variables that are defined nowhere",
	msgid.ConfigdirLabelUndefinedRef:  "Undefined reference",
	msgid.ConfigdirHintDefineVar:      "Define the variable in config/vars.yaml, or in the deploy file's vars:",
```

`catalog_zh.go`:

```go
	msgid.ConfigdirUnknownKey:         "%[1]s：%[2]s 不在组件的 configSchema 里，不会生效",
	msgid.ConfigdirUnknownKeyGuess:    "是不是想写 %[1]s？",
	msgid.ConfigdirLabelDeclared:      "已声明的配置项",
	msgid.ConfigdirNoSchema:           "%[1]s 没有声明 configSchema，它的配置文件里的内容全部不会生效",
	msgid.ConfigdirLabelIgnoredKeys:   "被忽略的键",
	msgid.ConfigdirSecretRefNotSecret: "%[1]s：%[2]s 用了 { existingSecret, key } 写法，但组件没有把它声明为 secret: true，已跳过",
	msgid.ConfigdirUndefinedVar:       "错误：%[1]s 引用了哪里都没有定义的公共变量",
	msgid.ConfigdirLabelUndefinedRef:  "未定义的引用",
	msgid.ConfigdirHintDefineVar:      "在 config/vars.yaml（或部署文件的 vars:）里定义这个变量",
```

- [ ] **Step 2: Write the failing tests**

`internal/configdir/resolve_test.go`:

```go
package configdir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/manifest"
)

func backendSchema() *manifest.ConfigSchema {
	return &manifest.ConfigSchema{
		Properties: map[string]manifest.ConfigProperty{
			"DB_HOST":     {Type: "string"},
			"DB_PASSWORD": {Type: "string", Secret: true},
			"DB_PORT":     {Type: "integer", Default: 5432},
			"LOG_LEVEL":   {Type: "string", Default: "info"},
			"OPTIONAL_X":  {Type: "string"},
		},
		Required: []string{"DB_HOST", "DB_PASSWORD"},
	}
}

func mustFile(t *testing.T, data string) *configdir.File {
	t.Helper()
	f, err := configdir.ParseComponentFile([]byte(data), "config/erp-backend.yaml")
	require.NoError(t, err)
	return f
}

func mustVars(t *testing.T, data string) map[string]configdir.Value {
	t.Helper()
	f, err := configdir.ParseVarsFile([]byte(data), "config/vars.yaml")
	require.NoError(t, err)
	return f.Map()
}

func TestResolveVarPrecedence(t *testing.T) {
	res, err := configdir.Resolve(configdir.Input{
		ComponentID: "erp/backend", Version: "2.0.0",
		Schema:     backendSchema(),
		File:       mustFile(t, "DB_HOST: $var:ERP_DB_HOST\nDB_PASSWORD: $var:DB_PASSWORD\nLOG_LEVEL: debug\nDB_HOTS: typo\n"),
		Vars:       mustVars(t, "ERP_DB_HOST: pg.internal\nDB_PASSWORD: ${SECRET_DB_PASSWORD}\n"),
		DeployVars: map[string]configdir.Value{"DB_PASSWORD": {Kind: configdir.KindLiteral, Text: "local_pwd"}},
	})
	require.NoError(t, err)
	assert.Empty(t, res.Missing)

	host, _ := res.Get("DB_HOST")
	assert.Equal(t, configdir.Resolved{Key: "DB_HOST", Value: configdir.Value{Kind: configdir.KindLiteral, Text: "pg.internal"},
		Origin: configdir.OriginVar, VarName: "ERP_DB_HOST"}, host)
	pwd, _ := res.Get("DB_PASSWORD")
	assert.Equal(t, "local_pwd", pwd.Value.Text, "deploy vars: override vars.yaml")
	assert.True(t, pwd.Secret)
	port, _ := res.Get("DB_PORT")
	assert.Equal(t, "5432", port.Value.Text)
	assert.Equal(t, configdir.OriginDefault, port.Origin)
	level, _ := res.Get("LOG_LEVEL")
	assert.Equal(t, configdir.OriginFile, level.Origin)
	_, present := res.Get("OPTIONAL_X")
	assert.False(t, present, "an optional key with no value and no default is not injected at all")

	require.Len(t, res.Warnings, 1, "DB_HOTS is not in configSchema")
	assert.Contains(t, res.Warnings[0].Hints[0], "DB_HOST")
}

func TestResolveEmptyRequiredIsMissing(t *testing.T) {
	res, err := configdir.Resolve(configdir.Input{
		ComponentID: "erp/backend", Version: "2.0.0", Schema: backendSchema(),
		File: mustFile(t, "DB_HOST: \"\"\nDB_PASSWORD: $var:EMPTY\n"),
		Vars: mustVars(t, "EMPTY: \"\"\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"DB_HOST", "DB_PASSWORD"}, res.Missing)
}

func TestResolveUndefinedVar(t *testing.T) {
	_, err := configdir.Resolve(configdir.Input{
		ComponentID: "erp/backend", Version: "2.0.0", Schema: backendSchema(),
		File: mustFile(t, "DB_HOST: $var:NOPE\nDB_PASSWORD: x\n"),
	})
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeConfigInvalid, e.Code)
	assert.Contains(t, e.Error(), "$var:NOPE")
}

func TestResolveNoFileUsesDefaults(t *testing.T) {
	res, err := configdir.Resolve(configdir.Input{ComponentID: "erp/backend", Version: "2.0.0", Schema: backendSchema()})
	require.NoError(t, err)
	assert.Equal(t, []string{"DB_HOST", "DB_PASSWORD"}, res.Missing)
	assert.Len(t, res.Values, 2)
}

func TestResolveNoSchemaWarns(t *testing.T) {
	res, err := configdir.Resolve(configdir.Input{ComponentID: "a/b", Version: "1.0.0", File: mustFile(t, "X: 1\n")})
	require.NoError(t, err)
	assert.Empty(t, res.Values)
	require.Len(t, res.Warnings, 1)
}

func TestResolveSecretRefOnNonSecret(t *testing.T) {
	res, err := configdir.Resolve(configdir.Input{
		ComponentID: "erp/backend", Version: "2.0.0", Schema: backendSchema(),
		File: mustFile(t, "DB_HOST: {existingSecret: db, key: host}\nDB_PASSWORD: {existingSecret: db, key: password}\n"),
	})
	require.NoError(t, err)
	require.Len(t, res.Warnings, 1)
	_, hostPresent := res.Get("DB_HOST")
	assert.False(t, hostPresent)
	pwd, _ := res.Get("DB_PASSWORD")
	assert.Equal(t, configdir.KindSecretRef, pwd.Value.Kind)
}
```

- [ ] **Step 3: Run to see failure**

Run: `go test ./internal/configdir/ -run TestResolve`
Expected: FAIL — `configdir.Resolve` undefined.

- [ ] **Step 4: Implement**

`internal/configdir/resolve.go`:

```go
package configdir

import (
	"fmt"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcheck"
)

// Origin 说明一个配置值从哪来。
type Origin string

const (
	OriginFile    Origin = "file"
	OriginVar     Origin = "var"
	OriginDefault Origin = "default"
)

// Resolved 是一个配置项的最终值。Key 就是环境变量名（附录 A10）；
// Value 已经穿过了 $var:，但 ${VAR} / file:// / existingSecret 仍是引用——何时求值是渲染器的事。
type Resolved struct {
	Key     string
	Value   Value
	Origin  Origin
	VarName string
	Secret  bool
}

// Input 是解析一个组件配置所需的全部输入。
type Input struct {
	ComponentID string
	Version     string
	Schema      *manifest.ConfigSchema
	// File 是该组件（该版本）的配置文件，没有时为 nil。
	File *File
	// Vars 是 config/vars.yaml；DeployVars 是当前部署文件的 vars:，同名时后者优先。
	Vars       map[string]Value
	DeployVars map[string]Value
}

// Result 是解析结果。
type Result struct {
	// Values 按键名排序，只含真的有值的项。
	Values []Resolved
	// Missing 是 required 却没有值的键（阻断启动由调用方决定措辞）。
	Missing []string
	// Warnings 是不阻断的问题：写了 schema 里没有的键、组件没有 configSchema……
	Warnings []*clierr.Error
}

// Get 取一个键的解析结果。
func (r *Result) Get(key string) (Resolved, bool) {
	for _, v := range r.Values {
		if v.Key == key {
			return v, true
		}
	}
	return Resolved{}, false
}

// LookupVar 按 deploy 文件的 vars: → config/vars.yaml 的顺序查公共变量。
func LookupVar(name string, deployVars, vars map[string]Value) (Value, bool) {
	if v, ok := deployVars[name]; ok {
		return v, true
	}
	v, ok := vars[name]
	return v, ok
}

// Resolve 按优先级算出一个组件每个配置项的值（提案 §7.5，经附录 A 修正）：
// 组件配置文件里写了且有值 → schema 默认值 → 没有（必填即缺失）。
func Resolve(in Input) (*Result, error) {
	res := &Result{}
	ref := in.ComponentID + "@" + in.Version
	path := ""
	if in.File != nil {
		path = in.File.Path
	}

	if in.Schema == nil {
		if keys := writtenKeys(in.File); len(keys) > 0 {
			res.Warnings = append(res.Warnings, noSchemaWarning(ref, path, keys))
		}
		return res, nil
	}

	written := in.File.Map()
	required := map[string]bool{}
	for _, key := range in.Schema.Required {
		required[key] = true
	}

	var undefined []string
	for _, key := range sortedProperties(in.Schema.Properties) {
		prop := in.Schema.Properties[key]
		r := Resolved{Key: key, Secret: prop.Secret}
		v, isWritten := written[key]

		switch {
		case isWritten && v.Kind == KindVarRef:
			target, found := LookupVar(v.Name, in.DeployVars, in.Vars)
			if !found {
				undefined = append(undefined, key+" → "+v.String())
				continue
			}
			if target.IsUnset() {
				if required[key] {
					res.Missing = append(res.Missing, key)
				}
				continue
			}
			r.Value, r.Origin, r.VarName = target, OriginVar, v.Name
		case isWritten && !v.IsUnset():
			r.Value, r.Origin = v, OriginFile
		case prop.Default != nil:
			r.Value, r.Origin = defaultValue(prop.Default), OriginDefault
		default:
			if required[key] {
				res.Missing = append(res.Missing, key)
			}
			continue
		}

		if r.Value.Kind == KindSecretRef && !prop.Secret {
			res.Warnings = append(res.Warnings, secretRefWarning(ref, path, key))
			continue
		}
		res.Values = append(res.Values, r)
	}

	res.Warnings = append(res.Warnings, unknownKeyWarnings(ref, path, in.File, in.Schema)...)
	if len(undefined) > 0 {
		return nil, undefinedVarError(ref, path, undefined)
	}
	return res, nil
}

func defaultValue(d any) Value {
	v, err := ParseValue(d)
	if err != nil {
		return literal(fmt.Sprint(d))
	}
	return v
}

func sortedProperties(props map[string]manifest.ConfigProperty) []string {
	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// writtenKeys 返回文件里真的写了值的键，排序。
func writtenKeys(f *File) []string {
	var out []string
	for key, v := range f.Map() {
		if !v.IsUnset() {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func unknownKeyWarnings(ref, path string, f *File, schema *manifest.ConfigSchema) []*clierr.Error {
	known := sortedProperties(schema.Properties)
	var out []*clierr.Error
	for _, key := range f.Keys() {
		if _, declared := schema.Properties[key]; declared {
			continue
		}
		w := clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.ConfigdirUnknownKey, ref, key)).
			WithDetail(i18n.T(msgid.LabelFile), path)
		if len(known) > 0 {
			w = w.WithDetail(i18n.T(msgid.ConfigdirLabelDeclared), strings.Join(known, i18n.T(msgid.ListSeparator)))
		}
		if guess := yamlcheck.Closest(key, known); guess != "" {
			w = w.WithHint(i18n.T(msgid.ConfigdirUnknownKeyGuess, guess))
		}
		out = append(out, w)
	}
	return out
}

func noSchemaWarning(ref, path string, keys []string) *clierr.Error {
	return clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.ConfigdirNoSchema, ref)).
		WithDetail(i18n.T(msgid.LabelFile), path).
		WithDetail(i18n.T(msgid.ConfigdirLabelIgnoredKeys), strings.Join(keys, i18n.T(msgid.ListSeparator)))
}

func secretRefWarning(ref, path, key string) *clierr.Error {
	return clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.ConfigdirSecretRefNotSecret, ref, key)).
		WithDetail(i18n.T(msgid.LabelFile), path)
}

func undefinedVarError(ref, path string, refs []string) *clierr.Error {
	err := clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.ConfigdirUndefinedVar, ref)).
		WithDetail(i18n.T(msgid.LabelFile), path)
	for _, r := range refs {
		err = err.WithDetail(i18n.T(msgid.ConfigdirLabelUndefinedRef), r)
	}
	return err.WithHint(i18n.T(msgid.ConfigdirHintDefineVar))
}
```

`TestResolveVarPrecedence`'s hint assertion relies on `yamlcheck.Closest("DB_HOTS", known)` returning `DB_HOST`: no prefix match, edit distance 2, within the helper's limit of 2 for names longer than 4 characters (`DB_PORT` is at distance 3).

- [ ] **Step 5: Run tests and checks**

Run: `go vet ./internal/configdir/... && go test ./internal/configdir/... ./tests/i18nguard/... && .tools/bin/golangci-lint run ./internal/configdir/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/configdir internal/msgid/configdir.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "feat(configdir): resolve component config with \$var: precedence, defaults and loud missing/undefined keys

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

## Task 7: `configdir.Skeleton` — the file `brickkit add` will generate

**Files:**
- Create: `internal/configdir/skeleton.go`, `internal/configdir/skeleton_test.go`
- Modify: `internal/msgid/configdir.go`, `internal/i18n/catalog_en.go`, `internal/i18n/catalog_zh.go`

**Interfaces:**
- Consumes: `manifest.ConfigSchema`, Task 5's `ScalarYAML`, `VarRefPrefix`, `yamlcomment.Block`.
- Produces:
  - `configdir.HeaderPrefix = "# Component: "` — a machine-readable header line, never translated (P6's archive restore reads the old version from it)
  - `configdir.Skeleton(id, version string, schema *manifest.ConfigSchema, varRefs map[string]string) []byte` — `nil` when the schema is nil or has no properties; `varRefs` maps config key → shared-variable name the user agreed to bind (§7.2.5)
  - `configdir.Header(data []byte) (id, version string, ok bool)`

Layout rule (Appendix A4, replacing §7.6's "copy defaults in"): required keys **without** a default are written as `KEY: ""`; every other key is written **commented out** (`# KEY: <default>`), so an untouched key keeps following the component's default across upgrades and only keys the user actually wrote can ever conflict. A key in `varRefs` is written uncommented as `KEY: $var:NAME` regardless of section.

- [ ] **Step 1: Add the messages**

Append to `internal/msgid/configdir.go`:

```go
	ConfigdirSkeletonIntro    = "configdir.skeleton_intro"
	ConfigdirSkeletonRequired = "configdir.skeleton_required"
	ConfigdirSkeletonOptional = "configdir.skeleton_optional"
	ConfigdirSkeletonDefault  = "configdir.skeleton_default"
```

`catalog_en.go`:

```go
	msgid.ConfigdirSkeletonIntro:    "Environment variables for this component; every key is injected as-is.\nShared variable: $var:NAME (config/vars.yaml) · environment variable: ${NAME} · local file: file://path",
	msgid.ConfigdirSkeletonRequired: "=== Required: startup is blocked until these have a value ===",
	msgid.ConfigdirSkeletonOptional: "=== Optional: commented keys use the component's default; uncomment to override ===",
	msgid.ConfigdirSkeletonDefault:  "default",
```

`catalog_zh.go`:

```go
	msgid.ConfigdirSkeletonIntro:    "这个组件的环境变量，每个键原样注入。\n公共变量：$var:NAME（config/vars.yaml）· 环境变量：${NAME} · 本地文件：file://path",
	msgid.ConfigdirSkeletonRequired: "=== 必填：没有值就无法启动 ===",
	msgid.ConfigdirSkeletonOptional: "=== 可选：注释掉的键使用组件默认值，取消注释即可覆盖 ===",
	msgid.ConfigdirSkeletonDefault:  "默认值",
```

- [ ] **Step 2: Write the failing test**

`internal/configdir/skeleton_test.go`:

```go
package configdir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
)

func TestSkeleton(t *testing.T) {
	schema := &manifest.ConfigSchema{
		Properties: map[string]manifest.ConfigProperty{
			"DB_HOST":     {Type: "string", Description: "database\nhost"},
			"DB_PASSWORD": {Type: "string", Secret: true},
			"DB_PORT":     {Type: "integer", Default: 5432},
			"TAGS":        {Type: "array", Default: []any{"a"}},
			"OPTIONAL_X":  {Type: "string"},
		},
		Required: []string{"DB_HOST", "DB_PASSWORD", "DB_PORT"},
	}
	out := string(configdir.Skeleton("erp/backend", "1.0.0", schema, map[string]string{"DB_PASSWORD": "SHARED_PWD"}))
	def := i18n.T(msgid.ConfigdirSkeletonDefault)

	assert.Contains(t, out, "# Component: erp/backend@1.0.0\n")
	assert.Contains(t, out, "DB_HOST: \"\"  # string | database host\n")
	assert.Contains(t, out, "DB_PASSWORD: $var:SHARED_PWD  # string | secret\n")
	assert.Contains(t, out, "# DB_PORT: 5432  # integer ("+def+")\n", "required-with-default goes to the optional section, commented")
	assert.Contains(t, out, "# TAGS: [\"a\"]  # array ("+def+")\n")
	assert.Contains(t, out, "# OPTIONAL_X:  # string\n")

	// 生成的骨架必须能直接被解析与解析出正确结果：只有真正必填的缺失。
	f, err := configdir.ParseComponentFile([]byte(out), "config/erp-backend.yaml")
	require.NoError(t, err)
	assert.Equal(t, []string{"DB_HOST", "DB_PASSWORD"}, f.Keys())
	res, err := configdir.Resolve(configdir.Input{
		ComponentID: "erp/backend", Version: "1.0.0", Schema: schema, File: f,
		Vars: map[string]configdir.Value{"SHARED_PWD": {Kind: configdir.KindLiteral, Text: "pwd"}},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"DB_HOST"}, res.Missing)
	tags, _ := res.Get("TAGS")
	assert.Equal(t, `["a"]`, tags.Value.Text)

	id, version, ok := configdir.Header([]byte(out))
	require.True(t, ok)
	assert.Equal(t, "erp/backend", id)
	assert.Equal(t, "1.0.0", version)

	assert.Nil(t, configdir.Skeleton("a/b", "1.0.0", nil, nil))
	assert.Nil(t, configdir.Skeleton("a/b", "1.0.0", &manifest.ConfigSchema{}, nil))
}
```

- [ ] **Step 3: Run to see failure**

Run: `go test ./internal/configdir/ -run TestSkeleton`
Expected: FAIL — `configdir.Skeleton` undefined.

- [ ] **Step 4: Implement**

`internal/configdir/skeleton.go`:

```go
package configdir

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcomment"
)

// HeaderPrefix 是骨架第一行的固定写法（不翻译）：归档恢复靠它认出旧文件属于哪个版本。
const HeaderPrefix = "# Component: "

// Skeleton 按 configSchema 生成组件配置文件骨架（提案 §7.6，经附录 A4 修正）。
//
// 只有"必填且没有默认值"的键写成 KEY: ""；其余一律注释掉——没被使用者碰过的键
// 就一直跟随组件的默认值，升级时默认值变了也不会制造假冲突。
func Skeleton(id, version string, schema *manifest.ConfigSchema, varRefs map[string]string) []byte {
	if schema == nil || len(schema.Properties) == 0 {
		return nil
	}
	required := map[string]bool{}
	for _, key := range schema.Required {
		required[key] = true
	}
	var requiredKeys, optionalKeys []string
	for _, key := range sortedProperties(schema.Properties) {
		if required[key] && schema.Properties[key].Default == nil {
			requiredKeys = append(requiredKeys, key)
		} else {
			optionalKeys = append(optionalKeys, key)
		}
	}

	var b strings.Builder
	b.WriteString(HeaderPrefix + id + "@" + version + "\n")
	b.WriteString(yamlcomment.Block("", i18n.T(msgid.ConfigdirSkeletonIntro)))
	section := func(title string, keys []string, isRequired bool) {
		if len(keys) == 0 {
			return
		}
		b.WriteString("\n" + yamlcomment.Block("", title))
		for _, key := range keys {
			b.WriteString(skeletonLine(key, schema.Properties[key], varRefs[key], isRequired))
		}
	}
	section(i18n.T(msgid.ConfigdirSkeletonRequired), requiredKeys, true)
	section(i18n.T(msgid.ConfigdirSkeletonOptional), optionalKeys, false)
	return []byte(b.String())
}

func skeletonLine(key string, prop manifest.ConfigProperty, varRef string, required bool) string {
	desc := describe(prop)
	switch {
	case varRef != "":
		return fmt.Sprintf("%s: %s%s  # %s\n", key, VarRefPrefix, varRef, desc)
	case required:
		return fmt.Sprintf("%s: \"\"  # %s\n", key, desc)
	case prop.Default != nil:
		return fmt.Sprintf("# %s: %s  # %s (%s)\n", key, defaultText(prop.Default), desc, i18n.T(msgid.ConfigdirSkeletonDefault))
	default:
		return fmt.Sprintf("# %s:  # %s\n", key, desc)
	}
}

// describe 是行尾说明：类型 | secret | 描述（多行描述压成一行）。
func describe(prop manifest.ConfigProperty) string {
	desc := prop.Type
	if prop.Secret {
		desc += " | secret"
	}
	if text := strings.Join(strings.Fields(prop.Description), " "); text != "" {
		desc += " | " + text
	}
	return desc
}

// defaultText 把默认值写成一行：标量走 YAML，列表 / 映射写 JSON（取消注释后仍是合法 YAML）。
func defaultText(v any) string {
	switch v.(type) {
	case string, bool, int, int64, uint64, float64:
		return ScalarYAML(v)
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}

// Header 从配置文件里读出 "# Component: <id>@<version>" 这一行。
func Header(data []byte) (id, version string, ok bool) {
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, HeaderPrefix) {
			continue
		}
		ref := strings.TrimSpace(strings.TrimPrefix(line, HeaderPrefix))
		i := strings.LastIndex(ref, "@")
		if i <= 0 {
			return "", "", false
		}
		return ref[:i], ref[i+1:], true
	}
	return "", "", false
}
```

- [ ] **Step 5: Run tests and checks**

Run: `go vet ./internal/configdir/... && go test ./internal/configdir/... ./tests/i18nguard/... && .tools/bin/golangci-lint run ./internal/configdir/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/configdir internal/msgid/configdir.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "feat(configdir): config skeleton with commented defaults and a machine-readable header

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

## Task 8: `project.Load` — deploy-file selection and every cross-file rule

**Files:**
- Create: `internal/project/load.go`, `internal/project/check.go`, `internal/project/configload.go`, `internal/project/load_test.go`
- Create: `internal/msgid/project.go`
- Modify: `internal/i18n/catalog_en.go`, `internal/i18n/catalog_zh.go`

**Interfaces:**
- Consumes: everything above — `projfile.ParseFile`, `deployfile.ParseFile`, `configdir.ParseVarsFile/ParseVarsMap/ParseComponentFile/ParseFileName/FileName/FileBase/ConflictError/Input`, `yamlfile.Read/Indexed`, `LocalModeOn`.
- Produces (every later phase consumes this):
  - `type project.DeploySource int` = `DeployTeam`, `DeployLocal`, `DeployExplicit`
  - `type project.LoadOptions struct { DeployFile string; NoLocal bool }` — `DeployFile` is the `-f` value (relative to the project root or absolute)
  - `type project.Project struct { Layout Layout; Decl *projfile.File; Deploy *deployfile.File; DeploySource DeploySource; DeployPath string; Vars, DeployVars map[string]configdir.Value; Warnings []*clierr.Error }`
  - `project.Load(root string, opts LoadOptions) (*Project, error)`
  - `(*Project).Config(id, version string) *configdir.File` (nil = no config file), `DeployEntry(id, version string) deployfile.Component`, `ShellOf(memberID string) (shellID string, ok bool)`, `ConfigInput(id, version string, schema *manifest.ConfigSchema) configdir.Input`

Selection strategy (spec §6.2, §11.6): `-f` given → that file only, local mode ignored (role team, so `mode: debug` is rejected); else local mode on and not `--no-local` → `deploy.local.yaml` (role local); else `deploy.yaml` (role team).

- [ ] **Step 1: Add the messages**

`internal/msgid/project.go`:

```go
package msgid

// internal/project（三层文件装载与跨文件一致性）的文案。
const (
	ProjectDeployMissing       = "project.deploy_missing"
	ProjectDeployMissingHint   = "project.deploy_missing_hint"
	ProjectLocalFileMissing    = "project.local_file_missing"
	ProjectDeployFileNotFound  = "project.deploy_file_not_found"
	ProjectLocalStale          = "project.local_stale"
	ProjectLocalStaleReason    = "project.local_stale_reason"
	ProjectHintLocalRefresh    = "project.hint_local_refresh"
	ProjectHintLocalEdit       = "project.hint_local_edit"
	ProjectHintLocalOff        = "project.hint_local_off"
	ProjectDeployInconsistent  = "project.deploy_inconsistent"
	ProjectHintDeploySync      = "project.hint_deploy_sync"
	ProjectHintDeployEdit      = "project.hint_deploy_edit"
	ProjectLabelMissingEntry   = "project.label_missing_entry"
	ProjectLabelExtraEntry     = "project.label_extra_entry"
	ProjectMembersOnNonShell   = "project.members_on_non_shell"
	ProjectMemberUndeclared    = "project.member_undeclared"
	ProjectMemberIsShell       = "project.member_is_shell"
	ProjectMemberMultiVersion  = "project.member_multi_version"
	ProjectMemberTwoShells     = "project.member_two_shells"
	ProjectConfigNameCollision = "project.config_name_collision"
	ProjectHintConfigCollision = "project.hint_config_collision"
	ProjectConfigAmbiguous     = "project.config_ambiguous"
	ProjectHintConfigAmbiguous = "project.hint_config_ambiguous"
	ProjectConfigOrphan        = "project.config_orphan"
	ProjectVarUndefined        = "project.var_undefined"
)
```

`catalog_en.go`:

```go
	msgid.ProjectDeployMissing:       "Error: deploy.yaml not found",
	msgid.ProjectDeployMissingHint:   "Run brickkit init in the project directory: it adds whatever is missing and leaves existing files alone",
	msgid.ProjectLocalFileMissing:    "Error: local mode is on, but deploy.local.yaml does not exist",
	msgid.ProjectDeployFileNotFound:  "Error: the deploy file given with --file does not exist",
	msgid.ProjectLocalStale:          "Error: deploy.local.yaml is out of date: it does not match the components in brickkit.yaml",
	msgid.ProjectLocalStaleReason:    "Local mode is on and brickkit.yaml changed, but your deploy.local.yaml was not updated",
	msgid.ProjectHintLocalRefresh:    "Option A (recommended): brickkit local refresh regenerates deploy.local.yaml from deploy.yaml and keeps the old one as deploy.local.yaml.bak",
	msgid.ProjectHintLocalEdit:       "Option B: add or remove the listed entries in deploy.local.yaml by hand",
	msgid.ProjectHintLocalOff:        "Option C: brickkit local off switches back to the team's deploy.yaml",
	msgid.ProjectDeployInconsistent:  "Error: %[1]s does not match the components in brickkit.yaml",
	msgid.ProjectHintDeploySync:      "Every component in brickkit.yaml needs exactly one entry in the deploy file (a bare ID covers all its versions, id@version covers one); brickkit add and remove keep deploy.yaml in sync",
	msgid.ProjectHintDeployEdit:      "Add or remove the listed entries by hand",
	msgid.ProjectLabelMissingEntry:   "No entry for",
	msgid.ProjectLabelExtraEntry:     "Entry matches no component",
	msgid.ProjectMembersOnNonShell:   "%[1]s lists members but is not a shell (kind: shell in brickkit.yaml)",
	msgid.ProjectMemberUndeclared:    "member %[1]s is not declared in brickkit.yaml",
	msgid.ProjectMemberIsShell:       "member %[1]s is itself a shell; shells cannot be nested",
	msgid.ProjectMemberMultiVersion:  "member %[1]s has several versions in brickkit.yaml; a shell member must have exactly one",
	msgid.ProjectMemberTwoShells:     "%[1]s is already a member of shell %[2]s; a component belongs to at most one shell",
	msgid.ProjectConfigNameCollision: "Error: %[1]s and %[2]s both map to config/%[3]s",
	msgid.ProjectHintConfigCollision: "Config file names replace / with -, so these two component IDs cannot be told apart; rename one of them",
	msgid.ProjectConfigAmbiguous:     "Error: config/%[1]s is ambiguous: %[2]s has several versions in brickkit.yaml (%[3]s)",
	msgid.ProjectHintConfigAmbiguous: "Create one file per version (%[1]s), or keep only one version in brickkit.yaml",
	msgid.ProjectConfigOrphan:        "%[1]s belongs to no component in brickkit.yaml and is ignored",
	msgid.ProjectVarUndefined:        "Error: config files reference shared variables that are defined nowhere",
```

`catalog_zh.go`:

```go
	msgid.ProjectDeployMissing:       "错误：找不到 deploy.yaml",
	msgid.ProjectDeployMissingHint:   "在项目目录执行 brickkit init：它只补齐缺失的文件，已有文件不动",
	msgid.ProjectLocalFileMissing:    "错误：本地模式已开启，但 deploy.local.yaml 不存在",
	msgid.ProjectDeployFileNotFound:  "错误：--file 指定的部署文件不存在",
	msgid.ProjectLocalStale:          "错误：deploy.local.yaml 已过期，与 brickkit.yaml 的组件对不上",
	msgid.ProjectLocalStaleReason:    "你开启了本地模式，而 brickkit.yaml 变了，你的 deploy.local.yaml 没有同步",
	msgid.ProjectHintLocalRefresh:    "方案 A（推荐）：brickkit local refresh 按 deploy.yaml 重新生成 deploy.local.yaml，旧文件备份为 deploy.local.yaml.bak",
	msgid.ProjectHintLocalEdit:       "方案 B：在 deploy.local.yaml 里手动增删下面列出的条目",
	msgid.ProjectHintLocalOff:        "方案 C：brickkit local off 切回团队的 deploy.yaml",
	msgid.ProjectDeployInconsistent:  "错误：%[1]s 与 brickkit.yaml 的组件对不上",
	msgid.ProjectHintDeploySync:      "brickkit.yaml 里的每个组件在部署文件里都要有且只有一个条目（裸 ID 覆盖它的所有版本，id@version 只覆盖一个）；brickkit add / remove 会自动保持 deploy.yaml 同步",
	msgid.ProjectHintDeployEdit:      "手动增删下面列出的条目",
	msgid.ProjectLabelMissingEntry:   "缺少条目",
	msgid.ProjectLabelExtraEntry:     "条目对应不到任何组件",
	msgid.ProjectMembersOnNonShell:   "%[1]s 列了 members，但它不是外壳（brickkit.yaml 里没有 kind: shell）",
	msgid.ProjectMemberUndeclared:    "成员 %[1]s 没有在 brickkit.yaml 里声明",
	msgid.ProjectMemberIsShell:       "成员 %[1]s 本身就是外壳；外壳不能嵌套",
	msgid.ProjectMemberMultiVersion:  "成员 %[1]s 在 brickkit.yaml 里有多个版本；外壳成员只能有一个版本",
	msgid.ProjectMemberTwoShells:     "%[1]s 已经是外壳 %[2]s 的成员；一个组件最多属于一个外壳",
	msgid.ProjectConfigNameCollision: "错误：%[1]s 与 %[2]s 都对应到 config/%[3]s",
	msgid.ProjectHintConfigCollision: "配置文件名把 / 换成了 -，这两个组件 ID 因此无法区分；请改掉其中一个",
	msgid.ProjectConfigAmbiguous:     "错误：config/%[1]s 有歧义：%[2]s 在 brickkit.yaml 里有多个版本（%[3]s）",
	msgid.ProjectHintConfigAmbiguous: "为每个版本各建一份（%[1]s），或在 brickkit.yaml 里只保留一个版本",
	msgid.ProjectConfigOrphan:        "%[1]s 不属于 brickkit.yaml 里的任何组件，已忽略",
	msgid.ProjectVarUndefined:        "错误：配置文件引用了哪里都没有定义的公共变量",
```

- [ ] **Step 2: Write the failing tests**

`internal/project/load_test.go`:

```go
package project_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
)

const baseDecl = `project: shop
components:
  - {id: erp/shell, version: 1.0.0, kind: shell}
  - {id: erp/backend, version: 2.0.0}
  - {id: people/basic, version: 1.0.0}
  - {id: people/basic, version: 2.0.0}
`

const baseDeploy = `target: docker
vars:
  DB_PASSWORD: deploy-pwd
components:
  - id: erp/shell
    members: [erp/backend]
  - id: erp/backend
  - id: people/basic
`

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
}

func baseProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, map[string]string{
		"brickkit.yaml":                  baseDecl,
		"deploy.yaml":                    baseDeploy,
		"config/vars.yaml":               "ERP_DB_HOST: pg.internal\nDB_PASSWORD: vars-pwd\n",
		"config/erp-backend.yaml":        "DB_HOST: $var:ERP_DB_HOST\nDB_PASSWORD: $var:DB_PASSWORD\n",
		"config/people-basic@1.0.0.yaml": "LOG_LEVEL: debug\n",
		"config/people-basic@2.0.0.yaml": "LOG_LEVEL: warn\n",
	})
	return root
}

func detailValues(err error) []string {
	var out []string
	for _, d := range clierr.As(err).Details {
		out = append(out, d.Value)
	}
	return out
}

func detailKeys(err error) []string {
	var out []string
	for _, d := range clierr.As(err).Details {
		out = append(out, d.Key)
	}
	return out
}

func TestLoadHappyPath(t *testing.T) {
	p, err := project.Load(baseProject(t), project.LoadOptions{})
	require.NoError(t, err)
	assert.Equal(t, project.DeployTeam, p.DeploySource)
	assert.Empty(t, p.Warnings)

	shell, ok := p.ShellOf("erp/backend")
	require.True(t, ok)
	assert.Equal(t, "erp/shell", shell)
	_, ok = p.ShellOf("people/basic")
	assert.False(t, ok)

	level, _ := p.Config("people/basic", "2.0.0").Lookup("LOG_LEVEL")
	assert.Equal(t, "warn", level.Text)
	assert.Nil(t, p.Config("erp/shell", "1.0.0"))
	assert.Equal(t, "people/basic", p.DeployEntry("people/basic", "1.0.0").ID)

	schema := &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"DB_HOST": {Type: "string"}, "DB_PASSWORD": {Type: "string", Secret: true},
	}}
	res, err := configdir.Resolve(p.ConfigInput("erp/backend", "2.0.0", schema))
	require.NoError(t, err)
	host, _ := res.Get("DB_HOST")
	pwd, _ := res.Get("DB_PASSWORD")
	assert.Equal(t, "pg.internal", host.Value.Text)
	assert.Equal(t, "deploy-pwd", pwd.Value.Text, "the deploy file's vars: win over config/vars.yaml")
}

func TestLoadDeploySelection(t *testing.T) {
	root := baseProject(t)
	write(t, root, map[string]string{
		// 改掉原有条目而不是再加一条 erp/backend@2.0.0：那样裸 ID 条目就什么也不覆盖，会被判为多余
		"deploy.local.yaml": strings.Replace(
			strings.Replace(baseDeploy, "target: docker", "target: podman", 1),
			"  - id: erp/backend\n", "  - id: erp/backend\n    mode: debug\n    localPort: 9000\n", 1),
		"deploy.prod.yaml": strings.Replace(baseDeploy, "target: docker", "target: k8s", 1),
	})
	l := project.NewLayout(root)
	require.NoError(t, project.SetLocalMode(l, true))

	p, err := project.Load(root, project.LoadOptions{})
	require.NoError(t, err)
	assert.Equal(t, project.DeployLocal, p.DeploySource)
	assert.Equal(t, "podman", p.Deploy.Target)
	assert.Equal(t, "debug", p.DeployEntry("erp/backend", "2.0.0").Mode)
	// erp/backend 是 erp/shell 的成员：成员必须可以设 debug / local（附录 A18），装载不得拒绝

	p, err = project.Load(root, project.LoadOptions{NoLocal: true})
	require.NoError(t, err)
	assert.Equal(t, project.DeployTeam, p.DeploySource)

	p, err = project.Load(root, project.LoadOptions{DeployFile: "deploy.prod.yaml"})
	require.NoError(t, err)
	assert.Equal(t, project.DeployExplicit, p.DeploySource)
	assert.Equal(t, "k8s", p.Deploy.Target)

	_, err = project.Load(root, project.LoadOptions{DeployFile: "missing.yaml"})
	require.Error(t, err)
	assert.Equal(t, clierr.CodeInvalidArgument, clierr.As(err).Code)
}

func TestLoadMissingDeployFiles(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"brickkit.yaml": baseDecl})
	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	assert.Equal(t, i18n.T(msgid.ProjectDeployMissing), clierr.As(err).Message)

	require.NoError(t, project.SetLocalMode(project.NewLayout(root), true))
	_, err = project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	assert.Equal(t, i18n.T(msgid.ProjectLocalFileMissing), clierr.As(err).Message)
}

func TestLoadLocalFileStale(t *testing.T) {
	root := baseProject(t)
	// 本地文件是按旧的 deploy.yaml 生成的；之后团队加了 crm/backend
	write(t, root, map[string]string{
		"deploy.local.yaml": baseDeploy,
		"brickkit.yaml":     baseDecl + "  - {id: crm/backend, version: 1.0.0}\n",
		"deploy.yaml":       baseDeploy + "  - id: crm/backend\n",
	})
	require.NoError(t, project.SetLocalMode(project.NewLayout(root), true))

	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeDeployInconsistent, e.Code)
	assert.Equal(t, i18n.T(msgid.ProjectLocalStale), e.Message)
	assert.Contains(t, detailValues(err), "crm/backend")
	require.NotEmpty(t, e.Hints)
	assert.Equal(t, i18n.T(msgid.ProjectHintLocalRefresh), e.Hints[0])
}

func TestLoadDeployExtraAndMissing(t *testing.T) {
	root := baseProject(t)
	write(t, root, map[string]string{"deploy.yaml": `target: docker
components:
  - {id: erp/shell, members: [erp/backend]}
  - id: erp/backend
  - id: old/thing
`})
	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeDeployInconsistent, e.Code)
	assert.Equal(t, i18n.T(msgid.ProjectDeployInconsistent, "deploy.yaml"), e.Message)
	values := detailValues(err)
	assert.Contains(t, values, "old/thing")
	assert.Contains(t, values, "people/basic@1.0.0", "several versions: missing entries name the exact version")
	assert.Contains(t, values, "people/basic@2.0.0")
}

func TestLoadBareEntryCoveringNothingIsExtra(t *testing.T) {
	root := baseProject(t)
	write(t, root, map[string]string{"deploy.yaml": baseDeploy +
		"  - id: people/basic@1.0.0\n  - id: people/basic@2.0.0\n"})
	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	assert.Contains(t, detailValues(err), "people/basic")
}

func TestLoadDebugRejectedOutsideLocalFile(t *testing.T) {
	root := baseProject(t)
	debug := baseDeploy + "  - {id: erp/backend@2.0.0, mode: debug, localPort: 9000}\n"
	write(t, root, map[string]string{"deploy.prod.yaml": debug})
	_, err := project.Load(root, project.LoadOptions{DeployFile: "deploy.prod.yaml"})
	require.Error(t, err)

	write(t, root, map[string]string{"deploy.yaml": debug})
	_, err = project.Load(root, project.LoadOptions{})
	require.Error(t, err)
}

func TestLoadMemberRules(t *testing.T) {
	cases := map[string]struct{ decl, deploy string }{
		"members on non-shell": {baseDecl, `target: docker
components:
  - id: erp/shell
  - {id: erp/backend, members: [people/basic]}
  - id: people/basic
`},
		"undeclared member": {baseDecl, `target: docker
components:
  - {id: erp/shell, members: [ghost/thing]}
  - id: erp/backend
  - id: people/basic
`},
		"multi-version member": {baseDecl, `target: docker
components:
  - {id: erp/shell, members: [people/basic]}
  - id: erp/backend
  - id: people/basic
`},
		"member in two shells": {baseDecl + "  - {id: erp/shell2, version: 1.0.0, kind: shell}\n", `target: docker
components:
  - {id: erp/shell, members: [erp/backend]}
  - {id: erp/shell2, members: [erp/backend]}
  - id: erp/backend
  - id: people/basic
`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := baseProject(t)
			write(t, root, map[string]string{"brickkit.yaml": tc.decl, "deploy.yaml": tc.deploy})
			_, err := project.Load(root, project.LoadOptions{})
			require.Error(t, err)
			found := false
			for _, key := range detailKeys(err) {
				found = found || strings.Contains(key, "members")
			}
			assert.True(t, found, "error must point at a members field: %v", detailKeys(err))
		})
	}
}

func TestLoadConfigAmbiguousAcrossVersions(t *testing.T) {
	root := baseProject(t)
	require.NoError(t, os.Remove(filepath.Join(root, "config", "people-basic@2.0.0.yaml")))
	write(t, root, map[string]string{"config/people-basic.yaml": "LOG_LEVEL: info\n"})

	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	assert.Equal(t, i18n.T(msgid.ProjectConfigAmbiguous, "people-basic.yaml", "people/basic", "1.0.0, 2.0.0"),
		clierr.As(err).Message)
}

func TestLoadConfigOrphansWarn(t *testing.T) {
	root := baseProject(t)
	write(t, root, map[string]string{
		"config/gone-thing.yaml":          "A: 1\n",
		"config/erp-backend@9.9.9.yaml":   "A: 1\n",
		"config/people-basic.yaml":        "A: 1\n", // 两个版本都有专属文件，无版本文件没人用
		"config/.archive/erp-backend.yaml": "A: 1\n",
		"config/README.md":                "notes\n",
	})
	p, err := project.Load(root, project.LoadOptions{})
	require.NoError(t, err)
	assert.Len(t, p.Warnings, 3)
}

func TestLoadUndefinedVarRef(t *testing.T) {
	root := baseProject(t)
	write(t, root, map[string]string{"config/erp-backend.yaml": "DB_HOST: $var:NOPE\n"})
	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	assert.Equal(t, i18n.T(msgid.ProjectVarUndefined), clierr.As(err).Message)
	joined := strings.Join(detailValues(err), "\n")
	assert.Contains(t, joined, "$var:NOPE")
	assert.Contains(t, joined, "erp-backend.yaml")
}

func TestLoadConflictsAcrossFilesReportedTogether(t *testing.T) {
	root := baseProject(t)
	write(t, root, map[string]string{
		"config/people-basic@1.0.0.yaml": "A: 1\nA: 2\n",
		"config/people-basic@2.0.0.yaml": "B: 1\nB: 2\n",
	})
	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigConflict, clierr.As(err).Code)
	joined := strings.Join(detailValues(err), "\n")
	assert.Contains(t, joined, "people-basic@1.0.0.yaml")
	assert.Contains(t, joined, "people-basic@2.0.0.yaml")
}

func TestLoadConfigNameCollision(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"brickkit.yaml": "project: p\ncomponents:\n  - {id: a-b/c, version: 1.0.0}\n  - {id: a/b-c, version: 1.0.0}\n",
		"deploy.yaml":   "target: docker\ncomponents:\n  - id: a-b/c\n  - id: a/b-c\n",
	})
	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	assert.Equal(t, i18n.T(msgid.ProjectConfigNameCollision, "a-b/c", "a/b-c", "a-b-c.yaml"), clierr.As(err).Message)
}
```

- [ ] **Step 3: Run to see failure**

Run: `go test ./internal/project/...`
Expected: FAIL — `project.Load` undefined.

- [ ] **Step 4: Implement loading and selection**

`internal/project/load.go`:

```go
package project

import (
	"errors"
	"io/fs"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/projfile"
)

// DeploySource 说明本次用的是哪一份部署文件。
type DeploySource int

const (
	// DeployTeam：deploy.yaml。
	DeployTeam DeploySource = iota
	// DeployLocal：本地模式开启，用 deploy.local.yaml。
	DeployLocal
	// DeployExplicit：-f / --file 显式指定，本地模式被忽略。
	DeployExplicit
)

// LoadOptions 是装载时的命令行选择。
type LoadOptions struct {
	// DeployFile 是 -f 的值（相对项目根或绝对路径）；空表示按默认规则选择。
	DeployFile string
	// NoLocal 让本次忽略本地模式（--no-local）。
	NoLocal bool
}

// Project 是装载完成、跨文件一致的三层项目。
type Project struct {
	Layout       Layout
	Decl         *projfile.File
	Deploy       *deployfile.File
	DeploySource DeploySource
	DeployPath   string
	// Vars 是 config/vars.yaml；DeployVars 是部署文件的 vars:（同名时优先）。
	Vars       map[string]configdir.Value
	DeployVars map[string]configdir.Value
	// Warnings 是装载过程中不阻断的问题，由命令决定何时打印。
	Warnings []*clierr.Error

	configs map[string]*configdir.File
	shellOf map[string]string
}

// Load 装载 root 下的项目，并执行全部跨文件校验。任何一条不满足都大声失败。
func Load(root string, opts LoadOptions) (*Project, error) {
	l := NewLayout(root)
	decl, err := projfile.ParseFile(l.DeclPath())
	if err != nil {
		return nil, err
	}

	path, source, err := selectDeploy(l, opts)
	if err != nil {
		return nil, err
	}
	role := deployfile.RoleTeam
	if source == DeployLocal {
		role = deployfile.RoleLocal
	}
	deploy, warnings, err := deployfile.ParseFile(path, role)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, deployMissingError(path, source)
	}
	if err != nil {
		return nil, err
	}

	p := &Project{
		Layout: l, Decl: decl, Deploy: deploy,
		DeploySource: source, DeployPath: path, Warnings: warnings,
	}
	for _, step := range []func() error{p.checkCoverage, p.checkMembers, p.loadConfig} {
		if err := step(); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// selectDeploy 决定读哪一份部署文件：-f > 本地模式 > deploy.yaml（提案 §6.2、§11.6）。
func selectDeploy(l Layout, opts LoadOptions) (string, DeploySource, error) {
	if opts.DeployFile != "" {
		return l.Resolve(opts.DeployFile), DeployExplicit, nil
	}
	if !opts.NoLocal {
		on, err := LocalModeOn(l)
		if err != nil {
			return "", 0, err
		}
		if on {
			return l.DeployLocalPath(), DeployLocal, nil
		}
	}
	return l.DeployPath(), DeployTeam, nil
}

func deployMissingError(path string, source DeploySource) *clierr.Error {
	switch source {
	case DeployLocal:
		return clierr.New(clierr.CodeProjectMissing, i18n.T(msgid.ProjectLocalFileMissing)).
			WithDetail(i18n.T(msgid.LabelPath), path).
			WithHint(i18n.T(msgid.ProjectHintLocalRefresh), i18n.T(msgid.ProjectHintLocalOff))
	case DeployExplicit:
		return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.ProjectDeployFileNotFound)).
			WithDetail(i18n.T(msgid.LabelPath), path).
			WithExit(clierr.ExitUsage)
	default:
		return clierr.New(clierr.CodeProjectMissing, i18n.T(msgid.ProjectDeployMissing)).
			WithDetail(i18n.T(msgid.LabelPath), path).
			WithHint(i18n.T(msgid.ProjectDeployMissingHint))
	}
}

func refKey(id, version string) string { return id + "@" + version }

// Config 返回组件某个版本的配置文件；没有时为 nil。
func (p *Project) Config(id, version string) *configdir.File { return p.configs[refKey(id, version)] }

// DeployEntry 返回覆盖该组件版本的部署条目（一致性校验保证一定存在）。
func (p *Project) DeployEntry(id, version string) deployfile.Component {
	c, _ := p.Deploy.Entry(id, version)
	return c
}

// ShellOf 返回某个成员所属的外壳。
func (p *Project) ShellOf(memberID string) (string, bool) {
	shell, ok := p.shellOf[memberID]
	return shell, ok
}

// ConfigInput 组装 configdir.Resolve 的输入；schema 来自组件 Manifest（装载器不读 Manifest）。
func (p *Project) ConfigInput(id, version string, schema *manifest.ConfigSchema) configdir.Input {
	return configdir.Input{
		ComponentID: id, Version: version, Schema: schema,
		File: p.Config(id, version), Vars: p.Vars, DeployVars: p.DeployVars,
	}
}
```

- [ ] **Step 5: Implement the consistency checks**

`internal/project/check.go`:

```go
package project

import (
	"path/filepath"
	"sort"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// checkCoverage 执行严格一致性校验（提案 §6.3）：brickkit.yaml 的每个组件版本恰好被一个
// 部署条目覆盖，没有覆盖不到任何组件的条目。
func (p *Project) checkCoverage() error {
	declared := map[string]bool{}
	for _, c := range p.Decl.Components {
		declared[c.Ref()] = true
	}

	explicit := map[string]bool{}
	bare := map[string]bool{}
	var extra []string
	for _, entry := range p.Deploy.Components {
		id, version := entry.Key()
		switch {
		case version != "":
			if declared[refKey(id, version)] {
				explicit[refKey(id, version)] = true
			} else {
				extra = append(extra, entry.ID)
			}
		case len(p.Decl.Versions(id)) == 0:
			extra = append(extra, entry.ID)
		default:
			bare[id] = true
		}
	}

	// 一个裸 ID 条目，如果它的每个版本都已有专属条目，它就什么也没覆盖
	for id := range bare {
		coversSomething := false
		for _, version := range p.Decl.Versions(id) {
			if !explicit[refKey(id, version)] {
				coversSomething = true
			}
		}
		if !coversSomething {
			extra = append(extra, id)
		}
	}

	var missing []string
	for _, c := range p.Decl.Components {
		if !explicit[c.Ref()] && !bare[c.ID] {
			missing = append(missing, p.displayRef(c.ID, c.Version))
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return nil
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return p.inconsistencyError(missing, extra)
}

// displayRef 在只有一个版本时只写 ID（与使用者的写法一致），多版本时写全。
func (p *Project) displayRef(id, version string) string {
	if len(p.Decl.Versions(id)) == 1 {
		return id
	}
	return refKey(id, version)
}

func (p *Project) inconsistencyError(missing, extra []string) *clierr.Error {
	local := p.DeploySource == DeployLocal
	message := i18n.T(msgid.ProjectDeployInconsistent, filepath.Base(p.DeployPath))
	if local {
		message = i18n.T(msgid.ProjectLocalStale)
	}
	err := clierr.New(clierr.CodeDeployInconsistent, message).
		WithDetail(i18n.T(msgid.LabelFile), p.DeployPath)
	for _, ref := range missing {
		err = err.WithDetail(i18n.T(msgid.ProjectLabelMissingEntry), ref)
	}
	for _, ref := range extra {
		err = err.WithDetail(i18n.T(msgid.ProjectLabelExtraEntry), ref)
	}
	if local {
		return err.WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ProjectLocalStaleReason)).
			WithHint(i18n.T(msgid.ProjectHintLocalRefresh), i18n.T(msgid.ProjectHintLocalEdit), i18n.T(msgid.ProjectHintLocalOff))
	}
	return err.WithHint(i18n.T(msgid.ProjectHintDeploySync), i18n.T(msgid.ProjectHintDeployEdit))
}

// checkMembers 校验外壳成员关系（提案 §8.1、§8.4）：members 只能写在外壳条目上，
// 成员必须已声明、不能是外壳、只能有一个版本、最多属于一个外壳。
func (p *Project) checkMembers() error {
	problems := clierr.NewProblemSet(clierr.CodeConfigInvalid,
		i18n.T(msgid.ProblemValidationFailed, filepath.Base(p.DeployPath))).
		WithSource(i18n.T(msgid.LabelFile), p.DeployPath)
	p.shellOf = map[string]string{}

	for i, entry := range p.Deploy.Components {
		if len(entry.Members) == 0 {
			continue
		}
		shellID, _ := entry.Key()
		field := yamlfile.Indexed("components", i) + ".members"
		if !p.Decl.IsShellID(shellID) {
			problems.Add(field, i18n.T(msgid.ProjectMembersOnNonShell, shellID))
			continue
		}
		for j, member := range entry.Members {
			memberField := yamlfile.Indexed(field, j)
			switch versions := p.Decl.Versions(member); {
			case len(versions) == 0:
				problems.Add(memberField, i18n.T(msgid.ProjectMemberUndeclared, member))
				continue
			case p.Decl.IsShellID(member):
				problems.Add(memberField, i18n.T(msgid.ProjectMemberIsShell, member))
				continue
			case len(versions) > 1:
				problems.Add(memberField, i18n.T(msgid.ProjectMemberMultiVersion, member))
				continue
			}
			if prev, ok := p.shellOf[member]; ok && prev != shellID {
				problems.Add(memberField, i18n.T(msgid.ProjectMemberTwoShells, member, prev))
				continue
			}
			p.shellOf[member] = shellID
		}
	}
	return problems.Err()
}
```

- [ ] **Step 6: Implement config loading**

`internal/project/configload.go`:

```go
package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// loadConfig 读 config/：公共变量、部署文件的 vars:、每个组件版本对应的配置文件，
// 并检查文件名冲突、多版本歧义、孤儿文件与悬空的 $var: 引用（提案 §7、附录 A5）。
func (p *Project) loadConfig() error {
	if err := p.loadVars(); err != nil {
		return err
	}
	deployVars, err := configdir.ParseVarsMap(p.Deploy.Vars, p.DeployPath)
	if err != nil {
		return err
	}
	p.DeployVars = deployVars

	if err := p.checkConfigNames(); err != nil {
		return err
	}
	present, err := p.scanConfigDir()
	if err != nil {
		return err
	}

	p.configs = map[string]*configdir.File{}
	used := map[string]bool{}
	conflicts := &configdir.ConflictError{}
	for _, c := range p.Decl.Components {
		name := configdir.FileName(c.ID, c.Version)
		if !present[name] {
			name = configdir.FileName(c.ID, "")
			if !present[name] {
				continue
			}
			if versions := p.Decl.Versions(c.ID); len(versions) > 1 {
				return ambiguityError(name, c.ID, versions)
			}
		}
		used[name] = true

		path := filepath.Join(p.Layout.ConfigDir(), name)
		data, err := yamlfile.Read(path)
		if err != nil {
			return err
		}
		f, err := configdir.ParseComponentFile(data, path)
		var conflict *configdir.ConflictError
		if errors.As(err, &conflict) {
			conflicts.Merge(conflict)
			continue
		}
		if err != nil {
			return err
		}
		p.configs[c.Ref()] = f
	}
	if len(conflicts.Files) > 0 {
		return conflicts.Render()
	}

	names := make([]string, 0, len(present))
	for name := range present {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !used[name] {
			p.Warnings = append(p.Warnings, clierr.Warn(clierr.CodeConfigInvalid,
				i18n.T(msgid.ProjectConfigOrphan, filepath.Join(DirConfig, name))))
		}
	}
	return p.checkVarRefs()
}

func (p *Project) loadVars() error {
	path := p.Layout.VarsPath()
	data, err := yamlfile.Read(path)
	if errors.Is(err, fs.ErrNotExist) {
		p.Vars = map[string]configdir.Value{}
		return nil
	}
	if err != nil {
		return err
	}
	f, err := configdir.ParseVarsFile(data, path)
	var conflict *configdir.ConflictError
	if errors.As(err, &conflict) {
		return conflict.Render()
	}
	if err != nil {
		return err
	}
	p.Vars = f.Map()
	return nil
}

// checkConfigNames 拦下"两个组件 ID 对应到同一个配置文件名"（a-b/c 与 a/b-c）。
func (p *Project) checkConfigNames() error {
	owner := map[string]string{}
	for _, id := range p.Decl.IDs() {
		base := configdir.FileBase(id)
		if prev, ok := owner[base]; ok {
			return clierr.New(clierr.CodeConfigInvalid,
				i18n.T(msgid.ProjectConfigNameCollision, prev, id, configdir.FileName(id, ""))).
				WithHint(i18n.T(msgid.ProjectHintConfigCollision))
		}
		owner[base] = id
	}
	return nil
}

// scanConfigDir 列出 config/ 下的组件配置文件名（不含 vars.yaml、子目录与其它文件）。
func (p *Project) scanConfigDir() (map[string]bool, error) {
	entries, err := os.ReadDir(p.Layout.ConfigDir())
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, _, ok := configdir.ParseFileName(e.Name()); ok {
			present[e.Name()] = true
		}
	}
	return present, nil
}

func ambiguityError(name, id string, versions []string) *clierr.Error {
	perVersion := make([]string, 0, len(versions))
	for _, v := range versions {
		perVersion = append(perVersion, filepath.Join(DirConfig, configdir.FileName(id, v)))
	}
	return clierr.New(clierr.CodeConfigInvalid,
		i18n.T(msgid.ProjectConfigAmbiguous, name, id, strings.Join(versions, ", "))).
		WithHint(i18n.T(msgid.ProjectHintConfigAmbiguous, strings.Join(perVersion, ", ")))
}

// checkVarRefs 在装载阶段就拦下悬空的 $var:——lint 与 up 走同一处，不必等到解析某个组件。
func (p *Project) checkVarRefs() error {
	var dangling []string
	for _, c := range p.Decl.Components {
		f := p.configs[c.Ref()]
		if f == nil {
			continue
		}
		for _, e := range f.Entries {
			if e.Value.Kind != configdir.KindVarRef {
				continue
			}
			if _, ok := configdir.LookupVar(e.Value.Name, p.DeployVars, p.Vars); ok {
				continue
			}
			rel, err := filepath.Rel(p.Layout.Root, f.Path)
			if err != nil {
				rel = f.Path
			}
			dangling = append(dangling, rel+": "+e.Key+" → "+e.Value.String())
		}
	}
	if len(dangling) == 0 {
		return nil
	}
	err := clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.ProjectVarUndefined))
	for _, ref := range dangling {
		err = err.WithDetail(i18n.T(msgid.ConfigdirLabelUndefinedRef), ref)
	}
	return err.WithHint(i18n.T(msgid.ConfigdirHintDefineVar))
}
```

Note for `TestLoadConfigAmbiguousAcrossVersions`: the loop reaches `people/basic@1.0.0` first, which has its own file, then `people/basic@2.0.0`, which falls back to `people-basic.yaml` → ambiguity. `versions` is `["1.0.0", "2.0.0"]` in declaration order, matching the expected message.

Note for `TestLoadConfigOrphansWarn`: `config/.archive/` is a directory (skipped), `README.md` is not `.yaml` (skipped), leaving exactly three orphans: `gone-thing.yaml`, `erp-backend@9.9.9.yaml`, `people-basic.yaml`.

- [ ] **Step 7: Run tests and checks**

Run: `go build ./... && go vet ./internal/project/... && go test ./internal/project/... ./internal/projfile/... ./internal/deployfile/... ./internal/configdir/... ./internal/envref/... ./internal/yamlfile/... ./tests/i18nguard/... && .tools/bin/golangci-lint run ./internal/project/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/project internal/msgid/project.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "feat(project): load a three-layer project with deploy-file selection and strict cross-file checks

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

## Hand-off to P2

When all eight tasks are committed, write `docs/superpowers/plans/2026-09-26-three-layer-p2-pipeline-switch.md` against the code as it now is (roadmap row P2). Everything P2 needs from this plan is listed in the **Interfaces → Produces** blocks above; in particular `project.Load`, `(*Project).ConfigInput` + `configdir.Resolve` (inject's new config source), `(*Project).DeployEntry` (cascade/compose/k8s per-component settings), `(*Project).ShellOf` (P3), and `configdir.Value.Kind` (per-target evaluation, A6/A7).
