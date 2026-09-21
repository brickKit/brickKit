package procsup

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSupervisor(t *testing.T, opts Options) (*Supervisor, *syncBuffer) {
	t.Helper()
	out := &syncBuffer{}
	opts.Out = out
	sup, err := New(opts)
	require.NoError(t, err)
	t.Cleanup(sup.Shutdown)
	return sup, out
}

func TestExitCrashed(t *testing.T) {
	cases := []struct {
		name string
		exit Exit
		want bool
	}{
		{"自己干净退出", Exit{Code: 0}, false},
		{"自己以非零码退出", Exit{Code: 3}, true},
		{"自己被信号杀死", Exit{Code: -1, Signal: "segmentation fault"}, true},
		{"被我们叫停之后以非零码退出", Exit{Code: 143, Stopped: true}, false},
		{"被我们叫停、被信号杀死", Exit{Code: -1, Signal: "terminated", Stopped: true}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, c.exit.Crashed())
		})
	}
}

func TestRunReturnsOnceEveryProcessHasExited(t *testing.T) {
	sup, out := newSupervisor(t, Options{NameWidth: 5})
	_, err := sup.Start(helperSpec("web", "lines", "2", "w"))
	require.NoError(t, err)

	exits := sup.Run(context.Background())

	require.Len(t, exits, 1)
	assert.Equal(t, "web", exits[0].Name)
	assert.Equal(t, 0, exits[0].Code)
	assert.False(t, exits[0].Crashed())
	assert.Empty(t, exits[0].Signal)
	assert.Equal(t, "web   | w-0\nweb   | w-1\n", out.String())
}

func TestNonZeroExitIsACrashAndKeepsItsLastOutput(t *testing.T) {
	sup, out := newSupervisor(t, Options{})
	p, err := sup.Start(helperSpec("api", "exit", "3", "boom"))
	require.NoError(t, err)

	exits := sup.Run(context.Background())

	require.Len(t, exits, 1)
	assert.Equal(t, 3, exits[0].Code)
	assert.True(t, exits[0].Crashed())
	assert.Equal(t, []string{"boom"}, exits[0].Tail, "崩溃前最后的输出要留在 Exit 里，供排查")
	assert.Equal(t, "api | boom\n", out.String())
	<-p.Done()
	assert.Equal(t, exits[0], p.Exit())
	assert.Equal(t, "api", p.Name())
	assert.Positive(t, p.PID())
}

func TestACrashShutsDownTheWholeSessionAndNamesTheCulprit(t *testing.T) {
	sup, out := newSupervisor(t, Options{})
	_, err := sup.Start(helperSpec("steady", "sleep"))
	require.NoError(t, err)
	waitForOutput(t, out, "steady | ready")
	_, err = sup.Start(helperSpec("crasher", "exit", "1", "boom"))
	require.NoError(t, err)

	exits := sup.Run(context.Background()) // 没有人取消 ctx：崩溃本身就让会话收尾

	require.Len(t, exits, 2)
	steady, crasher := exits[0], exits[1]
	assert.True(t, crasher.Crashed(), "谁崩了：Crashed() 为真的那个")
	assert.Equal(t, 1, crasher.Code)
	assert.Equal(t, []string{"boom"}, crasher.Tail, "崩溃前最后的输出留了下来")
	assert.True(t, steady.Stopped, "没崩的被叫停")
	assert.False(t, steady.Crashed(), "被我们叫停的不算崩溃，否则最后一屏会把受害者当成元凶")
}

// 留多少行由调用方定；不管进程是不是崩了，Tail 都是它最后的那几行。
func TestTailKeepsOnlyTheConfiguredNumberOfLines(t *testing.T) {
	cases := []struct {
		name      string
		tailLines int
		want      []string
	}{
		{"不设就是默认行数", 0, lastLines("x", 30, DefaultTailLines)},
		{"调小", 3, lastLines("x", 30, 3)},
		{"调得比输出还多就是全部", 100, lastLines("x", 30, 30)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sup, _ := newSupervisor(t, Options{TailLines: c.tailLines})
			_, err := sup.Start(helperSpec("x", "lines", "30", "x"))
			require.NoError(t, err)

			exits := sup.Run(context.Background())

			require.Len(t, exits, 1)
			assert.False(t, exits[0].Crashed())
			assert.Equal(t, c.want, exits[0].Tail)
		})
	}
}

// lastLines 是 helper 的 "lines" 模式输出的最后 keep 行。
func lastLines(tag string, total, keep int) []string {
	if keep > total {
		keep = total
	}
	var out []string
	for i := total - keep; i < total; i++ {
		out = append(out, fmt.Sprintf("%s-%d", tag, i))
	}
	return out
}

