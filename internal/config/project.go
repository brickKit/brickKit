package config

import (
	"fmt"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/yamlcomment"
)

// 项目名规则已移到 internal/projfile（三层文件重构 P1）；这几个包装在 P2 随本包一起删除。
const MaxProjectNameLen = projfile.MaxProjectNameLen

func ProjectNameRule() string               { return projfile.ProjectNameRule() }
func ValidateProjectName(name string) error { return projfile.ValidateProjectName(name) }
func SuggestProjectName(name string) string { return projfile.SuggestProjectName(name) }
func projectNameProblem(name string) string { return projfile.ProjectNameProblem(name) }

// Skeleton 生成 brickkit.yaml 骨架（004 §3.2）。
// fileName 只用于文件头注释，便于多环境配置（如 brickkit.prod.yaml）自解释。
func Skeleton(project, fileName string) []byte {
	if fileName == "" {
		fileName = DefaultConfigFile
	}
	return []byte(fmt.Sprintf(`%s%s
deploy:
  target: docker          # docker | k8s

%ssources:
  - id: local-dev
    type: local
    path: ./%s      # %s
%s  # - id: brickkit-market
  #   type: market
  #   url: https://market.example.com/api/v1

components: []
resources: []
`,
		yamlcomment.Block("", i18n.T(msgid.ConfigSkeletonHeader, fileName)), "project: "+project+"\n",
		yamlcomment.Block("", i18n.T(msgid.ConfigSkeletonSources)),
		DirComponents, i18n.T(msgid.ConfigSkeletonLocalDirNote),
		yamlcomment.Block("  ", i18n.T(msgid.ConfigSkeletonMarketNote))))
}
