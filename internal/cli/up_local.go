package cli

import (
	"github.com/brickkit/brickkit/internal/config"
)

// anyModeLocal 判断项目里有没有 mode: local 组件。
func anyModeLocal(components []config.Component) bool {
	for _, c := range components {
		if c.Mode == config.ModeLocal {
			return true
		}
	}
	return false
}
