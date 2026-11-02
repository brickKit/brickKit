package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// quadletRecorder 是 Quadlet 引擎的替身执行器：记下每条命令与它的标准输入，按子串给输出或失败。
type quadletRecorder struct {
	calls  []string
	stdin  map[string]string
	output map[string]string
	fail   map[string]bool
}

func newQuadletRecorder() *quadletRecorder {
	return &quadletRecorder{stdin: map[string]string{}, output: map[string]string{
		"version --format": "5.7.0\n",
	}, fail: map[string]bool{}}
}

func (r *quadletRecorder) run(_ context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, call)
	if stdin != nil {
		r.stdin[call] = string(stdin)
	}
	for pattern := range r.fail {
		if strings.Contains(call, pattern) {
			return []byte(r.output[pattern]), errors.New("exit status 1")
		}
	}
	for pattern, out := range r.output {
		if strings.Contains(call, pattern) {
			return []byte(out), nil
		}
	}
	return nil, nil
}

// called 报告有没有一条命令包含 want。
func (r *quadletRecorder) called(want string) bool {
	for _, c := range r.calls {
		if strings.Contains(c, want) {
			return true
		}
	}
	return false
}

func quadletWith(t *testing.T, rec *quadletRecorder, rootful bool) *Quadlet {
	return &Quadlet{rootful: rootful, root: t.TempDir(), runner: rec.run}
}

func units() *QuadletUnits {
	return &QuadletUnits{
		Files: []UnitFile{
			{Name: "brickkit-shop.network", Content: []byte("[Network]\nNetworkName=brickkit-shop-net\n")},
			{Name: "brickkit-shop-api-1-0-0-migration.container", Content: []byte("migration v1")},
			{Name: "brickkit-shop-api-1-0-0.container", Content: []byte("api v1")},
			{Name: "brickkit-shop-web-1-0-0@.container", Content: []byte("web v1")},
			{Name: "brickkit-shop-web-1-0-0@1.container", LinkTo: "brickkit-shop-web-1-0-0@.container"},
			{Name: "brickkit-shop-web-1-0-0@2.container", LinkTo: "brickkit-shop-web-1-0-0@.container"},
		},
		Secrets:    []Secret{{Name: "brickkit-shop-api-1-0-0-DB_PASSWORD", Value: []byte("pa$$\nword")}},
		Migrations: []string{"brickkit-shop-api-1-0-0-migration.service"},
		Components: []string{
			"brickkit-shop-api-1-0-0.service", "brickkit-shop-web-1-0-0@1.service", "brickkit-shop-web-1-0-0@2.service",
		},
	}
}

// 首次 up：secret 从标准输入建（值不落盘、不进命令行），单元写进项目的子目录（实例是指向模板的链接），
// daemon-reload，先单独跑迁移，再把（全是新的）组件重启、启动；最后删掉本项目不再用的 secret。用户的 systemd。
func TestQuadletFirstUp(t *testing.T) {
	rec := newQuadletRecorder()
	rec.output["secret ls"] = "brickkit-shop-api-1-0-0-DB_PASSWORD\nbrickkit-shop-api-1-0-0-OLD\nbrickkit-other-x-1-0-0-KEY\n"
	q := quadletWith(t, rec, false)

	require.NoError(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: units()}))

	assert.Equal(t, "pa$$\nword", rec.stdin["podman secret create --replace brickkit-shop-api-1-0-0-DB_PASSWORD -"])
	dir := q.Dir("brickkit-shop")
	content, err := os.ReadFile(filepath.Join(dir, "brickkit-shop-api-1-0-0.container"))
	require.NoError(t, err)
	assert.Equal(t, "api v1", string(content))
	target, err := os.Readlink(filepath.Join(dir, "brickkit-shop-web-1-0-0@2.container"))
	require.NoError(t, err)
	assert.Equal(t, "brickkit-shop-web-1-0-0@.container", target)

	var systemctl []string
	for _, c := range rec.calls {
		if strings.HasPrefix(c, "systemctl") {
			systemctl = append(systemctl, c)
		}
	}
	assert.Equal(t, []string{
		"systemctl --user daemon-reload",
		"systemctl --user start brickkit-shop-network.service brickkit-shop-api-1-0-0-migration.service",
		"systemctl --user restart --job-mode=ignore-requirements brickkit-shop-api-1-0-0.service brickkit-shop-web-1-0-0@1.service brickkit-shop-web-1-0-0@2.service",
		"systemctl --user start --job-mode=ignore-requirements brickkit-shop-api-1-0-0.service brickkit-shop-web-1-0-0@1.service brickkit-shop-web-1-0-0@2.service",
	}, systemctl)
	assert.True(t, rec.called("podman secret rm brickkit-shop-api-1-0-0-OLD"))
	assert.False(t, rec.called("brickkit-other-x"), "别的项目的 secret 不碰")
}

