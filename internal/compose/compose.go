// Package compose 把解析、级联、注入的结果渲染成 compose.yaml
// 。
//
// 它是纯函数：进去的是配置与三份计算结果，出来的是文件内容与一份
// "使用者还需要做什么"的清单。不碰磁盘、不调 docker——那是 up 的事。
package compose

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deploy"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
)

// 生成文件里的固定值。
const (
	// networkAlias 是 compose 文件内部引用网络的别名；真实网络名见 networkName。
	networkAlias = "brickkit-net"
	// healthcheckInterval 等参数是平台固定的健康检查节奏。
	healthcheckInterval = "10s"
	healthcheckTimeout  = "3s"
	healthcheckRetries  = 3
)

// Options 是生成选项。
type Options struct {
	// Now 用于文件头的生成时间，测试可注入。
	Now func() time.Time
	// Engine 是容器引擎（目前只有 EngineDocker）。
	// 只影响 mode: debug 时 extra_hosts 的宿主机别名；空值按 Docker 处理。
	Engine string
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
}

// EnvFile 是一个服务的 0600 环境变量文件：密钥与 file:// 内容不进 compose.yaml（附录 A7）。
type EnvFile struct {
	Service string
	// Path 相对项目根（compose 的 --project-directory）：.brickkit/generated/env/<service>.env
	Path    string
	Content []byte
}

// EnvFileDir 是 env 文件所在目录，相对项目根。
const EnvFileDir = ".brickkit/generated/env"

// Result 是一次生成的产物。
type Result struct {
	// YAML 是 compose.yaml 的内容。
	YAML []byte
	// EnvFiles 是要以 0600 写盘的 env 文件，按服务名排序。
	EnvFiles []EnvFile
	// LocalEnvFiles 是 mode: debug 组件的调试环境变量文件。
	LocalEnvFiles []LocalEnvFile
	// RunAfter 是 up 之后要单独跑完的一次性 service：裸进程外壳承载的成员的迁移——
	// 外壳不在 compose 文件里，没有 service 通过 depends_on 等着它们（engine.UpRequest.RunAfter）。
	RunAfter []string
	// Warnings 是不阻断的问题。
	Warnings []*clierr.Error
}

// Generate 渲染 compose.yaml。
//
// 只渲染**本次实际启动**的组件（级联结果），以及它们用到的、由 CLI 托管的基础资源。
func Generate(
	proj *project.Project, graph *resolver.Graph, states *cascade.Result,
	env *inject.Result, opts Options,
) (*Result, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}

	plan, err := newPlan(proj, graph, states, env, opts)
	if err != nil {
		return nil, err
	}

	networks := map[string]any{
		networkAlias: map[string]any{
			"name":   networkName(proj.Decl.Project),
			"driver": "bridge",
		},
	}

	doc := map[string]any{
		"services": plan.services(),
		"networks": networks,
	}
	body, err := marshal(doc)
	if err != nil {
		return nil, err
	}

	now := opts.Now()
	locals, err := plan.localEnvFiles(now)
	if err != nil {
		return nil, err
	}
	return &Result{
		YAML:          append(header(proj, plan, now), body...),
		EnvFiles:      plan.envFileList(),
		LocalEnvFiles: locals,
		RunAfter:      plan.runAfter(),
		Warnings:      plan.warnings,
	}, nil
}

// networkName 是项目专属网络名：brickkit-<项目名>-net。
func networkName(project string) string { return deploy.NetworkName(project) }

// header 是生成文件的头注释。
//
// 生成的文件会被人打开看、被 git 记录，所以要写清楚"这是谁生成的、别手改"。
func header(proj *project.Project, plan *plan, now time.Time) []byte {
	return deploy.FileHeader(proj.Decl.Project, now,
		filepath.Base(proj.DeployPath)+" → target: "+proj.Deploy.Target,
		i18n.T(msgid.ComposeHeaderComponents, len(plan.components)))
}

