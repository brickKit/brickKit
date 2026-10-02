package deploy

// 本文件放**两种部署目标共用**的命名规则。
//
// 放在这里而不是各自实现：同一个项目在 Docker 与 K8s 下必须叫同一个名字，
// 换目标时使用者不用重新学一套。两处各算一遍的话迟早会分叉，
// 而分叉的表现是"换个目标就连不上了"，且两边看起来都对。
//
// 这个包只依赖 config，因此 compose / k8s / inject 都能引用它而不会成环。

import "strings"

// Namespace 是项目的默认 K8s 命名空间：brickkit-<项目名>。
func Namespace(project string) string {
	if project == "" {
		// 配置校验保证项目名非空，这里只是不生成一个以 - 结尾的非法命名空间
		return "brickkit"
	}
	return "brickkit-" + project
}

// NetworkName 是项目专属的 Docker 网络名：brickkit-<项目名>-net。
func NetworkName(project string) string {
	if project == "" {
		project = "brickkit"
	}
	return "brickkit-" + project + "-net"
}

// HostMachineAlias 是"宿主机"在容器里的惯用别名。
//
// 它带点，因此不会被当成容器网络内的服务名；但容器里默认也解析不了它，
// 必须靠 extra_hosts 指到网关上。
const HostMachineAlias = "host.docker.internal"

// OnHostMachine 把一个值里的 host.docker.internal 换成 localhost：给**在宿主机上跑**的进程用
// （mode: local / debug、焦点组件）。
//
// `host.docker.internal` 是"宿主机"在**容器**里的名字，Linux 的宿主机自己解析不了它。同一份 config
// 既给容器用、也给搬到宿主机上跑的那个进程用：容器照旧拿到原文（平台给它加 extra_hosts），宿主机上的进程
// 拿到 localhost——指的是同一台机器，只是换成它那一侧叫得通的名字。不换的话只能在部署文件的 vars: 里
// 改成 localhost，而 vars: 同时作用于容器，它们就连不上了。
//
// 只换完整的主机名：值可以是一个 URL、一条 DSN、一段 JSON，名字出现几次换几次；
// 它只是更长的名字的一部分时（myhost.docker.internal、host.docker.internal.example.com）不动。
func OnHostMachine(value string) string {
	var b strings.Builder
	start := 0
	for {
		i := strings.Index(value[start:], HostMachineAlias)
		if i < 0 {
			break
		}
		i += start
		end := i + len(HostMachineAlias)
		b.WriteString(value[start:i])
		if partOfLongerName(value, i, end) {
			b.WriteString(HostMachineAlias)
		} else {
			b.WriteString("localhost")
		}
		start = end
	}
	if start == 0 {
		return value
	}
	b.WriteString(value[start:])
	return b.String()
}

// partOfLongerName 报告 s[i:end] 是不是一个更长的主机名的一部分：前面紧挨着主机名字符，
// 或者后面还接着一段。
func partOfLongerName(s string, i, end int) bool {
	if i > 0 && hostNameByte(s[i-1]) {
		return true
	}
	if end >= len(s) {
		return false
	}
	return hostNameLabelByte(s[end]) || (s[end] == '.' && end+1 < len(s) && hostNameLabelByte(s[end+1]))
}

// hostNameLabelByte 报告 c 能不能出现在主机名的一段里。
func hostNameLabelByte(c byte) bool {
	return c == '-' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// hostNameByte 报告 c 能不能出现在主机名里（一段里的字符，或者段之间的点）。
func hostNameByte(c byte) bool { return c == '.' || hostNameLabelByte(c) }
