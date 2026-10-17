package manifest

import "strings"

// IsShell 报告这个组件是不是外壳（出现 shell 块即是）。
func (m *Manifest) IsShell() bool { return m != nil && m.Shell != nil }

// HostedVersion 返回外壳声明编进去的 id 的精确版本；
// m 不是外壳、或没编进 id 时 ok 为 false。
func (m *Manifest) HostedVersion(id string) (version string, ok bool) {
	if !m.IsShell() {
		return "", false
	}
	for _, member := range m.Shell.Members {
		if memberID, v, found := SplitRef(member); found && memberID == id {
			return v, true
		}
	}
	return "", false
}

// HasStandaloneImage 报告组件是否有自己的镜像来源（预构建镜像或本地构建）。
// 外壳成员必须满足它。
func (m *Manifest) HasStandaloneImage() bool {
	return m != nil && (m.Deployment.Image != "" || m.Deployment.Build != nil)
}

// BuildsFromSource 报告组件写没写 deployment.build：写了才有从源码构建镜像的办法。只写 image 的组件，
// 镜像是作者构建好、推到 registry 的现成镜像，BrickKit 只拉取——不猜"目录里有 Dockerfile 大概就能构建"。
func (m *Manifest) BuildsFromSource() bool { return m != nil && m.Deployment.Build != nil }

// BuiltLocally 报告这个版本的镜像要在本机构建（brickkit build），而不是拉取：
//
//	没有 image                    只能构建
//	本地源给出、而且写了 build     正在开发的代码：镜像从工作区构建，不拿 registry 里的顶替
//
// 其余都拉取，包括本地源里只写了 image 的组件（只有契约、没有源码的组件被克隆或放进本地源时就是这样）。
func (m *Manifest) BuiltLocally(fromLocalSource bool) bool {
	return m.Deployment.Image == "" || (fromLocalSource && m.BuildsFromSource())
}

// ImageRef 返回部署时用的镜像引用。
//
//	image 带 tag 或 digest   原样
//	image 不带 tag          image:<metadata.version>——镜像 tag 与组件版本对齐
//	只有 build              <scope>-<name>:<metadata.version>——brickkit build 产出的就是它
func ImageRef(m *Manifest) string {
	image := m.Deployment.Image
	if image == "" {
		return strings.ReplaceAll(m.Metadata.ID, "/", "-") + ":" + m.Metadata.Version
	}
	// 最后一个 / 之后有 ":" 就是已经带了 tag（digest 的 @sha256:… 同样落在这里）；
	// localhost:5000/erp 里的冒号在 / 之前，是端口不是 tag
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		return image
	}
	return image + ":" + m.Metadata.Version
}
