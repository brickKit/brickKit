// Package gitrepo 是 BrickKit 对 git 仓库做只读查询的唯一入口。
//
// # 为什么要收成一个包
//
// 提交前的判据要读 index、restore 要读 HEAD、hook 安装要找 hooks 目录——三处都得
// 处理同样几件麻烦事：worktree（.git 是文件）、submodule、core.hooksPath、
// 路径分隔符（git 只认 /）、以及"别读使用者的全局配置"。分散在三处写，
// 迟早有一处写错，而写错的那一处不会报错，只会静默地判错或装错地方。
package gitrepo

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotRepo 表示给定目录不在任何 git 仓库里。
var ErrNotRepo = errors.New("不在 git 仓库里")

// Repo 是一个已定位的 git 仓库。
type Repo struct{ root string }

// Open 定位 dir 所在的 git 仓库。
func Open(dir string) (*Repo, error) {
	out, err := query(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, ErrNotRepo
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return nil, ErrNotRepo
	}
	// macOS 上 /tmp 是符号链接，git 报的是物理路径而 t.TempDir() 给的是链接路径；
	// 两边不统一，Rel 会算出一串 ../..，判据就会把整个仓库当成"在仓库外"。
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return &Repo{root: root}, nil
}

// Root 返回仓库根目录（绝对路径）。
func (r *Repo) Root() string { return r.root }

// Rel 把路径转成"相对仓库根、以 / 分隔"的形式。路径在仓库之外时 ok 为 false。
//
// git 只认 /，Windows 上必须转；而 .. 开头意味着它根本不在这个仓库里——
// 那种情况要让调用方看得出来，不能交给 git 去报一句莫名的错。
func (r *Repo) Rel(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	// 目标文件可能还不存在（配置文件路径），所以解析它的父目录而不是它自己。
	if dir, base := filepath.Split(abs); dir != "" {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			abs = filepath.Join(resolved, base)
		}
	}
	rel, err := filepath.Rel(r.root, abs)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
}

// HasHEAD 报告仓库有没有任何提交。没有提交就没有"最后一次提交"这个基准。
func (r *Repo) HasHEAD() bool {
	_, err := query(r.root, "rev-parse", "--verify", "--quiet", "HEAD")
	return err == nil
}

// Unmerged 报告是不是正处在冲突中（index 里有未合并条目）。
//
// 冲突中 `git show :<path>` 会直接 fatal（不存在 stage 0），所以判据必须先问这一句。
func (r *Repo) Unmerged() bool {
	out, err := query(r.root, "ls-files", "--unmerged")
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// Tracked 报告某个相对路径是否在 index 里。
func (r *Repo) Tracked(rel string) bool {
	out, err := query(r.root, "ls-files", "--cached", "--", rel)
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// IndexBlob 读 index 里某个路径的内容，也就是**即将提交的那一份**。
func (r *Repo) IndexBlob(rel string) ([]byte, error) {
	return query(r.root, "show", ":"+rel)
}

// HeadBlob 读 HEAD 里某个路径的内容，也就是**最后一次提交的那一份**。
func (r *Repo) HeadBlob(rel string) ([]byte, error) {
	return query(r.root, "show", "HEAD:"+rel)
}

// IndexEntry 是 index 里的一条记录。
type IndexEntry struct {
	// Mode 是 git 的文件模式：100644 普通文件、100755 可执行、120000 符号链接、
	// 160000 嵌套仓库指针。
	Mode string
	// Path 相对仓库根，以 / 分隔。
	Path string
}

// IsGitlink 报告这条记录是不是嵌套仓库指针——提交进去的只是一个指针，没有内容。
func (e IndexEntry) IsGitlink() bool { return e.Mode == "160000" }

// IndexEntries 列出 index 里某个前缀下的全部记录。
//
// 用 -z：带非 ASCII 字符的路径会被 git 加引号转义，-z 直接绕开这件事。
func (r *Repo) IndexEntries(relPrefix string) ([]IndexEntry, error) {
	out, err := query(r.root, "ls-files", "--cached", "--stage", "-z", "--", relPrefix)
	if err != nil {
		return nil, err
	}
	var entries []IndexEntry
	for _, rec := range strings.Split(string(out), "\x00") {
		// 一条记录的格式是：<mode> SP <object> SP <stage> TAB <path>
		tab := strings.IndexByte(rec, '\t')
		if tab < 0 {
			continue
		}
		fields := strings.Fields(rec[:tab])
		if len(fields) == 0 {
			continue
		}
		entries = append(entries, IndexEntry{Mode: fields[0], Path: rec[tab+1:]})
	}
	return entries, nil
}

// StagedUnder 报告某个前缀下有没有**已暂存**的改动。
func (r *Repo) StagedUnder(relPrefix string) bool {
	if !r.HasHEAD() {
		// 还没有任何提交时，index 里的每一条都算"已暂存"
		return r.Tracked(relPrefix)
	}
	out, err := query(r.root, "diff", "--cached", "--name-only", "--", relPrefix)
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// HooksDir 返回 hook 该装到哪。
//
// 用 git rev-parse --git-path hooks 而不是手拼 .git/hooks：实测它同时正确处理
// worktree（返回主仓库共享的那个）、submodule（.git 是文件）、以及
// core.hooksPath 被 husky / lefthook 设过的情形。手拼那三种全错。
//
// 它是本包**唯一**要读使用者真实配置的查询：core.hooksPath 可能设在全局配置里，
// 把全局配置屏蔽掉就会装到 .git/hooks，而 git 实际上根本不看那儿——
// hook 装了却永不运行，且没有任何迹象。
func (r *Repo) HooksDir() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--git-path", "hooks")
	cmd.Dir = r.root
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("定位 hooks 目录失败：%w", err)
	}
	dir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(r.root, dir)
	}
	return dir, nil
}

// query 在仓库里跑一条只读的 git 命令。
//
// 不读使用者的全局 / 系统配置：别人的 core.* 设置不该影响这些查询
// （与 workspace.gitOut 同一个立场）。唯一的例外是 HooksDir，理由写在它上面。
func query(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0")
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s：%w（%s）",
			strings.Join(args, " "), err, strings.TrimSpace(errBuf.String()))
	}
	return out.Bytes(), nil
}
