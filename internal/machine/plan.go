// Package machine 算出一台机器上的部署怎么跑：哪些组件进容器、哪些作为本机进程跑、外壳承载谁、
// 迁移怎么串、宿主机端口怎么分、地址怎么改写、哪些配置项以文件交付。
//
// 它是纯函数，不碰磁盘、不调引擎。compose（compose.yaml）与 Quadlet（systemd 单元）两个渲染器都只读它的结果，
// 所以两种文件里的迁移顺序、启动等待、外壳别名、端口不会各算各的。
package machine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
)

// Options 是计划选项。
type Options struct {
	// Root 是项目根：file:// 相对它解析。
	Root string
	// Lookup 解析 ${VAR}，用于本地进程的环境变量文件与 0600 env 文件里 file:// 以外的求值。
	//
	// compose 文件本身刻意保留占位符：那份文件会被人打开看、进 git diff，
	// 明文密码进去就等于泄露，而 docker compose 会自己从 .env 展开。
	// local-debug 文件不一样——它是给 IDE 读的（VS Code 的 envFile、
	// IntelliJ 的 EnvFile 插件），**它们都不做变量替换**，留着占位符，
	// IDE 里的进程就会拿着字面量 "${PG_PASSWORD}" 去连库。
	Lookup func(name string) (string, bool)
	// HostAliasOnLoopback 报告这台机器自己能不能把 host.docker.internal 解析到回环地址（见 plan.onHostMachine）。
	// 只在本机进程的值里真出现这个名字时才问，最多问一次。为空时当不能：换成 localhost。
	HostAliasOnLoopback func() bool
}

// HostAliasUse 说本机进程拿到的 host.docker.internal 是怎么处理的。
type HostAliasUse int

const (
	// HostAliasUnused：本机进程的值里没有这个名字。
	HostAliasUnused HostAliasUse = iota
	// HostAliasRewritten：本机解析不了它（或解析到的不是回环地址），换成了 localhost。
	HostAliasRewritten
	// HostAliasKept：本机把它解析到回环地址，原样给——与容器里是同一个名字。
	HostAliasKept
)

// ============================================================
// 生成计划
// ============================================================

// Workload 是一个要渲染成 service 的组件。
type Workload struct {
	Ref      resolver.Ref
	Service  string
	Manifest *manifest.Manifest
	Entry    deployfile.Entry
	Env      inject.Component
}

// plan 是整份文件的生成计划。
type Plan struct {
	proj   *project.Project
	graph  *resolver.Graph
	states *cascade.Result
	root   string
	lookup func(string) (string, bool)
	// hostAliasOnLoopback / hostAlias：本机进程的值里 host.docker.internal 怎么处理，第一次遇到时定下（onHostMachine）。
	hostAliasOnLoopback func() bool
	hostAlias           HostAliasUse

	// secretFiles 是以文件交付的配置项（secretfiles.go）：所属组件的服务名 → 键 → 文件内容。
	secretFiles map[string]map[string][]byte

	// components 是本次要渲染的组件（已排除 mode: debug），按服务名排序。
	components []Workload
	// locals 是 mode: debug 的组件：不生成容器，但要参与端口分配与 env 文件生成。
	locals []localComponent
	// served 是 外壳成员：不生成自己的容器/迁移，但要走它专属的
	// 几条提醒（见 servedby.go）。
	served []servedComponent
	// memberMigrations 是被外壳承载、又声明了 migration 的成员：它们没有主容器，
	// 迁移却仍然单独跑，用成员自己的镜像与配置。
	memberMigrations []Workload
	// shellAliases 是外壳的服务名 → 它要挂的额外网络别名（收编成员的
	// 版本化服务名），供 componentService 渲染 networks 段。
	shellAliases map[string][]string
	// rendered 是最终会出现在文件里的 service 名集合。
	rendered map[string]bool
	// migrationAfter 是"这个组件的迁移要等哪个迁移先成功结束"：
	// 服务名 → 前一个版本的迁移 service 名。同一组件 ID 的多个版本共用一个库，
	// 必须串起来（详见 chainMigrations）。
	migrationAfter map[string]string

	// 宿主机端口台账（详见 local.go）。
	//
	//	localPort      服务名 → local 组件在宿主机上监听的端口
	//	exposedPort    服务名 → expose 映射到宿主机的端口（外壳替成员开放的，记在成员自己的服务名下）
	//	debugPort      服务名 → 纯为本地调试而映射的宿主机端口
	//	debugExtraPort 服务名 → （容器额外端口 → 宿主机端口）
	localPort      map[string]int
	exposedPort    map[string]int
	debugPort      map[string]int
	debugExtraPort map[string]map[int]int

	// shellMemberHostPorts 是"外壳自己的 service 名 → 还要额外发布哪些端口"。
	// 这些映射不是外壳自己的端口，而是它某个 外壳成员的端口——成员
	// 没有自己的 compose service，映射只能落在外壳身上（详见 local.go
	// mapDependencyToHost）。
	shellMemberHostPorts map[string][]hostPortMapping

	warnings []*clierr.Error
}

