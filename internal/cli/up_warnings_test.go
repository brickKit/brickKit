// 本文件是 Step 15-C 的业务行为测试：`--config` 与启动前的告警
// （004 §3.5、§3.5.1）。覆盖 15.12，以及延后项 P5（资源密码硬编码告警）、
// P10（升级拉新版本）、P15（CheckUpgrade 接线）。
//
// 15.7 与 P22 曾经由 `--check-resources` 承担，那个参数已经删掉
// （理由见 TestUpNeverProbesResources）。
// 15.8–15.11 曾经由 `--only` 承担，那个参数也已删掉
// （003 §4.3：要收窄这次启动的范围就改 mode，不再多一套语义）。
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// threeTierProject：portal → erp → people。
func threeTierProject(t *testing.T) *projectFixture {
	t.Helper()

	comps := []comp{
		{ID: "portal/user-frontend", Version: "1.0.0", Requires: []string{"erp/backend@1.0.0"}},
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "portal/user-frontend@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
  - id: erp/backend
    version: 1.0.0
  - id: portal/user-frontend
    version: 1.0.0
`)
	return f
}

// ============================================================
// 选哪份部署文件：-f > 本地模式 > deploy.yaml（提案 §6.2、§11.6）
// ============================================================

// writeDeployFile 在项目根写一份部署文件（相对路径），三个组件都要覆盖到。
func writeDeployFile(t *testing.T, f *projectFixture, name, target, portalMode, erpMode, peopleMode string) {
	t.Helper()
	entry := func(id, mode string) string {
		out := "  - id: " + id + "\n"
		if mode != "" {
			out += "    mode: " + mode + "\n"
		}
		return out
	}
	body := "target: " + target + "\ncomponents:\n" +
		entry("portal/user-frontend", portalMode) + entry("erp/backend", erpMode) + entry("people/basic", peopleMode)
	require.NoError(t, os.WriteFile(filepath.Join(f.Dir, name), []byte(body), 0o644))
}

// -f 指定的部署文件决定这次起什么；生成物仍写进默认的 .brickkit/ 目录。
func TestUpFileFlagSelectsDeployFile(t *testing.T) {
	f := threeTierProject(t)
	writeDeployFile(t, f, "deploy.prod.yaml", "docker", "disable", "disable", "enabled")
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "-f", "deploy.prod.yaml")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, []string{"people-basic-1-0-0"}, eng.lastUp(t).Services)
	assert.Contains(t, r.stdout, "deploy.prod.yaml", "要说出这次用的是哪份部署文件")
	assert.FileExists(t, filepath.Join(f.Dir, ".brickkit", "generated", "compose.yaml"))
}

// -f 比本地模式优先：本地模式开着、deploy.local.yaml 写着 podman，
// -f 指向一份 k8s 的部署文件，这次就按 k8s 生成。
func TestUpFileFlagIgnoresLocalMode(t *testing.T) {
	f := threeTierProject(t)
	f.writeOverride(t, "target: podman\n")
	writeDeployFile(t, f, "deploy.prod.yaml", "k8s", "", "", "")

	r := runWithEngine(t, newK8sEngine(), f.Dir, "up", "--dry-run", "-f", "deploy.prod.yaml")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.DirExists(t, filepath.Join(f.Dir, ".brickkit", "generated", "k8s"))
	assert.Contains(t, r.stdout, "(target: k8s)")
}

// --no-local 让这一次忽略本地模式，读团队的 deploy.yaml。
func TestUpNoLocal(t *testing.T) {
	f := threeTierProject(t)
	f.writeOverride(t, "components:\n  - id: portal/user-frontend\n    mode: disable\n")

	local := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, local.code, local.stdout+local.stderr)
	assert.Contains(t, local.stdout, "deploy.local.yaml", "本地模式生效时要说出来")
	assert.Contains(t, local.stdout, "portal/user-frontend@1.0.0  disabled explicitly")

	team := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run", "--no-local")
	require.Equal(t, clierr.ExitOK, team.code, team.stdout+team.stderr)
	assert.NotContains(t, team.stdout, "deploy.local.yaml")
	assert.Contains(t, lineContaining(t, team.stdout, "portal/user-frontend@1.0.0"), "starting (top-level)")
}

// 密钥值与 file:// 读出的内容在 Docker 下写进 0600 的 env 文件（附录 A7），
// compose.yaml 里只剩 env_file 引用；不再生成的服务，它的旧 env 文件要清掉。
func TestUpDryRunWritesEnvFiles0600(t *testing.T) {
	spec := comp{ID: "demo/hello", Version: "1.0.0", ConfigSchema: []string{"API_TOKEN:"}, SecretConfig: []string{"API_TOKEN"}}
	f := addedProject(t, []comp{spec}, spec.ref())
	f.writeConfig(t, "components:\n  - id: demo/hello\n    version: 1.0.0\n    config:\n      API_TOKEN: sk-live-LITERAL\n")
	envDir := filepath.Join(f.Layout.GeneratedDir(), "env")
	require.NoError(t, os.MkdirAll(envDir, 0o700))
	stale := filepath.Join(envDir, "gone-away-1-0-0.env")
	require.NoError(t, os.WriteFile(stale, []byte("X=1\n"), 0o600))

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	path := filepath.Join(envDir, "demo-hello-1-0-0.env")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.Contains(t, readFile(t, path), "sk-live-LITERAL")
	compose := readFile(t, filepath.Join(f.Layout.GeneratedDir(), composeFileName))
	assert.NotContains(t, compose, "sk-live-LITERAL", "密钥不进 compose.yaml")
	assert.NoFileExists(t, stale, "不再生成的服务，旧 env 文件要清掉")
}

// ============================================================
// 启动前不做资源体检
// ============================================================

// ============================================================
// P5 资源密码硬编码告警
// ============================================================

// ============================================================
// 悬空资源绑定：警告，不阻断
// ============================================================

// ============================================================
// 弱依赖这次不跑：说清楚谁失去了什么
// ============================================================

// 关掉一个只被弱依赖指着的组件，调用方就拿不到它的 *_ENDPOINT。
//
// 003 §4.3 与 004 §4.1/§4.5 都承诺过这一句，而代码从前一个字不说：
// 状态表里只有一行"⬜ 显式禁用"，依赖图里只有一条"（弱）"，
// 两者都不说"于是 demo/caller 这次会走降级分支"。使用者得自己把两处对起来。
func TestWeakDependencyNotRunningIsReported(t *testing.T) {
	f := addedProject(t, []comp{
		{ID: "demo/caller", Version: "1.0.0", Optional: []string{"demo/hello@1.0.0"}},
		{ID: "demo/hello", Version: "1.0.0"},
	}, "demo/caller@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/caller
    version: 1.0.0
  - id: demo/hello
    version: 1.0.0
    mode: disable
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "demo/hello@1.0.0", "要点名是谁没跑")
	assert.Contains(t, r.stdout, "demo/caller", "要点名谁受影响")
	assert.Contains(t, r.stdout, "DEMO_HELLO_ENDPOINT", "要说清哪个变量拿不到")
	assert.Contains(t, r.stdout, "degradation", "要说清后果由调用方自己处理（002 §3.4）")
}

// 这是信息，不是警告。
//
// 关掉只被弱依赖引用的组件，正是 003 §4.3 推荐的"嫌容器多就下手"的做法，
// `up --dry-run` 甚至专门列出那份可以下手的名单。给一个推荐动作配 ⚠️，
// 只会训练使用者整块跳过警告区——而真正要紧的那几条也一起被跳过。
func TestWeakDependencyNotRunningIsNotAWarning(t *testing.T) {
	f := addedProject(t, []comp{
		{ID: "demo/caller", Version: "1.0.0", Optional: []string{"demo/hello@1.0.0"}},
		{ID: "demo/hello", Version: "1.0.0"},
	}, "demo/caller@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/caller
    version: 1.0.0
  - id: demo/hello
    version: 1.0.0
    mode: disable
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code)
	line := lineContaining(t, r.stdout, "DEMO_HELLO_ENDPOINT")
	assert.NotContains(t, line, "⚠️", "这是使用者刚做的决定，不是出了问题：%s", line)
}

// 弱依赖照常在跑时不该冒出这一行。
func TestRunningWeakDependencyIsQuiet(t *testing.T) {
	f := addedProject(t, []comp{
		{ID: "demo/caller", Version: "1.0.0", Optional: []string{"demo/hello@1.0.0"}},
		{ID: "demo/hello", Version: "1.0.0"},
	}, "demo/caller@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/caller
    version: 1.0.0
  - id: demo/hello
    version: 1.0.0
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "DEMO_HELLO_ENDPOINT")
}

// lineContaining 返回输出里包含该片段的那一行（含它上面那行标题）。
func lineContaining(t *testing.T, out, fragment string) string {
	t.Helper()
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if strings.Contains(line, fragment) {
			if i > 0 {
				return lines[i-1] + "\n" + line
			}
			return line
		}
	}
	t.Fatalf("输出里找不到含 %q 的行：\n%s", fragment, out)
	return ""
}

// ============================================================
// egress 覆盖不全：真 up 阻断，--dry-run 只警告
// ============================================================
