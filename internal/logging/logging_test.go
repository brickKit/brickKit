package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decodeLines(t *testing.T, s string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m), "日志行必须是合法 JSON：%q", line)
		out = append(out, m)
	}
	return out
}

// 开发计划验证项 2.6：日志为 JSON 格式，包含 time / level / message。
func TestLogIsJSONWithRequiredKeys(t *testing.T) {
	var buf bytes.Buffer
	Init(&buf, LevelInfo)
	Info("命令开始执行", "command", "brickkit version")

	lines := decodeLines(t, buf.String())
	require.Len(t, lines, 1)
	entry := lines[0]
	for _, key := range []string{"time", "level", "message"} {
		assert.Contains(t, entry, key, "日志必须包含 %s 字段", key)
	}
	assert.Equal(t, "命令开始执行", entry["message"])
	assert.Equal(t, "INFO", entry["level"])
	assert.Equal(t, "brickkit version", entry["command"])
	// slog 默认的 msg 键必须已被改名为 message。
	assert.NotContains(t, entry, "msg")
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	Init(&buf, LevelWarn)
	Debug("debug 不应输出")
	Info("info 不应输出")
	Warn("warn 应输出")
	Error("error 应输出")

	lines := decodeLines(t, buf.String())
	require.Len(t, lines, 2)
	assert.Equal(t, "warn 应输出", lines[0]["message"])
	assert.Equal(t, "error 应输出", lines[1]["message"])
}

func TestLevelOffSilencesEverything(t *testing.T) {
	var buf bytes.Buffer
	Init(&buf, LevelOff)
	Debug("x")
	Info("x")
	Warn("x")
	Error("x")
	assert.Empty(t, buf.String())
}

func TestSetLevelKeepsWriter(t *testing.T) {
	var buf bytes.Buffer
	Init(&buf, LevelError)
	Info("被过滤")
	SetLevel(LevelDebug)
	Debug("现在可见")

	lines := decodeLines(t, buf.String())
	require.Len(t, lines, 1)
	assert.Equal(t, "现在可见", lines[0]["message"])
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"INFO":    slog.LevelInfo,
		"":        slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"不认识":     slog.LevelInfo,
	}
	for in, want := range cases {
		assert.Equal(t, want, ParseLevel(in), "ParseLevel(%q)", in)
	}
	assert.Equal(t, levelOff, ParseLevel("off"))
}

func TestIsValidLevel(t *testing.T) {
	for _, name := range LevelNames() {
		assert.True(t, IsValidLevel(name), name)
	}
	assert.True(t, IsValidLevel("WARNING"))
	assert.False(t, IsValidLevel("verbose"))
	assert.False(t, IsValidLevel(""))
}
