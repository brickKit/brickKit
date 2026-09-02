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
	"strings"
	"time"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/deploy"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
)

// EngineDocker 是目前唯一支持的容器引擎（005 §7 说明了为什么没有 Podman）。
const EngineDocker = "docker"

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
// 值固定是 `host-gateway`。
//
// 保留这段说明是因为设计书原来写错过："Podman 用 host.containers.internal 替代"。那是把两件事
// 搞混了：`host.containers.internal` 是 Podman 自动注入到容器 /etc/hosts 的
// 一个**主机名**，不是 `--add-host` 能接受的**值**——真 Podman 上会直接报
// `invalid IP address in add-host`，容器根本创建不出来，local: true 整条路是断的。
// 而 `host-gateway` 是 Docker 20.10+ 的内置魔法值（Podman 也认，实测 169.254.1.2，
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

// localExposeWarnings 提醒"local 组件上的 expose / exposePort 本次不生效"。
//
// local: true 的组件不生成容器，平台因此没有任何东西可以映射到宿主机——
// 那两个字段就是写了不算数。003 §3.2 立的规矩是**写了不生效就得出声**，
// 而这一条从前完全没守：配了 exposePort: 8888 的人打开浏览器访问 8888
// 什么都没有，而 up 全程一个字不说。
//
// # 为什么是警告，而 local + replicas 是硬错误
//
// 两者不是一回事。`replicas: 3` 与 local 真的互相矛盾——IDE 里只有一个进程，
// 没有任何说得通的读法，所以校验阶段直接拒（config.validateReplicas）。
//
// 而 `expose: true` 与 local 并不矛盾：那个进程**确实**在宿主机上、**确实**
// 对外可达，只是这件事不再由平台来做。真正错的只有"在哪个端口"。所以这里
// 要说的不是"你写错了"，而是"它不归平台管了，而且地址是这个"——报错反而会
// 打断一个很正常的流程：给一个长期 expose 的前端组件临时加上 local: true 去调试。
func (p *plan) localExposeWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, l := range p.locals {
		if !l.Entry.Expose && l.Entry.ExposePort == 0 {
			continue
		}

		fields := "expose"
		if l.Entry.ExposePort > 0 {
			fields = "expose / exposePort"
		}
		w := clierr.Warn(clierr.CodeConfigInvalid,
			"local: true 的组件上，"+fields+" 本次不生效").
			WithDetail("组件", refText(l.Ref)).
			WithDetail("原因", "它不生成容器（跑在你的 IDE 里），平台没有端口可以映射到宿主机")
		if l.Entry.ExposePort > 0 {
			// 点名那个数字：使用者正是照着它去开浏览器的
			w = w.WithDetailf("写着的 exposePort", "%d —— 没有容器会去 bind 它", l.Entry.ExposePort)
		}
		// 光说"不生效"不够，得说清楚东西到底在哪，否则他还要自己去翻另一段输出
		out = append(out, w.
			WithDetailf("实际地址", "localhost:%d（由 localPort 决定，005 §4.6）", l.Port).
			WithHint(
				"让你的进程监听这个端口，浏览器直接访问它",
				"要回到平台映射端口的模式，去掉这个组件的 local: true",
			))
	}
	return out
}

// localLabelWarnings 提醒"local 组件上的 labels 本次没有落脚点"。
//
// 与 expose 那条是同一类事故的同一种处理：写了、没生效、而且**没有任何征兆**
// ——生成物里既不会多一段也不会少一段，`up` 一路绿灯。而 labels 这个字段的
// 用途恰恰是"让外面的工具找到它"，静默失效的表现是"Traefik 里查不到这条路由"，
// 那时人会去翻 Traefik 的日志，翻不出任何东西。
//
// 也和 expose 一样只警告不报错：合并部署交付现场满是 local: true
// （《组件合并部署》§4.3），而那份 brickkit.yaml 常常是从一份完整配置改出来的
// ——labels 留在那里是正常的，只是这一次挂不上。真正要挂 labels 的是使用者
// 自己写的那个外壳，不是这个不生成容器的条目。
func (p *plan) localLabelWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, l := range p.locals {
		if len(l.Entry.Labels) == 0 {
			continue
		}
		out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
			"local: true 的组件上，labels 本次不生效").
			WithDetail("组件", refText(l.Ref)).
			WithDetail("原因", "它不生成容器（跑在你的 IDE 里），没有可以挂标签的对象").
			WithDetailf("写着的键", "%s", strings.Join(sortedLabelKeys(l.Entry.Labels), "、")).
			WithHint(
				"要给自己写的外壳挂标签，写在你那份 compose 文件里",
				"要回到平台挂标签的模式，去掉这个组件的 local: true",
			))
	}
	return out
}

