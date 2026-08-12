// Package logging 提供 BrickKit CLI 的结构化日志。
//
// 设计依据：开发计划 Step 2 任务 4「日志模块：结构化 JSON 输出到 stderr」。
//
// 分工约定（很重要）：
//
//	stdout  面向用户的人类可读输出（✅ / 📦 / 表格等，见 004 各命令输出示例）
//	stderr  结构化 JSON 日志（诊断用）+ 错误块（clierr 渲染结果）
//
// 这样 `brickkit order > order.txt` 只会拿到人类可读内容，日志不会混进去。
//
// 日志字段固定包含 time / level / message 三个键（开发计划验证项 2.6）。
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// 日志级别名称，供 --log-level 使用。
const (
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
	LevelOff   = "off"
)

// EnvLogLevel 是覆盖默认日志级别的环境变量。
const EnvLogLevel = "BRICKKIT_LOG_LEVEL"

// levelOff 高于任何真实级别，用于彻底关闭日志。
const levelOff = slog.Level(64)

var (
	mu     sync.RWMutex
	logger *slog.Logger
	level  = new(slog.LevelVar)
)

func init() {
	Init(os.Stderr, LevelInfo)
}

// Init 重新初始化日志输出目标与级别。levelName 非法时回退到 info。
func Init(w io.Writer, levelName string) {
	mu.Lock()
	defer mu.Unlock()
	level.Set(ParseLevel(levelName))
	logger = slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: replaceAttr,
	}))
}

// SetLevel 只调整级别，不改变输出目标。
func SetLevel(levelName string) {
	level.Set(ParseLevel(levelName))
}

// ParseLevel 把级别名转换为 slog.Level，无法识别时返回 info。
func ParseLevel(name string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case LevelDebug:
		return slog.LevelDebug
	case LevelInfo, "":
		return slog.LevelInfo
	case LevelWarn, "warning":
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	case LevelOff, "none", "silent":
		return levelOff
	default:
		return slog.LevelInfo
	}
}

// LevelNames 返回所有合法级别名（供 --help 展示）。
func LevelNames() []string {
	return []string{LevelDebug, LevelInfo, LevelWarn, LevelError, LevelOff}
}

// IsValidLevel 判断级别名是否合法。
func IsValidLevel(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case LevelDebug, LevelInfo, LevelWarn, LevelError, LevelOff, "warning", "none", "silent":
		return true
	default:
		return false
	}
}

// replaceAttr 把 slog 默认的 "msg" 键改名为 "message"，
// 使日志字段与开发计划验证项 2.6 要求的 time / level / message 一致。
func replaceAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.MessageKey {
		a.Key = "message"
	}
	return a
}

// Logger 返回当前 slog.Logger。
func Logger() *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return logger
}

func log(l slog.Level, msg string, args ...any) {
	lg := Logger()
	if !lg.Enabled(context.Background(), l) {
		return
	}
	lg.Log(context.Background(), l, msg, args...)
}

// Debug 记录调试日志。args 为交替出现的 key/value。
func Debug(msg string, args ...any) { log(slog.LevelDebug, msg, args...) }

// Info 记录信息日志。
func Info(msg string, args ...any) { log(slog.LevelInfo, msg, args...) }

// Warn 记录警告日志。
func Warn(msg string, args ...any) { log(slog.LevelWarn, msg, args...) }

// Error 记录错误日志。
func Error(msg string, args ...any) { log(slog.LevelError, msg, args...) }
