# Three-layer refactor P2 — Pipeline switch — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every command that reads a project reads the three-layer model (`project.Load`) instead of `config.Config`; `resources`, `servedBy`, `override.yaml`, `--config`, `--context` are gone; secrets and file references reach Docker and K8s the way Appendix A6/A7 require; and the eight deferred P1 review minors are closed.

**Architecture:** Bottom-up. First the P1 packages get their review fixes (Task 1). Then the library layer switches one package at a time to consume `*project.Project` directly — the P1 aggregate is the only input, no adapter back to `config.Config` (Tasks 2–9). The CLI switches last (Tasks 10–11); **between Task 2 and Task 11 `go build ./...` is expected to fail in `internal/cli`** — each task verifies its own packages with `go test ./internal/<pkg>/...`, and Task 11 restores a building tree. Old packages are deleted in Task 12.

Design patterns: **Value Object + Strategy per target** — `configdir.Value` reaches the renderers with its kind intact, and each target has exactly one function deciding where a kind goes (`compose.envPlacement`, `k8s.envPlacement`, `compose.localValue`). Nobody decides "evaluate now or later" anywhere else.

**Tech Stack:** Go, `gopkg.in/yaml.v3`, testify, existing `clierr`/`i18n`/`msgid`.

**Spec:** `new_plan/提案.md` (Appendix A binding — A2, A5, A6, A7, A8, A10, A11, A14–A19 apply here), `new_plan/命令表.md`. Roadmap: `docs/superpowers/plans/2026-09-26-three-layer-refactor-roadmap.md`. P1 plan (interfaces consumed): `docs/superpowers/plans/2026-09-26-three-layer-p1-data-model.md`.

**Why this plan is written differently from P1:** P1 created new files, so every step could carry its full code. P2 is surgery on ~40 existing files whose bodies are long and heavily commented. Each task therefore gives: exact new signatures, the behavioural rules, full code for every *new* function and every *new* test, and precise transformation rules for mechanical migrations (e.g. "every `entries[ref]` lookup of a `config.Component` becomes `p.DeployEntry(ref.ID, ref.Version)`"). The executor reads each file before editing it.

## Global Constraints

- **The only project input below the CLI is `*project.Project`.** No package outside `internal/project` may parse `brickkit.yaml` / deploy files / `config/` itself.
- **Per-component deploy settings** always come from `p.DeployEntry(id, version)` (`deployfile.Component`); **declared components** from `p.Decl.Components`; **project-level K8s settings** from `p.Deploy.Settings()` / `p.Deploy.ShouldCreateNamespace()` etc.; **target** from `p.Deploy.Target` (`docker|podman|k8s`; `podman` renders exactly like `docker`).
- **Config keys are environment variable names** (A10): no camelCase → SNAKE conversion anywhere; `inject.EnvVarName` is deleted.
- **Reserved variables** after this plan: exact `COMPONENT_ID`, `COMPONENT_VERSION`, `BRICKKIT_SERVED_MEMBERS`, `BRICKKIT_SERVED_MEMBERS_CONFIG`, `PORT`; suffix `_ENDPOINT`. The resource prefixes (`DATABASE_`, `REDIS_`, `MQ_`, `STORAGE_`, `SEARCH_`, `SMTP_`) and `{envPrefix}_` are **no longer reserved** — resources are abolished, so those names belong to components.
- **Where a config value goes (A6/A7)** — the whole table, per value kind:

  | Kind | Docker/Podman `compose.yaml` | Docker env file (`.brickkit/generated/env/<service>.env`, 0600) | K8s | local-debug / `mode: local` process env |
  | --- | --- | --- | --- | --- |
  | Literal, not secret | inline, `$` escaped as `$$` | — | plain `env` value | as is |
  | `${VAR}` template, not secret | inline raw (compose interpolates at start) | — | expanded now (process env → `.env`), plain `env` | expanded now |
  | Literal or `${VAR}`, `secret: true` | — | yes (literal escaped; template written raw so compose interpolates) | expanded now → generated Secret + `secretKeyRef` | expanded now |
  | `file://path` (any) | — | yes: file content read now, escaped | content read now → generated Secret + `secretKeyRef` | content read now |
  | `{existingSecret, key}` | skipped (no Docker equivalent — unchanged behaviour) | — | `secretKeyRef` to that Secret | skipped |

  A missing `file://` target is a hard error at generation (`up`, `up --dry-run`) naming the component, key and path. Env-file encoding: `NAME="<value>"` with `\` → `\\`, `"` → `\"`, newline → `\n`, CR → `\r`, and (literals/file contents only) `$` → `$$` — verified 2026-09-26 against Compose v5.3.1: a multi-line PEM, `$`, quotes and backslashes arrive byte-exact.
