// 本文件测 Quadlet 单元的生成。断言落在“单元文件里写了什么”，systemd 与 Podman 怎样解读这些写法
// 要在真的 systemd 与 Podman 上跑才看得出来。
package quadlet_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project/projecttest"
	"github.com/brickkit/brickkit/internal/quadlet"
	"github.com/brickkit/brickkit/internal/resolver"
)

type stubProvider map[string]*manifest.Manifest

func (p stubProvider) Manifest(_ context.Context, id, version string) (*manifest.Manifest, error) {
	m, ok := p[id+"@"+version]
	if !ok {
		return nil, assert.AnError
	}
	return m, nil
}

type builder struct {
	t        *testing.T
	provider stubProvider
	spec     projecttest.Spec
	env      map[string]string
	rootful  bool
	root     string
}

func newBuilder(t *testing.T) *builder {
	return &builder{t: t, provider: stubProvider{}, spec: projecttest.Spec{
		Project: "shop", Target: deployfile.TargetPodman,
		Podman: &deployfile.Podman{Units: deployfile.UnitsQuadlet}, Files: projecttest.Files{},
	}}
}

func (b *builder) component(m *manifest.Manifest, entry projecttest.Entry) *builder {
	b.provider[m.Metadata.ID+"@"+m.Metadata.Version] = m
	entry.ID, entry.Version = m.Metadata.ID, m.Metadata.Version
	b.spec.Entries = append(b.spec.Entries, entry)
	return b
}

func (b *builder) build() (*quadlet.Result, error) {
	b.t.Helper()
	proj := projecttest.Build(b.t, b.spec)
	projecttest.FillShellCapability(b.spec, b.provider)
	b.root = proj.Layout.Root
	graph, err := resolver.New(b.provider).ResolveProject(context.Background(), proj)
	require.NoError(b.t, err)
	states, err := cascade.Compute(proj, graph)
	require.NoError(b.t, err)
	env, err := inject.Build(proj, graph, states)
	require.NoError(b.t, err)
	return quadlet.Generate(proj, graph, states, env, quadlet.Options{
		Root:    proj.Layout.Root,
		Lookup:  func(name string) (string, bool) { v, ok := b.env[name]; return v, ok },
		Rootful: b.rootful,
	})
}

func (b *builder) generate() *quadlet.Result {
	b.t.Helper()
	result, err := b.build()
	require.NoError(b.t, err)
	return result
}

// file 返回某个单元文件的内容；没有时测试失败。
func file(t *testing.T, r *quadlet.Result, name string) string {
	t.Helper()
	for _, f := range r.Files {
		if f.Name == name {
			return string(f.Content)
		}
	}
	require.Failf(t, "没有这个单元文件", "%s；有的是 %v", name, names(r))
	return ""
}

func names(r *quadlet.Result) []string {
	var out []string
	for _, f := range r.Files {
		out = append(out, f.Name)
	}
	return out
}

func secrets(r *quadlet.Result) map[string]string {
	out := map[string]string{}
	for _, s := range r.Secrets {
		out[s.Name] = string(s.Value)
	}
	return out
}

func simple(id, version string, port int) *manifest.Manifest {
	return &manifest.Manifest{
		APIVersion: manifest.APIVersion,
		Kind:       manifest.Kind,
		Metadata:   manifest.Metadata{ID: id, Name: id, Version: version},
		Deployment: manifest.Deployment{
			Type: manifest.DeploymentTypeContainer, Image: "registry.example.com/" + strings.ReplaceAll(id, "/", "-") + ":" + version,
			Port: port,
		},
		HealthCheck: manifest.HealthCheck{Type: manifest.HealthCheckHTTP, Path: "/healthz"},
	}
}

func dependsOn(m *manifest.Manifest, id, version string) *manifest.Manifest {
	if m.Dependencies == nil {
		m.Dependencies = &manifest.Dependencies{}
	}
	m.Dependencies.Components = append(m.Dependencies.Components, manifest.ComponentDep{ID: id, Version: version})
	return m
}

func withMigration(m *manifest.Manifest) *manifest.Manifest {
	m.Migration = &manifest.Migration{Command: []string{"/app/server", "migrate", "--to", "latest version"}}
	return m
}

func withSchema(m *manifest.Manifest, props map[string]manifest.ConfigProperty) *manifest.Manifest {
	m.ConfigSchema = &manifest.ConfigSchema{Properties: props}
	return m
}