// sortedLabelKeys 让警告里点名的键顺序稳定（map 的遍历顺序是随机的）。
func sortedLabelKeys(labels map[string]string) []string {
	out := make([]string, 0, len(labels))
	for key := range labels {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
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
		hostPort := exposeHostPort(c)
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

	// 基础资源不在这里：平台不部署它们（006 §9.1），它们本来就在容器网络之外。
	// 宿主机上的进程按声明的地址直接连得上，没有可映射的端口。
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

	node := p.graph.Node(dep)
	if node == nil || node.Manifest == nil {
		return
	}
	owner := "组件 " + refText(dep) + "（供本地调试访问）"

	// 主端口：expose 已经把它映射到宿主机了，用现成的那个，不重复占一个（13.13）
	_, exposed := p.exposedPort[service]
	_, mapped := p.debugPort[service]
	if !exposed && !mapped {
		p.debugPort[service] = ports.allocate(
			hostPortOffset+node.Manifest.Deployment.Port, hostPortBase, owner)
	}

	// 额外端口要单独映射，**expose 与否都一样**。
	//
	// `expose: true` 只发布主端口（hostPortsOf 里那一行 `<宿主机端口>:<deployment.port>`），
	// 额外端口一个都不在里面。而从前这个函数在 exposedPort 命中时整个 return，
	// 于是"依赖既 expose 又有 extraPorts"这一种组合下额外端口一个都不映射，
	// local-debug.env 里却照样写着 http://localhost:9090 ——
	// 那个端口宿主机上根本没人监听。表现是 HTTP 通、gRPC 稳定 connection refused，
	// 而两边的配置看上去都没毛病。
	for _, extra := range node.Manifest.Deployment.ExtraPorts {
		if _, done := p.debugExtraPort[service][extra.Port]; done {
			continue
		}
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

// ============================================================
// 渲染
// ============================================================

// hostMachineAlias 是"宿主机"在容器里的惯用别名。
//
// 它带点，因此不会被 isServiceName 判成服务名——CLI 不托管它，
// 使用者的意思是"连宿主机上那个已经跑着的库"。但容器里默认解析不了这个名字，
// 必须靠 extra_hosts 指到网关上（005 §7.5）。
const hostMachineAlias = deploy.HostMachineAlias

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
	p.pointResourcesAtLocalhost(vars)

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
				// 只剩一种情况：依赖自己也是 local。它的进程就在宿主机上，
				// 监听的还是 Manifest 里声明的那个额外端口（local 组件的额外端口
				// 不重新分配，见 assignHostPorts 第 3 步）。
				//
				// 从前这里还兜着"它的额外端口没被映射"——那是 mapDependencyToHost
				// 在依赖 expose 时提前 return 造成的漏洞，会写出一个宿主机上
				// 没人监听的地址。现在额外端口无论 expose 与否都会映射，
				// 这条兜底只服务于 local 依赖了。
				port = extra.Port
			}
			setVar(vars, prefix+"_"+strings.ToUpper(extra.Name)+"_ENDPOINT",
				localhostEndpoint(port))
		}
	}
}

// pointResourcesAtLocalhost 把 `host.docker.internal` 换成 `localhost`。
//
// 平台不部署基础资源（006 §9.1），所以 local 组件连它们时**地址基本不用动**：
// 资源本来就在容器网络之外，宿主机上的进程照着声明连就是了。
//
// 只有一种写法必须改：资源跑在**本机**时，容器里得写 `host.docker.internal`
// （P34，平台会为容器补上 extra_hosts）。而这个名字在 **Linux 的宿主机上
// 解析不了**——它是 Docker 注入到容器 /etc/hosts 里的，宿主机自己没有。
// 不改的话，IDE 里的进程会拿着一个解析不了的主机名去连库，
// 而容器里的同一个组件跑得好好的，最难联想到是这里。
func (p *plan) pointResourcesAtLocalhost(vars []inject.Var) {
	for i := range vars {
		if vars[i].Source == inject.SourceResource && vars[i].Value == hostMachineAlias {
			vars[i].Value = "localhost"
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
