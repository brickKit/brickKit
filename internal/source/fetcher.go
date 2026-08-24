package source

import (
	"context"
	"errors"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
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
	// latestVersion 返回该源上这个组件可安装的最新版本。
	// 该源没有此组件时返回 errNotFound（调用方继续尝试下一个源）；
	// 组件在、但 component.yaml 用不了时返回**真错误**，由调用方决定报不报。
	latestVersion(ctx context.Context, componentID string) (string, error)
	// artifactFile 返回一个产物文件的内容。
	artifactFile(ctx context.Context, componentID, version string, art manifest.Artifact, file string) ([]byte, error)
	// origin 返回组件的来源信息（开源 git / 闭源 registry），供 --repo 使用。
	origin(ctx context.Context, componentID, version string) (*Origin, error)
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

// singleVersionLatest 是目录型安装源（local / git）的"最新版本"。
//
// 这两种源按 <scope>/<name> 定位，目录里只有一份 component.yaml——
// 它写的是哪个版本，这个源能提供的就只有那个版本，"最新"没有别的候选。
// 因此复用 manifestBytes（它俩本来就忽略 version 参数），读出 metadata 即可。
func singleVersionLatest(ctx context.Context, f fetcher, componentID string) (string, error) {
	data, err := f.manifestBytes(ctx, componentID, "")
	if err != nil {
		return "", err
	}

	var h componentHeader
	if err := yaml.Unmarshal(data, &h); err != nil {
		// 组件**在**这儿，只是这份 component.yaml 读不了。
		// 从前这里返回 errNotFound，于是最终报的是"组件未找到，检查安装源配置"——
		// 把人引向完全无关的方向，而问题就在他指定的那个目录里。
		return "", manifestUnusable(f, componentID, "component.yaml 解析失败："+err.Error(),
			"用 YAML 校验器看一眼这个文件")
	}

	// 目录里放的是别的组件（git 源回落到仓库根目录时会出现）：等同于"这里没有"
	if h.Metadata.ID != componentID {
		return "", errNotFound
	}
	if !manifest.IsExactVersion(h.Metadata.Version) {
		got := h.Metadata.Version
		if got == "" {
			got = "（空）"
		}
		return "", manifestUnusable(f, componentID,
			"metadata.version 不是精确版本："+got,
			"改成 major.minor.patch，如 1.0.0（002 §7.1）")
	}
	return h.Metadata.Version, nil
}

// manifestUnusable 是"组件在、但它的 component.yaml 用不了"。
//
// 与 errNotFound 分开的理由：两者该让使用者去看的地方完全不同——
// 一个是安装源配置，一个是他自己刚写的那份 component.yaml。
func manifestUnusable(f fetcher, componentID, reason string, hints ...string) error {
	return clierr.Newf(clierr.CodeManifestInvalid, "错误：%s 的 component.yaml 用不了", componentID).
		WithDetail("安装源", f.id()+"（"+f.kind()+"）").
		WithDetail("原因", reason).
		WithHint(hints...)
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

// manifestParses 判断这份 component.yaml 至少还是合法 YAML。
//
// 与 manifestMatches 分开，是因为"解析不了"和"是别的组件"必须区别对待：
// 前者说明**文件确实在这儿，只是坏了**，那份错误必须让人看见；
// 把两者一起当成"这个源没有它"，就会悄悄退回上一份缓存（见
// Client.servedByLocalSource）。
func manifestParses(data []byte) bool {
	var h componentHeader
	return yaml.Unmarshal(data, &h) == nil
}

// 组件来源类型（007 §11.1）。
const (
	// OriginGit 表示开源组件：有 Git 仓库，可以 clone 源码。
	OriginGit = "git"
	// OriginRegistry 表示闭源组件：只有镜像与产物，没有源码仓库。
	OriginRegistry = "registry"
	// OriginLocal 表示组件来自本地目录安装源，没有 Git 仓库地址。
	OriginLocal = "local"
)

// Origin 描述组件在安装源中的来源信息，供 brickkit add --repo 使用。
type Origin struct {
	// SourceID 是提供该组件的安装源 id。
	SourceID string
	// Type 是 OriginGit / OriginRegistry / OriginLocal，未知时为空。
	Type string
	// GitURL 是开源组件的仓库地址（OriginGit 时有值）。
	GitURL string
}

// IsOpenSource 判断该组件是否可以 clone 源码。
func (o *Origin) IsOpenSource() bool { return o != nil && o.Type == OriginGit && o.GitURL != "" }
