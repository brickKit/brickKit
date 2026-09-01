# BrickKit Skills 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `brickkit init` 把一套 AI 助手技能装进项目，并提供 `brickkit skills` 显式刷新它们。

**Architecture:** 技能资产以纯文本躺在 `internal/skills/assets/`，用 `//go:embed` 编进二进制。
`internal/skills` 提供 `Installer`，按「五态判定」决定每个文件是写入还是跳过；
指纹记在 `.brickkit/skills.lock`。CLI 侧只做两件事：`init` 里调一次 `Apply`，
新增一条 `brickkit skills` 命令（`status` / `update`）。

**Tech Stack:** Go 1.22、cobra、testify、`embed`、`crypto/sha256`、`encoding/json`

**Spec:** `docs/superpowers/specs/2026-09-01-brickkit-skills-design.md`

## Global Constraints

- Go 1.22。依赖不新增：只用标准库 + 已有的 cobra / testify。
- 全部注释、错误文案、CLI 输出用**简体中文**，沿用仓库既有语气。
- 错误一律走 `internal/clierr`（`clierr.New` / `Newf` / `WithDetail` / `WithHint`）；
  结构化日志走 `internal/logging`。
- 文件权限 `0o644`，目录 `0o755`（与 `internal/config/scaffold.go` 的 `filePerm` / `dirPerm` 一致）。
- **`internal/` 覆盖率门槛 `COVER_MIN = 92`**（`make cover-check`）。新包必须自带扎实测试。
- SKILL.md 的 frontmatter **只用 Agent Skills 规范的六个字段**：
  `name`、`description`、`allowed-tools`、`metadata`、`license`、`compatibility`。
  开头的 `---` 必须是**文件第一行**。
- 每条 `description` 硬上限 **1024 字符**（listing 在 1536 处截断），目标 200–350 字。
- **绝不覆盖用户可能亲手写过的文件。** 判不准就跳过并提示，不提供 `--force`。
- 单元测试跑 `make test-unit`；静态检查跑 `make lint`。

---

## File Structure

**新建**

| 文件 | 职责 |
| --- | --- |
| `internal/skills/assets.go` | `//go:embed` 声明 + 资产清单（源路径 → 项目内落点） |
| `internal/skills/assets/AGENTS.md` | 项目级导读，唯一的内容载体 |
| `internal/skills/assets/CLAUDE.md` | 一行 `@AGENTS.md`，Claude Code 的入口 |
| `internal/skills/assets/claude/skills/brickkit-assemble/SKILL.md` | 拼装项目 |
| `internal/skills/assets/claude/skills/brickkit-component/SKILL.md` | 写组件 |
| `internal/skills/assets/claude/skills/brickkit-deploy/SKILL.md` | 部署上线 |
| `internal/skills/assets/claude/skills/brickkit-troubleshoot/SKILL.md` | 排障 |
| `internal/skills/lock.go` | `skills.lock` 读写 |
| `internal/skills/install.go` | 五态判定 + 写入 |
| `internal/cli/skills.go` | `brickkit skills` 命令 |

**修改**

| 文件 | 改什么 |
| --- | --- |
| `internal/config/layout.go` | 加 `FileSkillsLock` 常量与 `SkillsLockPath()` 方法 |
| `internal/cli/init.go` | 接线 `Apply` + `--no-skills` + 输出 |
| `internal/cli/root.go` | 注册 `newSkillsCommand` |
| `scripts/check-cli-docs.py` | 扫描范围纳入 `internal/skills/assets/` |
| 8 份文档 | 见 Task 8 |

**为什么 `Layout.SkillsLockPath()` 要写成 `l.path(DirBrickkit, FileSkillsLock)` 这个形状：**
`scripts/check-doc-tree.py` 不写死名单，它从 `layout.go` 里这个形状的方法推导
`.brickkit/` 下的合法项。写成这个形状，文档目录树的校验会自动认它。

---

### Task 1: 资产载体与自检

**Files:**
- Create: `internal/skills/assets.go`
- Create: `internal/skills/assets/AGENTS.md`
- Create: `internal/skills/assets/CLAUDE.md`
- Test: `internal/skills/assets_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `type Asset struct{ Source, Target string }`、`func Assets() []Asset`、
  `func (a Asset) Content() ([]byte, error)`、`func Sum(b []byte) string`（返回 `sha256:` + 十六进制）

本任务只做资产载体与自检，SKILL.md 留给 Task 2。交付物本身可用：
`Assets()` 能列出 AGENTS.md 与 CLAUDE.md，内容可读，自检通过。

- [ ] **Step 1: 写失败的测试**

`internal/skills/assets_test.go`：

```go
package skills

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 资产清单声明的每个源路径都必须真实存在，内容非空。
// 清单与文件分头维护，漏一个就在这里响。
func TestAssetsAllReadable(t *testing.T) {
	list := Assets()
	require.NotEmpty(t, list)
	for _, a := range list {
		b, err := a.Content()
		require.NoError(t, err, "读不到内嵌资产：%s", a.Source)
		assert.NotEmpty(t, b, "内嵌资产是空的：%s", a.Source)
	}
}

// 落点必须是项目内的相对路径：绝对路径或 .. 会写到项目外面去。
func TestAssetTargetsAreSafeRelativePaths(t *testing.T) {
	for _, a := range Assets() {
		assert.False(t, strings.HasPrefix(a.Target, "/"),
			"落点不能是绝对路径：%s", a.Target)
		assert.NotContains(t, a.Target, "..",
			"落点不能含 ..：%s", a.Target)
	}
}

// 落点不能重复——两份资产写同一个文件，后者会静默覆盖前者。
func TestAssetTargetsUnique(t *testing.T) {
	seen := map[string]string{}
	for _, a := range Assets() {
		if prev, ok := seen[a.Target]; ok {
			t.Fatalf("落点重复：%s 与 %s 都写 %s", prev, a.Source, a.Target)
		}
		seen[a.Target] = a.Source
	}
}

func TestSumIsStableAndPrefixed(t *testing.T) {
	s := Sum([]byte("hello"))
	assert.True(t, strings.HasPrefix(s, "sha256:"))
	assert.Equal(t, s, Sum([]byte("hello")))
	assert.NotEqual(t, s, Sum([]byte("hello!")))
}

// CLAUDE.md 必须导入 AGENTS.md：官方文档明确 Claude Code 读 CLAUDE.md 而不读
// AGENTS.md，这一行是两边读同一份内容的唯一连接点。断了就等于 Claude Code
// 什么导读都没有，而且不会有任何报错。
func TestClaudeMdImportsAgentsMd(t *testing.T) {
	for _, a := range Assets() {
		if a.Target != "CLAUDE.md" {
			continue
		}
		b, err := a.Content()
		require.NoError(t, err)
		assert.Contains(t, string(b), "@AGENTS.md")
		return
	}
	t.Fatal("资产清单里没有 CLAUDE.md")
}
```

- [ ] **Step 2: 跑测试确认它失败**

Run: `go test ./internal/skills/ -v`
Expected: 编译失败——`undefined: Assets`、`undefined: Sum`

- [ ] **Step 3: 写 `internal/skills/assets.go`**

```go
// Package skills 管理装进用户项目的 AI 助手技能资产。
//
// 资产以纯文本躺在 assets/ 下，用 //go:embed 编进二进制：BrickKit CLI 是
// 单二进制、用完即走、离线可用的，技能不该需要一次网络往返才拿得到。
// 版本严格跟着 CLI 走也正是想要的语义——那份文件描述的就是这个版本的行为。
package skills

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"path"
	"strings"
)

//go:embed assets
var assetFS embed.FS

// assetRoot 是内嵌资产在 embed.FS 里的前缀。
const assetRoot = "assets"

// Asset 是一份内嵌资产：内嵌路径与它在用户项目里的落点。
type Asset struct {
	// Source 是 embed.FS 里的路径，如 assets/claude/skills/x/SKILL.md。
	Source string
	// Target 是项目内的相对路径，如 .claude/skills/x/SKILL.md。
	Target string
}

// Content 返回内嵌内容。
func (a Asset) Content() ([]byte, error) {
	return assetFS.ReadFile(a.Source)
}

// Assets 返回全部资产，按落点排序（输出与 lock 顺序都要稳定）。
//
// 清单从 embed.FS 遍历得来而不是写死名单：assets/ 下加一个文件就自动纳入，
// 免得「加了文件忘了登记」——那种漏法不报错，只是静默少装一份。
func Assets() []Asset {
	var list []Asset
	walk(assetRoot, &list)
	return list
}

func walk(dir string, list *[]Asset) {
	entries, err := assetFS.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		p := path.Join(dir, e.Name())
		if e.IsDir() {
			walk(p, list)
			continue
		}
		*list = append(*list, Asset{Source: p, Target: targetOf(p)})
	}
}

