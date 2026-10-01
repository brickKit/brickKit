// 本文件是 Scaffold 的防腐测试：生成的 component.yaml 必须真的能通过
// Parse + Validate，而不是"看起来像"合法的 YAML。Manifest 结构一变，
// 这里就该跟着红——不然 brickkit new 生成的骨架第一条命令就报错，
// 比手写错了更打脸：这本该是平台自己保证过的东西。
package manifest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/manifest"
)

func TestScaffoldProducesValidManifest(t *testing.T) {
	files, err := manifest.Scaffold("demo/widget", manifest.ScaffoldOptions{})
	require.NoError(t, err)
	require.Len(t, files, 5, "不带 --contract 时生成 component.yaml 与四份文档")
	assert.Equal(t, manifest.FileName, files[0].Path)
	assert.Equal(t, "BRICKKIT.md", files[1].Path)

	m, err := manifest.Parse(files[0].Content, "demo/widget/component.yaml")
	require.NoError(t, err, "生成的骨架必须是合法 YAML 且能解析")
	require.NoError(t, m.Validate(), "生成的骨架必须通过 Validate")

	assert.Equal(t, "demo/widget", m.Metadata.ID)
	assert.Equal(t, "0.1.0", m.Metadata.Version)
	assert.True(t, manifest.IsExactVersion(m.Metadata.Version))
	assert.Equal(t, manifest.DeploymentTypeContainer, m.Deployment.Type)
	assert.Empty(t, m.Deployment.Image, "新组件还没有预构建镜像")
	require.NotNil(t, m.Deployment.Build)
	assert.Equal(t, "Dockerfile", m.Deployment.Build.Dockerfile)
	assert.Equal(t, "demo-widget:0.1.0", manifest.ImageRef(m))
	assert.Equal(t, 8080, m.Deployment.Port)
	assert.Equal(t, manifest.HealthCheckHTTP, m.HealthCheck.Type)
	assert.Equal(t, "/healthz", m.HealthCheck.Path)
	assert.Empty(t, m.Artifacts, "不带 --contract 就不该有 artifacts 段")
}

func TestScaffoldRejectsBadID(t *testing.T) {
	_, err := manifest.Scaffold("NotValid", manifest.ScaffoldOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid component ID")
}

func TestScaffoldWithOpenAPIContract(t *testing.T) {
	files, err := manifest.Scaffold("demo/widget", manifest.ScaffoldOptions{Contract: manifest.ContractOpenAPI})
	require.NoError(t, err)
	require.Len(t, files, 6, "带 --contract 时还要生成一份契约占位文件")

	m, err := manifest.Parse(files[0].Content, "demo/widget/component.yaml")
	require.NoError(t, err)
	require.NoError(t, m.Validate())

	require.Len(t, m.Artifacts, 1)
	a := m.Artifacts[0]
	assert.Equal(t, "api-contract", a.Type)
	assert.Equal(t, "openapi", a.Format)
	require.Equal(t, []string{"api/openapi.yaml"}, a.Files)

	assert.Equal(t, "api/openapi.yaml", files[5].Path)
	assert.Contains(t, string(files[5].Content), "openapi: 3.0.3")
}

func TestScaffoldWithProtoContract(t *testing.T) {
	files, err := manifest.Scaffold("demo/widget", manifest.ScaffoldOptions{Contract: manifest.ContractProto})
	require.NoError(t, err)
	require.Len(t, files, 6)

	m, err := manifest.Parse(files[0].Content, "demo/widget/component.yaml")
	require.NoError(t, err)
	require.NoError(t, m.Validate())

	require.Len(t, m.Artifacts, 1)
	assert.Equal(t, "proto", m.Artifacts[0].Format)
	require.Equal(t, []string{"api/service.proto"}, m.Artifacts[0].Files)

	assert.Equal(t, "api/service.proto", files[5].Path)
	assert.Contains(t, string(files[5].Content), `syntax = "proto3";`)
}

func TestScaffoldRejectsUnknownContract(t *testing.T) {
	_, err := manifest.Scaffold("demo/widget", manifest.ScaffoldOptions{Contract: "grpc-web"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --contract value")
}

// 外壳骨架：出现 shell.members 即为外壳；
// 校验不接受空的成员列表，而骨架必须能通过校验，所以放一个占位成员。
func TestScaffoldShellPassesValidation(t *testing.T) {
	files, err := manifest.Scaffold("erp/shell", manifest.ScaffoldOptions{Shell: true})
	require.NoError(t, err)
	m, err := manifest.Parse(files[0].Content, "erp/shell/component.yaml")
	require.NoError(t, err)
	require.NoError(t, m.Validate())
	assert.True(t, m.IsShell())
	assert.Equal(t, []string{"example/member@0.1.0"}, m.Shell.Members)
	assert.Contains(t, string(files[0].Content), "TODO")
	assert.NotContains(t, string(files[0].Content), "kind: shell", "kind: shell 写在 brickkit.yaml，不在 component.yaml")
}

// 组件级 BRICKKIT.md 骨架：五节标准结构；外壳声明一节只有外壳才填内容。
func TestScaffoldWritesComponentDoc(t *testing.T) {
	doc := func(opts manifest.ScaffoldOptions) string {
		files, err := manifest.Scaffold("erp/backend", opts)
		require.NoError(t, err)
		for _, f := range files {
			if f.Path == "BRICKKIT.md" {
				return string(f.Content)
			}
		}
		t.Fatal("没有生成 BRICKKIT.md")
		return ""
	}
	plain := doc(manifest.ScaffoldOptions{})
	assert.True(t, strings.HasPrefix(plain, "# erp/backend\n"))
	for _, heading := range []string{"## Purpose", "## Dependencies", "## Configuration", "## Contracts", "## Shell declaration"} {
		assert.Contains(t, plain, heading)
	}
	assert.Contains(t, plain, "Not a shell.")

	shell := doc(manifest.ScaffoldOptions{Shell: true})
	assert.Contains(t, shell, "example/member@0.1.0")
	assert.NotContains(t, shell, "Not a shell.")

	contract := doc(manifest.ScaffoldOptions{Contract: manifest.ContractOpenAPI})
	assert.Contains(t, contract, "`api/openapi.yaml`")
}
