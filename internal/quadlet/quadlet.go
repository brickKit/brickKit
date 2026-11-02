// Package quadlet 把一台机器上的部署计划（internal/machine）渲染成 Podman 的 Quadlet 单元：
// 每个组件一个 .container，每个迁移一个一次性的 .container，项目网络一个 .network。systemd 让它们一直跑着、
// 开机按依赖顺序拉起（部署文件的 podman.units: quadlet）。
//
// 它是纯函数：出来的是单元文件的内容、要建的 Podman secret、引擎要启动的单元名，不碰磁盘、不调 systemctl。
//
// # 与 compose 的不同
//
// 单元由 systemd 在开机时重新创建容器，那时没有使用者的环境，所以 ${VAR} 在生成时求值（与 K8s 一样），
// 求出来的值、密钥与 file:// 内容放进 Podman secret，不写进单元文件——单元文件会被人打开看。
//
// 单元里的值最终由 Quadlet 放进 ExecStart，systemd 在那里展开 ${X}、把 $$ 变成 $、把 %% 变成 %
// （2026-11-02 在 Podman 5.7.0 上实测），所以写进单元的每个值都按 systemdValue 转义。
package quadlet

import (
	"fmt"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/deploy"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/machine"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/yamlcomment"
)

// 单元里的固定值。
const (
	// LabelProject 与 LabelService 让 status 按项目找到容器、按服务名归并（与 K8s 的标签同一个键）。
	LabelProject = "brickkit.io/project"
	LabelService = "brickkit.io/service"
	// ForwardUnit 是有根 Podman 与 Docker 同机时放行 Podman 网桥的那个一次性单元（排障页给出它）。
	// 有根时每个单元都排在它后面：单元不存在时 systemd 忽略这条顺序。
	ForwardUnit = "podman-docker-forward.service"
	// startTimeoutMargin 是启动作业在健康检查的最长等待之外多给的秒数（拉镜像、建网络、进程本身）。
	startTimeoutMargin = 30
	// defaultStartTimeout 是没有健康检查的组件的启动作业时限：只等容器启动。
	defaultStartTimeout = 90
)

// Options 是生成选项。
type Options struct {
	// Root、Lookup 见 machine.Options：file:// 相对 Root，${VAR} 用 Lookup 求值。
	Root   string
	Lookup func(name string) (string, bool)
	// Rootful 是这次装进系统级（root）的 systemd：开机目标与防火墙单元的顺序不同。
	Rootful bool
}

// File 是一个要装进 Quadlet 目录的文件。LinkTo 非空时它是指向同目录里那个模板的链接（副本的实例），
// 没有内容。
type File struct {
	Name    string
	Content []byte
	LinkTo  string
}

// Secret 是一个要建（或替换）的 Podman secret。
type Secret struct {
	Name  string
	Value []byte
}

// Result 是一次生成的产物。
type Result struct {
	// Files 按文件名排序。
	Files []File
	// Secrets 按名字排序。
	Secrets []Secret
	// SecretFiles 是以文件交付的配置项（mount: file），写法与 compose 相同：单元把项目里的目录只读挂进容器。
	SecretFiles []machine.SecretFile
	// Migrations 是迁移单元（systemd 的服务名），按名字排序：up 先单独跑完它们，全部成功才动组件。
	Migrations []string
	// Components 是组件单元（副本展开成每个实例），按名字排序。
	Components []string
	// Network 是 down 要删的网络；部署文件写了 network:（项目自己的网络）时为空。
	Network string
	// Warnings 是不阻断的问题。
	Warnings []*clierr.Error
}

// UnitBase 是一个服务的单元名（不带后缀）：brickkit-<项目名>-<服务名>。systemd 的单元名在一台机器上
// （或一个用户的 systemd 里）是全局的，带上项目名才不会与别的项目撞。
func UnitBase(project, service string) string { return "brickkit-" + project + "-" + service }

// ServiceUnit 是一个服务的 systemd 服务名（不是副本）。
func ServiceUnit(project, service string) string { return UnitBase(project, service) + ".service" }

// MigrationUnit 是一个组件的迁移的 systemd 服务名。
func MigrationUnit(project, service string) string {
	return ServiceUnit(project, machine.MigrationService(service))
}

