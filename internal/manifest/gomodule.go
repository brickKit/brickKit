package manifest

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// GoModuleProblem：写了 release.goModule 时，组件目录里的 go.mod 接不接得住 Go 的 tag v<版本>。
// dir 是 component.yaml 所在的目录；没写这个字段时什么都不查。
//
// release 打 tag 之前拿它拦：Go 只在模块路径的主版本后缀与 tag 对得上时才认这个 tag（主版本 2 起
// 以 /v<主版本> 结尾，0 和 1 不带后缀），对不上的 tag 推出去就收不回来——版本不可变，删了重打会让
// 已经取过它的人报 VERSION_CHANGED。lint 在作者自己的组件上把同一件事提前当警告报。
//
// 不另外引入 golang.org/x/mod：要的只有读 module 行与核对主版本后缀，下面两个函数照 Go 自己的
// modfile.ModulePath 与 module.SplitPathVersion 的规则写；引入它的当前版本要把 go 指令抬到 1.26。
func GoModuleProblem(m *Manifest, dir string) *clierr.Error {
	if m == nil || !m.ReleasesGoModule() {
		return nil
	}
	path := filepath.Join(dir, "go.mod")
	raw, err := os.ReadFile(path)
	if err != nil {
		return clierr.New(clierr.CodeManifestInvalid, i18n.T(msgid.ManifestGoModMissing, m.Metadata.ID)).
			WithDetail(i18n.T(msgid.LabelFile), path).
			WithHint(i18n.T(msgid.ManifestHintGoModMissing))
	}
	module := goModulePath(raw)
	if module == "" {
		return clierr.New(clierr.CodeManifestInvalid, i18n.T(msgid.ManifestGoModNoModule, m.Metadata.ID)).
			WithDetail(i18n.T(msgid.LabelFile), path).
			WithHint(i18n.T(msgid.ManifestHintGoModNoModule))
	}
	major, err := strconv.Atoi(strings.SplitN(m.Metadata.Version, ".", 2)[0])
	if err != nil {
		return nil // 版本号本身不对，Validate 另有报错
	}
	got, want := goMajorSuffix(module, major)
	if got == want {
		return nil
	}
	var e *clierr.Error
	if want != "" {
		e = clierr.New(clierr.CodeManifestInvalid, i18n.T(msgid.ManifestGoModuleMajor, m.Metadata.ID, m.Metadata.Version, want))
	} else {
		e = clierr.New(clierr.CodeManifestInvalid, i18n.T(msgid.ManifestGoModuleMajorNone, m.Metadata.ID, m.Metadata.Version, got))
	}
	return e.WithDetail(i18n.T(msgid.LabelFile), path).
		WithDetail("module", module).
		WithHint(i18n.T(msgid.ManifestHintGoModuleMajor))
}

// goModulePath 取 go.mod 里 module 行的模块路径；没有时为空。规则与 Go 的 modfile.ModulePath 相同：
// 逐行看，去掉 // 注释，module 后面跟空白再跟路径，路径可以带引号。
func goModulePath(raw []byte) string {
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if i := bytes.Index(line, []byte("//")); i >= 0 {
			line = line[:i]
		}
		line = bytes.TrimSpace(line)
		rest, ok := bytes.CutPrefix(line, []byte("module"))
		if !ok {
			continue
		}
		path := bytes.TrimSpace(rest)
		if len(path) == len(rest) || len(path) == 0 {
			continue // moduleX，或者 module 后面什么都没有
		}
		if path[0] == '"' || path[0] == '`' {
			unquoted, err := strconv.Unquote(string(path))
			if err != nil {
				return ""
			}
			return unquoted
		}
		return string(path)
	}
	return ""
}

// goMajorSuffix 返回模块路径现在的主版本后缀与这个主版本要的后缀。一般的路径是 /v<N>，主版本 0、1
// 不带后缀；gopkg.in 的路径每个主版本都带 .v<N>。末尾的 /v1、/v02 这类 Go 不认的写法原样算作现在的
// 后缀，与要的对不上，照样报出来。
func goMajorSuffix(module string, major int) (got, want string) {
	sep := "/v"
	if strings.HasPrefix(module, "gopkg.in/") {
		sep = ".v"
		want = sep + strconv.Itoa(major)
	} else if major >= 2 {
		want = sep + strconv.Itoa(major)
	}
	i := len(module)
	for i > 0 && module[i-1] >= '0' && module[i-1] <= '9' {
		i--
	}
	if i < len(module) && i >= len(sep) && module[i-len(sep):i] == sep {
		got = module[i-len(sep):]
	}
	return got, want
}