- **Paths:** env files are referenced from `compose.yaml` relative to the Compose project directory the engine already passes (`--project-directory <root>`), i.e. `.brickkit/generated/env/<service>.env`.
- **i18n discipline** as in P1 (every user-visible string via `msgid` + both catalogs; `go test ./tests/i18nguard/...` green at the end of every task that touches strings).
- **Deleting is part of the job:** tests for removed features (resources, servedBy field, override.yaml, `--config`, `--context`) are deleted with the feature; tests for changed behaviour are rewritten, not skipped.
- **Commits:** straight to `main`, message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Per-task verification commands are listed in each task; `go build ./...` is required green only from Task 11 on.

## Review Focus

1. **A secret or `file://` value on Docker must never appear in `compose.yaml`**, and must arrive in the container byte-exact (multi-line PEM, `$`, quotes, backslashes). (Task 6 `TestComposeSecretsGoToEnvFile`, Task 13 end-to-end)
2. **A literal value containing `$` on Docker** (e.g. a password `pa$$word` written directly) must reach the container unchanged — previously Compose would have interpolated it. (Task 6 `TestComposeLiteralDollarEscaped`)
3. **`file://` pointing at a missing file** fails loudly at `up --dry-run`, naming component, key and path — not an empty variable. (Task 5 `TestEvaluateFileRefMissing`, Task 6)
4. **`-f deploy.prod.yaml` while local mode is on** uses the prod file, and `mode: debug` in it is rejected (P1 rule) — the CLI must pass `-f` through untouched to `project.Load`. (Task 11 `TestUpFileFlagIgnoresLocalMode`)
5. **Two versions of one component covered by a bare deploy entry with `exposePort`** must be a load error, not a Docker bind failure halfway through `up`. (Task 1 `TestLoadCrossVersionHostPortCollision`)

---

## Task 1: Close the P1 review minors

**Files:** `internal/project/{configload.go,check.go,layout.go,load_test.go}`, `internal/configdir/{conflict.go,conflict_test.go?}`, `internal/deployfile/validate.go`, `internal/yamlfile/{yamlfile.go,yamlfile_test.go}`, `internal/msgid/{project.go,configdir.go,layer.go}`, both catalogs.

**Interfaces produced:** none new outside the packages; `project.Layout` constants now alias `projfile.FileName`, `deployfile.FileTeam/FileLocal`, `configdir.VarsFile/ArchiveDir`.

Rules (one sub-step each, each RED → GREEN):

1. **Local-mode undefined-var hint.** In `checkVarRefs`, when `p.DeploySource == DeployLocal` and a dangling name is defined in the team `deploy.yaml`'s `vars:` (read best-effort with `deployfile.ParseFile(p.Layout.DeployPath(), RoleTeam)`; parse errors ignored), append hint `ProjectHintVarOnlyInTeamDeploy` ("%[1]s is defined in deploy.yaml's vars: but not in your deploy.local.yaml — run brickkit local refresh, or copy it over") and keep the existing `ConfigdirHintDefineVar`. Test `TestLoadUndefinedVarLocalModeHint`.
2. **One conflict report.** `loadVars` returns a `*configdir.ConflictError` instead of rendering it; `loadConfig` merges it with component-file conflicts into the single `ConflictError`. Test `TestLoadVarsAndComponentConflictsTogether` (vars.yaml + one component file both with duplicates → one error whose details name both files).
3. **Hint wording.** Split `ConfigdirConflictHintEdit` into `ConfigdirConflictHintEditMarked` (current text) used when any line has a `Side`, and `ConfigdirConflictHintEditPlain` ("Keep the line you want and delete the other, then run the command again") otherwise. `ambiguityError` joins with `i18n.T(msgid.ListSeparator)` (both places). Test asserts the plain hint for an unmarked duplicate.
4. **Unused unversioned file.** When `config/<base>.yaml` is unused *because every version of that ID has its own file*, the warning is `ProjectConfigUnversionedUnused` ("%[1]s is ignored: every version of %[2]s in brickkit.yaml has its own config/%[3]s@<version>.yaml") instead of the generic orphan text. Test updates `TestLoadConfigOrphansWarn` (still 3 warnings, one with the new message).
5. **Cross-version host-port collisions** (target docker/podman only): a new step `checkHostPorts` in `Load` after `checkCoverage`. For every declared ref compute its entry via `Deploy.Entry`; collect claims `exposePort` (only when `expose`), `localPort`. Two *different refs* claiming the same host port → hard error `clierr.CodePortConflict`, message `ProjectHostPortCollision` ("Error: host port %[1]d is claimed twice") with details naming both refs and fields, hint `ProjectHintHostPortPerVersion` ("A bare ID entry applies to every version: give each version its own id@version entry with a different port"). Test `TestLoadCrossVersionHostPortCollision` (bare `people/basic` with `expose: true, exposePort: 8080` covering 1.0.0 and 2.0.0) and `TestLoadLocalPortVsExposePort`.
6. **Single source of names.** `project.FileDecl = projfile.FileName`, `FileDeploy = deployfile.FileTeam`, `FileDeployLocal = deployfile.FileLocal`, `FileVars = configdir.VarsFile`, `DirConfigArchive = configdir.ArchiveDir`; `deployfile.varNameRe` deleted, `validateVarNames` uses `configdir.IsValidName` (deployfile may import configdir: no cycle).
7. **Multi-document YAML.** `yamlfile.Document` returns `LayerMultipleDocuments` ("Error: %[1]s contains more than one YAML document (a stray ---?)") when the stream has a second document. Implement with `yaml.NewDecoder` decoding twice (second `Decode` must return `io.EOF`). Test `TestDocumentRejectsSecondDocument`.
8. **All ID collisions.** `checkConfigNames` collects every colliding pair into one error (one detail per pair). Test with two independent collisions.