// NetworkFile 是项目网络的单元文件名。
func NetworkFile(project string) string { return "brickkit-" + project + ".network" }

// generator 是一次渲染。
type generator struct {
	*machine.Plan
	proj    *project.Project
	opts    Options
	project string
	// env 是每个服务的环境：inline 写进单元（Environment=），secrets 是这个服务要挂的 secret（Secret=）。
	inline  map[string][]string
	secrets map[string][]string
	values  map[string][]byte
	// replicas 是多副本组件的服务名 → 副本数（> 1）。
	replicas map[string]int
}

// Generate 渲染本次要跑的全部单元。
//
// 本机进程（mode: local / debug）与焦点在装载时就被拒绝了（deployfile.File.LongRunning），
// 计划里没有它们，也就没有本机端口映射、没有裸进程外壳。
func Generate(
	proj *project.Project, graph *resolver.Graph, states *cascade.Result,
	env *inject.Result, opts Options,
) (*Result, error) {
	plan, err := machine.Build(proj, graph, states, env, machine.Options{Root: opts.Root, Lookup: opts.Lookup})
	if err != nil {
		return nil, err
	}
	g := &generator{
		Plan: plan, proj: proj, opts: opts, project: proj.Decl.Project,
		inline: map[string][]string{}, secrets: map[string][]string{}, values: map[string][]byte{},
		replicas: map[string]int{},
	}
	for _, c := range plan.Components() {
		if n := c.Entry.ReplicaCount(); n > 1 {
			g.replicas[c.Service] = n
		}
	}
	if err := g.placeEnvironment(); err != nil {
		return nil, err
	}

	result := &Result{SecretFiles: plan.SecretFiles(), Warnings: plan.Warnings()}
	if proj.Deploy.Network == "" {
		result.Network = deploy.NetworkName(g.project)
		result.Files = append(result.Files, File{Name: NetworkFile(g.project), Content: g.networkUnit()})
	}
	for _, c := range plan.Components() {
		if c.Manifest.Migration != nil {
			result.Files = append(result.Files, g.migrationFile(c))
			result.Migrations = append(result.Migrations, MigrationUnit(g.project, c.Service))
		}
		files, units := g.componentFiles(c)
		result.Files = append(result.Files, files...)
		result.Components = append(result.Components, units...)
	}
	for _, m := range plan.MemberMigrations() {
		result.Files = append(result.Files, g.migrationFile(m))
		result.Migrations = append(result.Migrations, MigrationUnit(g.project, m.Service))
	}
	for name, value := range g.values {
		result.Secrets = append(result.Secrets, Secret{Name: name, Value: value})
	}
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Name < result.Files[j].Name })
	sort.Slice(result.Secrets, func(i, j int) bool { return result.Secrets[i].Name < result.Secrets[j].Name })
	sort.Strings(result.Migrations)
	sort.Strings(result.Components)
	return result, nil
}

// ============================================================
// 环境变量
// ============================================================

// placeEnvironment 为每个要生成容器的服务算好两份：写进单元的字面量，与要放进 secret 的值。
// 去处的判定与 compose 共用（machine.PlacementOf）；不同的是 Quadlet 在这里就求值，见包注释。
func (g *generator) placeEnvironment() error {
	undefined := map[string]bool{}
	for _, c := range append(append([]machine.Workload{}, g.Components()...), g.MemberMigrations()...) {
		for _, v := range c.Env.Env {
			placement := machine.PlacementOf(v)
			if placement == machine.PlaceSkip {
				continue
			}
			if v.Value.Kind == configdir.KindEnvTemplate {
				g.CollectUndefined(v.Value.Text, undefined)
			}
			if placement == machine.PlaceInline && v.Value.Kind != configdir.KindEnvTemplate &&
				!strings.ContainsAny(v.Value.Text, "\n\r") {
				g.inline[c.Service] = append(g.inline[c.Service], v.Name+"="+v.Value.Text)
				continue
			}
			// 密钥、file://、${VAR} 模板（求出来的值可能是密钥：用户为它写环境变量总有原因）、
			// 带换行的字面量（单元里一行写不下）：求值，进 Podman secret
			value, err := configdir.Evaluate(v.Value, g.Root(), g.Lookup())
			if err != nil {
				return machine.WithVariable(err, c.Ref, v.Name)
			}
			name := secretName(g.project, c.Service, v.Name)
			g.values[name] = []byte(value)
			g.secrets[c.Service] = append(g.secrets[c.Service], name+",type=env,target="+v.Name)
		}
	}
	return machine.UndefinedError(undefined)
}

