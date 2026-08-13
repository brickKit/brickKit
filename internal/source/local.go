package source

import (
	"context"
	"os"
	"path/filepath"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/manifest"
)

// localSource 是本地目录安装源（003 §6.4）。
//
// 目录结构：<root>/<scope>/<name>/component.yaml
type localSource struct {
	sourceID string
	// configured 是 brickkit.yaml 中原样写下的路径（用于错误提示）。
	configured string
	// root 是相对项目根解析后的目录。
	root string
}

func (s *localSource) id() string   { return s.sourceID }
func (s *localSource) kind() string { return "local" }
func (s *localSource) close() error { return nil }

func (s *localSource) manifestBytes(_ context.Context, componentID, _ string) ([]byte, error) {
	if err := s.checkRoot(); err != nil {
		return nil, err
	}
	return s.readFile(filepath.Join(s.componentDir(componentID), manifest.FileName))
}

func (s *localSource) artifactFile(_ context.Context, componentID, version string, _ manifest.Artifact, file string) ([]byte, error) {
	if err := s.checkRoot(); err != nil {
		return nil, err
	}
	dir := s.componentDir(componentID)
	// 先确认该目录里放的确实是这个版本，否则会把别的版本的产物取回来。
	header, err := s.readFile(filepath.Join(dir, manifest.FileName))
	if err != nil {
		return nil, err
	}
	if !manifestMatches(header, componentID, version) {
		return nil, errNotFound
	}
	return s.readFile(filepath.Join(dir, filepath.FromSlash(file)))
}

func (s *localSource) componentDir(componentID string) string {
	return filepath.Join(s.root, filepath.FromSlash(componentID))
}

// checkRoot 校验安装源目录本身。路径不存在是配置错误，必须报出来，
// 而不是当作"该源没有这个组件"静默跳过（开发计划 6.2）。
func (s *localSource) checkRoot() error {
	info, err := os.Stat(s.root)
	switch {
	case os.IsNotExist(err):
		return clierr.New(clierr.CodeConfigInvalid, "错误：本地安装源路径不存在").
			WithDetail("安装源", s.sourceID).
			WithDetail("路径", s.configured).
			WithDetail("解析为", s.root).
			WithHint(
				"确认 brickkit.yaml → sources 中该安装源的 path 是否正确（相对 brickkit.yaml）",
				"或将该安装源设为 enabled: false",
			).WithCause(err)
	case err != nil:
		return clierr.New(clierr.CodeConfigInvalid, "错误：本地安装源路径无法访问").
			WithDetail("安装源", s.sourceID).
			WithDetail("路径", s.configured).
			WithDetail("原因", err.Error()).
			WithHint("检查目录权限").
			WithCause(err)
	case !info.IsDir():
		return clierr.New(clierr.CodeConfigInvalid, "错误：本地安装源路径不是目录").
			WithDetail("安装源", s.sourceID).
			WithDetail("路径", s.configured).
			WithHint("local 安装源的 path 必须指向包含 <scope>/<name>/component.yaml 的目录")
	}
	return nil
}

func (s *localSource) readFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return nil, errNotFound
	case err != nil:
		return nil, clierr.New(clierr.CodeConfigInvalid, "错误：读取本地安装源文件失败").
			WithDetail("安装源", s.sourceID).
			WithDetail("路径", path).
			WithDetail("原因", err.Error()).
			WithHint("检查文件权限").
			WithCause(err)
	}
	return data, nil
}

// origin 返回本地目录来源。本地源没有 Git 仓库地址，--repo 无从 clone。
func (s *localSource) origin(_ context.Context, componentID, version string) (*Origin, error) {
	if err := s.checkRoot(); err != nil {
		return nil, err
	}
	header, err := s.readFile(filepath.Join(s.componentDir(componentID), manifest.FileName))
	if err != nil {
		return nil, err
	}
	if !manifestMatches(header, componentID, version) {
		return nil, errNotFound
	}
	return &Origin{SourceID: s.sourceID, Type: OriginLocal}, nil
}
