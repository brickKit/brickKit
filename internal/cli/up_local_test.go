package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/workspace"
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

// 不借 tests/components/demo-hello（那是给仓库自测容器化部署用的完整固件，
// 依赖多、体积大）——本地探测测试只需要一个能被 internal/runcmd 认出来的
// 最小 Go 目录，照 Plan 3 TestADetectedGoCommandReallyRuns 的路子，直接用
// writeTree 把 go.mod/main.go 写进 workspace.SourceDir。
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

// ============================================================
// Plan 4b：本地进程环境变量的严格展开
// ============================================================

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