// 一个组件：项目网络一个 .network，组件一个 .container；别的组件按服务名找它，status 按两个标签认它，
// 日志进 journald，systemd 让它一直跑着、开机拉起。
func TestComponentUnit(t *testing.T) {
	r := newBuilder(t).component(simple("erp/api", "1.0.0", 8080), projecttest.Entry{}).generate()

	assert.Equal(t, []string{"brickkit-shop-erp-api-1-0-0.container", "brickkit-shop.network"}, names(r))
	assert.Equal(t, "brickkit-shop-net", r.Network)
	assert.Contains(t, file(t, r, "brickkit-shop.network"), "[Network]\nNetworkName=brickkit-shop-net\n")
	assert.Equal(t, []string{"brickkit-shop-erp-api-1-0-0.service"}, r.Components)
	assert.Empty(t, r.Migrations)

	unit := file(t, r, "brickkit-shop-erp-api-1-0-0.container")
	for _, line := range []string{
		"Image=registry.example.com/erp-api:1.0.0",
		"Network=brickkit-shop.network",
		"ContainerName=brickkit-shop-erp-api-1-0-0",
		"NetworkAlias=erp-api-1-0-0",
		`Label="brickkit.io/project=shop"`,
		`Label="brickkit.io/service=erp-api-1-0-0"`,
		"LogDriver=journald",
		`Environment="PORT=8080"`,
		"[Service]\nRestart=always",
		"[Install]\nWantedBy=default.target",
	} {
		assert.Contains(t, unit, line+"\n")
	}
	assert.NotContains(t, unit, "podman-docker-forward")
}

// 单元里不写生成时间：up 靠比较内容判断哪个单元变了，两次生成必须一样。
func TestUnitsAreDeterministic(t *testing.T) {
	b := newBuilder(t).component(withMigration(simple("erp/api", "1.0.0", 8080)), projecttest.Entry{})
	first := b.generate()
	second := b.generate()
	require.Equal(t, len(first.Files), len(second.Files))
	for i := range first.Files {
		assert.Equal(t, string(first.Files[i].Content), string(second.Files[i].Content), first.Files[i].Name)
	}
}

// 有根：系统的开机目标是 multi-user.target；每个单元排在放行 Podman 网桥的防火墙单元之后
// （与 Docker 同机时要它，单元不存在时 systemd 忽略这条顺序）。
func TestRootfulUnits(t *testing.T) {
	b := newBuilder(t).component(withMigration(simple("erp/api", "1.0.0", 8080)), projecttest.Entry{})
	b.rootful = true
	r := b.generate()

	unit := file(t, r, "brickkit-shop-erp-api-1-0-0.container")
	assert.Contains(t, unit, "WantedBy=multi-user.target\n")
	assert.Contains(t, unit, "After=podman-docker-forward.service\n")
	assert.Contains(t, file(t, r, "brickkit-shop-erp-api-1-0-0-migration.container"), "After=podman-docker-forward.service\n")
}

// 迁移是一次性单元：不留存（跑完回到 inactive，下次 start 再跑），组件 Requires= + After= 它；
// entrypoint 与参数都覆盖，参数按 systemd 的规则加引号（带空格的参数保持一个）。
func TestMigrationUnit(t *testing.T) {
	r := newBuilder(t).component(withMigration(simple("erp/api", "1.0.0", 8080)), projecttest.Entry{}).generate()

	assert.Equal(t, []string{"brickkit-shop-erp-api-1-0-0-migration.service"}, r.Migrations)
	migration := file(t, r, "brickkit-shop-erp-api-1-0-0-migration.container")
	assert.Contains(t, migration, "[Service]\nType=oneshot\n")
	assert.NotContains(t, migration, "RemainAfterExit")
	assert.NotContains(t, migration, "Restart=")
	assert.NotContains(t, migration, "[Install]")
	assert.Contains(t, migration, "Entrypoint=/app/server\n")
	assert.Contains(t, migration, `Exec="migrate" "--to" "latest version"`+"\n")
	assert.Contains(t, migration, `Environment="PORT=8080"`+"\n", "环境变量与主容器一致")
	assert.NotContains(t, migration, "NetworkAlias", "迁移不是服务，别的组件不找它")

	unit := file(t, r, "brickkit-shop-erp-api-1-0-0.container")
	assert.Contains(t, unit, "Requires=brickkit-shop-erp-api-1-0-0-migration.service\n")
	assert.Contains(t, unit, "After=brickkit-shop-erp-api-1-0-0-migration.service\n")
}

