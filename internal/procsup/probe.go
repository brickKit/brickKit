package procsup

import (
	"context"
	"errors"
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
// 它有一处盲区——分不出端口上的监听者是不是被监管的那个进程。所以调用方启动之前先用 CheckPortFree
// 确认端口空着；万一两步之间端口被抢走，进程退出仍优先于监听：我们的进程因为 "address in use"
// 死掉时，要报退出，不能报成功。
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

// ErrPortAnswering：端口上已经有程序在应答。
var ErrPortAnswering = errors.New("another program already answers on this port")

// CheckPortFree 在启动进程之前确认 port 没被别的程序占着；占着时返回原因。
//
// 两条判据都要：回环地址上已经有人应答（ErrPortAnswering）——这正是 WaitListening 的判据，启动前就有人
// 应答，探测必然把它当成被监管的进程，报出一个假的"已监听"；以及在通配地址上绑不上（返回绑定的错误）——
// 别的程序可能只在别的地址上监听，进程自己绑 ":<port>" 时同样会失败。检查与进程真正绑定之间端口被抢走
// 的竞态仍然存在，由 WaitListening 的"退出优先"兜底。
func CheckPortFree(port int) error {
	if listening(port) {
		return ErrPortAnswering
	}
	l, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return err
	}
	return l.Close()
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
