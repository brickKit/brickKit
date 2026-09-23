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
