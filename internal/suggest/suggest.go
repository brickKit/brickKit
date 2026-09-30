// Package suggest 在一串已知名字里找与使用者打错的那个最接近的几个，用来在报错里说
// "你是不是想写 …"。只是建议：命令从不拿猜出来的名字去执行（设计 §7.2）。
package suggest

import (
	"slices"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// maxSuggestions 是最多给出的候选数：再多就不是提示，是清单了。
const maxSuggestions = 3

// maxDistance 是算作"打错了几个字"的最大编辑距离。
const maxDistance = 2

// Similar 返回 candidates 里与 typed 相近的至多三个，按这个优先级：包含 typed 的；
// 名字部分（/ 之后）与 typed 的名字部分相同的；编辑距离不超过 2 的。同一级按字母序，
// 与 typed 完全相同的不算。
func Similar(typed string, candidates []string) []string {
	var tiers [3][]string
	name := namePart(typed)
	for _, c := range candidates {
		switch {
		case c == typed:
		case strings.Contains(c, typed):
			tiers[0] = append(tiers[0], c)
		case namePart(c) == name:
			tiers[1] = append(tiers[1], c)
		case EditDistance(c, typed) <= maxDistance:
			tiers[2] = append(tiers[2], c)
		}
	}
	var out []string
	for _, tier := range tiers {
		sort.Strings(tier)
		out = append(out, tier...)
	}
	if len(out) > maxSuggestions {
		out = out[:maxSuggestions]
	}
	return out
}

func namePart(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

// EditDistance 是标准的 Levenshtein 距离。
func EditDistance(a, b string) int {
	if a == b {
		return 0
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

// Hint 是报错里"你是不是想写 …"那一句；typed 可以带 @版本（只拿 ID 去比）。
// ID 本身就在候选里、或者没有相近的候选时 ok 为 false，调用方就不加这条提示。
func Hint(typed string, candidates []string) (string, bool) {
	id, _, _ := strings.Cut(typed, "@")
	if slices.Contains(candidates, id) {
		return "", false // ID 写对了，错的是别处（版本之类）：这时给别的名字只会误导
	}
	similar := Similar(id, candidates)
	if len(similar) == 0 {
		return "", false
	}
	return i18n.T(msgid.HintDidYouMean, strings.Join(similar, i18n.T(msgid.ListSeparator))), true
}