// 再 up 一次、内容没变：迁移照样再跑，组件只 start（在跑的不动），不 restart。
// 只改了模板：它的每个实例都重启，别的不动。
func TestQuadletRestartsOnlyWhatChanged(t *testing.T) {
	rec := newQuadletRecorder()
	q := quadletWith(t, rec, false)
	require.NoError(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: units()}))

	rec.calls = nil
	require.NoError(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: units()}))
	assert.True(t, rec.called("systemctl --user start brickkit-shop-network.service brickkit-shop-api-1-0-0-migration.service"))
	assert.False(t, rec.called("restart"))

	rec.calls = nil
	changed := units()
	changed.Files[3].Content = []byte("web v2")
	require.NoError(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: changed}))
	assert.True(t, rec.called("systemctl --user restart --job-mode=ignore-requirements brickkit-shop-web-1-0-0@1.service brickkit-shop-web-1-0-0@2.service"))
	assert.False(t, rec.called("restart brickkit-shop-api"))
}

// 不再生成的单元：先停再删文件（文件没了 systemd 就管不到它的容器），并报给调用方。
func TestQuadletStopsAndRemovesStaleUnits(t *testing.T) {
	rec := newQuadletRecorder()
	q := quadletWith(t, rec, false)
	require.NoError(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: units()}))

	rec.calls = nil
	fewer := units()
	fewer.Files = fewer.Files[:3]
	fewer.Components = fewer.Components[:1]
	var pruned []string
	require.NoError(t, q.Up(context.Background(), UpRequest{
		Project: "brickkit-shop", Quadlet: fewer, OnPrune: func(u string) { pruned = append(pruned, u) },
	}))
	assert.True(t, rec.called("systemctl --user stop brickkit-shop-web-1-0-0@1.service brickkit-shop-web-1-0-0@2.service"))
	assert.Equal(t, []string{"brickkit-shop-web-1-0-0@1.service", "brickkit-shop-web-1-0-0@2.service"}, pruned)
	_, err := os.Stat(filepath.Join(q.Dir("brickkit-shop"), "brickkit-shop-web-1-0-0@.container"))
	assert.True(t, os.IsNotExist(err))
}

// 迁移失败：报 MIGRATION_FAILED，点名失败的单元与看日志的命令；组件一个都不动（旧版本照跑）。
func TestQuadletMigrationFailureTouchesNoComponent(t *testing.T) {
	rec := newQuadletRecorder()
	rec.fail["start brickkit-shop-network.service brickkit-shop-api-1-0-0-migration.service"] = true
	rec.output["is-failed"] = "failed\n"
	q := quadletWith(t, rec, false)

	err := q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: units()})
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeMigrationFailed, e.Code)
	assert.Contains(t, err.Error(), "brickkit-shop-api-1-0-0-migration.service")
	assert.Contains(t, err.Error(), "journalctl --user -u brickkit-shop-api-1-0-0-migration.service")
	assert.False(t, rec.called("restart"))
	assert.False(t, rec.called("ignore-requirements"))
}

