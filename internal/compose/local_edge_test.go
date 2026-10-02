// 本文件是本地调试生成的代码层测试：宿主机端口台账与引擎差异。
//
// 业务行为已由 local_test.go 从"生成出来的文件里有什么"这一侧盯住；
// 这里补的是那些从外面不容易逼出来的边界（端口越界、重复占用、端口耗尽）。
package compose

import (
	"github.com/brickkit/brickkit/internal/resolver"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/inject"
)

func TestPortTableClaimRejectsDuplicate(t *testing.T) {
	table := newPortTable()
	require.NoError(t, table.claim(8081, "组件 A"))

	err := table.claim(8081, "组件 B")

	require.Error(t, err)
	assert.Equal(t, clierr.CodePortConflict, clierr.As(err).Code)
	assert.Contains(t, err.Error(), "组件 A")
	assert.Contains(t, err.Error(), "组件 B")
}

// 同一个占用方重复登记不算冲突：一个组件既 expose 又被 local 组件依赖时，
// 两条路径会各登记一次同一个端口。
func TestPortTableClaimIsIdempotentForSameOwner(t *testing.T) {
	table := newPortTable()
	require.NoError(t, table.claim(8081, "组件 A"))

	assert.NoError(t, table.claim(8081, "组件 A"))
}

func TestPortTableAllocatePrefersOffsetPort(t *testing.T) {
	table := newPortTable()

	assert.Equal(t, 18080, table.allocate(hostPortOffset+8080, hostPortBase, "组件 A"))
	assert.Equal(t, 15432, table.allocate(hostPortOffset+5432, hostPortBase, "资源 pg"))
	assert.Equal(t, 19090, table.allocate(hostPortOffset+9090, hostPortBase, "组件 A grpc"))
}

// 首选端口被占了就从 base 起顺序扫描，绝不返回一个已被占用的端口。
func TestPortTableAllocateFallsBackToBase(t *testing.T) {
	table := newPortTable()
	require.NoError(t, table.claim(18080, "别人"))

	assert.Equal(t, 18081, table.allocate(hostPortOffset+8080, hostPortBase, "组件 A"))
}

// 10000 + 端口可能超出端口范围（如 60000 → 70000），这时只能走 base。
func TestPortTableAllocateIgnoresOutOfRangePreference(t *testing.T) {
	table := newPortTable()

	assert.Equal(t, hostPortBase, table.allocate(hostPortOffset+60000, hostPortBase, "组件 A"))
}

func TestPortTableAllocateSkipsTakenPortsInSequence(t *testing.T) {
	table := newPortTable()
	for port := 8081; port <= 8083; port++ {
		require.NoError(t, table.claim(port, "别人"))
	}

	assert.Equal(t, 8084, table.allocate(0, localPortBase, "组件 A"))
}

// Docker 与 Podman 用同一个魔法值（在真 Podman 上实测过，见 local_test.go 的说明）。
func TestHostGateway(t *testing.T) {
	assert.Equal(t, "host-gateway", hostGateway(EngineDocker))
	assert.Equal(t, "host-gateway", hostGateway(""), "没指定引擎时按 Docker 处理")
}

// setAddress 只改已有的、指向这个目标这个端口的变量：不存在意味着注入引擎判定"这条不该注入"
// （比如弱依赖没启动），本地调试没有理由把它凭空补回来。按 Target 认变量，不按名字；$endpoint: 写下的路径接在后面。
func TestSetAddressRewritesByTargetAndKeepsPath(t *testing.T) {
	api := resolver.Ref{ID: "erp/api", Version: "1.0.0"}
	other := resolver.Ref{ID: "erp/other", Version: "1.0.0"}
	vars := []inject.Var{
		{Name: "ERP_API_ENDPOINT", Value: inject.Literal("x"), Target: api},
		{Name: "API_JWKS_URL", Value: inject.Literal("x"), Target: api, Path: "/.well-known/jwks.json"},
		{Name: "ERP_API_GRPC_ENDPOINT", Value: inject.Literal("x"), Target: api, Port: "grpc"},
		{Name: "OTHER", Value: inject.Literal("keep"), Target: other},
	}

	setAddress(vars, api, "", "http://localhost:18080")

	require.Len(t, vars, 4)
	assert.Equal(t, "http://localhost:18080", vars[0].Value.Text)
	assert.Equal(t, "http://localhost:18080/.well-known/jwks.json", vars[1].Value.Text)
	assert.Equal(t, "x", vars[2].Value.Text, "别的端口不动")
	assert.Equal(t, "keep", vars[3].Value.Text)
}
