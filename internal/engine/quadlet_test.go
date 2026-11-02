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
		// 停之前问 systemd 加载了没有：按调用里的单元数答 loaded（见 run）
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
	if _, set := r.output["show --property=LoadState"]; !set && strings.Contains(call, "show --property=LoadState --value") {
		units := len(args) - 3
		if args[0] == "--user" {
			units--
		}
		return []byte(strings.Repeat("loaded\n", units)), nil
	}
	for pattern, out := range r.output {
		if strings.Contains(call, pattern) {
			return []byte(out), nil
		}
	}
	return nil, nil
}

// index 是第一条包含 want 的命令的位置；没有时为 -1。
func (r *quadletRecorder) index(want string) int {
	for i, c := range r.calls {
		if strings.Contains(c, want) {
			return i
		}
	}
	return -1
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
			{Name: "brickkit-shop_api-1-0-0-migration.container", Content: []byte("migration v1")},
			{Name: "brickkit-shop_api-1-0-0.container", Content: []byte("api v1")},
			{Name: "brickkit-shop_web-1-0-0@.container", Content: []byte("web v1")},
			{Name: "brickkit-shop_web-1-0-0@1.container", LinkTo: "brickkit-shop_web-1-0-0@.container"},
			{Name: "brickkit-shop_web-1-0-0@2.container", LinkTo: "brickkit-shop_web-1-0-0@.container"},
		},
		Secrets: []Secret{{Name: "brickkit-shop_api-1-0-0_DB_PASSWORD", Value: []byte("pa$$\nword")}},
		MigrationSecrets: []Secret{
			{Name: "brickkit-shop_api-1-0-0-migration_DB_PASSWORD", Value: []byte("pa$$\nword")},
		},
		Images:     []string{"registry.example.com/api:1.0.0", "registry.example.com/web:1.0.0"},
		Migrations: []string{"brickkit-shop_api-1-0-0-migration.service"},
		Components: []string{
			"brickkit-shop_api-1-0-0.service", "brickkit-shop_web-1-0-0@1.service", "brickkit-shop_web-1-0-0@2.service",
		},
	}
}

// 首次 up：缺的镜像先拉，secret 从标准输入建（值不落盘、不进命令行），单元写进项目的子目录（实例是指向模板的
// 链接）。分两步：迁移的 secret 与单元、daemon-reload、起网络、重跑迁移；之后才建组件的 secret、装组件的单元、
// 再 daemon-reload，把（全是新的）组件重启、启动；最后划掉待重启的记录、删掉本项目不再用的 secret（别的项目的
// 不碰，哪怕名字以本项目名开头）。用户的 systemd。
func TestQuadletFirstUp(t *testing.T) {
	rec := newQuadletRecorder()
	rec.output["secret ls"] = "brickkit-shop_api-1-0-0_DB_PASSWORD\nbrickkit-shop_api-1-0-0_OLD\nbrickkit-shop-x_api-1-0-0_KEY\n"
	rec.fail["image exists registry.example.com/web:1.0.0"] = true
	q := quadletWith(t, rec, false)

	require.NoError(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: units()}))

	assert.Equal(t, "pa$$\nword", rec.stdin["podman secret create --replace brickkit-shop_api-1-0-0_DB_PASSWORD -"])
	assert.Equal(t, "pa$$\nword", rec.stdin["podman secret create --replace brickkit-shop_api-1-0-0-migration_DB_PASSWORD -"])
	migrate := rec.index("restart brickkit-shop_api-1-0-0-migration.service")
	assert.Less(t, rec.index("secret create --replace brickkit-shop_api-1-0-0-migration_DB_PASSWORD"), migrate)
	assert.Greater(t, rec.index("secret create --replace brickkit-shop_api-1-0-0_DB_PASSWORD"), migrate,
		"组件的 secret 在迁移成功之后才换")
	assert.True(t, rec.called("podman pull registry.example.com/web:1.0.0"))
	assert.False(t, rec.called("podman pull registry.example.com/api:1.0.0"), "已经在本机的不拉")
	dir := q.Dir("brickkit-shop")
	content, err := os.ReadFile(filepath.Join(dir, "brickkit-shop_api-1-0-0.container"))
	require.NoError(t, err)
	assert.Equal(t, "api v1", string(content))
	target, err := os.Readlink(filepath.Join(dir, "brickkit-shop_web-1-0-0@2.container"))
	require.NoError(t, err)
	assert.Equal(t, "brickkit-shop_web-1-0-0@.container", target)

	var systemctl []string
	for _, c := range rec.calls {
		if strings.HasPrefix(c, "systemctl") {
			systemctl = append(systemctl, c)
		}
	}
	assert.Equal(t, []string{
		"systemctl --user daemon-reload",
		"systemctl --user start brickkit-shop-network.service",
		"systemctl --user restart brickkit-shop_api-1-0-0-migration.service",
		"systemctl --user daemon-reload",
		"systemctl --user restart brickkit-shop_api-1-0-0.service brickkit-shop_web-1-0-0@1.service brickkit-shop_web-1-0-0@2.service",
		"systemctl --user start brickkit-shop_api-1-0-0.service brickkit-shop_web-1-0-0@1.service brickkit-shop_web-1-0-0@2.service",
	}, systemctl)
	assert.True(t, rec.called("podman secret rm brickkit-shop_api-1-0-0_OLD"))
	assert.False(t, rec.called("brickkit-shop-x_"), "项目 shop-x 的 secret 不碰：名字以 brickkit-shop- 开头，不以 brickkit-shop_ 开头")
	assert.NoFileExists(t, filepath.Join(dir, pendingFile), "都重启过了，待重启的记录划掉")
}

