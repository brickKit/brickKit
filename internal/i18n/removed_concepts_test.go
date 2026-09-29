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
	// up / down 的 --context 已删除：换集群就换一份部署文件（k8s.context），命令行上不能临时改
	"up --context",
	"down --context",
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

// 设计文档（new_plan/ 下的提案与附录）与内部的反馈记录不随 CLI 发布：帮助文本里写"（附录 A4）"、
// 生成文件里写"（brickKit 反馈：…）"，使用者既找不到这些文档，也读不懂那个编号。要讲的道理直接写在文本里。
var designDocCitations = []string{"附录", "提案", "Appendix A", "proposal §", "§", "brickKit 反馈", "brickKit feedback"}

func TestCatalogsNeverCiteTheDesignDocuments(t *testing.T) {
	for _, lang := range SupportedLangs() {
		for id, text := range catalogFor(lang) {
			for _, cite := range designDocCitations {
				if strings.Contains(text, cite) {
					t.Errorf("%s %s 引用了使用者看不到的设计文档 %q：%s", lang, id, cite, text)
				}
			}
		}
	}
}