- [ ] Steps: for each rule write the named test, run `go test ./internal/project/... ./internal/configdir/... ./internal/yamlfile/... ./internal/deployfile/...` (RED), implement, re-run (GREEN), then `go test ./tests/i18nguard/...` and golangci-lint on the four packages. Commit `fix(project): close P1 review minors`.

---

## Task 2: `manifest` v2 — image/build, shell capability, key rule, no resources

**Files:** `internal/manifest/{types.go,validate.go,lint.go,scaffold.go,envvar.go}`, new `internal/manifest/image.go`, tests, `internal/msgid/manifest.go`, catalogs.

**Produces:**
- `Deployment.Image string yaml:"image,omitempty"` (no longer required); `Deployment.Build *Build yaml:"build,omitempty"`; `type Build struct { Context string yaml:"context,omitempty"; Dockerfile string yaml:"dockerfile,omitempty" }`.
- `Manifest.Shell *Shell yaml:"shell,omitempty"`; `type Shell struct { Members []string yaml:"members" }` — presence of `shell` makes the component a shell (A11); `(*Manifest).IsShell() bool`; `(*Manifest).CanHost(id string) bool`.
- `(*Manifest).HasStandaloneImage() bool` = `Image != "" || Build != nil`.
- `manifest.ImageRef(m *Manifest) string`: `Image` containing a tag (`:` after the last `/`) or digest (`@`) → as is; `Image` without tag → `Image + ":" + Metadata.Version`; no `Image` but `Build` → `FileBase(id) + ":" + version` where `FileBase` replaces `/` with `-` (the tag `brickkit build` will produce in P5, spec §9.10.4). Renderers use `ImageRef` everywhere they used `Deployment.Image`.
- Removed: `Dependencies.Resources`, `ResourceDep`, `ResourceKind*`, `ResourceKinds`, `IsKnownResourceKind`, `ResourceEnvPrefix`, `ResourceKindsText`.

Rules:
- `validateDeployment`: at least one of `image` / `build` → otherwise `ManifestImageOrBuildRequired`. `build.dockerfile`, `build.context` must not escape the repo root (reuse `escapesRepoRoot`).
- `validateConfigSchema`: every property key must satisfy `configdir.IsValidName` → `ManifestConfigKeyNotEnvName` ("%[1]q is not a valid environment variable name; configSchema keys are injected as-is (e.g. DB_HOST)"). Import direction `manifest → configdir` would cycle (configdir imports manifest), so **copy the regex** into manifest as `envNameRe` with a comment pointing at `configdir.IsValidName`, and add a test in `internal/configdir` asserting both accept/reject the same table (guards drift).
- `validateShell`: `members` non-empty, each a valid component ID, no duplicates, not the component itself.
- `dependencies.resources` written in an old manifest → rejected by the unknown-field walk (no special message).
- `lint.go` / `ReservedKeyWarnings`: keys are env names now (drop `EnvVarName` conversion).
- `scaffold.go` (`brickkit new`): writes `deployment.build: {context: ., dockerfile: Dockerfile}` instead of an `image:` placeholder, configSchema example key `LOG_LEVEL`; `--shell` support is P7.

- [ ] Steps: update existing manifest tests that used camelCase keys / resources (rewrite to env-name keys; delete resource-kind tests); add `TestImageRef` (tagged, digest, untagged, build-only), `TestValidateImageOrBuild`, `TestConfigKeyMustBeEnvName`, `TestValidateShell`, `TestResourcesFieldRejected`, and `internal/configdir`'s `TestEnvNameRuleMatchesManifest`. Run `go test ./internal/manifest/... ./internal/configdir/...` RED → implement → GREEN. Commit `feat(manifest): image/build, shell capability, env-name config keys; drop resources`.

---

## Task 3: `resolver` and `cascade` read the project

**Files:** `internal/resolver/resolver.go` (+tests), `internal/cascade/cascade.go` (+tests).

