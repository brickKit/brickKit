package k8s

import (
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/inject"
)

// placement 是一条变量在 K8s 目标下的去处。
type placement int

const (
	placePlain           placement = iota // Deployment 的 env.value（${VAR} 生成时就展开：kubectl 不做替换）
	placeGeneratedSecret                  // 进平台生成的 Secret，env 用 secretKeyRef
	placeExistingSecret                   // 引用外部已经建好的 Secret（existingSecret），平台只引用不生成
)

// envPlacement 是 K8s 目标下"这条变量放哪"的唯一判定处：
// 密钥与 file:// 内容（常是证书、私钥）一律进 Secret，绝不明文写进 Deployment。
func envPlacement(v inject.Var) placement {
	switch {
	case v.IsSecretRef():
		return placeExistingSecret
	case v.Secret, v.Value.Kind == configdir.KindFileRef:
		return placeGeneratedSecret
	default:
		return placePlain
	}
}

// valueOf 在生成时求值：字面量原样，${VAR} 用严格的 expander（缺了就阻断，见 expand.go），
// file:// 读文件内容。
func (p *plan) valueOf(v inject.Var) (string, error) {
	switch v.Value.Kind {
	case configdir.KindEnvTemplate:
		return p.expand.value(v.Value.Text), nil
	case configdir.KindFileRef:
		return configdir.Evaluate(v.Value, p.root, nil)
	default:
		return v.Value.Text, nil
	}
}
