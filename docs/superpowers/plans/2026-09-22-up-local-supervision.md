# `up` 前台监管模式：真正启动 `mode: local` 组件 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `mode: local` 组件真正跑起来——`brickkit up` 检测到项目里存在 `mode: local` 组件时，容器/Deployment 照常起（`eng.Up()`），然后切成前台监管：按拓扑序把每个 `mode: local` 组件用 `internal/runcmd`（Plan 3）探测出的命令拉起来，交给 `internal/procsup`（Plan 2）监管，`internal/sessionlock`（Plan 2）挡并发会话，Ctrl+C /任意进程崩溃时体面收尾并在最后一屏打出崩溃现场。新增 `brickkit up --crash-lines N` 旗位。

**Architecture:** 三个已经独立验证过的库（`runcmd` 探测命令、`procsup` 监管进程、`sessionlock` 挡并发）在 `internal/cli` 新文件 `up_local.go` 里被第一次接到一起，复用 `buildUpPlan` 已经算好的全部结论（拓扑序、注入结果、`compose.Result`）而不是重新算一遍。最关键的复用点是 Plan 4a 已经做的一个决定的副产品：`mode: local` 组件从 Task 3 起就被路由进跟 `mode: debug` 同一个 `p.locals` 桶，这意味着 `internal/compose/local.go` 的 `localEnvFile` 已经在算"这个本地组件应该拿到的完整环境变量、依赖地址已经指向 localhost、资源地址已经指向 localhost"——本计划只需要把这份已经算好的 `[]inject.Var`（本计划给 `LocalEnvFile` 新增的字段）取出来，做**严格的** `${VAR}` 展开（跟现有 `local-debug.*.env` 文件"展开不了就留着占位符"的宽松策略不同——那是给人看的排障文件，这里是真的要喂给进程的环境，展开不了必须报错，不能让一个字面量 `${DB_PASSWORD}` 悄悄成为进程的真实环境变量），再拼上 `runcmd.Command.Env`（语言适配器自己的环境变量，比如 `PYTHONUNBUFFERED=1`）与 `PORT=<端口>`，交给 `procsup.Spec.Env`。

**Tech Stack:** 标准库 `os/signal`（`signal.NotifyContext`）。不新增第三方依赖。

**Spec:** `docs/superpowers/specs/2026-09-21-mode-field-and-local-execution-design.md` 的 §1（"组件没有本地源码时写 mode: local/debug，生成阶段报错"——本计划执行前的调研已经发现这条对 `debug` 从来没真正实现过，且深入想过之后认为这条要求本质上只对 `local` 成立，见"设计决定"第 1 条）、§3（本地进程监管的编排细节）、§4 最后几条（端口探测）、§6.1（`PORT` 保留变量，Plan 4a 已完成注册）、§6.2（密钥解析：local 比 debug 更安全，全程内存不落盘）、§6.4（两种探测失败分别处理）。`docs/superpowers/plans/2026-09-22-local-process-supervision.md` 的"留给 Plan 4 的衔接点"一节是这两个库当初设计时设想的调用形状，本计划是那份设想的落地（有出入的地方在"设计决定"里说明为什么）。

## 这份计划在整体拆分里的位置

| 子计划 | 内容 | 状态 |
| --- | --- | --- |
| Plan 4a | `component.yaml` 的 `local:` 块、`mode: local` 五态校验、生成层安全跳过、`PORT` 保留变量 | 已完成 |
| **本计划（Plan 4b）** | `up` 前台监管模式：真正接起 `runcmd`+`procsup`+`sessionlock`，`--crash-lines` | 待执行 |
| Plan 4c（待写） | 调试挂载怎么接 | 依赖本计划 |
| Plan 4d（待写） | `graph`/`status`/`down` 展示层（含 `sessionlock.Inspect` 的跨终端可见性）、`check-guides` 的 `local` 层、文档 | 依赖本计划 |

本计划**不做**："status/down 从别的终端看到有会话在跑"——`sessionlock.Inspect` 已经在 Plan 2 交付，读它、把结果打到 `status`/`down` 的输出上是纯展示层工作，留给 Plan 4d 跟 `graph`/`lifecycle.go` 那几处一起做，理由是同一类（不影响任何命令的执行结果，只影响终端上打印的文字）。

## 设计决定（本计划执行前的调研结论）

1. **"没有本地源码时报错"只对 `mode: local`成立，不是 `debug` 与 `local` 共享的规则——这纠正了 spec §1 原文的措辞。** 调研已确认：`mode: debug` 今天的实现里，平台从未检查过用户声明的组件有没有本地源码目录，也不需要检查——`debug` 的进程由用户自己在 IDE 里启动，可能在这台机器上的任何地方，平台压根不关心。`mode: local` 不一样：**平台自己**要 `cd` 到某个目录去执行探测出的命令（`runcmd.Detect` 的 `dir` 参数、`procsup.Spec.Dir`），没有源码目录，平台连去哪个目录都不知道。所以这条校验是新写的、`mode: local` 专属的，不是把 `debug` 现有的什么校验复用/推广。
2. **这条校验放在 `buildUpPlan` 里，`--dry-run` 也要查——不等到真正启动那一刻。** 跟 `resolver.CheckRunningResourceBindings`（同一个函数里，紧邻的位置）是同一类"生成阶段就该知道、不该等运行时才炸"的检查，`--dry-run` 的整个意义就是"告诉我这次会发生什么"，一个连本地源码都没有的 `mode: local` 组件显然跑不起来，`--dry-run` 应该提前说清楚，而不是留到真启动才报错。
3. **本地进程的环境变量取自 `compose.LocalEnvFile`，不重新算一遍。** Plan 4a 把 `mode: local` 路由进了 `p.locals`，`internal/compose/local.go` 的 `localEnvFile` 因此已经在为每个 `mode: local` 组件算出"依赖地址指向 localhost、资源地址指向 localhost（含 `host.docker.internal` → `localhost` 的改写）"的完整变量列表——这正是本地进程需要的那份环境，一字不差。本计划给 `LocalEnvFile` 加一个 `Vars []inject.Var` 字段（`localEnvFile` 内部已经有这个切片，只是从没往外传），显示文件（`local-debug.<service>.env`）与真正喂给进程的环境这次终于共用同一份计算结果，不会出现"文件里写的和进程实际拿到的不一样"这种漂移。
4. **`${VAR}` 展开对本地进程必须严格，对显示文件保持宽松——这是两条不同的路，不是同一份逻辑改严格了。** `internal/compose/local.go` 的 `expandValue` 展开不了 `${VAR}` 时**原样保留占位符**，注释写得很清楚："这个文件是顺手生成的调试辅助，不该因为一个变量没配就让整个 brickkit up 失败"。这条理由对**文件**成立，对**真正要执行的进程**不成立——如果 `DATABASE_PASSWORD` 的值字面量是 `${DB_PASSWORD}` 这几个字符，说明这台机器的环境变量或 `.env` 里没有 `DB_PASSWORD`，组件会拿着这个不存在的"密码"去连库，得到一个完全牛头不对马嘴的连接失败，而不是"环境变量没配对"这个真正的原因。本地进程该有的行为是 K8s Secret 那一条路（005 §5.6）：**展开不了就直接阻断，报错点名哪个变量、哪个组件**，不是留着占位符继续跑。`local-debug.*.env` 文件本身完全不受影响，继续用它原来那条宽松的路径——两条路径共享同一份 `[]inject.Var` 输入，只是展开阶段的容错策略不同，服务对象不同（一个给人看，一个真的要执行）。
5. **容器先起、本地进程后起，不是"整个按拓扑序穿插"。** `eng.Up()` 一次性把全部容器服务交给 docker compose，compose 自己按内部的 `depends_on` 把顺序理清楚；本地进程由 brickkit 自己的 Go 代码一个个 `procsup.Start` 拉起，起的顺序必须是拓扑序（spec §3 最后一条：复用容器算好的顺序，不卡健康检查），但这只在"本地进程之间谁先谁后"以及"依赖某个容器的本地进程必须等那个容器先起来"这两件事上有意义——而容器一次性起完就已经满足"本地进程依赖的容器已经在跑"这个前提了。所以流程是：`eng.Up()` 成功 → 按拓扑序过滤出本地组件、依次 `procsup.Start`。不需要把容器和本地进程交织在同一个循环里。
6. **`.brickkit/session.lock` 走跟 `.brickkit/credentials` 一样的 `.gitignore` 路子：新项目 `init` 时自动加进去，已有项目要重新 `init` 才会补上。** 这是仓库已有机制（`EnsureGitignore` 只在 `brickkit init` 时被调用一次，不是每个命令都会去补），本计划照现成的套路加一行 `gitignoreSection`，不新造一个"随时补全 .gitignore"的机制。
7. **docker ↔ local 切换不需要新代码。** `mode: local` 从不生成容器（Plan 4a），所以一个组件从 `local` 切到别的 mode 时，它在 compose 文件里从无到有，跟任何别的新增组件没有区别；反过来从别的 mode 切到 `local` 时，它从 compose 文件里消失，`--remove-orphans`（已有机制，Docker 引擎调用时本来就带着）负责把上一次生成的、这次不再需要的容器清掉——跟别的组件被 `mode: disable` 关掉时是同一条路径，不是 `mode: local` 专属的新问题。本地进程这一侧不存在"孤儿"概念：它只在当前这个前台会话里存在，会话一结束（无论是正常退出还是 Ctrl+C）它就没了，没有跨会话遗留的东西需要清理。
8. **`--crash-lines` 用 `cmd.Flags().Changed("crash-lines")` 判断"用户是不是真写了这个旗位"，而不是拿值跟默认值比。** 项目里没有 `mode: local` 组件、又传了 `--crash-lines`（不管传的是不是恰好等于默认值 20）时要警告——按值比较分不清"用户手写了 20"和"用户压根没传、拿到的是默认值 20"，这仓库目前没有用过 `Flags().Changed`，但这正是 cobra 本身提供、专门解决这个问题的标准方法，不是发明新机制。
9. **崩溃汇总、`HeldError` 提示、探测结果对应的三种处理，全部走 `internal/msgid` + 两份 `catalog_*.go`。** `procsup`/`sessionlock` 库本身没有一句面向用户的话（Plan 2 的设计决定就是如此），这次要新增的全部面向用户文案都在这里补上。

