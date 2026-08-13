package manifest

import "strings"

// serviceNameReplacer 实现 002 §5.3 的转换规则：`/` → `-`、`.` → `-`。
var serviceNameReplacer = strings.NewReplacer("/", "-", ".", "-")

// ServiceName 返回组件的版本化服务名（002 §5.3）。
//
//	people/basic + 1.0.0 → people-basic-1-0-0
//
// 该名称同时用作 Docker Compose service 名、K8s Service 名、环境变量中的
// 组件地址，以及 .brickkit/artifacts/ 下的产物目录名，因此必须是合法 DNS 标签。
// 组件 ID 与版本号的合法性由 Manifest 校验保证（见 validate.go）。
func ServiceName(id, version string) string {
	return serviceNameReplacer.Replace(strings.ToLower(id + "-" + version))
}
