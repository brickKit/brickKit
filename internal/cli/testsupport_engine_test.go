package cli

import (
	"context"
	"sync"
	"testing"

	"github.com/brickkit/brickkit/internal/engine"
)

// fakeEngine 是一个记录调用的假引擎。
//
// 命令层的职责是"决定谁该启动、按什么顺序、先检查什么"，
// 而不是"怎么调 docker"。把引擎换成假的之后，这些决定可以在
// 没有 Docker 的机器上被完整验证；真引擎本身另有真实运行验证。
type fakeEngine struct {
	name string

	// mu 只保护 checked：镜像预检是并发的（36.1），其余调用都在单线程里
	mu sync.Mutex

	// 记录到的调用
	ups     []engine.UpRequest
	downs   []engine.DownRequest
	checked []string

	// 可编排的返回
	upErr     error
	downErr   error
	statusErr error
	checkErr  map[string]error
	// statuses 是 Status 的返回值；为 nil 时按 ups 里启动过的 service 编造"运行中"。
	statuses []engine.Status
	// currentContext 是引擎当前指向的集群（只有 K8s 有意义）。
	currentContext string
	// pruned 模拟"这次清理掉了这些孤儿资源"，通过 UpRequest.OnPrune 回传（P38）。
	pruned []string
	// networks 是"这台机器上已经存在的网络"，供 external 的启动前检查用（P39）。
	// 为 nil 时当作一张都没有——external 依赖的项目没部署过，正是要测的那种情况。
	networks map[string]bool
}

// HasNetwork 实现 engine.NetworkChecker。
func (f *fakeEngine) HasNetwork(_ context.Context, name string) (bool, error) {
	return f.networks[name], nil
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{name: engine.Docker, checkErr: map[string]error{}}
}

func (f *fakeEngine) Name() string { return f.name }

func (f *fakeEngine) Up(_ context.Context, req engine.UpRequest) error {
	f.ups = append(f.ups, req)
	// 真引擎清理孤儿时会逐个回调；夹具照做，命令层的汇报才测得到（P38）
	if req.OnPrune != nil {
		for _, resource := range f.pruned {
			req.OnPrune(resource)
		}
	}
	return f.upErr
}

func (f *fakeEngine) Down(_ context.Context, req engine.DownRequest) error {
	f.downs = append(f.downs, req)
	return f.downErr
}

func (f *fakeEngine) Status(_ context.Context, _ string) ([]engine.Status, error) {
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	if f.statuses != nil {
		return f.statuses, nil
	}

	// 没编排就按"启动过的都在跑"回答，让用例专注在别的事情上
	var out []engine.Status
	seen := map[string]bool{}
	for _, up := range f.ups {
		for _, service := range up.Services {
			if !seen[service] {
				seen[service] = true
				out = append(out, engine.Status{
					Service: service, State: "running", Health: "healthy",
				})
			}
		}
	}
	return out, nil
}

func (f *fakeEngine) CurrentContext(context.Context) (string, error) {
	return f.currentContext, nil
}

// CheckImage 会被**并发**调用（36.1 之后镜像预检是并行的），
// 所以这里必须上锁——否则真正在 race 的是夹具，而不是被测代码。
func (f *fakeEngine) CheckImage(_ context.Context, image string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checked = append(f.checked, image)
	return f.checkErr[image]
}

// lastUp 返回最后一次启动请求。
func (f *fakeEngine) lastUp(t *testing.T) engine.UpRequest {
	t.Helper()
	if len(f.ups) == 0 {
		t.Fatal("引擎没有被调用过（期望执行了一次启动）")
	}
	return f.ups[len(f.ups)-1]
}

// runWithEngine 用假引擎执行一条命令。
func runWithEngine(t *testing.T, eng *fakeEngine, dir string, args ...string) result {
	t.Helper()
	return runWith(t, func(o *Options) { o.Engine = eng }, dir, args...)
}

// checkedImages 返回被预检过的镜像（并发安全，见 CheckImage）。
func (f *fakeEngine) checkedImages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.checked...)
}
