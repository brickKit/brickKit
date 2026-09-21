//go:build unix

package procsup

import (
	"os"
	"os/exec"
	"syscall"
)

// sysState 是平台相关的状态。Unix 上不需要任何东西：进程组本身就是收尾的单位。
type sysState struct{}

func newSysState() (*sysState, error) { return &sysState{}, nil }

// configure 让子进程自成一个进程组（pgid == pid），信号才能发给整组：
// `go run`、`npm run dev` 这类"启动器"会再拉起真正的服务进程，只给启动器发信号收不干净。
// 副作用是终端的 Ctrl+C 不再直接打到子进程——收尾完全由 Supervisor 转发。
func (*sysState) configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func (*sysState) attach(*exec.Cmd) error { return nil }

// terminate 请每个进程组体面地退出。组已经没了（ESRCH）不算错。
func (*sysState) terminate(procs []*Proc) { signalGroups(procs, syscall.SIGTERM) }

// kill 强杀每个进程组。
func (*sysState) kill(procs []*Proc) { signalGroups(procs, syscall.SIGKILL) }

func (*sysState) close() {}

func signalGroups(procs []*Proc, sig syscall.Signal) {
	for _, p := range procs {
		_ = syscall.Kill(-p.pid, sig)
	}
}

func signalName(ps *os.ProcessState) string {
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return ws.Signal().String()
	}
	return ""
}
