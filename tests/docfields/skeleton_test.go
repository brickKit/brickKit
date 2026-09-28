package docfields_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// deploy.yaml 的组件条目写 mode / members：它和 brickkit.yaml 都有 components:，
// 但只有 deploy.yaml 认这两个字段。正确的片段不能被当成写错的 brickkit.yaml。
func TestSkeletonAcceptsDeployFileSnippet(t *testing.T) {
	body := "components:\n  - id: erp/backend\n    mode: enabled\n    members:\n      - id: people/basic\n"
	checked, file, problems := skeletonProblems(body)
	assert.True(t, checked, "这是一段合法的部署文件片段，必须被检查")
	assert.Empty(t, problems, "被当成了 %s：%v", file, problems)
}

// 完整的 deploy.yaml 骨架（target: + components:）里写错字段，必须被抓到，
// 而且报的是 deploy.yaml。
func TestSkeletonCatchesTypoInDeployFile(t *testing.T) {
	body := "target: docker\ncomponents:\n  - id: erp/backend\n    mdoe: debug\n"
	checked, file, problems := skeletonProblems(body)
	assert.True(t, checked, "带 target: 的部署文件骨架必须被检查")
	assert.Equal(t, "deploy.yaml", file)
	assert.NotEmpty(t, problems, "mdoe 是拼错的字段")
}

// brickkit.yaml 的片段照旧按 brickkit.yaml 查。
func TestSkeletonKeepsCheckingBrickkitYAML(t *testing.T) {
	body := "components:\n  - id: erp/backend\n    version: 1.0.0\n    requiredBy: [x/y]\n"
	checked, _, problems := skeletonProblems(body)
	assert.True(t, checked)
	assert.Empty(t, problems)

	checked, _, problems = skeletonProblems("sources:\n  - name: local-dev\n    typo: local\n")
	assert.True(t, checked)
	assert.NotEmpty(t, problems, "sources 里写错的字段必须被抓到")
}
