package project

import (
	"errors"
	"io/fs"
	"os"
)

// LocalModeOn 报告本地模式是否开启（.brickkit/local-mode 是否存在）。
func LocalModeOn(l Layout) (bool, error) {
	_, err := os.Stat(l.LocalModePath())
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}

// SetLocalMode 打开或关闭本地模式。关闭一个本来就关着的开关不算错。
func SetLocalMode(l Layout, on bool) error {
	if !on {
		err := os.Remove(l.LocalModePath())
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(l.BrickkitDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(l.LocalModePath(), []byte("on\n"), 0o644)
}
