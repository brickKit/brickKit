// Package msgid 声明 CLI 消息目录的 key。每个常量对应 internal/i18n 两份
// 目录（en/zh）里的一条文案；常量值本身（不是常量名）就是查表用的 key。
package msgid

const (
	// clierr 渲染骨架用的 key：跟"这是第几种错误"无关，只跟 Format() 的
	// 排版本身有关（明细行的 分隔符、"建议："这个标签）。
	DetailLine      = "detail.line"
	HintLabelSingle = "hint.label.single"
	HintLabelMulti  = "hint.label.multi"

	// internal/cli/version.go
	VersionManifestLine  = "version.manifest_line"
	VersionTargetsLine   = "version.targets_line"
	VersionCommitLine    = "version.commit_line"
	VersionBuildDateLine = "version.build_date_line"

	// internal/config/parse.go：PROJECT_MISSING
	ProjectMissing           = "project.missing"
	LabelPath                = "label.path"
	ProjectMissingHintInit   = "project.missing.hint.init"
	ProjectMissingHintConfig = "project.missing.hint.config"
)
