// Package skills 管理装进用户项目的 AI 助手技能资产。
//
// 资产以纯文本躺在 assets/ 下，用 //go:embed 编进二进制：BrickKit CLI 是
// 单二进制、用完即走、离线可用的，技能不该需要一次网络往返才拿得到。
// 版本严格跟着 CLI 走也正是想要的语义——那份文件描述的就是这个版本的行为。
package skills

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"path"
	"strings"
)

//go:embed assets
var assetFS embed.FS

// assetRoot 是内嵌资产在 embed.FS 里的前缀。
const assetRoot = "assets"

// Asset 是一份内嵌资产：内嵌路径与它在用户项目里的落点。
type Asset struct {
	// Source 是 embed.FS 里的路径，如 assets/claude/skills/x/SKILL.md。
	Source string
	// Target 是项目内的相对路径，如 .claude/skills/x/SKILL.md。
	Target string
}

// Content 返回内嵌内容。
func (a Asset) Content() ([]byte, error) {
	return assetFS.ReadFile(a.Source)
}

// Assets 返回全部资产，按落点排序（输出与 lock 顺序都要稳定）。
//
// 清单从 embed.FS 遍历得来而不是写死名单：assets/ 下加一个文件就自动纳入，
// 免得「加了文件忘了登记」——那种漏法不报错，只是静默少装一份。
func Assets() []Asset {
	var list []Asset
	walk(assetRoot, &list)
	return list
}

func walk(dir string, list *[]Asset) {
	entries, err := assetFS.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		p := path.Join(dir, e.Name())
		if e.IsDir() {
			walk(p, list)
			continue
		}
		*list = append(*list, Asset{Source: p, Target: targetOf(p)})
	}
}

// targetOf 把内嵌路径映射成项目内落点。
//
// assets/claude/ 这一层对应项目里的 .claude/：embed 不接受以点开头的目录，
// 所以内嵌侧只能叫 claude/，映射时补上那个点。
func targetOf(source string) string {
	rel := strings.TrimPrefix(source, assetRoot+"/")
	if after, ok := strings.CutPrefix(rel, "claude/"); ok {
		return ".claude/" + after
	}
	return rel
}

// Sum 返回内容的 sha256，形如 sha256:<十六进制>。
func Sum(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

// 文件权限：与 internal/config 保持一致（配置 0644、目录 0755）。
const (
	filePerm = 0o644
	dirPerm  = 0o755
)