// targetOf 把内嵌路径映射成项目内落点。
//
// assets/claude/ 这一层对应项目里的 .claude/：embed 不接受以点开头的目录，
// 所以内嵌侧只能叫 claude/，映射时补上那个点。
func targetOf(source string) string {
	rel := strings.TrimPrefix(source, assetRoot+"/")
	if after, ok := strings.CutPrefix(rel, "claude/"); ok {
		return ".claude/" + after
	}
	return rel
}

// Sum 返回内容的 sha256，形如 sha256:<十六进制>。
func Sum(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
```

- [ ] **Step 4: 写 `internal/skills/assets/AGENTS.md`**

```markdown
# 这个项目用 BrickKit 拼装

> 写给 AI 助手。人也能读，但它存在的理由是让助手不必猜。

BrickKit 是**组件管理与拼装平台**：每块积木（组件）独立开发、独立部署、独立调用，
`brickkit` CLI 负责把积木拉来、解析依赖、排启动顺序、生成部署文件、交给 Docker
或 Kubernetes 跑起来。平台刻意极简：**没有注册中心、没有常驻服务、没有配置中心、
没有网关**——别去找它们，也别建议加上，那是被明确拒绝过的设计。

## 两个 yaml

| 文件 | 是什么 | 谁写 |
| --- | --- | --- |
| `brickkit.yaml` | 项目配置：装了哪些组件、启停、资源绑定、部署目标 | 项目管理员 |
| `component.yaml` | 组件 Manifest：依赖、端口、配置项、迁移 | 组件作者 |

**要知道这个项目当前装了什么，读 `brickkit.yaml`**——它就在旁边，比任何摘要都准。

## 四条铁律

1. **版本必须精确。** `1.2.0` 可以，`^1.2` / `~1.2` / `latest` 一律不行。
   范围版本是被论证过后拒绝的，不是还没做。
2. **不许碰保留变量。** 这些环境变量由平台注入，组件的 `configSchema` 里
   起同名配置项会被忽略：`COMPONENT_ID`、`COMPONENT_VERSION`、
   任何以 `_ENDPOINT` 结尾的、以及 `DATABASE_` / `REDIS_` / `MQ_` /
   `STORAGE_` / `SEARCH_` / `SMTP_` 开头的。
3. **健康检查有禁令。** 别把依赖的可用性写进自己的健康检查——那会让一个组件的
   抖动级联成整片不健康。冷启动超过 30 秒的组件要写 `startPeriodSeconds`。
4. **启停跟着上层走。** 顶层组件关掉，它下面那一串跟着不启动。想收窄范围就改
   `brickkit.yaml` 的 `enabled`，别去逐个关。

## 别去记参数

**任何命令的参数都去问 `brickkit <命令> --help`。** 这份文件和 `.claude/skills/`
下的技能都刻意不复刻参数清单：复刻一份就是承诺维护两份，而过期的那份会让你
自信地敲出一条 `unknown flag`。

## 更细的东西在哪

`.claude/skills/` 下按任务分了四个技能（拼装 / 写组件 / 部署 / 排障），
Claude Code 会在相关时自动加载。

完整规范在仓库文档：<https://github.com/brickKit/brickKit>
（`AI-CONTEXT.md` 是全站压缩件；`design/` 下 14 本是规范性文档，
试用指南是验证——两者冲突时**以设计书为准**。）
```

- [ ] **Step 5: 写 `internal/skills/assets/CLAUDE.md`**

```markdown
@AGENTS.md
```

<!-- 只有这一行。Claude Code 读 CLAUDE.md 而不读 AGENTS.md，这一行把两边接上。
     内容一律写在 AGENTS.md 里，这里永远只有导入。 -->

- [ ] **Step 6: 跑测试确认通过**

Run: `cd /home/zhijie/Desktop/github/brickKit && go test ./internal/skills/ -v`
Expected: 全部 PASS（5 个测试）

- [ ] **Step 7: 提交**

```bash
git add internal/skills/
git commit -m "skills 资产载体：清单从 embed 遍历得来，不写死名单

写死名单的漏法不报错，只是静默少装一份文件。清单改从 assets/ 遍历，
加文件就自动纳入。

CLAUDE.md 里那一行 @AGENTS.md 有专门的测试守着：Claude Code 读
CLAUDE.md 而不读 AGENTS.md，这一行断了等于它什么导读都没有，
而且不会有任何报错。"
```

---

### Task 2: 四个 SKILL.md 的内容

**Files:**
- Create: `internal/skills/assets/claude/skills/brickkit-assemble/SKILL.md`
- Create: `internal/skills/assets/claude/skills/brickkit-component/SKILL.md`
- Create: `internal/skills/assets/claude/skills/brickkit-deploy/SKILL.md`
- Create: `internal/skills/assets/claude/skills/brickkit-troubleshoot/SKILL.md`
- Test: `internal/skills/skillmd_test.go`

**Interfaces:**
- Consumes: `Assets()`（Task 1）
- Produces: 无新导出符号

**内容怎么写（这一节是本任务的规格，不是建议）**

正文的每一条事实都必须能在 `design/` 里找到出处；**不得出现任何在
`brickkit <命令> --help` 里查不到的参数**——Task 7 的 CI 检查会抓这个。
写之前先读引用的小节，不要凭印象写。

四个技能的 frontmatter **照抄下面这四段**，一字不改（description 是加载开关，
措辞已按「什么时候用」而非「这是什么」调过）：

`brickkit-assemble/SKILL.md`：
```yaml
---
name: brickkit-assemble
description: 在 BrickKit 项目里增删组件、调整启停、启动或停止整套服务、查看运行状态时使用。覆盖 add / remove / fetch / sync / up / down / status 的适用场景，依赖解析与启动顺序怎么算，以及「启停跟着上层走」的判定规则。当用户提到 brickkit.yaml 的 components / enabled 字段，或者问「怎么把某个组件加进来 / 关掉 / 为什么它没起来」时，这个技能适用。
---
```

`brickkit-component/SKILL.md`：
```yaml
---
name: brickkit-component
description: 新写一个 BrickKit 组件、修改 component.yaml、加数据库迁移、或让组件对外提供 gRPC / HTTP 接口时使用。含任何组件都必须满足的硬性契约、平台保留变量的禁区、健康检查禁令与启动宽限期、以及依赖声明的规则。当用户说「写一个组件」「组件起不来该怎么声明」或在编辑 component.yaml 时，这个技能适用。
---
```

`brickkit-deploy/SKILL.md`：
```yaml
---
name: brickkit-deploy
description: 把 BrickKit 项目部署到 Docker 或 Kubernetes、绑定数据库/缓存/消息队列等基础资源、配置密钥、对外暴露服务时使用。含两条部署路径的差别、地址注入的格式、资源绑定怎么声明、以及生产环境的密钥处理。当用户提到 deploy / target / k8s / compose / ingress / 资源绑定，或问「怎么上线」时，这个技能适用。
---
```

`brickkit-troubleshoot/SKILL.md`：
```yaml
---
name: brickkit-troubleshoot
description: brickkit 命令报错、组件起不来、地址注入不生效、依赖解析失败、或需要按错误码定位问题时使用。含错误码到处理动作的映射、常见故障的排查顺序、以及哪些「看起来像 bug」其实是被刻意设计成这样的行为。当用户贴出 brickkit 的报错输出、或说「为什么起不来 / 连不上 / 找不到」时，这个技能适用。
---
```

正文章节骨架（四个技能同构，便于阅读也便于审查）：

1. `## 什么时候用这个技能` —— 三到五个具体场景，一行一个
2. `## 你会猜错的地方` —— 本技能覆盖范围内那些反直觉的规则，**这是技能的核心价值**
3. `## 典型流程` —— 按步骤写，命令只写命令名与必需参数，可选参数指向 `--help`
4. `## 去哪查更细的` —— 指向 `design/` 具体小节与在线文档

每个技能正文的出处与必须覆盖的事实：

| 技能 | 读这些小节 | 正文必须覆盖 |
| --- | --- | --- |
| assemble | `design/004` §3.3/§3.4/§3.8/§3.9、`design/003` §4.3、`design/011` | 精确版本；一个组件 ID 在 dependencies 里只能出现一次；启停跟着上层走；`sync` 只动目录不碰容器 |
| component | `design/002` 全篇、`design/009`、`design/附录合集` 附录 B/C | 保留变量的完整模式；健康检查禁令；`startPeriodSeconds` 与 30 秒阈值；迁移状态表；apiVersion `brickkit/v1` |
| deploy | `design/005`、`design/006`、`design/003` §3 | `docker` 与 `k8s` 两个部署目标；本地与生产同一套地址格式，组件代码零修改；六类资源怎么绑；密钥不入库 |
| troubleshoot | `internal/clierr/clierr.go` 的错误码、`design/011` 常见报错一节、`design/012` | 高频错误码到动作的映射；哪些行为是刻意设计（如弱依赖不注入空字符串） |

- [ ] **Step 1: 写失败的测试**

`internal/skills/skillmd_test.go`：

```go
package skills

import (
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// specFields 是 Agent Skills 规范允许的六个 frontmatter 字段。
// 只用这六个是为了跨工具可移植：规范外的键在别的分发路径上会报
// Unexpected key(s) in SKILL.md frontmatter。
var specFields = map[string]bool{
	"name": true, "description": true, "allowed-tools": true,
	"metadata": true, "license": true, "compatibility": true,
}

// descriptionMax 是硬上限。skill listing 在 1536 字符处截断（description 与
// when_to_use 合计），留出余量按 1024 判。
const descriptionMax = 1024

func skillAssets() []Asset {
	var out []Asset
	for _, a := range Assets() {
		if path.Base(a.Target) == "SKILL.md" {
			out = append(out, a)
		}
	}
	return out
}

func TestSkillCount(t *testing.T) {
	assert.Len(t, skillAssets(), 4, "四个技能，多一个少一个都要先改设计")
}

func TestSkillFrontmatter(t *testing.T) {
	for _, a := range skillAssets() {
		t.Run(a.Target, func(t *testing.T) {
			b, err := a.Content()
			require.NoError(t, err)
			text := string(b)

			// frontmatter 只在开头的 --- 位于文件第一行时才被解析。
			// 差一个空行，整个文件连 --- 都被当成正文，而且不报错。
			require.True(t, strings.HasPrefix(text, "---\n"),
				"frontmatter 必须从第一行开始")

			_, rest, ok := strings.Cut(text[4:], "\n---\n")
			require.True(t, ok, "frontmatter 没有闭合的 ---")
			front := text[4 : len(text)-len(rest)-5]

			var fm map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(front), &fm))

			for k := range fm {
				assert.True(t, specFields[k],
					"frontmatter 用了规范外的字段：%s", k)
			}

			name, _ := fm["name"].(string)
			desc, _ := fm["description"].(string)

			// name 在项目级 skill 里只是显示标签，调用名来自目录名。
			// 两者不一致会让 /brickkit-x 和列表里显示的名字对不上。
			assert.Equal(t, path.Base(path.Dir(a.Target)), name,
				"name 必须与目录名一致")

			require.NotEmpty(t, desc, "description 是加载开关，不能空")
			assert.LessOrEqual(t, len(desc), descriptionMax,
				"description 超出硬上限 %d", descriptionMax)

			assert.NotEmpty(t, strings.TrimSpace(rest), "正文不能空")
		})
	}
}

// 技能正文必须真的写了那些「AI 会猜错」的事实。
// 这是内容层唯一能机械校验的部分：漏掉一条，技能就退化成了目录。
func TestSkillCoversFactsThatGetGuessedWrong(t *testing.T) {
	required := map[string][]string{
		".claude/skills/brickkit-assemble/SKILL.md": {
			"跟着上层走", "enabled",
		},
		".claude/skills/brickkit-component/SKILL.md": {
			"COMPONENT_ID", "_ENDPOINT", "DATABASE_", "REDIS_", "MQ_",
			"STORAGE_", "SEARCH_", "SMTP_",
			"startPeriodSeconds", "brickkit/v1",
		},
		".claude/skills/brickkit-deploy/SKILL.md": {
			"docker", "k8s",
		},
		".claude/skills/brickkit-troubleshoot/SKILL.md": {
			"DEPENDENCY_MISSING", "RESOURCE_UNBOUND",
		},
	}
	byTarget := map[string]Asset{}
	for _, a := range skillAssets() {
		byTarget[a.Target] = a
	}
	for target, facts := range required {
		a, ok := byTarget[target]
		require.True(t, ok, "找不到技能：%s", target)
		b, err := a.Content()
		require.NoError(t, err)
		for _, f := range facts {
			assert.Contains(t, string(b), f,
				"%s 漏了必须覆盖的事实：%s", target, f)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认它失败**

Run: `cd /home/zhijie/Desktop/github/brickKit && go test ./internal/skills/ -run Skill -v`
Expected: FAIL —— `TestSkillCount` 报 0 != 4

- [ ] **Step 3: 写四份 SKILL.md**

frontmatter 照抄本任务上方那四段。正文按上方的章节骨架与出处表写，
写之前先读引用的 design 小节。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd /home/zhijie/Desktop/github/brickKit && go test ./internal/skills/ -v`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/skills/assets/claude/
git commit -m "四个技能只写「AI 会猜错的东西」，参数一律指向 --help

保留变量、健康检查禁令、startPeriodSeconds 阈值、跟着上层走——这些
没一条能靠常识推出来。参数清单反过来：--help 永不过期，复刻一份就是
承诺维护两份，而过期的那份会让人自信地敲出 unknown flag。

测试里钉了两件事：frontmatter 必须从第一行开始（差一个空行，整个文件
连 --- 都被当正文，且不报错），以及那批事实必须真的出现在正文里——
漏掉就退化成了目录。"
```

---

### Task 3: skills.lock 读写

**Files:**
- Create: `internal/skills/lock.go`
- Modify: `internal/config/layout.go`
- Test: `internal/skills/lock_test.go`
- Test: `internal/config/layout_test.go`（已存在则追加）

**Interfaces:**
- Consumes: `Sum`（Task 1）
- Produces:
  - `type LockEntry struct{ Path, Version, Sum string }`
  - `type Lock struct{ Entries []LockEntry }`
  - `func LoadLock(path string) (*Lock, error)`（文件不存在返回空 Lock 且 err == nil）
  - `func (l *Lock) Get(target string) (LockEntry, bool)`
  - `func (l *Lock) Set(e LockEntry)`
  - `func (l *Lock) Save(path string) error`
  - `config.FileSkillsLock`、`func (l config.Layout) SkillsLockPath() string`

- [ ] **Step 1: 写失败的测试**

`internal/skills/lock_test.go`：

```go
package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadLockMissingFileIsEmptyNotError(t *testing.T) {
	// 没有 lock 是常态（老项目、刚 clone），不是错误。
	l, err := LoadLock(filepath.Join(t.TempDir(), "skills.lock"))
	require.NoError(t, err)
	assert.Empty(t, l.Entries)
}

func TestLockRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "skills.lock")
	l := &Lock{}
	l.Set(LockEntry{Path: "AGENTS.md", Version: "0.1.0", Sum: Sum([]byte("a"))})
	require.NoError(t, l.Save(p))

	got, err := LoadLock(p)
	require.NoError(t, err)
	e, ok := got.Get("AGENTS.md")
	require.True(t, ok)
	assert.Equal(t, "0.1.0", e.Version)
	assert.Equal(t, Sum([]byte("a")), e.Sum)
}

