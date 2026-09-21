// 本文件是 Scaffold 的防腐测试：生成的 component.yaml 必须真的能通过
// Parse + Validate，而不是"看起来像"合法的 YAML。Manifest 结构一变，
// 这里就该跟着红——不然 brickkit new 生成的骨架第一条命令就报错，
// 比手写错了更打脸：这本该是平台自己保证过的东西。
package manifest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/manifest"
)

func TestScaffoldProducesValidManifest(t *testing.T) {
	files, err := manifest.Scaffold("demo/widget", manifest.ScaffoldOptions{})
	require.NoError(t, err)
	require.Len(t, files, 1, "不带 --contract 时只生成 component.yaml")
	assert.Equal(t, manifest.FileName, files[0].Path)

	m, err := manifest.Parse(files[0].Content, "demo/widget/component.yaml")
	require.NoError(t, err, "生成的骨架必须是合法 YAML 且能解析")
	require.NoError(t, m.Validate(), "生成的骨架必须通过 Validate")

	assert.Equal(t, "demo/widget", m.Metadata.ID)
	assert.Equal(t, "0.1.0", m.Metadata.Version)
	assert.True(t, manifest.IsExactVersion(m.Metadata.Version))
	assert.Equal(t, manifest.DeploymentTypeContainer, m.Deployment.Type)
	assert.Equal(t, "demo/widget:0.1.0", m.Deployment.Image)
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
	require.Len(t, files, 2, "带 --contract 时还要生成一份契约占位文件")

	m, err := manifest.Parse(files[0].Content, "demo/widget/component.yaml")
	require.NoError(t, err)
	require.NoError(t, m.Validate())

	require.Len(t, m.Artifacts, 1)
	a := m.Artifacts[0]
	assert.Equal(t, "api-contract", a.Type)
	assert.Equal(t, "openapi", a.Format)
	require.Equal(t, []string{"api/openapi.yaml"}, a.Files)

	assert.Equal(t, "api/openapi.yaml", files[1].Path)
	assert.Contains(t, string(files[1].Content), "openapi: 3.0.3")
}

func TestScaffoldWithProtoContract(t *testing.T) {
	files, err := manifest.Scaffold("demo/widget", manifest.ScaffoldOptions{Contract: manifest.ContractProto})
	require.NoError(t, err)
	require.Len(t, files, 2)

	m, err := manifest.Parse(files[0].Content, "demo/widget/component.yaml")
	require.NoError(t, err)
	require.NoError(t, m.Validate())

	require.Len(t, m.Artifacts, 1)
	assert.Equal(t, "proto", m.Artifacts[0].Format)
	require.Equal(t, []string{"api/service.proto"}, m.Artifacts[0].Files)

	assert.Equal(t, "api/service.proto", files[1].Path)
	assert.Contains(t, string(files[1].Content), `syntax = "proto3";`)
}

func TestScaffoldRejectsUnknownContract(t *testing.T) {
	_, err := manifest.Scaffold("demo/widget", manifest.ScaffoldOptions{Contract: "grpc-web"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --contract value")
}
