# 本地源组件的 `metadata.id` 改成不匹配的新值后，`Manifest()`/`DownloadArtifacts()` 仍然沿用改名前的缓存，行为与设计文档记载的不一致

- 日期：2026-09-08
- 状态：**已修复**（commit `f8e1fa9`）——按第 6 节方向 1 的最简形式，且**范围收窄到只改 `metadata.id` 那一半**，见文末「修复结果」
- 发现于：`be-assembly-standard` 项目（brickKit 的真实使用方）在阶段二给 20 条平台断言（设计书 §9.6.2）写自动化回归测试时，用例 9 的后半段（"改 `metadata.id` 后依赖方的 `*_ENDPOINT` 应该消失"）
- 影响面：`internal/source/source.go` 的 `Client.servedByLocalSource()`（核心根因），它的两个调用方 `Client.Manifest()` 与 `Client.DownloadArtifacts()`

---

## 1. 背景：这条规则本来就是显式写下来的，只是覆盖范围留了一个洞

`004-CLI 设计.md` §7.5「Manifest 缓存」明确写着：

> ⚠️ **本地安装源（`type: local`）不吃缓存**，每次都以硬盘上那份 `component.yaml` 为准
>
> **为什么本地源要例外：** 缓存是为了省网络往返，而本地源根本没有网络往返；它的语义恰恰是"这份源码正在被我改"。缓存一份快照的后果是：改了端口、迁移命令或资源配额之后 `brickkit up` 依旧按旧的生成，**而且一声不吭**。

`internal/source/source.go` 里负责落实这条规则的函数 `servedByLocalSource()`，它自己的文档注释也把这件事说得很清楚（359-378 行）：

```go
// servedByLocalSource 判断这个组件会不会由某个**本地**安装源提供。
//
// 本地源的 component.yaml 就在使用者硬盘上、正被他编辑；缓存一份快照
// 只会让改动静默地不生效……
//
// # 文件在、但坏了，也算"由本地源提供"
//
// 判据不能只有 manifestMatches：它在 YAML 解析失败时返回 false，
// 于是"文件坏了"与"这个源没有它"变成同一个答案，调用方退回缓存——
// 使用者改坏了 component.yaml，`up` 却拿上一份好的缓存**照常成功**，
// 一个字都不说。那正是本地源不吃缓存要防的事，只是失败方式更隐蔽：
// 不是"改了没生效"，而是"改错了也没人告诉你"。
//
// 所以只要本地源真的拿得出这个文件，就返回 true，让后面的
// fetchManifest 去解析并把那条语法错误抛出来。
```

这段注释准确地想到了**一种**"改坏了"的形态——YAML 语法错误——并且正确地防住了它。但**另一种"改坏了"的形态**——文件语法完全合法，只是 `metadata.id`（或 `metadata.version`）被改成了跟请求的 `id`/`version` 不匹配的值——没有被同一段推理覆盖，而它触发的正是这段注释自己描述的那个后果："`up` 却拿上一份好的缓存照常成功，一个字都不说"。

同一条规则的地基在更上游：`001-平台理念与总体架构.md` §8.3 与 `002-组件规范.md` §5.2 都把"环境变量名基于组件 ID、值带版本号"定为整个注入规范**双向可推算**的地基——`012-架构设计原理与考量.md` 657 行说得很直接："看到 `people/basic` 就知道变量叫 `PEOPLE_BASIC_ENDPOINT`，看到 `PEOPLE_BASIC_ENDPOINT` 也知道它指的是谁"。`metadata.id` 因此不是一个普通字段，是这条双向规则的锚点——它一旦跟"缓存里记着的身份"脱节而没有任何提示，锚点本身就悄悄漂移了。

## 2. 具体现象

### 2.1 复现路径

1. 一个 `type: local` 安装源里有组件 `foo/bar@1.0.0`。
2. 另一个组件 `baz/qux` 声明对它的（弱）依赖。
3. 第一次 `brickkit up --dry-run`：解析成功，`.brickkit/manifests/foo-bar-1.0.0.yaml` 写入缓存，`baz/qux` 容器正确拿到 `FOO_BAR_ENDPOINT`。
4. **不清理 `.brickkit/` 缓存**，把 `foo/bar` 目录下 `component.yaml` 的 `metadata.id` 改成 `foo/bar2`（目录本身、`brickkit.yaml` 里的引用都不动——这就是一次"改坏了身份"的编辑，不是"改了目录结构"）。
5. 再跑一次 `brickkit up --dry-run`。

