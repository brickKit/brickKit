package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 依赖组件地址的环境变量名是 `{组件ID前缀}_ENDPOINT`，
// 前缀由组件 ID 转大写下划线得到（变量名不带版本号，值才带）。
func TestEnvPrefix(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"department/tree", "DEPARTMENT_TREE"},
		{"people/basic", "PEOPLE_BASIC"},
		// 弱依赖警告里逐字出现了这个变量名
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

// 额外端口的变量名跟组件 ID 前缀同一条规则：- 变成 _，否则 ERP_API_ADMIN-API_ENDPOINT 不是合法的 shell 变量名。
func TestExtraPortEndpointEnvVarIsAValidName(t *testing.T) {
	assert.Equal(t, "ERP_API_ADMIN_API_ENDPOINT", ExtraPortEndpointEnvVar("erp/api", "admin-api"))
	assert.Equal(t, "PEOPLE_BASIC_GRPC_ENDPOINT", ExtraPortEndpointEnvVar("people/basic", "grpc"))
}