// 同一组件的多个版本：迁移按版本号串起来，后一个 Requires= + After= 前一个。
func TestMigrationsOfOneComponentAreChained(t *testing.T) {
	b := newBuilder(t)
	b.component(withMigration(simple("erp/api", "1.0.0", 8080)), projecttest.Entry{})
	b.component(withMigration(simple("erp/api", "2.0.0", 8080)), projecttest.Entry{RequiredBy: []string{"erp/web"}})
	b.component(dependsOn(simple("erp/web", "1.0.0", 8080), "erp/api", "2.0.0"), projecttest.Entry{})
	r := b.generate()

	later := file(t, r, "brickkit-shop-erp-api-2-0-0-migration.container")
	assert.Contains(t, later, "Requires=brickkit-shop-erp-api-1-0-0-migration.service\n")
	assert.Contains(t, later, "After=brickkit-shop-erp-api-1-0-0-migration.service\n")
	assert.NotContains(t, file(t, r, "brickkit-shop-erp-api-1-0-0-migration.container"), "Requires=")
}

// 强依赖只排顺序（Wants= + After=），不连带停止；有健康检查的单元写 Notify=healthy，
// 依赖方因此等到它健康。
func TestDependencyOrdersAndWaitsForHealth(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("erp/api", "1.0.0", 8080), projecttest.Entry{})
	b.component(dependsOn(simple("erp/web", "1.0.0", 8081), "erp/api", "1.0.0"), projecttest.Entry{})
	r := b.generate()

	web := file(t, r, "brickkit-shop-erp-web-1-0-0.container")
	assert.Contains(t, web, "Wants=brickkit-shop-erp-api-1-0-0.service\n")
	assert.Contains(t, web, "After=brickkit-shop-erp-api-1-0-0.service\n")
	assert.NotContains(t, web, "Requires=brickkit-shop-erp-api")
	assert.Contains(t, file(t, r, "brickkit-shop-erp-api-1-0-0.container"), "Notify=healthy\n")
}

// 健康检查：节奏与 compose 相同。探的是存活（没写 readinessCheck）时不健康就杀掉、由 Restart= 拉起；
// 写了 readinessCheck 时探它、不杀。没有健康检查时不写 Notify=healthy（等不到的健康会让启动永远不结束）。
func TestHealthCheck(t *testing.T) {
	b := newBuilder(t)
	live := simple("erp/live", "1.0.0", 8080)
	live.HealthCheck.StartPeriodSeconds = 120
	ready := simple("erp/ready", "1.0.0", 8081)
	ready.ReadinessCheck = &manifest.ReadinessCheck{Type: manifest.HealthCheckHTTP, Path: "/ready"}
	none := simple("erp/none", "1.0.0", 8082)
	none.HealthCheck = manifest.HealthCheck{Type: manifest.HealthCheckNone}
	b.component(live, projecttest.Entry{}).component(ready, projecttest.Entry{}).component(none, projecttest.Entry{})
	r := b.generate()

	unit := file(t, r, "brickkit-shop-erp-live-1-0-0.container")
	for _, line := range []string{
		"HealthCmd=wget -q -O /dev/null http://127.0.0.1:8080/healthz || curl -fsS -o /dev/null http://127.0.0.1:8080/healthz || exit 1",
		"HealthInterval=10s", "HealthTimeout=3s", "HealthRetries=3", "HealthStartPeriod=120s",
		"Notify=healthy", "HealthOnFailure=kill",
		"TimeoutStartSec=180",
	} {
		assert.Contains(t, unit, line+"\n")
	}

	readyUnit := file(t, r, "brickkit-shop-erp-ready-1-0-0.container")
	assert.Contains(t, readyUnit, "http://127.0.0.1:8081/ready")
	assert.Contains(t, readyUnit, "Notify=healthy\n")
	assert.NotContains(t, readyUnit, "HealthOnFailure")

	noneUnit := file(t, r, "brickkit-shop-erp-none-1-0-0.container")
	assert.NotContains(t, noneUnit, "HealthCmd")
	assert.NotContains(t, noneUnit, "Notify=")
	assert.Contains(t, noneUnit, "TimeoutStartSec=90\n")
}