func TestLockSetReplacesInsteadOfAppending(t *testing.T) {
	l := &Lock{}
	l.Set(LockEntry{Path: "AGENTS.md", Version: "0.1.0", Sum: "sha256:aa"})
	l.Set(LockEntry{Path: "AGENTS.md", Version: "0.2.0", Sum: "sha256:bb"})
	require.Len(t, l.Entries, 1, "同一路径不能留两条记录")
	e, _ := l.Get("AGENTS.md")
	assert.Equal(t, "0.2.0", e.Version)
}

// lock 是要提交进 Git 的：顺序不稳定会让每次 update 都产生假 diff。
func TestLockSaveIsSortedAndStable(t *testing.T) {
	p := filepath.Join(t.TempDir(), "skills.lock")
	l := &Lock{}
	l.Set(LockEntry{Path: "z.md", Version: "1", Sum: "sha256:z"})
	l.Set(LockEntry{Path: "a.md", Version: "1", Sum: "sha256:a"})
	require.NoError(t, l.Save(p))
	first, err := os.ReadFile(p)
	require.NoError(t, err)

	assert.Less(t, indexOf(string(first), "a.md"), indexOf(string(first), "z.md"),
		"条目必须按路径排序")

	// 反序再存一次，字节应当完全一致。
	l2 := &Lock{}
	l2.Set(LockEntry{Path: "a.md", Version: "1", Sum: "sha256:a"})
	l2.Set(LockEntry{Path: "z.md", Version: "1", Sum: "sha256:z"})
	p2 := filepath.Join(t.TempDir(), "skills.lock")
	require.NoError(t, l2.Save(p2))
	second, err := os.ReadFile(p2)
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second))
}

func TestLoadLockCorruptFileReportsClearly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "skills.lock")
	require.NoError(t, os.WriteFile(p, []byte("{ 这不是 json"), 0o644))
	_, err := LoadLock(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "skills.lock")
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

`internal/config/layout_test.go` 追加：

```go
func TestSkillsLockPath(t *testing.T) {
	l := NewLayout("/proj", DefaultConfigFile)
	assert.Equal(t, filepath.Join("/proj", DirBrickkit, FileSkillsLock),
		l.SkillsLockPath())
}
```

