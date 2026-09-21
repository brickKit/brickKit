package procsup

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 这个包的测试要真的启动子进程。做法是让测试二进制自己当子进程：
// helperEnv 一置位，TestMain 就不跑测试，而是按第一个参数扮演某种行为的子进程。
// 这样不依赖 sh、sleep 这类外部命令，Windows 上也能用。
const helperEnv = "PROCSUP_TEST_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		runHelper(os.Args[1:])
		return
	}
	os.Exit(m.Run())
}

func runHelper(args []string) {
	switch args[0] {
	case "lines": // lines <n> <tag>：打印 n 行 "<tag>-<i>"
		n, _ := strconv.Atoi(args[1])
		for i := 0; i < n; i++ {
			fmt.Printf("%s-%d\n", args[2], i)
		}
	case "exit": // exit <code> [text]
		if len(args) > 2 {
			fmt.Println(args[2])
		}
		code, _ := strconv.Atoi(args[1])
		os.Exit(code)
	case "env": // env <KEY>：打印这个环境变量与工作目录
		fmt.Printf("%s=%s\n", args[1], os.Getenv(args[1]))
		wd, _ := os.Getwd()
		fmt.Println("cwd=" + wd)
	case "sleep":
		fmt.Println("ready")
		time.Sleep(time.Hour)
	case "ignore-term": // 装作没听见 SIGTERM，逼 Supervisor 走强杀
		signal.Ignore(syscall.SIGTERM)
		fmt.Println("ready")
		time.Sleep(time.Hour)
	case "grandchild": // 拉起一个继承 stdout 的孙进程，打印它的 pid，然后自己等着
		fmt.Printf("grandchild %d\n", startGrandchild())
		fmt.Println("ready")
		time.Sleep(time.Hour)
	case "orphan": // 拉起孙进程之后自己立刻退出，把孙进程留成孤儿
		fmt.Printf("grandchild %d\n", startGrandchild())
	case "supervise": // 扮演前台会话：监管一个 sleep 子进程，收到 StopSignals 就收尾
		superviseOneChild()
	default:
		fmt.Fprintln(os.Stderr, "unknown helper mode:", args[0])
		os.Exit(2)
	}
}

func superviseOneChild() {
	ctx, stop := signal.NotifyContext(context.Background(), StopSignals...)
	defer stop()
	sup, err := New(Options{Out: os.Stdout})
	if err != nil {
		fmt.Println("new failed:", err)
		os.Exit(2)
	}
	p, err := sup.Start(helperSpec("child", "sleep"))
	if err != nil {
		fmt.Println("start failed:", err)
		os.Exit(2)
	}
	fmt.Printf("child %d\n", p.PID())
	sup.Run(ctx)
	fmt.Println("session over")
}

func startGrandchild() int {
	gc := exec.Command(os.Args[0], "sleep")
	gc.Env = append(os.Environ(), helperEnv+"=1")
	gc.Stdout = os.Stdout
	if err := gc.Start(); err != nil {
		fmt.Println("grandchild failed:", err)
		os.Exit(2)
	}
	return gc.Process.Pid
}

func helperSpec(name string, args ...string) Spec {
	return Spec{Name: name, Argv: append([]string{os.Args[0]}, args...), Env: []string{helperEnv + "=1"}}
}

// syncBuffer 是可以一边被 Supervisor 写、一边被测试读的缓冲。
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// waitForOutput 等输出里出现某段文字。
func waitForOutput(t *testing.T, out *syncBuffer, want string) {
	t.Helper()
	require.Eventually(t, func() bool { return strings.Contains(out.String(), want) },
		5*time.Second, 10*time.Millisecond, "等不到输出 %q，目前的输出：\n%s", want, out.String())
}