// 组件起不来：点名哪几个单元失败、日志怎么看。
func TestQuadletComponentFailureNamesTheUnits(t *testing.T) {
	rec := newQuadletRecorder()
	rec.fail["restart"] = true
	rec.output["is-failed"] = "active\nfailed\nactive\n"
	q := quadletWith(t, rec, true)

	err := q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: units()})
	require.Error(t, err)
	assert.Equal(t, clierr.CodeEngineFailed, clierr.As(err).Code)
	assert.Contains(t, err.Error(), "journalctl -u brickkit-shop-web-1-0-0@1.service", "有根：系统的日志，不带 --user")
	assert.NotContains(t, err.Error(), "brickkit-shop-api-1-0-0.service")
}

// Podman 太旧：报要几，什么都不装。
func TestQuadletRefusesOldPodman(t *testing.T) {
	rec := newQuadletRecorder()
	rec.output["version --format"] = "4.9.3\n"
	q := quadletWith(t, rec, false)

	err := q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: units()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "4.9.3")
	assert.Contains(t, err.Error(), QuadletMinVersion)
	_, statErr := os.Stat(q.Dir("brickkit-shop"))
	assert.True(t, os.IsNotExist(statErr))
	assert.False(t, rec.called("systemctl"))
}

func TestVersionAtLeast(t *testing.T) {
	for version, want := range map[string]bool{
		"5.3.0": true, "5.7.0": true, "6.0.1": true, "5.3.0-rc1": true, "5.2.4": false, "4.9.3": false, "5": false,
	} {
		assert.Equal(t, want, versionAtLeast(version, "5.3"), version)
	}
}

func TestUnitOf(t *testing.T) {
	assert.Equal(t, "x.service", UnitOf("x.container"))
	assert.Equal(t, "x@2.service", UnitOf("x@2.container"))
	assert.Equal(t, "", UnitOf("x@.container"))
	assert.Equal(t, "brickkit-shop-network.service", UnitOf("brickkit-shop.network"))
}

// down：停掉全部单元、卸下目录、daemon-reload、清掉失败记录，删网络（停网络单元不删它）与本项目的 secret。
// 没装过时什么都不做。
func TestQuadletDown(t *testing.T) {
	rec := newQuadletRecorder()
	rec.output["secret ls"] = "brickkit-shop-api-1-0-0-DB_PASSWORD\n"
	q := quadletWith(t, rec, false)
	require.NoError(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: units()}))

	rec.calls = nil
	require.NoError(t, q.Down(context.Background(), DownRequest{Project: "brickkit-shop"}))
	assert.True(t, rec.called("systemctl --user stop brickkit-shop-api-1-0-0-migration.service brickkit-shop-api-1-0-0.service "+
		"brickkit-shop-web-1-0-0@1.service brickkit-shop-web-1-0-0@2.service brickkit-shop-network.service"))
	assert.True(t, rec.called("systemctl --user daemon-reload"))
	assert.True(t, rec.called("podman network rm brickkit-shop-net"))
	assert.True(t, rec.called("podman secret rm brickkit-shop-api-1-0-0-DB_PASSWORD"))
	_, err := os.Stat(q.Dir("brickkit-shop"))
	assert.True(t, os.IsNotExist(err))

	rec.calls = nil
	require.NoError(t, q.Down(context.Background(), DownRequest{Project: "brickkit-shop"}))
	assert.Equal(t, []string{"podman ps -a --filter label=com.docker.compose.project=brickkit-shop --format {{.Names}}"}, rec.calls,
		"没装过单元：只查一遍 compose 留下的容器")
}