## File Structure

- Modify: `internal/compose/local.go`——`LocalEnvFile` 加 `Vars []inject.Var` 字段，`localEnvFile` 顺手传出
- Modify: `internal/compose/compose_test.go` 或 `local_test.go`——新增字段的测试
- Modify: `internal/config/layout.go`——`FileSessionLock` 常量、`Layout.SessionLockPath()`
- Modify: `internal/config/scaffold.go`——`gitignoreSections()` 加一行
- Modify: `internal/config/layout_test.go`、`scaffold_test.go`——对应测试
- Create: `internal/cli/up_local.go`——本计划的主体：本地源码校验、命令探测、环境变量展开、前台监管循环
- Create: `internal/cli/up_local_test.go`
- Modify: `internal/cli/up.go`——`buildUpPlan` 接入本地源码校验与本地组件收集；`runUp`/`start` 接入前台监管；新增 `--crash-lines` 旗位
- Modify: `internal/msgid/cli.go`（或对应文件）、`internal/i18n/catalog_{en,zh}.go`——新增面向用户的文案
- Modify: `docs/superpowers/plans/2026-09-22-up-local-supervision.md`（本文件）——收尾时回写执行结果

---

## Task 1: 打通数据源——`LocalEnvFile.Vars`、会话锁路径、`--crash-lines` 旗位骨架

**背景：** 三件互相独立的小事，为后面的任务把地基铺好：把 `localEnvFile` 已经算出、却没往外传的 `[]inject.Var` 暴露出来；给会话锁一个有名有姓的路径（照抄 `SkillsLockPath` 的写法）；把 `--crash-lines` 旗位定义出来（先只做"能不能传、传了没有 `mode: local` 组件时警不警告"，具体怎么用留给后面任务）。

**Files:**
- Modify: `internal/compose/local.go`
- Modify: `internal/compose/compose_test.go`
- Modify: `internal/config/layout.go`
- Modify: `internal/config/scaffold.go`
- Modify: `internal/config/layout_test.go`
- Modify: `internal/config/scaffold_test.go`
- Modify: `internal/cli/up.go`
- Modify: `internal/msgid/cli.go`
- Modify: `internal/i18n/catalog_en.go`、`internal/i18n/catalog_zh.go`

**Interfaces:**
- Produces：`compose.LocalEnvFile.Vars []inject.Var`、`config.Layout.SessionLockPath() string`、`config.FileSessionLock`、`up` 命令的 `--crash-lines` 旗位（此刻只警告，不接监管逻辑）。

- [ ] **Step 1: `LocalEnvFile` 加 `Vars`**

`internal/compose/local.go` 第 50-59 行，`LocalEnvFile` 结构体加一个字段：

```go
type LocalEnvFile struct {
	Ref resolver.Ref
	// Name 是文件名：local-debug.<版本化服务名>.env。
	// 用版本化服务名，同一组件的两个版本同时调试时才不会互相覆盖。
	Name string
	// Port 是该进程应当在宿主机上监听的端口。
	Port int
	// Vars 是渲染 Content 用的那份变量列表：依赖地址、资源地址都已经改写成
	// localhost，但 ${VAR} 引用还没展开（Content 用宽松的展开策略，允许
	// 展开不了时留着占位符；mode: local 真正启动进程时用的是严格展开，
	// 见 internal/cli/up_local.go——两条路径共用这份原始列表，不会漂移）。
	Vars    []inject.Var
	Content []byte
}
```

`localEnvFile` 函数（第 539-555 行）把已经算好的 `vars` 一并放进返回值：

```go
func (p *plan) localEnvFile(l localComponent, now time.Time, lookup func(string) (string, bool)) LocalEnvFile {
	vars := make([]inject.Var, len(l.Env.Env))
	copy(vars, l.Env.Env)

	p.pointDependenciesAtLocalhost(l, vars)
	p.pointResourcesAtLocalhost(vars)

	return LocalEnvFile{
		Ref:     l.Ref,
		Name:    "local-debug." + l.Service + ".env",
		Port:    l.Port,
		Vars:    vars,
		Content: renderEnvFile(l, vars, now, lookup),
	}
}
```

- [ ] **Step 2: 测试**

在 `internal/compose/compose_test.go`（或者跟 `TestModeLocalDependentsGetAnAutoAssignedPortAndExtraHosts` 放在一起，`Plan 4a：mode: local` 那一节）加一条，确认 `Vars` 与 `Content` 描述的是同一份数据：

```go
func TestLocalEnvFileVarsMatchTheRenderedContent(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{Mode: config.ModeLocal})

	result := b.generate() // builder.generate()（compose_test.go 已有）：跑完整条链路，失败即断言失败

	require.Len(t, result.LocalEnvFiles, 1)
	file := result.LocalEnvFiles[0]
	require.NotEmpty(t, file.Vars)
	for _, v := range file.Vars {
		if v.ExistingSecretRef != "" {
			continue
		}
		assert.Contains(t, string(file.Content), v.Name+"=", "Vars 里的每一条都该出现在渲染出的文件里")
	}
}
```

（`newBuilder`/`b.component`/`simple`/`b.generate()` 都是 `compose_test.go` 已有的测试构造帮助函数，跟 Plan 4a 的 `TestModeLocal*` 系列用的是同一套。）

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/compose/ -v -run TestLocalEnvFileVarsMatchTheRenderedContent`
Expected: `PASS`。

Run: `go test ./internal/compose/`
Expected: `PASS`。

- [ ] **Step 4: 会话锁路径**

`internal/config/layout.go`，在 `FileSkillsLock` 常量（第 31-32 行）附近加：

```go
	// FileSessionLock 是本地进程前台监管的会话锁（005 §3）。
	FileSessionLock = "session.lock"
```

在 `SkillsLockPath` 方法（第 96 行）附近加：

```go
func (l Layout) SessionLockPath() string { return l.path(DirBrickkit, FileSessionLock) }
```

- [ ] **Step 5: `.gitignore` 模板加一行**

`internal/config/scaffold.go` 的 `gitignoreSections()`（第 123-134 行），在 `ConfigGitignoreCredentials` 那一行后面加一行（新增一个 msgid：`ConfigGitignoreSessionLock`，两份 catalog 都要写译文，照抄 `ConfigGitignoreCredentials` 的文案风格）：

```go
		{"# " + i18n.T(msgid.ConfigGitignoreGenerated), []string{".brickkit/generated/"}},
		{"# " + i18n.T(msgid.ConfigGitignoreCredentials), []string{".brickkit/credentials"}},
		{"# " + i18n.T(msgid.ConfigGitignoreSessionLock), []string{".brickkit/session.lock"}},
		{"# " + i18n.T(msgid.ConfigGitignoreEnvFile), []string{".env"}},
```

- [ ] **Step 6: 测试**

`internal/config/layout_test.go`：照抄现有的 `SkillsLockPath` 测试，加一条断言 `SessionLockPath()` 返回 `.brickkit/session.lock`（相对项目根）。

`internal/config/scaffold_test.go`：找到测 `gitignoreSections`/`EnsureGitignore` 的现有测试（多半是断言生成的 `.gitignore` 内容包含 `.brickkit/credentials` 那一类），加一行断言包含 `.brickkit/session.lock`。

Run: `go test ./internal/config/`
Expected: `PASS`。

- [ ] **Step 7: `--crash-lines` 旗位骨架**

`internal/cli/up.go` 的 `newUpCommand`（第 34-60 行），加一个 `int` 旗位与对应的 `upOptions` 字段：

```go
	var (
		dryRun         bool
		kubeContext    string
		ignoreServedBy bool
		crashLines     int
	)

	cmd := &cobra.Command{
		Use:     "up",
		Short:   i18n.T(msgid.CliUpShort),
		GroupID: groupLifecycle,
		Long:    i18n.T(msgid.CliUpLong),
		Example: i18n.T(msgid.CliUpExample),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUp(cmd.Context(), opts, upOptions{
				dryRun: dryRun, kubeContext: kubeContext, ignoreServedBy: ignoreServedBy,
				crashLines: crashLines, crashLinesSet: cmd.Flags().Changed("crash-lines"),
			})
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, i18n.T(msgid.CliUpOnlyGenerateTheDeploymentFiles))
	cmd.Flags().StringVar(&kubeContext, "context", "", i18n.T(msgid.CliDownKubeconfigContextOverridesDeployContext))
	cmd.Flags().BoolVar(&ignoreServedBy, "ignore-served-by", false,
		i18n.T(msgid.CliUpClearEveryServedbyDeclarationIn))
	cmd.Flags().IntVar(&crashLines, "crash-lines", procsup.DefaultTailLines,
		i18n.T(msgid.CliUpCrashLinesHowManyLinesOfOutput))
	return cmd
```

`upOptions`（第 99-108 行）加两个字段：

```go
type upOptions struct {
	dryRun bool
	kubeContext string
	ignoreServedBy bool
	// crashLines 是 --crash-lines 的值；crashLinesSet 为 true 才说明用户真的
	// 传了这个旗位（不能靠"值等不等于默认值"判断——用户完全可能手写
	// --crash-lines 20，跟不传时拿到的默认值撞在一起）。
	crashLines    int
	crashLinesSet bool
}
```

在 `buildUpPlan`（第 158 行附近，紧跟 `warnTargetOnlyFields(opts, cfg)` 之后）加一条警告：项目没有任何 `mode: local` 组件、但 `flags.crashLinesSet` 为真：

```go
	if flags.crashLinesSet && !anyModeLocal(cfg.Components) {
		renderWarnings(opts, []*clierr.Error{clierr.Warn(clierr.CodeConfigInvalid,
			i18n.T(msgid.CliUpCrashLinesHasNoEffect))})
	}
```

`anyModeLocal` 是新的小函数（放进本任务新建的 `up_local.go`，Task 2 会创建这个文件——这一步先在 `up.go` 里内联一个最小实现，Task 2 落地后可以直接搬过去）：

```go
func anyModeLocal(components []config.Component) bool {
	for _, c := range components {
		if c.Mode == config.ModeLocal {
			return true
		}
	}
	return false
}
```

- [ ] **Step 8: 测试**

在 `internal/cli/up_test.go` 加两条：

```go
func TestUpWarnsWhenCrashLinesIsSetWithoutAnyModeLocalComponent(t *testing.T) {
	f := composeProject(t) // 现成的 fixture：erp/backend 依赖 people/basic，都不是 mode: local
	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run", "--crash-lines", "5")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout+r.stderr, "crash-lines")
}

