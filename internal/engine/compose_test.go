// 本文件是引擎层的代码测试：命令怎么拼、输出怎么解析、失败怎么翻译。
//
// 命令层的决策（谁该启动、先检查什么）由 internal/cli 的用例盯住；
// 这里只管"把决定翻译成 docker 命令"这一段。真引擎另有真实运行验证。
package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// recorder 记录执行过的命令，并按需返回预设的输出与错误。
type recorder struct {
	calls  [][]string
	output map[string]string
	fail   map[string]error
}

func newRecorder() *recorder {
	return &recorder{output: map[string]string{}, fail: map[string]error{}}
}

func (r *recorder) run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	key := strings.Join(args, " ")
	for pattern, err := range r.fail {
		if strings.Contains(key, pattern) {
			return []byte(r.output[pattern]), err
		}
	}
	for pattern, out := range r.output {
		if strings.Contains(key, pattern) {
			return []byte(out), nil
		}
	}
	return nil, nil
}

func (r *recorder) lastCall(t *testing.T) string {
	t.Helper()
	require.NotEmpty(t, r.calls, "期望执行了命令")
	return strings.Join(r.calls[len(r.calls)-1], " ")
}

func dockerWith(rec *recorder) *Compose {
	c := NewDocker()
	c.runner = rec.run
	return c
}

// ============================================================
// 命令拼装
// ============================================================

func TestUpCommand(t *testing.T) {
	rec := newRecorder()

	require.NoError(t, dockerWith(rec).Up(context.Background(), UpRequest{
		File:     "/p/.brickkit/generated/docker-compose.yaml",
		Project:  "brickkit-my-erp",
		Services: []string{"people-basic-1-0-0"},
	}))

	call := rec.lastCall(t)
	assert.Contains(t, call, "docker compose")
	assert.Contains(t, call, "-p brickkit-my-erp", "项目名必须显式传，否则所有项目都叫 generated")
	assert.Contains(t, call, "-f /p/.brickkit/generated/docker-compose.yaml")
	assert.Contains(t, call, "up -d --wait")
	assert.Contains(t, call, "people-basic-1-0-0")
}

// down 绝不能带 -v：那会连数据库数据一起删掉（004 §3.6）。
func TestDownNeverRemovesVolumes(t *testing.T) {
	rec := newRecorder()

	require.NoError(t, dockerWith(rec).Down(context.Background(), DownRequest{
		File: "f.yaml", Project: "brickkit-my-erp",
	}))

	call := rec.lastCall(t)
	assert.Contains(t, call, "down")
	assert.NotContains(t, call, "-v")
	assert.NotContains(t, call, "--volumes")
}

// 只停部分组件时用 rm -sf 而不是 down：
// down 会把网络一起拆掉，剩下还在跑的组件会瞬间失去彼此。
func TestDownPartialUsesRemoveNotDown(t *testing.T) {
	rec := newRecorder()

	require.NoError(t, dockerWith(rec).Down(context.Background(), DownRequest{
		File: "f.yaml", Project: "brickkit-my-erp",
		Services: []string{"erp-backend-1-0-0"},
	}))

	call := rec.lastCall(t)
	assert.Contains(t, call, "rm -sf erp-backend-1-0-0")
	assert.NotContains(t, call, " down")
}

func TestPodmanUsesItsOwnBinary(t *testing.T) {
	rec := newRecorder()
	c := NewPodman()
	c.runner = rec.run

	require.NoError(t, c.Up(context.Background(), UpRequest{File: "f.yaml", Project: "p"}))

	assert.Equal(t, Podman, c.Name())
	assert.True(t, strings.HasPrefix(rec.lastCall(t), "podman-compose "), rec.lastCall(t))
	assert.NotContains(t, rec.lastCall(t), "compose compose")
}

// ============================================================
// 镜像检查
// ============================================================

// 本地已有的镜像不去问 registry。
//
// 自己 build 出来的镜像根本不在任何 registry 里，去问只会得到一个假的
// "未授权"，把使用者引向 docker login 这条死路——而他要做的其实什么都没有。
func TestCheckImageAcceptsLocalImage(t *testing.T) {
	rec := newRecorder()

	require.NoError(t, dockerWith(rec).CheckImage(context.Background(), "brickkit-demo/dept:1.0.0"))

	require.Len(t, rec.calls, 1, "本地有就到此为止，不该再问 registry")
	assert.Contains(t, rec.lastCall(t), "image inspect")
}

func TestCheckImageFallsBackToRegistry(t *testing.T) {
	rec := newRecorder()
	rec.fail["image inspect"] = errors.New("exit 1")

	require.NoError(t, dockerWith(rec).CheckImage(context.Background(), "registry.io/a:1"))

	assert.Len(t, rec.calls, 2)
	assert.Contains(t, rec.lastCall(t), "manifest inspect")
}

