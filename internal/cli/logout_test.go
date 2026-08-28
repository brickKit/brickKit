// 本文件是 brickkit logout 的行为测试（004 §3.13）。
package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/source"
)

// loggedInProject 造一个已经登录过的项目，并返回市场收到的注销请求数。
func loggedInProject(t *testing.T, marketURL string) *projectFixture {
	t.Helper()
	f := newProjectFixture(t)
	require.NoError(t, source.SaveCredentials(f.Layout.CredentialsPath(), &source.Credentials{
		Type:      source.CredentialTypePassword,
		MarketURL: marketURL,
		Username:  "zhangsan",
		Token:     "tok-123",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}))
	return f
}

// 正常退出：通知市场作废，并删掉本地凭据。
func TestLogoutRevokesTokenAndRemovesCredentials(t *testing.T) {
	var gotAuth string
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotAuth = r.Header.Get("Authorization")
		assert.Equal(t, "/api/v1/auth/logout", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	f := loggedInProject(t, srv.URL+"/api/v1")
	r := runIn(t, f.Dir, "logout")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, 1, calls, "要真的通知市场作废那个 Token")
	assert.Equal(t, "Bearer tok-123", gotAuth)
	assert.NoFileExists(t, f.Layout.CredentialsPath(), "本地凭据必须删掉")
	assert.Contains(t, r.stdout, "zhangsan")
}

// 市场连不上时**本地凭据照样删掉**，只警告一句。
//
// 反过来的话，一次网络抖动就让人以为自己已经退出了，而那份凭据还躺在盘上——
// 比没退出更糟，因为他不会再去管它。
func TestLogoutRemovesCredentialsEvenWhenMarketIsDown(t *testing.T) {
	f := loggedInProject(t, "http://127.0.0.1:59998/api/v1")

	r := runIn(t, f.Dir, "logout")

	require.Equal(t, clierr.ExitOK, r.code, "市场不可达不该让退出失败：%s", r.stdout+r.stderr)
	assert.NoFileExists(t, f.Layout.CredentialsPath())
	assert.Contains(t, r.stdout, "市场不可达", "但要说清楚发生了什么")
	assert.Contains(t, r.stdout, "仍然有效", "以及那个 Token 还没被作废")
}

// --keep-remote 只删本地，不碰市场。
func TestLogoutKeepRemoteSkipsTheMarket(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
	}))
	defer srv.Close()

	f := loggedInProject(t, srv.URL+"/api/v1")
	r := runIn(t, f.Dir, "logout", "--keep-remote")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Zero(t, calls, "--keep-remote 时一次请求都不该发")
	assert.NoFileExists(t, f.Layout.CredentialsPath())
}

// 没登录时什么都不做，也不算失败。
func TestLogoutWithoutCredentialsIsNotAnError(t *testing.T) {
	f := newProjectFixture(t)

	r := runIn(t, f.Dir, "logout")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "没有登录凭据")
}

// 凭据文件坏了也要能退出——那正是使用者想清掉它的时候。
func TestLogoutRemovesBrokenCredentials(t *testing.T) {
	f := newProjectFixture(t)
	require.NoError(t, os.WriteFile(f.Layout.CredentialsPath(), []byte("{ 坏掉的 json"), 0o600))

	r := runIn(t, f.Dir, "logout")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NoFileExists(t, f.Layout.CredentialsPath())
}