**期望**（按 §7.5 与 §8.3 的规则）：`foo/bar@1.0.0` 现在解析不到了（本地源那个目录已经不再声明自己是 `foo/bar`），应该走"弱依赖缺失"的降级路径——警告 + `FOO_BAR_ENDPOINT` 不再注入。

**实际**：`foo/bar@1.0.0` 仍然被判定为存在并启动，`FOO_BAR_ENDPOINT` 依然被注入到 `baz/qux` 容器里，**没有任何警告**。

### 2.2 根因（已定位到具体代码）

`internal/source/source.go`，`Client.Manifest()`（222-259 行）：

```go
func (c *Client) Manifest(ctx context.Context, id, version string) (*Fetched, error) {
	if err := checkRef(id, version); err != nil {
		return nil, err
	}

	cachePath := c.ManifestCachePath(id, version)
	if !c.opts.Refresh && !c.servedByLocalSource(ctx, id, version) {
		if fetched, ok := c.fromCache(cachePath, id, version); ok {
			return fetched, nil   // ← 问题在这里：走了缓存
		}
	}

	raw, m, sourceID, kind, sig, err := c.fetchManifest(ctx, id, version)
	...
```

`servedByLocalSource()`（379-393 行，完整贴出）：

```go
func (c *Client) servedByLocalSource(ctx context.Context, id, version string) bool {
	for _, f := range c.fetchers {
		if f.kind() != config.SourceTypeLocal {
			return false
		}
		raw, err := f.manifestBytes(ctx, id, version)
		if err != nil {
			continue
		}
		if !manifestParses(raw) || manifestMatches(raw, id, version) {
			return true
		}
	}
	return false
}
```

按上面的复现步骤第 5 步：本地源的 `manifestBytes("foo/bar", "1.0.0")` 依然能读到文件（路径没变），`err == nil`；文件内容合法 YAML，`manifestParses(raw)` 为 `true`；但 `manifestMatches(raw, "foo/bar", "1.0.0")` 为 `false`（文件里现在是 `foo/bar2`）。于是 `!manifestParses(raw) || manifestMatches(raw, id, version)` 求值为 `false || false = false`，这次循环不满足 `return true` 的条件；只有一个 fetcher 时循环结束，函数整体返回 `false`。

`Manifest()` 那句判断因此变成 `!Refresh(true) && !false(true)` = `true`，进了 `fromCache` 分支——而 `.brickkit/manifests/foo-bar-1.0.0.yaml` 里还留着改名前写入的那份合法缓存，`fromCache` 正常命中，直接返回，**根本没有走到 `fetchManifest()`**。

而 `fetchManifest()`（488 行起）其实是能正确处理这种情况的——它比对解析出来的 `m.Metadata.ID != id || m.Metadata.Version != version`，不匹配就当作"这个源没有它"继续找下一个源，最终在没有其他源的情况下报"未找到"。**问题precisely 出在 `Manifest()` 的这一层缓存旁路判断上**，只在"本地源目录还在、但内容已经是另一个身份"这个特定组合下才会触发——它从未走到 `fetchManifest()` 那条本该生效的正确逻辑。

### 2.3 第二个受影响的调用点：`DownloadArtifacts`

同一个文件里，`Client.DownloadArtifacts()`（413-425 行）用的是完全一样的判断：

```go
useCache := !c.opts.Refresh && !c.servedByLocalSource(ctx, id, version)
```

紧跟着的注释说的也是同一条规则："与 Manifest 同一条规则：本地源的产物（`.proto`、`openapi.json`）也在使用者硬盘上跟着代码一起改，缓存住只会让调用方按旧契约生成客户端"。这意味着同样的场景下（本地源组件改名，磁盘上原先下载过的 `.brickkit/artifacts/foo-bar-1-0-0/` 还在），产物也会被判定为"可以用缓存"而不重新下载/校验——虽然我们没有单独为这条路径写复现（`servedByLocalSource` 是共享的同一份代码，根因一致，值得在同一次修复里一并覆盖）。

