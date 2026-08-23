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

// listComponents 扫出该目录下的所有组件（003 §6.4 的 <scope>/<name>/component.yaml）。
//
// 两道过滤缺一不可：
//   - 点开头的目录一律不当作 scope。默认约定里 local 源就指向 ./components，
//     而 components/.archived/（brickkit sync 的归档目录）和 .git/ 都在那底下。
//   - 目录名拼出来必须是合法组件 ID。非法 ID 进不了 brickkit.yaml，
//     扫出来只会在后面炸；这里挡住，报错才有意义。
func (s *localSource) listComponents() ([]string, []listProblem, error) {
	if err := s.checkRoot(); err != nil {
		return nil, nil, err
	}

	scopes, err := os.ReadDir(s.root)
	if err != nil {
		return nil, nil, s.listError(s.root, err)
	}

	var ids []string
	var problems []listProblem
	for _, scope := range scopes {
		if !scope.IsDir() || strings.HasPrefix(scope.Name(), ".") {
			continue
		}
		names, err := os.ReadDir(filepath.Join(s.root, scope.Name()))
		if err != nil {
			return nil, nil, s.listError(filepath.Join(s.root, scope.Name()), err)
		}
		for _, name := range names {
			if !name.IsDir() || strings.HasPrefix(name.Name(), ".") {
				continue
			}
			id := scope.Name() + "/" + name.Name()
			if manifest.ComponentIDProblem(id) != "" {
				continue
			}
			path := filepath.Join(s.root, scope.Name(), name.Name(), manifest.FileName)
			data, err := os.ReadFile(path)
			if err != nil {
				// 没有 component.yaml 的只是个普通目录，不是组件
				continue
			}

			// 下面几种是"像组件、但用不了"。**记下来交给调用方说出去**，
			// 而不是静默跳过：目录明明在那儿，扫描结果里却少一个、连名字都不出现，
			// 使用者只会去翻安装源配置，而问题在他自己刚写的这份文件里。
			var h componentHeader
			if err := yaml.Unmarshal(data, &h); err != nil {
				problems = append(problems, listProblem{id, "component.yaml 解析失败：" + err.Error()})
				continue
			}
			if h.Metadata.ID != id {
				problems = append(problems, listProblem{id,
					"目录名是 " + id + "，component.yaml 里却写着 " + h.Metadata.ID})
				continue
			}
			if !manifest.IsExactVersion(h.Metadata.Version) {
				got := h.Metadata.Version
				if got == "" {
					got = "（空）"
				}
				problems = append(problems, listProblem{id, "metadata.version 不是精确版本：" + got})
				continue
			}
			ids = append(ids, id)
		}
	}

	sort.Strings(ids)
	return ids, problems, nil
}

func (s *localSource) listError(path string, cause error) error {
	return clierr.New(clierr.CodeConfigInvalid, "错误：读取本地安装源目录失败").
		WithDetail("安装源", s.sourceID).
		WithDetail("目录", path).
		WithDetail("原因", cause.Error()).
		WithHint("检查目录权限").
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
// 归档目录仍然**不参与扫描**（listComponents 跳过点开头的目录）：
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
