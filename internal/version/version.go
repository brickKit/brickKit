// Package version 保存 BrickKit CLI 的版本与能力常量。
//
// 版本号由 Makefile 通过 -ldflags 注入，未注入时使用 dev 占位值。
package version

import "strings"

var (
	// Version 是 CLI 版本号（构建时注入）。
	Version = "0.0.0-dev"
	// Commit 是构建对应的 Git commit（构建时注入）。
	Commit = "unknown"
	// BuildDate 是构建时间（构建时注入）。
	BuildDate = "unknown"
)

// ManifestAPIVersion 是 CLI 支持的 component.yaml apiVersion（002 §2）。
const ManifestAPIVersion = "brickkit/v1"

// DeployTargets 是 CLI 支持的部署目标（003 §3）。
var DeployTargets = []string{"docker", "k8s"}

// SupportedTargets 返回逗号分隔的部署目标列表。
func SupportedTargets() string {
	return strings.Join(DeployTargets, ", ")
}

// Display 返回带 v 前缀的版本号（004 §11.3 输出格式为 "v1.0.0"）。
// Version 本身已带 v 前缀时不重复添加。
func Display() string {
	if strings.HasPrefix(Version, "v") {
		return Version
	}
	return "v" + Version
}
