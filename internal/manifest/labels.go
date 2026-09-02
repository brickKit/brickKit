package manifest

// 本文件管 `labels` 这个**透传口**（002 §4.7、003 §4.11）。
//
// # 平台不解释键值，只透传
//
// Traefik / Caddy / Prometheus / Loki 这一整类工具的标准接入方式就是读容器
// labels。平台不做网关（012 §2.1、005 §5.11），这条立场本身没问题——问题在于
// 平台**同时**没有给出透传口，于是"不做网关"变成了"也不让你自己做"：
// 使用者只能退回去手写一份 file-provider 配置，而那份配置里必须写满**版本化
// 服务名**（`erp-sales-1-0-0`），组件每升一次版本就要同步改一遍，
// 而平台一个字都不会提醒。60 个组件时那份手写配置是必然会腐烂的那一份。
//
// 所以这个口子恰恰**让平台可以继续不理解网关**，而不是相反。详见 012 §2.23。
//
// # 唯一的校验：不许覆盖平台自己拥有的键
//
// 与 `deployment.resources`「CLI 透传，不校验数值合理性」是同一种姿态——
// 键名拼错、值写得没意义，都由下游工具自己报。平台只拦一件事：
// 把自己赖以工作的键覆盖掉。撞了**当场报错**，不静默丢弃：
// 静默丢弃的症状是"labels 写了但没生效"，而那正是最难查的一类。

import (
	"sort"
	"strings"
)

// 平台自己拥有的 labels / annotations 键。
//
// ⚠️ 这几个值必须与 internal/k8s 里的标签常量保持一致。反向不能靠 import
// （k8s 依赖 manifest，倒过来会成环），所以由 k8s 侧的
// TestPlatformOwnedKeysAreReserved 盯着两边不许分叉——那条测试直接拿 k8s
// 的常量来问 ReservedLabelKey，答不出"保留"就红。
const (
	// reservedLabelPrefix 是平台的标签命名空间：
	// brickkit.io/component、component-version、project、role、component-id。
	reservedLabelPrefix = "brickkit.io/"
	// reservedComposePrefix 是 compose 自己写的那一套（项目名、服务名、
	// config-hash…）。覆盖它们会让 `docker compose` 认不出自己生成的东西。
	reservedComposePrefix = "com.docker.compose."
	// reservedAppLabel 是 K8s 下 Deployment 找到自己 Pod 的唯一依据，
	// 也是 NetworkPolicy 的匹配依据（005 §5.3、§5.13.1）。
	reservedAppLabel = "app"
)

// ReservedLabelKey 判断这个键是不是平台自己拥有的，是就返回**该说的理由**，
// 不是则返回空串。
//
// 返回理由而不是布尔：使用者需要知道的不是"不许写"，而是"为什么这一个不许写"
// ——`app` 和 `brickkit.io/component` 不许写的原因根本不是同一个。
func ReservedLabelKey(key string) string {
	switch {
	case strings.HasPrefix(key, reservedLabelPrefix):
		return "`" + reservedLabelPrefix + "` 是平台自己的命名空间（组件 ID、版本、项目名都记在这里），不能透传"
	case strings.HasPrefix(key, reservedComposePrefix):
		return "`" + reservedComposePrefix + "` 是 docker compose 自己写的标签，覆盖后 compose 认不出自己生成的容器"
	case key == reservedAppLabel:
		return "`" + reservedAppLabel + "` 是 K8s 下 Deployment 找到自己 Pod 的唯一依据，也是 NetworkPolicy 的匹配依据（005 §5.3、§5.13.1）"
	}
	return ""
}

// ValidateLabels 校验一组透传 labels，path 是它在 YAML 里的位置。
//
// 只查两件事：键不能空、键不能是平台自己拥有的。值一个字都不看。
func ValidateLabels(labels map[string]string, path string, add func(field, message string)) {
	for _, key := range sortedKeys(labels) {
		field := path + "." + key
		if strings.TrimSpace(key) == "" {
			add(path, "标签的键不能为空")
			continue
		}
		if reason := ReservedLabelKey(key); reason != "" {
			add(field, "这个键归平台所有，不能透传："+reason)
		}
	}
}

// MergeLabels 按"后来的覆盖先前的"逐键合并，全空时返回 nil。
//
// 逐键合并而不是整块覆盖，与 mergeResources 同一个理由（004 §5.6.2）：
// 使用者常常只想加一条路由规则，不该因此把组件作者写的抓取路径一起丢掉。
//
// 全空返回 nil 而不是空 map：渲染器据此判断"这一段要不要生成"。
func MergeLabels(layers ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, layer := range layers {
		for key, value := range layer {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// sortedKeys 让"多个键都撞了"时的报错顺序稳定——map 的遍历顺序是随机的，
// 不排的话同一份 Manifest 连跑两次会给出不同顺序的问题清单。
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
