// 本文件是 Step 19「brickkit login」的业务行为测试，
// 覆盖开发计划 19.1–19.6，以及 004 §3.12 的凭据格式与 Token 优先级。
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// credentialsPath 是登录凭据的位置（004 §5.3）。
func credentialsPath(dir string) string {
	return filepath.Join(dir, ".brickkit", "credentials")
}

// readCredentials 读出凭据文件并解析。
func readCredentials(t *testing.T, dir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(credentialsPath(dir))
	require.NoError(t, err, "凭据文件应存在")

	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out), "凭据文件应是合法 JSON：%s", data)
	return out
}

// newMarketProject 建一个把假市场配成安装源的项目。
func newMarketProject(t *testing.T, m *fakeMarket, authToken string) *projectFixture {
	t.Helper()
	dir := t.TempDir()
	f := newProjectFixtureAt(t, dir, marketSourceFragment("my-market", m.url(), authToken))
	f.writeConfig(t, "components: []\nresources: []\n")
	return f
}

// ============================================================
// 19.1 / 19.3 登录成功
// ============================================================

func TestLoginWritesCredentials(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")

	r := runStdin(t, f.Dir, "zhangsan\ncorrect-horse-battery\n", "login")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.FileExists(t, credentialsPath(f.Dir), "19.3：Token 必须存到 .brickkit/credentials")
	assert.Contains(t, r.stdout, "✅ 登录成功")
	assert.Contains(t, r.stdout, "zhangsan")
	assert.Contains(t, r.stdout, ".brickkit/credentials")
}

// 19.4 / 19.5 凭据文件的字段（004 §3.12 的格式）。
func TestLoginCredentialsFormat(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")

	require.Equal(t, clierr.ExitOK, runStdin(t, f.Dir, "zhangsan\ncorrect-horse-battery\n", "login").code)

	creds := readCredentials(t, f.Dir)
	assert.Equal(t, "password", creds["type"], "19.4：type 字段当前固定为 password")
	assert.Equal(t, "zhangsan", creds["username"])
	assert.Equal(t, m.token, creds["token"])
	assert.Equal(t, m.url(), creds["marketUrl"], "必须记下是哪个市场的 Token（008：不能跨市场发凭据）")
	require.Contains(t, creds, "expiresAt", "19.5：必须有 expiresAt")

	expiresAt, err := time.Parse(time.RFC3339, creds["expiresAt"].(string))
	require.NoError(t, err, "expiresAt 必须是 RFC3339")
	assert.True(t, expiresAt.After(time.Now()), "有效期应在未来")
	assert.Contains(t, creds, "createdAt")
}

// 凭据文件里是明文 Token，权限必须收紧到只有本人可读。
func TestLoginCredentialsFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上没有 Unix 权限位")
	}
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")

	require.Equal(t, clierr.ExitOK, runStdin(t, f.Dir, "zhangsan\ncorrect-horse-battery\n", "login").code)

	info, err := os.Stat(credentialsPath(f.Dir))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "凭据文件权限必须是 0600")
}

// 密码绝不能出现在终端输出或日志里。
func TestLoginNeverEchoesPassword(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")

	r := runStdin(t, f.Dir, "zhangsan\ncorrect-horse-battery\n", "login")

	require.Equal(t, clierr.ExitOK, r.code)
	assert.NotContains(t, r.stdout, "correct-horse-battery")
	assert.NotContains(t, r.stderr, "correct-horse-battery")
}

// ============================================================
// 19.2 登录失败
// ============================================================

func TestLoginWithWrongPasswordFails(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")

	r := runStdin(t, f.Dir, "zhangsan\nwrong-password\n", "login")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "登录失败")
	assert.Contains(t, r.stderr, "用户名或密码")
	assert.NoFileExists(t, credentialsPath(f.Dir), "登录失败不该留下凭据文件")
}

// 登录失败不能把已有的有效凭据毁掉。
func TestFailedLoginKeepsExistingCredentials(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	require.Equal(t, clierr.ExitOK, runStdin(t, f.Dir, "zhangsan\ncorrect-horse-battery\n", "login").code)
	before := readCredentials(t, f.Dir)

	r := runStdin(t, f.Dir, "zhangsan\nwrong-password\n", "login")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Equal(t, before, readCredentials(t, f.Dir), "失败的登录不能动已有凭据")
}

// 市场不可达时要说"连不上"，而不是"密码错误"——这两件事的处理方式完全不同。
func TestLoginAgainstUnreachableMarketReportsNetworkError(t *testing.T) {
	dir := t.TempDir()
	f := newProjectFixtureAt(t, dir,
		marketSourceFragment("dead-market", "http://127.0.0.1:1/api/v1", ""))
	f.writeConfig(t, "components: []\nresources: []\n")

	r := runStdin(t, f.Dir, "zhangsan\nany-password\n", "login")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "市场不可达")
}

// ============================================================
// 市场地址的确定
// ============================================================

// 项目里没有市场安装源时，必须让用户显式给出地址。
func TestLoginWithoutMarketSourceRequiresFlag(t *testing.T) {
	dir := t.TempDir()
	f := newProjectFixtureAt(t, dir)

	r := runStdin(t, f.Dir, "zhangsan\npw\n", "login")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "--market")
}

// 配了多个市场时不能瞎猜该登录哪个。
func TestLoginWithMultipleMarketsRequiresChoice(t *testing.T) {
	m1, m2 := newFakeMarket(t), newFakeMarket(t)
	dir := t.TempDir()
	f := newProjectFixtureAt(t, dir,
		marketSourceFragment("market-a", m1.url(), ""),
		marketSourceFragment("market-b", m2.url(), ""))
	f.writeConfig(t, "components: []\nresources: []\n")

	r := runStdin(t, f.Dir, "zhangsan\ncorrect-horse-battery\n", "login")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "--market")
	assert.Contains(t, r.stderr, "market-a")
	assert.Contains(t, r.stderr, "market-b")
}

// --market 显式指定时不需要项目配置：在任何目录都能登录。
func TestLoginWithExplicitMarketNeedsNoProject(t *testing.T) {
	m := newFakeMarket(t)
	dir := t.TempDir()

	r := runStdin(t, dir, "zhangsan\ncorrect-horse-battery\n", "login", "--market", m.url())

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.FileExists(t, credentialsPath(dir))
}

// ============================================================
// 非交互式登录（CI 用）
// ============================================================

// CI 里没有人能敲键盘：用户名走参数，密码走标准输入。
func TestLoginNonInteractiveWithPasswordStdin(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")

	r := runStdin(t, f.Dir, "correct-horse-battery\n",
		"login", "--username", "zhangsan", "--password-stdin")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, m.token, readCredentials(t, f.Dir)["token"])
}

// 重复登录是覆盖，不是报错：换账号、换市场都很常见。
func TestLoginOverwritesPreviousCredentials(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	require.Equal(t, clierr.ExitOK, runStdin(t, f.Dir, "zhangsan\ncorrect-horse-battery\n", "login").code)

	m.username, m.token = "lisi", "another-token"
	r := runStdin(t, f.Dir, "lisi\ncorrect-horse-battery\n", "login")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	creds := readCredentials(t, f.Dir)
	assert.Equal(t, "lisi", creds["username"])
	assert.Equal(t, "another-token", creds["token"])
}
