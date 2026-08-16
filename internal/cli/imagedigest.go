package cli

// 本文件把镜像 tag 解析成 registry 里的 digest（P29）。
//
// # 为什么要钉 digest
//
// Manifest 签名覆盖规范化后的整个 Manifest，`deployment.image` 就在里面。
// 所以"部署哪个镜像"已经被签名保护了——剩下的缺口只有一个：
// **签名保证镜像字符串没被改，保证不了那个字符串还指向同样的字节**。
//
// tag 是可变的。发布者签名时是 `repo:1.0.0`，攻击者拿到 registry 权限后
// 用同一个 tag 重新 push，签名依然有效、跑起来的却是另一个镜像。
// 换成 digest 这个缺口就自己关上了，**不需要任何新的密码学机制**。

import (
	"context"
	"os/exec"
	"regexp"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
)

// digestPattern 是 OCI 的 digest 写法：sha256: 加 64 个十六进制字符。
var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// isDigestRef 判断镜像引用是不是已经钉了 digest。
func isDigestRef(image string) bool {
	_, digest, ok := strings.Cut(image, "@")
	return ok && digestPattern.MatchString(digest)
}

// commandRunner 执行外部命令，测试可替换。
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// defaultDigestResolver 是真实实现：问 registry 要 digest。
func defaultDigestResolver() func(context.Context, string) (string, error) {
	return digestResolverWith(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	})
}

// digestResolverWith 用给定的执行器构造解析器。
//
// # 只用 buildx，**不做 fallback**
//
// `docker manifest inspect -v` 也能给出一个 digest，但那是**单个平台**的
// manifest digest，不是多架构索引的。实测 alpine:3.20：
//
//	buildx              → sha256:d9e853e8…  application/vnd.oci.image.index.v1+json
//	manifest inspect -v → sha256:c64c687c…  ...image.manifest.v1+json  linux/amd64
//
// 钉住后者会把组件**锁死在 amd64**，ARM 节点上拉不到——而且是发布半年后
// 换机器时才发现。一个悄悄锁架构的 fallback，比直接报错糟得多。
//
// 查的是 **registry**，不是本地 daemon。本地 `RepoDigests` 看着也像答案，
// 但它可能来自 `docker load`，未必真在目标 registry 里存在——
// 而消费方是从 registry 拉的。
func digestResolverWith(run commandRunner) func(context.Context, string) (string, error) {
	return func(ctx context.Context, image string) (string, error) {
		out, err := run(ctx, "docker",
			"buildx", "imagetools", "inspect", image, "--format", "{{.Manifest.Digest}}")
		if err != nil {
			return "", err
		}

		digest := strings.TrimSpace(string(out))
		if !digestPattern.MatchString(digest) {
			// 拿到一串不是 digest 的东西时不能当成成功：buildx 失败时
			// 也可能以 0 退出并把错误打在输出里
			return "", clierr.Newf(clierr.CodeEngineFailed,
				"错误：没能从 registry 取到镜像 digest").
				WithDetail("镜像", image).
				WithDetail("实际输出", tailLine(digest))
		}
		return digest, nil
	}
}

// tailLine 取输出的最后一行非空文字，用于错误提示。
func tailLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return "(空)"
}

// digestUnresolvable 把解析失败翻译成一条能指出下一步的错误。
//
// 两种原因要都说出来——它们该做的下一步完全不同：
// 镜像没推上去，得先 push；registry 连不上，得查网络或凭据。
func digestUnresolvable(image string, cause error) error {
	return clierr.New(clierr.CodeEngineFailed, "错误：无法确定镜像的 digest，发布已中止").
		WithDetail("镜像", image).
		WithDetail("原因", clierr.As(cause).Message).
		WithDetail("为什么要拦住", "发布出去的版本号不可回收（007 §18.14）。"+
			"取不到 digest 通常意味着这个镜像消费方也拉不到——"+
			"与其在市场里留下一个装不上的版本，不如现在停下").
		WithHint(
			"确认镜像已经**推送**到 registry：docker push "+image,
			"确认本机能访问该 registry（私有仓库先 docker login）",
			"确实无法解析时用 --no-pin-digest 跳过——但那样签名就只锁住 tag，"+
				"registry 上换掉同名 tag 的话签名照样有效",
		).
		WithCause(cause)
}
