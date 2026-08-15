package compose

// 本文件实现 local: true（005 §4）。
//
// 一个组件被标成 local 时，它从容器里搬到了宿主机的 IDE 里，于是**两个方向**
// 都断了，必须由 CLI 各补一条路：
//
//	容器 → 宿主机：依赖方容器用 extra_hosts 把它的服务名指到宿主机网关，
//	               端口换成 localPort。依赖方代码一行不改。
//	宿主机 → 容器：它要访问的依赖与基础资源还在容器网络里，宿主机进不去，
//	               CLI 把这些容器的端口映射到宿主机，并把地址写进
//	               local-debug.<版本化服务名>.env 供 IDE 加载。
//
// 两个方向共用一张宿主机端口台账：localPort、exposePort、自动映射的端口
// 抢的是同一台机器上的同一批端口号，分开算迟早撞车。

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
)

// 容器引擎（005 §7.3–7.5）。两者生成的 compose 文件唯一的差别就是宿主机别名。
const (
	EngineDocker = "docker"
	EnginePodman = "podman"
)

// 宿主机端口分配的基准（005 §4.6、§4.8）。
const (
	// localPortBase 是 local 组件自己监听端口的起点。
	localPortBase = 8081
	// hostPortBase 是把容器端口映射到宿主机时的起点。
	hostPortBase = 18080
	// hostPortOffset 让映射出来的端口号自带含义：5432 → 15432、8080 → 18080。
	hostPortOffset = 10000
	maxPort        = 65535
)

// LocalEnvFile 是一个 local 组件的调试环境变量文件（005 §4.9）。
type LocalEnvFile struct {
	Ref resolver.Ref
	// Name 是文件名：local-debug.<版本化服务名>.env。
	// 用版本化服务名，同一组件的两个版本同时调试时才不会互相覆盖。
	Name string
	// Port 是该进程应当在宿主机上监听的端口。
	Port    int
	Content []byte
}

// hostGateway 返回 extra_hosts 里指向宿主机的魔法值（005 §7.5）。
//
// Docker 与 Podman **用同一个值**：`host-gateway`。
//
// 设计书原来写的是"Podman 用 host.containers.internal 替代"，那是把两件事
// 搞混了：`host.containers.internal` 是 Podman 自动注入到容器 /etc/hosts 的
// 一个**主机名**，不是 `--add-host` 能接受的**值**——真 Podman 上会直接报
// `invalid IP address in add-host`，容器根本创建不出来，local: true 整条路是断的。
// 而 `host-gateway` Podman 同样支持（实测解析到 169.254.1.2，
// 正是 host.containers.internal 的那个地址）。
//
// 参数保留是因为调用方本来就知道引擎是谁；将来若真出现第三种引擎需要别的值，
// 改这一个函数即可。
func hostGateway(string) string { return "host-gateway" }

// localComponent 是一个不生成容器、跑在宿主机 IDE 里的组件。
type localComponent struct {
	Ref      resolver.Ref
	Service  string
	Manifest *manifest.Manifest
	Entry    config.Component
	Env      inject.Component
	// Port 是它在宿主机上监听的端口（localPort 或自动分配）。
	Port int
}

// ============================================================
// 宿主机端口台账
// ============================================================

// portTable 记录宿主机端口的占用情况。
type portTable struct {
	owner map[int]string
}

func newPortTable() *portTable { return &portTable{owner: map[int]string{}} }

// claim 占用一个**明确指定**的端口，已被占用时报错（13.12、P4）。
//
// 指定的端口不能挪，撞了只能让使用者自己决定改谁——CLI 替他挑一个
// 会让 IDE 里配好的调试端口莫名其妙地失效。
func (t *portTable) claim(port int, owner string) error {
	if previous, taken := t.owner[port]; taken && previous != owner {
		return clierr.Newf(clierr.CodePortConflict,
			"错误：宿主机端口 %d 被多个组件占用", port).
			WithDetail("占用方", previous).
			WithDetail("占用方", owner).
			WithDetailf("宿主机端口", "%d", port).
			WithHint(
				"给其中一方改 localPort 或 exposePort",
				"或者一次只本地调试其中一个组件",
			)
	}
	t.owner[port] = owner
	return nil
}