// hostPortMapping 是一条"宿主机端口 → 容器端口"映射。
type hostPortMapping struct {
	hostPort      int
	containerPort int
}

// Build 算出一台机器上这次怎么跑：哪些组件进容器、哪些是本机进程、外壳承载谁、迁移怎么串、
// 宿主机端口怎么分、地址怎么改写。compose 与 Quadlet 两个渲染器都只读它。
func Build(
	proj *project.Project, graph *resolver.Graph, states *cascade.Result,
	env *inject.Result, opts Options,
) (*Plan, error) {
	p := &Plan{
		proj: proj, graph: graph, states: states,
		root: opts.Root, lookup: opts.Lookup, hostAliasOnLoopback: opts.HostAliasOnLoopback,
		rendered:       map[string]bool{},
		migrationAfter: map[string]string{},
		localPort:      map[string]int{},
		exposedPort:    map[string]int{},
		debugPort:      map[string]int{},
		debugExtraPort: map[string]map[int]int{},
		shellAliases:   map[string][]string{},

		shellMemberHostPorts: map[string][]hostPortMapping{},
		secretFiles:          map[string]map[string][]byte{},
	}

	// 以文件交付的配置项先换成路径（secretfiles.go）：之后的每一步看到的都是普通变量
	env, err := p.mountSecretFiles(env)
	if err != nil {
		return nil, err
	}

	envByRef := map[resolver.Ref]inject.Component{}
	for _, c := range env.Components {
		envByRef[c.Ref] = c
	}

	for _, ref := range states.Running() {
		entry := proj.DeployEntry(ref.ID, ref.Version)
		node := graph.Node(ref)
		if node == nil {
			continue
		}
		service := manifest.ServiceName(ref.ID, ref.Version)

		if entry.IsBareProcess() {
			// mode: debug 与 mode: local 的组件都不生成容器——
			// 前者在宿主机 IDE 里跑，后者由 brickkit 自己拉起裸进程（Plan 4b 起才
			// 真正启动），但两者都仍然是"启动中"的组件，依赖方要能找到它。
			p.locals = append(p.locals, localComponent{
				Ref: ref, Service: service, Manifest: node.Manifest,
				Entry: entry, Env: envByRef[ref],
			})
			continue
		}
		// 被外壳承载的成员不生成自己的容器（外壳没跑时 HostOf 为假，成员回落成普通组件）
		if shellRef, hosted := states.HostOf(proj, ref); hosted {
			_, hasRepo := proj.LocalRepo(ref.ID)
			fromSource := hasRepo && proj.DeployEntry(shellRef.ID, shellRef.Version).IsBareProcess()
			p.served = append(p.served, servedComponent{
				Ref: ref, Service: service, Manifest: node.Manifest,
				Entry: entry, Shell: shellRef, Env: copyComponent(envByRef[ref]), FromSource: fromSource,
			})
			if node.Manifest != nil && node.Manifest.Migration != nil && !fromSource {
				p.memberMigrations = append(p.memberMigrations, Workload{
					Ref: ref, Service: service, Manifest: node.Manifest, Entry: entry, Env: envByRef[ref],
				})
			}
			continue
		}
		p.components = append(p.components, Workload{
			Ref:      ref,
			Service:  service,
			Manifest: node.Manifest,
			Entry:    entry,
			Env:      envByRef[ref],
		})
		p.rendered[service] = true
	}

	// 排序只为确定性：自动分配的端口依赖遍历顺序，
	// map 的随机顺序会让同一份配置每次生成出不同的端口号
	sort.Slice(p.components, func(i, j int) bool { return p.components[i].Service < p.components[j].Service })
	sort.Slice(p.locals, func(i, j int) bool { return p.locals[i].Service < p.locals[j].Service })
	sort.Slice(p.served, func(i, j int) bool { return p.served[i].Service < p.served[j].Service })
	sort.Slice(p.memberMigrations, func(i, j int) bool { return p.memberMigrations[i].Service < p.memberMigrations[j].Service })

	p.chainMigrations()

	if err := p.checkExposePorts(); err != nil {
		return nil, err
	}
	if err := p.assignHostPorts(); err != nil {
		return nil, err
	}
	if err := p.checkReplicaHostPorts(); err != nil {
		return nil, err
	}
	p.rewriteEndpointsForLocalDependencies()
	p.hostPathsForBareShellMembers()

	// 成员交给外壳的环境用计划里改写过的那一份（本地调试地址、裸进程外壳的 localhost 地址）
	groups, err := shell.Resolve(proj, graph, states, p.envForShells(env), opts.Lookup)
	if err != nil {
		return nil, err
	}
	p.applyShellGroups(groups)

	p.warnings = append(p.warnings, p.localMigrationWarnings()...)
	p.warnings = append(p.warnings, p.localExposeWarnings()...)
	p.warnings = append(p.warnings, p.bareShellExposeWarnings()...)
	p.warnings = append(p.warnings, p.localLabelWarnings()...)
	p.warnings = append(p.warnings, p.fallbackStandaloneWarnings()...)
	p.warnings = append(p.warnings, p.bareSkipWaitForWarnings()...)
	return p, nil
}