func TestUpDoesNotWarnAboutCrashLinesWhenNotPassed(t *testing.T) {
	f := composeProject(t)
	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout+r.stderr, "crash-lines")
}
```

Run: `go test ./internal/cli/ -v -run TestUpWarnsWhenCrashLines\|TestUpDoesNotWarnAboutCrashLines`
Expected: 两条都 `PASS`。

- [ ] **Step 9: 全量测试 + gofmt + 提交**

Run: `gofmt -l internal/compose internal/config internal/cli internal/i18n`
Run: `go test ./internal/compose/ ./internal/config/ ./internal/cli/ ./internal/i18n/`
Expected: 全部 `PASS`。

```bash
git add internal/compose/local.go internal/compose/compose_test.go internal/config/layout.go internal/config/scaffold.go internal/config/layout_test.go internal/config/scaffold_test.go internal/cli/up.go internal/cli/up_test.go internal/msgid/cli.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "$(cat <<'EOF'
新增：LocalEnvFile.Vars、会话锁路径、--crash-lines 旗位骨架

三件独立的地基工作：① localEnvFile 已经算出的 []inject.Var 这次真正往外传，
显示文件与后续真正启动本地进程用的是同一份计算结果，不会漂移；② 会话锁路径
照抄 SkillsLockPath 的写法，.gitignore 模板同步加一行；③ --crash-lines 旗位
先只接警告逻辑（没有 mode: local 组件却传了这个旗位），用 Flags().Changed
而不是比较默认值——用户完全可能手写出恰好等于默认值的数字。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: 本地源码校验 + 命令探测（`internal/cli/up_local.go` 上半部）

**背景：** `mode: local` 组件必须有本地源码目录（跟 `mode: debug` 不同，见"设计决定"第 1 条），这条校验要在 `buildUpPlan` 里做、`--dry-run` 也要查得到。查完之后，对每个 `mode: local` 组件调 `internal/runcmd.Detect`，把 `manifest.Local` 转译成 `runcmd.Hints`。

**Files:**
- Create: `internal/cli/up_local.go`
- Create: `internal/cli/up_local_test.go`
- Modify: `internal/cli/up.go`
- Modify: `internal/msgid/cli.go`
- Modify: `internal/i18n/catalog_en.go`、`internal/i18n/catalog_zh.go`

**Interfaces:**
- Consumes：`internal/runcmd.Detect`/`Hints`/`Params`/`Command`（Plan 3）、`internal/workspace.Exists`/`SourceDir`。
- Produces：`localComponentPlan{Ref, Service, Dir string, Command runcmd.Command, Port int}`（`Env []string` 由 Task 3 补上）、`func collectLocalComponents(layout config.Layout, cfg *config.Config, graph *resolver.Graph, order *resolver.Plan, localEnvFiles []compose.LocalEnvFile, lookup func(string) (string, bool)) ([]localComponentPlan, error)`——签名从这一步起就带着 `lookup`（Task 2 自己的逻辑还用不上它，Task 3 再接进函数体，这样 Task 3 只用改函数体、不用回头改一次签名与 Task 2 已经写好的调用点）。Task 3、Task 4 直接消费这个类型与函数。

- [ ] **Step 1: 建 `up_local.go`，写本地源码校验**

```go
package cli

import (
	"fmt"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/runcmd"
	"github.com/brickkit/brickkit/internal/workspace"
)

// anyModeLocal 判断项目里有没有 mode: local 组件。
func anyModeLocal(components []config.Component) bool {
	for _, c := range components {
		if c.Mode == config.ModeLocal {
			return true
		}
	}
	return false
}

// checkLocalSources 确认每个即将运行的 mode: local 组件都有本地源码目录。
//
// 跟 mode: debug 不一样：debug 的进程由用户自己在 IDE 里启动，可能在这台机器
// 上的任何地方，平台不关心；local 的进程由平台自己拉起，得知道去哪个目录、
// cd 进去执行探测出的命令——没有源码目录，这件事根本无从谈起。
//
// 放在生成阶段查（buildUpPlan 里，--dry-run 也会走到）：跟资源绑定检查
// （resolver.CheckRunningResourceBindings）同一类"生成阶段就该知道、
// 不该等运行时才炸"的检查。
func checkLocalSources(layout config.Layout, cfg *config.Config, running []resolver.Ref) error {
	runningSet := make(map[string]bool, len(running))
	for _, ref := range running {
		runningSet[ref.ID] = true
	}

	p := clierr.NewProblemSet(clierr.CodeConfigInvalid, i18n.T(msgid.CliUpLocalComponentsMissingSource))
	for i, c := range cfg.Components {
		if c.Mode != config.ModeLocal || !runningSet[c.ID] {
			continue
		}
		if !workspace.Exists(layout, c.ID) {
			p.Add(fmt.Sprintf("components[%d]", i), i18n.T(msgid.CliUpNoLocalSourceFor, c.ID))
		}
	}
	return p.Err()
}
```

- [ ] **Step 2: 新增 msgid + 两份译文**

`internal/msgid/cli.go`：

```go
	CliUpLocalComponentsMissingSource = "cli.up_local_components_missing_source"
	CliUpNoLocalSourceFor             = "cli.up_no_local_source_for"
```

`internal/i18n/catalog_en.go`：

```go
	msgid.CliUpLocalComponentsMissingSource: "one or more mode: local components have no local source directory",
	msgid.CliUpNoLocalSourceFor:             "%[1]s has mode: local but no source directory under components/ — run `brickkit add %[1]s --repo`, or write it there yourself",
```

`internal/i18n/catalog_zh.go`：

```go
	msgid.CliUpLocalComponentsMissingSource: "一个或多个 mode: local 组件没有本地源码目录",
	msgid.CliUpNoLocalSourceFor:             "%[1]s 写了 mode: local，但 components/ 下没有它的源码目录——跑 `brickkit add %[1]s --repo`，或者自己把源码放进去",
```

- [ ] **Step 3: 测试**

在 `internal/cli/up_local_test.go`：

```go
package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// addedProject（remove_test.go）只用 `add`（不带 --repo）搭项目：只拉 Manifest
// 缓存，从不往 components/ 下写源码目录（AGENTS.md §2.3：add 默认不 clone 源码）。
// 所以这条测试不需要任何"删掉源码目录"的步骤——它天然就不存在。
func TestUpModeLocalWithoutSourceDirectoryIsAnError(t *testing.T) {
	comps := []comp{{ID: "people/basic", Version: "1.0.0"}}
	f := addedProject(t, comps, "people/basic@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
    mode: local
`)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout+r.stderr, "people/basic")
	assert.Contains(t, r.stdout+r.stderr, "mode: local")
}
```

- [ ] **Step 4: 接进 `buildUpPlan`**

`internal/cli/up.go`，在 `renderOrder(opts, order, plan.graph)`（第 256 行）之后、`env, err := inject.Build(...)`（第 258 行）之前插入：

```go
	if err := checkLocalSources(layout, cfg, plan.states.Running()); err != nil {
		return nil, err
	}
```

- [ ] **Step 5: 命令探测**

在 `up_local.go` 追加：

```go
// localComponentPlan 是一个 mode: local 组件要怎么启动的全部结论。
type localComponentPlan struct {
	Ref     resolver.Ref
	Service string
	Dir     string
	Command runcmd.Command
	Port    int
}

// collectLocalComponents 按拓扑序收集全部 mode: local 组件，探测出各自的启动命令。
//
// lookup 这一步还用不上（Task 3 才会把它接进 buildLocalEnv）——现在就放进签名，
// 是不想等 Task 3 再回头改一次签名、连带改掉这一步已经写好的调用点与测试。
func collectLocalComponents(
	layout config.Layout, cfg *config.Config, graph *resolver.Graph,
	order *resolver.Plan, localEnvFiles []compose.LocalEnvFile, lookup func(string) (string, bool),
) ([]localComponentPlan, error) {
	localMode := make(map[string]bool, len(cfg.Components))
	for _, c := range cfg.Components {
		if c.Mode == config.ModeLocal {
			localMode[c.ID] = true
		}
	}
	if len(localMode) == 0 {
		return nil, nil
	}

	portOf := make(map[resolver.Ref]int, len(localEnvFiles))
	for _, f := range localEnvFiles {
		portOf[f.Ref] = f.Port
	}

	var out []localComponentPlan
	for _, step := range order.Steps {
		ref := step.Ref
		if !localMode[ref.ID] {
			continue
		}
		node := graph.Node(ref)
		if node == nil || node.Manifest == nil {
			continue
		}
		port, ok := portOf[ref]
		if !ok {
			continue // localEnvFiles 只含"这次真的在跑"的组件；跟 collectTargets 同一份 running 集合
		}

		dir := workspace.SourceDir(layout, ref.ID)
		hints := hintsFromManifest(node.Manifest.Local)
		cmd, err := runcmd.Detect(dir, hints, runcmd.Params{Port: port})
		if err != nil {
			return nil, detectionError(ref, err)
		}
		if err := cmd.CheckProgram(); err != nil {
			return nil, programMissingError(ref, err)
		}

		out = append(out, localComponentPlan{
			Ref: ref, Service: step.Service, Dir: dir, Command: cmd, Port: port,
		})
	}
	return out, nil
}

