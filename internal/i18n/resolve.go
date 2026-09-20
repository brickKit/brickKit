package i18n

import (
	"os"
	"strings"

	"github.com/brickkit/brickkit/internal/userconfig"
)

// EnvLang 是覆盖当前语言的环境变量，优先级最高——适合 CI 与一次性调用
// （BRICKKIT_LANG=en brickkit up），不需要 --lang 命令行参数：语言必须
// 在 cobra 搭命令树之前就确定，命令行参数要等 cobra 解析完才能拿到值。
const EnvLang = "BRICKKIT_LANG"

// Source 标记一次语言解析结果来自哪一层，供 `brickkit lang` 展示给用户。
type Source string

const (
	SourceEnv     Source = "env"
	SourceConfig  Source = "config"
	SourceDefault Source = "default"
)

// ParseLang 校验字符串是否是已支持的语言，容忍大小写与前后空白。
func ParseLang(s string) (Lang, bool) {
	switch Lang(strings.ToLower(strings.TrimSpace(s))) {
	case EN:
		return EN, true
	case ZH:
		return ZH, true
	default:
		return "", false
	}
}

// SupportedLangs 返回全部已支持语言，顺序固定。
func SupportedLangs() []Lang {
	return []Lang{EN, ZH}
}

// LangNames 是 SupportedLangs 的字符串形式，用于拼错误提示。
func LangNames() []string {
	langs := SupportedLangs()
	names := make([]string, len(langs))
	for i, l := range langs {
		names[i] = string(l)
	}
	return names
}

// Resolve 按优先级解析当前应该使用的语言：
// BRICKKIT_LANG 环境变量 > 全局配置文件（brickkit lang set 写入）> 默认英语。
//
// BRICKKIT_LANG 或全局配置文件里的值如果不是已支持的语言，当作没设置、
// 继续往下一层找——这不是"静默吞掉一个会导致悄悄用错服务的坑"（§9.13
// 那一类），只是退回默认语言，用户会立刻从看到的文字里发现，代价很小，
// 换来的是不需要在命令树建好之前就有能力中断整个进程去报一个用法错误。
func Resolve() (Lang, Source) {
	if v := strings.TrimSpace(os.Getenv(EnvLang)); v != "" {
		if l, ok := ParseLang(v); ok {
			return l, SourceEnv
		}
	}
	if cfg, err := userconfig.Load(); err == nil && cfg != nil {
		if l, ok := ParseLang(cfg.Lang); ok {
			return l, SourceConfig
		}
	}
	return EN, SourceDefault
}
