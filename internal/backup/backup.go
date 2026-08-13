// Package backup 实现 brickkit.yaml 的自动备份与恢复。
//
// 设计依据：003 §7 配置备份与恢复、004 §3.10 brickkit reset。
//
// 备份时机（003 §7.1）：
//
//	brickkit init         → .brickkit/backup/<配置名>.initial   初始骨架
//	每次 add / remove 前  → .brickkit/backup/<配置名>.last      上一步操作前的状态
//
// 两个快照互不覆盖：.initial 永远是 init 时的骨架，.last 只表示"上一步之前"，
// 不是历史堆栈。恢复只改写配置文件本身，不动 .brickkit/ 下的其他内容。
package backup

import (
	"os"
	"path/filepath"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
)

// 文件权限与 internal/config 保持一致：配置与备份 0644，目录 0755。
const (
	filePerm = 0o644
	dirPerm  = 0o755
)

// Kind 是备份种类。
type Kind string

const (
	// Initial 是 brickkit init 生成的初始骨架备份。
	Initial Kind = "initial"
	// Last 是 add / remove 前生成的"上一步操作前"备份。
	Last Kind = "last"
)

// Label 返回该备份种类的中文说明，用于命令输出。
func (k Kind) Label() string {
	switch k {
	case Initial:
		return "初始状态"
	case Last:
		return "上一次备份"
	default:
		return string(k)
	}
}

// Record 是一次备份 / 恢复的结果，供命令层渲染输出。
type Record struct {
	// Kind 是备份种类。
	Kind Kind
	// Path 是备份文件的完整路径。
	Path string
	// RelPath 是相对项目根的备份路径（用于输出，如 .brickkit/backup/brickkit.yaml.initial）。
	RelPath string
	// ConfigPath 是配置文件的完整路径。
	ConfigPath string
}

// Path 返回某种备份的完整路径。
func Path(l config.Layout, kind Kind) string {
	switch kind {
	case Initial:
		return l.InitialBackupPath()
	case Last:
		return l.LastBackupPath()
	default:
		return ""
	}
}

// Exists 判断某种备份是否存在。
func Exists(l config.Layout, kind Kind) bool {
	path := Path(l, kind)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// SaveLast 把当前配置备份为 .last。add / remove 在**修改配置之前**调用（003 §7.1）。
func SaveLast(l config.Layout) (*Record, error) {
	return save(l, Last)
}

func save(l config.Layout, kind Kind) (*Record, error) {
	dst := Path(l, kind)
	if dst == "" {
		return nil, unknownKindError(kind)
	}

	data, err := os.ReadFile(l.ConfigPath())
	switch {
	case os.IsNotExist(err):
		return nil, clierr.Newf(clierr.CodeProjectMissing, "错误：项目未初始化：找不到 %s", l.ConfigName()).
			WithDetail("路径", l.ConfigPath()).
			WithHint(
				"在项目根目录执行本命令",
				"或先执行 brickkit init <项目名称> 初始化项目",
			).WithCause(err)
	case err != nil:
		return nil, ioError("读取配置", l.ConfigPath(), err)
	}

	if err := os.MkdirAll(filepath.Dir(dst), dirPerm); err != nil {
		return nil, ioError("创建备份目录", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, data, filePerm); err != nil {
		return nil, ioError("写入备份", dst, err)
	}
	return newRecord(l, kind, dst), nil
}

// Restore 用指定种类的备份覆盖当前配置（004 §3.10）。
//
// 恢复不校验备份内容：reset 正是"配置被改坏了"时的救急手段，
// 在这里加校验只会把唯一的退路也堵上。
func Restore(l config.Layout, kind Kind) (*Record, error) {
	src := Path(l, kind)
	if src == "" {
		return nil, unknownKindError(kind)
	}

	data, err := os.ReadFile(src)
	switch {
	case os.IsNotExist(err):
		return nil, missingBackupError(l, kind, err)
	case err != nil:
		return nil, ioError("读取备份", src, err)
	}

	if err := os.MkdirAll(filepath.Dir(l.ConfigPath()), dirPerm); err != nil {
		return nil, ioError("创建项目目录", filepath.Dir(l.ConfigPath()), err)
	}
	if err := os.WriteFile(l.ConfigPath(), data, filePerm); err != nil {
		return nil, ioError("写入配置", l.ConfigPath(), err)
	}
	return newRecord(l, kind, src), nil
}

func newRecord(l config.Layout, kind Kind, path string) *Record {
	return &Record{
		Kind:       kind,
		Path:       path,
		RelPath:    display(l, path),
		ConfigPath: l.ConfigPath(),
	}
}

// display 把路径转成相对项目根的形式，用于面向用户的输出。
func display(l config.Layout, path string) string {
	rel, err := filepath.Rel(l.Root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func missingBackupError(l config.Layout, kind Kind, cause error) error {
	e := clierr.New(clierr.CodeBackupMissing, "错误：备份文件不存在").
		WithDetail("备份文件", display(l, Path(l, kind))).
		WithCause(cause)

	switch kind {
	case Last:
		return e.
			WithDetail("原因", ".last 在每次 brickkit add / brickkit remove 前自动生成，当前还没有生成过").
			WithHint(
				"先执行一次 brickkit add / brickkit remove，之后即可用 brickkit reset --last 回退",
				"或执行 brickkit reset 恢复到初始状态",
			)
	default:
		return e.
			WithDetail("原因", ".initial 由 brickkit init 生成，可能被误删或当前目录不是项目根目录").
			WithHint(
				"确认在项目根目录执行本命令（--config 指定的配置名要与备份名一致）",
				"如果备份已被删除，请从版本库恢复 "+l.ConfigName(),
			)
	}
}

func unknownKindError(kind Kind) error {
	return clierr.Newf(clierr.CodeInternal, "错误：未知的备份类型：%s", kind)
}

func ioError(action, path string, cause error) error {
	return clierr.Newf(clierr.CodeConfigInvalid, "错误：%s失败", action).
		WithDetail("路径", path).
		WithDetail("原因", cause.Error()).
		WithHint("检查文件与目录权限").
		WithCause(cause)
}
