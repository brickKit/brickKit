package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// FindRoot 从 dir 往上找最近的一个含 brickkit.yaml 的目录，就像 git 找 .git。
//
// 不在嵌套的 .git 处停下：大项目 components/ 下的组件多半各自是 git 仓库，
// 在里面干活时要找的正是外面那个项目。离 dir 最近的 brickkit.yaml 胜出——组件目录里
// 自己有一份（工作台）时，它就是项目。
func FindRoot(dir string) (root string, found bool, err error) {
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", false, err
	}
	for {
		_, statErr := os.Stat(filepath.Join(dir, FileDecl))
		switch {
		case statErr == nil:
			return dir, true, nil
		case !errors.Is(statErr, fs.ErrNotExist):
			return "", false, statErr
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false, nil
		}
		dir = parent
	}
}
