package source

import (
	"context"
	"errors"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/manifest"
)

// errNotFound 表示"该安装源里没有这个组件（或没有这个版本）"。
//
// 它不是失败：调用方应继续尝试下一个安装源（003 §6.5 安装源优先级）。
var errNotFound = errors.New("组件在该安装源中不存在")

// isNotFound 判断一个错误是否是"该源没有"。
func isNotFound(err error) bool { return errors.Is(err, errNotFound) }

// fetcher 是单个安装源的统一抽象。三种实现：local / git / market。
type fetcher interface {
	// id 是 brickkit.yaml 中该安装源的 id。
	id() string
	// kind 是安装源类型（local / git / market）。
	kind() string
	// manifestBytes 返回 component.yaml 的原始内容。
	// 该源没有此组件时返回 errNotFound（调用方继续尝试下一个源）。
	manifestBytes(ctx context.Context, componentID, version string) ([]byte, error)
	// artifactFile 返回一个产物文件的内容。
	artifactFile(ctx context.Context, componentID, version string, art manifest.Artifact, file string) ([]byte, error)
	// close 释放该源占用的临时资源。
	close() error
}

// componentHeader 只取判定"这份 component.yaml 属于哪个组件版本"所需的字段。
type componentHeader struct {
	Metadata struct {
		ID      string `yaml:"id"`
		Version string `yaml:"version"`
	} `yaml:"metadata"`
}

// manifestMatches 判断一份 component.yaml 是否正是该组件的该版本。
//
// 目录型安装源（local / git）按 <scope>/<name> 定位，目录里放的是哪个版本要看
// component.yaml：不校验就会把 1.0.0 的产物当作 2.0.0 的产物下载下来。
func manifestMatches(data []byte, componentID, version string) bool {
	var h componentHeader
	if err := yaml.Unmarshal(data, &h); err != nil {
		return false
	}
	return h.Metadata.ID == componentID && h.Metadata.Version == version
}
