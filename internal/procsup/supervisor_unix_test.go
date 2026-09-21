//go:build unix

package procsup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// processAlive 判断一个进程是不是还活着。僵尸进程（已经死了、只是没人收尸）算死的：
// 测试环境里 1 号进程不一定会回收孤儿。
func processAlive(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true // 没有 /proc（macOS）：分不出僵尸，按活着算
	}
	return !strings.Contains(string(stat), ") Z")
}

func requireGone(t *testing.T, pid int) {
	t.Helper()
	require.Eventually(t, func() bool { return !processAlive(pid) },
		5*time.Second, 20*time.Millisecond, "进程 %d 还活着", pid)
}

var grandchildRe = regexp.MustCompile(`grandchild (\d+)`)

func grandchildPID(t *testing.T, out *syncBuffer) int {
	t.Helper()
	waitForOutput(t, out, "grandchild ")
	m := grandchildRe.FindStringSubmatch(out.String())
	require.NotNil(t, m)
	pid, err := strconv.Atoi(m[1])
	require.NoError(t, err)
	return pid
}

// runUntilReady 启动 Run，等到输出里出现 "ready"，返回取消函数与拿结果的通道。
func runUntilReady(t *testing.T, sup *Supervisor, out *syncBuffer) (cancel func(), result <-chan []Exit) {
	t.Helper()
	ctx, cancelFn := context.WithCancel(context.Background())
	done := make(chan []Exit, 1)
	go func() { done <- sup.Run(ctx) }()
	waitForOutput(t, out, "ready")
	return cancelFn, done
}

func awaitExits(t *testing.T, result <-chan []Exit) []Exit {
	t.Helper()
	select {
	case exits := <-result:
		return exits
	case <-time.After(15 * time.Second):
		t.Fatal("Run 没有及时返回")
		return nil
	}
}

func TestShutdownAsksPolitelyFirst(t *testing.T) {
	sup, out := newSupervisor(t, Options{})
	_, err := sup.Start(helperSpec("svc", "sleep"))
	require.NoError(t, err)
	cancel, result := runUntilReady(t, sup, out)

	cancel()
	exits := awaitExits(t, result)

	require.Len(t, exits, 1)
	assert.True(t, exits[0].Stopped)
	assert.False(t, exits[0].Crashed(), "被我们叫停的不算崩溃")
	assert.Equal(t, "terminated", exits[0].Signal, "先发的是 SIGTERM")
	assert.Equal(t, -1, exits[0].Code)
}

// 被外部信号杀死（比如内存不够被 OOM killer 干掉）同样是崩溃：不是我们叫它停的，也不是干净退出。
func TestAProcessKilledBySomeoneElseCountsAsACrash(t *testing.T) {
	sup, out := newSupervisor(t, Options{})
	victim, err := sup.Start(helperSpec("victim", "sleep"))
	require.NoError(t, err)
	_, err = sup.Start(helperSpec("bystander", "sleep"))
	require.NoError(t, err)
	done := make(chan []Exit, 1)
	go func() { done <- sup.Run(context.Background()) }()
	waitForOutput(t, out, "victim | ready")
	waitForOutput(t, out, "bystander | ready")

	require.NoError(t, syscall.Kill(victim.PID(), syscall.SIGKILL))

	exits := awaitExits(t, done)
	require.Len(t, exits, 2)
	assert.True(t, exits[0].Crashed())
	assert.Equal(t, "killed", exits[0].Signal)
	assert.Equal(t, -1, exits[0].Code)
	assert.True(t, exits[1].Stopped, "旁观者被叫停，会话结束")
	assert.False(t, exits[1].Crashed())
}

func TestShutdownEscalatesToKillWhenTheProcessIgnoresTerm(t *testing.T) {
	sup, out := newSupervisor(t, Options{GracePeriod: 200 * time.Millisecond})
	_, err := sup.Start(helperSpec("stubborn", "ignore-term"))
	require.NoError(t, err)
	cancel, result := runUntilReady(t, sup, out)

	started := time.Now()
	cancel()
	exits := awaitExits(t, result)

	require.Len(t, exits, 1)
	assert.Equal(t, "killed", exits[0].Signal)
	assert.True(t, exits[0].Stopped)
	assert.GreaterOrEqual(t, time.Since(started), 200*time.Millisecond, "得先给它宽限期")
}

func TestShutdownKillsTheWholeProcessTree(t *testing.T) {
	sup, out := newSupervisor(t, Options{})
	_, err := sup.Start(helperSpec("launcher", "grandchild"))
	require.NoError(t, err)
	cancel, result := runUntilReady(t, sup, out)
	pid := grandchildPID(t, out)
	require.True(t, processAlive(pid), "前提：孙进程此刻还活着")

	cancel()
	awaitExits(t, result)

	requireGone(t, pid)
}

func TestAnOrphanLeftBehindByAnExitedLauncherIsCleanedUp(t *testing.T) {
	oldDelay := waitDelay
	waitDelay = 200 * time.Millisecond
	t.Cleanup(func() { waitDelay = oldDelay })
	sup, out := newSupervisor(t, Options{})
	_, err := sup.Start(helperSpec("launcher", "orphan"))
	require.NoError(t, err)

	exits := sup.Run(context.Background())

	require.Len(t, exits, 1)
	assert.Equal(t, 0, exits[0].Code, "启动器自己是干净退出的")
	requireGone(t, grandchildPID(t, out))
}

// 前台会话跑在另一个进程里，用真的信号去停它：Ctrl+C（SIGINT）、被 kill（SIGTERM）、
// 终端被关掉（SIGHUP）。三种都得把子进程一起带走，并且让会话自己体面地收尾。
func TestStopSignalsEndTheSessionAndTakeTheChildrenWithIt(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			session := exec.Command(os.Args[0], "supervise")
			session.Env = append(os.Environ(), helperEnv+"=1")
			out := &syncBuffer{}
			session.Stdout = out
			require.NoError(t, session.Start())
			t.Cleanup(func() { _ = session.Process.Kill(); _ = session.Wait() })
			waitForOutput(t, out, "child ")
			waitForOutput(t, out, "child | ready")
			m := childRe.FindStringSubmatch(out.String())
			require.NotNil(t, m)
			childPID, err := strconv.Atoi(m[1])
			require.NoError(t, err)
			require.True(t, processAlive(childPID))

			require.NoError(t, session.Process.Signal(sig))

			waitForOutput(t, out, "session over")
			require.NoError(t, session.Wait())
			requireGone(t, childPID)
		})
	}
}

var childRe = regexp.MustCompile(`(?m)^child (\d+)$`)
