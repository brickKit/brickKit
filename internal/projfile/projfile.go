// Package projfile 负责 brickkit.yaml（三层文件的"组件声明"层）：项目名、安装源、
// 有哪些组件与版本、谁是外壳、组件从哪来、信任哪些发布者公钥。
//
// 部署怎么做（deploy.yaml）与组件要读的环境变量（config/）都不在这里——每件事只写在一个地方。
package projfile

// FileName 是声明层文件名。
const FileName = "brickkit.yaml"

// 安装源类型。market 继续可用，git 按组件一仓库推导地址，local 是开发态目录。
const (
	SourceTypeMarket = "market"
	SourceTypeGit    = "git"
	SourceTypeLocal  = "local"
)

// KindShell 标记外壳组件（由 CLI 维护，只作识别用，成员关系只看 deploy 文件的 members）。
const KindShell = "shell"

// File 是 brickkit.yaml 的完整结构。
type File struct {
	Project    string      `yaml:"project"`
	Sources    []Source    `yaml:"sources,omitempty"`
	Components []Component `yaml:"components,omitempty"`
	Installer  *Installer  `yaml:"installer,omitempty"`

	// Source 是文件路径，只用于报错。
	Source string `yaml:"-"`
}

// Source 是一个安装源。
type Source struct {
	Name string `yaml:"name"`
	Type string `yaml:"type" jsonschema:"enum=market|git|local"`
	// URL 是 market 源的 API 地址。
	URL string `yaml:"url,omitempty"`
	// BaseURL 是 git 源的组织地址：组件 erp/backend 推导为 <BaseURL>erp-backend。
	BaseURL string `yaml:"baseUrl,omitempty"`
	// Path 是 local 源扫描的目录。
	Path      string `yaml:"path,omitempty"`
	AuthToken string `yaml:"authToken,omitempty"`
	// Enabled 缺省为 true。
	Enabled *bool `yaml:"enabled,omitempty"`
}

// IsEnabled 返回该源是否启用（缺省 true）。
func (s Source) IsEnabled() bool { return s.Enabled == nil || *s.Enabled }

// Component 是一条组件声明。
//
// Version 永远是精确版本——本地源也写 component.yaml 里的真实版本，
// 否则依赖方的精确匹配会落空，多版本共存会再从远端拉一份。
type Component struct {
	ID      string           `yaml:"id"`
	Version string           `yaml:"version" jsonschema:"pattern=^[0-9]+[.][0-9]+[.][0-9]+$"`
	Kind    string           `yaml:"kind,omitempty" jsonschema:"enum=shell"`
	Source  *ComponentSource `yaml:"source,omitempty"`
	// RequiredBy 标出"这个版本只是因为这些组件依赖它才在项目里"（多版本兼容）。
	// 同一个 ID 恰好有一行不写它，那一行就是默认版本（DefaultVersion）。
	RequiredBy []string `yaml:"requiredBy,omitempty"`
}

// Ref 返回 <id>@<version>。
func (c Component) Ref() string { return c.ID + "@" + c.Version }

// IsShell 报告它是不是外壳。
func (c Component) IsShell() bool { return c.Kind == KindShell }

// ComponentSource 显式覆盖一个组件的来源。
type ComponentSource struct {
	Type string `yaml:"type" jsonschema:"enum=git|local"`
	// Repo 是 git 仓库地址（映射冲突或第三方组织时用）。
	Repo string `yaml:"repo,omitempty"`
	// Path 是本地源目录，相对项目根。
	Path string `yaml:"path,omitempty"`
}

// Installer 是安装器行为配置（签名校验）。
type Installer struct {
	RequireSignature *bool             `yaml:"requireSignature,omitempty"`
	PublicKeys       map[string]string `yaml:"publicKeys,omitempty"`
}

// RequireSignature 返回是否强制签名校验（缺省 true）。
func (f *File) RequireSignature() bool {
	if f.Installer == nil || f.Installer.RequireSignature == nil {
		return true
	}
	return *f.Installer.RequireSignature
}

// PublicKeys 返回信任的发布者公钥，未配置时为 nil。
func (f *File) PublicKeys() map[string]string {
	if f.Installer == nil {
		return nil
	}
	return f.Installer.PublicKeys
}

// EnabledSources 返回启用中的安装源，保持声明顺序（即优先级）。
func (f *File) EnabledSources() []Source {
	var out []Source
	for _, s := range f.Sources {
		if s.IsEnabled() {
			out = append(out, s)
		}
	}
	return out
}

// ComponentsByID 返回某个 ID 的全部版本条目。
func (f *File) ComponentsByID(id string) []Component {
	var out []Component
	for _, c := range f.Components {
		if c.ID == id {
			out = append(out, c)
		}
	}
	return out
}

// Versions 返回某个 ID 的全部版本，按声明顺序。
func (f *File) Versions(id string) []string {
	var out []string
	for _, c := range f.ComponentsByID(id) {
		out = append(out, c.Version)
	}
	return out
}

// DefaultVersion 返回某个 ID 的默认版本：不带 requiredBy 的那一行（校验保证恰好一行）。
// 部署文件的裸 ID 条目、无版本号的配置文件、外壳承载的成员，指的都是它。
func (f *File) DefaultVersion(id string) (string, bool) {
	for _, c := range f.ComponentsByID(id) {
		if len(c.RequiredBy) == 0 {
			return c.Version, true
		}
	}
	return "", false
}

// IsDefault 报告该版本是不是这个 ID 的默认版本。
func (f *File) IsDefault(id, version string) bool {
	v, ok := f.DefaultVersion(id)
	return ok && v == version
}

// IDs 返回去重后的组件 ID，按首次出现的顺序。
func (f *File) IDs() []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range f.Components {
		if !seen[c.ID] {
			seen[c.ID] = true
			out = append(out, c.ID)
		}
	}
	return out
}

// IsShellID 报告某个 ID 是否被标成外壳。
func (f *File) IsShellID(id string) bool {
	for _, c := range f.Components {
		if c.ID == id && c.IsShell() {
			return true
		}
	}
	return false
}
