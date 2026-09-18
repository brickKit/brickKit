package security

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/brickkit/brickkit/internal/clierr"
)

// cosignInstallHint 是所有"没装 cosign"错误共用的提示。
// 直接给命令，不让使用者自己去搜。
const cosignInstallHint = "安装：go install github.com/sigstore/cosign/v2/cmd/cosign@latest"

// SignOptions 控制签名生成。
type SignOptions struct {
	// KeyPath 是 cosign 私钥（cosign.key）路径。
	KeyPath string
	// PublicKeyRef 写进签名，使用者照它在 installer.publicKeys 里配公钥。
	// 必填：这是发布者与使用者之间的契约，不能由 CLI 代为编造。
	PublicKeyRef string
	// SignedBy 是签名者标识（008 §8.3），如 release-bot@brickkit.io。
	SignedBy string
	// CosignPath 是 cosign 可执行文件，留空时从 PATH 查找。
	CosignPath string
	// Now 用于填 signedAt，留空时用 time.Now。
	Now func() time.Time
}

// Sign 调用 cosign 对 payload 签名（010 §6.2）。
//
// payload 应当是 CanonicalPayload 的输出。
//
// # 为什么调外部 cosign 而不是自己用标准库签
//
// 验签用标准库是因为"每个使用者都要验"，多一个依赖等于劝人关掉校验（见包注释）。
// 签名恰恰相反：只有发布者做，通常在 CI 里，而私钥管理（口令、KMS、硬件密钥、
// keyless）全是 cosign 的地盘。自己实现只能覆盖"明文私钥文件"这一种最不安全的
// 情形，还要把它变成唯一的选择。
func Sign(ctx context.Context, payload []byte, opts SignOptions) (*Signature, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(opts.PublicKeyRef) == "" {
		return nil, clierr.New(clierr.CodeConfigInvalid, "错误：签名缺少 publicKeyRef").
			WithHint("publicKeyRef 是使用者查找公钥的名字，必须显式指定",
				"约定用 keys/<组件名>-release.pub 这样的形式")
	}

	bin, err := resolveCosign(opts.CosignPath)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(opts.KeyPath); err != nil {
		return nil, clierr.New(clierr.CodeConfigInvalid, "错误：找不到 cosign 私钥").
			WithDetail("路径", opts.KeyPath).
			WithHint("首次发布请先执行 cosign generate-key-pair 生成密钥对",
				"cosign.key 是私钥，绝不能提交进 Git；只有 cosign.pub 需要分发").
			WithCause(err)
	}

	// cosign sign-blob 只接受文件，不读 stdin
	dir, err := os.MkdirTemp("", "brickkit-sign-")
	if err != nil {
		return nil, clierr.New(clierr.CodeConfigInvalid, "错误：创建临时目录失败").WithCause(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	blob := filepath.Join(dir, "payload")
	bundleFile := filepath.Join(dir, "bundle.json")
	if err := os.WriteFile(blob, payload, 0o600); err != nil {
		return nil, clierr.New(clierr.CodeConfigInvalid, "错误：写入待签名内容失败").
			WithDetail("路径", blob).WithCause(err)
	}

	args := signArgs(opts.KeyPath, bundleFile, blob)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = os.Stdin // 私钥有口令、且在终端里执行时，cosign 在这里提示输入
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, signFailed(bin, args, out, err)
	}

	value, err := readBundleSignature(bundleFile)
	if err != nil {
		return nil, err
	}

	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Signature{
		Algorithm:    AlgorithmCosign,
		PublicKeyRef: opts.PublicKeyRef,
		Value:        value,
		SignedAt:     now().UTC(),
		SignedBy:     opts.SignedBy,
	}, nil
}

// cosignBundle 只取 sign-blob --bundle 输出里我们需要的那一个字段（Sigstore
// bundle 的完整 schema 里还有证书链、Rekor 条目等，我们一概不关心——本包全部
// 验签逻辑只吃 Signature.Value 这一个 base64 签名值，见包注释）。
//
// 两个字段对应实测过的两种真实 --bundle 输出形状，不是猜的：
//   - cosign v2.x（本包 cosignInstallHint 让人 go install 装的那个版本就是这
//     一支）写的是扁平的 {"base64Signature": "..."}
//   - cosign v3.x（sigstore/cosign-installer@v4 装的那支，commit 5b2d70a 就是
//     在这支上踩的）写的是新版 Sigstore bundle，签名嵌在
//     {"messageSignature": {"signature": "..."}} 里
//
// 两种目前都在用（见上面两条各自的出处），所以两个字段都要认，谁非空用谁。
type cosignBundle struct {
	Base64Signature  string `json:"base64Signature"` // cosign v2.x
	MessageSignature struct {
		Signature string `json:"signature"`
	} `json:"messageSignature"` // cosign v3.x 起
}

// readBundleSignature 从 cosign --bundle 写出的 JSON 里取出签名值。
func readBundleSignature(bundleFile string) (string, error) {
	raw, err := os.ReadFile(bundleFile)
	if err != nil {
		return "", clierr.New(clierr.CodeConfigInvalid, "错误：读取 cosign 签名结果失败").
			WithDetail("路径", bundleFile).WithCause(err)
	}

	var bundle cosignBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return "", clierr.New(clierr.CodeConfigInvalid, "错误：解析 cosign bundle 失败").
			WithDetail("路径", bundleFile).
			WithHint("cosign 输出格式可能变了，需要跟着更新 signArgs/readBundleSignature").
			WithCause(err)
	}
	value := strings.TrimSpace(bundle.MessageSignature.Signature)
	if value == "" {
		value = strings.TrimSpace(bundle.Base64Signature)
	}
	if value == "" {
		return "", clierr.New(clierr.CodeConfigInvalid, "错误：cosign bundle 里没有签名值").
			WithDetail("路径", bundleFile).
			WithHint("cosign 输出格式可能变了，需要跟着更新 signArgs/readBundleSignature")
	}
	return value, nil
}

