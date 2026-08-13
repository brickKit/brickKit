package resolver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/source"
)

// ============================================================
// 组件构造
// ============================================================

// comp 描述一个用于测试的组件。
type comp struct {
	ID      string
	Version string
	// Requires 是强依赖，写法为 "department/tree@1.0.0"。
	Requires []string
	// Optional 是弱依赖，写法同上。
	Optional []string
	// Resources 是资源依赖，写法为 "database:postgresql"。
	Resources []string
	// NoDependencies 为 true 时写出 `dependencies: {components: []}` 空列表。
	EmptyDependencies bool
}

func (c comp) ref() Ref { return Ref{ID: c.ID, Version: c.Version} }

// yamlText 渲染出一份合法的 component.yaml（002 §2.2）。
func (c comp) yamlText() string {
	var b strings.Builder
	b.WriteString("apiVersion: brickkit/v1\nkind: Component\nmetadata:\n")
	fmt.Fprintf(&b, "  id: %s\n", c.ID)
	fmt.Fprintf(&b, "  name: 测试组件 %s\n", c.ID)
	fmt.Fprintf(&b, "  version: %s\n", c.Version)
	fmt.Fprintf(&b, "  description: 用于依赖解析测试的组件 %s@%s\n", c.ID, c.Version)

	switch {
	case len(c.Requires) > 0 || len(c.Optional) > 0 || len(c.Resources) > 0:
		b.WriteString("dependencies:\n")
		if len(c.Requires) > 0 || len(c.Optional) > 0 {
			b.WriteString("  components:\n")
			for _, r := range c.Requires {
				fmt.Fprintf(&b, "    - %s\n", r)
			}
			for _, o := range c.Optional {
				fmt.Fprintf(&b, "    - id: %s\n      optional: true\n", o)
			}
		}
		if len(c.Resources) > 0 {
			b.WriteString("  resources:\n")
			for _, r := range c.Resources {
				kind, engine, _ := strings.Cut(r, ":")
				fmt.Fprintf(&b, "    - kind: %s\n      engine: %s\n", kind, engine)
			}
		}
	case c.EmptyDependencies:
		b.WriteString("dependencies:\n  components: []\n")
	}

	b.WriteString("deployment:\n  type: container\n")
	fmt.Fprintf(&b, "  image: registry.example.com/%s:%s\n", strings.ReplaceAll(c.ID, "/", "-"), c.Version)
	b.WriteString("  port: 8080\n")
	b.WriteString("healthCheck:\n  type: http\n  path: /healthz\n")
	return b.String()
}

// ============================================================
// 被测环境：真实的 internal/source 客户端 + 本地安装源
// ============================================================

// fixture 是一次测试用的解析环境。
type fixture struct {
	Resolver *Resolver
	Provider *countingProvider
	Layout   config.Layout
	Config   *config.Config
}

// newFixture 把每个组件写进**独立的本地安装源目录**，再用真实的 source.Client 提供 Manifest。
//
// 一个目录只能放一个版本（local 源按 <scope>/<name> 定位），因此多版本共存的用例
// 需要每个版本一个目录；安装源链会依次尝试，版本不匹配的源自动跳过。
func newFixture(t *testing.T, comps ...comp) *fixture {
	t.Helper()

	layout := config.NewLayout(t.TempDir(), "")
	cfg := &config.Config{
		Project: "test-project",
		Deploy:  config.Deploy{Target: config.TargetDocker},
	}
	// 始终配置一个空的本地安装源：即使一个组件都没写，也要区分
	// "安装源里没有这个组件"与"根本没配安装源"两种情形。
	require.NoError(t, os.MkdirAll(filepath.Join(layout.Root, "src"), 0o755))
	cfg.Sources = append(cfg.Sources, config.Source{
		ID: "local-empty", Type: config.SourceTypeLocal, Path: "./src",
	})

	for i, c := range comps {
		dir := filepath.Join(layout.Root, "src"+strconv.Itoa(i))
		path := filepath.Join(dir, filepath.FromSlash(c.ID), manifest.FileName)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(c.yamlText()), 0o644))
		cfg.Sources = append(cfg.Sources, config.Source{
			ID:   "local-" + strconv.Itoa(i),
			Type: config.SourceTypeLocal,
			Path: "./src" + strconv.Itoa(i),
		})
	}

	client, err := source.New(layout, cfg, source.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	provider := &countingProvider{inner: FromSource(client)}
	return &fixture{
		Resolver: New(provider),
		Provider: provider,
		Layout:   layout,
		Config:   cfg,
	}
}

// countingProvider 记录每个组件版本被真正获取了几次（用于验证去重）。
type countingProvider struct {
	inner Provider

	mu    sync.Mutex
	calls map[string]int
}

func (p *countingProvider) Manifest(ctx context.Context, id, version string) (*manifest.Manifest, error) {
	p.mu.Lock()
	if p.calls == nil {
		p.calls = map[string]int{}
	}
	p.calls[id+"@"+version]++
	p.mu.Unlock()
	return p.inner.Manifest(ctx, id, version)
}

func (p *countingProvider) count(ref string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[ref]
}

// ============================================================
// fakeProvider：用于构造解析器必须防住、但 Manifest 校验器不允许写出来的图
// （如自依赖——component.yaml 里写自依赖会被 002 校验直接拒掉）
// ============================================================

type fakeProvider struct {
	manifests map[string]*manifest.Manifest
	errs      map[string]error
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{manifests: map[string]*manifest.Manifest{}, errs: map[string]error{}}
}

// add 直接构造 Manifest 结构体，绕过 component.yaml 的校验。
func (p *fakeProvider) add(id, version string, deps ...manifest.ComponentDep) *fakeProvider {
	m := &manifest.Manifest{
		APIVersion:  manifest.APIVersion,
		Kind:        manifest.Kind,
		Metadata:    manifest.Metadata{ID: id, Name: id, Version: version, Description: id},
		Deployment:  manifest.Deployment{Type: manifest.DeploymentTypeContainer, Image: "img", Port: 8080},
		HealthCheck: manifest.HealthCheck{Type: manifest.HealthCheckHTTP, Path: "/healthz"},
	}
	if len(deps) > 0 {
		m.Dependencies = &manifest.Dependencies{Components: deps}
	}
	p.manifests[id+"@"+version] = m
	return p
}

func (p *fakeProvider) Manifest(_ context.Context, id, version string) (*manifest.Manifest, error) {
	key := id + "@" + version
	if err, ok := p.errs[key]; ok {
		return nil, err
	}
	if m, ok := p.manifests[key]; ok {
		return m, nil
	}
	return nil, notFoundErr(key)
}

func dep(ref string) manifest.ComponentDep {
	id, version, _ := strings.Cut(ref, "@")
	return manifest.ComponentDep{ID: id, Version: version, Ref: ref}
}

func optDep(ref string) manifest.ComponentDep {
	d := dep(ref)
	d.Optional = true
	return d
}

// notFoundErr 模拟安装源的"组件未找到"（与 internal/source 的错误形状一致）。
func notFoundErr(ref string) error {
	return clierr.New(clierr.CodeComponentNotFound, "错误：组件未找到").
		WithDetail("组件", ref).
		WithDetail("原因", "该组件在所有安装源中均未找到")
}