// marshal 渲染 YAML。缩进 2 空格，与设计书样例一致。
func marshal(doc map[string]any) ([]byte, error) {
	var b bytes.Buffer
	encoder := yaml.NewEncoder(&b)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, fmt.Errorf("%s%w", i18n.T(msgid.ComposeRenderFailed), err)
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// ============================================================
// 生成计划
// ============================================================

// componentPlan 是一个要渲染成 service 的组件。
type componentPlan struct {
	Ref      resolver.Ref
	Service  string
	Manifest *manifest.Manifest
	Entry    deployfile.Entry
	Env      inject.Component
}

// plan 是整份文件的生成计划。
type plan struct {
	proj   *project.Project
	graph  *resolver.Graph
	states *cascade.Result
	engine string
	root   string
	lookup func(string) (string, bool)

	// inline / envFile 是每个服务算好的环境变量去处（envplace.go）：
	// inline 进 compose.yaml 的 environment，envFile 进 0600 的 env 文件。
	inline  map[string][]string
	envFile map[string][]byte

	// components 是本次要渲染的组件（已排除 mode: debug），按服务名排序。
	components []componentPlan
	// locals 是 mode: debug 的组件：不生成容器，但要参与端口分配与 env 文件生成。
	locals []localComponent
	// served 是 外壳成员：不生成自己的容器/迁移，但要走它专属的
	// 几条提醒（见 servedby.go）。
	served []servedComponent
	// memberMigrations 是被外壳承载、又声明了 migration 的成员：它们没有主容器，
	// 迁移却仍然单独跑，用成员自己的镜像与配置（提案 §8.1 规则 2、§8.9.4）。
	memberMigrations []componentPlan
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
	//	exposedPort    服务名 → expose 映射到宿主机的端口
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

func newPlan(
	proj *project.Project, graph *resolver.Graph, states *cascade.Result,
	env *inject.Result, opts Options,
) (*plan, error) {
	p := &plan{
		proj: proj, graph: graph, states: states, engine: opts.Engine,
		root: opts.Root, lookup: opts.Lookup,
		inline: map[string][]string{}, envFile: map[string][]byte{},
		rendered:       map[string]bool{},
		migrationAfter: map[string]string{},
		localPort:      map[string]int{},
		exposedPort:    map[string]int{},
		debugPort:      map[string]int{},
		debugExtraPort: map[string]map[int]int{},
		shellAliases:   map[string][]string{},

		shellMemberHostPorts: map[string][]hostPortMapping{},
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
			p.served = append(p.served, servedComponent{
				Ref: ref, Service: service, Manifest: node.Manifest,
				Entry: entry, Shell: shellRef, Env: copyComponent(envByRef[ref]),
			})
			if node.Manifest != nil && node.Manifest.Migration != nil {
				p.memberMigrations = append(p.memberMigrations, componentPlan{
					Ref: ref, Service: service, Manifest: node.Manifest, Entry: entry, Env: envByRef[ref],
				})
			}
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
	p.rewriteEndpointsForLocalDependencies()

	// 成员交给外壳的环境用计划里改写过的那一份（本地调试地址、裸进程外壳的 localhost 地址）
	groups, err := shell.Resolve(proj, graph, states, p.envForShells(env), opts.Lookup)
	if err != nil {
		return nil, err
	}
	p.applyShellGroups(groups)
	if err := p.placeEnvironment(); err != nil {
		return nil, err
	}

	p.warnings = append(p.warnings, p.localMigrationWarnings()...)
	p.warnings = append(p.warnings, p.localExposeWarnings()...)
	p.warnings = append(p.warnings, p.localLabelWarnings()...)
	p.warnings = append(p.warnings, p.fallbackStandaloneWarnings()...)
	p.warnings = append(p.warnings, p.bareSkipWaitForWarnings()...)
	return p, nil
}

// chainMigrations 把**同一个组件 ID** 的多个版本的迁移按版本号串成一条链。
//
// # 为什么必须串
//
// 资源绑定按组件 ID 记（不带版本），所以同一组件的多个版本拿到的
// `DATABASE_NAME` 必然是同一个；迁移状态表的主键是 (component_id, version)
// ，两个版本的 component_id 也是同一个。于是"两个迁移容器同时对
// 同一个库、用同一个身份跑迁移"完全是**平台自己生成出来的**——使用者在
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
func (p *plan) chainMigrations() {
	// 按组件 ID 归集有迁移的版本
	byID := map[string][]componentPlan{}
	for _, c := range append(append([]componentPlan{}, p.components...), p.memberMigrations...) {
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
			p.migrationAfter[versions[i].Service] = migrationService(versions[i-1].Service)
		}
	}
}

// migrationService 是某个组件的迁移 service 名。
func migrationService(service string) string { return service + "-migration" }

// checkExposePorts 检查宿主机端口冲突。
//
// 两个组件抢同一个宿主机端口时，docker 会在启动到第二个容器时才失败，
// 那时第一个已经起来了——不如在生成阶段就说清楚。
func (p *plan) checkExposePorts() error {
	// 只记"是谁占了"：报错要点名两个组件。占的是不是使用者显式写的
	// exposePort 无所谓——两种情况的出路是同一条（给其中一个设 exposePort）
	claimed := map[int]string{}

	for _, c := range p.components {
		if !c.Entry.Expose {
			continue
		}
		hostPort := exposeHostPort(c)
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

// exposeHostPort 返回宿主机端口：exposePort 优先，否则用组件主端口。
func exposeHostPort(c componentPlan) int {
	if c.Entry.ExposePort > 0 {
		return c.Entry.ExposePort
	}
	return c.Manifest.Deployment.Port
}

// services 渲染 services 段。
func (p *plan) services() map[string]any {
	services := map[string]any{}

	for _, c := range p.components {
		if c.Manifest.Migration != nil {
			services[migrationService(c.Service)] = p.migrationDoc(c)
		}
		services[c.Service] = p.componentService(c)
	}
	for _, m := range p.memberMigrations {
		services[migrationService(m.Service)] = p.migrationDoc(m)
	}
	return services
}

// ============================================================
// 组件 service
// ============================================================

func (p *plan) componentService(c componentPlan) map[string]any {
	svc := map[string]any{
		"image":   manifest.ImageRef(c.Manifest),
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

	p.applyEnvironment(svc, c.Service)
	if ports := p.hostPortsOf(c); len(ports) > 0 {
		svc["ports"] = ports
	}
	// 13.2：把 local 组件的服务名解析到宿主机，容器里的代码一行不用改
	if hosts := p.extraHostsOf(c); len(hosts) > 0 {
		svc["extra_hosts"] = hosts
	}
	if health := healthcheckOf(c.Manifest); health != nil {
		svc["healthcheck"] = health
	}
	if deploy := deployOf(c.Env); deploy != nil {
		svc["deploy"] = deploy
	}
	// 平台不解释键值，只透传。
	//
	// **只写主容器，不写迁移容器。** 迁移不是服务：Traefik / Prometheus 一类
	// 工具读到同一份 labels，会把那个跑完就退出的一次性容器当成路由目标或抓取
	// 目标。这与"环境变量必须与主容器完全一致"不冲突——那是因为
	// 迁移要连同一个库，而 labels 说的是"外面怎么找到这个服务"。
	if len(c.Env.Labels) > 0 {
		svc["labels"] = c.Env.Labels
	}
	if dependsOn := p.componentDependsOn(c); len(dependsOn) > 0 {
		svc["depends_on"] = dependsOn
	}
	return svc
}

// componentDependsOn 生成 depends_on。
//
// 两类依赖：自己的迁移必须**成功结束**、强依赖组件必须**健康**。
// 弱依赖不写——它可能根本不启动，写进去会把整个项目卡死。
//
// **基础资源不出现在这里**：平台不部署它们，compose 文件里
// 根本没有对应的 service，写进 depends_on 只会让 compose 直接报错。
// 资源没起来时组件自己会连不上——这正是 `up` 每次都把"要先跑起来什么"
// 列出来的理由（那是一句**声明**，不是替使用者去探测一遍）。
func (p *plan) componentDependsOn(c componentPlan) map[string]any {
	dependsOn := map[string]any{}

	if c.Manifest.Migration != nil {
		// 12.12：等迁移成功结束，而不是等它"起来了"
		dependsOn[migrationService(c.Service)] = condition("service_completed_successfully")
	}
	// 外壳：它承载的成员的迁移也必须先成功结束——成员的代码在外壳进程里加载
	for _, m := range p.memberMigrations {
		if shellRef, ok := p.shellOf(m.Ref); ok && shellRef == c.Ref {
			dependsOn[migrationService(m.Service)] = condition("service_completed_successfully")
		}
	}

	// 外壳要等的还包括它承载的成员的强依赖：成员的代码就在外壳进程里（shell.WaitFor）
	// 只等 WaitFor：skipWaitFor 写掉的强依赖不进 depends_on（附录 A23），但照样连得到
	for _, dep := range shell.WaitFor(p.proj, p.graph, p.states, c.Ref) {
		service := manifest.ServiceName(dep.ID, dep.Version)
		if p.rendered[service] {
			dependsOn[service] = condition(p.readyCondition(dep))
			continue
		}
		// 依赖的这个组件被另一个外壳承载：它没有自己的 service，真正要等的是那个外壳
		if shellRef, ok := p.shellOf(dep); ok {
			shellService := manifest.ServiceName(shellRef.ID, shellRef.Version)
			if p.rendered[shellService] {
				// 多个成员可能指向同一个外壳，只写一次
				if _, exists := dependsOn[shellService]; !exists {
					dependsOn[shellService] = condition(p.readyCondition(shellRef))
				}
			}
		}
	}
	return dependsOn
}

// readyCondition 决定等待条件。
//
// 没有健康检查的组件只能等它"启动了"——等 service_healthy 会永远等下去。
func (p *plan) readyCondition(ref resolver.Ref) string {
	node := p.graph.Node(ref)
	if node == nil || node.Manifest == nil || healthcheckOf(node.Manifest) == nil {
		return "service_started"
	}
	return "service_healthy"
}

func condition(value string) map[string]any { return map[string]any{"condition": value} }

// migrationDoc 生成迁移用的一次性 service。
func (p *plan) migrationDoc(c componentPlan) map[string]any {
	svc := map[string]any{
		// 用组件自己的镜像，迁移脚本与业务代码同版本
		"image":    manifest.ImageRef(c.Manifest),
		"networks": []string{networkAlias},
		// 12.11：一次性任务，失败了要让人看见，不能自动重启
		"restart": "no",
	}

	// 必须同时覆盖 entrypoint 与 command。
	//
	// 组件镜像普遍带 ENTRYPOINT（推荐写法），而 compose 的 command 只覆盖 CMD：
	// 只写 command 会拼成 `<entrypoint> <migration.command...>`，参数错位，
	// 于是"迁移容器"实际上把**服务**起了起来，主服务永远等不到"迁移完成"，
	// 整个项目卡死。这是真跑起来才发现的。
	command := c.Manifest.Migration.Command
	svc["entrypoint"] = []string{command[0]}
	if len(command) > 1 {
		svc["command"] = command[1:]
	}
	// 环境变量与主容器完全一致（同一份 inline 与同一个 env 文件）
	p.applyEnvironment(svc, c.Service)
	// 环境变量一致，寻址方式也得一致：拿到一个指向宿主机的地址
	// 却没有 extra_hosts，这个主机名在迁移容器里根本解析不了
	if hosts := p.extraHostsOf(c); len(hosts) > 0 {
		svc["extra_hosts"] = hosts
	}

	// 同一组件的上一个版本的迁移必须先成功结束（见 chainMigrations）。
	//
	// 资源不在这里：资源不由平台部署，compose 文件里根本没有对应的 service，
	// 写进 depends_on 只会让 compose 直接报错。迁移是第一个连库的东西，
	// 库没起来它就是第一个失败的——那条 connection refused 比任何
	// 平台自己编的说法都准确
	if previous := p.migrationAfter[c.Service]; previous != "" {
		svc["depends_on"] = map[string]any{
			previous: condition("service_completed_successfully"),
		}
	}
	return svc
}

// healthcheckOf 把 Manifest 的健康检查转成 compose 的 healthcheck。
//
// 探的是**主端口**：额外端口不参与健康检查。
func healthcheckOf(m *manifest.Manifest) map[string]any {
	if m == nil {
		return nil
	}
	// start_period 内探测失败**不计入 retries**，而且一旦成功立刻转 healthy
	// ——所以它只推迟"判死刑"，不推迟"判活"。少了它，平台给组件的启动预算
	// 就是写死的 interval × retries = 30 秒，任何冷启动超过半分钟的组件
	// （Spring Boot / Django / .NET）都会被判 unhealthy，
	// 而 `up -d --wait` 见到 unhealthy 直接失败。
	startPeriod := fmt.Sprintf("%ds", m.HealthCheck.StartPeriod())

	switch m.HealthCheck.Type {
	case manifest.HealthCheckHTTP:
		url := fmt.Sprintf("http://localhost:%d%s", m.Deployment.Port, m.HealthCheck.Path)
		// wget 与 curl 都试一遍。
		//
		// compose 的健康检查跑在**容器内部**，用的必须是镜像里真有的命令。
		// 只写 wget 的话，python:slim / 各种 distroless 基础镜像上必然失败——
		// 组件明明跑得好好的，平台却判它 unhealthy，依赖方永远等不到它，
		// 而容器日志里写着"组件已就绪"。这是真跑起来撞到的。
		return map[string]any{
			"test": []string{"CMD-SHELL", fmt.Sprintf(
				"wget -q --spider %s || curl -fsS %s || exit 1", url, url)},
			"interval":     healthcheckInterval,
			"timeout":      healthcheckTimeout,
			"retries":      healthcheckRetries,
			"start_period": startPeriod,
		}
	case manifest.HealthCheckTCP:
		return map[string]any{
			"test": []string{"CMD-SHELL",
				fmt.Sprintf("nc -z localhost %d", m.Deployment.Port)},
			"interval":     healthcheckInterval,
			"timeout":      healthcheckTimeout,
			"retries":      healthcheckRetries,
			"start_period": startPeriod,
		}
	default:
		// none：不生成健康检查。生成一个探不通的检查会让依赖方永远等不到 healthy
		return nil
	}
}

// deployOf 渲染资源配额。
//
// compose 用 limits / reservations 表达 K8s 的 limits / requests。
func deployOf(c inject.Component) map[string]any {
	resources := map[string]any{}

	if spec := quotaOf(c.Resources.Limits); len(spec) > 0 {
		resources["limits"] = spec
	}
	if spec := quotaOf(c.Resources.Requests); len(spec) > 0 {
		resources["reservations"] = spec
	}
	if len(resources) == 0 {
		return nil
	}
	return map[string]any{"resources": resources}
}

func quotaOf(spec *manifest.ResourceSpec) map[string]any {
	if spec == nil {
		return nil
	}
	out := map[string]any{}
	if cpus, err := cpuToCompose(spec.CPU); err == nil && cpus != "" {
		out["cpus"] = cpus
	}
	if memory, err := memoryToCompose(spec.Memory); err == nil && memory != "" {
		out["memory"] = memory
	}
	return out
}