- [ ] **Step 2: 跑测试确认它失败**

Run: `cd /home/zhijie/Desktop/github/brickKit && go test ./internal/skills/ ./internal/config/ -run 'Lock|SkillsLock' -v`
Expected: 编译失败——`undefined: LoadLock`、`undefined: FileSkillsLock`

- [ ] **Step 3: 写 `internal/skills/lock.go`**

```go
package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// LockEntry 是一份托管文件的记录。
type LockEntry struct {
	// Path 是项目内相对路径（与 Asset.Target 一致）。
	Path string `json:"path"`
	// Version 是写入时的 CLI 版本。
	Version string `json:"version"`
	// Sum 是写入时的内容指纹（sha256:...）。
	Sum string `json:"sum"`
}

// Lock 是 .brickkit/skills.lock 的内容。
//
// 它只回答一个问题：**这个文件上次是我们写的、内容是什么样**。
// 有了它才能区分「用户手改过」和「CLI 升级导致过期」——前者绝不能覆盖。
type Lock struct {
	Entries []LockEntry `json:"entries"`
}

// LoadLock 读取 lock。文件不存在时返回空 Lock 且不报错：
// 没有 lock 是常态（老项目、刚 clone、用户删过），不是故障。
func LoadLock(path string) (*Lock, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Lock{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 skills.lock 失败：%w", err)
	}
	var l Lock
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("skills.lock 解析失败：%w", err)
	}
	return &l, nil
}

// Get 按项目内相对路径取记录。
func (l *Lock) Get(target string) (LockEntry, bool) {
	for _, e := range l.Entries {
		if e.Path == target {
			return e, true
		}
	}
	return LockEntry{}, false
}

// Set 写入或替换一条记录。
func (l *Lock) Set(e LockEntry) {
	for i := range l.Entries {
		if l.Entries[i].Path == e.Path {
			l.Entries[i] = e
			return
		}
	}
	l.Entries = append(l.Entries, e)
}

// Save 写入 lock。条目按路径排序、结尾带换行：
// 这个文件要提交进 Git，顺序不稳定会让每次 update 都产生一堆假 diff。
func (l *Lock) Save(path string) error {
	sort.Slice(l.Entries, func(i, j int) bool {
		return l.Entries[i].Path < l.Entries[j].Path
	})
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 skills.lock 失败：%w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建 skills.lock 所在目录失败：%w", err)
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
```

- [ ] **Step 4: 改 `internal/config/layout.go`**

在 `FileGitignore` 后面加常量：

```go
	// FileSkillsLock 是 AI 助手技能的托管清单（brickkit skills 管理）。
	FileSkillsLock = "skills.lock"
```

在 `GitignorePath()` 附近加方法（**形状必须是 `l.path(DirBrickkit, ...)`**，
`scripts/check-doc-tree.py` 靠这个形状推导 `.brickkit/` 下的合法项）：

```go
// SkillsLockPath 返回 AI 助手技能托管清单的路径。
func (l Layout) SkillsLockPath() string { return l.path(DirBrickkit, FileSkillsLock) }
```

- [ ] **Step 5: 跑测试确认通过**

Run: `cd /home/zhijie/Desktop/github/brickKit && go test ./internal/skills/ ./internal/config/ -v`
Expected: 全部 PASS

- [ ] **Step 6: 提交**

```bash
git add internal/skills/lock.go internal/skills/lock_test.go internal/config/layout.go internal/config/layout_test.go
git commit -m "skills.lock：它只回答「上次是我们写的、长什么样」

有了这个才能区分「用户手改过」与「CLI 升级导致过期」——前者绝不能覆盖。
文件不存在不算错误：老项目、刚 clone、用户删过，都是常态。

条目按路径排序后再写：这文件要进 Git，顺序不稳定会让每次 update
都产生一堆假 diff。SkillsLockPath 刻意写成 l.path(DirBrickkit, ...)
的形状——check-doc-tree.py 靠这个形状自动认出 .brickkit/ 下的合法项。"
```

---

### Task 4: 五态判定与写入

**Files:**
- Create: `internal/skills/install.go`
- Test: `internal/skills/install_test.go`

**Interfaces:**
- Consumes: `Assets`、`Sum`、`Lock`、`LoadLock`（Task 1、3）
- Produces:
  - `type State string` 与 `StateMissing / StateCurrent / StateOutdated / StateModified / StateUntracked`
  - `type FileStatus struct{ Target string; State State; FromVersion string }`
  - `type Installer struct{ Root, LockPath, Version string }`
  - `func (in Installer) Status() ([]FileStatus, error)`
  - `type ApplyResult struct{ Written []string; Skipped []FileStatus }`
  - `func (in Installer) Apply() (*ApplyResult, error)`

判定顺序（**顺序本身是规格**）：

```
文件不存在                       → 缺失     （写）
磁盘指纹 == 当前资产指纹          → 最新     （不写，但补登记 lock）
lock 里没有这条                  → 未托管   （不碰）
磁盘指纹 != lock 记录的指纹       → 已手改   （不碰）
其余                             → 待更新   （写）
```

「最新」放在 lock 查询之前是有意的：一个内容恰好与资产一致的文件，判成「最新」
并补登记，比判成「未托管」永远跳过要好——否则它下个版本还是「未托管」，
永远升不上去。

- [ ] **Step 1: 写失败的测试**

`internal/skills/install_test.go`：

```go
package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newInstaller(t *testing.T) Installer {
	t.Helper()
	root := t.TempDir()
	return Installer{
		Root:     root,
		LockPath: filepath.Join(root, ".brickkit", "skills.lock"),
		Version:  "0.1.0",
	}
}

func stateOf(t *testing.T, in Installer, target string) FileStatus {
	t.Helper()
	list, err := in.Status()
	require.NoError(t, err)
	for _, s := range list {
		if s.Target == target {
			return s
		}
	}
	t.Fatalf("状态列表里没有 %s", target)
	return FileStatus{}
}

func TestFreshProjectIsAllMissingThenWritten(t *testing.T) {
	in := newInstaller(t)
	for _, s := range mustStatus(t, in) {
		assert.Equal(t, StateMissing, s.State, s.Target)
	}

	res, err := in.Apply()
	require.NoError(t, err)
	assert.Len(t, res.Written, len(Assets()))
	assert.Empty(t, res.Skipped)

	for _, a := range Assets() {
		_, err := os.Stat(filepath.Join(in.Root, a.Target))
		assert.NoError(t, err, "没写出来：%s", a.Target)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	in := newInstaller(t)
	_, err := in.Apply()
	require.NoError(t, err)

	res, err := in.Apply()
	require.NoError(t, err)
	assert.Empty(t, res.Written, "第二次不该有任何写入")
	assert.Empty(t, res.Skipped)
	for _, s := range mustStatus(t, in) {
		assert.Equal(t, StateCurrent, s.State, s.Target)
	}
}

// 用户手改过的文件绝不覆盖——这是整个设计里最要紧的一条。
func TestModifiedFileIsNeverOverwritten(t *testing.T) {
	in := newInstaller(t)
	_, err := in.Apply()
	require.NoError(t, err)

	p := filepath.Join(in.Root, "AGENTS.md")
	mine := []byte("这是我自己写的，别动\n")
	require.NoError(t, os.WriteFile(p, mine, 0o644))

	assert.Equal(t, StateModified, stateOf(t, in, "AGENTS.md").State)

	res, err := in.Apply()
	require.NoError(t, err)
	assert.NotContains(t, res.Written, "AGENTS.md")

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, string(mine), string(after), "手改的内容被覆盖了")
}

// lock 里没有记录的既有文件也不碰：可能是用户自己建的 CLAUDE.md。
func TestUntrackedFileIsNeverOverwritten(t *testing.T) {
	in := newInstaller(t)
	p := filepath.Join(in.Root, "CLAUDE.md")
	mine := []byte("@我自己的规则.md\n")
	require.NoError(t, os.WriteFile(p, mine, 0o644))

	assert.Equal(t, StateUntracked, stateOf(t, in, "CLAUDE.md").State)

	res, err := in.Apply()
	require.NoError(t, err)
	assert.NotContains(t, res.Written, "CLAUDE.md")

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, string(mine), string(after))
}

// lock 记的指纹与磁盘一致、但与当前资产不一致 → 待更新，可以覆盖。
func TestOutdatedFileIsUpdated(t *testing.T) {
	in := newInstaller(t)
	p := filepath.Join(in.Root, "AGENTS.md")
	old := []byte("旧版本的导读\n")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, old, 0o644))

	l := &Lock{}
	l.Set(LockEntry{Path: "AGENTS.md", Version: "0.0.1", Sum: Sum(old)})
	require.NoError(t, l.Save(in.LockPath))

	st := stateOf(t, in, "AGENTS.md")
	assert.Equal(t, StateOutdated, st.State)
	assert.Equal(t, "0.0.1", st.FromVersion, "要能说出是从哪个版本升上来的")

	res, err := in.Apply()
	require.NoError(t, err)
	assert.Contains(t, res.Written, "AGENTS.md")

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.NotEqual(t, string(old), string(after))
}

// 删掉 lock 之后，既有文件全部变「未托管」，一个都不许动。
func TestLostLockMakesEverythingUntracked(t *testing.T) {
	in := newInstaller(t)
	_, err := in.Apply()
	require.NoError(t, err)
	require.NoError(t, os.Remove(in.LockPath))

	for _, s := range mustStatus(t, in) {
		assert.Equal(t, StateUntracked, s.State, s.Target)
	}
	res, err := in.Apply()
	require.NoError(t, err)
	assert.Empty(t, res.Written)
}

// 内容恰好与资产一致、但 lock 里没记录 → 判「最新」并补登记，
// 否则它下个版本还是「未托管」，永远升不上去。
func TestCurrentWithoutLockEntryGetsRecorded(t *testing.T) {
	in := newInstaller(t)
	var agents Asset
	for _, a := range Assets() {
		if a.Target == "AGENTS.md" {
			agents = a
		}
	}
	want, err := agents.Content()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(in.Root, "AGENTS.md"), want, 0o644))

	assert.Equal(t, StateCurrent, stateOf(t, in, "AGENTS.md").State)

	_, err = in.Apply()
	require.NoError(t, err)

	l, err := LoadLock(in.LockPath)
	require.NoError(t, err)
	e, ok := l.Get("AGENTS.md")
	require.True(t, ok, "「最新」也要补登记进 lock")
	assert.Equal(t, "0.1.0", e.Version)
}

// 缺失的文件被重新写出来（用户删了某个 skill 目录）。
func TestMissingFileIsRestored(t *testing.T) {
	in := newInstaller(t)
	_, err := in.Apply()
	require.NoError(t, err)

	p := filepath.Join(in.Root, "AGENTS.md")
	require.NoError(t, os.Remove(p))
	assert.Equal(t, StateMissing, stateOf(t, in, "AGENTS.md").State)

	res, err := in.Apply()
	require.NoError(t, err)
	assert.Contains(t, res.Written, "AGENTS.md")
}

func mustStatus(t *testing.T, in Installer) []FileStatus {
	t.Helper()
	list, err := in.Status()
	require.NoError(t, err)
	require.NotEmpty(t, list)
	return list
}
```