// chainMigrations 把**同一个组件 ID** 的多个版本的迁移按版本号串成一条链。
//
// # 为什么必须串
//
// 同一个组件的两个版本管的是同一份数据，它们的配置（无论共用 config/<id>.yaml，
// 还是各有一份带版本号的）通常指向同一个库；迁移状态表的主键是
// (component_id, version)，两个版本的 component_id 也是同一个。于是"两个迁移容器
// 同时对同一个库、用同一个身份跑迁移"完全是**平台自己生成出来的**——使用者在
// brickkit.yaml 里只是写了两行版本号。
//
// 迁移只增不改，所以高版本的迁移集合是低版本的超集。这在老库上
// 没问题（低版本发现已应用就跳过、干净退出），但在**空库**上两个容器都会去跑
// 那批重合的迁移——一个成功，另一个撞主键退出，那个版本的主服务永远停在
// Created。而且重跑一次就好（那时已经写进去了），错误指向数据库主键冲突，
// 与"我配了多版本"看不出任何关系。
//
// # 为什么只串同一个组件 ID
//
// 不同组件的 component_id 不同，主键 (component_id, version) 已经让它们互不
// 相干——那正是这个主键的设计目的。把它们也串起来只会平白拖慢
// `up`（迁移是所有组件的前置阻塞步骤），换不来任何东西。
//
// # 平台挡不住的那一半
//
// 这条链只作用于**这一次 up**。两个人同时 `brickkit up` 打同一个开发库、
// 或者 CI 与人撞上，平台一点办法没有——那要靠迁移工具自己的库级锁
// （迁移工具自己要保证并发安全）。
//
// 由低到高也只在空库上决定库的样子：从第二次 up 起每个版本的迁移都再跑一遍，
// 低版本的迁移面对的总是被高版本迁移过的库，组件必须把它当作已完成（文档的组件要求）。
// 所以从源码跑的迁移（local.migrationCommand，引擎跑完容器之后才跑）进不了这条链也不拦：
// 它排在同组件更高版本的容器迁移之后，与以后每次 up 的情形一样。
func (p *Plan) chainMigrations() {
	// 按组件 ID 归集有迁移的版本
	byID := map[string][]Workload{}
	for _, c := range append(append([]Workload{}, p.components...), p.memberMigrations...) {
		if c.Manifest.Migration == nil {
			continue
		}
		byID[c.Ref.ID] = append(byID[c.Ref.ID], c)
	}

	for _, versions := range byID {
		if len(versions) < 2 {
			continue
		}
		// 按**版本号**排，不是按服务名：字典序会把 10.0.0 排在 2.0.0 前面，
		// 于是先跑的是更新的那一版，而迁移是只增不改、必须由低到高的
		sort.Slice(versions, func(i, j int) bool {
			return manifest.CompareVersions(versions[i].Ref.Version, versions[j].Ref.Version) < 0
		})
		for i := 1; i < len(versions); i++ {
			p.migrationAfter[versions[i].Service] = MigrationService(versions[i-1].Service)
		}
	}
}