// secretName 是一条变量的 Podman secret 名：brickkit-<项目名>-<服务名>-<变量名>。
func secretName(project, service, name string) string {
	return UnitBase(project, service) + "-" + name
}

// ============================================================
// 单元
// ============================================================

// unit 按节累积一个单元文件的行。
type unit struct {
	sections map[string][]string
}

func newUnit() *unit { return &unit{sections: map[string][]string{}} }

func (u *unit) add(section, key, value string) {
	u.sections[section] = append(u.sections[section], key+"="+value)
}

// render 按 Unit、Service、Container、Network、Install 的顺序写出各节，开头是“别手改”的说明。
// 不写生成时间：up 靠比较内容判断哪个单元变了、要重启，时间会让每次都不一样。
func (u *unit) render(project string) []byte {
	var b strings.Builder
	b.WriteString(strings.TrimRight(string(yamlcomment.Banner(strings.Join([]string{
		i18n.T(msgid.HeaderDoNotEdit), i18n.T(msgid.HeaderOverwritten), i18n.T(msgid.HeaderProject, project),
	}, "\n"))), "\n") + "\n")
	for _, section := range []string{"Unit", "Service", "Container", "Network", "Install"} {
		lines := u.sections[section]
		if len(lines) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n[%s]\n", section)
		for _, line := range lines {
			b.WriteString(line + "\n")
		}
	}
	return []byte(b.String())
}

func (g *generator) networkUnit() []byte {
	u := newUnit()
	u.add("Unit", "Description", g.description(i18n.T(msgid.QuadletDescriptionNetwork)))
	u.add("Network", "NetworkName", deploy.NetworkName(g.project))
	return u.render(g.project)
}

func (g *generator) description(what string) string {
	return systemdValue(i18n.T(msgid.QuadletDescription, what, g.project))
}

// network 是容器加入的网络：项目网络的单元（Quadlet 自己补上对它的依赖），或部署文件写的项目自己的网络。
func (g *generator) network() string {
	if name := g.proj.Deploy.Network; name != "" {
		return name
	}
	return NetworkFile(g.project)
}

// common 是组件与它的迁移共有的部分：网络、环境、文件交付的配置项、宿主机别名、有根时的顺序。
func (g *generator) common(u *unit, c machine.Workload, mounts []string) {
	if g.opts.Rootful {
		u.add("Unit", "After", ForwardUnit)
	}
	u.add("Container", "Image", systemdValue(manifest.ImageRef(c.Manifest)))
	u.add("Container", "Network", systemdValue(g.network()))
	for _, kv := range g.inline[c.Service] {
		u.add("Container", "Environment", quote(kv))
	}
	for _, s := range g.secrets[c.Service] {
		u.add("Container", "Secret", systemdValue(s))
	}
	sort.Strings(mounts)
	for _, owner := range mounts {
		if g.HasSecretFiles(owner) {
			source := machine.SecretFilesPath(g.proj.Layout) + "/" + owner
			u.add("Container", "Volume", systemdValue(source+":"+manifest.SecretFilesRoot+"/"+owner+":ro"))
		}
	}
	for _, host := range g.ExtraHostsOf(c) {
		u.add("Container", "AddHost", systemdValue(host))
	}
}

// componentFiles 是一个组件的单元文件与它的 systemd 服务名：一个副本时一个 .container；
// 多个副本时一个模板 …@.container 加每个实例一个链接 …@<n>.container（Quadlet 原生支持模板）。
func (g *generator) componentFiles(c machine.Workload) ([]File, []string) {
	base := UnitBase(g.project, c.Service)
	content := g.componentUnit(c)
	n := g.replicas[c.Service]
	if n == 0 {
		return []File{{Name: base + ".container", Content: content}}, []string{base + ".service"}
	}
	template := base + "@.container"
	files := []File{{Name: template, Content: content}}
	var units []string
	for i := 1; i <= n; i++ {
		files = append(files, File{Name: fmt.Sprintf("%s@%d.container", base, i), LinkTo: template})
		units = append(units, fmt.Sprintf("%s@%d.service", base, i))
	}
	return files, units
}

