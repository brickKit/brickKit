package override

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOverrideMissingFileReturnsNilNoError(t *testing.T) {
	o, err := ParseOverrideFile("/nonexistent/override.yaml")
	require.NoError(t, err)
	assert.Nil(t, o)
}

func TestParseOverrideEmptyContentIsValidNoOverrides(t *testing.T) {
	o, err := ParseOverride([]byte(""), "override.yaml")
	require.NoError(t, err)
	require.NotNil(t, o)
	assert.Empty(t, o.Target)
	assert.Empty(t, o.Components)
}

func TestParseOverrideFullShapeRoundTrips(t *testing.T) {
	o, err := ParseOverride([]byte(`
target: podman
targetBaseline: docker
components:
  - id: department/tree
  - id: erp/backend
    members:
      - id: people/basic
      - id: auth/rbac
        mode: disable
  - id: infra/redis-event-bus
    mode: local
    localPort: 8082
  - id: payment/gateway
    mode: debug
    localPort: 9091
    baseline: local
`), "override.yaml")
	require.NoError(t, err)
	require.NotNil(t, o)

	assert.Equal(t, "podman", o.Target)
	assert.Equal(t, "docker", o.TargetBaseline)
	require.Len(t, o.Components, 4)

	assert.Equal(t, "department/tree", o.Components[0].ID)
	assert.Empty(t, o.Components[0].Mode)

	shell := o.Components[1]
	assert.Equal(t, "erp/backend", shell.ID)
	require.Len(t, shell.Members, 2)
	assert.Equal(t, "people/basic", shell.Members[0].ID)
	assert.Equal(t, "auth/rbac", shell.Members[1].ID)
	assert.Equal(t, "disable", shell.Members[1].Mode)

	redis := o.Components[2]
	assert.Equal(t, "local", redis.Mode)
	assert.Equal(t, 8082, redis.LocalPort)

	payment := o.Components[3]
	assert.Equal(t, "debug", payment.Mode)
	assert.Equal(t, 9091, payment.LocalPort)
	assert.Equal(t, "local", payment.Baseline)
}

func TestParseOverrideRejectsUnknownField(t *testing.T) {
	_, err := ParseOverride([]byte(`
components:
  - id: demo/hello
    version: 1.0.0
`), "override.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version")
}

func TestParseOverrideRejectsComponentsNotArray(t *testing.T) {
	_, err := ParseOverride([]byte(`
components: demo/hello
`), "override.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "components")
}

func TestParseOverrideRejectsInvalidTarget(t *testing.T) {
	_, err := ParseOverride([]byte(`
target: swarm
`), "override.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target")
}

func TestParseOverrideAcceptsPodmanTarget(t *testing.T) {
	o, err := ParseOverride([]byte(`
target: podman
`), "override.yaml")
	require.NoError(t, err)
	assert.Equal(t, "podman", o.Target)
}

func TestParseOverrideRejectsMissingComponentID(t *testing.T) {
	_, err := ParseOverride([]byte(`
components:
  - mode: debug
`), "override.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "components[0].id")
}

func TestParseOverrideRejectsDuplicateComponentID(t *testing.T) {
	_, err := ParseOverride([]byte(`
components:
  - id: demo/hello
  - id: demo/hello
`), "override.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "demo/hello")
}

func TestParseOverrideRejectsDuplicateIDAcrossNestedMembers(t *testing.T) {
	_, err := ParseOverride([]byte(`
components:
  - id: erp/backend
    members:
      - id: people/basic
  - id: people/basic
`), "override.yaml")
	require.Error(t, err)
}

func TestParseOverrideRejectsInvalidMode(t *testing.T) {
	_, err := ParseOverride([]byte(`
components:
  - id: demo/hello
    mode: bogus
`), "override.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "components[0].mode")
}

// mode: debug 不写 localPort 是合法的——跟 mode: local 一样，没写就由平台自动分配
// 一个空闲端口（internal/compose 的 TestAutoAssignedLocalPortDefaultsToDeclaredPort
// 已经验过这条自动分配机制本身；这里验的是 override.yaml 这一层的解析/校验不会
// 对这个组合有意见）。validateEntry 只看 id/mode 两个字段，从不管 LocalPort，
// 所以这条本来就该过，只是原先没有一条测试把它钉住（config.edge_test.go 里
// mode: debug 从 brickkit.yaml 挪到 override.yaml 之后，TestDebugWithoutPortIsValid
// 被删了，却没有人在 override 这一层补回等价覆盖）。
func TestParseOverrideAcceptsDebugModeWithoutLocalPort(t *testing.T) {
	o, err := ParseOverride([]byte(`
components:
  - id: demo/hello
    mode: debug
`), "override.yaml")
	require.NoError(t, err)
	require.Len(t, o.Components, 1)
	assert.Equal(t, "debug", o.Components[0].Mode)
	assert.Zero(t, o.Components[0].LocalPort)
}
