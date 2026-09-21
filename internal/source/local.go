package source

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
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

// 编译期断言：localSource 是（也是唯一的）listableFetcher。list.go 里靠类型断言找它，
// 方法一改名，断言会静默变成"不是"——LocalComponents / LocalManifestFiles 悄悄什么都不列；
// 有了这一行，那会变成编译错误。
var _ listableFetcher = (*localSource)(nil)

func (s *localSource) id() string   { return s.sourceID }
func (s *localSource) kind() string { return "local" }
func (s *localSource) close() error { return nil }

func (s *localSource) manifestBytes(_ context.Context, componentID, _ string) ([]byte, error) {
	if err := s.checkRoot(); err != nil {
		return nil, err
	}
	return s.readFile(filepath.Join(s.componentDir(componentID), manifest.FileName))
}

func (s *localSource) latestVersion(ctx context.Context, componentID string) (string, error) {
	return singleVersionLatest(ctx, s, componentID)
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

// localManifestFile 是本地安装源目录下的一份 component.yaml。
type localManifestFile struct {
	// id 是按目录名（<scope>/<name>）拼出来的组件 ID。
	id string
	// path 是 component.yaml 的完整路径。
	path string
}

// manifestFiles 枚举 <root>/<scope>/<name>/component.yaml（003 §6.4）。
//
// 两道过滤各管一件事：
//   - 目录名拼出来必须是合法组件 ID。非法 ID 进不了 brickkit.yaml，
//     扫出来只会在后面炸；这里挡住，报错才有意义。就输出而言，光这一道就够了：
//     组件 ID 不能以点开头，.archived/、.git/ 这类目录本来就拼不出合法 ID。
//   - 点开头的目录不当作 scope。默认约定里 local 源就指向 ./components，
//     而 components/.archived/（brickkit sync 的归档目录）和 .git/ 都在那底下。
//     这一道的作用不在输出，而在**不去读那些目录**：没有它，要先 ReadDir 一遍它们才会被
//     上一道丢掉，其中任何一个读不了（权限之类）都会让整次枚举报错中断——
//     lint 和 add --local 会因为一个使用者根本不会编辑的目录而整体失败
//     （TestLocalManifestFilesDoesNotReadDotDirectories 钉着这一点）。
//     scope 之下点开头的名字同样跳过，但那里没有读目录的动作，纯属被 ID 检查覆盖，
//     留着只为让意图一眼看得懂。
//
// 只看文件在不在，读不读得动、内容对不对是调用方的事：listComponents 要在此之上
// 做"表头"筛选，lint 要完整报告每一份文件——枚举这一步不能替它们丢东西。
func (s *localSource) manifestFiles() ([]localManifestFile, error) {
	if err := s.checkRoot(); err != nil {
		return nil, err
	}

	scopes, err := os.ReadDir(s.root)
	if err != nil {
		return nil, s.listError(s.root, err)
	}

	var out []localManifestFile
	for _, scope := range scopes {
		if !scope.IsDir() || strings.HasPrefix(scope.Name(), ".") {
			continue
		}
		names, err := os.ReadDir(filepath.Join(s.root, scope.Name()))
		if err != nil {
			return nil, s.listError(filepath.Join(s.root, scope.Name()), err)
		}
		for _, name := range names {
			if !name.IsDir() || strings.HasPrefix(name.Name(), ".") {
				continue
			}
			id := scope.Name() + "/" + name.Name()
			if manifest.ComponentIDProblem(id) != "" {
				continue
			}
			dir := filepath.Join(s.root, scope.Name(), name.Name())
			if !hasManifest(dir) {
				// 没有 component.yaml 的只是个普通目录，不是组件
				continue
			}
			out = append(out, localManifestFile{id: id, path: filepath.Join(dir, manifest.FileName)})
		}
	}
	return out, nil
}

// listComponents 扫出该目录下的所有组件（003 §6.4 的 <scope>/<name>/component.yaml）。
//
// 在 manifestFiles 的目录遍历之上再过一遍"表头"：读不动的文件直接跳过，
// 表头不合格的记成 listProblem。
func (s *localSource) listComponents() ([]string, []listProblem, error) {
	files, err := s.manifestFiles()
	if err != nil {
		return nil, nil, err
	}

	var ids []string
	var problems []listProblem
	for _, f := range files {
		data, err := os.ReadFile(f.path)
		if err != nil {
			continue
		}

		// 下面几种是"像组件、但用不了"。**记下来交给调用方说出去**，
		// 而不是静默跳过：目录明明在那儿，扫描结果里却少一个、连名字都不出现，
		// 使用者只会去翻安装源配置，而问题在他自己刚写的这份文件里。
		var h componentHeader
		if err := yaml.Unmarshal(data, &h); err != nil {
			problems = append(problems, listProblem{f.id, i18n.T(msgid.SourceManifestParseFailed, err.Error())})
			continue
		}
		if h.Metadata.ID != f.id {
			problems = append(problems, listProblem{f.id,
				i18n.T(msgid.SourceDirNameMismatch, f.id, h.Metadata.ID)})
			continue
		}
		if !manifest.IsExactVersion(h.Metadata.Version) {
			got := h.Metadata.Version
			if got == "" {
				got = i18n.T(msgid.SourceEmptyValue)
			}
			problems = append(problems, listProblem{f.id, i18n.T(msgid.SourceVersionNotExact, got)})
			continue
		}
		ids = append(ids, f.id)
	}

	sort.Strings(ids)
	return ids, problems, nil
}

func (s *localSource) listError(path string, cause error) error {
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.SourceListDirFailed)).
		WithDetail(i18n.T(msgid.LabelSource), s.sourceID).
		WithDetail(i18n.T(msgid.LabelDir), path).
		WithDetail(i18n.T(msgid.LabelReason), cause.Error()).
		WithHint(i18n.T(msgid.SourceHintCheckDirPermissions)).
		WithCause(cause)
}

