package procsup

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tailForTest 是这个文件里的测试给 Tail 留的行数。默认行数常量要到 Task 3 才有，这里不依赖它。
const tailForTest = 20

func newPrefixWriter(prefix string) (*prefixWriter, *bytes.Buffer) {
	var out bytes.Buffer
	return &prefixWriter{sink: &lineSink{out: &out}, prefix: prefix, tailCap: tailForTest}, &out
}

func TestPrefixForPadsNamesToWidth(t *testing.T) {
	assert.Equal(t, "web | ", prefixFor("web", 0))
	assert.Equal(t, "web    | ", prefixFor("web", 6))
	assert.Equal(t, "people/basic | ", prefixFor("people/basic", 6), "比宽度长的名字原样保留")
}

func TestPrefixWriterPrefixesEveryLine(t *testing.T) {
	w, out := newPrefixWriter("x | ")

	_, err := w.Write([]byte("a\nb\n"))

	require.NoError(t, err)
	assert.Equal(t, "x | a\nx | b\n", out.String())
}

func TestPrefixWriterJoinsLinesSplitAcrossWrites(t *testing.T) {
	w, out := newPrefixWriter("x | ")

	for _, chunk := range []string{"hel", "lo\nwor", "ld\n"} {
		_, err := w.Write([]byte(chunk))
		require.NoError(t, err)
	}

	assert.Equal(t, "x | hello\nx | world\n", out.String())
}

func TestPrefixWriterFlushEmitsTheUnterminatedTail(t *testing.T) {
	w, out := newPrefixWriter("x | ")
	_, _ = w.Write([]byte("tail"))
	assert.Empty(t, out.String(), "没有换行之前不该输出")

	w.Flush()

	assert.Equal(t, "x | tail\n", out.String())
	w.Flush()
	assert.Equal(t, "x | tail\n", out.String(), "重复 Flush 不该再吐东西")
}

func TestPrefixWriterStripsCarriageReturnAndKeepsBlankLines(t *testing.T) {
	w, out := newPrefixWriter("x | ")

	_, _ = w.Write([]byte("a\r\n\nb\n"))

	assert.Equal(t, "x | a\nx | \nx | b\n", out.String())
}

func TestPrefixWriterSplitsOverlongLines(t *testing.T) {
	w, out := newPrefixWriter("x | ")

	_, _ = w.Write([]byte(strings.Repeat("a", maxLine*2+10) + "\n"))

	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	require.Len(t, lines, 3)
	assert.Len(t, lines[0], len("x | ")+maxLine)
	assert.Len(t, lines[1], len("x | ")+maxLine)
	assert.Len(t, lines[2], len("x | ")+10)
}

func TestPrefixWriterKeepsTheMostRecentLinesForTroubleshooting(t *testing.T) {
	w, _ := newPrefixWriter("x | ")
	for i := 0; i < tailForTest+5; i++ {
		_, _ = w.Write([]byte(fmt.Sprintf("line-%d\n", i)))
	}
	_, _ = w.Write([]byte("unfinished"))
	w.Flush()

	tail := w.Tail()

	require.Len(t, tail, tailForTest, "只留最近的 tailCap 行")
	assert.Equal(t, "line-6", tail[0], "更早的被挤掉了")
	assert.Equal(t, "line-24", tail[len(tail)-2])
	assert.Equal(t, "unfinished", tail[len(tail)-1], "没换行的尾巴 Flush 之后也算一行")
}

// 行数由调用方定。环形缓冲绕了一圈又一圈之后，"最旧的在前"的顺序不能乱。
func TestPrefixWriterTailHonoursItsCapacityAcrossWrapArounds(t *testing.T) {
	cases := []struct {
		name  string
		cap   int
		lines int
		want  []string
	}{
		{"只留最后一行", 1, 4, []string{"l3"}},
		{"没填满", 5, 3, []string{"l0", "l1", "l2"}},
		{"刚好填满", 3, 3, []string{"l0", "l1", "l2"}},
		{"绕过头两圈之后顺序仍然是最旧的在前", 3, 8, []string{"l5", "l6", "l7"}},
		{"容量是 0 就什么都不留", 0, 4, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := &prefixWriter{sink: &lineSink{out: io.Discard}, prefix: "x | ", tailCap: c.cap}
			for i := 0; i < c.lines; i++ {
				_, _ = w.Write([]byte(fmt.Sprintf("l%d\n", i)))
			}

			assert.Equal(t, c.want, w.Tail())
		})
	}
}

func TestPrefixWriterTailIsEmptyForASilentProcessAndIsACopy(t *testing.T) {
	w, _ := newPrefixWriter("x | ")
	assert.Empty(t, w.Tail())

	_, _ = w.Write([]byte("only\n"))
	got := w.Tail()
	got[0] = "tampered"

	assert.Equal(t, []string{"only"}, w.Tail(), "改返回值不能影响内部状态")
}

func TestPrefixWriterNeverFailsTheChild(t *testing.T) {
	w := &prefixWriter{sink: &lineSink{out: failingWriter{}}, prefix: "x | "}

	n, err := w.Write([]byte("hello\n"))

	require.NoError(t, err, "终端写失败不能连累子进程")
	assert.Equal(t, 6, n)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, fmt.Errorf("closed") }

// 并发写同一个 sink：每一行都得完整，前缀不能被别的进程的输出截断。
func TestPrefixWritersSharingASinkNeverInterleaveWithinALine(t *testing.T) {
	var out bytes.Buffer
	sink := &lineSink{out: &out}
	var wg sync.WaitGroup
	for _, name := range []string{"a", "b", "c"} {
		w := &prefixWriter{sink: sink, prefix: name + " | "}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				// 故意把一行切成两次写，逼出"写到一半被别人插队"的可能
				_, _ = w.Write([]byte(fmt.Sprintf("%s-", name)))
				_, _ = w.Write([]byte(fmt.Sprintf("%d\n", i)))
			}
		}()
	}
	wg.Wait()

	lineRe := regexp.MustCompile(`^([abc]) \| ([abc])-\d+$`)
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	assert.Len(t, lines, 600)
	for _, line := range lines {
		m := lineRe.FindStringSubmatch(line)
		require.NotNil(t, m, "行被弄坏了：%q", line)
		assert.Equal(t, m[1], m[2], "前缀与内容不是同一个进程的：%q", line)
	}
}