func TestCheckImageUnauthorized(t *testing.T) {
	rec := newRecorder()
	rec.fail["image inspect"] = errors.New("exit 1")
	rec.fail["manifest inspect"] = errors.New("exit 1")
	rec.output["manifest inspect"] = "unauthorized: authentication required"

	err := dockerWith(rec).CheckImage(context.Background(), "registry.io/a:1")

	require.Error(t, err)
	assert.Equal(t, clierr.CodeImageUnauthorized, clierr.As(err).Code)
	assert.Contains(t, clierr.As(err).Format(), "docker login")
}

// 网络不通不该建议 docker login：那是完全另一回事。
func TestCheckImageNetworkFailureIsNotAuth(t *testing.T) {
	rec := newRecorder()
	rec.fail["image inspect"] = errors.New("exit 1")
	rec.fail["manifest inspect"] = errors.New("exit 1")
	rec.output["manifest inspect"] = "dial tcp: lookup registry.io: no such host"

	err := dockerWith(rec).CheckImage(context.Background(), "registry.io/a:1")

	require.Error(t, err)
	assert.Equal(t, clierr.CodeNetworkUnreachable, clierr.As(err).Code)
	assert.NotContains(t, clierr.As(err).Format(), "docker login")
}

func TestCheckImageNotFound(t *testing.T) {
	rec := newRecorder()
	rec.fail["image inspect"] = errors.New("exit 1")
	rec.fail["manifest inspect"] = errors.New("exit 1")
	rec.output["manifest inspect"] = "manifest unknown"

	err := dockerWith(rec).CheckImage(context.Background(), "registry.io/a:1")

	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "先 build 出这个镜像")
}

// ============================================================
// 状态解析
// ============================================================

// 真实 compose 的输出（Docker Compose v5，2026-08 实测原样截取）。
//
// 关键在 Publishers：它是**对象数组**而不是字符串。当初按字符串映射，
// 第一次真跑就在这里解析失败——而失败时 CLI 只会说一句
// "无法解析容器引擎的状态输出"，看不出是哪个字段。
const realComposePS = `{"Command":"\"/app/department-tree\"","ExitCode":0,"Health":"healthy","Service":"department-tree-1-0-0","State":"running","Publishers":[{"URL":"","TargetPort":8080,"PublishedPort":0,"Protocol":"tcp"}],"Ports":"8080/tcp"}
{"Command":"\"/app/department-tre…\"","ExitCode":0,"Health":"","Service":"department-tree-1-0-0-migration","State":"exited","Publishers":[],"Ports":""}
{"Command":"\"python -m app.main\"","ExitCode":0,"Health":"starting","Service":"people-basic-1-0-0","State":"running","Publishers":[{"URL":"0.0.0.0","TargetPort":8080,"PublishedPort":18092,"Protocol":"tcp"}],"Ports":"9090/tcp, 0.0.0.0:18092->8080/tcp"}
{"Command":"\"docker-entrypoint.s…\"","ExitCode":0,"Health":"healthy","Service":"postgres","State":"running","Publishers":[{"URL":"","TargetPort":5432,"PublishedPort":0,"Protocol":"tcp"}],"Ports":"5432/tcp"}`

func TestParseRealComposeOutput(t *testing.T) {
	statuses, err := parsePS([]byte(realComposePS))

	require.NoError(t, err, "真实输出必须能解析")
	require.Len(t, statuses, 4)

	byService := map[string]Status{}
	for _, s := range statuses {
		byService[s.Service] = s
	}
	assert.True(t, byService["department-tree-1-0-0"].Running())
	assert.False(t, byService["department-tree-1-0-0-migration"].Running(), "迁移容器跑完就退出")
	assert.False(t, byService["people-basic-1-0-0"].Running(), "health=starting 还不算起来了")
	assert.Contains(t, byService["people-basic-1-0-0"].Ports, "18092->8080")
}

// Publishers 是数组时也要能给出端口描述：有些版本只有 Publishers 没有 Ports。
func TestParsePSRendersPublishers(t *testing.T) {
	out := `{"Service":"a","State":"running","Publishers":[{"URL":"0.0.0.0","TargetPort":8080,"PublishedPort":18080,"Protocol":"tcp"},{"URL":"","TargetPort":9090,"PublishedPort":0,"Protocol":"tcp"}]}`

	statuses, err := parsePS([]byte(out))

	require.NoError(t, err)
	require.Len(t, statuses, 1)
	assert.Equal(t, "0.0.0.0:18080->8080/tcp", statuses[0].Ports,
		"没映射到宿主机的端口不必列出")
}