// 部署文件从 compose 改成了 Quadlet：up 先删掉这个项目由 compose 建的容器（按 compose 的项目标签，不要 docker-compose），
// 并报出来——两种都留着会抢同一批端口。
func TestQuadletUpRemovesComposeContainers(t *testing.T) {
	rec := newQuadletRecorder()
	rec.output["label=com.docker.compose.project=brickkit-shop"] = "brickkit-shop-api-1-0-0-1\n"
	q := quadletWith(t, rec, false)
	var pruned []string
	require.NoError(t, q.Up(context.Background(), UpRequest{
		Project: "brickkit-shop", Quadlet: units(), OnPrune: func(s string) { pruned = append(pruned, s) },
	}))
	assert.True(t, rec.called("podman rm -f brickkit-shop-api-1-0-0-1"))
	assert.Equal(t, []string{"brickkit-shop-api-1-0-0-1"}, pruned)
}

// 反过来：从 Quadlet 改回 compose，Podman 的 compose 引擎在 up 之前把这个项目的单元卸下并报出来。
func TestPodmanComposeUpRemovesQuadletUnits(t *testing.T) {
	qrec := newQuadletRecorder()
	q := quadletWith(t, qrec, false)
	require.NoError(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: units()}))

	c := podmanWith(newRecorder())
	c.quadlet = q
	qrec.calls = nil
	var pruned []string
	require.NoError(t, c.Up(context.Background(), UpRequest{
		File: "compose.yaml", Project: "brickkit-shop", OnPrune: func(s string) { pruned = append(pruned, s) },
	}))
	assert.True(t, qrec.called("systemctl --user stop"))
	assert.Contains(t, pruned, "brickkit-shop-api-1-0-0.service")
	_, err := os.Stat(q.Dir("brickkit-shop"))
	assert.True(t, os.IsNotExist(err))
}

// status：按单元找容器，按 brickkit.io/service 归并副本（取最差的），健康取自 Status 的括号；
// 失败了的单元（迁移的容器跑完就删）另列一条。
func TestQuadletStatus(t *testing.T) {
	rec := newQuadletRecorder()
	q := quadletWith(t, rec, false)
	require.NoError(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: units()}))

	rec.output["ps -a --format json"] = `[
 {"State":"running","Status":"Up 2 minutes (healthy)",
  "Labels":{"brickkit.io/service":"api-1-0-0","PODMAN_SYSTEMD_UNIT":"brickkit-shop-api-1-0-0.service"},
  "Ports":[{"host_ip":"","host_port":18080,"container_port":8080,"protocol":"tcp"}]},
 {"State":"running","Status":"Up 2 minutes (healthy)",
  "Labels":{"brickkit.io/service":"web-1-0-0","PODMAN_SYSTEMD_UNIT":"brickkit-shop-web-1-0-0@1.service"}},
 {"State":"running","Status":"Up 1 minute (unhealthy)",
  "Labels":{"brickkit.io/service":"web-1-0-0","PODMAN_SYSTEMD_UNIT":"brickkit-shop-web-1-0-0@2.service"}},
 {"State":"running","Status":"Up 1 minute (healthy)",
  "Labels":{"brickkit.io/service":"api-1-0-0","PODMAN_SYSTEMD_UNIT":"brickkit-other-api-1-0-0.service"}}
]`
	rec.output["is-failed"] = "failed\nactive\nactive\nactive\n"
	statuses, err := q.Status(context.Background(), "brickkit-shop")
	require.NoError(t, err)

	byService := map[string]Status{}
	for _, s := range statuses {
		byService[s.Service] = s
	}
	assert.True(t, byService["api-1-0-0"].Running())
	assert.Equal(t, "0.0.0.0:18080->8080/tcp", byService["api-1-0-0"].Ports)
	assert.Equal(t, "unhealthy", byService["web-1-0-0"].Health)
	assert.Equal(t, 2, byService["web-1-0-0"].Replicas)
	assert.Equal(t, 1, byService["web-1-0-0"].Ready)
	assert.Equal(t, "failed", byService["api-1-0-0-migration"].State)
	assert.True(t, rec.called("--filter label=PODMAN_SYSTEMD_UNIT"))
	assert.Equal(t, 1, byService["api-1-0-0"].Replicas, "别的项目里同名服务的容器不算")
}
