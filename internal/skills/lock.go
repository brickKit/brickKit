package skills

import (
	"encoding/json"
	"fmt"
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
}

// LoadLock 读取 lock。文件不存在时返回空 Lock 且不报错：
// 没有 lock 是常态（老项目、刚 clone、用户删过），不是故障。
func LoadLock(path string) (*Lock, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Lock{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 skills.lock 失败：%w", err)
	}
	var l Lock
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("skills.lock 解析失败：%w", err)
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
		return fmt.Errorf("序列化 skills.lock 失败：%w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("创建 skills.lock 所在目录失败：%w", err)
	}
	return os.WriteFile(path, append(b, '\n'), filePerm)
}