// allocate 自动挑一个空闲端口：先试 preferred，被占了再从 base 起递增。
//
// preferred 取 10000 + 容器端口，端口号因此自带含义（postgres 的 5432 → 15432、
// 组件的 8080 → 18080）；排障时一眼能对上是谁。撞车了才退回顺序扫描。
func (t *portTable) allocate(preferred, base int, owner string) int {
	if preferred > 0 && preferred <= maxPort {
		if _, taken := t.owner[preferred]; !taken {
			t.owner[preferred] = owner
			return preferred
		}
	}
	for port := base; port <= maxPort; port++ {
		if _, taken := t.owner[port]; !taken {
			t.owner[port] = owner
			return port
		}
	}
	// 从 8081 起有五万多个端口，走到这里意味着机器上已经没端口可用了
	return 0
}

// ============================================================
// 端口分配
// ============================================================

// assignHostPorts 把 localPort 与所有需要映射到宿主机的端口一次算清。
//
// 顺序不能乱：使用者**写死**的端口（exposePort / localPort / local 组件的额外端口）
// 先占位，自动分配再在剩下的空位里挑。反过来会出现"自动分配抢走了
// 使用者钦定的端口"这种莫名其妙的失败。
func (p *plan) assignHostPorts() error {
	ports := newPortTable()

	// 1) 使用者钦定的 exposePort（冲突已由 checkExposePorts 拦下）
	for _, c := range p.components {
		if !c.Entry.Expose {
			continue
		}
		hostPort, _ := exposeHostPort(c)
		if err := ports.claim(hostPort, "组件 "+refText(c.Ref)+"（expose）"); err != nil {
			return err
		}
		p.exposedPort[c.Service] = hostPort
	}

	// 2) 使用者钦定的 localPort
	for _, l := range p.locals {
		if l.Entry.LocalPort == 0 {
			continue
		}
		if err := ports.claim(l.Entry.LocalPort, "组件 "+refText(l.Ref)+"（localPort）"); err != nil {
			return err
		}
		p.localPort[l.Service] = l.Entry.LocalPort
	}

	// 3) local 组件的额外端口：进程在宿主机上同样要占住它们
	//    （容器里互不干扰的 9090，搬到宿主机上就只有一个）
	for _, l := range p.locals {
		for _, extra := range l.Manifest.Deployment.ExtraPorts {
			owner := fmt.Sprintf("组件 %s 的额外端口 %s", refText(l.Ref), extra.Name)
			if err := ports.claim(extra.Port, owner); err != nil {
				return err
			}
		}
	}

	// 4) 没写 localPort 的自动分配（005 §4.6，延后项 P3）
	//
	//    默认就用组件**自己声明的主端口**：进程在容器里监听的是它，
	//    搬到宿主机上跑的还是同一份代码，监听的当然也是它。直接从 8081 起分配
	//    会得到一个没人监听的端口——依赖方照着连过去只能拿到 connection refused
	//    （真跑起来验过：调用方稳定 503）。8081 是**退路**，只在这个端口
	//    已经被别人占了时才用。
	for i, l := range p.locals {
		if port, ok := p.localPort[l.Service]; ok {
			p.locals[i].Port = port
			continue
		}
		port := ports.allocate(
			l.Manifest.Deployment.Port, localPortBase, "组件 "+refText(l.Ref)+"（自动分配）")
		p.localPort[l.Service] = port
		p.locals[i].Port = port
	}

	// 5) local 组件要访问的容器依赖 → 映射到宿主机（005 §4.8）
	for _, l := range p.locals {
		for _, dep := range p.runningDependencies(l.Ref) {
			p.mapDependencyToHost(ports, dep)
		}
	}

	// 6) local 组件绑定的、由 CLI 托管的资源 → 映射到宿主机
	for _, l := range p.locals {
		for _, r := range p.managedResourcesOf(l.Ref.ID) {
			if _, done := p.resourcePort[r.Host]; done || r.Port == 0 {
				continue
			}
			p.resourcePort[r.Host] = ports.allocate(
				hostPortOffset+r.Port, hostPortBase, "资源 "+r.ID)
		}
	}
	return nil
}