// hintsFromManifest 把 component.yaml 的 local: 块翻译成 runcmd.Hints；
// 没写这个块（*Local 为 nil）就是全部留空，交给自动探测。
func hintsFromManifest(l *manifest.Local) runcmd.Hints {
	if l == nil {
		return runcmd.Hints{}
	}
	return runcmd.Hints{Language: l.Language, RunCommand: l.RunCommand}
}
```

`runcmd` 那几个结构化错误类型（`*NoCommandError`/`*AmbiguousError`/`*UnknownLanguageError`/`*ProgramMissingError`）各自的 `Error()` 已经把 `Problem.Reason`/`Detail`/`Options`（或候选语言/命令）拼成了一句完整、可读的英文诊断——按 Plan 3 自己的 Global Constraints，这些是"给开发者看的诊断信息，不是面向用户的文案"，本该由这一步翻成 i18n 文案，但每种 `Reason`（十种）分别配一条 msgid 会让这一步膨胀成一张几乎复述 `runcmd/errors.go` 全部枚举值的翻译表，而且两边的分类迟早会走漂——`runcmd` 未来新增一种 `Reason` 时，这里的 msgid 表不会自动跟着长出新的一项。

改用 `up.go` 已有的 `engineFailure`（第 581-589 行）同一个手法：不逐条翻译底层错误的文字本身，而是给一条**通用**的顶层消息（"探测不出这个组件该怎么启动"），把 `err.Error()` 原样放进一条 `Detail`（复用现成的 `msgid.LabelReason`），再给一句通用的 hint（去 `component.yaml` 写 `local.runCommand`）。`runcmd` 那几个 `Error()` 已经把组件目录、候选项、具体原因都拼清楚了，直接透传比另造一张十几行的翻译表更不容易漂移：

```go
func detectionError(ref resolver.Ref, err error) error {
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliUpCouldNotDetermineHowToStart, ref.String())).
		WithDetail(i18n.T(msgid.LabelReason), err.Error()).
		WithHint(i18n.T(msgid.CliUpWriteLocalRunCommandInThe, ref.ID))
}

func programMissingError(ref resolver.Ref, err error) error {
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliUpTheDetectedProgramIsNotInstalled, ref.String())).
		WithDetail(i18n.T(msgid.LabelReason), err.Error())
}
```

新增两条 msgid + 译文：`CliUpCouldNotDetermineHowToStart`（en: `"couldn't determine how to start %[1]s"`；zh: `"探测不出 %[1]s 该怎么启动"`）、`CliUpWriteLocalRunCommandInThe`（en: `"write local.runCommand in %[1]s's component.yaml"`；zh: `"在 %[1]s 的 component.yaml 里写 local.runCommand"`）、`CliUpTheDetectedProgramIsNotInstalled`（en: `"the detected command for %[1]s needs a program that isn't installed on this machine"`；zh: `"%[1]s 探测出的命令需要一个这台机器上没装的程序"`）。

- [ ] **Step 6: 测试**

不借 `tests/components/demo-hello`（那是给仓库自测容器化部署用的完整固件，依赖多、体积大）——本地探测测试只需要一个能被 `internal/runcmd` 认出来的最小 Go 目录，照 Plan 3 `TestADetectedGoCommandReallyRuns` 的路子，直接用 `writeTree`（`testsupport_component_test.go` 已有）把 `go.mod`/`main.go` 写进 `workspace.SourceDir(f.Layout, "demo/hello")`：

```go
func TestCollectLocalComponentsDetectsARealGoFixture(t *testing.T) {
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	writeTree(t, workspace.SourceDir(f.Layout, "demo/hello"), map[string]string{
		"go.mod":  "module example.com/hello\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)

	cfg := f.parsed(t)
	graph, states, err := resolveTopology(context.Background(), newTopologyClient(t, f, cfg), cfg)
	require.NoError(t, err)
	order, err := resolver.Order(graph.Subgraph(states.Running()))
	require.NoError(t, err)
	env, err := inject.Build(cfg, graph, states)
	require.NoError(t, err)
	genResult, err := compose.Generate(cfg, graph, states, env, compose.Options{Engine: compose.EngineDocker})
	require.NoError(t, err)

	plans, err := collectLocalComponents(f.Layout, cfg, graph, order, genResult.LocalEnvFiles, envLookup(f.Dir))

	require.NoError(t, err)
	require.Len(t, plans, 1)
	assert.Equal(t, []string{"go", "run", "."}, plans[0].Command.Argv)
}
```

这一步测的是"探测阶段"本身，不需要真的启动进程，所以不用等到 Task 4 的真实进程基础设施。

- [ ] **Step 7: 跑测试 + gofmt + 提交**

Run: `gofmt -l internal/cli`
Run: `go test ./internal/cli/ -v -run TestUpModeLocalWithoutSourceDirectoryIsAnError\|TestCollectLocalComponentsDetectsARealGoFixture`
Run: `go test ./internal/cli/`
Expected: 全部 `PASS`。

```bash
git add internal/cli/up_local.go internal/cli/up_local_test.go internal/cli/up.go internal/msgid/cli.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "$(cat <<'EOF'
新增：mode: local 的本地源码校验与命令探测

mode: local 必须有本地源码目录——跟 mode: debug 不同，平台自己要 cd 进去
执行探测出的命令，不像 debug 那样完全不关心进程在哪。校验放进 buildUpPlan，
--dry-run 也查得到，跟资源绑定检查同一类"生成阶段就该知道"的检查。
collectLocalComponents 按拓扑序对每个 mode: local 组件调 runcmd.Detect，
探测失败/程序缺失都翻成点名组件的错误。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: 严格展开环境变量 + `PORT` 注入（`up_local.go` 中段）

**背景：** `LocalEnvFile.Vars`（Task 1）是"localhost 化过、但 `${VAR}` 还没展开"的原始变量列表；本地进程真正启动前要把它们严格展开成 `procsup.Spec.Env` 能用的 `"KEY=VALUE"` 列表——展开不了直接报错，不能让字面量 `${...}` 溜进真实进程的环境（设计决定第 4 条）。再叠上 `runcmd.Command.Env`（语言专属，比如 `PYTHONUNBUFFERED=1`）与 `PORT=<端口>`。

**Files:**
- Modify: `internal/cli/up_local.go`
- Modify: `internal/cli/up_local_test.go`
- Modify: `internal/msgid/cli.go`
- Modify: `internal/i18n/catalog_en.go`、`internal/i18n/catalog_zh.go`

**Interfaces:**
- Consumes：Task 1 的 `LocalEnvFile.Vars`，Task 2 的 `localComponentPlan`。
- Produces：`func buildLocalEnv(ref resolver.Ref, vars []inject.Var, port int, cmdEnv []string, lookup func(string) (string, bool)) ([]string, error)`——本任务 Step 1 顺带把它接进 `collectLocalComponents`（Task 2 已经把 `lookup` 参数留在签名里），`localComponentPlan` 因此多出 `Env []string` 字段，Task 4 直接读这个字段喂给 `procsup.Spec.Env`。

- [ ] **Step 1: 写严格展开函数**

在 `up_local.go` 追加（`envVarRe` 这条正则跟 `internal/compose/local.go` 里的那条是同一份规则，`003 §5.4`——不导出，各自维护一份是现有仓库惯例，`internal/config` 与 `internal/compose` 也各自有一份同规则的正则，不是本计划引入的新重复）：

```go
var localEnvVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// buildLocalEnv 把 vars 严格展开成 "KEY=VALUE" 列表，叠上语言适配器自己的
// 环境变量与 PORT。展开不了任何一个 ${VAR} 都直接报错、点名是哪个变量——
// 跟 local-debug.*.env 文件"展开不了就留着占位符"的宽松策略不同，见
// 设计决定第 4 条：这是真的要喂给进程的环境，不是给人看的排障文件。
func buildLocalEnv(
	ref resolver.Ref, vars []inject.Var, port int, cmdEnv []string, lookup func(string) (string, bool),
) ([]string, error) {
	out := make([]string, 0, len(vars)+len(cmdEnv)+1)
	for _, v := range vars {
		if v.ExistingSecretRef != "" {
			// existingSecret 引用的是 K8s Secret；mode: local 跟 mode: debug 一样
			// docker-only，这条引用在这两种模式下都没有对应的值（005 §5.6）。
			continue
		}
		value, missing := expandStrict(v.Value, lookup)
		if missing != "" {
			return nil, missingEnvVarError(ref, v.Name, missing)
		}
		out = append(out, v.Name+"="+value)
	}
	out = append(out, cmdEnv...)
	out = append(out, fmt.Sprintf("PORT=%d", port))
	return out, nil
}

// expandStrict 展开 raw 里的 ${VAR}；第一个展开不了的变量名通过 missing 返回。
func expandStrict(raw string, lookup func(string) (string, bool)) (value, missing string) {
	if lookup == nil || !strings.Contains(raw, "${") {
		return raw, ""
	}
	var firstMissing string
	expanded := localEnvVarRe.ReplaceAllStringFunc(raw, func(match string) string {
		name := match[2 : len(match)-1]
		if v, ok := lookup(name); ok {
			return v
		}
		if firstMissing == "" {
			firstMissing = name
		}
		return match
	})
	return expanded, firstMissing
}
```

- [ ] **Step 2: 把 `buildLocalEnv` 接进 `collectLocalComponents`**

`localComponentPlan`（Task 2）加一个字段：

```go
type localComponentPlan struct {
	Ref     resolver.Ref
	Service string
	Dir     string
	Command runcmd.Command
	Env     []string
	Port    int
}
```

`collectLocalComponents` 函数体里，找到这个组件对应的 `compose.LocalEnvFile`（跟算 `port` 时用的是同一个 `localEnvFiles` 切片，加一张按 `Ref` 查 `Vars` 的表），`cmd.CheckProgram()` 通过之后、组装 `localComponentPlan{}` 之前，插入：

```go
	varsOf := make(map[resolver.Ref][]inject.Var, len(localEnvFiles))
	for _, f := range localEnvFiles {
		varsOf[f.Ref] = f.Vars
	}
```

（这张表跟已有的 `portOf` 那张表放在一起建，在同一个 `for _, f := range localEnvFiles` 循环里一次填两张表也可以——两种写法都行，选哪种不影响正确性。）

`localComponentPlan{}` 的组装从：

```go
		out = append(out, localComponentPlan{
			Ref: ref, Service: step.Service, Dir: dir, Command: cmd, Port: port,
		})
```

改成：

```go
		env, err := buildLocalEnv(ref, varsOf[ref], port, cmd.Env, lookup)
		if err != nil {
			return nil, err
		}
		out = append(out, localComponentPlan{
			Ref: ref, Service: step.Service, Dir: dir, Command: cmd, Env: env, Port: port,
		})
```

- [ ] **Step 3: 新增 msgid + 译文**

```go
	CliUpMissingEnvVarFor = "cli.up_missing_env_var_for"
```

en: `"%[1]s needs %[2]s (referenced as ${%[2]s} in %[3]s), but it's not set in the environment or .env"`
zh: `"%[1]s 需要 %[2]s（在 %[3]s 里写成 ${%[2]s}），但当前环境变量与 .env 里都没有它"`

```go
func missingEnvVarError(ref resolver.Ref, envVarName, missingVarName string) error {
	return clierr.New(clierr.CodeConfigInvalid,
		i18n.T(msgid.CliUpMissingEnvVarFor, ref.String(), missingVarName, envVarName))
}
```

- [ ] **Step 4: 测试**

```go
func TestBuildLocalEnvExpandsResolvableVars(t *testing.T) {
	ref := resolver.Ref{ID: "people/basic", Version: "1.0.0"}
	vars := []inject.Var{{Name: "DATABASE_PASSWORD", Value: "${DB_PASSWORD}"}}
	lookup := func(name string) (string, bool) {
		if name == "DB_PASSWORD" {
			return "secret123", true
		}
		return "", false
	}

	env, err := buildLocalEnv(ref, vars, 9000, []string{"PYTHONUNBUFFERED=1"}, lookup)

	require.NoError(t, err)
	assert.Equal(t, []string{"DATABASE_PASSWORD=secret123", "PYTHONUNBUFFERED=1", "PORT=9000"}, env)
}

func TestBuildLocalEnvErrorsOnUnresolvableVar(t *testing.T) {
	ref := resolver.Ref{ID: "people/basic", Version: "1.0.0"}
	vars := []inject.Var{{Name: "DATABASE_PASSWORD", Value: "${DB_PASSWORD}"}}
	lookup := func(string) (string, bool) { return "", false }

	_, err := buildLocalEnv(ref, vars, 9000, nil, lookup)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "DB_PASSWORD")
	assert.Contains(t, err.Error(), "people/basic")
}

func TestBuildLocalEnvSkipsExistingSecretRef(t *testing.T) {
	ref := resolver.Ref{ID: "people/basic", Version: "1.0.0"}
	vars := []inject.Var{{Name: "API_KEY", ExistingSecretRef: "some-k8s-secret"}}

	env, err := buildLocalEnv(ref, vars, 9000, nil, func(string) (string, bool) { return "", false })

	require.NoError(t, err)
	assert.Equal(t, []string{"PORT=9000"}, env, "existingSecret 在 docker-only 的 local 模式下没有对应的值")
}
```

- [ ] **Step 5: 跑测试 + gofmt + 提交**

Run: `gofmt -l internal/cli`
Run: `go test ./internal/cli/ -v -run TestBuildLocalEnv`
Run: `go test ./internal/cli/`
Expected: 全部 `PASS`。

```bash
git add internal/cli/up_local.go internal/cli/up_local_test.go internal/msgid/cli.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "$(cat <<'EOF'
新增：本地进程环境变量的严格展开——展开不了直接报错，不留占位符

local-debug.*.env 文件展开不了 \${VAR} 时留着占位符（给人看的排障文件，
不该因为一个变量没配就让整个 up 失败），真正要喂给本地进程的环境不能
沿用这条宽松策略——字面量 \${DB_PASSWORD} 混进真实环境变量比报错更危险，
组件会拿着这几个字符去连库，得到一个牛头不对马嘴的连接失败。两条路径
共用 Task 1 新增的同一份 []inject.Var，只是展开阶段的容错策略不同。
collectLocalComponents 的 lookup 参数（Task 2 就留好了）这次真正接上，
localComponentPlan 多出 Env 字段供 Task 4 喂给 procsup.Spec.Env。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: 前台监管循环（`up_local.go` 下半部）

**背景：** 把 Task 2、3 的结论真正喂给 `procsup`：拿会话锁、按拓扑序 `Start` 每个本地组件、每个起来之后做一次性端口探测（硬失败/软警告/取消三种结果分别处理）、`Run` 阻塞到全部退出或 Ctrl+C、最后一屏只打崩溃的那几个。这是全计划里最核心、最需要真实进程验证的一步。

**Files:**
- Modify: `internal/cli/up_local.go`
- Modify: `internal/cli/up_local_test.go`
- Modify: `internal/msgid/cli.go`
- Modify: `internal/i18n/catalog_en.go`、`internal/i18n/catalog_zh.go`

**Interfaces:**
- Consumes：`internal/procsup`（Plan 2）、`internal/sessionlock`（Plan 2）、Task 2/3 的类型与函数。
- Produces：`func runLocalComponents(ctx context.Context, opts *Options, layout config.Layout, plans []localComponentPlan, crashLines int) error`——Task 5 从 `runUp` 里调用它。

- [ ] **Step 1: 写监管循环**

```go
// runLocalComponents 拿会话锁，按拓扑序拉起每个本地组件，阻塞到全部退出、
// 某个进程崩溃触发整个会话收尾、或者用户按了 Ctrl+C。
//
// 顺序上，这一步永远在 eng.Up() 之后调用（设计决定第 5 条）：容器已经在跑，
// 本地进程依赖的容器地址已经可用，不需要把容器和本地进程交织进同一个
// 拓扑序循环里。
func runLocalComponents(
	ctx context.Context, opts *Options, layout config.Layout,
	plans []localComponentPlan, crashLines int,
) error {
	if len(plans) == 0 {
		return nil
	}

	lock, err := sessionlock.Acquire(layout.SessionLockPath())
	if err != nil {
		var held *sessionlock.HeldError
		if errors.As(err, &held) {
			return clierr.New(clierr.CodeConfigInvalid,
				i18n.T(msgid.CliUpSessionAlreadyRunning, held.Info.PID))
		}
		return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpFailedToAcquireTheSession)).WithCause(err)
	}
	defer func() { _ = lock.Release() }()

	ctx, stop := signal.NotifyContext(ctx, procsup.StopSignals...)
	defer stop()

	widest := 0
	for _, p := range plans {
		if len(p.Service) > widest {
			widest = len(p.Service)
		}
	}
	sup, err := procsup.New(procsup.Options{Out: opts.Stdout, NameWidth: widest, TailLines: crashLines})
	if err != nil {
		return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpFailedToStartTheLocal)).WithCause(err)
	}
	defer sup.Shutdown()

	opts.Printf("\n%s\n", i18n.T(msgid.CliUpStartingLocalComponents, len(plans)))

	for _, p := range plans {
		proc, err := sup.Start(procsup.Spec{Name: p.Service, Argv: p.Command.Argv, Dir: p.Dir, Env: p.Env})
		if errors.Is(err, procsup.ErrStopped) {
			break // 已经有别的进程崩了，会话在收尾：别再启动新的
		}
		if err != nil {
			return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpFailedToStartTheLocal)).
				WithDetail(i18n.T(msgid.LabelComponent), p.Ref.String()).WithCause(err)
		}

		switch procsup.WaitListening(ctx, p.Port, proc.Done(), startupProbeTimeout) {
		case procsup.ProbeExited:
			// 进程自己没起来，是硬失败；也可能是别的进程崩了、这个是被叫停的——
			// 到 Run 返回之后统一从 Exits() 里看谁 Crashed()，这里不重复判断
		case procsup.ProbeTimedOut:
			opts.Printf("%s\n", i18n.T(msgid.CliUpWarningStillNotListeningOn, p.Ref.String(), p.Port))
		case procsup.ProbeCanceled:
			goto summarize
		case procsup.ProbeListening:
			opts.Printf("%s\n", i18n.T(msgid.CliUpListeningOnPort, p.Ref.String(), p.Port))
		}
	}

	sup.Run(ctx)

