// Package procsup 在前台监管一组裸进程——不经过容器引擎，由 brickkit 自己启动、自己收尾。
//
// 它只管四件事：把每个进程放进独立的进程组（Windows 是 Job Object），让整棵进程树能一起收掉；
// 把所有进程的 stdout/stderr 按行加上名字前缀，汇到同一个 io.Writer；收尾时先请进程体面地停，
// 超时再强杀；把每个进程的退出情况交给调用方。
//
// 它刻意不做的：重启、健康检查、跨会话存活。进程的生命周期严格等于持有 Supervisor 的这段前台会话
// ——所以不需要 PID 文件、也不需要后台服务。
//
// 一个进程崩了（不是我们叫它停的、退出码非零，含被外部信号杀死），整个会话随即收尾：其余进程被体面地停掉。
// 崩溃的现场留在 Exit 里——退出码、信号、跑了多久，以及它最后的若干行输出（行数由 Options.TailLines 定）
// ——调用方在 Run 返回之后，只需要把 Crashed() 为真的那几个打在最后一屏，不会被收尾时其余进程的输出冲走。
package procsup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// DefaultGracePeriod 是 Shutdown 请进程体面退出之后，等多久才动手强杀。
	DefaultGracePeriod = 5 * time.Second
	// DefaultTailLines 是每个进程默认留多少行最近的输出。
	DefaultTailLines = 20
)

// StopSignals 是前台会话应该当作"停"的信号：Ctrl+C、被 kill、终端被关掉。
// 这三个常量在各平台上都有定义。
var StopSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

var (
	// 子进程退出之后，最多再等这么久让输出管道排空。孙进程如果继承了 stdout 又活得比子进程久，
	// 不设上限的话 cmd.Wait 会一直挂着。
	waitDelay = 2 * time.Second
	// 强杀之后最多再等这么久：SIGKILL 杀不掉的只有卡在内核里的进程，不能因此让 Ctrl+C 永远挂着。
	killWait = 5 * time.Second
)

// ErrStopped 表示 Supervisor 已经在收尾，不再接受新进程。
var ErrStopped = errors.New("procsup: supervisor is shutting down")

// Spec 描述要启动的一个进程。
type Spec struct {
	// Name 用作输出前缀与 Exit.Name，通常是组件 ID。
	Name string
	// Argv[0] 是可执行文件（按 PATH 查找，含路径分隔符时相对 Dir），其余是参数。不经过 shell。
	Argv []string
	// Dir 是工作目录；空表示继承当前目录。
	Dir string
	// Env 是 "KEY=VALUE" 列表，追加在当前进程的环境之后，同名以后者为准。
	// 值只经由 exec.Cmd.Env 在内存里传给子进程，不会落盘。
	Env []string
}

// Options 配置 Supervisor。
type Options struct {
	// Out 接收所有进程带前缀的输出；nil 表示丢弃。
	Out io.Writer
	// NameWidth 把前缀里的名字补齐到这个宽度，多个进程的输出才对得齐；0 表示不补。
	NameWidth int
	// GracePeriod 见 DefaultGracePeriod；<= 0 时取默认值。
	GracePeriod time.Duration
	// TailLines 是每个进程留多少行最近的输出（Exit.Tail），排查崩溃用；<= 0 时取 DefaultTailLines。
	// 内存占用只取决于进程实际输出了多少行，不会预先按这个数分配。
	TailLines int
}

// Exit 是一个进程的退出情况。
type Exit struct {
	Name string
	PID  int
	// Code 是退出码；被信号杀死时为 -1。
	Code int
	// Signal 是杀死它的信号名（"terminated"、"killed"……），没被信号杀死时为空；Windows 上恒为空。
	Signal string
	// Stopped 表示这个进程是 Shutdown 要求它停的，而不是自己先退的。
	Stopped bool
	// Duration 是从启动到退出的时长。
	Duration time.Duration
	// Tail 是它最后的若干行输出（stdout 与 stderr 合并，不带前缀，最旧的在前），
	// 最多 Options.TailLines 行，排查崩溃用。
	Tail []string
}

// Crashed 表示进程自己异常退出了：不是我们叫它停的，而且退出码非零（含被信号杀死）。
func (e Exit) Crashed() bool { return !e.Stopped && e.Code != 0 }

// Proc 是一个已经启动的进程。
type Proc struct {
	name    string
	pid     int
	started time.Time

	stopping atomic.Bool
	done     chan struct{}
	exit     Exit // 只在 done 关闭之后才可读
}

// Name 返回 Spec.Name。
func (p *Proc) Name() string { return p.name }

// PID 返回进程号。
func (p *Proc) PID() int { return p.pid }

// Done 在进程退出（并且输出排空或等待超时）之后关闭。
func (p *Proc) Done() <-chan struct{} { return p.done }

// Exit 返回退出情况；只有 Done 关闭之后才有意义。
func (p *Proc) Exit() Exit { return p.exit }

// Supervisor 监管一组进程。零值不可用，用 New 创建。
type Supervisor struct {
	opts Options
	sink *lineSink
	sys  *sysState

	mu       sync.Mutex
	procs    []*Proc
	stopping bool

	once sync.Once
}

// New 创建一个 Supervisor。用完必须调用 Shutdown（Windows 上它还负责释放 Job Object）。
func New(opts Options) (*Supervisor, error) {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}
	sys, err := newSysState()
	if err != nil {
		return nil, fmt.Errorf("procsup: %w", err)
	}
	if opts.TailLines <= 0 {
		opts.TailLines = DefaultTailLines
	}
	return &Supervisor{opts: opts, sink: &lineSink{out: out}, sys: sys}, nil
}

