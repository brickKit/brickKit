package runcmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runDetected 用真实的操作系统探测 dir，再把探测出的命令原样跑一遍，返回合并后的输出。
// 探测出的 argv 只有真的能跑，"认得出"才有意义——所以这里不停留在核对 argv 的形状上。
func runDetected(t *testing.T, dir string) string {
	t.Helper()
	cmd, err := Detect(dir, Hints{}, Params{Port: testPort})
	require.NoError(t, err)
	require.NoError(t, cmd.CheckProgram())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := exec.CommandContext(ctx, cmd.Argv[0], cmd.Argv[1:]...)
	c.Dir = cmd.Dir
	c.Env = append(os.Environ(), append(cmd.Env, "NO_UPDATE_NOTIFIER=1", "npm_config_update_notifier=false")...)
	out, err := c.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}

func TestADetectedGoCommandReallyRuns(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("这台机器没有 go 工具链")
	}
	dir := write(t, map[string]string{
		"go.mod":  "module example.com/hello\n",
		"main.go": "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"runcmd-go-ok\") }\n",
	})

	assert.Contains(t, runDetected(t, dir), "runcmd-go-ok")
}

func TestADetectedNodeCommandReallyRuns(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("这台机器没有 npm")
	}
	dir := write(t, map[string]string{
		"package.json": `{"name": "hello", "version": "1.0.0", "scripts": {"start": "node server.js"}}`,
		"server.js":    "console.log('runcmd-node-ok');\n",
	})

	assert.Contains(t, runDetected(t, dir), "runcmd-node-ok")
}

// 仓库自己的自测组件是最真实的样本：六个 Go 组件、两个 FastAPI 组件、一个只有测试的 Go 模块。
// 这里同时记下"刻意不认"的边界——FastAPI 没有约定可循，得由用户手写 local.runCommand。
func TestTheRepositorysOwnTestComponents(t *testing.T) {
	root := filepath.Join("..", "..", "tests", "components")

	for _, name := range []string{
		"demo-hello", "demo-caller", "auth-password-login", "authorization-rbac",
		"department-tree", "erp-backend", "infra-api-docs",
	} {
		cmd := mustDetect(t, filepath.Join(root, name), Hints{})
		assert.Equal(t, []string{"go", "run", "."}, cmd.Argv, name)
	}

	for _, name := range []string{"people-basic", "infra-redis-event-bus"} {
		assert.Empty(t, problemsOf(t, filepath.Join(root, name), Hints{}), name)
	}

	assert.Equal(t, []Problem{{Language: "go", Reason: ReasonNoEntryPoint, Detail: "go.mod"}},
		problemsOf(t, filepath.Join(root, "portal-user-frontend"), Hints{}))
}