// mapDependencyToHost 让宿主机上的 local 组件够得着容器里的这个依赖。
func (p *plan) mapDependencyToHost(ports *portTable, dep resolver.Ref) {
	service := manifest.ServiceName(dep.ID, dep.Version)

	// 依赖本身也是 local：两个进程都在宿主机上，直接走它的 localPort
	if _, isLocal := p.localPort[service]; isLocal {
		return
	}
	// 不在本次生成的文件里（被跳过了），没什么可映射的
	if !p.rendered[service] {
		return
	}
	// 已经 expose 过就用现成的端口，不重复映射（13.13）
	if _, exposed := p.exposedPort[service]; exposed {
		return
	}
	if _, done := p.debugPort[service]; done {
		return
	}

	node := p.graph.Node(dep)
	if node == nil || node.Manifest == nil {
		return
	}
	owner := "组件 " + refText(dep) + "（供本地调试访问）"
	p.debugPort[service] = ports.allocate(
		hostPortOffset+node.Manifest.Deployment.Port, hostPortBase, owner)

	for _, extra := range node.Manifest.Deployment.ExtraPorts {
		if p.debugExtraPort[service] == nil {
			p.debugExtraPort[service] = map[int]int{}
		}
		p.debugExtraPort[service][extra.Port] = ports.allocate(
			hostPortOffset+extra.Port, hostPortBase, owner+" "+extra.Name)
	}
}

// runningDependencies 返回该组件本次实际启动的依赖（强依赖 + 起来了的弱依赖）。
func (p *plan) runningDependencies(ref resolver.Ref) []resolver.Ref {
	node := p.graph.Node(ref)
	if node == nil {
		return nil
	}

	var out []resolver.Ref
	for _, dep := range append(append([]resolver.Ref{}, node.Requires...), node.Optional...) {
		if p.states.IsRunning(dep) {
			out = append(out, dep)
		}
	}
	sort.Slice(out, func(i, j int) bool { return refText(out[i]) < refText(out[j]) })
	return out
}

