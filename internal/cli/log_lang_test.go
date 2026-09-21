package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// JSON 日志的 message 跟着语言走；键名（command、error_code……）是机器读的
// 稳定接口，任何语言下都是英文。

func TestLogMessagesAreEnglishByDefault(t *testing.T) {
	r := run(t, "version")
	assert.Contains(t, r.stderr, `"message":"Command started"`)
	assert.Contains(t, r.stderr, `"message":"Command finished"`)
	assert.NotContains(t, r.stderr, "命令开始执行")
}

func TestLogMessagesAreChineseWhenBrickkitLangIsZH(t *testing.T) {
	t.Setenv("BRICKKIT_LANG", "zh")
	r := run(t, "version")
	assert.Contains(t, r.stderr, `"message":"命令开始执行"`)
	assert.Contains(t, r.stderr, `"message":"命令执行完成"`)
	assert.NotContains(t, r.stderr, "Command started")
}

func TestFailureLogLocalizesMessageButKeepsErrorCodeStable(t *testing.T) {
	en := run(t, "up")
	assert.Contains(t, en.stderr, `"message":"Command failed"`)
	assert.Contains(t, en.stderr, `"error_code":"PROJECT_MISSING"`)

	t.Setenv("BRICKKIT_LANG", "zh")
	zh := run(t, "up")
	assert.Contains(t, zh.stderr, `"message":"命令执行失败"`)
	assert.Contains(t, zh.stderr, `"error_code":"PROJECT_MISSING"`, "脚本按 error_code 分支，它不能随语言变")
}
