package procsup

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// freePort 找一个此刻没人监听的端口。
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

func listenOn(t *testing.T, addr string) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func TestWaitListeningSeesAnExistingListener(t *testing.T) {
	l := listenOn(t, "127.0.0.1:0")

	got := WaitListening(context.Background(), l.Addr().(*net.TCPAddr).Port, nil, time.Second)

	assert.Equal(t, ProbeListening, got)
}

func TestWaitListeningSeesAListenerThatStartsLater(t *testing.T) {
	port := freePort(t)
	started := make(chan net.Listener, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			l = nil
		}
		started <- l
	}()

	got := WaitListening(context.Background(), port, nil, 3*time.Second)

	assert.Equal(t, ProbeListening, got)
	if l := <-started; l != nil {
		_ = l.Close()
	}
}

func TestWaitListeningTimesOutWhenNothingListens(t *testing.T) {
	started := time.Now()

	got := WaitListening(context.Background(), freePort(t), nil, 300*time.Millisecond)

	assert.Equal(t, ProbeTimedOut, got)
	assert.GreaterOrEqual(t, time.Since(started), 300*time.Millisecond)
}

func TestWaitListeningReportsAProcessThatExited(t *testing.T) {
	exited := make(chan struct{})
	go func() {
		time.Sleep(150 * time.Millisecond)
		close(exited)
	}()

	got := WaitListening(context.Background(), freePort(t), exited, 5*time.Second)

	assert.Equal(t, ProbeExited, got)
}

// 端口被别的东西占着、我们的进程却因为 "address in use" 死了：得报退出，不能被那个占着端口的东西骗过去。
func TestWaitListeningPrefersExitOverAForeignListener(t *testing.T) {
	l := listenOn(t, "127.0.0.1:0")
	exited := make(chan struct{})
	close(exited)

	got := WaitListening(context.Background(), l.Addr().(*net.TCPAddr).Port, exited, time.Second)

	assert.Equal(t, ProbeExited, got)
}

func TestWaitListeningStopsWhenTheContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	got := WaitListening(ctx, freePort(t), nil, 5*time.Second)

	assert.Equal(t, ProbeCanceled, got)
}

func TestWaitListeningAlsoSeesAnIPv6OnlyListener(t *testing.T) {
	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("这台机器没有 IPv6 回环：%v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	got := WaitListening(context.Background(), l.Addr().(*net.TCPAddr).Port, nil, time.Second)

	assert.Equal(t, ProbeListening, got)
}