func (g *generator) componentUnit(c machine.Workload) []byte {
	u := newUnit()
	u.add("Unit", "Description", g.description(c.Ref.String()))
	g.waits(u, c)
	g.common(u, c, g.SecretOwnersOf(c))

	name := UnitBase(g.project, c.Service)
	if g.replicas[c.Service] > 0 {
		name += "-%i"
	}
	u.add("Container", "ContainerName", name)
	// 别的组件照旧按服务名找它；外壳还挂上它承载的成员的服务名
	u.add("Container", "NetworkAlias", c.Service)
	for _, alias := range g.ShellAliases(c.Service) {
		u.add("Container", "NetworkAlias", alias)
	}
	for _, port := range g.HostPortsOf(c) {
		u.add("Container", "PublishPort", port)
	}

	timeout := defaultStartTimeout
	if check := machine.HealthCheckOf(c.Manifest); check != nil {
		u.add("Container", "HealthCmd", systemdValue(check.Command))
		u.add("Container", "HealthInterval", machine.HealthInterval)
		u.add("Container", "HealthTimeout", machine.HealthTimeout)
		u.add("Container", "HealthRetries", fmt.Sprint(machine.HealthRetries))
		u.add("Container", "HealthStartPeriod", fmt.Sprintf("%ds", check.StartPeriodSeconds))
		// 启动作业等到健康才算完成：依赖它的单元（After=）因此等到它健康，up 也等到它健康
		u.add("Container", "Notify", "healthy")
		// 探的是存活（没有单独的 readinessCheck）时，不健康就杀掉、由 Restart= 拉起，与 K8s 的存活探针一样。
		// 声明了 readinessCheck 时探的是就绪：暂时不接流量不是死了，不杀（K8s 也不因就绪失败重启）
		if c.Manifest.ReadinessCheck == nil {
			u.add("Container", "HealthOnFailure", "kill")
		}
		timeout = check.StartPeriodSeconds + healthWindow() + startTimeoutMargin
	}
	if seconds := c.Env.StopGracePeriodSeconds; seconds > 0 {
		u.add("Container", "StopTimeout", fmt.Sprint(seconds))
	}
	for _, label := range labels(c.Env.Labels) {
		u.add("Container", "Label", quote(label))
	}
	u.add("Container", "Label", quote(LabelProject+"="+g.project))
	u.add("Container", "Label", quote(LabelService+"="+c.Service))
	u.add("Container", "LogDriver", "journald")
	if args := quotaArgs(c); args != "" {
		u.add("Container", "PodmanArgs", args)
	}

	u.add("Service", "Restart", "always")
	u.add("Service", "TimeoutStartSec", fmt.Sprint(timeout))
	u.add("Install", "WantedBy", g.wantedBy())
	return u.render(g.project)
}

// waits 把启动等待写成单元之间的依赖。
//
//	迁移（自己的、外壳承载的成员的）  Requires= + After=：失败就不启动，开机也一样
//	强依赖                          Wants= + After=：只排顺序。Requires= 会在依赖被停止或重启时连带停掉、
//	                                重启这个组件，而平台的约定是组件自己重试连接（与 compose 的 depends_on 一样）
//
// 等到“健康”还是“启动了”由依赖自己的单元决定：有健康检查的写了 Notify=healthy，启动作业等到健康才完成。
func (g *generator) waits(u *unit, c machine.Workload) {
	for _, w := range g.Waits(c) {
		if w.Kind == machine.WaitCompleted {
			dep := ServiceUnit(g.project, w.Service)
			u.add("Unit", "Requires", dep)
			u.add("Unit", "After", dep)
			continue
		}
		for _, dep := range g.unitsOf(w.Service) {
			u.add("Unit", "Wants", dep)
			u.add("Unit", "After", dep)
		}
	}
}

// unitsOf 是一个组件的 systemd 服务名：多副本时每个实例一个。
func (g *generator) unitsOf(service string) []string {
	n := g.replicas[service]
	if n == 0 {
		return []string{ServiceUnit(g.project, service)}
	}
	out := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, fmt.Sprintf("%s@%d.service", UnitBase(g.project, service), i))
	}
	return out
}

