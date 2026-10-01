package skills

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/brickkit/brickkit/internal/agentsmd"
	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// State 是一份托管文件的当前状态。
type State string

// 取值是语言中立的标识（比较、分流用），从不直接显示；要给人看走 Label()。
const (
	// StateMissing 文件不存在。
	StateMissing State = "missing"
	// StateCurrent 内容已与当前版本的资产一致。
	StateCurrent State = "current"
	// StateOutdated 内容是我们上次写的，但资产已经变了。
	StateOutdated State = "outdated"
	// StateModified 内容与我们上次写的不一致——用户改过。
	StateModified State = "modified"
	// StateUntracked 文件存在但不带记录（也不是旧版 CLI 写的那份）。
	StateUntracked State = "untracked"
)

// Label 是给人看的状态名，跟着当前语言走。
func (s State) Label() string {
	switch s {
	case StateMissing:
		return i18n.T(msgid.SkillsStateMissing)
	case StateCurrent:
		return i18n.T(msgid.SkillsStateCurrent)
	case StateOutdated:
		return i18n.T(msgid.SkillsStateOutdated)
	case StateModified:
		return i18n.T(msgid.SkillsStateModified)
	case StateUntracked:
		return i18n.T(msgid.SkillsStateUntracked)
	}
	return string(s)
}

// writable 判断这个状态下是否允许写入。
// 只有这两种状态可写，其余一律不动——尤其是「已手改」与「未托管」。
func (s State) writable() bool {
	return s == StateMissing || s == StateOutdated
}

// FileStatus 是一份托管文件的状态。
type FileStatus struct {
	// Target 是项目内相对路径。
	Target string
	// State 是当前状态。
	State State
	// FromVersion 仅在 StateOutdated 时可能有值：文件记录（或旧版 lock）里写着的旧版本。
	FromVersion string
}

// Installer 把内嵌资产装进一个项目。
//
// 每份装进去的文件自己带着记录（文件最后一行，见 marker.go）：哪个版本的 CLI 写的、写的时候正文的指纹。
// 记录跟着文件进 Git，同事新克隆下来也分得清"我们写的、没改过"与"被手改过"。
type Installer struct {
	// Root 是项目根目录。
	Root string
	// Version 是当前 CLI 版本，写进每份文件的记录。
	Version string
	// Scope 是管理哪一部分资产，零值是完整的项目那一套。
	Scope Scope
	// Lang 显式指定这次要用哪种语言的资产（"" 表示不指定，走 resolveLang 的
	// 优先级）。brickkit init 与 brickkit skills update --lang 会显式传它；
	// 裸的 status / update 留空，沿用项目已经在用的语言。
	Lang i18n.Lang
	// LegacyLockPath 是旧版的 .brickkit/skills.lock：文件还没带记录的项目，靠它认出哪些是旧版 CLI
	// 写的、没被改过；Apply 用过一次就删掉。
	LegacyLockPath string
}

// loadLegacy 读旧版 lock；没有路径或没有文件时是空的。
func (in Installer) loadLegacy() (*Lock, error) {
	if in.LegacyLockPath == "" {
		return &Lock{}, nil
	}
	return LoadLegacyLock(in.LegacyLockPath)
}

// resolveLang 决定这次实际使用的语言，按优先级：
//
//  1. Installer.Lang——调用方显式指定（init 传当前 CLI 语言；update --lang 传参数值）
//  2. AGENTS.md 维护区记的语言——项目已经选定的、提交进 Git 的，不随运行 CLI 的语言变
//  3. 旧版 lock 记的语言（还没迁移的项目；没有 Lang 字段的更旧的 lock 是中文，见 lockLangBeforeLangField）
//  4. 都没有——用当前 CLI 语言，等价于"就当现在装一份"
//
// 结果再经过 AssetLang：还没有技能资产译本的语言装源语言的资产。
func (in Installer) resolveLang(legacy *Lock) i18n.Lang {
	return AssetLang(in.requestedLang(legacy))
}

// lockLangBeforeLangField 是没有 Lang 字段的旧 skills.lock 所装资产的语言：那个字段出现之前，
// 技能资产只写过中文。这是关于旧 lock 文件的一个事实，不是按语言分支。
const lockLangBeforeLangField = i18n.ZH

func (in Installer) requestedLang(legacy *Lock) i18n.Lang {
	if in.Lang != "" {
		return in.Lang
	}
	if code, ok := agentsmd.BlockLang(in.Root); ok {
		if l, ok := i18n.ParseLang(code); ok {
			return l
		}
	}
	if l, ok := i18n.ParseLang(legacy.Lang); ok {
		return l
	}
	if len(legacy.Entries) > 0 {
		return lockLangBeforeLangField
	}
	return i18n.Current()
}

// ResolvedLang 返回这次实际会用的语言（只读），供 `skills status` 说"这个项目现在用的是哪种语言"。
func (in Installer) ResolvedLang() (i18n.Lang, error) {
	legacy, err := in.loadLegacy()
	if err != nil {
		return "", err
	}
	return in.resolveLang(legacy), nil
}