// 旧版 compose 每行一个 JSON 对象、Publishers 是字符串。
func TestParsePSLineDelimited(t *testing.T) {
	out := `{"Service":"people-basic-1-0-0","State":"running","Health":"healthy","Publishers":"0.0.0.0:18092->8080/tcp"}
{"Service":"people-basic-1-0-0-migration","State":"exited","ExitCode":0}`

	statuses, err := parsePS([]byte(out))

	require.NoError(t, err)
	require.Len(t, statuses, 2)
	assert.True(t, statuses[0].Running())
	assert.Equal(t, "0.0.0.0:18092->8080/tcp", statuses[0].Ports)
	assert.False(t, statuses[1].Running())
}

// 旧版 compose 整段一个数组。CLI 不该因为使用者装的版本不同就瞎掉。
func TestParsePSArray(t *testing.T) {
	out := `[{"Service":"a","State":"running"},{"Service":"b","State":"created"}]`

	statuses, err := parsePS([]byte(out))

	require.NoError(t, err)
	require.Len(t, statuses, 2)
	assert.True(t, statuses[0].Running(), "没有健康检查时，running 就算好")
	assert.False(t, statuses[1].Running())
}

func TestParsePSEmpty(t *testing.T) {
	statuses, err := parsePS([]byte("  \n"))

	require.NoError(t, err)
	assert.Empty(t, statuses)
}

func TestParsePSGarbage(t *testing.T) {
	_, err := parsePS([]byte("{not json}"))

	require.Error(t, err)
	assert.Equal(t, clierr.CodeEngineFailed, clierr.As(err).Code)
}

// running 但 unhealthy 不算好：对使用者来说那个组件并不能用。
func TestRunningRequiresHealthy(t *testing.T) {
	assert.False(t, Status{State: "running", Health: "unhealthy"}.Running())
	assert.False(t, Status{State: "running", Health: "starting"}.Running())
	assert.True(t, Status{State: "running", Health: "healthy"}.Running())
	assert.True(t, Status{State: "running"}.Running())
	assert.False(t, Status{State: "exited"}.Running())
}

// ============================================================
// 其他
// ============================================================

func TestProjectName(t *testing.T) {
	assert.Equal(t, "brickkit-my-erp", ProjectName("my-erp"))
	assert.Equal(t, "brickkit", ProjectName(""), "没有项目名也要有个稳定的值")
}

func TestMissingBinaryGivesInstallHint(t *testing.T) {
	rec := newRecorder()
	rec.fail["up"] = errors.New(`exec: "docker": executable file not found in $PATH`)

	err := dockerWith(rec).Up(context.Background(), UpRequest{File: "f.yaml", Project: "p"})

	require.Error(t, err)
	assert.Equal(t, clierr.CodeEngineMissing, clierr.As(err).Code)
	assert.Contains(t, clierr.As(err).Format(), "Podman")
}

// 引擎失败时把它的输出带上——那才是真正有用的信息。
func TestEngineFailureKeepsOutput(t *testing.T) {
	rec := newRecorder()
	rec.fail["up"] = errors.New("exit 1")
	rec.output["up"] = "\n\nError response from daemon: port is already allocated\n"

	err := dockerWith(rec).Up(context.Background(), UpRequest{File: "f.yaml", Project: "p"})

	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "port is already allocated")
}

// compose 的输出是一长串进度行，**真正的原因在最后**。
//
// 取第一行会得到 "Network xxx Creating" 这种毫无信息量的句子，
// 而"迁移容器 exit 1"那行被丢掉——真跑起来第一次就撞上了。
func TestEngineFailureShowsTheEndOfOutput(t *testing.T) {
	rec := newRecorder()
	rec.fail["up"] = errors.New("exit 1")
	rec.output["up"] = strings.Join([]string{
		" Network brickkit-demo-net  Creating",
		" Container brickkit-demo-postgres-1  Created",
		" Container brickkit-demo-dept-migration-1  Started",
		` Container brickkit-demo-dept-migration-1  Error`,
		`service "dept-migration" didn't complete successfully: exit 1`,
	}, "\n")

	err := dockerWith(rec).Up(context.Background(), UpRequest{File: "f.yaml", Project: "p"})

	require.Error(t, err)
	text := clierr.As(err).Format()
	assert.Contains(t, text, "didn't complete successfully", "真正的原因必须出现")
	assert.NotContains(t, text, "Creating", "开头的进度行没有价值")
}

func TestTailKeepsLastMeaningfulLines(t *testing.T) {
	assert.Equal(t, "c", tail("a\nb\nc\n", 1))
	assert.Equal(t, "b / c", tail("a\nb\nc", 2))
	assert.Equal(t, "only", tail("\n only \n\n", 3))
	assert.Equal(t, "", tail("\n \n", 2))
}
