package cli

// 本文件是"要么全改完，要么原样不动"的那块底料：改一组文件之前记下每个的原样（不存在也是一种原样），
// 出错时一个个放回去。add / remove / upgrade 的三份文件、up 写焦点时的个人文件与本地模式开关，
// 用的都是它——同一件事只有一种写法，还原得全不全也只要验一处。

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

type fileBackup struct {
	path    string
	data    []byte
	mode    os.FileMode
	existed bool
}

// fileSnapshot 记下一组文件改动前的原样。
type fileSnapshot struct {
	backups map[string]*fileBackup
	order   []string
}

// take 在第一次碰一个文件之前记下它的原样；同一个文件只记第一次。
func (s *fileSnapshot) take(path string) error {
	if s.backups == nil {
		s.backups = map[string]*fileBackup{}
	}
	if _, ok := s.backups[path]; ok {
		return nil
	}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		mode := os.FileMode(0o644)
		if info, statErr := os.Stat(path); statErr == nil {
			mode = info.Mode().Perm()
		}
		s.backups[path] = &fileBackup{path: path, data: data, mode: mode, existed: true}
	case errors.Is(err, fs.ErrNotExist):
		s.backups[path] = &fileBackup{path: path}
	default:
		return writeError(path, err)
	}
	s.order = append(s.order, path)
	return nil
}

// restore 把记下的文件全部放回原样：原本有的写回（连同权限），原本没有的删掉。倒着来。
func (s *fileSnapshot) restore() {
	for i := len(s.order) - 1; i >= 0; i-- {
		b := s.backups[s.order[i]]
		if b.existed {
			_ = os.MkdirAll(filepath.Dir(b.path), 0o755)
			_ = os.WriteFile(b.path, b.data, b.mode)
			_ = os.Chmod(b.path, b.mode)
		} else {
			_ = os.Remove(b.path)
		}
	}
}