**Produces:**
- `(*resolver.Resolver).ResolveProject(ctx context.Context, p *project.Project) (*Graph, error)` — roots are `p.Decl.Components` in declaration order. `ResolveConfig` deleted.
- Deleted from resolver: `CheckRunningResourceBindings`, `CheckResourceBindings`, `unboundResourceDetails`, `servingShellID`, `resourceHints`, `matchResource`, `engineMismatch` and their messages/tests.
- `cascade.Compute(p *project.Project, graph *resolver.Graph) (*Result, error)` — `declSet` becomes `map[resolver.Ref]deployfile.Component` filled by `p.DeployEntry(c.ID, c.Version)` for each declared component; `pinned`/`disabled` use `deployfile.Component.IsPinned/IsDisabled`. Error messages that said "brickkit.yaml" for `mode` now name the deploy file: pass `filepath.Base(p.DeployPath)` into the hint messages (`CascadeHintRemoveDisabledFlag`, `CascadeHintRemovePinnedFlag` gain a `%[3]s` file argument in both catalogs).

Test helper (new file `internal/project/projecttest/projecttest.go`, package `projecttest`, used by every later task's tests):

```go
// Package projecttest 在测试里用几行 YAML 搭一个三层项目并装载它。
package projecttest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/project"
)

// Files 是 相对路径 → 内容。
type Files map[string]string

// Write 把文件写进 root。
func Write(t testing.TB, root string, files Files) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
}

// Load 在临时目录里写好文件并装载，失败直接让测试失败。
func Load(t testing.TB, files Files) *project.Project {
	t.Helper()
	root := t.TempDir()
	Write(t, root, files)
	p, err := project.Load(root, project.LoadOptions{})
	require.NoError(t, err)
	return p
}
```

- [ ] Steps: migrate `resolver_test.go` / `cascade_test.go` fixtures from `config.Config` literals to `projecttest.Load` (translation rule: each old `config.Component{ID, Version, Mode}` becomes a `brickkit.yaml` line plus a `deploy.yaml` entry with that `mode`). RED (compile) → implement → `go test ./internal/resolver/... ./internal/cascade/... ./internal/project/...` GREEN. Commit `refactor(resolver,cascade): consume project.Project; drop resource bindings`.

---

## Task 4: `inject` — config from `configdir.Resolve`, values keep their kind

**Files:** `internal/inject/{inject.go,reserved.go}` (+tests), `internal/msgid/inject.go`, catalogs.

**Produces:**

```go
// Var 是一条环境变量。Value 保留 configdir 的引用种类：何时求值、写到哪里由渲染器决定。
type Var struct {
	Name   string
	Value  configdir.Value
	Source string // SourcePlatform | SourceEndpoint | SourceConfig
	// Secret 来自 configSchema 的 secret: true（平台从不按名字猜）。
	Secret bool
	// Key 是原始 configSchema 键（A10 之后与 Name 相同，保留给外壳 JSON 使用）。
	Key string
	// Owner 是配置类变量所属组件的版本化服务名（K8s Secret 命名用）。
	Owner string
}

func Literal(s string) configdir.Value // helper: configdir.Value{Kind: KindLiteral, Text: s}
func (v Var) IsSecretRef() bool          // v.Value.Kind == configdir.KindSecretRef
func Build(p *project.Project, graph *resolver.Graph, states *cascade.Result) (*Result, error)
```

`SourceResource`, `SourceOverride`, `ResourceID`, `SecretKey`, `ExistingSecretRef`, `resourceVars`, `resourceBindings`, `envPrefixesOf`, `EnvVarName`, `formatValue`, `unknownConfigWarnings`, `noConfigSchemaWarning` are deleted (the latter two now live in `configdir.Resolve`).

Rules for `buildComponent`:
1. Platform vars and endpoints as before, values wrapped with `Literal`.
2. Config: `res, err := configdir.Resolve(p.ConfigInput(ref.ID, ref.Version, m.ConfigSchema))`; an error aborts `Build`. Each `res.Values` entry becomes `Var{Name: r.Key, Value: r.Value, Source: SourceConfig, Secret: r.Secret, Key: r.Key, Owner: service}` unless the name is reserved (existing `reservedConflictWarning`, now without envPrefix). `res.Missing` feeds the existing aggregated `missingRequiredError` (its hint now says to set the key in `config/<file>`: `InjectHintSetValue` args become file name + key). `res.Warnings` append to `Result.Warnings`.
3. Resources/labels merge from `p.DeployEntry(ref.ID, ref.Version).Resources/.Labels`.

- [ ] Steps: rewrite `inject_test.go` on `projecttest` fixtures; new tests `TestBuildKeepsValueKinds` (a `${X}` config value arrives as `KindEnvTemplate`, a `file://` as `KindFileRef`, `secret: true` sets `Secret`), `TestBuildDatabasePrefixNoLongerReserved` (`DATABASE_URL` in configSchema is injected, no warning), `TestBuildReservedEndpointSuffixStillBlocked`. RED → implement → GREEN (`go test ./internal/inject/...`). Commit `refactor(inject): config from configdir.Resolve, typed values, no resources`.

---

## Task 5: Evaluating values — `configdir.Evaluate` and the shared `.env` lookup

**Files:** new `internal/configdir/evaluate.go` (+test), `internal/envref/dotenv.go` (+test; moved from `internal/cli/up_k8s.go` `envLookup`/`readDotEnv`/`parseDotEnv` with their tests), `internal/msgid/configdir.go`, catalogs.

**Produces:**
- `envref.Lookup(root string) func(name string) (string, bool)` — process env first, then `<root>/.env` (parsed with the existing quoted/multi-line rules), cached.
- `configdir.Evaluate(v Value, root string, lookup func(string) (string, bool)) (string, error)`:
  - `KindLiteral` → `v.Text`; `KindEnvTemplate` → `envref.Expand(v.Text, lookup)` (unknown refs stay as `${X}`, unchanged behaviour); `KindFileRef` → content of `filepath.Join(root, v.Path)` (absolute paths as is), **byte-exact** (no trimming); missing/unreadable → `*clierr.Error` `ConfigdirFileRefMissing` ("Error: %[1]s points at a file that cannot be read") with detail path and reason; `KindSecretRef` / `KindVarRef` → programming error `CodeInternal` (callers must not evaluate those).

```go
func TestEvaluateKinds(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "k.pem"), []byte("-----BEGIN-----\nabc\n"), 0o600))
	lookup := func(n string) (string, bool) { return map[string]string{"U": "app"}[n], n == "U" }

	got, err := configdir.Evaluate(configdir.Value{Kind: configdir.KindFileRef, Path: "k.pem"}, root, lookup)
	require.NoError(t, err)
	assert.Equal(t, "-----BEGIN-----\nabc\n", got)
	got, err = configdir.Evaluate(configdir.Value{Kind: configdir.KindEnvTemplate, Text: "pg://${U}@${NOPE}"}, root, lookup)
	require.NoError(t, err)
	assert.Equal(t, "pg://app@${NOPE}", got)
}

func TestEvaluateFileRefMissing(t *testing.T) {
	_, err := configdir.Evaluate(configdir.Value{Kind: configdir.KindFileRef, Path: "nope.pem"}, t.TempDir(), nil)
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Error(), "nope.pem")
}
```

- [ ] Steps: move dotenv code + tests (RED: package missing) → GREEN; write evaluate tests (RED) → implement → GREEN. Commit `feat(configdir): evaluate value references; shared .env lookup`.

---

## Task 6: `compose` — project input, per-kind placement, env files

**Files:** `internal/compose/{compose.go,local.go,quota.go,servedby.go}` (+tests), new `internal/compose/envplace.go` (+test), `internal/msgid/compose.go`, catalogs.

**Produces:**

```go
type Options struct {
	Now    func() time.Time
	Engine string                             // docker | podman (extra_hosts alias)
	Root   string                             // project root, for file:// and env-file paths
	Lookup func(name string) (string, bool)   // envref.Lookup(root)
}

// EnvFile 是一个服务的 0600 环境变量文件（密钥与 file:// 内容，A7）。
type EnvFile struct {
	Service string
	// Path 相对项目根：.brickkit/generated/env/<service>.env
	Path    string
	Content []byte
}

type Result struct {
	YAML          []byte
	EnvFiles      []EnvFile
	LocalEnvFiles []LocalEnvFile
	Warnings      []*clierr.Error
}

func Generate(p *project.Project, graph *resolver.Graph, states *cascade.Result, env *inject.Result, opts Options) (*Result, error)
```

`Resources`/`ResourceRequirement`, `serviceNameResourceWarnings`, `looksLikeServiceName`, the `deploy.LocalhostResourceWarnings` call are deleted.

`envplace.go` (new, full code):

```go
package compose

import (
	"strings"

	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/inject"
)

// placement 是一条变量在 Docker 目标下的去处（Global Constraints 的表）。
type placement int

const (
	placeInline  placement = iota // 写进 compose.yaml 的 environment
	placeEnvFile                  // 写进 0600 的 env 文件
	placeSkip                     // Docker 没有对应概念（existingSecret）
)

// envPlacement 是 Docker 目标下"这条变量放哪"的唯一判定处。
func envPlacement(v inject.Var) placement {
	switch {
	case v.Value.Kind == configdir.KindSecretRef:
		return placeSkip
	case v.Value.Kind == configdir.KindFileRef, v.Secret:
		return placeEnvFile
	default:
		return placeInline
	}
}

// composeEscape 让字面量不被 compose 插值：$ → $$。
func composeEscape(s string) string { return strings.ReplaceAll(s, "$", "$$") }

// envFileLine 写一行 NAME="value"。literal 为 true 时 $ 也转义（字面量、文件内容），
// 为 false 时保留 ${VAR} 让 compose 在启动时插值（密钥模板）。
// 转义规则实测于 Compose v5.3.1：多行 PEM、$、引号、反斜杠逐字节到达容器。
func envFileLine(name, value string, literal bool) string {
	var b strings.Builder
	b.WriteString(name)
	b.WriteString(`="`)
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '$':
			if literal {
				b.WriteString("$$")
			} else {
				b.WriteRune(r)
			}
		default:
			b.WriteRune(r)
		}
	}
	b.WriteString("\"\n")
	return b.String()
}
```

Rules:
- `newPlan(p, …)`: entries via `p.DeployEntry`; `mode: debug|local` → `locals` (unchanged logic); shell membership via `p.ShellOf(ref.ID)` instead of `entry.ServedBy` (Task 9 adapts `shell.Resolve`). Network name from `p.Decl.Project`. Header shows `target: <p.Deploy.Target>` and the deploy file name.
- `environmentOf(c)` → `(inline []string, file []byte, err error)`: for each var by `envPlacement`: inline literal → `NAME=` + `composeEscape(text)`; inline template → `NAME=` + raw text; env file literal/file → `configdir.Evaluate` then `envFileLine(name, value, true)`; env file template (secret `${VAR}`) → `envFileLine(name, text, false)`. Evaluation errors abort `Generate` with the component ref and key added as details.
- A service with a non-empty env file gets `env_file: [{path: .brickkit/generated/env/<svc>.env}]` on **both** its main and migration service.
- `image` everywhere via `manifest.ImageRef(m)`.
- Existing migration chain, host-port checks, healthchecks unchanged.

Tests (new, full code in the test file; fixtures via `projecttest` + a fake `resolver.Graph` built the way the existing compose tests build it):
- `TestComposeLiteralDollarEscaped`: config `PASSWORD: pa$$word` (YAML literal, not secret) → `compose.yaml` environment contains `PASSWORD=pa$$$$word` (i.e. each `$` doubled) and the env file list is empty.
- `TestComposeSecretsGoToEnvFile`: schema `TOKEN {secret: true}`, `KEY {secret: false}` with `KEY: file://secrets/key.pem` (multi-line content with `$` and `"`), `TOKEN: ${TOKEN_FROM_ENV}` → neither name appears in `compose.yaml`; env file content equals `KEY="<escaped>"\nTOKEN="${TOKEN_FROM_ENV}"\n`; both main and migration services reference the env file.
- `TestComposeFileRefMissing`: `KEY: file://nope` → `Generate` error naming `nope`.
- `TestComposeExistingSecretSkipped`.

