package skills

import (
	"encoding/json"
	"fmt"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"os"
	"path/filepath"
	"sort"
)

// LockEntry 是一份托管文件的记录。
type LockEntry struct {
	// Path 是项目内相对路径（与 Asset.Target 一致）。
	Path string `json:"path"`
	// Version 是写入时的 CLI 版本。
	Version string `json:"version"`
	// Sum 是写入时的内容指纹（sha256:...）。
	Sum string `json:"sum"`
}

// Lock 是 .brickkit/skills.lock 的内容。
//
// 它只回答一个问题：**这个文件上次是我们写的、内容是什么样**。
// 有了它才能区分「用户手改过」和「CLI 升级导致过期」——前者绝不能覆盖。
type Lock struct {
	Entries []LockEntry `json:"entries"`
	// Lang 记录这个项目的技能资产上次是用哪种语言装的（"en" / "zh"）。
	// 语言是项目级的、稳定的选择，不跟着运行 CLI 那台机器当下的语言走——
	// 否则两个语言不同的队友会把提交进仓库的技能文件改来改去。
	//
	// omitempty：这个字段是后加的，早期项目的 skills.lock 里没有它；
	// 读到空值时 Installer 按"这批资产历史上只有中文"回退到 zh，
	// 而不是当成一个新项目去问当前 CLI 语言（见 install.go 的 resolveLang）。
	Lang string `json:"lang,omitempty"`
}

// LoadLock 读取 lock。文件不存在时返回空 Lock 且不报错：
// 没有 lock 是常态（老项目、刚 clone、用户删过），不是故障。
func LoadLock(path string) (*Lock, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Lock{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s%w", i18n.T(msgid.SkillsLockFailedToReadSkillsLock), err)
	}
	var l Lock
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("%s%w", i18n.T(msgid.SkillsLockFailedToParseSkillsLock), err)
	}
	return &l, nil
}

// Get 按项目内相对路径取记录。
func (l *Lock) Get(target string) (LockEntry, bool) {
	for _, e := range l.Entries {
		if e.Path == target {
			return e, true
		}
	}
	return LockEntry{}, false
}

// Set 写入或替换一条记录。
func (l *Lock) Set(e LockEntry) {
	for i := range l.Entries {
		if l.Entries[i].Path == e.Path {
			l.Entries[i] = e
			return
		}
	}
	l.Entries = append(l.Entries, e)
}

// Save 写入 lock。条目按路径排序、结尾带换行：
// 这个文件要提交进 Git，顺序不稳定会让每次 update 都产生一堆假 diff。
func (l *Lock) Save(path string) error {
	sort.Slice(l.Entries, func(i, j int) bool {
		return l.Entries[i].Path < l.Entries[j].Path
	})
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("%s%w", i18n.T(msgid.SkillsLockFailedToSerializeSkillsLock), err)
	}
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("%s%w", i18n.T(msgid.SkillsLockFailedToCreateTheDirectory), err)
	}
	return os.WriteFile(path, append(b, '\n'), filePerm)
}
