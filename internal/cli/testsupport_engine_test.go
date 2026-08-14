package cli

import (
	"context"
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
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{name: engine.Docker, checkErr: map[string]error{}}
}

func (f *fakeEngine) Name() string { return f.name }

func (f *fakeEngine) Up(_ context.Context, req engine.UpRequest) error {
	f.ups = append(f.ups, req)
	return f.upErr
}

func (f *fakeEngine) Down(_ context.Context, req engine.DownRequest) error {
	f.downs = append(f.downs, req)
	return f.downErr
}

func (f *fakeEngine) Status(_ context.Context, _, _ string) ([]engine.Status, error) {
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

func (f *fakeEngine) CheckImage(_ context.Context, image string) error {
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