## 3. 排除过的一个假阳性（供你判断我的复现是否可靠）

第一次尝试复现这个问题时，我的临时项目里配置了**两个**本地源（`source-a`、`source-b`），两边都放了一份 `foo/bar@1.0.0`（用来验证"同 id 同 version 两份源，靠前的赢"这条相邻断言）。改名时我只改了 `source-a` 里那份，`source-b` 里那份没有改名——`fetchManifest()` 遍历到 `source-b` 时正确匹配上了，于是"看起来"整个流程仍然成功，让我一度以为这个问题不存在。换成**单一本地源**、干净隔离的临时目录后，问题依然稳定复现——这不是"多源遮蔽"造成的假象，是单源场景下真实存在的行为。

## 4. 最小复现（隔离环境，不涉及真实项目数据）

```bash
mkdir -p /tmp/bk-repro/components/foo/bar && cd /tmp/bk-repro

cat > brickkit.yaml <<'EOF'
project: repro
deploy:
  target: docker
sources:
  - id: local-dev
    type: local
    path: ./components
components:
  - id: foo/bar
    version: 1.0.0
EOF

cat > components/foo/bar/component.yaml <<'EOF'
apiVersion: brickkit/v1
kind: Component
metadata:
  id: foo/bar
  name: 测试组件
  version: 1.0.0
  description: MARKER-BEFORE-RENAME
  license: Apache-2.0
  vendor: brickKit
deployment:
  type: container
  image: brickenterprise/fixture:1.0.0
  port: 19001
healthCheck:
  type: http
  path: /healthz
EOF

# 第一次：正常解析，建立缓存。
brickkit up --dry-run >/dev/null 2>&1
grep description .brickkit/manifests/foo-bar-1.0.0.yaml
# 输出：description: MARKER-BEFORE-RENAME  ← 缓存已写入

# 改 metadata.id（目录不动），不清缓存，直接重跑。
sed -i 's/id: foo\/bar$/id: foo\/bar2/' components/foo/bar/component.yaml
brickkit up --dry-run 2>&1 | grep -E "foo/bar|错误|警告"
# 实际输出：
#    ✅ foo/bar@1.0.0  启动（顶层）
#
# 期望输出（本地源不该吃缓存，这个 id 现在已经解析不到了）：
#    应该报"未找到"或至少给出与本地源身份不匹配相关的警告/错误
```

（如果想同时确认这**不是**"本地源普通字段编辑不生效"的问题——那条路径完全正常：把 `description` 改成别的值、不碰 `metadata.id`，不清缓存直接重跑，缓存文件会立刻反映新内容。问题严格局限于"改动后 `metadata.id`/`version` 与请求的 `id`/`version` 不再匹配"这一种情况。）

## 5. 影响面：不是一个边缘配置

`be-assembly-standard` 项目的全部组件（当前 5 个已建成，最终目标 62 个）都是通过 `type: local` 安装源 + git submodule 管理的（`components/<scope>/<name>/`，一个组件一个 submodule）。这正是 004 §8.2 与另一份既有报告（`2026-09-06-submodule-tracked-components-gap-report.md`）讨论过的"组件仓库→精确 commit"这种更强审计轨迹的用法，直接落在这条代码路径上——不是一个理论上才会撞到的配置组合。虽然"手滑改坏 `metadata.id`"预期是低频操作，但它触发的静默失败模式（`up` 全程无警告、`*_ENDPOINT` 照常存在）与 §7.5 那段设计意图原文描述的正是同一件事，只是覆盖范围差了一种情形。

## 6. 可能的修复方向（未决定，供评估）