summarize:
	return renderCrashSummary(opts, sup.Exits())
}

// startupProbeTimeout 是等一个本地组件监听期望端口的上限——不是健康检查，
// 只是"命令跑起来了没有"的一次性检测（procsup 包文档）。
const startupProbeTimeout = 30 * time.Second

// renderCrashSummary 在最后一屏只打印崩溃的那几个进程，不被收尾时其余进程
// 的输出冲走（Plan 2 的设计决定 1）。被叫停的、干净退出的都不打印。
func renderCrashSummary(opts *Options, exits []procsup.Exit) error {
	var crashed []procsup.Exit
	for _, e := range exits {
		if e.Crashed() {
			crashed = append(crashed, e)
		}
	}
	if len(crashed) == 0 {
		return nil
	}

	opts.Printf("\n%s\n", i18n.T(msgid.CliUpTheFollowingLocalComponentsCrashed))
	for _, e := range crashed {
		how := i18n.T(msgid.CliUpExitCode, e.Code)
		if e.Signal != "" {
			how = i18n.T(msgid.CliUpKilledBySignal, e.Signal)
		}
		opts.Printf("   %s  %s  %s\n", e.Name, how, e.Duration.Round(time.Second))
		for _, line := range e.Tail {
			opts.Printf("      %s\n", line)
		}
	}
	return clierr.New(clierr.CodeEngineFailed, i18n.T(msgid.CliUpLocalComponentsCrashed, len(crashed)))
}
```

`p.Env` 是 Task 3 已经接好的字段，这里直接用。

- [ ] **Step 2: 新增 msgid（10 条）+ 两份译文**

十条新 msgid，`internal/msgid/cli.go` 里跟本计划前面几步加的那些放在一起：

```go
	CliUpSessionAlreadyRunning              = "cli.up_session_already_running"
	CliUpFailedToAcquireTheSession           = "cli.up_failed_to_acquire_the_session"
	CliUpFailedToStartTheLocal                = "cli.up_failed_to_start_the_local"
	CliUpStartingLocalComponents               = "cli.up_starting_local_components"
	CliUpWarningStillNotListeningOn             = "cli.up_warning_still_not_listening_on"
	CliUpListeningOnPort                         = "cli.up_listening_on_port"
	CliUpTheFollowingLocalComponentsCrashed       = "cli.up_the_following_local_components_crashed"
	CliUpExitCode                                  = "cli.up_exit_code"
	CliUpKilledBySignal                             = "cli.up_killed_by_signal"
	CliUpLocalComponentsCrashed                      = "cli.up_local_components_crashed"
