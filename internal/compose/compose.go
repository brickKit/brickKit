// Package compose 把解析、级联、注入的结果渲染成 docker-compose.yaml
// （004 §5.3、005 §5）。
//
// 它是纯函数：进去的是配置与三份计算结果，出来的是文件内容与一份
// "使用者还需要做什么"的清单。不碰磁盘、不调 docker——那是 up 的事（Step 15）。
package compose

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/deploy"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
)

// 生成文件里的固定值。
const (
	// networkAlias 是 compose 文件内部引用网络的别名；真实网络名见 networkName。
	networkAlias = "brickkit-net"
	// healthcheckInterval 等参数对齐 005 §5 的样例。
	healthcheckInterval = "10s"
	healthcheckTimeout  = "3s"
	healthcheckRetries  = 3
)

// Options 是生成选项。
type Options struct {
	// Now 用于文件头的生成时间，测试可注入。
	Now func() time.Time
	// Engine 是容器引擎（目前只有 EngineDocker）。
	// 只影响 local: true 时 extra_hosts 的宿主机别名（005 §7.5）；空值按 Docker 处理。
	Engine string
	// Lookup 解析 ${VAR}，**只用于 local-debug 环境变量文件**。
	//
	// compose 文件本身刻意保留占位符：那份文件会被人打开看、进 git diff，
	// 明文密码进去就等于泄露，而 docker compose 会自己从 .env 展开。
	// local-debug 文件不一样——它是给 IDE 读的（VS Code 的 envFile、
	// IntelliJ 的 EnvFile 插件），**它们都不做变量替换**，留着占位符，
	// IDE 里的进程就会拿着字面量 "${PG_PASSWORD}" 去连库。
	Lookup func(name string) (string, bool)
}

// ResourceRequirement 是一个**必须先跑起来**的基础资源。
//
// 定义在 internal/deploy：K8s 目标要给出同样的清单，两处各算一遍迟早会分叉。
type ResourceRequirement = deploy.ResourceRequirement

// Result 是一次生成的产物。
type Result struct {
	// YAML 是 docker-compose.yaml 的内容。
	YAML []byte
	// Resources 是必须先跑起来的基础资源（平台不部署它们，006 §9.1）。
	Resources []ResourceRequirement
	// LocalEnvFiles 是 local: true 组件的调试环境变量文件（005 §4.9）。
	LocalEnvFiles []LocalEnvFile
	// HostPorts 是本次会占用的宿主机端口（expose / 本地调试映射 / 资源映射 /
	// local 组件自己监听的端口），供 up 在启动前体检（P22）。
	HostPorts []HostPort
	// Warnings 是不阻断的问题。
	Warnings []*clierr.Error
}

// Generate 渲染 docker-compose.yaml。
//
// 只渲染**本次实际启动**的组件（级联结果），以及它们用到的、由 CLI 托管的基础资源。
func Generate(
	cfg *config.Config, graph *resolver.Graph, states *cascade.Result,
	env *inject.Result, opts Options,
) (*Result, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}

	plan, err := newPlan(cfg, graph, states, env, opts.Engine)
	if err != nil {
		return nil, err
	}

	networks := map[string]any{
		networkAlias: map[string]any{
			"name":   networkName(cfg.Project),
			"driver": "bridge",
		},
	}
	// P39：引用（而不是创建）外部项目的网络，依赖方才解析得出对方的服务名
	for name, spec := range plan.externalNetworks() {
		networks[name] = spec
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
	return &Result{
		YAML:          append(header(cfg, plan, now), body...),
		Resources:     plan.requirements(),
		LocalEnvFiles: plan.localEnvFiles(now, opts.Lookup),
		HostPorts:     plan.hostPorts(),
		Warnings:      plan.warnings,
	}, nil
}

// networkName 是项目专属网络名（005 §5）：brickkit-<项目名>-net。
func networkName(project string) string { return deploy.NetworkName(project) }

// header 是生成文件的头注释（12.16）。
//
// 生成的文件会被人打开看、被 git 记录，所以要写清楚"这是谁生成的、别手改"。
func header(cfg *config.Config, plan *plan, now time.Time) []byte {
	var b bytes.Buffer
	b.WriteString("# ============================================================\n")
	b.WriteString("# 由 BrickKit CLI 自动生成，请勿手动编辑\n")
	b.WriteString("# 手工改动会在下次 brickkit up 时被覆盖；请改 brickkit.yaml\n")
	fmt.Fprintf(&b, "# 生成时间：%s\n", now.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "# 项目：%s\n", cfg.Project)
	fmt.Fprintf(&b, "# deploy.target: %s\n", cfg.Deploy.Target)
	fmt.Fprintf(&b, "# 组件数：%d\n", len(plan.components))
	b.WriteString("# ============================================================\n\n")
	return b.Bytes()
}