// migrationFile 是一个迁移的一次性单元。不写 RemainAfterExit：跑完回到 inactive，下一次 start 会再跑一遍
// （up 每次都跑迁移，组件重启时也先再跑一遍）；跑完转 inactive 不会连带停掉 Requires= 它的组件。
func (g *generator) migrationFile(c machine.Workload) File {
	service := machine.MigrationService(c.Service)
	u := newUnit()
	u.add("Unit", "Description", g.description(i18n.T(msgid.QuadletDescriptionMigration, c.Ref.String())))
	// 同一组件的上一个版本的迁移必须先成功结束（machine 的迁移链）
	if previous := g.MigrationAfter(c.Service); previous != "" {
		dep := ServiceUnit(g.project, previous)
		u.add("Unit", "Requires", dep)
		u.add("Unit", "After", dep)
	}
	// 环境变量与主容器完全一致，以文件交付的那几项也一样挂进来
	g.common(u, c, []string{c.Service})
	u.add("Container", "ContainerName", UnitBase(g.project, service))

	// 必须同时覆盖 entrypoint 与参数：组件镜像普遍带 ENTRYPOINT，只给参数会拼成 `<entrypoint> <迁移命令>`，
	// 于是“迁移”实际上把服务起了起来、永远不结束（compose 那边真跑起来撞到过）
	command := c.Manifest.Migration.Command
	u.add("Container", "Entrypoint", systemdValue(command[0]))
	if len(command) > 1 {
		args := make([]string, len(command)-1)
		for i, arg := range command[1:] {
			args[i] = quote(arg)
		}
		u.add("Container", "Exec", strings.Join(args, " "))
	}
	u.add("Container", "Label", quote(LabelProject+"="+g.project))
	u.add("Container", "Label", quote(LabelService+"="+service))
	u.add("Container", "LogDriver", "journald")
	u.add("Service", "Type", "oneshot")
	return File{Name: UnitBase(g.project, service) + ".container", Content: u.render(g.project)}
}

// wantedBy 是开机时拉起组件的目标：用户的 systemd 是 default.target，系统的是 multi-user.target。
func (g *generator) wantedBy() string {
	if g.opts.Rootful {
		return "multi-user.target"
	}
	return "default.target"
}

// healthWindow 是启动宽限期之后、判定不健康之前最多还要等的秒数：interval × retries。
func healthWindow() int {
	var interval int
	_, _ = fmt.Sscanf(machine.HealthInterval, "%ds", &interval)
	return interval * machine.HealthRetries
}

// quotaArgs 是资源配额的 podman 参数。Quadlet 没有 CPU 的键，Memory= 要 Podman 5.5，所以统一走 PodmanArgs=
// （最低版本见 engine 的 Quadlet）。requests 的 CPU 在一台机器上没有对应的限制，与 compose 一样只是记录。
func quotaArgs(c machine.Workload) string {
	var args []string
	if limits := c.Env.Resources.Limits; limits != nil {
		if cpus, err := machine.CPUs(limits.CPU); err == nil && cpus != "" {
			args = append(args, "--cpus="+cpus)
		}
		if memory, err := machine.Memory(limits.Memory); err == nil && memory != "" {
			args = append(args, "--memory="+memory)
		}
	}
	if requests := c.Env.Resources.Requests; requests != nil {
		if memory, err := machine.Memory(requests.Memory); err == nil && memory != "" {
			args = append(args, "--memory-reservation="+memory)
		}
	}
	return strings.Join(args, " ")
}

// labels 是透传的 labels，按键排序，写成 key=value。
func labels(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key, value := range m {
		out = append(out, key+"="+value)
	}
	sort.Strings(out)
	return out
}

// systemdValue 转义一个不加引号写进单元的值：% 是 systemd 的说明符，$ 在 ExecStart 里会被展开。
func systemdValue(s string) string {
	return strings.NewReplacer("%", "%%", "$", "$$").Replace(s)
}

// quote 把一个值写成 systemd 的双引号字符串（Environment=、Label=、Exec= 的一个参数）：
// 反斜杠与双引号按 C 的写法转义，% 与 $ 同 systemdValue。值里不能有换行（带换行的变量进 secret）。
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "$", "$$").Replace(s) + `"`
}
