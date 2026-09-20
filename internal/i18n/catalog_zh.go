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
}