// marshal 渲染 YAML。缩进 2 空格，与设计书样例一致。
func marshal(doc map[string]any) ([]byte, error) {
	var b bytes.Buffer
	encoder := yaml.NewEncoder(&b)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, fmt.Errorf("渲染 docker-compose.yaml 失败：%w", err)
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
	Entry    config.Component
	Env      inject.Component
}

// plan 是整份文件的生成计划。
type plan struct {
	cfg    *config.Config
	graph  *resolver.Graph
	states *cascade.Result
	engine string

	// components 是本次要渲染的组件（已排除 local: true），按服务名排序。
	components []componentPlan
	// locals 是 local: true 的组件：不生成容器，但要参与端口分配与 env 文件生成。
	locals []localComponent
	// externals 是 external 的组件（P39）：由别的项目部署，本项目什么都不生成，
	// 只据此把依赖方接进对方项目的网络。
	externals []externalComponent
	// rendered 是最终会出现在文件里的 service 名集合。
	rendered map[string]bool

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

	warnings []*clierr.Error
}

func newPlan(
	cfg *config.Config, graph *resolver.Graph, states *cascade.Result,
	env *inject.Result, engine string,
) (*plan, error) {
	p := &plan{
		cfg: cfg, graph: graph, states: states, engine: engine,
		rendered:       map[string]bool{},
		localPort:      map[string]int{},
		exposedPort:    map[string]int{},
		debugPort:      map[string]int{},
		debugExtraPort: map[string]map[int]int{},
	}

	entries := map[resolver.Ref]config.Component{}
	for _, c := range cfg.Components {
		entries[resolver.Ref{ID: c.ID, Version: c.Version}] = c
	}

	envByRef := map[resolver.Ref]inject.Component{}
	for _, c := range env.Components {
		envByRef[c.Ref] = c
	}

	for _, ref := range states.Running() {
		entry := entries[ref]
		node := graph.Node(ref)
		if node == nil {
			continue
		}
		service := manifest.ServiceName(ref.ID, ref.Version)

		if entry.IsExternal() {
			// P39：它由**别的项目**部署，本项目不生成它的 service、
			// 不生成它的迁移容器；但依赖方要连得上，所以记下它属于哪个项目，
			// 后面据此把依赖方接进那张网络
			p.externals = append(p.externals, externalComponent{
				Ref: ref, Service: service, Project: entry.External.Project,
			})
			continue
		}
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

	// 排序只为确定性：自动分配的端口依赖遍历顺序，
	// map 的随机顺序会让同一份配置每次生成出不同的端口号
	sort.Slice(p.components, func(i, j int) bool { return p.components[i].Service < p.components[j].Service })
	sort.Slice(p.locals, func(i, j int) bool { return p.locals[i].Service < p.locals[j].Service })

	if err := p.checkExposePorts(); err != nil {
		return nil, err
	}
	if err := p.assignHostPorts(); err != nil {
		return nil, err
	}
	p.rewriteEndpointsForLocalDependencies()
	p.warnings = append(p.warnings, p.localMigrationWarnings()...)
	p.warnings = append(p.warnings, p.serviceNameResourceWarnings()...)
	return p, nil
}

// checkExposePorts 检查宿主机端口冲突（延后项 P4、004 §10.3）。
//
// 两个组件抢同一个宿主机端口时，docker 会在启动到第二个容器时才失败，
// 那时第一个已经起来了——不如在生成阶段就说清楚。
func (p *plan) checkExposePorts() error {
	type claim struct {
		component string
		explicit  bool
	}
	claimed := map[int]claim{}

	for _, c := range p.components {
		if !c.Entry.Expose {
			continue
		}
		hostPort, explicit := exposeHostPort(c)
		if previous, taken := claimed[hostPort]; taken {
			return clierr.Newf(clierr.CodePortConflict,
				"错误：宿主机端口 %d 被多个组件占用", hostPort).
				WithDetail("组件", previous.component).
				WithDetail("组件", c.Ref.ID+"@"+c.Ref.Version).
				WithDetailf("宿主机端口", "%d", hostPort).
				WithHint(
					"在 brickkit.yaml 中给其中一个组件设置不同的 exposePort",
					"或去掉其中一个组件的 expose: true（组件之间在容器网络内互访不需要 expose）",
				)
		}
		claimed[hostPort] = claim{component: c.Ref.ID + "@" + c.Ref.Version, explicit: explicit}
	}
	return nil
}

// exposeHostPort 返回宿主机端口：exposePort 优先，否则用组件主端口。
func exposeHostPort(c componentPlan) (port int, explicit bool) {
	if c.Entry.ExposePort > 0 {
		return c.Entry.ExposePort, true
	}
	return c.Manifest.Deployment.Port, false
}

// services 渲染 services 段。
func (p *plan) services() map[string]any {
	services := map[string]any{}

	for _, c := range p.components {
		if c.Manifest.Migration != nil {
			services[c.Service+"-migration"] = p.migrationService(c)
		}
		services[c.Service] = p.componentService(c)
	}
	return services
}

// requirements 汇总本次必须先跑起来的基础资源（006 §9.1、§9.5）。
func (p *plan) requirements() []ResourceRequirement {
	return deploy.Requirements(p.cfg, p.componentIDs())
}

// componentIDs 是本次会跑起来的组件 ID。
//
// local 组件也算：它不生成容器，但它照样要连自己的库。
func (p *plan) componentIDs() []string {
	out := make([]string, 0, len(p.components)+len(p.locals))
	for _, c := range p.components {
		out = append(out, c.Ref.ID)
	}
	for _, l := range p.locals {
		out = append(out, l.Ref.ID)
	}
	return out
}

// ============================================================
// 组件 service
// ============================================================

func (p *plan) componentService(c componentPlan) map[string]any {
	svc := map[string]any{
		"image":    c.Manifest.Deployment.Image,
		"networks": p.networksFor(c.Ref),
		"restart":  "unless-stopped", // 12.10
	}

	if env := environmentOf(c.Env); len(env) > 0 {
		svc["environment"] = env
	}
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
// **基础资源不出现在这里**：平台不部署它们（006 §9.1），compose 文件里
// 根本没有对应的 service，写进 depends_on 只会让 compose 直接报错。
// 资源没起来时组件自己会连不上——这正是 `up` 每次都把"要先跑起来什么"
// 列出来、以及 `--check-resources` 存在的理由。
func (p *plan) componentDependsOn(c componentPlan) map[string]any {
	dependsOn := map[string]any{}

	if c.Manifest.Migration != nil {
		// 12.12：等迁移成功结束，而不是等它"起来了"
		dependsOn[c.Service+"-migration"] = condition("service_completed_successfully")
	}

	node := p.graph.Node(c.Ref)
	if node != nil {
		for _, dep := range node.Requires {
			service := manifest.ServiceName(dep.ID, dep.Version)
			// local: true 或被跳过的依赖不在文件里，写进去 compose 会直接报错
			if !p.rendered[service] {
				continue
			}
			dependsOn[service] = condition(p.readyCondition(dep))
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

// migrationService 生成迁移用的一次性 service（002 §8.3、12.6）。
func (p *plan) migrationService(c componentPlan) map[string]any {
	svc := map[string]any{
		// 002 §8.4：用组件自己的镜像，迁移脚本与业务代码同版本
		"image":    c.Manifest.Deployment.Image,
		"networks": []string{networkAlias},
		// 12.11：一次性任务，失败了要让人看见，不能自动重启
		"restart": "no",
	}

	// 必须同时覆盖 entrypoint 与 command。
	//
	// 组件镜像普遍带 ENTRYPOINT（推荐写法），而 compose 的 command 只覆盖 CMD：
	// 只写 command 会拼成 `<entrypoint> <migration.command...>`，参数错位，
	// 于是"迁移容器"实际上把**服务**起了起来，主服务永远等不到"迁移完成"，
	// 整个项目卡死。这是真跑起来才发现的（005 §5 的样例同样有此问题）。
	command := c.Manifest.Migration.Command
	svc["entrypoint"] = []string{command[0]}
	if len(command) > 1 {
		svc["command"] = command[1:]
	}
	// 002 §8.5：环境变量与主容器完全一致
	if env := environmentOf(c.Env); len(env) > 0 {
		svc["environment"] = env
	}
	// 环境变量一致，寻址方式也得一致：拿到一个指向宿主机的地址
	// 却没有 extra_hosts，这个主机名在迁移容器里根本解析不了
	if hosts := p.extraHostsOf(c); len(hosts) > 0 {
		svc["extra_hosts"] = hosts
	}

	// 资源不由平台部署，因此没有可等的 service：迁移是第一个连库的东西，
	// 库没起来它就是第一个失败的——那条错误（connection refused）比任何
	// 平台自己编的说法都准确
	return svc
}

// environmentOf 把注入结果渲染成 compose 的 environment 列表。
//
// 用 `KEY=value` 的列表而不是 map：列表保序，生成的文件可比对
// （map 在 YAML 里会被重排，每次 diff 都是噪音）。
func environmentOf(c inject.Component) []string {
	out := make([]string, 0, len(c.Env))
	for _, v := range c.Env {
		out = append(out, v.Name+"="+v.Value)
	}
	return out
}

// healthcheckOf 把 Manifest 的健康检查转成 compose 的 healthcheck（12.3）。
//
// 探的是**主端口**（002 §5.5）：额外端口不参与健康检查。
func healthcheckOf(m *manifest.Manifest) map[string]any {
	if m == nil {
		return nil
	}
	switch m.HealthCheck.Type {
	case manifest.HealthCheckHTTP:
		url := fmt.Sprintf("http://localhost:%d%s", m.Deployment.Port, m.HealthCheck.Path)
		// wget 与 curl 都试一遍。
		//
		// compose 的健康检查跑在**容器内部**，用的必须是镜像里真有的命令。
		// 只写 wget 的话，python:slim / 各种 distroless 基础镜像上必然失败——
		// 组件明明跑得好好的，平台却判它 unhealthy，依赖方永远等不到它，
		// 而容器日志里写着"组件已就绪"。这是真跑起来撞到的（002 §9.6）。
		return map[string]any{
			"test": []string{"CMD-SHELL", fmt.Sprintf(
				"wget -q --spider %s || curl -fsS %s || exit 1", url, url)},
			"interval": healthcheckInterval,
			"timeout":  healthcheckTimeout,
			"retries":  healthcheckRetries,
		}
	case manifest.HealthCheckTCP:
		return map[string]any{
			"test": []string{"CMD-SHELL",
				fmt.Sprintf("nc -z localhost %d", m.Deployment.Port)},
			"interval": healthcheckInterval,
			"timeout":  healthcheckTimeout,
			"retries":  healthcheckRetries,
		}
	default:
		// none：不生成健康检查。生成一个探不通的检查会让依赖方永远等不到 healthy
		return nil
	}
}

// deployOf 渲染资源配额（12.4）。
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

// ============================================================
// 基础资源
// ============================================================

// serviceNameResourceWarnings 提醒"host 写成了服务名，但没人会创建这个服务"。
//
// 平台曾经在 host 不含点时**自己生成一个 postgres / redis 容器**（旧的 006 §10.4）。
// 那条路已经取消：它只覆盖 6 种 kind 里的 2 种、在 K8s 目标下从来不存在、
// 而且托管出来的实例还没法跨项目共享（各个 compose 项目各起各的）。
//
// 取消之后，`host: pg` 这种写法就成了一个**看不出来的空指向**：
// 生成的 compose 完全正常，容器里却解析不了这个名字，表现是启动之后才出现的
// `no such host`。所以在生成阶段就说一句——这正是当初那条隐式判据
// （host 里有没有点）最该被换掉的地方。
func (p *plan) serviceNameResourceWarnings() []*clierr.Error {
	used := map[string]bool{}
	for _, id := range p.componentIDs() {
		used[id] = true
	}

	var out []*clierr.Error
	seen := map[string]bool{}
	for _, r := range p.cfg.Resources {
		if !looksLikeServiceName(r.Host) || seen[r.ID] {
			continue
		}
		bound := false
		for _, b := range r.Bindings {
			if used[b.ComponentID] {
				bound = true
				break
			}
		}
		if !bound {
			continue
		}
		seen[r.ID] = true

		out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
			"基础资源的 host 看起来是个服务名，容器里可能解析不了").
			WithDetail("资源", r.ID).
			WithDetail("host", r.Host).
			WithDetail("原因", "平台不部署基础资源（006 §9.1），compose 里不会有叫这个名字的 service").
			WithHint(
				"资源跑在本机时写 host: "+hostMachineAlias+"（平台会自动补 extra_hosts）",
				"资源跑在别处时写它的 IP 或域名",
				"确实已经手工把该容器接进了本项目网络的话，这条提醒可以忽略",
			))
	}
	return out
}

// looksLikeServiceName 判断 host 像不像一个容器网络内的服务名。
//
// 判据只用来**提醒**，不用来决定行为——行为上所有资源一视同仁，
// 都由使用者自己准备好。这正是它与旧判据的根本差别：
// 从前这个函数的返回值决定"平台要不要替你起一个数据库"。
func looksLikeServiceName(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" || host == "localhost" || strings.Contains(host, ".") {
		return false
	}
	// 纯数字不像服务名，多半是写错的地址
	return strings.IndexFunc(host, func(r rune) bool { return r < '0' || r > '9' }) >= 0
}
