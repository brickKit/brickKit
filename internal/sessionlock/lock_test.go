package sessionlock

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 跨进程的测试让测试二进制自己当"另一个会话"：helperEnv 一置位，TestMain 就去拿锁并一直占着。
const helperEnv = "SESSIONLOCK_TEST_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		if _, err := Acquire(os.Args[1]); err != nil {
			fmt.Println("acquire failed:", err)
			os.Exit(2)
		}
		fmt.Println("held")
		time.Sleep(time.Hour)
		return
	}
	os.Exit(m.Run())
}

func lockPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "session.lock")
}

func TestAcquireLeavesTheHoldersInfoForOtherTerminals(t *testing.T) {
	path := lockPath(t)
	l, err := Acquire(path)
	require.NoError(t, err)
	defer func() { _ = l.Release() }()

	info, held, err := Inspect(path)

	require.NoError(t, err)
	assert.True(t, held)
	assert.Equal(t, os.Getpid(), info.PID)
	assert.WithinDuration(t, time.Now(), info.Started, time.Minute)
}

func TestASecondAcquireIsRefusedWhileTheLockIsHeld(t *testing.T) {
	path := lockPath(t)
	l, err := Acquire(path)
	require.NoError(t, err)
	defer func() { _ = l.Release() }()

	_, err = Acquire(path)

	var held *HeldError
	require.ErrorAs(t, err, &held)
	assert.Equal(t, os.Getpid(), held.Info.PID, "报错里要带着持有者是谁")
	assert.ErrorContains(t, err, fmt.Sprint(os.Getpid()))
}

func TestReleaseFreesTheLockAndClearsTheHint(t *testing.T) {
	path := lockPath(t)
	l, err := Acquire(path)
	require.NoError(t, err)

	require.NoError(t, l.Release())

	_, held, err := Inspect(path)
	require.NoError(t, err)
	assert.False(t, held)
	st, err := os.Stat(path)
	require.NoError(t, err, "锁文件永远不删")
	assert.Zero(t, st.Size(), "提示信息要清掉，免得留着一个过期的 PID")
	l2, err := Acquire(path)
	require.NoError(t, err, "释放之后能再拿")
	require.NoError(t, l2.Release())
}

func TestInspectOfAMissingFileIsNotHeld(t *testing.T) {
	info, held, err := Inspect(lockPath(t))

	require.NoError(t, err)
	assert.False(t, held)
	assert.Zero(t, info)
}

func TestInspectDoesNotLeaveTheLockTaken(t *testing.T) {
	path := lockPath(t)
	l, err := Acquire(path)
	require.NoError(t, err)
	require.NoError(t, l.Release())

	_, _, err = Inspect(path)
	require.NoError(t, err)

	l, err = Acquire(path)
	require.NoError(t, err, "探测完锁必须还是空闲的")
	require.NoError(t, l.Release())
}

func TestAcquireFailsCleanlyWhenTheDirectoryCannotBeCreated(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, nil, 0o644))

	_, err := Acquire(filepath.Join(blocker, "sub", "session.lock"))

	require.Error(t, err)
	var held *HeldError
	assert.NotErrorAs(t, err, &held, "建不出目录不是'被别人持有'")
}

func TestAcquireFailsCleanlyWhenThePathIsADirectory(t *testing.T) {
	_, err := Acquire(t.TempDir())

	require.Error(t, err)
	var held *HeldError
	assert.NotErrorAs(t, err, &held)
}

func TestAcquireCreatesMissingDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "session.lock")

	l, err := Acquire(path)

	require.NoError(t, err)
	require.NoError(t, l.Release())
}

// 锁的生死跟持有者进程绑在一起：进程被 kill -9 之后，文件里还留着它的 PID，
// 但锁已经被内核释放了——判断有没有会话看的是锁，不是那个 PID。
func TestALockHeldByAnotherProcessDiesWithIt(t *testing.T) {
	path := lockPath(t)
	cmd := exec.Command(os.Args[0], path)
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "held\n", line)

	info, held, err := Inspect(path)
	require.NoError(t, err)
	assert.True(t, held)
	assert.Equal(t, cmd.Process.Pid, info.PID, "看得到另一个进程持有着")
	_, err = Acquire(path)
	var heldErr *HeldError
	require.ErrorAs(t, err, &heldErr)
	assert.Equal(t, cmd.Process.Pid, heldErr.Info.PID)

	require.NoError(t, cmd.Process.Kill())
	_ = cmd.Wait()

	_, held, err = Inspect(path)
	require.NoError(t, err)
	assert.False(t, held, "持有者死了，锁就该空出来")
	l, err := Acquire(path)
	require.NoError(t, err)
	require.NoError(t, l.Release())
}