// componentDir 返回该组件在本安装源里的目录。
//
// 活跃目录优先；那里没有 component.yaml 时，回落到归档目录
// `<root>/.archived/<scope>/<name>/`——也就是 brickkit sync 搬过去的那一份。
//
// # 为什么按 ID 找时要认归档目录
//
// sync 的用途是"把这次不跑的组件从眼前挪开"（004 §3.9），而挪开**不等于**
// 从项目里消失：brickkit.yaml 里那一行还在，级联计算就得读得到它的 Manifest。
//
// 不回落的话，默认约定（init 骨架把 local 源指向 ./components）下有一个
// 解不开的死局：归档 → Manifest 缓存过期或被清 → `up` 报"组件未找到，
// 检查安装源配置"，而 `sync` 自己也要先解析全图，于是连"把它移回来"都做不到，
// 只能手工 mv。组件越多、归档得越狠，越容易撞上——恰好惩罚了 sync 想支持的用法。
//
// # 与 listComponents 的分工
//
// 归档目录仍然**不参与扫描**（manifestFiles 跳过点开头的目录，listComponents
// 与 LocalManifestFiles 都建立在它之上）：
// `add --local` 不该把刚归档的组件又拽回配置里。两条规则各管各的——
// **扫描时看不见，按 ID 找时找得到。**
func (s *localSource) componentDir(componentID string) string {
	active := filepath.Join(s.root, filepath.FromSlash(componentID))
	if hasManifest(active) {
		return active
	}
	if archived := filepath.Join(s.root, config.DirArchived, filepath.FromSlash(componentID)); hasManifest(archived) {
		return archived
	}
	// 两处都没有：返回活跃目录，让"找不到"的报错指向使用者预期的位置
	return active
}

// hasManifest 判断一个目录里有没有 component.yaml。
func hasManifest(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, manifest.FileName))
	return err == nil && !info.IsDir()
}

// checkRoot 校验安装源目录本身。路径不存在是配置错误，必须报出来，
// 而不是当作"该源没有这个组件"静默跳过（开发计划 6.2）。
func (s *localSource) checkRoot() error {
	info, err := os.Stat(s.root)
	switch {
	case os.IsNotExist(err):
		return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.SourceLocalPathMissing)).
			WithDetail(i18n.T(msgid.LabelSource), s.sourceID).
			WithDetail(i18n.T(msgid.LabelPath), s.configured).
			WithDetail(i18n.T(msgid.SourceLabelResolvedAs), s.root).
			WithHint(
				i18n.T(msgid.SourceHintCheckLocalPath),
				i18n.T(msgid.SourceHintDisableSource),
			).WithCause(err)
	case err != nil:
		return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.SourceLocalPathInaccessible)).
			WithDetail(i18n.T(msgid.LabelSource), s.sourceID).
			WithDetail(i18n.T(msgid.LabelPath), s.configured).
			WithDetail(i18n.T(msgid.LabelReason), err.Error()).
			WithHint(i18n.T(msgid.SourceHintCheckDirPermissions)).
			WithCause(err)
	case !info.IsDir():
		return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.SourceLocalPathNotDir)).
			WithDetail(i18n.T(msgid.LabelSource), s.sourceID).
			WithDetail(i18n.T(msgid.LabelPath), s.configured).
			WithHint(i18n.T(msgid.SourceHintLocalPathMustBeRoot))
	}
	return nil
}

func (s *localSource) readFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return nil, errNotFound
	case err != nil:
		return nil, clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.SourceLocalFileReadFailed)).
			WithDetail(i18n.T(msgid.LabelSource), s.sourceID).
			WithDetail(i18n.T(msgid.LabelPath), path).
			WithDetail(i18n.T(msgid.LabelReason), err.Error()).
			WithHint(i18n.T(msgid.ProblemHintCheckPermissions)).
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
