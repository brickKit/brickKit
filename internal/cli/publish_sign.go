package cli

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/security"
)

// defaultSigningKey 是 --key 的默认值：cosign generate-key-pair 就叫这个名字。
const defaultSigningKey = "cosign.key"

// signPackage 对发布包签名（008 §8.2、010 §6.2），--sign 未指定时什么都不做。
//
// 签名的对象是 **Manifest 的规范化载荷**，不是 component.yaml 的原始字节：
// 这份文档一路 YAML→JSON→YAML 地被改写形态，对某一种写法的字节签名必然失效
// （008 §8.3.1）。规范化由 internal/security 负责，两侧共用同一个函数。
func signPackage(ctx context.Context, opts *Options, pkg *publishPackage, f publishFlags) error {
	if !f.sign {
		return nil
	}

	keyPath := f.key
	if strings.TrimSpace(keyPath) == "" {
		keyPath = defaultSigningKey
	}
	if !filepath.IsAbs(keyPath) {
		keyPath = filepath.Join(opts.WorkDir, keyPath)
	}

	ref, err := publicKeyRefFor(f.publicKeyRef, f.key, keyPath)
	if err != nil {
		return err
	}

	payload, err := security.CanonicalPayload(pkg.document)
	if err != nil {
		return err
	}

	sig, err := security.Sign(ctx, payload, security.SignOptions{
		KeyPath:      keyPath,
		PublicKeyRef: ref,
		SignedBy:     f.signedBy,
		Now:          opts.Now,
	})
	if err != nil {
		return err
	}
	pkg.signature = sig

	opts.Printf("%s\n", i18n.T(msgid.CliPublishSignSignedCosignPublicKeyRef, ref))
	// 签名的另一半在使用者手里：不把这句话说出来，发布者不知道要转告什么，
	// 使用者也就永远配不上 publicKeys，签名等于白签
	opts.Printf("%s\n", i18n.T(msgid.CliPublishSignUsersNeedToDeclareIt))
	opts.Printf("%s\n", i18n.T(msgid.CliPublishSignPublicKeyFilePath, ref))
	return nil
}

// publicKeyRefFor 决定写进签名的 publicKeyRef。
//
// ref 是发布者与使用者之间的契约（使用者照它在 installer.publicKeys 里配同名
// 条目），所以既不能空着，也不该由 CLI 随手编一个。规则只有一条：
// 按 .key → .pub 推导，并保留 --key 写的那个相对路径形式。
// 于是 --key keys/people-basic-release.key 得到 keys/people-basic-release.pub
// ——正是 008 §8.3 示例里的那个名字，可预期也可解释。
func publicKeyRefFor(explicit, flagKey, resolvedKey string) (string, error) {
	if ref := strings.TrimSpace(explicit); ref != "" {
		return ref, nil
	}

	// 优先用使用者在 --key 里写的原样路径；--key 没写时退回默认密钥名
	base := strings.TrimSpace(flagKey)
	if base == "" {
		base = defaultSigningKey
	}
	base = filepath.ToSlash(base)

	ext := filepath.Ext(base)
	if ext != ".key" {
		return "", clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliPublishSignErrorCannotDerivePublickeyref)).
			WithDetail(i18n.T(msgid.CliPublishSignPrivateKey), resolvedKey).
			WithHint(i18n.T(msgid.CliPublishSignWhenThePrivateKeyFile),
				i18n.T(msgid.CliPublishSignForExamplePublicKeyRef)).
			WithTip(i18n.T(msgid.CliPublishSignThisRefIsTheName))
	}
	return strings.TrimSuffix(base, ext) + ".pub", nil
}