- [ ] **Step 2: 跑测试确认它失败**

Run: `cd /home/zhijie/Desktop/github/brickKit && go test ./internal/skills/ -run 'Installer|Fresh|Apply|Modified|Untracked|Outdated|Lost|Current|Missing' -v`
Expected: 编译失败——`undefined: Installer`

- [ ] **Step 3: 写 `internal/skills/install.go`**

```go
package skills

import (
	"fmt"
	"os"
	"path/filepath"
)

// State 是一份托管文件的当前状态。
type State string

const (
	// StateMissing 文件不存在。
	StateMissing State = "缺失"
	// StateCurrent 内容已与当前版本的资产一致。
	StateCurrent State = "最新"
	// StateOutdated 内容是我们上次写的，但资产已经变了。
	StateOutdated State = "待更新"
	// StateModified 内容与我们上次写的不一致——用户改过。
	StateModified State = "已手改"
	// StateUntracked 文件存在但 lock 里没有记录。
	StateUntracked State = "未托管"
)

// writable 判断这个状态下是否允许写入。
// 只有这两种状态可写，其余一律不动——尤其是「已手改」与「未托管」。
func (s State) writable() bool {
	return s == StateMissing || s == StateOutdated
}

// FileStatus 是一份托管文件的状态。
type FileStatus struct {
	// Target 是项目内相对路径。
	Target string
	// State 是当前状态。
	State State
	// FromVersion 仅在 StateOutdated 时有值：lock 里记的旧版本。
	FromVersion string
}

// Installer 把内嵌资产装进一个项目。
type Installer struct {
	// Root 是项目根目录。
	Root string
	// LockPath 是 skills.lock 的路径。
	LockPath string
	// Version 是当前 CLI 版本，写进 lock。
	Version string
}

// Status 计算全部资产的当前状态。只读，不写任何文件。
func (in Installer) Status() ([]FileStatus, error) {
	lock, err := LoadLock(in.LockPath)
	if err != nil {
		return nil, err
	}
	var out []FileStatus
	for _, a := range Assets() {
		st, err := in.stateOf(a, lock)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// stateOf 判定单个资产的状态。
//
// 判定顺序本身是规格：
//
//	文件不存在                → 缺失
//	磁盘指纹 == 资产指纹       → 最新
//	lock 里没有这条            → 未托管
//	磁盘指纹 != lock 记录      → 已手改
//	其余                      → 待更新
//
// 「最新」刻意排在 lock 查询之前：一个内容恰好与资产一致的文件，判成「最新」
// 并补登记，比判成「未托管」永远跳过要好——否则它下个版本还是「未托管」。
func (in Installer) stateOf(a Asset, lock *Lock) (FileStatus, error) {
	st := FileStatus{Target: a.Target}

	want, err := a.Content()
	if err != nil {
		return st, fmt.Errorf("读取内嵌资产 %s 失败：%w", a.Source, err)
	}
	disk, err := os.ReadFile(filepath.Join(in.Root, a.Target))
	if os.IsNotExist(err) {
		st.State = StateMissing
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("读取 %s 失败：%w", a.Target, err)
	}

	if Sum(disk) == Sum(want) {
		st.State = StateCurrent
		return st, nil
	}
	entry, ok := lock.Get(a.Target)
	if !ok {
		st.State = StateUntracked
		return st, nil
	}
	if entry.Sum != Sum(disk) {
		st.State = StateModified
		return st, nil
	}
	st.State = StateOutdated
	st.FromVersion = entry.Version
	return st, nil
}

// ApplyResult 是一次 Apply 的结果。
type ApplyResult struct {
	// Written 是本次写入的项目内相对路径。
	Written []string
	// Skipped 是刻意没碰的文件（已手改 / 未托管）。
	Skipped []FileStatus
}

// Apply 按状态写入资产：缺失与待更新写入，已手改与未托管跳过，最新只补登记 lock。
//
// 全程不删任何文件，也不动跳过的那些在 lock 里的记录。
func (in Installer) Apply() (*ApplyResult, error) {
	lock, err := LoadLock(in.LockPath)
	if err != nil {
		return nil, err
	}
	res := &ApplyResult{}
	for _, a := range Assets() {
		st, err := in.stateOf(a, lock)
		if err != nil {
			return nil, err
		}
		switch {
		case st.State.writable():
			content, err := a.Content()
			if err != nil {
				return nil, fmt.Errorf("读取内嵌资产 %s 失败：%w", a.Source, err)
			}
			if err := in.write(a.Target, content); err != nil {
				return nil, err
			}
			lock.Set(LockEntry{Path: a.Target, Version: in.Version, Sum: Sum(content)})
			res.Written = append(res.Written, a.Target)
		case st.State == StateCurrent:
			content, err := a.Content()
			if err != nil {
				return nil, fmt.Errorf("读取内嵌资产 %s 失败：%w", a.Source, err)
			}
			lock.Set(LockEntry{Path: a.Target, Version: in.Version, Sum: Sum(content)})
		default:
			res.Skipped = append(res.Skipped, st)
		}
	}
	if err := lock.Save(in.LockPath); err != nil {
		return nil, err
	}
	return res, nil
}

func (in Installer) write(target string, content []byte) error {
	path := filepath.Join(in.Root, target)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建目录 %s 失败：%w", filepath.Dir(target), err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("写入 %s 失败：%w", target, err)
	}
	return nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd /home/zhijie/Desktop/github/brickKit && go test ./internal/skills/ -v`
Expected: 全部 PASS

- [ ] **Step 5: 查覆盖率**

Run: `cd /home/zhijie/Desktop/github/brickKit && go test ./internal/skills/ -cover`
Expected: 该包覆盖率 ≥ 92%。低于门槛就补测试，别改门槛。

- [ ] **Step 6: 提交**

```bash
git add internal/skills/install.go internal/skills/install_test.go
git commit -m "五态判定：只有「缺失」和「待更新」可写，另外三种一律不碰

判定顺序本身是规格。「最新」刻意排在 lock 查询之前——一个内容恰好与
资产一致的文件，判「最新」并补登记，比判「未托管」永远跳过要好，
否则它下个版本还是「未托管」，永远升不上去。

删掉 lock 的项目里所有文件都变「未托管」，一个都不动。这是保守的一侧：
判不准就不碰，与 init 的 O_EXCL 是同一种态度。"
```

---

### Task 5: init 接线与 --no-skills

