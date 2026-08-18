package cli

// 本文件测**产物下载并发**（P40）。
//
// # 这条修改推翻了我自己在 Step 36 记下的判断
//
// 当初把 P40 登记为"暂不做"，理由写的是：`CheckImage` 是**延迟受限**（并发有效），
// 而产物下载是**带宽受限**（并发只是把同一条管道切成几份）。
//
// 那个判断是**没有量就下的**。真量之后：
//
//	本项目真实产物大小   443 B – 6.5 KB（11 个文件全部实测）
//	一次往返的等待       30–350 ms
//	6.5 KB 的传输        ~0.3 ms（实测带宽 ~170 Mbit/s）
//
// **延迟主导 100–1000 倍**，和 CheckImage 完全同一形状。产物是 proto、
// openapi.json 这类契约文件，本来就该是小的——"下载"这个词让我先入为主
// 想成了大文件传输。
//
// # 为什么并发在**组件**这一层
//
// 一个组件通常只声明 1–2 个产物文件（本项目 8 个组件全都是），
// 而组件数会到几十。在组件层并发拿到的是接近组件数的倍数；
// 在文件层并发只有 2 倍，还得为两层各设一个上限。

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/resolver"
)

// slowDownloader 记录并发度并让每次下载睡一会。
type slowDownloader struct {
	delay time.Duration

	mu      sync.Mutex
	active  int
	maxSeen int
	calls   []string
}

func (s *slowDownloader) enter(ref string) {
	s.mu.Lock()
	s.active++
	if s.active > s.maxSeen {
		s.maxSeen = s.active
	}
	s.calls = append(s.calls, ref)
	s.mu.Unlock()

	time.Sleep(s.delay)

	s.mu.Lock()
	s.active--
	s.mu.Unlock()
}

func (s *slowDownloader) peak() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxSeen
}

func (s *slowDownloader) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// 下载必须并发，否则组件一多就要等一串串行往返。
func TestArtifactDownloadRunsConcurrently(t *testing.T) {
	const (
		count = 24
		delay = 40 * time.Millisecond
	)
	d := &slowDownloader{delay: delay}
	graph := manyNodeGraph(t, count)

	start := time.Now()
	sum := downloadArtifactsWith(context.Background(), graph,
		func(_ context.Context, node *resolver.Node) artifactOutcome {
			d.enter(node.Ref.ID)
			return artifactOutcome{downloaded: 1}
		})
	elapsed := time.Since(start)

	require.NotNil(t, sum)
	assert.Equal(t, count, d.count(), "P40：每个组件都要下到")

	serial := count * delay
	assert.Less(t, elapsed, serial/2,
		"P40：%d 个组件用了 %v，串行也就是 %v——看起来根本没并发", count, elapsed, serial)
	assert.Greater(t, d.peak(), 1, "P40：任何时刻都只有一个在下，说明是串行的")
}

// 并发要有上限：产物大多来自同一个市场，几十个并发请求只会撞限流。
func TestArtifactDownloadBoundsConcurrency(t *testing.T) {
	d := &slowDownloader{delay: 20 * time.Millisecond}
	graph := manyNodeGraph(t, 100)

	downloadArtifactsWith(context.Background(), graph,
		func(_ context.Context, node *resolver.Node) artifactOutcome {
			d.enter(node.Ref.ID)
			return artifactOutcome{downloaded: 1}
		})

	assert.LessOrEqual(t, d.peak(), artifactConcurrency,
		"P40：并发数超过上限——对市场来说这和 DDoS 没区别")
}

// 计数与每组件的文件数必须准确。
func TestArtifactSummaryCountsAreCorrect(t *testing.T) {
	graph := manyNodeGraph(t, 5)

	sum := downloadArtifactsWith(context.Background(), graph,
		func(context.Context, *resolver.Node) artifactOutcome {
			return artifactOutcome{downloaded: 2, cached: 1}
		})

	assert.Equal(t, 10, sum.downloaded, "P40：并发累加不能丢数")
	assert.Equal(t, 5, sum.cached)
	assert.Len(t, sum.perNode, 5)
	for ref, n := range sum.perNode {
		assert.Equal(t, 3, n, "%s 的文件数不对", ref)
	}
}

// 警告的顺序必须稳定。
//
// 并发天然让完成顺序每次都不一样。直接按完成顺序追加的话，同一次 add 连跑两次
// 会输出顺序不同的警告——使用者会以为发生了不同的事情，
// 而 diff 两次输出也看不出真正的差别。
func TestArtifactWarningsAreDeterministic(t *testing.T) {
	graph := manyNodeGraph(t, 12)

	var first []string
	for i := 0; i < 5; i++ {
		sum := downloadArtifactsWith(context.Background(), graph,
			func(_ context.Context, node *resolver.Node) artifactOutcome {
				return artifactOutcome{warning: node.Ref.ID + " 取不到"}
			})

		got := make([]string, 0, len(sum.warnings))
		for _, w := range sum.warnings {
			got = append(got, w.Message)
		}
		if i == 0 {
			first = got
			continue
		}
		assert.Equal(t, first, got,
			"P40：第 %d 次的警告顺序变了——同一次 add 连跑两次不该给出不同的输出", i+1)
	}
}

// manyNodeGraph 造一个含 n 个互不依赖组件的依赖图。
func manyNodeGraph(t *testing.T, n int) *resolver.Graph {
	t.Helper()

	g := &resolver.Graph{}
	for i := 0; i < n; i++ {
		ref := resolver.Ref{
			ID:      "demo/c" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Version: "1.0.0",
		}
		g.Nodes = append(g.Nodes, &resolver.Node{Ref: ref})
	}
	return g
}
