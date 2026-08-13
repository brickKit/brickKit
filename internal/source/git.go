package source

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/manifest"
)

// gitSource 是 Git 仓库安装源（003 §6.3）。
//
// 仓库支持两种布局：
//
//	组件集合仓库：<repo>/<scope>/<name>/component.yaml
//	单组件仓库：  <repo>/component.yaml
//
// 仓库在首次使用时 clone 到临时目录，同一次命令内复用；Close 时删除。
// 缓存（.brickkit/manifests、.brickkit/artifacts）才是跨命令的持久层，
// 因此这里不需要维护长期的本地仓库副本。
type gitSource struct {
	sourceID string
	url      string

	once     sync.Once
	dir      string
	cloneErr error
}

func (s *gitSource) id() string   { return s.sourceID }
func (s *gitSource) kind() string { return "git" }

func (s *gitSource) close() error {
	if s.dir == "" {
		return nil
	}
	err := os.RemoveAll(s.dir)
	s.dir = ""
	return err
}

func (s *gitSource) manifestBytes(ctx context.Context, componentID, _ string) ([]byte, error) {
	dir, err := s.checkout(ctx)
	if err != nil {
		return nil, err
	}
	// 优先按 <scope>/<name>/component.yaml 定位；否则回落到单组件仓库的根目录。
	// 根目录的 component.yaml 是否真的是该组件，由调用方比对 metadata 判定。
	for _, root := range s.componentRoots(dir, componentID) {
		data, err := s.readComponentYAML(root)
		if err != nil {
			return nil, err
		}
		if data != nil {
			return data, nil
		}
	}
	return nil, errNotFound
}

func (s *gitSource) artifactFile(ctx context.Context, componentID, version string, _ manifest.Artifact, file string) ([]byte, error) {
	dir, err := s.checkout(ctx)
	if err != nil {
		return nil, err
	}
	for _, root := range s.componentRoots(dir, componentID) {
		header, err := s.readComponentYAML(root)
		if err != nil {
			return nil, err
		}
		// 只从"确实是该组件该版本"的目录取产物，避免跨版本串味。
		if header == nil || !manifestMatches(header, componentID, version) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		switch {
		case os.IsNotExist(err):
			return nil, errNotFound
		case err != nil:
			return nil, s.readError(filepath.Join(root, file), err)
		}
		return data, nil
	}
	return nil, errNotFound
}

// componentRoots 返回该组件在仓库中可能的根目录（组件集合仓库、单组件仓库）。
func (s *gitSource) componentRoots(dir, componentID string) []string {
	return []string{filepath.Join(dir, filepath.FromSlash(componentID)), dir}
}

// readComponentYAML 读取某个目录下的 component.yaml。不存在时返回 (nil, nil)。
func (s *gitSource) readComponentYAML(root string) ([]byte, error) {
	path := filepath.Join(root, manifest.FileName)
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return nil, nil
	case err != nil:
		return nil, s.readError(path, err)
	}
	return data, nil
}

// checkout 保证仓库已 clone，并把 clone 结果（成功或失败）在本次运行内复用。
func (s *gitSource) checkout(ctx context.Context) (string, error) {
	s.once.Do(func() {
		dir, err := os.MkdirTemp("", "brickkit-git-")
		if err != nil {
			s.cloneErr = clierr.New(clierr.CodeCloneFailed, "错误：无法创建临时目录").
				WithDetail("安装源", s.sourceID).
				WithDetail("原因", err.Error()).
				WithCause(err)
			return
		}
		if out, err := s.clone(ctx, dir); err != nil {
			_ = os.RemoveAll(dir)
			s.cloneErr = clierr.New(clierr.CodeCloneFailed, "错误：Git 仓库克隆失败").
				WithDetail("安装源", s.sourceID).
				WithDetail("仓库", s.url).
				WithDetail("原因", firstLine(out, err)).
				WithHint(
					"检查网络连接与仓库地址是否正确",
					"确认对该仓库有访问权限（私有仓库需配置 Git 凭据）",
					"或将该安装源设为 enabled: false",
				).WithCause(err)
			return
		}
		s.dir = dir
	})
	return s.dir, s.cloneErr
}

func (s *gitSource) clone(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--quiet", s.url, dir)
	// 不允许 git 弹出交互式凭据提示：CLI 不能在此挂起等待输入。
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (s *gitSource) readError(path string, cause error) error {
	return clierr.New(clierr.CodeCloneFailed, "错误：读取 Git 仓库文件失败").
		WithDetail("安装源", s.sourceID).
		WithDetail("路径", path).
		WithDetail("原因", cause.Error()).
		WithCause(cause)
}

// firstLine 取命令输出的首行作为原因；没有输出时回落到 error 本身。
func firstLine(out string, err error) string {
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return err.Error()
}
