package cli

// 本文件实现提交前的结构自洽判据（brickkit restore --check）。
//
// 它回答一个问题：**这次提交里，brickkit.yaml 与组件源码结构自洽吗。**
//
// 判据分成两半，本文件上半是**纯函数**（不碰 git、不碰磁盘），下半才接线。
// 分开是因为这个判据要硬拦人：3 × 4 的状态表必须逐格测到，而带上 git 与
// Manifest 解析之后，写全那 12 格的代价会高到没人愿意写。

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/gitrepo"
)

// commitLayout 是"即将提交的那份目录结构"按**组件 ID** 折出来的结果。
//
// 按 ID 而不是按 (id, version) 条目：一个组件 ID 只有一份源码目录（004 §8.1），
// 同 ID 的多个版本共用它。按条目判会让"同 ID 一个版本跑、一个不跑"自相矛盾。
type commitLayout struct {
	// active[id] 为真表示 index 里 <components>/<id>/ 下有东西。
	active map[string]bool
	// archived[id] 为真表示 index 里 <components>/.archived/<id>/ 下有东西。
	archived map[string]bool
	// gitlinks 是 index 里的嵌套仓库指针路径（相对仓库根，已排序）。
	gitlinks []string
}

// layoutFromIndex 把 index 记录折成 commitLayout。
//
// ids 是 brickkit.yaml 里声明过的组件 ID。**没声明的一律不管**：判定算不到它，
// 那是使用者自己在开发、还没 add 的源码（与 planSync 同一个边界）。
func layoutFromIndex(entries []gitrepo.IndexEntry, componentsRel string, ids []string) commitLayout {
	l := commitLayout{active: map[string]bool{}, archived: map[string]bool{}}
	archivedRoot := componentsRel + "/" + config.DirArchived
	for _, e := range entries {
		if e.IsGitlink() {
			l.gitlinks = append(l.gitlinks, e.Path)
		}
		for _, id := range ids {
			switch {
			case under(e.Path, archivedRoot+"/"+id):
				l.archived[id] = true
			case under(e.Path, componentsRel+"/"+id):
				l.active[id] = true
			}
		}
	}
	sort.Strings(l.gitlinks)
	return l
}

// under 报告 path 是不是 prefix 本身、或 prefix 下的东西。
//
// "prefix 本身"这一支是给嵌套仓库用的：它在 index 里是一条 160000 记录，
// 路径就是那个目录本身，**没有尾斜杠**（实测 components/erp/backend）。
// 而拼上 "/" 再比是为了不把 backend2 当成 backend 的孩子。
func under(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// violationKind 是一次提交里两种自洽性问题。
type violationKind string

const (
	// violationArchived 是"判定说它该跑，源码却提交在归档目录里"——那个反复发生的失误。
	violationArchived violationKind = "archived"
	// violationBoth 是"同一个组件的源码在提交里出现了两处"。
	violationBoth violationKind = "both"
)

// violation 是一条违规。
type violation struct {
	componentID string
	kind        violationKind
}

// judgeCommit 得出违规清单。纯函数：不碰 git，也不碰磁盘。
//
// # 为什么只拦一个方向
//
// 反方向——源码在活跃目录、而 yaml 说它不跑——只是"没跑过 sync"。004 §3.9
// 明说 sync 是可选的（"用户忘记执行就自己发现、自己处理"），拦它等于强迫
// 全员跑 sync，影响面大得多。
//
// 而"归档结构 + enabled: false 一起进了提交"同样放行：那是使用者的**意图声明**。
// 他要这个结构，平台没有立场替他改主意。
//
// # 为什么"两处都有"是独立的一条，且不看判定结果
//
// 它不是主判据的特例，它是一个**死循环的解药**。这种状态下 workspace.Locate
// 活跃优先，planSync 判它"已经在该在的位置"、什么都不做——restore 跑完什么都没变，
// 而闸门还在拦，且没有任何出路。所以它必须先判、单独报、并给出手工出路。
func judgeCommit(ids []string, running map[string]bool, l commitLayout) []violation {
	var vs []violation
	for _, id := range ids {
		switch {
		case l.active[id] && l.archived[id]:
			vs = append(vs, violation{id, violationBoth})
		case running[id] && l.archived[id]:
			vs = append(vs, violation{id, violationArchived})
		}
	}
	return vs
}