```

`internal/i18n/catalog_en.go`：

```go
	msgid.CliUpSessionAlreadyRunning:        "this project already has a local session running (pid %[1]d) — stop it first, or switch to that terminal",
	msgid.CliUpFailedToAcquireTheSession:    "failed to acquire the session lock",
	msgid.CliUpFailedToStartTheLocal:        "failed to start the local process",
	msgid.CliUpStartingLocalComponents:      "Starting %[1]d local component(s) — press Ctrl+C to stop",
	msgid.CliUpWarningStillNotListeningOn:   "%[1]s still isn't listening on port %[2]d — it may just be slow to start",
	msgid.CliUpListeningOnPort:              "%[1]s  listening on port %[2]d",
	msgid.CliUpTheFollowingLocalComponentsCrashed: "The following local component(s) crashed:",
	msgid.CliUpExitCode:                     "exit code %[1]d",
	msgid.CliUpKilledBySignal:               "killed by %[1]s",
	msgid.CliUpLocalComponentsCrashed:       "%[1]d local component(s) crashed",
```

`internal/i18n/catalog_zh.go`：

```go
	msgid.CliUpSessionAlreadyRunning:        "这个项目已经有一个本地会话在跑（PID %[1]d）——先停掉它，或者切到那个终端",
	msgid.CliUpFailedToAcquireTheSession:    "拿会话锁失败",
	msgid.CliUpFailedToStartTheLocal:        "启动本地进程失败",
	msgid.CliUpStartingLocalComponents:      "正在启动 %[1]d 个本地组件——按 Ctrl+C 停止",
	msgid.CliUpWarningStillNotListeningOn:   "%[1]s 还没监听端口 %[2]d——可能只是启动得慢",
	msgid.CliUpListeningOnPort:              "%[1]s  已监听端口 %[2]d",
	msgid.CliUpTheFollowingLocalComponentsCrashed: "以下本地组件崩溃了：",
	msgid.CliUpExitCode:                     "退出码 %[1]d",
	msgid.CliUpKilledBySignal:               "被 %[1]s 杀死",
	msgid.CliUpLocalComponentsCrashed:       "%[1]d 个本地组件崩溃了",
```

- [ ] **Step 3: 真实进程的端到端测试**

`internal/cli/root.go` 的 `Run` 只会 `root.ExecuteC()`，没有任何办法从测试注入一个可取消的 `context.Context`——真实调用链上 `cmd.Context()` 永远是 `context.Background()`。这意味着"起一个长期运行的进程、测试主动模拟 Ctrl+C 去取消它"这条路在现有测试基础设施下走不通，**不需要为了这一步单独给 `Run` 加一个 `RunContext` 变体**——用两个会自己在短时间内终止的一次性小 Go 程序即可，完全不需要外部取消：

```go
// listenThenExitCleanly：先监听（net.Listen 是同步调用，返回时端口已经绑定，
// procsup.WaitListening 一定能探测到），再睡一小会儿、正常退出（code 0）。
// 睡一会儿而不是直接退：给探测留出确定能命中的窗口，不用靠巧合的调度时序。
const listenThenExitCleanly = `package main

import (
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	l, err := net.Listen("tcp", ":"+os.Getenv("PORT"))
	if err != nil {
		os.Exit(2)
	}
	go http.Serve(l, nil)
	time.Sleep(500 * time.Millisecond)
	os.Exit(0)
}
`

// exitImmediately：完全不监听、立刻以非零退出码结束——procsup 会在
// exited 通道关闭时立刻返回 ProbeExited，不需要等到探测超时。
const exitImmediately = `package main

import "os"

func main() { os.Exit(1) }
`

// localComponentPlansFor 搭一个真实项目、真的跑一遍解析+生成，返回
// collectLocalComponents 的结果——这是 Task 5 把 runLocalComponents 接进
// runUp 之前，Task 4 独立验证它的路子：**不能**像下面两条测试最初设想的
// 那样直接调 `runWithEngine(t, eng, f.Dir, "up")`——那要等 Task 5 把
// runLocalComponents 接进 runUp 之后才走得通，Task 4 自己的产出是
// runLocalComponents 这个函数本身，直接调它，不通过整条命令。
func localComponentPlansFor(t *testing.T, f *projectFixture, mainGo string) (*Options, []localComponentPlan) {
	t.Helper()
	writeTree(t, workspace.SourceDir(f.Layout, "demo/hello"), map[string]string{
		"go.mod":  "module example.com/hello\n",
		"main.go": mainGo,
	})

	cfg := f.parsed(t)
	graph, states, err := resolveTopology(context.Background(), newTopologyClient(t, f, cfg), cfg)
	require.NoError(t, err)
	order, err := resolver.Order(graph.Subgraph(states.Running()))
	require.NoError(t, err)
	env, err := inject.Build(cfg, graph, states)
	require.NoError(t, err)
	genResult, err := compose.Generate(cfg, graph, states, env, compose.Options{Engine: compose.EngineDocker})
	require.NoError(t, err)

	plans, err := collectLocalComponents(f.Layout, cfg, graph, order, genResult.LocalEnvFiles, envLookup(f.Dir))
	require.NoError(t, err)
	require.Len(t, plans, 1)

	opts := &Options{WorkDir: f.Dir, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	return opts, plans
}

func TestRunLocalComponentsStartsARealComponentAndItListens(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("这台机器没有 go 工具链")
	}
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	opts, plans := localComponentPlansFor(t, f, listenThenExitCleanly)

	err := runLocalComponents(context.Background(), opts, f.Layout, plans, 20)

	out := opts.Stdout.(*bytes.Buffer).String()
	require.NoError(t, err, out)
	assert.Contains(t, out, "demo-hello-1-0-0")
	assert.Contains(t, out, "listening on port")
}

func TestRunLocalComponentsReportsACrash(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("这台机器没有 go 工具链")
	}
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	opts, plans := localComponentPlansFor(t, f, exitImmediately)

	err := runLocalComponents(context.Background(), opts, f.Layout, plans, 20)

	out := opts.Stdout.(*bytes.Buffer).String()
	require.Error(t, err)
	assert.Contains(t, out, "demo-hello-1-0-0")
	assert.Contains(t, out, "exit code 1")
}
```

写出来才发现的第二处细节：`runLocalComponents` 自己打印的状态行（"正在启动"、"已监听端口"、崩溃汇总）最初写的是 `p.Ref.String()`（"demo/hello@1.0.0"），但 `procsup.Spec.Name` 传的是 `p.Service`（"demo-hello-1-0-0"，版本化服务名）——同一个组件在自己的输出前缀里叫一个名字、在状态行里又叫另一个名字，两者对不上。统一改成 `p.Service`：跟 `procsup` 给进程输出加的前缀、容器组件在 `reportStarted` 里的汇报，用的是同一个身份标识。`detectionError`/`programMissingError`/`checkLocalSources` 这几处指向 `component.yaml` 该改哪一行的提示不受影响，那些地方用 `ref.String()`/组件 ID 是因为它们说的是配置文件里的那一行，不是运行中的这个进程。

`TestRunLocalComponentsStartsARealComponentAndItListens` 顺带验证了一件 Task 4 Step 1 代码里容易出岔子的事：干净退出（code 0）不该被 `renderCrashSummary` 当成崩溃，`runLocalComponents` 必须返回 `nil`。两条测试都额外用 `go test -race -count=3` 单独跑过，确认干净——这是全计划里 goroutine/信号处理最密集的一段代码。

- [ ] **Step 4: 跑测试 + gofmt + 提交**

Run: `gofmt -l internal/cli`
Run: `go test ./internal/cli/ -v -run TestUpStartsARealModeLocalComponentAndItListens\|TestUpReportsACrashedLocalComponent`
Run: `go test ./internal/cli/`
Run: `go test -race ./internal/cli/`
Expected: 全部 `PASS`。

```bash
git add internal/cli/up_local.go internal/cli/up_local_test.go internal/msgid/cli.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "$(cat <<'EOF'
新增：mode: local 的前台监管循环——真正把进程拉起来

拿会话锁、按拓扑序 Start、每个起来后做一次性端口探测（硬失败/软警告/
取消三种结果分别处理）、Run 阻塞到全部退出或 Ctrl+C、最后一屏只打崩溃的
那几个进程（procsup 库的既有承诺，这里只是第一次真正调用它）。用仓库
自带的真实固件组件端到端验证，不是只测到探测那一步为止。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: 接进 `runUp`——容器起来之后再起本地进程

**背景：** 把前四个任务的成果接进 `runUp` 的主流程：`--dry-run` 只做到探测+校验（不真的 `Start`，但要把探测出的命令打印出来，让"看看会发生什么"名副其实）；真启动时，`eng.Up()` 成功之后再调 `runLocalComponents`。

**Files:**
- Modify: `internal/cli/up.go`
- Modify: `internal/cli/up_local.go`（`renderLocalComponentCommands`）
- Modify: `internal/cli/up_test.go`
- Modify: `internal/cli/up_dryrun_test.go`
- Modify: `internal/msgid/cli_up.go`（实际文件名，`cli.go` 是本计划早前草稿的笔误，Task 1 执行时已发现并沿用至今）
- Modify: `internal/i18n/catalog_en.go`、`internal/i18n/catalog_zh.go`

**Interfaces:**
- Consumes：Task 2–4 的全部函数。
- Produces：无新符号，这一步是把已经写好的部分接线。

- [ ] **Step 1: `upPlan` 加字段，`buildUpPlan` 收集本地组件**

`upPlan` 结构体（第 66-86 行）加两个字段：

```go
	// localComponents 是本次要真正拉起的 mode: local 组件（按拓扑序）。
	localComponents []localComponentPlan
	// crashLines 是 --crash-lines 的值，从 upOptions 原样抄过来——upPlan 本来
	// 就是"这次 up 要做什么的全部结论"，start 不接收 upOptions，靠这里传下去。
	crashLines int
```

`buildUpPlan` 里构造 `plan` 那一行（第 169 行）加上 `crashLines`：

```go
	plan := &upPlan{layout: layout, cfg: cfg, kubeContext: contextOf(cfg, flags.kubeContext), crashLines: flags.crashLines}
```

`buildUpPlan`（`plan.collectTargets(order)` 之后）：

```go
	plan.collectTargets(order)
	// plan.generated 只在 docker 目标下才有值（k8s 目标 generate() 只填 plan.k8s，
	// 见上面 generate 的实现）；k8s 目标下 mode: local 已经在配置解析阶段被拒绝
	// （005 §5.6：docker only），所以这里判空跳过既不会漏掉真实的 local 组件，
	// 也避免对 nil 的 plan.generated 取字段直接 panic。
	if plan.generated != nil {
		plan.localComponents, err = collectLocalComponents(
			layout, cfg, plan.graph, order, plan.generated.LocalEnvFiles, envLookup(opts.WorkDir))
		if err != nil {
			return nil, err
		}
	}
```

