package i18n

import "github.com/brickkit/brickkit/internal/msgid"

var zh = map[string]string{
	msgid.DetailLine:      "%[1]s：%[2]s",
	msgid.HintLabelSingle: "建议：%[1]s",
	msgid.HintLabelMulti:  "建议：",

	msgid.VersionManifestLine:  "支持 Manifest 版本：%[1]s",
	msgid.VersionTargetsLine:   "支持部署目标：%[1]s",
	msgid.VersionCommitLine:    "Git commit：%[1]s",
	msgid.VersionBuildDateLine: "构建时间：%[1]s",

	msgid.ProjectMissing:           "错误：项目配置文件不存在",
	msgid.LabelPath:                "路径",
	msgid.ProjectMissingHintInit:   "在项目目录中执行 brickkit init <项目名称> 初始化项目",
	msgid.ProjectMissingHintConfig: "或用 --config 指定正确的配置文件路径",

	msgid.LangCmdShort:       "查看 CLI 当前的显示语言",
	msgid.LangSetCmdShort:    "设置 CLI 的显示语言",
	msgid.LangCurrentLine:    "当前语言：%[1]s（来源：%[2]s）",
	msgid.LangSourceEnv:      "BRICKKIT_LANG 环境变量",
	msgid.LangSourceConfig:   "全局配置文件",
	msgid.LangSourceDefault:  "默认值",
	msgid.LangSetSuccess:     "语言已设为 %[1]s",
	msgid.LangSetWriteFailed: "写入语言偏好失败",
	msgid.LangInvalidValue:   "不支持的语言：%[1]s（支持：%[2]s）",
}