- [ ] Steps: rewrite existing compose tests onto `projecttest`; delete resource tests; write the four new tests (RED); implement; `go test ./internal/compose/...` GREEN. Commit `refactor(compose): project input; secrets and file:// via 0600 env files; \$ escaping`.

---

## Task 7: local processes (`mode: debug|local`) evaluate everything

**Files:** `internal/compose/local.go` (+tests), `internal/cli/up_local.go` is switched in Task 11.

Rule: `compose.localValue(v inject.Var, root, lookup) (value string, ok bool, err error)` — the single decision point for bare processes: `KindSecretRef` → `ok=false` (skipped, existing behaviour); everything else → `configdir.Evaluate`. `LocalEnvFile` generation and the POSIX-quoting of `local-debug.env` stay as they are, fed by `localValue`. Test `TestLocalEnvEvaluatesFileRef`.

- [ ] Steps: RED → implement → GREEN (`go test ./internal/compose/...`). Commit `refactor(compose): bare-process env evaluates file:// and templates`.

---

## Task 8: `k8s` — project input, per-kind placement, no resources

**Files:** `internal/k8s/*.go` (+tests), new `internal/k8s/envplace.go` (+test), `internal/msgid/k8s.go`, catalogs.

**Produces:** `k8s.Generate(p *project.Project, graph, states, env, opts)` with `opts` gaining `Root` and `Lookup` (the renderer evaluates now); `k8s.NamespaceOf(p *project.Project) string` (`p.Deploy.Settings().Namespace` or `brickkit-<project>`); `k8s.CheckEgressCoverage` deleted; `AllowToTarget` is `deployfile.AllowToTarget` (no `Resource`; `portsFor` returns `target.Ports`).