**执行时发现原草稿有一处真的会 panic 的 bug**：草稿原本直接写 `collectLocalComponents(..., plan.generated.LocalEnvFiles, ...)`，没有判空。Go 的参数求值顺序是"调用前先求出全部实参"——`plan.generated.LocalEnvFiles` 这个表达式本身就要先对 `plan.generated` 解引用取字段，`k8s` 目标下 `plan.generated` 是 nil，这一步在**进函数之前**就已经空指针 panic 了，`collectLocalComponents` 内部"用 `localMode` 提前 `return nil, nil`"的判断根本救不到——那时候已经晚了，实参已经崩在求值阶段。原草稿那句"不要假设它天然安全"猜对了方向，但猜错了修法：不是调整函数内部顺序，是调用点要判空。改成 `if plan.generated != nil` 包住整段调用，k8s 目标（`mode: local` 在这条路径下必然不存在）直接跳过，`plan.localComponents` 保持 nil，后续 `renderLocalComponentCommands`/`runLocalComponents` 对 nil/空切片各自都已经是"直接返回"的第一行，不需要再处理。

- [ ] **Step 2: `--dry-run` 下打印探测出的命令**

`runUp`（第 138-144 行，`if flags.dryRun` 分支里，`renderUpgradeSummary` 之后）加一段：

```go
		renderLocalComponentCommands(opts, plan.localComponents)
```

```go
// renderLocalComponentCommands 在 --dry-run 下告诉使用者每个 mode: local
// 组件探测出的启动命令是什么——"看看会发生什么"这条命令的整个意义所在，
// 本地组件不该是唯一说不清楚的部分。
func renderLocalComponentCommands(opts *Options, plans []localComponentPlan) {
	if len(plans) == 0 {
		return
	}
	opts.Printf("\n%s\n", i18n.T(msgid.CliUpLocalComponentsWouldStart))
	for _, p := range plans {
		opts.Printf("   %s  %s\n", p.Ref, strings.Join(p.Command.Argv, " "))
	}
}
```

- [ ] **Step 3: 真启动时接进 `start`**

`start` 函数（第 505-527 行）末尾，`reportStarted` 成功返回之后，追加调用 `runLocalComponents`：

```go
func start(
	ctx context.Context, opts *Options, eng engine.Engine, plan *upPlan,
	file, pruneSelector string,
) error {
	project := engine.ProjectName(plan.cfg.Project)

	opts.Printf("\n%s\n", i18n.T(msgid.CliUpStarting, eng.Name()))
	if err := eng.Up(ctx, engine.UpRequest{
		File: file, Project: project, ProjectDir: opts.WorkDir, Services: plan.services,
		PruneSelector: pruneSelector,
	}); err != nil {
		return engineFailure(i18n.T(msgid.CliUpStart), err)
	}

	statuses, err := eng.Status(ctx, project)
	if err != nil {
		opts.Printf("%s\n", i18n.T(msgid.CliUpTheContainerStateCouldNot, clierr.As(err).Message))
		opts.Printf("%s\n", i18n.T(msgid.CliUpK8sCheckAgainWithBrickkitStatus))
		return nil
	}
	if err := reportStarted(opts, plan, statuses); err != nil {
		return err
	}

	return runLocalComponents(ctx, opts, plan.layout, plan.localComponents, plan.crashLines)
}
```

`start` 目前不接收 `upOptions`，改它的签名去传一个新参数会牵动全部调用点（`upK8s` 那条分支也调用类似的收尾函数）——`plan.crashLines`（Step 1 已经加好）正是为了绕开这个改动面。

- [ ] **Step 4: 测试**

在 `up_dryrun_test.go` 加一条，确认 `--dry-run` 打印探测出的命令（放在
`TestUpDryRunWithoutLocalComponentWritesNoEnvFile` 之后，"错误路径"分隔线
之前；`workspace` 需要新增 import）：

```go
// "看看会发生什么"这条命令的整个意义所在：mode: local 组件不该是唯一
// 说不清楚会发生什么的部分——它不生成容器，dry-run 原本对它完全沉默。
func TestUpDryRunShowsTheDetectedLocalCommand(t *testing.T) {
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	writeTree(t, workspace.SourceDir(f.Layout, "demo/hello"), map[string]string{
		"go.mod":  "module example.com/hello\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "demo/hello")
	assert.Contains(t, r.stdout, "go run .")
}
```

在 `up_test.go` 补一条端到端（复用 Task 4 Step 3 已经写好的 `listenThenExitCleanly`
固件——同一个包内，不需要重新定义）：容器组件 + `mode: local` 组件混在同一个
项目里，`up`（真启动，非 dry-run）既能看到容器启动成功的汇报，又能看到本地
组件"已监听端口"的输出——顺序上容器的汇报必须先出现（放在
`TestUpModeLocalComponentIsNotAWorkloadTarget` 之后；`up_test.go` 需要新增
`os/exec` import）：

```go
// Task 5：容器与本地进程混部同一个项目——容器由引擎负责，mode: local 组件由
// runLocalComponents 负责，start() 里先 reportStarted 后 runLocalComponents，
// 输出顺序必须体现这一点：容器的汇报先出现，本地组件的"已监听端口"后出现。
func TestUpMixesContainerAndLocalComponents(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("这台机器没有 go 工具链")
	}
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")
	writeTree(t, workspace.SourceDir(f.Layout, "people/basic"), map[string]string{
		"go.mod":  "module example.com/basic\n",
		"main.go": listenThenExitCleanly,
	})
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
    mode: local
  - id: erp/backend
    version: 1.0.0
`)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	containerIdx := strings.Index(r.stdout, "erp-backend-1-0-0")
	localIdx := strings.Index(r.stdout, "listening on port")
	require.NotEqual(t, -1, containerIdx, r.stdout)
	require.NotEqual(t, -1, localIdx, r.stdout)
	assert.Less(t, containerIdx, localIdx, "容器汇报必须先于本地组件的输出出现")
}
```

**执行时顺带验证的一件事**：`TestUpModeLocalComponentIsNotAWorkloadTarget`
（Task 2 写的、这次没有改动它本身）用的 `people/basic` 固件是空 `main()`、
立刻以退出码 0 结束、不监听任何端口。Task 5 接线之后，这条测试原本只验证
"mode: local 不混进引擎目标列表"的老断言，现在会**顺带真的**通过
`runLocalComponents` 启动一次这个组件——进程秒退、`WaitListening` 走到
`ProbeExited` 分支、`sup.Run` 因为没有更多进程要等而立刻返回、退出码 0 不算
`Crashed()`，`runLocalComponents` 返回 `nil`。重跑这条测试确认仍然
`PASS`，证明"干净退出的本地组件不会让 `up` 本身失败或挂起"这条行为对已有
测试也成立，不是只在 Task 4 新写的固件上才对。

- [ ] **Step 5: 跑测试 + gofmt + 提交**

Run: `gofmt -l internal/cli`
Run: `go test ./internal/cli/`
Run: `go test -race ./internal/cli/`
Expected: 全部 `PASS`。

```bash
git add internal/cli/up.go internal/cli/up_local.go internal/cli/up_test.go internal/cli/up_dryrun_test.go internal/msgid/cli_up.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go docs/superpowers/plans/2026-09-22-up-local-supervision.md
git commit -m "$(cat <<'EOF'
新增：接进 runUp——容器起来之后再启动本地进程

--dry-run 打印每个 mode: local 组件探测出的启动命令（"看看会发生什么"这条
命令的整个意义所在，本地组件不该是唯一说不清楚的部分）；真启动时
eng.Up() 成功、状态汇报完之后才调 runLocalComponents——容器已经在跑，
本地进程依赖的容器地址已经可用。

buildUpPlan 里调 collectLocalComponents 时判空 plan.generated（本计划
Step 1 已同步改写并解释原因）：k8s 目标下 plan.generated 是 nil，原草稿
直接取 plan.generated.LocalEnvFiles 会在实参求值阶段就 panic，跟
collectLocalComponents 内部的判断顺序无关。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: 全量验证与收尾

**背景：** 跑一遍全仓库测试、`-race`、`make lint`，确认没有相互破坏，回写执行结果。

**Files:**
- Modify: `docs/superpowers/plans/2026-09-22-up-local-supervision.md`（本文件）

- [ ] **Step 1: 全仓库测试**

Run: `go test ./...`
Expected: 全部 `ok`。

- [ ] **Step 2: `-race`**

Run: `go test -race ./internal/compose/ ./internal/config/ ./internal/cli/`
Expected: 干净——这次特别要看 `internal/cli`，前台监管循环本身涉及 goroutine（`procsup.watch`）与信号处理，是全计划里 race 风险最高的一步。

- [ ] **Step 3: 交叉编译确认**

Run: `make check-cross-build`
Expected: 退出码 0（本计划全程只用跨平台标准库调用，`signal.NotifyContext` + `procsup.StopSignals` 在 Plan 2 里已经验证过三平台都编得过）。

- [ ] **Step 4: 完整 `make lint`**

Run: `make lint > /tmp/lint.log 2>&1; echo "exit=$?"`
Expected: `exit=0`。

- [ ] **Step 5: 手动跑一遍真实场景（不是单元测试，是眼见为实）**

在一个临时目录里用 `demo/hello`（或任何仓库自带的最小 Go 固件）手搭一个只有它自己、`mode: local` 的项目，真的跑 `brickkit up`，眼睛看着：
- 命令探测出了 `go run .`；
- 进程真的启动、终端上能看到它的输出（带组件名前缀）；
- `Ctrl+C`（或者发 `SIGTERM`）能让它体面退出，回到 shell；
- 故意让它崩溃（改一行代码 `os.Exit(1)`）一次，确认最后一屏只打印了这一个组件的崩溃现场，没有被别的输出冲走。

这一步没有对应的自动化测试断言，是给执行者的一条明确指令：**在把这次改动标记为完成之前，亲眼看它跑起来一次**，不能只靠单元测试的绿灯。

- [x] **Step 6: 回写执行结果，提交**