// MigrationService 是一个组件迁移容器的 service 名。CLI 在 up 失败后按它认出失败的迁移。
func MigrationService(service string) string { return service + "-migration" }

// checkExposePorts 检查宿主机端口冲突。
//
// 两个组件抢同一个宿主机端口时，docker 会在启动到第二个容器时才失败，
// 那时第一个已经起来了——不如在生成阶段就说清楚。
func (p *Plan) checkExposePorts() error {
	// 只记"是谁占了"：报错要点名两个组件。占的是不是使用者显式写的
	// exposePort 无所谓——两种情况的出路是同一条（给其中一个设 exposePort）
	claimed := map[int]string{}

	type claim struct {
		Ref      resolver.Ref
		hostPort int
	}
	var claims []claim
	for _, c := range p.components {
		if c.Entry.Expose {
			claims = append(claims, claim{c.Ref, exposeHostPort(c)})
		}
	}
	for _, s := range p.exposedMembers() {
		claims = append(claims, claim{s.Ref, memberExposeHostPort(s)})
	}
	for _, c := range claims {
		hostPort := c.hostPort
		if previous, taken := claimed[hostPort]; taken {
			return clierr.New(clierr.CodePortConflict,
				i18n.T(msgid.ComposeHostPortConflict, hostPort)).
				WithDetail(i18n.T(msgid.LabelComponent), previous).
				WithDetail(i18n.T(msgid.LabelComponent), c.Ref.ID+"@"+c.Ref.Version).
				WithDetailf(i18n.T(msgid.ComposeLabelHostPort), "%d", hostPort).
				WithHint(
					i18n.T(msgid.ComposeHintChangeExposePort),
					i18n.T(msgid.ComposeHintDropExpose),
				)
		}
		claimed[hostPort] = c.Ref.ID + "@" + c.Ref.Version
	}
	return nil
}

// checkReplicaHostPorts 拒绝"多个副本 + 宿主机端口"：一个宿主机端口只能映射给一个容器。
//
// 不拦的话，compose 在启动第二个副本时才报端口被占用，那时第一个已经起来了；
// 而"只给第一个副本映射"会让经宿主机端口来的流量悄悄只到一个副本——两样都不该发生。
// 宿主机端口有两个来源，出路不同，提示按来源给：
//
//	expose            开放给外面（外壳替成员开放的也算）：改成 replicas: 1，或者不开放
//	本机上的调用方     mode: local / debug 的进程经宿主机端口连它：这次读的部署文件里改成 replicas: 1
//
// 外壳成员条目上的 replicas 不在这里：成员没有自己的容器，它跟着外壳的副本数。
func (p *Plan) checkReplicaHostPorts() error {
	for _, c := range p.components {
		replicas := c.Entry.ReplicaCount()
		ports := p.HostPortsOf(c)
		if replicas <= 1 || len(ports) == 0 {
			continue
		}
		exposed := 0
		if _, ok := p.exposedPort[c.Service]; ok {
			exposed++
		}
		for _, s := range p.exposedMembers() {
			if manifest.ServiceName(s.Shell.ID, s.Shell.Version) == c.Service {
				exposed++
			}
		}
		var hints []string
		if exposed > 0 {
			hints = append(hints, i18n.T(msgid.ComposeHintReplicasDropExpose))
		}
		if len(ports) > exposed {
			hints = append(hints, i18n.T(msgid.ComposeHintReplicasForHostCallers))
		}
		return clierr.New(clierr.CodePortConflict, i18n.T(msgid.ComposeReplicasWithHostPorts, refText(c.Ref), replicas)).
			WithDetail(i18n.T(msgid.ComposeLabelHostPorts), strings.Join(ports, ", ")).
			WithHint(hints...)
	}
	return nil
}

// exposeHostPort 返回宿主机端口：exposePort 优先，否则用组件主端口。
func exposeHostPort(c Workload) int {
	if c.Entry.ExposePort > 0 {
		return c.Entry.ExposePort
	}
	return c.Manifest.Deployment.Port
}

