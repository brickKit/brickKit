package cli

import (
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
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
		return imageError(image, i18n.T(msgid.CliImagerefTheImageAddressMustNot), i18n.T(msgid.CliImagerefFillInTheFullImage))
	}
	if strings.ContainsAny(image, " \t\n") {
		return imageError(image, i18n.T(msgid.CliImagerefInvalidImageAddressItMust), i18n.T(msgid.CliImagerefCheckHowDeploymentImageIs))
	}

	// digest 形式要真的是个 digest。`@` 后面原本什么都能写——
	// `repo@latest` 这种会一路传到市场，消费方拉取时才失败，
	// 而那时已经查不清是谁传坏的了（P29）
	if repo, digest, ok := strings.Cut(image, "@"); ok {
		switch {
		case repo == "":
			return imageError(image, i18n.T(msgid.CliImagerefInvalidImageAddressTheImage),
				i18n.T(msgid.CliImagerefTheCorrectFormLooksLike))
		case !digestPattern.MatchString(digest):
			return imageError(image, i18n.T(msgid.CliImagerefInvalidImageDigestFormat),
				i18n.T(msgid.CliImagerefItMustBeSha256Followed),
				i18n.T(msgid.CliImagerefCurrently, digest),
				i18n.T(msgid.CliImagerefGetTheCorrectValueWith))
		}
	}

	name, tag := splitImageTag(image)
	if name == "" {
		return imageError(image, i18n.T(msgid.CliImagerefInvalidImageAddressTheImage2), i18n.T(msgid.CliImagerefTheCorrectFormLooksLike2))
	}
	if strings.ToLower(name) != name {
		return imageError(image, i18n.T(msgid.CliImagerefInvalidImageAddressTheImage3), i18n.T(msgid.CliImagerefChangeTheImageNameTo))
	}
	if tag == "" {
		return imageError(image, i18n.T(msgid.CliImagerefTheImageAddressHasNo),
			i18n.T(msgid.CliImagerefUseATagWithAn, name))
	}
	if tag == "latest" {
		return imageError(image, i18n.T(msgid.CliImagerefTheImageTagMustNot),
			i18n.T(msgid.CliImagerefUseATagThatMatches, name),
			i18n.T(msgid.CliImagerefLatestLetsTheSameReference))
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
	return clierr.New(clierr.CodeManifestInvalid, i18n.T(msgid.CliRestoreError, reason)).
		WithDetail("deployment.image", image).
		WithHint(hints...)
}
