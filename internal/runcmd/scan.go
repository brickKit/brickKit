package runcmd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// scan 是一次探测的上下文：要看的目录、目标操作系统、运行参数。
// 适配器只通过它读文件系统，所以"Windows 那一列命令"能在 Linux 上靠 goos 字段单独测。
type scan struct {
	dir    string
	goos   string
	params Params
	// entries 是目录根下的条目，探测入口读一次，之后按扩展名找文件都用它。
	entries []os.DirEntry
}

// outcome 是一个适配器的探测结果，三种互斥的情形：
//   - ok：给出了命令；
//   - problem 非空：认得出这种语言（标记文件在），但给不出命令；
//   - 两者皆空：目录里没有这种语言。
type outcome struct {
	cmd     Command
	ok      bool
	problem *Problem
}

func (s *scan) windows() bool { return s.goos == "windows" }

func (s *scan) port() string { return strconv.Itoa(s.params.Port) }

func (s *scan) path(rel string) string { return filepath.Join(s.dir, filepath.FromSlash(rel)) }

func (s *scan) isFile(rel string) bool {
	info, err := os.Stat(s.path(rel))
	return err == nil && info.Mode().IsRegular()
}

func (s *scan) executable(rel string) bool {
	info, err := os.Stat(s.path(rel))
	return err == nil && info.Mode()&0o111 != 0
}

// read 读一个标记文件。present 为 false 表示文件不存在（语言不在场）；
// present 为 true 而 err 非空表示文件在、却读不了。
func (s *scan) read(rel string) (text string, present bool, err error) {
	data, err := os.ReadFile(s.path(rel))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", true, err
	}
	return string(data), true, nil
}

// namesWithExt 列出目录根下扩展名匹配的普通文件名（已排序）。
// 不用 filepath.Glob：目录路径里若有 `[`、`*` 之类，Glob 会把它们当成通配符。
func (s *scan) namesWithExt(exts ...string) []string {
	var names []string
	for _, e := range s.entries {
		if !e.Type().IsRegular() {
			continue
		}
		for _, ext := range exts {
			if strings.EqualFold(filepath.Ext(e.Name()), ext) {
				names = append(names, e.Name())
				break
			}
		}
	}
	sort.Strings(names)
	return names
}

// subdirs 列出 rel 下的子目录名（已排序）；rel 不存在则为空。
func (s *scan) subdirs(rel string) []string {
	entries, err := os.ReadDir(s.path(rel))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func (s *scan) ok(argv []string, evidence ...string) outcome {
	return outcome{ok: true, cmd: Command{Argv: argv, Dir: s.dir, Evidence: evidence, Detected: true}}
}

func problemOutcome(reason Reason, detail string, options ...string) outcome {
	return outcome{problem: &Problem{Reason: reason, Detail: detail, Options: options}}
}

// wrapper 选出 Maven / Gradle 的启动前缀：项目自带的 wrapper 脚本优先（版本由项目锁定），
// 没有才退回 PATH 里的全局命令。返回前缀命令，以及用到的 wrapper 文件名（没用则为空）。
//
// Unix 上 wrapper 丢了可执行位很常见（Windows 上 clone、zip 解压都会），这时改用 sh 去跑它，
// 而不是让用户撞上一句 "permission denied"。
func (s *scan) wrapper(unix, windows, global string) ([]string, string) {
	if s.windows() {
		if s.isFile(windows) {
			return []string{s.path(windows)}, windows
		}
		return []string{global}, ""
	}
	if s.isFile(unix) {
		if s.executable(unix) {
			return []string{s.path(unix)}, unix
		}
		return []string{"sh", s.path(unix)}, unix
	}
	return []string{global}, ""
}