// LegacyAgentsSum 是旧版 lock 给 AGENTS.md 记的指纹（AGENTS.md 曾经是技能资产）；没有时为空。
func (in Installer) LegacyAgentsSum() string {
	legacy, err := in.loadLegacy()
	if err != nil {
		return ""
	}
	if e, ok := legacy.Get(docspec.FileAgents); ok {
		return e.Sum
	}
	return ""
}

// Status 计算全部资产的当前状态。只读，不写任何文件。
func (in Installer) Status() ([]FileStatus, error) {
	legacy, err := in.loadLegacy()
	if err != nil {
		return nil, err
	}
	var out []FileStatus
	for _, a := range AssetsFor(in.Scope, in.resolveLang(legacy)) {
		st, err := in.stateOfWith(a, legacy)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// stateOf 判定单个资产的状态（没有旧版 lock 时）。
func (in Installer) stateOf(a Asset) (FileStatus, error) { return in.stateOfWith(a, &Lock{}) }

// stateOfWith 判定单个资产的状态。判定顺序本身是规格：
//
//	文件不存在                          → 缺失
//	带记录，正文就是当前资产              → 最新
//	带记录，正文指纹与记录不符            → 已手改
//	带记录，其余                          → 待更新（旧版本写的、没改过）
//	不带记录，旧版 lock 记的指纹与文件一致 → 待更新（迁移：旧版 CLI 写的）
//	不带记录，旧版 lock 有记录但不一致     → 已手改
//	不带记录，内容恰好就是当前资产         → 待更新（补上记录）
//	其余                                  → 未托管（可能是使用者自己写的同名文件）
func (in Installer) stateOfWith(a Asset, legacy *Lock) (FileStatus, error) {
	st := FileStatus{Target: a.Target}
	want, err := a.Content()
	if err != nil {
		return st, fmt.Errorf("%s%w", i18n.T(msgid.SkillsInstallFailedToReadTheEmbedded, a.Source), err)
	}
	disk, err := os.ReadFile(filepath.Join(in.Root, a.Target))
	if os.IsNotExist(err) {
		st.State = StateMissing
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("%s%w", i18n.T(msgid.SkillsInstallFailedToRead, a.Target), err)
	}
	body, version, recorded, marked := ReadMarker(disk)
	switch {
	case marked && bytes.Equal(body, normalize(want)):
		st.State = StateCurrent
	case marked && Sum(body) != recorded:
		st.State = StateModified
	case marked:
		st.State, st.FromVersion = StateOutdated, version
	default:
		if e, found := legacy.Get(a.Target); found {
			if e.Sum == Sum(disk) {
				st.State, st.FromVersion = StateOutdated, e.Version
			} else {
				st.State = StateModified
			}
		} else if bytes.Equal(normalize(disk), normalize(want)) {
			st.State = StateOutdated
		} else {
			st.State = StateUntracked
		}
	}
	return st, nil
}

// ApplyResult 是一次 Apply 的结果。
type ApplyResult struct {
	// Written 是本次写入的项目内相对路径。
	Written []string
	// Skipped 是刻意没碰的文件（已手改 / 未托管）。
	Skipped []FileStatus
	// Lang 是实际装的资产语言：要的语言还没有技能译本时，是源语言（见 AssetLang）。
	Lang i18n.Lang
}

// Apply 按状态写入资产：缺失与待更新写入（带记录），已手改与未托管跳过，最新不动。全程不删任何技能文件。
// 旧版 lock 用来认完旧文件之后删掉：从此每份文件自己带着记录。
//
// 语言切换不需要特殊逻辑：要的语言一变，取的就是另一棵资产树，磁盘上没改过的旧语言文件
// 天然判成「待更新」，被手改过的天然判成「已手改」照样跳过。语言本身记在 AGENTS.md 的维护区里，
// 由调用方（skills update）写回。
func (in Installer) Apply() (*ApplyResult, error) {
	legacy, err := in.loadLegacy()
	if err != nil {
		return nil, err
	}
	lang := in.resolveLang(legacy)
	res := &ApplyResult{Lang: lang}
	for _, a := range AssetsFor(in.Scope, lang) {
		st, err := in.stateOfWith(a, legacy)
		if err != nil {
			return nil, err
		}
		if st.State == StateCurrent {
			continue
		}
		if !st.State.writable() {
			res.Skipped = append(res.Skipped, st)
			continue
		}
		content, err := a.Content()
		if err != nil {
			return nil, fmt.Errorf("%s%w", i18n.T(msgid.SkillsInstallFailedToReadTheEmbedded, a.Source), err)
		}
		if err := in.write(a.Target, Mark(content, in.Version)); err != nil {
			return nil, err
		}
		res.Written = append(res.Written, a.Target)
	}
	if in.LegacyLockPath != "" {
		if err := os.Remove(in.LegacyLockPath); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("%s%w", i18n.T(msgid.SkillsLockFailedToRemoveLegacy), err)
		}
	}
	return res, nil
}

func (in Installer) write(target string, content []byte) error {
	path := filepath.Join(in.Root, target)
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("%s%w", i18n.T(msgid.SkillsInstallFailedToCreateTheDirectory, filepath.Dir(target)), err)
	}
	if err := os.WriteFile(path, content, filePerm); err != nil {
		return fmt.Errorf("%s%w", i18n.T(msgid.SkillsInstallFailedToWrite, target), err)
	}
	return nil
}