// Start 启动一个进程并立刻返回。进程的输出随即开始流向 Options.Out。
func (s *Supervisor) Start(spec Spec) (*Proc, error) {
	if len(spec.Argv) == 0 {
		return nil, fmt.Errorf("procsup: %s has an empty command", spec.Name)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return nil, ErrStopped
	}

	cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.WaitDelay = waitDelay
	w := &prefixWriter{sink: s.sink, prefix: prefixFor(spec.Name, s.opts.NameWidth), tailCap: s.opts.TailLines}
	cmd.Stdout, cmd.Stderr = w, w // 同一个 Writer：一根管道，stdout 与 stderr 的先后顺序不会被打乱
	s.sys.configure(cmd)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("procsup: start %s: %w", spec.Name, err)
	}
	if err := s.sys.attach(cmd); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("procsup: attach %s: %w", spec.Name, err)
	}

	p := &Proc{name: spec.Name, pid: cmd.Process.Pid, started: time.Now(), done: make(chan struct{})}
	s.procs = append(s.procs, p)
	go s.watch(p, cmd, w)
	return p, nil
}

func (s *Supervisor) watch(p *Proc, cmd *exec.Cmd, w *prefixWriter) {
	_ = cmd.Wait() // *ExitError、ErrWaitDelay 都不影响下面从 ProcessState 读到的退出情况
	w.Flush()

	code, signal := -1, ""
	if ps := cmd.ProcessState; ps != nil {
		code, signal = ps.ExitCode(), signalName(ps)
	}
	p.exit = Exit{
		Name: p.name, PID: p.pid, Code: code, Signal: signal,
		Stopped: p.stopping.Load(), Duration: time.Since(p.started), Tail: w.Tail(),
	}
	close(p.done)

	// 一个崩了，整个会话收尾。用 goroutine 是因为 Shutdown 要等所有进程的 Done，包括我们自己。
	if p.exit.Crashed() {
		go s.Shutdown()
	}
}

// Run 阻塞到所有已启动的进程都退出，或者 ctx 被取消——取消时先 Shutdown 再返回。
// 返回全部进程的退出情况，按启动顺序。Run 期间不要再 Start。
//
// 有进程崩了，Supervisor 会自己收尾（见包注释），Run 随之返回：返回值里 Crashed() 为真的就是"谁崩了"，
// 其余的是被叫停的（Stopped）。自己干净退出（退出码 0）的进程不算崩溃，也不会连累别的进程。
func (s *Supervisor) Run(ctx context.Context) []Exit {
	s.mu.Lock()
	procs := append([]*Proc(nil), s.procs...)
	s.mu.Unlock()

	all := make(chan struct{})
	go func() {
		for _, p := range procs {
			<-p.done
		}
		close(all)
	}()

	select {
	case <-all:
	case <-ctx.Done():
	}
	s.Shutdown()
	return s.Exits()
}

// Shutdown 收尾：先请每个进程组体面地退出，等 GracePeriod，还没走的强杀，最后释放平台资源。
// 可以重复调用，也可以并发调用；后到的调用者会等到收尾完成。
func (s *Supervisor) Shutdown() { s.once.Do(s.shutdown) }

func (s *Supervisor) shutdown() {
	s.mu.Lock()
	s.stopping = true
	procs := append([]*Proc(nil), s.procs...)
	s.mu.Unlock()

	for _, p := range procs {
		p.stopping.Store(true)
	}
	s.sys.terminate(procs)

	grace := s.opts.GracePeriod
	if grace <= 0 {
		grace = DefaultGracePeriod
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
wait:
	for _, p := range procs {
		select {
		case <-p.done:
		case <-timer.C:
			break wait
		}
	}

	// 无条件强杀一遍：进程组的领头进程可能已经走了，组里却还留着不理睬 SIGTERM 的孙进程。
	s.sys.kill(procs)

	deadline := time.NewTimer(killWait)
	defer deadline.Stop()
	for _, p := range procs {
		select {
		case <-p.done:
		case <-deadline.C:
			s.sys.close()
			return
		}
	}
	s.sys.close()
}

// Printf 把一行格式化文本写进 Options.Out，跟任何一个子进程的输出行共用同一把锁——
// 调用方（比如前台监管循环里要插的"正在监听端口"这类状态行）需要在别的进程还在跑、
// 还在往同一个 Out 写东西的时候插自己的行时用这个，不要绕开 Supervisor 直接写 Options.Out。
//
// 直接写的问题：Options.Out 常常是测试里的 *bytes.Buffer 这类非并发安全的 io.Writer，
// prefixWriter 的输出拷贝协程和调用方各自的 Write 调用之间没有任何同步，是一处真实的
// 数据竞争（brickKit 反馈：跑一条用真实 Node/npm 的端到端测试时 -race 抓到）。生产环境下
// Out 通常是 os.Stdout，竞争不会导致崩溃，但同样会把两行输出交叉打乱。
func (s *Supervisor) Printf(format string, args ...any) {
	s.sink.mu.Lock()
	defer s.sink.mu.Unlock()
	_, _ = fmt.Fprintf(s.sink.out, format, args...)
}

// Exits 返回已经退出的进程的退出情况，按启动顺序。
func (s *Supervisor) Exits() []Exit {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []Exit
	for _, p := range s.procs {
		select {
		case <-p.done:
			out = append(out, p.exit)
		default:
		}
	}
	return out
}
