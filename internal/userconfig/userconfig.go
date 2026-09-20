// Package userconfig 管理 BrickKit CLI 第一份"项目之外"的状态：机器级、
// 跨项目的用户偏好（目前只有显示语言一项）。
//
// BrickKit 到目前为止的所有状态都长在某个项目的 .brickkit/ 目录里
// （internal/config.Layout 的每条路径都相对项目 Root 推导）。但语言偏好
// 天然是机器级的——brickkit init、brickkit version、brickkit login 这些
// 命令本来就可能在还没有项目、或不在任何项目目录里执行，语言偏好没有
// 项目目录可以寄存。
package userconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// EnvDirOverride 覆盖 Dir() 的返回值，仅供测试使用——避免测试读到/写到
// 跑测试的人自己机器上真实的全局配置（这是 BrickKit 第一次引入跨项目
// 状态，需要这样一个隔离开关；其余状态都在某个项目的 .brickkit/ 里，
// 测试用 t.TempDir() 当项目根目录就已经隔离了，不需要它）。
const EnvDirOverride = "BRICKKIT_USERCONFIG_DIR"

const fileName = "config.json"

// Config 是全局配置文件的内容。
type Config struct {
	Lang string `json:"lang"`
}

// Dir 返回全局配置目录：跨平台正确（Linux 走 XDG_CONFIG_HOME 或
// ~/.config，macOS 走 ~/Library/Application Support），可用
// EnvDirOverride 覆盖。
func Dir() (string, error) {
	if v := os.Getenv(EnvDirOverride); v != "" {
		return v, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "brickkit"), nil
}

// Path 返回全局配置文件的完整路径。
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Load 读取全局配置。文件不存在时返回空值而不是错误——还没设置过语言
// 等价于"用默认值"，跟 internal/source.LoadCredentials 的"未登录不是
// 错误"是同一个道理。
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return &Config{}, nil
	case err != nil:
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Save 写入全局配置。先写临时文件再 rename——写到一半失败不会把已有的
// 有效配置毁掉，跟 internal/source/credentials.go 的写法一致。语言偏好
// 不是密钥，文件权限用 0644（credentials.go 的 0600 是因为那是明文
// Token，这里不需要）。
func Save(c *Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	path := filepath.Join(dir, fileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