// 再 up 一次、内容没变：迁移照样重跑（restart），组件只 start（在跑的不动），不 restart。
// 只改了模板：它的每个实例都重启，别的不动。
func TestQuadletRestartsOnlyWhatChanged(t *testing.T) {
	rec := newQuadletRecorder()
	q := quadletWith(t, rec, false)
	require.NoError(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: units()}))

	rec.calls = nil
	require.NoError(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: units()}))
	assert.True(t, rec.called("systemctl --user restart brickkit-shop_api-1-0-0-migration.service"))
	assert.False(t, rec.called("restart brickkit-shop_api-1-0-0.service"))
	assert.False(t, rec.called("restart brickkit-shop_web"))

	rec.calls = nil
	changed := units()
	changed.Files[3].Content = []byte("web v2")
	require.NoError(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: changed}))
	assert.True(t, rec.called("systemctl --user restart brickkit-shop_web-1-0-0@1.service brickkit-shop_web-1-0-0@2.service"))
	assert.False(t, rec.called("restart brickkit-shop_api-1-0-0.service"))
}

// 不再生成的单元：迁移成功之后，先停再删文件（文件没了 systemd 就管不到它的容器），并报给调用方。
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
	assert.True(t, rec.called("systemctl --user stop brickkit-shop_web-1-0-0@1.service brickkit-shop_web-1-0-0@2.service"))
	assert.Equal(t, []string{"brickkit-shop_web-1-0-0@1.service", "brickkit-shop_web-1-0-0@2.service"}, pruned)
	_, err := os.Stat(filepath.Join(q.Dir("brickkit-shop"), "brickkit-shop_web-1-0-0@.container"))
	assert.True(t, os.IsNotExist(err))
}

