package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 002 §5.3 版本化服务名：组件 ID 转换 + 精确版本号。
// 转换规则：`/` → `-`、`.` → `-`、全部小写、拼接版本号（版本号中的 `.` → `-`）。
func TestServiceName(t *testing.T) {
	cases := []struct {
		id      string
		version string
		want    string
	}{
		// 002 §5.3 表格中的四行，逐字对齐
		{"people/basic", "1.0.0", "people-basic-1-0-0"},
		{"department/tree", "1.2.0", "department-tree-1-2-0"},
		{"erp/backend", "2.1.3", "erp-backend-2-1-3"},
		{"portal/user-frontend", "1.0.0", "portal-user-frontend-1-0-0"},
		// 多版本共存：同一组件的不同版本必须得到不同的服务名
		{"people/basic", "2.0.0", "people-basic-2-0-0"},
		// 大版本号不做补零
		{"infra/redis-event-bus", "10.20.30", "infra-redis-event-bus-10-20-30"},
	}

	for _, c := range cases {
		assert.Equal(t, c.want, ServiceName(c.id, c.version), "%s@%s", c.id, c.version)
	}
}

// 服务名同时用作 Docker Compose service 名与 K8s Service 名，必须是合法 DNS 标签。
func TestServiceNameIsDNSLabel(t *testing.T) {
	name := ServiceName("portal/user-frontend", "1.0.0")
	assert.Regexp(t, `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`, name)
}
