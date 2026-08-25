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

// HostMachineAlias 是"宿主机"在容器里的惯用别名。
//
// 它带点，因此不会被当成容器网络内的服务名；但容器里默认也解析不了它，
// 必须靠 extra_hosts 指到网关上（P34）。
const HostMachineAlias = "host.docker.internal"

// DialHost 把资源的 host 换成**从宿主机拨号时**该用的名字。
//
// `host.docker.internal` 是 Docker 注入到**容器** /etc/hosts 里的，
// Linux 的宿主机自己解析不了它。`brickkit status` 的资源探测
// 是从宿主机发起的，不换的话会对一个**完全健康**的资源报"不可达"——
// 而组件正连着它跑得好好的。
//
// 与 local-debug.env 里那次改写（compose 包）是**同一条规则**：
// 谁在宿主机上跑，谁就该用 localhost。放在这里是为了只留一份判据。
func DialHost(host string) string {
	if host == HostMachineAlias {
		return "localhost"
	}
	return host
}
