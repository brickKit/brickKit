// Package compose 把一台机器上的部署计划（internal/machine）渲染成 compose.yaml
// 。
//
// 它是纯函数：进去的是配置与三份计算结果，出来的是文件内容与一份
// "使用者还需要做什么"的清单。不碰磁盘、不调 docker——那是 up 的事。
package compose

import (
	"bytes"
	"fmt"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deploy"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/machine"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
)

// 生成文件里的固定值。
const (
	// networkAlias 是 compose 文件内部引用网络的别名；真实网络名见 networkName。
	networkAlias = "brickkit-net"
	// healthcheckStartInterval 是 start_period 里、第一次成功之前的检查间隔：它决定多快发现组件已经起来，
	// 依赖方等 service_healthy、`up --wait` 都等它，依赖链有几层就等几次。成功之后改回 interval，稳态没有成本。
	// 不写时 Docker 25+ 在启动阶段每 5 秒查一次；实测（Docker 29.9.2，一启动就就绪的容器，各 5 次）
	// `up -d --wait` 从 5.73–5.74 秒降到 1.73–1.76 秒。Podman 的兼容 API（5.7.0 报 1.41）不认这个字段、
	// 照 interval 查，不报错，加不加看不出差别（compose 对报 1.41 的引擎照样传这个字段）；Docker 25 之前的引擎
	// 也报 1.41 一类的旧版本、没有这个功能，推断同样忽略，没有实测。
	healthcheckStartInterval = "1s"
)

// Options 是生成选项。
type Options struct {
	// Now 用于文件头的生成时间，测试可注入。
	Now func() time.Time
	// Root、Lookup、HostAliasOnLoopback 见 machine.Options。
	Root                string
	Lookup              func(name string) (string, bool)
	HostAliasOnLoopback func() bool
}

// EnvFile 是一个服务的 0600 环境变量文件：密钥与 file:// 内容不进 compose.yaml。
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
	// SecretFiles 是以文件交付的配置项（configSchema 的 mount: file），按服务名、键名排序。
	// 怎么写盘见 machine.SecretFileDir 那段注释：目录与文件的权限、原地替换都有讲究。
	SecretFiles []machine.SecretFile
	// LocalEnvFiles 是本机进程组件（mode: debug 与 mode: local）的环境变量文件。
	LocalEnvFiles []machine.LocalEnvFile
	// MemberEnvFiles 是本机外壳承载的、从源码运行的成员的环境变量文件（LocalEnvFile.Shell 非零）：
	// 它们的迁移从源码跑（local.migrationCommand），用的就是这份变量。
	MemberEnvFiles []machine.LocalEnvFile
	// RunAfter 是 up 之后要单独跑完的一次性 service：裸进程外壳承载的成员的迁移——
	// 外壳不在 compose 文件里，没有 service 通过 depends_on 等着它们（engine.UpRequest.RunAfter）。
	RunAfter []string
	// HostAlias 说本机进程拿到的 host.docker.internal 是原样给的还是换成了 localhost（up 打印一行）。
	HostAlias machine.HostAliasUse
	// Warnings 是不阻断的问题。
	Warnings []*clierr.Error
}

// renderer 是一次渲染：计划加上 compose 自己的环境变量编码（envplace.go）。
type renderer struct {
	*machine.Plan
	// inline / envFile 是每个服务算好的环境变量去处：
	// inline 进 compose.yaml 的 environment，envFile 进 0600 的 env 文件。
	inline  map[string][]string
	envFile map[string][]byte
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

	plan, err := machine.Build(proj, graph, states, env, machine.Options{
		Root: opts.Root, Lookup: opts.Lookup, HostAliasOnLoopback: opts.HostAliasOnLoopback,
	})
	if err != nil {
		return nil, err
	}
	r := &renderer{Plan: plan, inline: map[string][]string{}, envFile: map[string][]byte{}}
	if err := r.placeEnvironment(); err != nil {
		return nil, err
	}

	networks := map[string]any{
		networkAlias: map[string]any{
			"name":   networkName(proj.Decl.Project),
			"driver": "bridge",
		},
	}
	// 部署文件说网络由项目提供：只引用，不建也不删（compose 对 external 网络两样都不做）
	if name := proj.Deploy.Network; name != "" {
		networks[networkAlias] = map[string]any{"name": name, "external": true}
	}

	doc := map[string]any{
		"services": r.services(),
		"networks": networks,
	}
	body, err := marshal(doc)
	if err != nil {
		return nil, err
	}

	now := opts.Now()
	locals, err := plan.LocalEnvFiles(now)
	if err != nil {
		return nil, err
	}
	members, err := plan.MemberEnvFiles(now)
	if err != nil {
		return nil, err
	}
	return &Result{
		MemberEnvFiles: members,
		YAML:           append(header(proj, plan, now), body...),
		EnvFiles:       r.envFileList(),
		SecretFiles:    plan.SecretFiles(),
		LocalEnvFiles:  locals,
		RunAfter:       plan.RunAfter(),
		HostAlias:      plan.HostAlias(),
		Warnings:       plan.Warnings(),
	}, nil
}