`envplace.go`: `envPlacement(v inject.Var) placement` with `placePlain` (literal or template, not secret → plain `env` value, template expanded via `configdir.Evaluate`), `placeGeneratedSecret` (`Secret` or `KindFileRef` → evaluated, goes into the component's generated Secret, env uses `secretKeyRef`), `placeExistingSecret` (`KindSecretRef` → `secretKeyRef{name: SecretName, key: SecretKey}`). Secret naming and the 0600 write of Secret files are unchanged.

Rules: every `p.cfg.Deploy.X` → `p.proj.Deploy.Settings().X` / helper methods; every `c.Entry` is a `deployfile.Component`; `LocalhostResourceWarnings` call deleted; images via `manifest.ImageRef`.

Tests: rewrite onto `projecttest`; new `TestK8sPlacement` (plain literal, plain expanded template, secret literal → Secret, `file://` → Secret, existingSecret → ref) and `TestK8sNamespaceFromDeployFile`.

- [ ] Steps: RED → implement → `go test ./internal/k8s/...` GREEN. Commit `refactor(k8s): project input; per-kind env placement; egress without resources`.

---

## Task 9: `shell` — membership from `deploy.yaml` (minimal; P3 does the rest)

**Files:** `internal/shell/shell.go` (+tests), `internal/compose/servedby.go`, `internal/k8s/servedby.go`.

Rule: `shell.Resolve(p *project.Project, graph, states, env)` — a component is a member when `p.ShellOf(id)` returns a shell whose ref is running; `ParseRef` is deleted; errors "servedBy target not found / not running" are rewritten as "members of shell X: shell not running" (`ShellMembersShellNotRunning`). Everything else (port checks, env merge, `BRICKKIT_SERVED_MEMBERS*` JSON) is kept exactly as is — P3 replaces the JSON and value handling. Member `mode: debug|local` (A18) keeps compiling: such a member is treated as a bare process (it is in `locals` from Task 6/8 and is not counted as a member) — P3 designs the full semantics.

- [ ] Steps: migrate shell tests onto `projecttest` (deploy entry `members:` instead of `servedBy:`); RED → implement → `go test ./internal/shell/... ./internal/compose/... ./internal/k8s/...` GREEN. Commit `refactor(shell): membership from deploy members`.

---

## Task 10: Remove `deploy/resource.go`; sources take `projfile`

**Files:** delete `internal/deploy/resource.go` (+tests, msgids); `internal/source/*.go` (+tests); `internal/workspace/workspace.go` (+tests).

Rules:
- `source.New(layout project.Layout, decl *projfile.File, opts Options)`; `newFetcher(s projfile.Source)`: `market` and `local` as before (`Name` replaces `ID` in messages); `git` returns a `clierr` `SourceGitNotYetSupported` ("git sources are being rebuilt for per-component repositories (baseUrl); use a local or market source for now") until P4. Per-component `source:` overrides are ignored until P4 (P4 rebuilds resolution order).
- `workspace` takes the project (`p.Layout`, `p.Decl`) and the cascade result.

- [ ] Steps: RED → implement → `go test ./internal/source/... ./internal/workspace/... ./internal/deploy/...` GREEN. Commit `refactor(source,workspace): consume projfile/project; drop resource requirements`.

---

## Task 11: CLI switch

**Files:** `internal/cli/*.go` (+tests), `internal/msgid/cli_*.go`, catalogs.

**Produces:**
- `Options`: `ConfigPath` removed; `DeployFile string`, `NoLocal bool` added. Persistent flags on `up`, `down`, `status`, `sync`, `lint`, `graph`: `-f, --file <path>` and `--no-local`. `--config` and `up/down --context` removed (Appendix: `--context` rejected by §11.0; cluster selection is `k8s.context` in the deploy file).
- `loadProject(ctx, opts)` = `project.Load(opts.WorkDir, project.LoadOptions{DeployFile: opts.DeployFile, NoLocal: opts.NoLocal})`, then prints `p.Warnings` once; the `project` struct in `lifecycle.go` holds `*project.Project` (field name `proj`) instead of `layout`+`cfg`; `overriddenMode` is replaced by "which deploy file" — `status` labels a component disabled in `deploy.local.yaml` with `(deploy.local.yaml)` when `DeploySource == DeployLocal` (message ids renamed `CliStatusViaLocalFile*`).
- `up`: builds via `resolver.ResolveProject` → `cascade.Compute(p, …)` → `inject.Build(p, …)` → `compose.Generate`/`k8s.Generate` with `Root: p.Layout.Root`, `Lookup: envref.Lookup(root)`; writes `EnvFiles` with mode 0600 under `.brickkit/generated/env/` (and removes stale env files of services no longer rendered); `--ignore-served-by` clears membership in memory (`p` gets an unexported `ignoreShells` switch honoured by `ShellOf`, set through `project.(*Project).IgnoreShells()`); resource requirement printing is deleted; target `podman` selects the existing Podman engine from `p.Deploy.Target` (not from override).
- Commands deleted: `override` (and `add_override.go`, `remove_override.go`). Commands stubbed with `clierr.NotImplemented`-style errors until their phase (message `CliCommandRebuilding`: "brickkit %[1]s is being rebuilt for the three-layer project model (phase %[2]s)"): `add` (P4), `remove` (P4). `init` becomes a minimal creator (P7 extends): writes `brickkit.yaml` (`project:` + commented `sources` example), `deploy.yaml` (`target: docker`, `components: []`), `config/vars.yaml` (comment header), `.gitignore` entries `deploy.local.yaml`, `deploy.local.yaml.bak`, `.secrets/`, `.brickkit/`, `config/.archive/`, and `.brickkit/` dirs. `lint` becomes `project.Load` + manifest lint of local-source components + print warnings (`--strict` semantics unchanged); P7 adds §11.3's full list.
- `restore`/`hooks`/`restore_check`: "mode changed" now compares **deploy.yaml** (team file) against the last commit instead of brickkit.yaml.

Tests: every cli test that built a `brickkit.yaml` with `deploy:`/`mode`/`config`/`resources`/`servedBy` is rewritten to three files via `projecttest.Write`; tests of deleted features are deleted. New:
- `TestUpFileFlagIgnoresLocalMode` — local mode on, `deploy.local.yaml` target podman, `-f deploy.prod.yaml` target k8s → `up --dry-run` writes K8s manifests.
- `TestUpNoLocal` — local mode on, `--no-local` → uses `deploy.yaml`.
- `TestUpDryRunWritesEnvFiles0600`.
- `TestInitCreatesThreeLayers`.
- `TestRemovedFlagsRejected` — `--config` and `--context` are unknown flags.

- [ ] Steps: switch file by file, starting at `lifecycle.go`/`topology.go` (shared), then `up*.go`, `down.go`, `status.go`, `sync.go`, `graph.go`, `restore*.go`, `hooks.go`, `k8s_cluster.go`, `lint.go`, `init.go`, `fetch.go`, `login.go`, `logout.go`, `publish*.go`, `skills.go`, `new.go`, `root.go`; `go build ./...` GREEN; `go test ./internal/cli/...` GREEN. Commit `refactor(cli): commands read the three-layer project; -f/--no-local; drop override/--config/--context`.

---

## Task 12: Delete the old model; regenerate schemas

**Files:** delete `internal/config/`, `internal/override/`, `schemas/override.schema.json`, `internal/msgid/{config.go,override.go,cli_override.go}` entries that became unused (and their catalog lines); `internal/schemagen/*`, `cmd/gen-schemas/*`, `schemas/*.json`.

Rules:
- `schemagen`: `ProjectFile = "brickkit.schema.json"` from `projfile.File`; new `DeployFile = "deploy.schema.json"` from `deployfile.File` (override entry: `map[string]yaml.Node` for `vars` → `{"type":"object","additionalProperties":{"type":["string","number","boolean","object","null"]}}`); component schema regenerated from the new manifest; `OverrideFile` removed.
- A guard test `TestNoOldModelImports` in `tests/i18nguard` (or a new `tests/archguard`): no Go file imports `internal/config` or `internal/override` (those directories must not exist).
- Unused msgids: the i18n guard already fails on catalog entries without a constant? If not, remove them by hand: every constant in `msgid/config.go` / `override.go` / `cli_override.go` with no remaining reference (`grep -rn`) is deleted with its two catalog lines.

- [ ] Steps: delete → `go build ./...` → fix fallout → `make generate-schemas` → `go test ./internal/schemagen/... ./cmd/gen-schemas/...` GREEN. Commit `refactor: delete config/override packages; schemas for brickkit.yaml v2 and deploy.yaml`.

---

## Task 13: End-to-end fixture + full suite green

**Files:** new `internal/cli/testdata/three-layer/` project (two local-source components, one with `migration`, one secret, one `file://` PEM containing `$` and `"`, a `vars.yaml`, a `deploy.yaml`, a `deploy.local.yaml`), new `internal/cli/threelayer_e2e_test.go`.

Test `TestThreeLayerDryRunDockerAndK8s`: copy fixture to a temp dir; `up --dry-run` (docker) → assert `compose.yaml` contains no secret/PEM text, env file 0600 with exact escaped content, migration service references env file, `$` literal doubled; `up --dry-run -f deploy.k8s.yaml` → assert PEM lands in a Secret manifest byte-exact and never in a Deployment.

Then the whole suite: `go build ./... && go vet ./... && go test ./internal/... ./cmd/... ./tests/i18nguard/...` GREEN (docs-related suites under `tests/docfields`, `scripts/*` stay red until P8/P9 per roadmap). golangci-lint on `./internal/...`.

- [ ] Steps: write test (RED until fixture exists) → fixture → GREEN → full suite → commit `test(cli): three-layer end-to-end dry run for docker and k8s`.

---

## Hand-off to P3

P3 (shell) starts from: `shell.Resolve(p, …)` with membership from `members`, `manifest.Shell`/`HasStandaloneImage`, the Docker env-file mechanism (the shell's `BRICKKIT_SERVED_MEMBERS_CONFIG` JSON with resolved values will go through `envFileLine(…, literal=true)`), and member `mode: debug|local` being treated as bare processes.
