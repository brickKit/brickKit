package i18n

import (
	"strings"
	"testing"
)

// 三层文件重构删掉的概念（提案 §5.2、§11.0，附录 A17）不能再出现在 CLI 给人看的文字里：
// 帮助文本里还写着 --config、deploy.target、基础资源，使用者照着敲，得到的只会是
// unknown flag 或一个 CLI 根本不认的字段。
var removedConcepts = []string{
	"--config ",
	"deploy.target",
	"servedBy",
	"override.yaml",
	"brickkit override",
	"--ignore-served-by",
	"envPrefix",
	"base resource",
	"基础资源",
	// 旧 brickkit.yaml 的 deploy.<字段>：这些设置搬进了部署文件的 k8s: 块（附录 A15），
	// 字段路径是 k8s.<字段>。提示里还写 deploy.networkPolicy，使用者照着写只会得到"未知字段"
	"deploy.context",
	"deploy.namespace",
	"deploy.createNamespace",
	"deploy.podSecurity",
	"deploy.imagePullSecrets",
	"deploy.ingressClass",
	"deploy.ingressAnnotations",
	"deploy.networkPolicy",
	"deploy.serviceAccount",
}

func TestCatalogsHaveNoRemovedConcepts(t *testing.T) {
	for _, lang := range SupportedLangs() {
		for id, text := range catalogFor(lang) {
			for _, gone := range removedConcepts {
				if strings.Contains(text, gone) {
					t.Errorf("%s %s 还在讲已删除的概念 %q：%s", lang, id, gone, text)
				}
			}
		}
	}
}
