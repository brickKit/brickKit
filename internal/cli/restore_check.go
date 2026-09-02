package cli

// 本文件实现提交前的结构自洽判据（brickkit restore --check）。
//
// 它回答一个问题：**这次提交里，brickkit.yaml 与组件源码结构自洽吗。**
//
// 判据分成两半，本文件上半是**纯函数**（不碰 git、不碰磁盘），下半才接线。
// 分开是因为这个判据要硬拦人：3 × 4 的状态表必须逐格测到，而带上 git 与
// Manifest 解析之后，写全那 12 格的代价会高到没人愿意写。

import (
	"context"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
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

// runRestoreCheck 执行 brickkit restore --check。
//
// # 它读哪两份东西，以及为什么不能读别处
//
//	即将提交的 yaml    → index（git show :<rel>）
//	即将提交的结构      → index（git ls-files --cached --stage）
//
// 读 HEAD 的 yaml 会造出一个真死锁：yaml 的改动永远比结构晚一拍，于是
// "改 yaml + 归档结构一起提交"这件事**永远做不成**。读工作区的结构则会管到
// 没 git add 的东西——那些不进这次提交，不该拦。
//
// # 四种情形放行，绝不拦
//
// 冲突中、配置没交给 git、全图算不出来、找不到自己——闸门是抓一个特定失误，
// 不是质量门。把提交堵死在一次网络错误上，代价远大于漏掉一次。
func runRestoreCheck(ctx context.Context, opts *Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)

	repo, err := gitrepo.Open(layout.Root)
	if err != nil {
		return nil // 不在 git 仓库里：没有"即将提交的东西"可判
	}
	if repo.Unmerged() {
		opts.Printf("⚠️  正在解决冲突，跳过组件结构检查\n")
		return nil
	}
	cfgRel, ok := repo.Rel(layout.ConfigPath())
	if !ok || !repo.Tracked(cfgRel) {
		return nil // 配置在仓库外、或没交给 git：管不着
	}
	compRel, ok := repo.Rel(layout.ComponentsDir())
	if !ok {
		return nil
	}

	// 只查这一次：判定、短路、gitlink 提醒三处共用同一份结果。
	// 分成多次查会让短路把 gitlink 提醒一起短路掉。
	entries, err := repo.IndexEntries(compRel)
	if err != nil {
		return skipCheck(opts, "读不到即将提交的组件目录结构", err)
	}
	if !hasArchivedEntry(entries, compRel) {
		// components/ 还在 .gitignore 里的默认情形走的就是这一条：零成本
		warnGitlinks(opts, gitlinkPaths(entries))
		return nil
	}

	data, err := repo.IndexBlob(cfgRel)
	if err != nil {
		return skipCheck(opts, "读不到即将提交的 "+layout.ConfigName(), err)
	}
	cfg, err := config.ParseConfig(data, "index:"+cfgRel)
	if err != nil {
		return skipCheck(opts, "即将提交的 "+layout.ConfigName()+" 解析不了", err)
	}
	f, err := syncFocus(ctx, opts, layout, cfg)
	if err != nil {
		// 算不出来 ≠ 判据不通过。Manifest 缺失或要联网时会走到这里。
		return skipCheck(opts, "算不出这次会启动哪些组件", err)
	}

	ids := declaredIDs(cfg)
	l := layoutFromIndex(entries, compRel, ids)
	warnGitlinks(opts, l.gitlinks)

	vs := judgeCommit(ids, f.keep, l)
	if len(vs) == 0 {
		return nil
	}
	return violationError(vs, compRel, layout.ConfigName())
}

// hasArchivedEntry 报告即将提交的东西里有没有归档目录下的路径。
func hasArchivedEntry(entries []gitrepo.IndexEntry, componentsRel string) bool {
	root := componentsRel + "/" + config.DirArchived
	for _, e := range entries {
		if under(e.Path, root) {
			return true
		}
	}
	return false
}

// gitlinkPaths 挑出 index 里的嵌套仓库指针路径。
func gitlinkPaths(entries []gitrepo.IndexEntry) []string {
	var paths []string
	for _, e := range entries {
		if e.IsGitlink() {
			paths = append(paths, e.Path)
		}
	}
	sort.Strings(paths)
	return paths
}

// skipCheck 说明为什么这次没检查，然后放行。
//
// 放行而不是拦：闸门守的是一个特定失误，不是"什么都得对"。堵死一次提交的
// 代价，远大于漏掉一次——尤其当原因是网络或缓存，与使用者正在做的事毫无关系。
func skipCheck(opts *Options, reason string, cause error) error {
	opts.Printf("%s", clierr.Warn(clierr.CodeConfigInvalid, "跳过组件结构检查："+reason).
		WithDetail("原因", cause.Error()).
		WithHint("这次提交照常进行；想手工确认就跑 brickkit restore --check").
		Format())
	return nil
}

// warnGitlinks 提醒嵌套的 Git 仓库进了提交。**只提醒，不改退出码。**
//
// 它超出"结构还原"的职责，但和"把 components/ 从 .gitignore 去掉"是同一个决定
// 引出来的坑：没有 .gitmodules 的 gitlink 不是指针，是个死记录。
// 004 §8.2 早就点过"会出现 Git 嵌套仓库的问题"，这里只是让它在真发生时说话。
func warnGitlinks(opts *Options, paths []string) {
	for _, p := range paths {
		opts.Printf("%s", clierr.Warn(clierr.CodeConfigInvalid,
			p+" 是一个嵌套的 Git 仓库（提交进去的只是一个指针）").
			WithHint(
				"仓库里没有 .gitmodules，队友 clone 下来只会得到一个空目录",
				"git submodule update 也拉不回来——没有地方记着它的 URL",
			).Format())
	}
}

// violationError 把违规清单变成那句该说的话。
//
// "两处都有"优先：它比"提交在归档目录里"更准，而且出路完全不同。两种同时存在时
// 先报它——修完再跑一次就看到另一种。
func violationError(vs []violation, componentsRel, configName string) error {
	archivedRoot := componentsRel + "/" + config.DirArchived

	var both, archived []string
	for _, v := range vs {
		if v.kind == violationBoth {
			both = append(both, v.componentID)
			continue
		}
		archived = append(archived, v.componentID)
	}

	if len(both) > 0 {
		e := clierr.New(clierr.CodeConfigConflict,
			"提交被拦下：同一个组件的源码在提交里出现了两处")
		for _, id := range both {
			e = e.WithDetail(id, componentsRel+"/"+id+"  与  "+archivedRoot+"/"+id)
		}
		return e.WithHint(
			"一个组件 ID 只能有一个源码目录（004 §8.1）",
			"多半是 git add 的路径太窄，漏掉了旧路径的删除：git add -A "+componentsRel+"/",
			"两处都有源码时，平台不替你决定保留哪一份",
		)
	}

	e := clierr.New(clierr.CodeConfigConflict,
		"提交被拦下：组件源码提交在归档目录里，但 "+configName+" 说它该启动")
	for _, id := range archived {
		e = e.WithDetail(id, "即将提交的位置："+archivedRoot+"/"+id)
	}
	return e.WithHint(
		"想保留这个归档结构 → git add "+configName+
			"（yaml 里的 enabled: false 进了提交，就是你的意图声明）",
		"不想 → brickkit restore，然后重新 git add",
	)
}
