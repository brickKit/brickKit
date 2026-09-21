package procsup

import (
	"bytes"
	"fmt"
	"io"
	"sync"
)

// maxLine 是没有换行符时最多攒多长：超过就当一行吐出去，
// 免得一个不带换行的超长输出把内存吃光。
const maxLine = 64 * 1024

// prefixFor 生成一行输出的前缀，形如 "people/basic | "；width 用来把多个组件的前缀对齐。
func prefixFor(name string, width int) string {
	return fmt.Sprintf("%-*s | ", width, name)
}

// lineSink 把所有子进程的输出汇到同一个 io.Writer：一次只写一整行，行与行之间不会穿插。
type lineSink struct {
	mu  sync.Mutex
	out io.Writer
}

func (s *lineSink) writeLine(prefix string, line []byte) {
	buf := make([]byte, 0, len(prefix)+len(line)+1)
	buf = append(buf, prefix...)
	buf = append(buf, line...)
	buf = append(buf, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.out.Write(buf)
}

// prefixWriter 是一个进程的输出端：按行切开，每行加上前缀再交给 lineSink。
//
// Write 永远不返回错误：exec 的拷贝 goroutine 一旦遇到写错误就会停止读管道，
// 子进程随后会在写满管道时被卡死——为了终端上的一行输出把整个进程卡住不划算。
type prefixWriter struct {
	sink   *lineSink
	prefix string

	// tailCap 是 Tail 最多留多少行；<= 0 表示不留。收尾时其余进程的关闭输出会刷屏，
	// 把崩溃的现场冲出屏幕之外，所以要把最后这几行留在 Exit 里，由调用方在最后一屏重新打出来。
	tailCap int

	mu   sync.Mutex
	buf  []byte
	ring []string // 最近的 tailCap 行；满了之后从 next 开始覆盖最旧的
	next int
}

func (w *prefixWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		chunk := p
		if i >= 0 {
			chunk = p[:i]
		}
		w.buf = append(w.buf, chunk...)
		for len(w.buf) > maxLine {
			w.emit(w.buf[:maxLine])
			w.buf = append(w.buf[:0], w.buf[maxLine:]...)
		}
		if i < 0 {
			break
		}
		w.emit(w.buf)
		w.buf = w.buf[:0]
		p = p[i+1:]
	}
	return n, nil
}

// Flush 把还没遇到换行的尾巴当成一行吐出去。进程退出之后调用。
func (w *prefixWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.buf) > 0 {
		w.emit(w.buf)
		w.buf = w.buf[:0]
	}
}

func (w *prefixWriter) emit(line []byte) {
	line = bytes.TrimSuffix(line, []byte("\r"))
	w.remember(string(line))
	w.sink.writeLine(w.prefix, line)
}

// remember 把一行记进环形缓冲：满了就覆盖最旧的那一行。每行 O(1)——行数调得再大，
// 也不会因为每来一行就整体挪一遍而拖慢输出。
func (w *prefixWriter) remember(line string) {
	if w.tailCap <= 0 {
		return
	}
	if len(w.ring) < w.tailCap {
		w.ring = append(w.ring, line)
		return
	}
	w.ring[w.next] = line
	w.next = (w.next + 1) % w.tailCap
}

// Tail 返回最近输出的几行（不带前缀，最旧的在前），最多 tailCap 行。返回的是副本。
func (w *prefixWriter) Tail() []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	out := make([]string, 0, len(w.ring))
	out = append(out, w.ring[w.next:]...) // 没满时 next 是 0，这一段就是全部
	return append(out, w.ring[:w.next]...)
}