1. **`servedByLocalSource` 增加"存在但不匹配"的第三态**，把返回值从 `bool` 改成一个三值结果（如 `notServed` / `servedMatching` / `servedButMismatched`），`Manifest()`/`DownloadArtifacts()` 对 `servedButMismatched` 一律拒绝走缓存（等同于现在对"解析失败"的处理）。这条改法改动面最小，且与函数自己已经写下的推理（"文件在、但坏了，也算由本地源提供"）保持同一种思路的自然延伸——"内容对不上"和"解析不了"本质上是同一类"文件在、但不是我们要的那份"。
2. **`Manifest()`/`DownloadArtifacts()` 对本地源一律 `Refresh`，不经过 `servedByLocalSource` 这层判断**——既然本地源"没有网络往返，也就没有缓存的理由"（§7.5 原话），这层优化本身的价值可能不足以承担现在这个静默失败的代价，直接跳过缓存判断对本地源生效即可。这条改法更彻底，但需要评估是否会影响其它依赖"本地源走 `servedByLocalSource` 这条快速路径"的场景（如果有的话）。
3. 折中：只在 `manifestMatches` 返回 `false`（而不是解析失败）时，把这次 `Manifest()`/`DownloadArtifacts()` 调用标记为"需要走 `fetchManifest()` 重新判定"，但不改变 `servedByLocalSource` 的返回类型——把判断逻辑从 `servedByLocalSource` 内部挪一部分到调用方。

**这条改动的价值面向所有把本地目录组件当作 git submodule（或任何"目录路径固定、内容会独立演进"的场景）使用的 brickKit 使用者，不是 `be-assembly-standard` 专属的需求**——任何在本地源里重命名过组件 ID（哪怕只是手滑）的团队都会撞上同一个静默失败。要不要修、按第 6 节哪个方向修、修到多深，由你决定。

---

## 修复结果（commit `f8e1fa9`）

### 采用的方向

第 6 节**方向 1 的最简形式**：不引入三值枚举，直接在 `servedByLocalSource` 的判据里补一条。

```go
// internal/source/source.go
if !manifestParses(raw) || !manifestIDMatches(raw, id) || manifestMatches(raw, id, version) {
    return true   // 坏 YAML / id 对不上 / 正常匹配 —— 三种都不许走缓存
}
// 唯一放行缓存的情形：文件合法、id 匹配、单纯版本不同
```

新增 `manifestIDMatches(data, componentID)`（只比对 `metadata.id`，不看版本）。`servedByLocalSource` 的返回类型不变、两个调用方（`Manifest` / `DownloadArtifacts`）一个字都不用改。

### 关键收窄：`metadata.version` 那一半**不改**

本报告标题与 §1、§2.2 把 `metadata.id` 与 `metadata.version` 的不匹配归成同一类。评估后发现**只有 `id` 那一半是 bug**：

- **`id` 对不上** = "这个目录已经不认这个组件了" → 该报"未找到"，不能拿改名前的缓存顶。
- **`id` 相同、只是 `version` 不同** = "同一个组件、升到了别的版本" → **这是多版本共存的正常用法**（试用指南 §8.2「升级不会自动改调用方」、§8.6「版本变更摘要」）。本地源一个目录只放得下一个版本，把组件升上去之后，还依赖旧版本的调用方**只能**从 Manifest 缓存里取那一份；`versionMismatchError` 的函数注释也把这写成已知边界。对 `version` 不匹配一并拒绝缓存，会让 §8.2 的 `brickkit up` 当场报错——最初按"文件在就跳过缓存"改的那一版正是被 `make lint` 的 `check-guide-output` 拦下的。

所以判据用的是 `!manifestIDMatches`（只看 id），不是 `!manifestMatches`（id + version）。

### 验证

| 项 | 结果 |
| --- | --- |
| 新增单测 `TestLocalSourceIDRenamedErrorsInsteadOfUsingCache` | 在改动前的代码上先复现失败（拿到旧缓存、无 error），改动后报 `CodeComponentNotFound` |
| 新增单测 `TestLocalSourceUpgradedStillServesOldVersionFromCache` | 升级后请求旧版本仍从缓存命中、请求新版本不吃缓存 —— 守住 §8.2 |
| `go test ./...` / `go vet` / `gofmt` / internal 覆盖率 93%（门槛 92%） | 全绿 |
| `check-guide-output` / `check-docs` / `check-cli-docs` / `check-doc-tree` | 全绿 |
| 端到端（真二进制）：本地源改坏 `metadata.id` 后 `brickkit up --dry-run` | 顶层组件报"组件未找到"退出码 1；弱依赖走降级（`⚠️ 弱依赖缺失`、`*_ENDPOINT` 不注入、生成的 compose 里 0 次） |

`DownloadArtifacts` 与 `Manifest` 共用 `servedByLocalSource`，同一次改动一并覆盖，未单独写复现。
