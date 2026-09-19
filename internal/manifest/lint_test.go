package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// configSchema.properties.<key> 声明里拼错的键会被解析器静默丢掉——defualt 拼错，
// 组件就拿不到默认值，而且没有任何东西说一声。作者该在这里听到。
func TestPropertyKeyWarningsCatchesTypoAndGuesses(t *testing.T) {
	warnings := PropertyKeyWarnings([]byte(minimalYAML+`
configSchema:
  type: object
  properties:
    poolSize:
      type: integer
      defualt: 10
`), "component.yaml")

	require.Len(t, warnings, 1)
	assert.True(t, warnings[0].Warning, "只能是警告，不能阻断")

	text := warnings[0].Format()
	assert.Contains(t, text, "configSchema.properties.poolSize")
	assert.Contains(t, text, "defualt")
	assert.Contains(t, text, "default", "要猜出作者想写的键")
}

// 认识的键一个都不该被报——包括新加的 minimum / maximum / pattern。
func TestPropertyKeyWarningsQuietForKnownKeys(t *testing.T) {
	warnings := PropertyKeyWarnings([]byte(minimalYAML+`
configSchema:
  type: object
  properties:
    dbPort:
      type: integer
      default: 5432
      description: 端口
      minimum: 1
      maximum: 65535
    mode:
      type: string
      enum: [a, b]
      pattern: "^[ab]$"
    hosts:
      type: array
      items:
        type: string
    apiKey:
      type: string
      secret: true
`), "component.yaml")

	assert.Empty(t, warnings)
}

// 属性名本身是作者自己定的，从来不查；没有 configSchema 也不该有任何动静。
func TestPropertyKeyWarningsIgnoresAbsentOrArbitraryNames(t *testing.T) {
	assert.Empty(t, PropertyKeyWarnings([]byte(minimalYAML), "component.yaml"))
	assert.Empty(t, PropertyKeyWarnings([]byte(minimalYAML+`
configSchema:
  type: object
  properties:
    anything-goes_here.42:
      type: string
`), "component.yaml"))
}

// 多个属性、多处笔误合成**一条**警告，逐条列出，而不是刷屏。
func TestPropertyKeyWarningsAggregatesIntoOne(t *testing.T) {
	warnings := PropertyKeyWarnings([]byte(minimalYAML+`
configSchema:
  type: object
  properties:
    a:
      type: string
      descripton: 拼错了
    b:
      type: integer
      format: int64
`), "component.yaml")

	require.Len(t, warnings, 1)
	text := warnings[0].Format()
	assert.Contains(t, text, "configSchema.properties.a")
	assert.Contains(t, text, "configSchema.properties.b")
}

// 这是建议性检查，不是 Parse 的一部分：Parse 对同一份文本的行为不能变
// （已发布组件里带着多余键的 Manifest 照样得装得上）。
func TestParseStillAcceptsUnknownPropertyKeys(t *testing.T) {
	_, err := Parse([]byte(minimalYAML+`
configSchema:
  type: object
  properties:
    poolSize:
      type: integer
      defualt: 10
`), "component.yaml")
	require.NoError(t, err)
}