// WaitKind 是一个容器启动之前要等的东西到了什么状态。
type WaitKind int

const (
	// WaitCompleted：一次性的迁移成功结束。
	WaitCompleted WaitKind = iota
	// WaitHealthy：依赖健康（它有健康检查）。
	WaitHealthy
	// WaitStarted：依赖启动了（它没有健康检查，等 healthy 会永远等下去）。
	WaitStarted
)

// Wait 是一条启动等待：Service 是要等的那个容器（迁移或组件）的服务名。
type Wait struct {
	Service string
	Kind    WaitKind
}

// Waits 是 c 的容器启动之前要等的东西，按服务名排序。
//
// 两类：自己的迁移必须**成功结束**、强依赖组件必须**健康**。
// 弱依赖不在里面——它可能根本不启动，等它会把整个项目卡死。
//
// **基础资源不出现在这里**：平台不部署它们，生成物里
// 根本没有对应的容器，等它只会让引擎直接报错。
// 资源没起来时组件自己会连不上——这正是 `up` 每次都把"要先跑起来什么"
// 列出来的理由（那是一句**声明**，不是替使用者去探测一遍）。
func (p *Plan) Waits(c Workload) []Wait {
	waits := map[string]WaitKind{}

	if c.Manifest.Migration != nil {
		// 等迁移成功结束，而不是等它"起来了"
		waits[MigrationService(c.Service)] = WaitCompleted
	}
	// 外壳：它承载的成员的迁移也必须先成功结束——成员的代码在外壳进程里加载
	for _, m := range p.memberMigrations {
		if shellRef, ok := p.shellOf(m.Ref); ok && shellRef == c.Ref {
			waits[MigrationService(m.Service)] = WaitCompleted
		}
	}

	// 外壳要等的还包括它承载的成员的强依赖：成员的代码就在外壳进程里（shell.WaitFor）
	// 只等 WaitFor：skipWaitFor 写掉的强依赖不进来，但照样连得到
	for _, dep := range shell.WaitFor(p.proj, p.graph, p.states, c.Ref) {
		service := manifest.ServiceName(dep.ID, dep.Version)
		if p.rendered[service] {
			waits[service] = p.readyKind(dep)
			continue
		}
		// 依赖的这个组件被另一个外壳承载：它没有自己的容器，真正要等的是那个外壳
		if shellRef, ok := p.shellOf(dep); ok {
			shellService := manifest.ServiceName(shellRef.ID, shellRef.Version)
			if p.rendered[shellService] {
				// 多个成员可能指向同一个外壳，只写一次
				if _, exists := waits[shellService]; !exists {
					waits[shellService] = p.readyKind(shellRef)
				}
			}
		}
	}

	out := make([]Wait, 0, len(waits))
	for service, kind := range waits {
		out = append(out, Wait{Service: service, Kind: kind})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Service < out[j].Service })
	return out
}

// readyKind 决定等依赖到什么状态。
//
// 没有健康检查的组件只能等它"启动了"——等 healthy 会永远等下去。
func (p *Plan) readyKind(ref resolver.Ref) WaitKind {
	node := p.graph.Node(ref)
	if node == nil || node.Manifest == nil || HealthCheckOf(node.Manifest) == nil {
		return WaitStarted
	}
	return WaitHealthy
}

// HealthCheck 是一个容器的健康检查：在容器里跑的命令，加上平台固定的节奏。
type HealthCheck struct {
	// Command 是交给 shell 执行的一条命令（CMD-SHELL）。
	Command string
	// StartPeriodSeconds 是启动宽限期。
	StartPeriodSeconds int
}

// 平台固定的健康检查节奏（compose 与 Quadlet 相同）。
const (
	HealthInterval = "10s"
	HealthTimeout  = "3s"
	HealthRetries  = 3
)

// healthcheckHost 是容器里探自己时用的地址。写 127.0.0.1 而不是 localhost：容器的 /etc/hosts 里 localhost
// 同时指向 127.0.0.1 与 ::1，busybox（Alpine）的 wget / nc 取到的是 ::1，而只监听 0.0.0.0 的服务
// （Python、Node 的常见写法）在 IPv6 上没人接——组件好好的，却被判成不健康。监听 :: 的服务（Go 的缺省）
// 两个地址都接，所以 127.0.0.1 对两种都成立。这是真跑起来撞到的。
const healthcheckHost = "127.0.0.1"

