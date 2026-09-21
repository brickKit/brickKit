package procsup

import (
	"context"
	"net"
	"strconv"
	"time"
)

// ProbeResult 是 WaitListening 的结论。
type ProbeResult int

const (
	// ProbeListening：端口上有东西在监听了。
	ProbeListening ProbeResult = iota
	// ProbeExited：等的过程中进程退出了。"命令没跑起来"，是硬失败。
	ProbeExited
	// ProbeTimedOut：进程还在跑，但到时间了端口还没监听。只是警告的理由：慢启动是正常情况。
	ProbeTimedOut
	// ProbeCanceled：ctx 被取消了（通常是用户按了 Ctrl+C）。
	ProbeCanceled
)

const (
	probeInterval    = 100 * time.Millisecond
	probeDialTimeout = 200 * time.Millisecond
)

// WaitListening 等本机的 port 上出现监听，最多等 timeout；exited 是被监管进程的 Proc.Done()，
// 它先关闭就说明进程已经退出了，不必再等。
//
// 这只是一次性的"命令跑起来了吗"检测，不是健康检查：不发请求、不看响应，连上就算。
// 它有一处盲区——分不出端口上的监听者是不是被监管的那个进程。所以进程退出优先于监听：
// 端口被别的东西占着、而我们的进程因为 "address in use" 死掉时，要报退出，不能报成功。
func WaitListening(ctx context.Context, port int, exited <-chan struct{}, timeout time.Duration) ProbeResult {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(probeInterval)
	defer tick.Stop()

	for {
		select {
		case <-exited:
			return ProbeExited
		default:
		}
		if listening(port) {
			return ProbeListening
		}
		select {
		case <-exited:
			return ProbeExited
		case <-ctx.Done():
			return ProbeCanceled
		case <-deadline.C:
			return ProbeTimedOut
		case <-tick.C:
		}
	}
}

// listening 试着连一下 IPv4 与 IPv6 的回环地址：进程可能只绑了其中一个。
func listening(port int) bool {
	for _, host := range []string{"127.0.0.1", "::1"} {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), probeDialTimeout)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
}
