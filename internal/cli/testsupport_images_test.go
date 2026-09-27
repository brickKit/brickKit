package cli

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/brickkit/brickkit/internal/engine"
)

// everyImagePresent 是测试默认的本机镜像替身：什么镜像都当作在本机（多数用例关心的不是镜像），
// 标签一律查不到——外壳镜像的核对只在用例专门给出本机镜像时才发生。测试绝不碰真的 docker。
type everyImagePresent struct{}

func (everyImagePresent) ImageExists(context.Context, string) (bool, error) { return true, nil }
func (everyImagePresent) ImageLabels(context.Context, string) (map[string]string, bool, error) {
	return nil, false, nil
}
func (everyImagePresent) Build(context.Context, engine.BuildRequest) error { return nil }

// fakeImages 是本机镜像的替身：present 里的镜像算在本机，Build 记下请求并把镜像放进 present。
type fakeImages struct {
	mu       sync.Mutex
	present  map[string]map[string]string // 镜像 → 标签
	builds   []engine.BuildRequest
	buildErr map[string]error // 按 tag
	// hadDockerfile 记下构建那一刻 Dockerfile 在不在（导出的源码构建完就删了）
	hadDockerfile map[string]bool
}

func newFakeImages(present ...string) *fakeImages {
	f := &fakeImages{present: map[string]map[string]string{}, buildErr: map[string]error{}}
	for _, p := range present {
		f.present[p] = map[string]string{}
	}
	return f
}

func (f *fakeImages) ImageExists(_ context.Context, ref string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.present[ref]
	return ok, nil
}

func (f *fakeImages) ImageLabels(_ context.Context, ref string) (map[string]string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	labels, ok := f.present[ref]
	return labels, ok, nil
}

func (f *fakeImages) Build(_ context.Context, req engine.BuildRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.buildErr[req.Tag]; err != nil {
		return err
	}
	f.builds = append(f.builds, req)
	f.present[req.Tag] = req.Labels
	if f.hadDockerfile == nil {
		f.hadDockerfile = map[string]bool{}
	}
	_, statErr := os.Stat(req.Dockerfile)
	f.hadDockerfile[req.Tag] = statErr == nil
	return nil
}

func (f *fakeImages) built() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var tags []string
	for _, b := range f.builds {
		tags = append(tags, b.Tag)
	}
	return tags
}

// recordingContext 在构建时读出上下文里的 VERSION 文件（导出的源码在构建完就删了）。
type recordingContext struct {
	*fakeImages
	contexts map[string]string
}

func (r recordingContext) Build(ctx context.Context, req engine.BuildRequest) error {
	if data, err := os.ReadFile(filepath.Join(req.Context, "VERSION")); err == nil {
		r.contexts[req.Tag] = string(data)
	}
	return r.fakeImages.Build(ctx, req)
}