**Files:**
- Modify: `internal/cli/init.go`
- Test: `internal/cli/init_test.go`（追加）

**Interfaces:**
- Consumes: `skills.Installer`、`config.Layout.SkillsLockPath()`（Task 3、4）
- Produces: 无新导出符号

- [ ] **Step 1: 写失败的测试**

`internal/cli/init_test.go` 追加：

```go
func TestInitInstallsSkills(t *testing.T) {
	dir := t.TempDir()
	r := runIn(t, dir, "init", "my-project")
	require.Equal(t, 0, r.code, r.stderr)

	for _, rel := range []string{
		"AGENTS.md",
		"CLAUDE.md",
		filepath.Join(".claude", "skills", "brickkit-assemble", "SKILL.md"),
		filepath.Join(".brickkit", "skills.lock"),
	} {
		_, err := os.Stat(filepath.Join(dir, rel))
		assert.NoError(t, err, "init 没装出 %s", rel)
	}
	assert.Contains(t, r.stdout, ".claude/skills/")
}

func TestInitNoSkillsInstallsNothing(t *testing.T) {
	dir := t.TempDir()
	r := runIn(t, dir, "init", "my-project", "--no-skills")
	require.Equal(t, 0, r.code, r.stderr)

	for _, rel := range []string{"AGENTS.md", "CLAUDE.md", ".claude",
		filepath.Join(".brickkit", "skills.lock")} {
		_, err := os.Stat(filepath.Join(dir, rel))
		assert.True(t, os.IsNotExist(err), "--no-skills 却产生了 %s", rel)
	}
	assert.NotContains(t, r.stdout, ".claude/skills/")
}

// 技能文件是要提交进 Git 的（团队共享），所以 .gitignore 里绝不能出现它们。
// 这条没人守着：下一个人「顺手」把 .claude/ 加进忽略规则，共享就静默失效了。
func TestInitDoesNotIgnoreSkillFiles(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "my-project").code)

	b, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	for _, forbidden := range []string{".claude", "AGENTS.md", "CLAUDE.md", "skills.lock"} {
		assert.NotContains(t, string(b), forbidden,
			".gitignore 忽略了技能文件，团队共享会静默失效")
	}
}

// 用户已有的 CLAUDE.md 是他项目里最要紧的一份指令文件，绝不能被动。
func TestInitKeepsExistingClaudeMd(t *testing.T) {
	dir := t.TempDir()
	mine := "# 我自己的规则\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(mine), 0o644))

	r := runIn(t, dir, "init", "my-project")
	require.Equal(t, 0, r.code, r.stderr)

	after, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	require.NoError(t, err)
	assert.Equal(t, mine, string(after), "已有的 CLAUDE.md 被改了")
	assert.Contains(t, r.stdout, "CLAUDE.md", "跳过了要说出来")
	assert.Contains(t, r.stdout, "@AGENTS.md", "要告诉用户自己加哪一行")
}

```

- [ ] **Step 2: 跑测试确认它失败**

Run: `cd /home/zhijie/Desktop/github/brickKit && go test ./internal/cli/ -run 'InitInstallsSkills|NoSkills|ExistingClaudeMd|DoesNotIgnore' -v`
Expected: FAIL —— `unknown flag: --no-skills`，且文件都不存在

- [ ] **Step 3: 改 `internal/cli/init.go`**

命令定义里加 flag 与说明：

```go
	var noSkills bool
	cmd := &cobra.Command{
		// ... Use / Short / GroupID 不变
		Long: `在当前目录初始化一个 BrickKit 项目。

行为（004 §3.2）：
  1. 创建项目目录结构（.brickkit/、components/）
  2. 生成 brickkit.yaml 骨架
  3. 追加 .gitignore 规则（003 §11）
  4. 装入 AI 助手技能（.claude/skills/、AGENTS.md、CLAUDE.md）

项目名称必须显式指定：只能包含小写字母、数字与中划线，
且以字母或数字开头结尾（用于 K8s namespace 与 Docker Network 命名）。

装入的技能只写「照常识会猜错」的东西——保留变量、健康检查禁令、
启停跟着上层走——参数一律不复刻，指向 --help。它们跟着项目提交，
团队共享；CLI 升级后用 brickkit skills update 刷新。
不想要就加 --no-skills。`,
		Example: `  brickkit init my-project
  brickkit init my-project --config brickkit.prod.yaml   初始化指定环境的配置
  brickkit init my-project --no-skills                   不装 AI 助手技能`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return clierr.New(clierr.CodeInvalidArgument, "请指定项目名称：brickkit init <项目名称>").
					WithExit(clierr.ExitUsage)
			}
			return runInit(opts, args[0], noSkills)
		},
	}
	cmd.Flags().BoolVar(&noSkills, "no-skills", false,
		"不装 AI 助手技能（.claude/skills/、AGENTS.md、CLAUDE.md）")
	return cmd
```

`runInit` 在打印「下一步」之前插入技能安装：

```go
func runInit(opts *Options, project string, noSkills bool) error {
	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)

	result, err := config.InitProject(layout, project)
	if err != nil {
		return err
	}

	logging.Info("项目已初始化",
		"project", result.ProjectName,
		"config", layout.ConfigPath(),
		"gitignore_updated", result.GitignoreUpdated,
	)

	// 输出格式与 004 §3.2 / 009 / 011 中的样例逐字一致。
	opts.Printf("✅ 项目已初始化：%s\n", result.ProjectName)
	opts.Printf("   📁 %-21s%s\n", result.ConfigName, "项目配置")
	// components/ 一直都在建，只是从前没说过；现在它还是默认安装源，更该点出来
	opts.Printf("   📁 %-21s%s\n", config.DirComponents+"/", "组件源码（已配为本地安装源 local-dev）")
	opts.Printf("   📁 %-21s%s\n", config.DirBrickkit+"/", "CLI 工作目录")

	if !noSkills {
		if err := installSkills(opts, layout); err != nil {
			return err
		}
	}

	opts.Printf("\n")
	opts.Printf("下一步：\n")
	opts.Printf("  brickkit add --local               把 components/ 下的组件全加进来\n")
	opts.Printf("  brickkit add people/basic@1.0.0    从安装源添加组件\n")
	opts.Printf("  brickkit up                        一键启动\n")
	return nil
}

// installSkills 装入 AI 助手技能，并把跳过的文件说清楚。
//
// 装不上是**错误**而不是静默跳过：init 说了它会装，那就得装上或者说明为什么没装。
// 但错误里要讲明项目本身已经建好了——否则人会以为整个 init 都白跑了。
func installSkills(opts *Options, layout config.Layout) error {
	in := skills.Installer{
		Root:     layout.Root,
		LockPath: layout.SkillsLockPath(),
		Version:  version.Version,
	}
	res, err := in.Apply()
	if err != nil {
		return clierr.Newf(clierr.CodeInternal, "错误：AI 助手技能装入失败").
			WithDetail("原因", err.Error()).
			WithHint(
				"项目本身已经初始化完成，只是技能没装上",
				"修好权限后执行 brickkit skills update 补装",
				"也可以不要技能：它不影响 brickkit 的任何功能",
			).
			WithCause(err)
	}

	if len(res.Written) > 0 {
		opts.Printf("   📁 %-21s%s\n", ".claude/skills/", "AI 助手技能（4 个）")
		opts.Printf("   📁 %-21s%s\n", "AGENTS.md", "AI 助手项目导读")
	}
	for _, s := range res.Skipped {
		opts.Printf("   ⏭  %-21s%s\n", s.Target, "已存在，未改动（"+string(s.State)+"）")
		if s.Target == "CLAUDE.md" {
			opts.Printf("      %s\n", "想让 Claude Code 也读到导读，在它开头加一行：@AGENTS.md")
		}
	}
	logging.Info("AI 助手技能已装入",
		"written", len(res.Written), "skipped", len(res.Skipped))
	return nil
}
```

import 补 `"github.com/brickkit/brickkit/internal/skills"` 与
`"github.com/brickkit/brickkit/internal/version"`。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd /home/zhijie/Desktop/github/brickKit && go test ./internal/cli/ -v -run Init`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/cli/init.go internal/cli/init_test.go
git commit -m "init 装技能；已有的 CLAUDE.md 只提示、不追加

用户已有的 CLAUDE.md 是他项目里最要紧的一份指令文件，往里追加内容
是不能接受的。所以跳过它，并把该加的那行 @AGENTS.md 直接打给他看。

装不上算错误而不是静默跳过——init 说了它会装。但错误文案里要讲明
项目本身已经建好了，否则人会以为整个 init 都白跑了。"
```

---

### Task 6: brickkit skills 命令（连同命令计数文档）

**Files:**
- Create: `internal/cli/skills.go`
- Modify: `internal/cli/root.go`
- Modify: `README.md`、`llms.txt`、`AI-CONTEXT.md`
- Test: `internal/cli/skills_test.go`