// 迁移失败：报 MIGRATION_FAILED，点名失败的单元与看日志的命令；组件一个都不动（旧版本照跑）——升级时旧版本的
// 单元名不同，它也不停、不删；compose 留下的容器也不删。组件的单元与 secret 也不换：开机与崩溃重启时 systemd
// 照着它们起的仍是旧版本，而不是面对没迁移过的库的新版本。修好再 up：新版本照常装上、启动，旧版本卸下。
func TestQuadletMigrationFailureTouchesNoComponent(t *testing.T) {
	rec := newQuadletRecorder()
	q := quadletWith(t, rec, false)
	require.NoError(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: units()}))

	upgraded := units()
	upgraded.Files[2] = UnitFile{Name: "brickkit-shop_api-2-0-0.container", Content: []byte("api v2")}
	upgraded.Files[1] = UnitFile{Name: "brickkit-shop_api-2-0-0-migration.container", Content: []byte("migration v2")}
	upgraded.Migrations = []string{"brickkit-shop_api-2-0-0-migration.service"}
	upgraded.Components[0] = "brickkit-shop_api-2-0-0.service"
	rec.calls = nil
	rec.fail["restart brickkit-shop_api-2-0-0-migration.service"] = true
	rec.output["is-failed"] = "failed\n"
	rec.output["label=com.docker.compose.project"] = "brickkit-shop-old-1\n"

	err := q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: upgraded})
	require.Error(t, err)
	assert.Equal(t, clierr.CodeMigrationFailed, clierr.As(err).Code)
	assert.Contains(t, err.Error(), "brickkit-shop_api-2-0-0-migration.service")
	assert.Contains(t, err.Error(), "journalctl --user -u brickkit-shop_api-2-0-0-migration.service")
	assert.False(t, rec.called("restart brickkit-shop_api-2-0-0.service"))
	assert.False(t, rec.called("stop"), "旧版本的单元不停")
	assert.FileExists(t, filepath.Join(q.Dir("brickkit-shop"), "brickkit-shop_api-1-0-0.container"))
	assert.NoFileExists(t, filepath.Join(q.Dir("brickkit-shop"), "brickkit-shop_api-2-0-0.container"),
		"新版本的组件单元没装：装上它，下一次开机就会起来")
	assert.False(t, rec.called("secret create --replace brickkit-shop_api-1-0-0_DB_PASSWORD"), "组件的 secret 不换")
	assert.False(t, rec.called("podman rm"), "compose 留下的容器不删")

	delete(rec.fail, "restart brickkit-shop_api-2-0-0-migration.service")
	delete(rec.output, "label=com.docker.compose.project")
	rec.calls = nil
	require.NoError(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: upgraded}))
	assert.True(t, rec.called("systemctl --user stop brickkit-shop_api-1-0-0-migration.service brickkit-shop_api-1-0-0.service"))
	assert.True(t, rec.called("systemctl --user restart brickkit-shop_api-2-0-0.service"))
}

// 组件的单元换了、重启却失败了（或被打断）：再 up 时文件已经是新的，照样重启它（待重启的记录），
// 不会以为没变、让组件跑着旧配置却报成功。
func TestQuadletRetriesARestartThatFailed(t *testing.T) {
	rec := newQuadletRecorder()
	q := quadletWith(t, rec, false)
	require.NoError(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: units()}))

	changed := units()
	changed.Files[2].Content = []byte("api v2")
	rec.fail["restart brickkit-shop_api-1-0-0.service"] = true
	require.Error(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: changed}))

	delete(rec.fail, "restart brickkit-shop_api-1-0-0.service")
	rec.calls = nil
	require.NoError(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: changed}))
	assert.True(t, rec.called("systemctl --user restart brickkit-shop_api-1-0-0.service"))
	assert.NoFileExists(t, filepath.Join(q.Dir("brickkit-shop"), pendingFile))
}

// 停之前只停 systemd 加载了的单元：没加载的（生成器拒绝了它的文件，或上次在写文件与 reload 之间被打断）stop 会以 5
// 退出，up 与 down 就永远卡在它上面。
func TestQuadletStopsOnlyLoadedUnits(t *testing.T) {
	rec := newQuadletRecorder()
	q := quadletWith(t, rec, false)
	require.NoError(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: units()}))

	rec.calls = nil
	// 顺序与安装目录里的文件一样：网络、迁移、api、web@1、web@2
	rec.output["show --property=LoadState"] = "not-found\nnot-found\nloaded\nloaded\nloaded\n"
	require.NoError(t, q.Down(context.Background(), DownRequest{Project: "brickkit-shop"}))
	assert.True(t, rec.called("systemctl --user stop brickkit-shop_api-1-0-0.service brickkit-shop_web-1-0-0@1.service brickkit-shop_web-1-0-0@2.service"))
	_, err := os.Stat(q.Dir("brickkit-shop"))
	assert.True(t, os.IsNotExist(err))
}