// managedResourcesOf 返回绑定到该组件、且由 CLI 托管的资源。
//
// 外部资源（host 是 IP 或域名）不在这里：它本来就在容器网络之外，
// 宿主机上的进程按原地址就能连上，改写反而会连到一个不存在的服务。
func (p *plan) managedResourcesOf(componentID string) []config.Resource {
	var out []config.Resource
	seen := map[string]bool{}
	for _, r := range p.cfg.Resources {
		if !isServiceName(r.Host) || seen[r.Host] {
			continue
		}
		for _, binding := range r.Bindings {
			if binding.ComponentID == componentID {
				seen[r.Host] = true
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// hostAccessPort 返回"从宿主机访问这个依赖的主端口"该用哪个端口。
func (p *plan) hostAccessPort(dep resolver.Ref) (int, bool) {
	service := manifest.ServiceName(dep.ID, dep.Version)
	for _, table := range []map[string]int{p.localPort, p.exposedPort, p.debugPort} {
		if port, ok := table[service]; ok {
			return port, true
		}
	}
	return 0, false
}

// HostPort 是一个会被占用的宿主机端口。
type HostPort struct {
	Port int
	// Owner 说明是谁要用它（组件或资源），出现在体检输出里。
	Owner string
	// Purpose 是用途：expose / 本地调试 / local 组件监听。
	Purpose string
}

// hostPorts 汇总本次会占用的宿主机端口（P22）。
//
// 生成阶段只能保证"项目内部不打架"；这台机器上别的进程占着某个端口，
// 得启动前真的探一下才知道（`--check-resources`）。
func (p *plan) hostPorts() []HostPort {
	var out []HostPort
	for _, c := range p.components {
		if port, ok := p.exposedPort[c.Service]; ok {
			out = append(out, HostPort{port, refText(c.Ref), "expose"})
		}
		if port, ok := p.debugPort[c.Service]; ok {
			out = append(out, HostPort{port, refText(c.Ref), "供本地调试访问"})
		}
		for _, extra := range c.Manifest.Deployment.ExtraPorts {
			if port, ok := p.debugExtraPort[c.Service][extra.Port]; ok {
				out = append(out, HostPort{port, refText(c.Ref), "供本地调试访问 " + extra.Name})
			}
		}
	}
	for _, l := range p.locals {
		out = append(out, HostPort{l.Port, refText(l.Ref), "local 组件监听"})
	}
	for _, r := range p.managed {
		if port, ok := p.resourcePort[r.Host]; ok {
			out = append(out, HostPort{port, "资源 " + r.ID, "供本地调试访问"})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

// ============================================================
// 渲染
// ============================================================

// hostMachineAlias 是"宿主机"在容器里的惯用别名。
//
// 它带点，因此不会被 isServiceName 判成服务名——CLI 不托管它，
// 使用者的意思是"连宿主机上那个已经跑着的库"。但容器里默认解析不了这个名字，
// 必须靠 extra_hosts 指到网关上（005 §7.5）。
const hostMachineAlias = "host.docker.internal"

// extraHostsOf 返回该容器要映射到宿主机的名字。
//
// 两个来源：
//
//	local: true 的依赖组件  服务名 → 宿主机网关（005 §4.2）
//	host.docker.internal    资源在宿主机上时，这个名字得能解析（P34）
//
// 后者是真实装配时踩出来的：把资源 host 写成 host.docker.internal 之后，
// 迁移容器直接报 `dial tcp: lookup host.docker.internal ... no such host`——
// 而这个写法本身完全合理，只是缺了一行 extra_hosts。
func (p *plan) extraHostsOf(c componentPlan) []string {
	seen := map[string]bool{}
	var out []string

	add := func(entry string) {
		if !seen[entry] {
			seen[entry] = true
			out = append(out, entry)
		}
	}

	for _, dep := range p.runningDependencies(c.Ref) {
		service := manifest.ServiceName(dep.ID, dep.Version)
		if _, isLocal := p.localPort[service]; isLocal {
			add(service + ":" + hostGateway(p.engine))
		}
	}
	if p.usesHostMachineResource(c) {
		add(hostMachineAlias + ":" + hostGateway(p.engine))
	}

	sort.Strings(out)
	return out
}

// usesHostMachineResource 判断这个组件有没有绑定跑在宿主机上的资源。
func (p *plan) usesHostMachineResource(c componentPlan) bool {
	for _, resource := range p.cfg.Resources {
		if resource.Host != hostMachineAlias {
			continue
		}
		for _, binding := range resource.Bindings {
			if binding.ComponentID == c.Ref.ID {
				return true
			}
		}
	}
	return false
}

// hostPortsOf 返回该容器要开到宿主机的端口映射。
func (p *plan) hostPortsOf(c componentPlan) []string {
	var out []string
	if hostPort, ok := p.exposedPort[c.Service]; ok {
		out = append(out, fmt.Sprintf("%d:%d", hostPort, c.Manifest.Deployment.Port))
	} else if hostPort, ok := p.debugPort[c.Service]; ok {
		out = append(out, fmt.Sprintf("%d:%d", hostPort, c.Manifest.Deployment.Port))
	}
	for _, extra := range c.Manifest.Deployment.ExtraPorts {
		if hostPort, ok := p.debugExtraPort[c.Service][extra.Port]; ok {
			out = append(out, fmt.Sprintf("%d:%d", hostPort, extra.Port))
		}
	}
	return out
}

// rewriteEndpointsForLocalDependencies 把指向 local 组件的地址换成 localPort。
//
// 服务名保持不变——靠 extra_hosts 把它解析到宿主机，容器里的代码
// 依旧访问 `http://people-basic-1-0-0:8081`，一行都不用改（005 §4.5）。
func (p *plan) rewriteEndpointsForLocalDependencies() {
	for i := range p.components {
		c := &p.components[i]
		for _, dep := range p.runningDependencies(c.Ref) {
			service := manifest.ServiceName(dep.ID, dep.Version)
			port, isLocal := p.localPort[service]
			if !isLocal {
				continue
			}
			setVar(c.Env.Env, manifest.EndpointEnvVar(dep.ID), serviceEndpoint(service, port))
			// 额外端口不改：宿主机上的进程仍然监听 Manifest 里声明的那些端口
		}
	}
}

// localEnvFiles 生成所有 local 组件的调试 env 文件（005 §4.9）。
func (p *plan) localEnvFiles(now time.Time, lookup func(string) (string, bool)) []LocalEnvFile {
	out := make([]LocalEnvFile, 0, len(p.locals))
	for _, l := range p.locals {
		out = append(out, p.localEnvFile(l, now, lookup))
	}
	return out
}

func (p *plan) localEnvFile(l localComponent, now time.Time, lookup func(string) (string, bool)) LocalEnvFile {
	// 从容器版的注入结果出发，只改"怎么连过去"，不改连什么：
	// 库名、配置项、密码引用都应当与容器里跑的完全一致，
	// 否则本地调试出来的行为不能说明容器里也对。
	vars := make([]inject.Var, len(l.Env.Env))
	copy(vars, l.Env.Env)

	p.pointDependenciesAtLocalhost(l, vars)
	p.pointResourcesAtLocalhost(l, vars)

	return LocalEnvFile{
		Ref:     l.Ref,
		Name:    "local-debug." + l.Service + ".env",
		Port:    l.Port,
		Content: renderEnvFile(l, vars, now, lookup),
	}
}

// pointDependenciesAtLocalhost 把依赖地址改成宿主机上的映射端口（13.5）。
func (p *plan) pointDependenciesAtLocalhost(l localComponent, vars []inject.Var) {
	for _, dep := range p.runningDependencies(l.Ref) {
		node := p.graph.Node(dep)
		if node == nil || node.Manifest == nil {
			continue
		}
		if port, ok := p.hostAccessPort(dep); ok {
			setVar(vars, manifest.EndpointEnvVar(dep.ID), localhostEndpoint(port))
		}

		service := manifest.ServiceName(dep.ID, dep.Version)
		prefix := manifest.EnvPrefix(dep.ID)
		for _, extra := range node.Manifest.Deployment.ExtraPorts {
			port, ok := p.debugExtraPort[service][extra.Port]
			if !ok {
				// 依赖也是 local，或者它的额外端口没被映射：
				// 两种情况下宿主机上都是原端口
				port = extra.Port
			}
			setVar(vars, prefix+"_"+strings.ToUpper(extra.Name)+"_ENDPOINT",
				localhostEndpoint(port))
		}
	}
}

// pointResourcesAtLocalhost 把 CLI 托管的资源地址改成 localhost:<映射端口>（13.8）。
//
// 只按"来源是资源连接"且"值恰好是这个资源的 host / port"来改，
// 不按变量名前缀猜——使用者可以给同一类资源起不同的 envPrefix。
func (p *plan) pointResourcesAtLocalhost(l localComponent, vars []inject.Var) {
	for _, r := range p.managedResourcesOf(l.Ref.ID) {
		hostPort, mapped := p.resourcePort[r.Host]
		if !mapped {
			continue
		}
		port := strconv.Itoa(r.Port)
		for i := range vars {
			if vars[i].Source != inject.SourceResource {
				continue
			}
			switch {
			case vars[i].Value == r.Host:
				vars[i].Value = "localhost"
			case strings.HasSuffix(vars[i].Name, "_PORT") && vars[i].Value == port:
				vars[i].Value = strconv.Itoa(hostPort)
			}
		}
	}
}

// renderEnvFile 渲染 .env 文件内容。
func renderEnvFile(
	l localComponent, vars []inject.Var, now time.Time, lookup func(string) (string, bool),
) []byte {
	var b bytes.Buffer
	b.WriteString("# ============================================================\n")
	b.WriteString("# 由 BrickKit CLI 自动生成，供 IDE 加载，请勿手动编辑\n")
	b.WriteString("# 每次 brickkit up 都会重新生成；改动请落到 brickkit.yaml\n")
	fmt.Fprintf(&b, "# 组件：%s@%s（local: true）\n", l.Ref.ID, l.Ref.Version)
	fmt.Fprintf(&b, "# 本地监听端口：%d —— 请让 IDE 里启动的进程监听这个端口\n", l.Port)
	fmt.Fprintf(&b, "# 生成时间：%s\n", now.UTC().Format(time.RFC3339))
	b.WriteString("# 用法：VS Code 在 launch.json 里配 envFile；\n")
	b.WriteString("#       命令行 `set -a && source 本文件 && set +a` 之后再启动进程\n")
	b.WriteString("# ============================================================\n\n")

	for _, v := range vars {
		fmt.Fprintf(&b, "%s=%s\n", v.Name, expandValue(v.Value, lookup))
	}
	return b.Bytes()
}

// envVarRe 匹配 ${ENV_VAR}，与 config 侧的规则一致（003 §5.4）。
var envVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandValue 展开 ${VAR}；展开不了就原样保留。
//
// 原样保留而不是报错：这个文件是"顺手生成的调试辅助"，不该因为一个变量
// 没配就让整个 brickkit up 失败——留着占位符至少还能看出漏了哪个变量。
// （K8s 的 Secret 不一样，那份文件会真的部署上去，所以那边是阻断的。）
func expandValue(raw string, lookup func(string) (string, bool)) string {
	if lookup == nil || !strings.Contains(raw, "${") {
		return raw
	}
	return envVarRe.ReplaceAllStringFunc(raw, func(match string) string {
		if value, ok := lookup(match[2 : len(match)-1]); ok {
			return value
		}
		return match
	})
}

// localMigrationWarnings 提醒 local 组件的迁移得自己跑（13.1）。
//
// local 组件不生成容器，它的迁移容器也就一并没了。不说这一句，
// 开发者会对着一句 "relation does not exist" 找半天。
func (p *plan) localMigrationWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, l := range p.locals {
		if l.Manifest == nil || l.Manifest.Migration == nil {
			continue
		}
		out = append(out, clierr.Warn(clierr.CodeMigrationSkipped,
			"提示：local 组件的数据库迁移不会自动执行").
			WithDetail("组件", refText(l.Ref)).
			WithDetail("原因", "local: true 的组件不生成容器，它的迁移容器也一并跳过").
			WithHint(
				"在本机手动执行该组件的迁移命令："+
					strings.Join(l.Manifest.Migration.Command, " "),
				"环境变量用 local-debug."+l.Service+".env 里的那一份",
			))
	}
	return out
}

// ============================================================
// 小工具
// ============================================================

// setVar 就地改写一条已存在的变量；不存在就不加。
//
// 不存在意味着注入引擎判定"这条不该注入"（比如弱依赖没启动），
// 本地调试没有理由把它凭空补回来。
func setVar(vars []inject.Var, name, value string) {
	for i := range vars {
		if vars[i].Name == name {
			vars[i].Value = value
			return
		}
	}
}

func localhostEndpoint(port int) string { return fmt.Sprintf("http://localhost:%d", port) }

func serviceEndpoint(service string, port int) string {
	return fmt.Sprintf("http://%s:%d", service, port)
}

func refText(ref resolver.Ref) string { return ref.ID + "@" + ref.Version }