在本文件末尾追加"执行结果与遗留"一节，记录：六个任务的提交哈希、真实场景手动验证的结果、任何偏离计划之处、留给 Plan 4c/4d 的清单。

```bash
git add docs/superpowers/plans/2026-09-22-up-local-supervision.md
git commit -m "$(cat <<'EOF'
文档：Plan 4b（up 前台监管模式）的执行结果回写进计划

六个任务全部完成：LocalEnvFile.Vars 与会话锁路径、本地源码校验与命令探测、
严格展开环境变量、前台监管循环、接进 runUp、全量验证。mode: local 组件
现在真的会被拉起来了。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Self-Review Checklist（执行完 6 个任务后逐条核对）

- [x] `go test ./...` 全绿；`go test -race` 对 `compose`/`config`/`cli` 干净
- [x] `make check-cross-build`、完整 `make lint` 都是 `exit=0`
- [x] `mode: local` 组件没有本地源码目录时，`up --dry-run` 就能报错（不用等真启动）
- [x] `LocalEnvFile.Vars` 与 `LocalEnvFile.Content` 用的是同一份 `[]inject.Var`，没有漂移
- [x] 本地进程的环境变量展开是严格的（展开不了直接报错），`local-debug.*.env` 文件的展开策略没有被改动（仍然宽松）
- [x] `PORT` 被正确注入每个本地进程（值等于该组件在宿主机上分配到的端口，跟依赖方看到的 `*_ENDPOINT` 端口一致）
- [x] 容器先起、本地进程后起；本地进程之间按拓扑序
- [x] 一个本地进程崩了，整个会话收尾，其余本地进程被体面停掉，容器不受影响；最后一屏只打印崩溃的那几个，且只打一次
- [x] `--crash-lines` 只在真的传了（`Flags().Changed`）且项目没有 `mode: local` 组件时警告；`0` 值只打印崩溃信息不带输出行
- [x] Ctrl+C 能让整个会话体面收尾并正常退出
- [x] 会话锁：同一个项目不能有两个前台会话同时跑，第二个会话报错点名第一个的 PID
- [x] `.gitignore` 模板新增了 `.brickkit/session.lock`
- [x] 至少手动跑通一次真实场景（Task 6 Step 5），不是只看单元测试绿灯
- [x] `internal/procsup`/`internal/sessionlock`/`internal/runcmd` 三个库本身没有被本计划修改一行——它们已经独立验证过，本计划只是第一次真正调用它们
- [x] `AGENTS.md`、`docs/en/`、`docs/zh/` 没有任何改动（Plan 4d 的事）

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-09-22-up-local-supervision.md`. Two execution options:

1. **Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration
2. **Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

选哪种？

---

## 执行结果与遗留（2026-09-22，Inline 执行）

六个任务全部完成，`mode: local` 组件现在真的会被 `brickkit up` 拉起来、
监管、崩溃时正确收尾。提交哈希（按顺序）：

| Task | 内容 | 提交 |
| --- | --- | --- |
| 1 | `LocalEnvFile.Vars`、会话锁路径、`--crash-lines` 旗位骨架 | `05f8356` |
| 2 | 本地源码校验与命令探测 | `843a9e2` |
| 3 | 严格展开环境变量 + `PORT` 注入 | `012609b` |
| 4 | 前台监管循环（session 锁 + 崩溃汇总） | `427b89e` |
| 5 | 接进 `runUp` | `a9f5d0e` |
| 6（bugfix 1） | `LocalEnvFile.Mode`：mode: local 不再冒出 mode: debug 的话术 | `14788ca` |
| 6（bugfix 2，同一提交） | 全是 local 组件时跳过引擎，不再报 `no service selected` | `14788ca` |
| 6（bugfix 3） | `--crash-lines 0` 两层语义对不上的修复 | `6149762` |
| 6（测试补漏 1） | 两个 local 组件之间的拓扑启动顺序 | `3b22a28` |
| 6（测试补漏 2） | 会话锁冲突报错点名 PID | `3cebe3d` |

### 自动化验证

- `go test ./...`（`brickkit` 与 `market-server` 两个 Go module）全绿。
- `go test -race ./internal/compose/ ./internal/config/ ./internal/cli/` 干净。
- `make check-cross-build`：linux/darwin/windows 都编得过。
- 完整 `make lint`（这台机器没装 golangci-lint，回退到 `go vet`，**合并前要在装了它的环境再跑一次**）：`exit=0`，`internal` 覆盖率 93.3%（门槛 92%）。唯一的非阻断提示是"`up --crash-lines` 没有任何文档提到它"——符合预期，文档是 Plan 4d 的事。

### Task 6 Step 5：真机手动验证（不是单元测试，是眼见为实）

用真实 Docker + 真实 Go 工具链，在 `/tmp` 下手搭了一个独立项目（不是仓库固件），依次验证：

1. **单组件、`mode: local`，`--dry-run`**：探测出 `go run .`，且（修复前）曾冒出自相矛盾的"🔧 Local debugging (mode: debug)"段落——第一个真实 bug 由此发现。
2. **同一项目，真启动**：真进程起来、终端能看到带前缀的实时输出、`WaitListening` 探测到端口——过程中发现第二个真实 bug：`docker compose up` 对着一份 `services: {}` 的空文件报 `no service selected`，因为这个项目一个容器都不需要，`start()` 却无条件调了引擎。
3. **Ctrl+C（`SIGINT`）**：会话体面收尾，退出码 0（`timeout -s INT 5` 之所以自己报 124，只是因为它介入了计时——真正说明问题的是 brickkit 自己的结构化日志行 `"exit_code":0`）。
4. **单组件故意崩溃**（`os.Exit(1)`）：最后一屏只打印这一个组件的崩溃现场（退出码、耗时、尾部输出），且只打一次。
5. **两组件（一个崩溃、一个存活）**：崩溃的那个被打进最后一屏，存活的那个收到停止信号后打印"friend shutting down cleanly"、干净退出，**没有**被算进崩溃汇总——验证了 Plan 2 的"一个崩，其余体面收尾，只打崩溃的那几个"这条设计承诺在真实多进程场景下成立。
6. **`--crash-lines 0`**：这一步复现出了第三个真实 bug——最后一屏应该只打崩溃信息不带输出行，实际却打出了默认的 20 行，因为 `procsup.Options.TailLines <= 0` 在 procsup 自己的约定里是"取默认值"，跟 CLI 帮助文本"0 = 不带输出行"的承诺正好相反。修复后重新手动验证，最后一屏确实只剩崩溃信息那一行。

### 三个真实 bug 的共同点

三个都不是"漏写了一行代码"，而是**两个本来各自独立、各自合理的假设撞在了一起**，只有真实运行时才会现形：

1. `LocalEnvFile` 被两种 mode（debug/local）共用，但渲染层一直假设"能拿到 `LocalEnvFile` 就是 debug"——这个假设在 Plan 4a 把 `mode: local` 路由进同一个桶之前一直成立，Plan 4a 让它不再成立，但没人回头检查这几处显示逻辑。
2. `plan.services` 为空这件事此前只在"level 组件都是 mode: debug"时理论上可能发生（`mode: debug` 从设计上就没打算被大规模拿来单独 up 一个纯调试项目），`mode: local` 让"整个项目一个容器都不需要"变成了一个真实、常见的场景，而 `start()` 从来没为这个场景准备过。
3. `procsup.Options.TailLines` 的"`<=0` 取默认值"是它作为通用监管库对自己调用方的合理默认，但 `--crash-lines` 后来被赋予了一个更具体的、面向最终用户的承诺（"0 就是 0"），两层语义在同一个整数上打了架。

三处都按 CLAUDE.md 的原则修在了产生不一致的那一层，而不是在调用点绕过去：第 1、2 处修在 CLI 层自己的判断逻辑里（`compose.LocalEnvFile.Mode` 字段、`runUp` 的 `len(plan.services) == 0` 分支），第 3 处也刻意没有去改 `internal/procsup`（它是独立验证过的通用库，"0/未设置给个默认值"对它的其他潜在调用方仍然是合理惯例）——CLI 自己在 `renderCrashSummary` 里兑现只属于它自己的那条承诺。三处都补了回归测试，并用"临时还原成 bug → 确认测试变红 → 复原"的方式逐条验证过测试真的能守住修复。

### 偏离计划之处

- Task 4 的 Step 3 测试从计划草稿设想的"跑完整 `up` 命令"改成了直接调 `runLocalComponents`（`runLocalComponents` 要到 Task 5 才接进 `runUp`，这是执行时自己发现、写进 Task 4 提交说明的计划时序问题）。
- Task 5 的 `buildUpPlan` 调用点加了 `plan.generated != nil` 判空——计划原草稿会在 k8s 目标下对 nil 指针取字段直接 panic（Go 的参数在调用前就会被求值，`collectLocalComponents` 函数内部"没有 local 组件就提前返回"的判断根本来不及生效）。
- Task 6 新增了三处计划里没有预见到的真实 bug 修复（见上）+ 两条查漏的测试（本地组件间拓扑序、会话锁冲突报错），都在最后一步"全量验证"的精神范围内——Step 5 明确要求真机跑一遍，这些正是那一步应该发现的问题。

### 留给 Plan 4c/4d 的清单

- **Plan 4c（调试挂载）**：待写，Plan 3 的实验已经推翻了原 spec"环境变量注入就够"的假设，需要单独设计（Maven 用 `-Dspring-boot.run.jvmArguments=...`、Gradle 用 `--debug-jvm`、Node 目前没有不改探测策略就能做到的安全办法）。
- **Plan 4d（展示层 + 文档）**：
  - `internal/cli/lifecycle.go`/`graph.go`/`status.go` 三处纯展示的 `ModeDebug` 判断仍未加 `ModeLocal`（Plan 4a 留下的遗留，已确认不影响真机命令执行，只是展示不准确）。
  - `--crash-lines` 目前没有任何文档提到它（`make lint` 的非阻断提示）；`mode: local` 整体的用户向文档（`docs/{en,zh}`、AGENTS.md 相关小节）也还没写。
  - `component.yaml` 参考文档的 `local` 字段一节 Plan 4a 已经提前写好，Plan 4d 不用重写。

用户已确认继续在这个分支（`worktree-mode-field-migration`）上写，不合并、不推送——是否收尾这个分支交由下一步的 `finishing-a-development-branch` 环节向用户确认。
