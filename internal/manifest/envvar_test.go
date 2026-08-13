package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 004 §5.6：依赖组件地址的环境变量名是 `{组件ID前缀}_ENDPOINT`，
// 前缀由组件 ID 转大写下划线得到（变量名不带版本号，值才带）。
func TestEnvPrefix(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"department/tree", "DEPARTMENT_TREE"},
		{"people/basic", "PEOPLE_BASIC"},
		// 004 §4.5 的弱依赖警告里逐字出现了这个变量名
		{"infra/redis-event-bus", "INFRA_REDIS_EVENT_BUS"},
		{"portal/user-frontend", "PORTAL_USER_FRONTEND"},
	}

	for _, c := range cases {
		assert.Equal(t, c.want, EnvPrefix(c.id), c.id)
	}
}

func TestEndpointEnvVar(t *testing.T) {
	assert.Equal(t, "DEPARTMENT_TREE_ENDPOINT", EndpointEnvVar("department/tree"))
	assert.Equal(t, "INFRA_REDIS_EVENT_BUS_ENDPOINT", EndpointEnvVar("infra/redis-event-bus"))
}