// HealthCheckOf 把 Manifest 的健康检查转成容器的健康检查；none 时返回 nil。
//
// 探的是**主端口**：额外端口不参与健康检查。
func HealthCheckOf(m *manifest.Manifest) *HealthCheck {
	if m == nil {
		return nil
	}
	// start_period 内探测失败**不计入 retries**，而且一旦成功立刻转 healthy
	// ——所以它只推迟"判死刑"，不推迟"判活"。少了它，平台给组件的启动预算
	// 就是写死的 interval × retries = 30 秒，任何冷启动超过半分钟的组件
	// （Spring Boot / Django / .NET）都会被判 unhealthy，
	// 而 `up -d --wait` 见到 unhealthy 直接失败。
	startPeriod := m.HealthCheck.StartPeriod()

	// 这个检查决定依赖方什么时候启动、up 什么时候返回——问的正是"能接流量了吗"，
	// 所以声明了 readinessCheck 就探它（manifest.ReadyCheck）
	checkType, path := m.ReadyCheck()
	switch checkType {
	case manifest.HealthCheckHTTP:
		url := fmt.Sprintf("http://%s:%d%s", healthcheckHost, m.Deployment.Port, path)
		// wget 与 curl 都试一遍。
		//
		// 健康检查跑在**容器内部**，用的必须是镜像里真有的命令。
		// 只写 wget 的话，只装了 curl 的镜像（不少 Debian 系的）上必然失败——
		// 组件明明跑得好好的，平台却判它 unhealthy，依赖方永远等不到它，
		// 而容器日志里写着"组件已就绪"。这是真跑起来撞到的。
		//
		// 两条都发 GET，与 K8s 的 httpGet 一致，组件只要答 GET。不用 `wget --spider`：
		// BusyBox 的 --spider 发 GET，GNU wget（Debian / Ubuntu 镜像装的那个）发 HEAD，
		// 只为 GET 注册了路由的组件在 Alpine 上健康、换个基础镜像就永远 unhealthy。
		// 响应体丢进 /dev/null；非 2xx/3xx 时两者都返回非零。
		return &HealthCheck{
			Command:            "wget -q -O /dev/null " + url + " || curl -fsS -o /dev/null " + url + " || exit 1",
			StartPeriodSeconds: startPeriod,
		}
	case manifest.HealthCheckTCP:
		return &HealthCheck{
			Command:            fmt.Sprintf("nc -z %s %d", healthcheckHost, m.Deployment.Port),
			StartPeriodSeconds: startPeriod,
		}
	default:
		// none：不生成健康检查。生成一个探不通的检查会让依赖方永远等不到 healthy
		return nil
	}
}

// Components 是这次要生成容器的组件（不含本机进程与被承载的成员），按服务名排序。
func (p *Plan) Components() []Workload { return p.components }

// MemberMigrations 是被外壳承载、又声明了 migration 的成员：它们没有主容器，迁移却仍然单独跑，
// 用成员自己的镜像与配置。按服务名排序。
func (p *Plan) MemberMigrations() []Workload { return p.memberMigrations }

// ShellAliases 是外壳的容器要额外挂的网络别名：它承载的成员的版本化服务名，排好序。
func (p *Plan) ShellAliases(service string) []string { return p.shellAliases[service] }

// MigrationAfter 是这个组件的迁移要等哪个迁移先成功结束（同一组件的上一个版本），没有时为空。
func (p *Plan) MigrationAfter(service string) string { return p.migrationAfter[service] }

// HasSecretFiles 报告 owner（一个版本化服务名）有没有以文件交付的配置项。
func (p *Plan) HasSecretFiles(owner string) bool { return len(p.secretFiles[owner]) > 0 }

// HostAlias 说本机进程拿到的 host.docker.internal 是怎么处理的（LocalEnvFiles 之后才定）。
func (p *Plan) HostAlias() HostAliasUse { return p.hostAlias }

// Warnings 是不阻断的问题。
func (p *Plan) Warnings() []*clierr.Error { return p.warnings }

// Project 是计划所属的项目。
func (p *Plan) Project() *project.Project { return p.proj }