// 多副本：一个模板加每个实例一个链接，容器名带实例号、别名相同（DNS 返回全部副本）；
// 依赖它的单元等每一个实例。
func TestReplicasAreTemplateInstances(t *testing.T) {
	two := 2
	b := newBuilder(t)
	b.component(simple("erp/api", "1.0.0", 8080), projecttest.Entry{Replicas: &two})
	b.component(dependsOn(simple("erp/web", "1.0.0", 8081), "erp/api", "1.0.0"), projecttest.Entry{})
	r := b.generate()

	assert.Contains(t, names(r), "brickkit-shop-erp-api-1-0-0@.container")
	links := map[string]string{}
	for _, f := range r.Files {
		if f.LinkTo != "" {
			links[f.Name] = f.LinkTo
		}
	}
	assert.Equal(t, map[string]string{
		"brickkit-shop-erp-api-1-0-0@1.container": "brickkit-shop-erp-api-1-0-0@.container",
		"brickkit-shop-erp-api-1-0-0@2.container": "brickkit-shop-erp-api-1-0-0@.container",
	}, links)
	assert.Equal(t, []string{
		"brickkit-shop-erp-api-1-0-0@1.service", "brickkit-shop-erp-api-1-0-0@2.service", "brickkit-shop-erp-web-1-0-0.service",
	}, r.Components)

	template := file(t, r, "brickkit-shop-erp-api-1-0-0@.container")
	assert.Contains(t, template, "ContainerName=brickkit-shop-erp-api-1-0-0-%i\n")
	assert.Contains(t, template, "NetworkAlias=erp-api-1-0-0\n")

	web := file(t, r, "brickkit-shop-erp-web-1-0-0.container")
	assert.Contains(t, web, "Wants=brickkit-shop-erp-api-1-0-0@1.service\n")
	assert.Contains(t, web, "After=brickkit-shop-erp-api-1-0-0@2.service\n")
}

// 外壳：挂上成员的服务名作别名；成员的迁移用成员自己的镜像单独跑，外壳 Requires= 它。
func TestShellHostsItsMembers(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell", "1.0.0", 9000), projecttest.Entry{})
	b.component(withMigration(simple("erp/api", "1.0.0", 8080)), projecttest.Entry{ServedBy: "infra/shell@1.0.0"})
	r := b.generate()

	assert.NotContains(t, names(r), "brickkit-shop-erp-api-1-0-0.container", "成员没有自己的容器")
	assert.Equal(t, []string{"brickkit-shop-erp-api-1-0-0-migration.service"}, r.Migrations)
	shell := file(t, r, "brickkit-shop-infra-shell-1-0-0.container")
	assert.Contains(t, shell, "NetworkAlias=infra-shell-1-0-0\nNetworkAlias=erp-api-1-0-0\n")
	assert.Contains(t, shell, "Requires=brickkit-shop-erp-api-1-0-0-migration.service\n")
	assert.Contains(t, file(t, r, "brickkit-shop-erp-api-1-0-0-migration.container"), "Image=registry.example.com/erp-api:1.0.0\n")
}

// 普通字面量写进单元：$ 与 % 双写（Quadlet 把值放进 ExecStart，systemd 在那里展开它们——实测），
// 引号与反斜杠按 C 的写法转义。
func TestInlineValuesAreEscapedForSystemd(t *testing.T) {
	b := newBuilder(t)
	b.component(withSchema(simple("erp/api", "1.0.0", 8080), map[string]manifest.ConfigProperty{
		"GREETING": {Type: "string"},
	}), projecttest.Entry{Config: map[string]any{"GREETING": `say "hi" to 50% of $HOME at c:\dir`}})
	r := b.generate()

	assert.Contains(t, file(t, r, "brickkit-shop-erp-api-1-0-0.container"),
		`Environment="GREETING=say \"hi\" to 50%% of $$HOME at c:\\dir"`+"\n")
	assert.Empty(t, r.Secrets)
}

// 密钥、file://、${VAR} 模板、带换行的字面量：在生成时求值，进 Podman secret，单元里只有引用；
// 主容器与迁移挂同一个 secret。
func TestSecretsGoToPodmanSecrets(t *testing.T) {
	b := newBuilder(t)
	b.spec.Files["keys/ca.pem"] = "-----BEGIN-----\nabc\n-----END-----\n"
	b.component(withMigration(withSchema(simple("erp/api", "1.0.0", 8080), map[string]manifest.ConfigProperty{
		"DB_PASSWORD": {Type: "string", Secret: true},
		"CA":          {Type: "string"},
		"DSN":         {Type: "string"},
		"BANNER":      {Type: "string"},
	})), projecttest.Entry{Config: map[string]any{
		"DB_PASSWORD": "pa$$word",
		"CA":          "file://keys/ca.pem",
		"DSN":         "pg://u:${DB_PASS}@h/$x",
		"BANNER":      "line one\nline two",
	}})
	b.env = map[string]string{"DB_PASS": "p"}
	r := b.generate()

	assert.Equal(t, map[string]string{
		"brickkit-shop-erp-api-1-0-0-DB_PASSWORD": "pa$$word",
		"brickkit-shop-erp-api-1-0-0-CA":          "-----BEGIN-----\nabc\n-----END-----\n",
		"brickkit-shop-erp-api-1-0-0-DSN":         "pg://u:p@h/$x",
		"brickkit-shop-erp-api-1-0-0-BANNER":      "line one\nline two",
	}, secrets(r))
	for _, name := range []string{"brickkit-shop-erp-api-1-0-0.container", "brickkit-shop-erp-api-1-0-0-migration.container"} {
		unit := file(t, r, name)
		assert.Contains(t, unit, "Secret=brickkit-shop-erp-api-1-0-0-DB_PASSWORD,type=env,target=DB_PASSWORD\n", name)
		assert.Contains(t, unit, "Secret=brickkit-shop-erp-api-1-0-0-BANNER,type=env,target=BANNER\n", name)
		assert.NotContains(t, unit, "pa$", name)
		assert.NotContains(t, unit, "line one", name)
	}
}