// networkName 是项目专属网络名：brickkit-<项目名>-net。
func networkName(project string) string { return deploy.NetworkName(project) }

// header 是生成文件的头注释。
//
// 生成的文件会被人打开看、被 git 记录，所以要写清楚"这是谁生成的、别手改"。
func header(proj *project.Project, plan *machine.Plan, now time.Time) []byte {
	return deploy.FileHeader(proj.Decl.Project, now,
		filepath.Base(proj.DeployPath)+" → target: "+proj.Deploy.Target,
		i18n.T(msgid.ComposeHeaderComponents, len(plan.Components())))
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

// MigrationService 是一个组件迁移容器的 service 名。CLI 在 up 失败后按它认出失败的迁移。
func MigrationService(service string) string { return machine.MigrationService(service) }

// services 渲染 services 段。
func (r *renderer) services() map[string]any {
	services := map[string]any{}

	for _, c := range r.Components() {
		if c.Manifest.Migration != nil {
			services[machine.MigrationService(c.Service)] = r.migrationDoc(c)
		}
		services[c.Service] = r.componentService(c)
	}
	for _, m := range r.MemberMigrations() {
		services[machine.MigrationService(m.Service)] = r.migrationDoc(m)
	}
	return services
}

// ============================================================
// 组件 service
// ============================================================

// restartPolicy 是组件容器的重启策略：崩溃后拉起、开机时再启动，都交给引擎。
// Docker 用 unless-stopped：守护进程开机时拉起它们，手动 stop 过的保持停止。
// Podman 用 always：两者在 Podman 运行时完全相同（podman-run(1)），但它的开机单元
// podman-restart.service 只启动 restart-policy=always 的容器（5.7.0 实测），unless-stopped 开机不会回来。
func restartPolicy(target string) string {
	if target == deployfile.TargetPodman {
		return "always"
	}
	return "unless-stopped"
}

func (r *renderer) componentService(c machine.Workload) map[string]any {
	svc := map[string]any{
		"image":   manifest.ImageRef(c.Manifest),
		"restart": restartPolicy(r.Project().Deploy.Target),
	}
	if aliases := r.ShellAliases(c.Service); len(aliases) > 0 {
		aliasList := make([]any, len(aliases))
		for i, a := range aliases {
			aliasList[i] = a
		}
		svc["networks"] = map[string]any{networkAlias: map[string]any{"aliases": aliasList}}
	} else {
		svc["networks"] = []any{networkAlias}
	}

	r.applyEnvironment(svc, c.Service)
	// 以文件交付的配置项：自己的目录，外壳还要挂上它承载的成员的
	r.applySecretVolumes(svc, r.SecretOwnersOf(c)...)
	if ports := r.HostPortsOf(c); len(ports) > 0 {
		svc["ports"] = ports
	}
	// 把 local 组件的服务名解析到宿主机，容器里的代码一行不用改
	if hosts := r.ExtraHostsOf(c); len(hosts) > 0 {
		svc["extra_hosts"] = hosts
	}
	if health := healthcheckOf(c.Manifest); health != nil {
		svc["healthcheck"] = health
	}
	if seconds := c.Env.StopGracePeriodSeconds; seconds > 0 {
		svc["stop_grace_period"] = fmt.Sprintf("%ds", seconds)
	}
	if deploy := deployOf(c); deploy != nil {
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
	if dependsOn := dependsOnOf(r.Waits(c)); len(dependsOn) > 0 {
		svc["depends_on"] = dependsOn
	}
	return svc
}

// dependsOnOf 把启动等待（machine.Plan.Waits）写成 compose 的 depends_on 条件。
func dependsOnOf(waits []machine.Wait) map[string]any {
	dependsOn := map[string]any{}
	for _, w := range waits {
		switch w.Kind {
		case machine.WaitCompleted:
			// 等迁移成功结束，而不是等它"起来了"
			dependsOn[w.Service] = condition("service_completed_successfully")
		case machine.WaitHealthy:
			dependsOn[w.Service] = condition("service_healthy")
		default:
			dependsOn[w.Service] = condition("service_started")
		}
	}
	return dependsOn
}

func condition(value string) map[string]any { return map[string]any{"condition": value} }

// migrationDoc 生成迁移用的一次性 service。
func (r *renderer) migrationDoc(c machine.Workload) map[string]any {
	svc := map[string]any{
		// 用组件自己的镜像，迁移脚本与业务代码同版本
		"image":    manifest.ImageRef(c.Manifest),
		"networks": []string{networkAlias},
		// 一次性任务，失败了要让人看见，不能自动重启
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
	// 环境变量与主容器完全一致（同一份 inline 与同一个 env 文件），以文件交付的那几项也一样挂进来
	r.applyEnvironment(svc, c.Service)
	r.applySecretVolumes(svc, c.Service)
	// 环境变量一致，寻址方式也得一致：拿到一个指向宿主机的地址
	// 却没有 extra_hosts，这个主机名在迁移容器里根本解析不了
	if hosts := r.ExtraHostsOf(c); len(hosts) > 0 {
		svc["extra_hosts"] = hosts
	}

	// 同一组件的上一个版本的迁移必须先成功结束（见 machine 的 chainMigrations）。
	//
	// 资源不在这里：资源不由平台部署，compose 文件里根本没有对应的 service，
	// 写进 depends_on 只会让 compose 直接报错。迁移是第一个连库的东西，
	// 库没起来它就是第一个失败的——那条 connection refused 比任何
	// 平台自己编的说法都准确
	if previous := r.MigrationAfter(c.Service); previous != "" {
		svc["depends_on"] = map[string]any{
			previous: condition("service_completed_successfully"),
		}
	}
	return svc
}

// healthcheckOf 把容器的健康检查（machine.HealthCheckOf）写成 compose 的 healthcheck。
//
// compose 的 healthcheck 不杀容器，它只决定依赖方什么时候启动、up --wait 什么时候返回。
func healthcheckOf(m *manifest.Manifest) map[string]any {
	check := machine.HealthCheckOf(m)
	if check == nil {
		return nil
	}
	return map[string]any{
		"test":           []string{"CMD-SHELL", check.Command},
		"interval":       machine.HealthInterval,
		"timeout":        machine.HealthTimeout,
		"retries":        machine.HealthRetries,
		"start_period":   fmt.Sprintf("%ds", check.StartPeriodSeconds),
		"start_interval": healthcheckStartInterval,
	}
}

// deployOf 渲染 compose 的 deploy 段：副本数与资源配额。
//
// 副本数只在大于 1 时写：compose 缺省就是 1。compose 的 limits / reservations
// 表达 K8s 的 limits / requests。
func deployOf(c machine.Workload) map[string]any {
	deploy := map[string]any{}
	if replicas := c.Entry.ReplicaCount(); replicas > 1 {
		deploy["replicas"] = replicas
	}

	resources := map[string]any{}
	if spec := quotaOf(c.Env.Resources.Limits); len(spec) > 0 {
		resources["limits"] = spec
	}
	if spec := quotaOf(c.Env.Resources.Requests); len(spec) > 0 {
		resources["reservations"] = spec
	}
	if len(resources) > 0 {
		deploy["resources"] = resources
	}
	if len(deploy) == 0 {
		return nil
	}
	return deploy
}

func quotaOf(spec *manifest.ResourceSpec) map[string]any {
	if spec == nil {
		return nil
	}
	out := map[string]any{}
	if cpus, err := machine.CPUs(spec.CPU); err == nil && cpus != "" {
		out["cpus"] = cpus
	}
	if memory, err := machine.Memory(spec.Memory); err == nil && memory != "" {
		out["memory"] = memory
	}
	return out
}
