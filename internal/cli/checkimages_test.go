package cli

// 本文件测**镜像预检的并发**（开发计划 36.1）。
//
// # 为什么这条测试存在
//
// Step 36 量完纯计算层之后，结论是那几层离成为瓶颈差三到六个数量级
// （解析 100 个依赖 42µs，生成 50 个组件的 compose 2.4ms）。
// 真正不随组件数伸缩的只有一处：`checkImages`。
//
// 它对每个镜像调一次 `eng.CheckImage`，而 Docker 那边本地没有该镜像时
// 会走一次 **registry 往返**。原来是串行的，于是：
//
//	50 个组件 × 一次往返 = 50 次串行网络请求
//
// 按健康网络 0.3–0.5 秒一次算，光预检就是 15–25 秒，
// 而计划给整个 `up` 的预算是 30 秒——**第一个容器还没开始启动**。
// 实测这台机器上 registry 路径慢的时候单次要 12 秒，50 个就是十分钟。
//
// 这些检查彼此完全独立，串行没有任何理由。
//
// # 这条测试怎么证明"真的并发了"
//
// 用一个会睡觉的引擎：串行的话总耗时必然 ≥ 个数 × 单次耗时，
// 并发则约等于 个数/并发度 × 单次耗时。两者差一个数量级，
// 所以即使阈值放得很松也不会误判。

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// slowEngine 的 CheckImage 会睡 delay，用来区分串行与并发。
type slowEngine struct {
	*fakeEngine
	delay time.Duration

	mu      sync.Mutex
	seen    []string
	active  int
	maxSeen int // 观察到的最大并发数
}

func newSlowEngine(delay time.Duration) *slowEngine {
	return &slowEngine{fakeEngine: newFakeEngine(), delay: delay}
}

func (s *slowEngine) CheckImage(_ context.Context, image string) error {
	s.mu.Lock()
	s.active++
	if s.active > s.maxSeen {
		s.maxSeen = s.active
	}
	s.seen = append(s.seen, image)
	s.mu.Unlock()

	time.Sleep(s.delay)

	s.mu.Lock()
	s.active--
	s.mu.Unlock()

	return s.fakeEngine.checkErr[image]
}

func (s *slowEngine) checkedImages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.seen...)
}

func (s *slowEngine) peakConcurrency() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxSeen
}

// quietOptions 是一份不往测试输出里写东西的 Options。
func quietOptions() *Options {
	return &Options{Stdout: io.Discard, Stderr: io.Discard}
}

// manyImages 造 n 个待检镜像。
func manyImages(n int) []imageInfo {
	out := make([]imageInfo, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, imageInfo{
			component: "demo/c" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			image:     "registry.example.com/img" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
		})
	}
	return out
}

// 预检必须并发，否则组件一多就把 up 的预算全吃掉。
func TestCheckImagesRunsConcurrently(t *testing.T) {
	const (
		count = 24
		delay = 40 * time.Millisecond
	)
	eng := newSlowEngine(delay)

	start := time.Now()
	err := checkImages(context.Background(), quietOptions(), eng, manyImages(count))
	elapsed := time.Since(start)

	require.NoError(t, err)

	// 串行需要 24×40ms = 960ms。放到 480ms 判失败：
	// 只要真的并发了，实际会在 150ms 上下，离阈值远得很；
	// 卡太紧只会换来一个在负载高时随机红的测试。
	serial := count * delay
	assert.Less(t, elapsed, serial/2,
		"36.1：%d 个镜像用了 %v，串行也就是 %v——看起来根本没并发。"+
			"组件一多，光预检就会吃掉整个 up 的时间预算", count, elapsed, serial)
	assert.Greater(t, eng.peakConcurrency(), 1,
		"36.1：任何时刻都只有一个检查在跑，说明是串行的")
}

// 并发要有上限：一次性对 registry 发几百个请求会被限流，
// 那时候快不了，只会换来一堆 429。
func TestCheckImagesBoundsConcurrency(t *testing.T) {
	eng := newSlowEngine(20 * time.Millisecond)

	require.NoError(t, checkImages(context.Background(), quietOptions(), eng, manyImages(100)))

	assert.LessOrEqual(t, eng.peakConcurrency(), checkImageConcurrency,
		"36.1：并发数超过了上限——对 registry 来说这和 DDoS 没区别，会被限流")
}

// 并发之后每个镜像仍然都要被检查到，一个都不能漏。
func TestCheckImagesChecksEveryImage(t *testing.T) {
	images := manyImages(30)
	eng := newSlowEngine(time.Millisecond)

	require.NoError(t, checkImages(context.Background(), quietOptions(), eng, images))

	checked := eng.checkedImages()
	assert.Len(t, checked, len(images), "36.1：漏检的镜像会在启动时变成 ImagePullBackOff")
	for _, want := range images {
		assert.Contains(t, checked, want.image)
	}
}

// 失败仍然要报出是**哪个组件**——这是这条检查存在的全部意义。
func TestCheckImagesReportsFailingComponent(t *testing.T) {
	images := manyImages(20)
	eng := newSlowEngine(time.Millisecond)
	eng.checkErr = map[string]error{
		images[7].image: errors.New("denied: 需要先 docker login"),
	}

	err := checkImages(context.Background(), quietOptions(), eng, images)

	require.Error(t, err)
	text := clierr.As(err).Format()
	assert.Contains(t, text, images[7].component,
		"36.1：并发之后如果丢了组件名，使用者就只知道'有个镜像拉不到'：%s", text)
}

// 多个都失败时，报的必须是**固定的**那一个。
//
// 并发天然会让"谁先失败"每次都不一样。如果直接报最先返回的那个，
// 同一份配置连跑两次会得到两条不同的错误——使用者会以为问题在飘，
// 修完一个又冒出另一个。按输入顺序取第一个，结果就稳定了。
func TestCheckImagesFailureIsDeterministic(t *testing.T) {
	images := manyImages(20)
	eng := newSlowEngine(time.Millisecond)
	eng.checkErr = map[string]error{
		images[3].image:  errors.New("denied"),
		images[11].image: errors.New("denied"),
		images[17].image: errors.New("denied"),
	}

	for i := 0; i < 5; i++ {
		err := checkImages(context.Background(), quietOptions(), eng, images)
		require.Error(t, err)
		assert.Contains(t, clierr.As(err).Format(), images[3].component,
			"36.1：第 %d 次报的不是输入顺序里第一个失败的——错误信息会在每次运行时变", i+1)
	}
}

// 没有镜像要检查时不该多做任何事。
func TestCheckImagesWithNoImages(t *testing.T) {
	eng := newSlowEngine(time.Second) // 真被调到就会明显变慢

	require.NoError(t, checkImages(context.Background(), quietOptions(), eng, nil))
	assert.Empty(t, eng.checkedImages())
}
