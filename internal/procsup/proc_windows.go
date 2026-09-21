//go:build windows

package procsup

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// sysState 持有一个 Job Object：所有子进程（连同它们后来拉起的孙进程）都在里面，
// 关闭它、或者 brickkit 自己死掉，整棵进程树一起被系统收走。
type sysState struct{ job windows.Handle }

func newSysState() (*sysState, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return &sysState{job: job}, nil
}

// configure 给子进程一个新的进程组，这样才能只对它发 CTRL_BREAK_EVENT。
func (*sysState) configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// attach 把刚启动的进程放进 Job Object。
//
// 进程是先启动、后入 Job 的（os/exec 没法以挂起状态创建进程），所以启动到入 Job 之间
// 那几微秒里它拉起的孙进程会逃出 Job。这是已知的缺口：真实场景里启动器不会在第一微秒就 fork。
func (st *sysState) attach(cmd *exec.Cmd) error {
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	return windows.AssignProcessToJobObject(st.job, h)
}

// terminate 给每个进程组发 CTRL_BREAK_EVENT，相当于在控制台里对它按 Ctrl+Break。
// 没有控制台可共享时发不出去，忽略即可：GracePeriod 之后 kill 会兜底。
func (*sysState) terminate(procs []*Proc) {
	for _, p := range procs {
		_ = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(p.pid))
	}
}

// kill 直接终止整个 Job，里面所有进程一起死。
func (st *sysState) kill([]*Proc) { _ = windows.TerminateJobObject(st.job, 1) }

func (st *sysState) close() { _ = windows.CloseHandle(st.job) }

func signalName(*os.ProcessState) string { return "" }
