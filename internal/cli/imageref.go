package cli

import (
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
)

// checkImageReference 校验镜像引用（开发计划 19.13、010 §5）。
//
// 只做发布前拦得住的检查——镜像到底存不存在要问镜像仓库，那是 up 时的事。
// 这里管的是两件在发布这一刻就能确定是错的事：
//
//   - 没有标签：拉取时会退化成 latest，等于放弃了版本控制；
//   - 标签是 latest：同一个引用在不同时间指向不同镜像，
//     002 §7.1 建立在精确版本上的全部可复现性都会崩。
func checkImageReference(image string) error {
	image = strings.TrimSpace(image)
	if image == "" {
		return imageError(image, "镜像地址不能为空", "在 component.yaml 的 deployment.image 中填写完整镜像地址")
	}
	if strings.ContainsAny(image, " \t\n") {
		return imageError(image, "镜像地址不合法：不能包含空白字符", "检查 component.yaml 中 deployment.image 的写法")
	}

	name, tag := splitImageTag(image)
	if name == "" {
		return imageError(image, "镜像地址不合法：缺少镜像名", "正确写法形如 registry.example.com/people-basic:1.2.0")
	}
	if strings.ToLower(name) != name {
		return imageError(image, "镜像地址不合法：镜像名必须全小写", "把镜像名改成全小写后重新构建并推送")
	}
	if tag == "" {
		return imageError(image, "镜像地址缺少标签",
			"必须使用明确版本的标签，例如 "+name+":1.2.0（010 §5：生产环境不使用 latest）")
	}
	if tag == "latest" {
		return imageError(image, "镜像标签不能是 latest",
			"改用与组件版本一致的标签，例如 "+name+":1.2.0",
			"latest 会让同一个引用在不同时间指向不同镜像，安装结果不可复现")
	}
	return nil
}

// splitImageTag 拆出镜像名与标签。
//
// 难点在于 registry 地址里的端口号也带冒号（registry:5000/app），
// 因此只认最后一个路径段里的冒号。digest 形式（@sha256:...）视为已锁定版本。
func splitImageTag(image string) (name, tag string) {
	if at := strings.Index(image, "@"); at >= 0 {
		// registry.example.com/app@sha256:abc… —— digest 本身就是精确引用
		return image[:at], "sha256-digest"
	}

	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon < 0 || colon < slash {
		return image, ""
	}
	return image[:colon], image[colon+1:]
}

func imageError(image, reason string, hints ...string) error {
	return clierr.New(clierr.CodeManifestInvalid, "错误："+reason).
		WithDetail("deployment.image", image).
		WithHint(hints...)
}
