package cli

// 本文件写 pre-commit hook：它是唯一能在 git commit 那个时点上真的拦住人的
// 东西（004 §3.14）。
//
// 因此它自己绝不能变成新的故障源。三条底线：
//
//	找不到 brickkit        放行。否则新人 clone 下来第一件事就是提交不了，
//	                      而原因跟他要做的事毫无关系
//	别人的 pre-commit      绝不覆盖。报错，并把该插进去的那一行告诉他
//	项目目录已经不在了      跳过。一条过期的路径不该堵死整个仓库的提交

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/gitrepo"
)

// hookMarker 是"这个 hook 是 brickkit 写的"的标记。认它才敢覆盖。
const hookMarker = "# brickkit-managed-hook"

// hookListStart / hookListEnd 圈出脚本里的项目清单，升级时按它们把清单读回来。
const (
	hookListStart = "BRICKKIT_PROJECTS"
	hookListEnd   = "BRICKKIT_PROJECTS"
)

// hookProject 是 hook 要检查的一个项目。
type hookProject struct {
	// Dir 是项目根相对仓库根的路径（以 / 分隔；仓库根本身是 "."）。
	Dir string
	// Config 是该项目的配置文件名（--config 可以改名）。
	Config string
}

// renderHook 生成 pre-commit 脚本。
//
// 全程 POSIX sh，不用任何 bash 特性：Windows 上 Git for Windows 用自带的 sh
// 跑 hook，一句 [[ ]] 就会让它在那儿直接崩掉。
//
// 清单用 quoted here-doc 喂给 while 循环，而不是 for + 变量展开：
// 后者会按空格切词，带空格的路径就散了。here-doc 喂给当前 shell 的循环，
// 循环里设的 rc 也就带得出来（管道会把循环丢进子 shell，rc 出不来）。
func renderHook(binPath, ver string, projects []hookProject) string {
	var list strings.Builder
	for _, p := range projects {
		list.WriteString(p.Dir + "|" + p.Config + "\n")
	}
	return `#!/bin/sh
` + hookMarker + ` ` + ver + `
# 由 brickkit init --hooks 写入。可安全覆盖升级；想卸载就删掉这个文件。
#
# 它拦一件事：组件源码提交在 components/.archived/ 里，而 brickkit.yaml 说它该启动。
# 判据与出路见 brickkit restore --check。
BRICKKIT_BIN='` + binPath + `'
[ -x "$BRICKKIT_BIN" ] || BRICKKIT_BIN=$(command -v brickkit 2>/dev/null)
if [ -z "$BRICKKIT_BIN" ]; then
	echo "⚠️  找不到 brickkit，跳过组件结构检查（brickkit init --hooks 可重装本 hook）" >&2
	exit 0
fi
rc=0
while IFS='|' read -r dir cfg; do
	[ -n "$dir" ] || continue
	[ -d "$dir" ] || continue
	( cd "$dir" && "$BRICKKIT_BIN" restore --check --config "$cfg" ) || rc=1
done <<'` + hookListStart + `'
` + list.String() + hookListEnd + `
exit $rc
`
}

// parseHookProjects 从脚本里把项目清单读回来（升级时要保住别的项目）。
func parseHookProjects(script string) []hookProject {
	_, rest, ok := strings.Cut(script, "<<'"+hookListStart+"'\n")
	if !ok {
		return nil
	}
	body, _, ok := strings.Cut(rest, "\n"+hookListEnd+"\n")
	if !ok {
		return nil
	}
	var projects []hookProject
	for _, line := range strings.Split(body, "\n") {
		dir, cfg, ok := strings.Cut(line, "|")
		if !ok || dir == "" {
			continue
		}
		projects = append(projects, hookProject{Dir: dir, Config: cfg})
	}
	return projects
}

// installHook 把 pre-commit hook 装进仓库，幂等追加项目。
//
// added 报告这次是不是真加了一个新项目，用于输出措辞（"已装上" / "已经装过了"）。
func installHook(
	repo *gitrepo.Repo, p hookProject, binPath, ver string,
) (string, bool, error) {
	hooks, err := repo.HooksDir()
	if err != nil {
		return "", false, clierr.New(clierr.CodeInternal, "错误：定位 hooks 目录失败").
			WithDetail("原因", err.Error()).WithCause(err)
	}
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		return "", false, clierr.New(clierr.CodeInternal, "错误：创建 hooks 目录失败").
			WithDetail("目录", hooks).
			WithDetail("原因", err.Error()).WithCause(err)
	}
	path := filepath.Join(hooks, "pre-commit")

	// 清单必须在写之前读回来：写完再读就只能读到自己刚写的那一份
	before := len(parseHookProjectsFile(path))
	projects := []hookProject{p}
	if existing, err := os.ReadFile(path); err == nil {
		if !strings.Contains(string(existing), hookMarker) {
			return "", false, foreignHookError(path, p)
		}
		projects = mergeHookProjects(parseHookProjects(string(existing)), p)
	}
	added := len(projects) != before

	if err := os.WriteFile(path, []byte(renderHook(binPath, ver, projects)), 0o755); err != nil {
		return "", false, clierr.New(clierr.CodeInternal, "错误：写入 pre-commit hook 失败").
			WithDetail("文件", path).
			WithDetail("原因", err.Error()).WithCause(err)
	}
	return path, added, nil
}

// parseHookProjectsFile 读文件里的项目清单；读不到就当空的。
func parseHookProjectsFile(path string) []hookProject {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseHookProjects(string(data))
}

// mergeHookProjects 把一个项目并进清单，按 Dir 去重（重复安装不重复加）。
func mergeHookProjects(existing []hookProject, p hookProject) []hookProject {
	for i, e := range existing {
		if e.Dir == p.Dir {
			// 同一个项目重装：配置文件名可能变了，跟最新的走
			existing[i] = p
			return existing
		}
	}
	return append(existing, p)
}

// foreignHookError 是"这儿已经有别人的 hook 了"该说的那句话。
//
// 绝不覆盖：那可能是 husky、lefthook，或者他自己写的十几行。平台没有立场
// 替他决定丢掉它——所以把该插进去的那一行给他，让他自己放。
func foreignHookError(path string, p hookProject) error {
	line := "brickkit restore --check --config " + p.Config + " || exit 1"
	if p.Dir != "." {
		line = "( cd " + p.Dir + " && brickkit restore --check --config " + p.Config + " ) || exit 1"
	}
	return clierr.New(clierr.CodeConfigConflict, "错误：pre-commit hook 已存在，不是 brickkit 写的").
		WithDetail("文件", path).
		WithDetail("要插进去的那一行", line).
		WithHint(
			"平台绝不覆盖你自己的 hook——它可能是 husky / lefthook，也可能是你写的",
			"把上面那一行加进你的 pre-commit 即可",
			"确认那个文件没用了，也可以删掉它再跑 brickkit init --hooks",
		)
}
