package cli

import (
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/security"
	"github.com/brickkit/brickkit/internal/source"
)

// signaturePolicyOf 从项目配置构造签名校验策略（003 §3.6、008 §8.5）。
//
// 信任锚点只有一个来源：brickkit.yaml 的 installer.publicKeys。绝不从市场取
// 公钥——那等于市场自己给自己发证，签名就成了摆设（见 security.KeyRing 的说明）。
func signaturePolicyOf(opts *Options, cfg *config.Config) (source.SignaturePolicy, error) {
	if cfg == nil {
		return source.SignaturePolicy{}, nil
	}

	// 公钥路径相对**项目根目录**解析，不是当前工作目录：
	// 否则同一份配置在 components/ 子目录下执行会找不到公钥
	ring, err := security.LoadKeyRing(cfg.PublicKeys(), opts.WorkDir)
	if err != nil {
		return source.SignaturePolicy{}, err
	}
	return source.SignaturePolicy{Require: cfg.RequireSignature(), Ring: ring}, nil
}

// newSourceClient 构造带签名策略的安装源客户端。
//
// 所有取 Manifest 的命令都走这里。留一条不带策略的构造路径，就等于留一条
// 不验签的通路——而那条通路平时完全看不出来。
func newSourceClient(
	opts *Options, layout config.Layout, cfg *config.Config, sourceOpts source.Options,
) (*source.Client, error) {
	policy, err := signaturePolicyOf(opts, cfg)
	if err != nil {
		return nil, err
	}
	sourceOpts.Signature = policy
	return source.New(layout, cfg, sourceOpts)
}
