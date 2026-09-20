package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/userconfig"
)

func TestLangShowsDefaultWhenNothingConfigured(t *testing.T) {
	r := run(t, "lang")
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "en")
	assert.Contains(t, r.stdout, "default")
}

func TestLangReflectsEnvOverride(t *testing.T) {
	t.Setenv("BRICKKIT_LANG", "zh")
	r := run(t, "lang")
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "zh")
}

func TestLangSetPersistsAcrossInvocations(t *testing.T) {
	// 用同一个全局配置目录跑两次调用，验证"设置后下次调用还生效"；
	// run() 只在调用方还没设置过 EnvDirOverride 时才会自己隔离一个，
	// 这里先设置好，run() 就不会覆盖它。
	t.Setenv(userconfig.EnvDirOverride, t.TempDir())

	setResult := run(t, "lang", "set", "zh")
	assert.Equal(t, clierr.ExitOK, setResult.code)
	assert.Contains(t, setResult.stdout, "zh")

	showResult := run(t, "lang")
	assert.Equal(t, clierr.ExitOK, showResult.code)
	assert.Contains(t, showResult.stdout, "zh")
	// 语言已经被设成 zh：这次调用的输出（包括"来源"这个标签本身）
	// 理应整句都用中文渲染，不是只翻一半——所以这里断言中文标签，
	// 不是英文的 "global config file"。
	assert.Contains(t, showResult.stdout, "全局配置文件")
}

func TestLangSetRejectsUnsupportedValue(t *testing.T) {
	r := run(t, "lang", "set", "fr")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "fr")
	assert.Contains(t, r.stderr, "en")
	assert.Contains(t, r.stderr, "zh")
}

func TestLangSetRequiresExactlyOneArg(t *testing.T) {
	r := run(t, "lang", "set")
	assert.Equal(t, clierr.ExitUsage, r.code)
}