**Interfaces:**
- Consumes: `skills.Installer`、`newTable`（`internal/cli/table.go`）
- Produces: `func newSkillsCommand(opts *Options) *cobra.Command`

**为什么文档必须和命令同一个提交：**
`scripts/check-cli-docs.py` 的 `check_command_count` 会把**顶层业务命令数**
（不含 `completion` / `help` / `version`）与所有文档里 `N 个命令` / `N 条命令`
的声明比对。加了 `skills` 之后真实数是 **12**，文档里还写 11 就 `make lint` 红。
`FROZEN_DOCS`（`开发计划.md`、`开发进度/`）豁免，其余都要改。

另外 `undocumented()` 是反向检查：二进制里有、文档里一次没出现的命令与参数会被报出来，
所以 `--no-skills` 也必须在这一步进文档。

`brickkit skills` 是本仓库**第一条带子命令的命令**。`cli_surface` 只枚举顶层命令
（候选来自 `brickkit --help` 的分组列表，再逐个 `probe`），子命令对它不可见——
不会误报，但也不受保护。这一步最后必须真跑一次 `make check-cli-docs` 确认。

- [ ] **Step 1: 写失败的测试**

`internal/cli/skills_test.go`：

```go
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillsStatusOnFreshProject(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "p", "--no-skills").code)

	r := runIn(t, dir, "skills", "status")
	require.Equal(t, 0, r.code, r.stderr)
	assert.Contains(t, r.stdout, "缺失")
	assert.Contains(t, r.stdout, "AGENTS.md")
}

// 不带子命令时等于 status——只读是安全的默认。
func TestSkillsBareIsStatus(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "p", "--no-skills").code)

	r := runIn(t, dir, "skills")
	require.Equal(t, 0, r.code, r.stderr)
	assert.Contains(t, r.stdout, "缺失")

	_, err := os.Stat(filepath.Join(dir, "AGENTS.md"))
	assert.True(t, os.IsNotExist(err), "光看状态不该写文件")
}

func TestSkillsUpdateInstallsThenIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "p", "--no-skills").code)

	r := runIn(t, dir, "skills", "update")
	require.Equal(t, 0, r.code, r.stderr)
	_, err := os.Stat(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)

	again := runIn(t, dir, "skills", "update")
	require.Equal(t, 0, again.code, again.stderr)
	assert.Contains(t, again.stdout, "已是最新")
}

func TestSkillsUpdateSkipsModifiedAndSaysHow(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "p").code)

	p := filepath.Join(dir, "AGENTS.md")
	mine := []byte("我改过了\n")
	require.NoError(t, os.WriteFile(p, mine, 0o644))

	r := runIn(t, dir, "skills", "update")
	require.Equal(t, 0, r.code, r.stderr)
	assert.Contains(t, r.stdout, "已手改")
	assert.Contains(t, r.stdout, "删掉", "要告诉人怎么放弃本地修改")

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, string(mine), string(after))
}

// 未初始化的目录里跑 skills 要说清楚，而不是默默在别人家里建 .claude/。
func TestSkillsRefusesOutsideProject(t *testing.T) {
	r := runIn(t, t.TempDir(), "skills", "update")
	assert.NotEqual(t, 0, r.code)
	assert.Contains(t, r.stderr+r.stdout, "brickkit init")
}
```

- [ ] **Step 2: 跑测试确认它失败**

Run: `cd /home/zhijie/Desktop/github/brickKit && go test ./internal/cli/ -run Skills -v`
Expected: FAIL —— `unknown command "skills"`

- [ ] **Step 3: 写 `internal/cli/skills.go`**

```go
package cli

// 本文件实现 brickkit skills：查看与刷新装进项目的 AI 助手技能。
//
// 它是本仓库第一条带子命令的命令。不带子命令时等于 status——
// 只读是安全的默认，敲错了不会改任何东西。

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/skills"
	"github.com/brickkit/brickkit/internal/version"
)

// newSkillsCommand 实现 brickkit skills。
func newSkillsCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "skills",
		Short:   "查看与刷新装进项目的 AI 助手技能",
		GroupID: groupProject,
		Long: `管理装进本项目的 AI 助手技能（.claude/skills/、AGENTS.md、CLAUDE.md）。

这些文件由 brickkit init 装入，跟着项目提交、团队共享。它们描述的是
**当前这个 CLI 版本**的行为，所以 CLI 升级后需要刷新一次。

  brickkit skills          看每个文件的状态（只读，等同 status）
  brickkit skills status   同上
  brickkit skills update   缺的装上、旧的刷新

**手改过的文件绝不覆盖。** update 会把它们列出来并跳过。想放弃本地
修改，删掉那个文件再执行一次 update——刻意不提供 --force：删文件这个
动作本身已经足够明确，而多一个开关就多一条误伤路径。

lock 文件（.brickkit/skills.lock）丢了的项目，所有文件都会被判成
「未托管」而一律跳过。那是保守的一侧：判不准就不碰。`,
		Example: `  brickkit skills          看装了什么、有没有过期
  brickkit skills update   刷新到当前 CLI 版本`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsStatus(opts)
		},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:     "status",
			Short:   "查看每个技能文件的状态（只读）",
			Args:    cobra.NoArgs,
			Example: `  brickkit skills status`,
			RunE: func(cmd *cobra.Command, args []string) error {
				return runSkillsStatus(opts)
			},
		},
		&cobra.Command{
			Use:     "update",
			Short:   "缺的装上、旧的刷新；手改过的跳过",
			Args:    cobra.NoArgs,
			Example: `  brickkit skills update`,
			RunE: func(cmd *cobra.Command, args []string) error {
				return runSkillsUpdate(opts)
			},
		},
	)
	return cmd
}

// installer 构造 Installer，并先确认这儿真是个 BrickKit 项目。
//
// 不确认的话，在随便一个目录里敲 skills update 会默默建出 .claude/ 与
// AGENTS.md——在别人家里留下文件，比报个错糟糕得多。
func installer(opts *Options) (skills.Installer, error) {
	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	if _, err := os.Stat(layout.ConfigPath()); err != nil {
		return skills.Installer{}, clierr.New(clierr.CodeProjectMissing,
			"错误：当前目录不是 BrickKit 项目").
			WithDetail("找不到", layout.ConfigName()).
			WithHint("先执行 brickkit init <项目名称>",
				"或用 --config 指定配置文件")
	}
	return skills.Installer{
		Root:     layout.Root,
		LockPath: layout.SkillsLockPath(),
		Version:  version.Version,
	}, nil
}

func runSkillsStatus(opts *Options) error {
	in, err := installer(opts)
	if err != nil {
		return err
	}
	list, err := in.Status()
	if err != nil {
		return wrapSkillsError(err)
	}

	t := newTable("文件", "状态")
	stale := 0
	for _, s := range list {
		state := string(s.State)
		switch s.State {
		case skills.StateOutdated:
			state = string(s.State) + "（" + s.FromVersion + " → " + version.Version + "）"
			stale++
		case skills.StateMissing:
			stale++
		case skills.StateModified, skills.StateUntracked:
			state = string(s.State) + "，update 会跳过"
		}
		t.add(s.Target, state)
	}
	opts.Printf("%s", t.render("   "))
	if stale > 0 {
		opts.Printf("\n有 %d 个文件需要刷新：brickkit skills update\n", stale)
	}
	return nil
}

func runSkillsUpdate(opts *Options) error {
	in, err := installer(opts)
	if err != nil {
		return err
	}
	res, err := in.Apply()
	if err != nil {
		return wrapSkillsError(err)
	}

	if len(res.Written) == 0 && len(res.Skipped) == 0 {
		opts.Printf("✅ AI 助手技能已是最新（CLI %s）\n", version.Display())
		return nil
	}

	opts.Printf("✅ AI 助手技能已更新\n")
	if len(res.Written) > 0 {
		opts.Printf("   已写入 %d 个：\n", len(res.Written))
		for _, w := range res.Written {
			opts.Printf("     %s\n", w)
		}
	}
	if len(res.Skipped) > 0 {
		opts.Printf("   已跳过 %d 个：\n", len(res.Skipped))
		for _, s := range res.Skipped {
			opts.Printf("     %s（%s）\n", s.Target, s.State)
		}
		opts.Printf("\n提示：想放弃本地修改，删掉那个文件后重新执行 brickkit skills update\n")
	}
	logging.Info("AI 助手技能已刷新",
		"written", len(res.Written), "skipped", len(res.Skipped))
	return nil
}

func wrapSkillsError(cause error) error {
	return clierr.Newf(clierr.CodeInternal, "错误：读写 AI 助手技能失败").
		WithDetail("原因", cause.Error()).
		WithHint("检查目录权限与磁盘空间").
		WithCause(cause)
}
```

- [ ] **Step 4: 在 `internal/cli/root.go` 注册**

`root.AddCommand(...)` 里，`newInitCommand(opts)` 之后加一行：