// signFailed 把 cosign 的失败翻译成看得懂的话。
//
// 最常见的一种在 CI 里必然撞上：私钥要口令、但没有终端可以提示，cosign 报的是
//
//	reading key: inappropriate ioctl for device
//
// 字面意思是"对这个设备执行了不合适的 ioctl"——跟签名、跟口令一个字都不沾边，
// 谁看了都会往终端或权限的方向去查。真正要做的只是设 COSIGN_PASSWORD。
func signFailed(bin string, args []string, out []byte, cause error) error {
	output := strings.TrimSpace(string(out))
	err := clierr.New(clierr.CodeConfigInvalid, "错误：cosign 签名失败").
		WithDetail("命令", bin+" "+strings.Join(args, " "))

	if isPasswordPromptFailure(output) {
		return err.
			WithDetail("原因", "cosign 需要私钥口令，但当前环境没有终端可以输入").
			WithHint("用环境变量传入口令后重试：COSIGN_PASSWORD=<口令> brickkit publish --sign",
				"私钥没有口令时也要显式设置为空：COSIGN_PASSWORD= brickkit publish --sign").
			WithTip("CI 里请把口令放在密钥管理里注入 COSIGN_PASSWORD，不要写进流水线文件。").
			WithCause(cause)
	}

	return err.
		WithDetail("输出", output).
		WithHint("私钥有口令时，用环境变量 COSIGN_PASSWORD 传入以便非交互执行").
		WithCause(cause)
}

// isPasswordPromptFailure 判断 cosign 是不是卡在了要口令这一步。
func isPasswordPromptFailure(output string) bool {
	return strings.Contains(output, "inappropriate ioctl for device") ||
		strings.Contains(output, "Enter password for private key")
}

// signArgs 组装 cosign sign-blob 的参数。
//
// --tlog-upload=false 不是可选优化，是必须的：cosign **默认**会把签名条目上传到
// Sigstore 的公共 Rekor 透明日志，全世界可查。对开源组件那是优点（可审计、
// 可发现伪造），但 BrickKit 的组件大量是 private 的（007 §5.1），默认上传等于
// 把"某公司在某时刻发布了某个内部组件、其内容哈希是什么"公开出去。
// 要透明日志的项目可以自建 Rekor，那是另一件事。
//
// --bundle（不是旧版 --output-signature）+ --use-signing-config=false：
// cosign v3 起，--use-signing-config 默认 true，这时旧的
// --output-signature/--output-certificate 被直接忽略——不给 --bundle 就直接
// 报错退出（"must specify --bundle with --new-bundle-format"）。这条已经在
// .github/workflows/release.yml 的另一处签名调用上踩过一次（commit
// 5b2d70a），当时只改了那一处，这里的组件签名调用留着旧参数没跟着改，
// 本该是同一个 bug。--bundle 输出的 JSON 里 messageSignature.signature
// 字段就是我们一直在用的那个 base64 签名值，见 readBundleSignature。
func signArgs(keyPath, bundleFile, blob string) []string {
	return []string{
		"sign-blob",
		"--key", keyPath,
		"--use-signing-config=false",
		"--tlog-upload=false",
		"--yes", // 不做交互确认，CI 里才能跑
		"--bundle", bundleFile,
		blob,
	}
}

// resolveCosign 定位 cosign 可执行文件。
func resolveCosign(configured string) (string, error) {
	if configured != "" {
		if _, err := os.Stat(configured); err != nil {
			return "", clierr.New(clierr.CodeConfigInvalid, "错误：找不到 cosign").
				WithDetail("路径", configured).
				WithHint(cosignInstallHint).
				WithCause(err)
		}
		return configured, nil
	}

	path, err := exec.LookPath("cosign")
	if err != nil {
		// go install 装到 GOPATH/bin，很多人的 PATH 里没有它——直接找一下，
		// 比让人对着"未安装"发愣强
		if home, homeErr := os.UserHomeDir(); homeErr == nil {
			candidate := filepath.Join(home, "go", "bin", "cosign")
			if _, statErr := os.Stat(candidate); statErr == nil {
				return candidate, nil
			}
		}
		return "", clierr.New(clierr.CodeConfigInvalid, "错误：签名需要 cosign，但没有找到").
			WithHint(cosignInstallHint,
				"装好后确认 cosign version 可执行（GOPATH/bin 要在 PATH 里）").
			WithTip("只有**发布**组件需要 cosign。安装组件时的签名校验不需要它。").
			WithCause(err)
	}
	return path, nil
}
