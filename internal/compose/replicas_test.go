// 本文件测 Docker / Podman 下的 replicas：渲染成 compose 的 deploy.replicas，迁移照旧只跑一次，
// 多个副本不能映射宿主机端口（一个宿主机端口只能给一个容器）。
package compose_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/project/projecttest"
)

const (
	exposeHint      = "drop expose"
	hostCallersHint = "A process on this machine"
)

func deployOfService(t *testing.T, svc map[string]any) map[string]any {
	t.Helper()
	deploy, _ := svc["deploy"].(map[string]any)
	return deploy
}

func TestReplicasRenderAsComposeDeployReplicas(t *testing.T) {
	b := newBuilder(t)
	b.component(withMigration(simple("people/basic", "1.0.0", 8080)), projecttest.Entry{Replicas: projecttest.IntPtr(3)})
	b.component(simple("department/tree", "1.0.0", 8080), projecttest.Entry{Replicas: projecttest.IntPtr(1)})
	doc := b.parsed()

	assert.EqualValues(t, 3, deployOfService(t, serviceOf(t, doc, "people-basic-1-0-0"))["replicas"])
	assert.NotContains(t, deployOfService(t, serviceOf(t, doc, "department-tree-1-0-0")), "replicas",
		"replicas: 1 是 compose 的缺省，不必写")
	assert.NotContains(t, deployOfService(t, serviceOf(t, doc, "people-basic-1-0-0-migration")), "replicas",
		"迁移是一次性的，不论主服务有几个副本都只跑一次")
}

// 多个副本、被别的容器调用：不经宿主机端口，服务名在网络里解析到每个副本——这正是它的用处。
func TestReplicasCalledOnlyByContainersAreAccepted(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"), projecttest.Entry{})
	b.component(simple("people/basic", "1.0.0", 8080), projecttest.Entry{Replicas: projecttest.IntPtr(2)})

	svc := serviceOf(t, b.parsed(), "people-basic-1-0-0")
	assert.NotContains(t, svc, "ports")
	assert.EqualValues(t, 2, deployOfService(t, svc)["replicas"])
}

func TestReplicasWithExposeAreRefused(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("portal/web", "1.0.0", 80), projecttest.Entry{Expose: true, Replicas: projecttest.IntPtr(2)})

	_, err := b.build(compose.Options{})

	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodePortConflict, e.Code)
	text := e.Format()
	assert.Contains(t, text, "portal/web@1.0.0")
	assert.Contains(t, text, "80:80")
	assert.Contains(t, text, exposeHint)
	assert.NotContains(t, text, hostCallersHint, "没有本机进程调用它，这条出路说不通")
}

// 本机进程（mode: debug / local）经宿主机端口连它的依赖：依赖有多个副本时那个端口没法映射。
func TestReplicasOfADependencyOfAProcessOnThisMachineAreRefused(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("people/basic", "1.0.0", 8080), "department/tree", "1.0.0"),
		projecttest.Entry{Mode: deployfile.ModeDebug})
	b.component(simple("department/tree", "1.0.0", 8080), projecttest.Entry{Replicas: projecttest.IntPtr(2)})

	_, err := b.build(compose.Options{})

	require.Error(t, err)
	text := clierr.As(err).Format()
	assert.Contains(t, text, "department/tree@1.0.0")
	assert.Contains(t, text, hostCallersHint)
	assert.NotContains(t, text, exposeHint, "它没有 expose，这条出路说不通")
}

// 外壳替成员开放端口：映射开在外壳的容器上，外壳有多个副本时同样映射不了。
func TestShellReplicasWithAnExposedMemberAreRefused(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("erp/shell", "1.0.0", 8080), projecttest.Entry{Replicas: projecttest.IntPtr(2)})
	bff := servedByEntry("erp/shell", "1.0.0")
	bff.Expose = true
	b.component(simple("erp/bff", "1.0.0", 8085), bff)

	_, err := b.build(compose.Options{})

	require.Error(t, err)
	text := clierr.As(err).Format()
	assert.Contains(t, text, "erp/shell@1.0.0")
	assert.Contains(t, text, "8085:8085")
	assert.Contains(t, text, exposeHint)
}