```go
		newSkillsCommand(opts),
```

- [ ] **Step 5: 跑测试确认通过**

Run: `cd /home/zhijie/Desktop/github/brickKit && go test ./internal/cli/ -v -run 'Skills|Init'`
Expected: 全部 PASS

- [ ] **Step 6: 改文档里的命令数与新命令（同一提交，否则 lint 红）**

先看真实数字与所有需要改的地方：

```bash
cd /home/zhijie/Desktop/github/brickKit
make build-cli
grep -rn "[0-9]\+ *[个条]命令" --include='*.md' --include='*.txt' . | grep -v '^./开发计划.md' | grep -v '^./开发进度/'
```

逐处把 11 改成 12，并在这三份里补上 `skills` 一行：

- `README.md`：能力概览里那句「11 条命令 + `version`」
- `llms.txt`：`design/004` 那条描述里的命令数
- `AI-CONTEXT.md`：命令一览表加 `skills` 行、命令数改 12

- [ ] **Step 7: 跑文档检查**

Run: `cd /home/zhijie/Desktop/github/brickKit && make check-cli-docs`
Expected: 通过。若报「文档写了不存在的命令」，看是不是把 `brickkit skills update`
的 `update` 当成了顶层命令——那说明 `cli_surface` 的候选正则把子命令也收进来了，
此时应在 Task 7 里一并处理，不要改动 CLI 的命令形状去迁就脚本。

- [ ] **Step 8: 提交**

```bash
git add internal/cli/skills.go internal/cli/skills_test.go internal/cli/root.go README.md llms.txt AI-CONTEXT.md
git commit -m "brickkit skills：不带子命令时等于 status，只读是安全的默认

命令数从 11 变 12，所以文档同一个提交里改——check_command_count 会把
真实顶层命令数与所有文档里的「N 条命令」比对，不同步就 lint 红。

skills 在项目外会报错而不是默默干活：在随便一个目录里敲 update 就建出
.claude/ 和 AGENTS.md，在别人家里留下文件比报个错糟糕得多。

刻意不给 --force。删掉文件再跑一次 update 已经足够明确，多一个开关
就多一条误伤路径。"
```

---

### Task 7: 把技能纳入防说谎检查

**Files:**
- Modify: `scripts/check-cli-docs.py`
- Test: 手动跑 `make check-cli-docs`（脚本自带自检）

**Interfaces:**
- Consumes: Task 2 的四份 SKILL.md
- Produces: 无

技能是**会被照着敲的文档**，所以必须受同一道守卫。`check-cli-docs.py` 已经在为
`*.md` 做这件事，这一步只是把内嵌资产纳入它的 `docs()` 范围。

- [ ] **Step 1: 先确认现在抓不到（造一条假参数）**

```bash
cd /home/zhijie/Desktop/github/brickKit
echo '照着敲：brickkit up --这个参数不存在' >> internal/skills/assets/claude/skills/brickkit-assemble/SKILL.md
make check-cli-docs
```

Expected: **通过**——这正是问题：技能里的假参数现在无人看管。

- [ ] **Step 2: 改 `scripts/check-cli-docs.py` 的 `docs()`**

在 `docs()` 的 glob 模式列表里加上内嵌资产。改完后 `docs()` 的 docstring
补一句说明为什么它们算文档：

```python
    # 内嵌进 CLI 的 AI 助手技能也算文档，而且是**最会被照着敲**的一类：
    # 它们由 brickkit init 直接装进用户项目，读者是 AI 助手——
    # 它不会像人一样怀疑「是不是我装错了版本」，只会自信地把假参数敲下去。
    "internal/skills/assets/**/*.md",
```

- [ ] **Step 3: 确认现在抓得到**

Run: `cd /home/zhijie/Desktop/github/brickKit && make check-cli-docs`
Expected: FAIL，报出 `--这个参数不存在`

- [ ] **Step 4: 撤掉那行假参数，确认恢复通过**

```bash
cd /home/zhijie/Desktop/github/brickKit
git checkout internal/skills/assets/claude/skills/brickkit-assemble/SKILL.md
make check-cli-docs
```

Expected: 通过

- [ ] **Step 5: 跑完整静态检查**

Run: `cd /home/zhijie/Desktop/github/brickKit && make lint`
Expected: 全部通过。`check-doc-tree` 若报 `.brickkit/` 目录树不一致，
说明文档里的目录树还没写 `skills.lock`——那属于 Task 8。

- [ ] **Step 6: 提交**

```bash
git add scripts/check-cli-docs.py
git commit -m "技能纳入防说谎检查：它们是最会被照着敲的一类文档

读者是 AI 助手，它不会像人一样怀疑「是不是我装错了版本」，
只会自信地把假参数敲下去。

改之前先造了一条假参数确认现在抓不到，改完确认抓得到——
一个不会失败的检查等于没有检查。"
```

---

### Task 8: 剩余文档同步

**Files:**
- Modify: `design/004-CLI 设计.md`
- Modify: `design/附录合集.md`
- Modify: `试用指南/01-初始化项目.md`
- Modify: `开发进度/决策索引.md`
- Modify: `AI-CONTEXT.md`（目录树与导航）

- [ ] **Step 1: `design/004` 新增 skills 一节**

在 `init`（§3.2）之后、`add`（§3.3）之前不要插——小节号是被 `check-docs`
引用校验的。**追加到命令小节的末尾**，取下一个未使用的编号。内容覆盖：
用途、两个子命令、五种状态表（照抄 spec §6）、为什么不提供 `--force`。
并在 §3.2 `init` 的行为列表里补一条「装入 AI 助手技能」与 `--no-skills`。

- [ ] **Step 2: `design/附录合集.md` 附录 E 补 skills**

按附录 E 现有的表格形状补 `skills` 与两个子命令、`init --no-skills`。

- [ ] **Step 3: `试用指南/01-初始化项目.md` 更新预期输出**

`check-guide-output.py` 会把指南里的「✅ 预期」与 CLI 真实输出**逐行**比对，
所以这里必须用真实输出，不能手写。

```bash
cd /home/zhijie/Desktop/github/brickKit
make build-cli
cd $(mktemp -d) && /home/zhijie/Desktop/github/brickKit/bin/brickkit init my-project
```

把真实输出贴进指南，并在目录树里补上 `.claude/skills/`、`AGENTS.md`、
`CLAUDE.md`、`.brickkit/skills.lock`。

- [ ] **Step 4: `AI-CONTEXT.md` 补目录树与导航**

`.brickkit/` 目录树里补 `skills.lock`（`check-doc-tree.py` 会校验）；
文末第 11 节导航补一句技能装在哪、怎么刷新。

- [ ] **Step 5: `开发进度/决策索引.md` 记决策**

按该文件既有条目形状追加，记这四条：
① 技能内容只写「AI 会猜错的东西」，参数指向 `--help`；
② 不做框架级技能（平台不越界 + 18 份 CI 跑不到的散文会集体腐烂）；
③ 手改过的文件不覆盖、不提供 `--force`；
④ Claude Code 不读 `AGENTS.md`，所以另配一行 `@AGENTS.md` 的 `CLAUDE.md`。

- [ ] **Step 6: 跑全部检查**

```bash
cd /home/zhijie/Desktop/github/brickKit
make lint
make test-unit
make cover-check
```

Expected: 三项全通过。`cover-check` 门槛 92%。

- [ ] **Step 7: 提交**

```bash
git add design/ 试用指南/ 开发进度/ AI-CONTEXT.md
git commit -m "文档跟上 skills：目录树、附录 E、指南预期输出、决策索引

指南里的预期输出是真跑一次贴进去的，不是手写的——check-guide-output.py
会逐行比对，手写迟早对不上。

决策索引记了四条，其中「不做框架级技能」和「不给 --force」是拒绝，
理由一起记下来，免得下一个人再想加回来。"
```

---

## 完成标准

全部任务做完后，这些必须同时成立：

- [ ] `make test-unit` 通过
- [ ] `make lint` 通过（含 `check-cli-docs`、`check-doc-tree`、`check-guide-output`、`cover-check`）
- [ ] 空目录里 `brickkit init p` 装出 4 个 SKILL.md + `AGENTS.md` + `CLAUDE.md` + `skills.lock`
- [ ] `brickkit init p --no-skills` 一个技能文件都不产生
- [ ] `brickkit skills update` 连跑两次，第二次报「已是最新」
- [ ] 手改任一技能文件后 `brickkit skills update` 不覆盖它，且提示怎么放弃本地修改
- [ ] 预先存在的 `CLAUDE.md` 在 `init` 后内容不变，且输出里提示了 `@AGENTS.md`
- [ ] 删掉 `skills.lock` 后 `brickkit skills update` 一个文件都不改
- [ ] `.gitignore` 里没有任何一条忽略技能文件（团队共享靠它们入库）
