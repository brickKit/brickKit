package manifest

import "strings"

// envPrefixReplacer 把组件 ID 转成环境变量前缀：`/` 与 `-` 都变下划线。
var envPrefixReplacer = strings.NewReplacer("/", "_", "-", "_")

// EnvPrefix 返回组件 ID 对应的环境变量前缀（004 §5.6）。
//
//	infra/redis-event-bus → INFRA_REDIS_EVENT_BUS
//
// 注意：环境变量名**不带版本号**（基于组件 ID），带版本号的是变量的值
// （指向版本化服务名，见 ServiceName）。
func EnvPrefix(id string) string {
	return strings.ToUpper(envPrefixReplacer.Replace(id))
}

// EndpointEnvVar 返回依赖组件地址的环境变量名（004 §5.6）。
//
//	department/tree → DEPARTMENT_TREE_ENDPOINT
//
// 弱依赖缺失时，CLI **完全不注入**该变量（不注入空字符串，002 §3.4）。
func EndpointEnvVar(id string) string { return EnvPrefix(id) + "_ENDPOINT" }