// 组件起不来：点名哪几个单元失败、日志怎么看。
func TestQuadletComponentFailureNamesTheUnits(t *testing.T) {
	rec := newQuadletRecorder()
	rec.fail["restart brickkit-shop_api-1-0-0.service"] = true
	rec.output["is-failed"] = "active\nfailed\nactive\n"
	q := quadletWith(t, rec, true)

	err := q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: units()})
	require.Error(t, err)
	assert.Equal(t, clierr.CodeEngineFailed, clierr.As(err).Code)
	assert.Contains(t, err.Error(), "journalctl -u brickkit-shop_web-1-0-0@1.service", "有根：系统的日志，不带 --user")
	assert.NotContains(t, err.Error(), "brickkit-shop_api-1-0-0.service")
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
	rec.output["secret ls"] = "brickkit-shop_api-1-0-0_DB_PASSWORD\n"
	q := quadletWith(t, rec, false)
	require.NoError(t, q.Up(context.Background(), UpRequest{Project: "brickkit-shop", Quadlet: units()}))

	rec.calls = nil
	require.NoError(t, q.Down(context.Background(), DownRequest{Project: "brickkit-shop"}))
	assert.True(t, rec.called("systemctl --user stop brickkit-shop-network.service brickkit-shop_api-1-0-0-migration.service "+
		"brickkit-shop_api-1-0-0.service brickkit-shop_web-1-0-0@1.service brickkit-shop_web-1-0-0@2.service"))
	assert.True(t, rec.called("systemctl --user daemon-reload"))
	assert.True(t, rec.called("podman network rm brickkit-shop-net"))
	assert.True(t, rec.called("podman secret rm brickkit-shop_api-1-0-0_DB_PASSWORD"))
	_, err := os.Stat(q.Dir("brickkit-shop"))
	assert.True(t, os.IsNotExist(err))

	rec.calls = nil
	require.NoError(t, q.Down(context.Background(), DownRequest{Project: "brickkit-shop"}))
	assert.False(t, rec.called("systemctl"), "没装过单元：systemd 那边什么都不做，只清 compose 留下的容器与本项目的 secret")
}

// 部署文件从 compose 改成了 Quadlet：up 先删掉这个项目由 compose 建的容器（按 compose 的项目标签，不要 docker-compose），
// 并报出来——两种都留着会抢同一批端口。
func TestQuadletUpRemovesComposeContainers(t *testing.T) {
	rec := newQuadletRecorder()
	rec.output["label=com.docker.compose.project=brickkit-shop"] = "brickkit-shop_api-1-0-0-1\n"
	q := quadletWith(t, rec, false)
	var pruned []string
	require.NoError(t, q.Up(context.Background(), UpRequest{
		Project: "brickkit-shop", Quadlet: units(), OnPrune: func(s string) { pruned = append(pruned, s) },
	}))
	assert.True(t, rec.called("podman rm -f brickkit-shop_api-1-0-0-1"))
	assert.Equal(t, []string{"brickkit-shop_api-1-0-0-1"}, pruned)
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
	assert.Contains(t, pruned, "brickkit-shop_api-1-0-0.service")
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
  "Labels":{"brickkit.io/service":"api-1-0-0","PODMAN_SYSTEMD_UNIT":"brickkit-shop_api-1-0-0.service"},
  "Ports":[{"host_ip":"","host_port":18080,"container_port":8080,"protocol":"tcp"}]},
 {"State":"running","Status":"Up 2 minutes (healthy)",
  "Labels":{"brickkit.io/service":"web-1-0-0","PODMAN_SYSTEMD_UNIT":"brickkit-shop_web-1-0-0@1.service"}},
 {"State":"running","Status":"Up 1 minute (unhealthy)",
  "Labels":{"brickkit.io/service":"web-1-0-0","PODMAN_SYSTEMD_UNIT":"brickkit-shop_web-1-0-0@2.service"}},
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
