package manifest

import "strings"

// SplitRef 拆开组件引用 <组件ID>@<版本>：在第一个 @ 处切开，没有 @ 时 hasVersion 为 false。
// component.yaml 的依赖与 shell.members、部署文件的条目、配置文件头、命令行参数都用它——
// 拆法只有一种。组件 ID 不许含 @，多出来的 @ 落进版本里，由精确版本校验拦下。
func SplitRef(ref string) (id, version string, hasVersion bool) {
	return strings.Cut(ref, "@")
}