func TestACleanExitDoesNotStopItsSiblings(t *testing.T) {
	sup, out := newSupervisor(t, Options{})
	quick, err := sup.Start(helperSpec("quick", "exit", "0"))
	require.NoError(t, err)
	_, err = sup.Start(helperSpec("steady", "sleep"))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan []Exit, 1)
	go func() { done <- sup.Run(ctx) }()

	<-quick.Done()
	waitForOutput(t, out, "steady | ready")
	select {
	case <-done:
		t.Fatal("自己干净退出（退出码 0）不是崩溃，Run 不该跟着结束")
	default:
	}
	cancel()
	exits := <-done

	require.Len(t, exits, 2)
	assert.False(t, exits[0].Crashed())
	assert.False(t, exits[0].Stopped)
	assert.True(t, exits[1].Stopped)
}

func TestNothingCanStartOnceASessionHasCrashed(t *testing.T) {
	sup, _ := newSupervisor(t, Options{})
	p, err := sup.Start(helperSpec("crasher", "exit", "2"))
	require.NoError(t, err)
	<-p.Done()

	require.Eventually(t, func() bool {
		sup.mu.Lock()
		defer sup.mu.Unlock()
		return sup.stopping
	}, 5*time.Second, 5*time.Millisecond, "崩溃之后 Supervisor 应该自己进入收尾")
	_, err = sup.Start(helperSpec("late", "exit", "0"))
	assert.ErrorIs(t, err, ErrStopped)
}

func TestStartReportsAMissingBinaryAndRegistersNothing(t *testing.T) {
	sup, _ := newSupervisor(t, Options{})

	_, err := sup.Start(Spec{Name: "ghost", Argv: []string{filepath.Join(t.TempDir(), "nope")}})

	require.Error(t, err)
	assert.ErrorContains(t, err, "ghost")
	assert.ErrorIs(t, err, fs.ErrNotExist, "要能用 errors.Is 认出'文件不存在'")
	assert.Empty(t, sup.Exits())
}

func TestStartRejectsAnEmptyCommand(t *testing.T) {
	sup, _ := newSupervisor(t, Options{})

	_, err := sup.Start(Spec{Name: "blank"})

	assert.ErrorContains(t, err, "blank")
}

func TestStartAfterShutdownIsRefused(t *testing.T) {
	sup, _ := newSupervisor(t, Options{})
	sup.Shutdown()

	_, err := sup.Start(helperSpec("late", "exit", "0"))

	assert.ErrorIs(t, err, ErrStopped)
}

func TestShutdownIsIdempotent(t *testing.T) {
	sup, _ := newSupervisor(t, Options{})
	sup.Shutdown()
	sup.Shutdown()
}

func TestEnvAndDirReachTheChild(t *testing.T) {
	t.Setenv("PROCSUP_INHERITED", "from-parent")
	t.Setenv("PROCSUP_OVERRIDDEN", "old")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	sup, out := newSupervisor(t, Options{})
	inherited := helperSpec("inherited", "env", "PROCSUP_INHERITED")
	inherited.Dir = dir
	_, err = sup.Start(inherited)
	require.NoError(t, err)
	overridden := helperSpec("overridden", "env", "PROCSUP_OVERRIDDEN")
	overridden.Env = append(overridden.Env, "PROCSUP_OVERRIDDEN=new")
	_, err = sup.Start(overridden)
	require.NoError(t, err)

	sup.Run(context.Background())

	got := out.String()
	assert.Contains(t, got, "inherited | PROCSUP_INHERITED=from-parent", "继承父进程的环境")
	assert.Contains(t, got, "inherited | cwd="+dir, "工作目录")
	assert.Contains(t, got, "overridden | PROCSUP_OVERRIDDEN=new", "同名以 Spec.Env 里的为准")
}

func TestOutputOfConcurrentProcessesStaysLineIntact(t *testing.T) {
	sup, out := newSupervisor(t, Options{})
	for _, tag := range []string{"p0", "p1", "p2"} {
		_, err := sup.Start(helperSpec(tag, "lines", "300", tag))
		require.NoError(t, err)
	}

	sup.Run(context.Background())

	lineRe := regexp.MustCompile(`^(p[0-2]) \| (p[0-2])-(\d+)$`)
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	require.Len(t, lines, 900)
	next := map[string]int{}
	for _, line := range lines {
		m := lineRe.FindStringSubmatch(line)
		require.NotNil(t, m, "行被弄坏了：%q", line)
		assert.Equal(t, m[1], m[2], "前缀与内容不是同一个进程的：%q", line)
		n, err := strconv.Atoi(m[3])
		require.NoError(t, err)
		assert.Equal(t, next[m[1]], n, "同一个进程的输出顺序不能乱：%q", line)
		next[m[1]]++
	}
}

func TestStopSignalsAreDefinedEverywhere(t *testing.T) {
	assert.Contains(t, StopSignals, os.Interrupt)
	assert.Len(t, StopSignals, 3)
}
