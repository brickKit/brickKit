package deploy

// 本文件放**两种部署目标共用**的命名规则。
//
// 放在这里而不是各自实现：同一个项目在 Docker 与 K8s 下必须叫同一个名字，
// 换目标时使用者不用重新学一套。两处各算一遍的话迟早会分叉，
// 而分叉的表现是"换个目标就连不上了"，且两边看起来都对。
//
// 这个包只依赖 config，因此 compose / k8s / inject 都能引用它而不会成环。

// Namespace 是项目的默认 K8s 命名空间：brickkit-<项目名>（005 §5.2）。
func Namespace(project string) string {
	if project == "" {
		// 配置校验保证项目名非空，这里只是不生成一个以 - 结尾的非法命名空间
		return "brickkit"
	}
	return "brickkit-" + project
}

// NetworkName 是项目专属的 Docker 网络名：brickkit-<项目名>-net（005 §5）。
func NetworkName(project string) string {
	if project == "" {
		project = "brickkit"
	}
	return "brickkit-" + project + "-net"
}