// ${VAR} 展不开：生成时就报，与 K8s 和 compose 同一个错误。
func TestUndefinedVariableFails(t *testing.T) {
	b := newBuilder(t)
	b.component(withSchema(simple("erp/api", "1.0.0", 8080), map[string]manifest.ConfigProperty{
		"DSN": {Type: "string"},
	}), projecttest.Entry{Config: map[string]any{"DSN": "pg://${NOPE}@h"}})
	_, err := b.build()
	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)
	assert.Contains(t, err.Error(), "NOPE")
}

// 部署文件写了 network:：加入那个网络，不建项目网络，down 也不删它。
func TestProjectProvidedNetwork(t *testing.T) {
	b := newBuilder(t)
	b.spec.Network = "corp-net"
	r := b.component(simple("erp/api", "1.0.0", 8080), projecttest.Entry{}).generate()

	assert.Equal(t, []string{"brickkit-shop-erp-api-1-0-0.container"}, names(r))
	assert.Empty(t, r.Network)
	assert.Contains(t, file(t, r, "brickkit-shop-erp-api-1-0-0.container"), "Network=corp-net\n")
}

// expose 发布到宿主机；配额写成 podman 参数；labels 透传；停机宽限期；host.docker.internal 解析到宿主机。
func TestPortsQuotaLabelsAndHosts(t *testing.T) {
	b := newBuilder(t)
	b.component(withSchema(simple("erp/api", "1.0.0", 8080), map[string]manifest.ConfigProperty{
		"DB_HOST": {Type: "string"},
	}), projecttest.Entry{
		Expose: true, ExposePort: 18443,
		Resources: &manifest.Resources{
			Limits:   &manifest.ResourceSpec{CPU: "500m", Memory: "256Mi"},
			Requests: &manifest.ResourceSpec{CPU: "100m", Memory: "128Mi"},
		},
		Labels:                 map[string]string{"traefik.enable": "true"},
		StopGracePeriodSeconds: 25,
		Config:                 map[string]any{"DB_HOST": "host.docker.internal"},
	})
	unit := file(t, b.generate(), "brickkit-shop-erp-api-1-0-0.container")

	for _, line := range []string{
		"PublishPort=18443:8080",
		"PodmanArgs=--cpus=0.50 --memory=256M --memory-reservation=128M",
		`Label="traefik.enable=true"`,
		"StopTimeout=25",
		"AddHost=host.docker.internal:host-gateway",
	} {
		assert.Contains(t, unit, line+"\n")
	}
}

// mount: file：值写成项目里的文件（与 compose 同一处），单元把那个目录只读挂进容器，变量里是容器里的路径。
func TestMountFileMountsTheProjectDirectory(t *testing.T) {
	b := newBuilder(t)
	b.component(withSchema(simple("erp/api", "1.0.0", 8080), map[string]manifest.ConfigProperty{
		"KEY_FILE": {Type: "string", Secret: true, Mount: manifest.MountFile},
	}), projecttest.Entry{Config: map[string]any{"KEY_FILE": "k"}})
	r := b.generate()

	require.Len(t, r.SecretFiles, 1)
	unit := file(t, r, "brickkit-shop-erp-api-1-0-0.container")
	assert.Contains(t, unit, "Volume="+b.root+"/.brickkit/generated/secrets/erp-api-1-0-0:/run/brickkit/secrets/erp-api-1-0-0:ro\n")
	assert.Contains(t, unit, `Environment="KEY_FILE=/run/brickkit/secrets/erp-api-1-0-0/KEY_FILE"`+"\n")
}
