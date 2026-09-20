// Package msgid 声明 CLI 消息目录的 key。每个常量对应 internal/i18n 两份
// 目录（en/zh）里的一条文案；常量值本身（不是常量名）就是查表用的 key。
package msgid

const (
	// clierr 渲染骨架用的 key：跟"这是第几种错误"无关，只跟 Format() 的
	// 排版本身有关（明细行的 分隔符、"建议："这个标签）。
	DetailLine      = "detail.line"
	HintLabelSingle = "hint.label.single"
	HintLabelMulti  = "hint.label.multi"

	// 跨包共享的通用 Detail 标签——WithDetail 的 key 往往是"原因"、"组件"这类
	// 到处都用得到的词，不按来源包各开一份，统一在这里声明一次。是否该收进
	// 这里的判断标准：在两个以上不相关的包里出现、含义完全一致。
	LabelPath      = "label.path"
	LabelReason    = "label.reason"
	LabelComponent = "label.component"
	LabelSource    = "label.source" // "安装源"（sources 配置项），不是泛指任意来源
	LabelFile      = "label.file"
	LabelDir       = "label.dir"
	LabelImage     = "label.image"
	LabelConfigKey = "label.config_key"
	LabelImpact    = "label.impact"
	LabelAddress   = "label.address"
	LabelOutput    = "label.output"
	LabelCommand   = "label.command"
	LabelRepo      = "label.repo"
	LabelResource  = "label.resource"

	// internal/cli/version.go
	VersionManifestLine  = "version.manifest_line"
	VersionTargetsLine   = "version.targets_line"
	VersionCommitLine    = "version.commit_line"
	VersionBuildDateLine = "version.build_date_line"

	// internal/config/parse.go：PROJECT_MISSING
	ProjectMissing           = "project.missing"
	ProjectMissingHintInit   = "project.missing.hint.init"
	ProjectMissingHintConfig = "project.missing.hint.config"

	// internal/cli/lang.go
	LangCmdShort        = "lang.cmd.short"
	LangSetCmdShort     = "lang.set_cmd.short"
	LangCurrentLine     = "lang.current_line"
	LangSourceEnv       = "lang.source.env"
	LangSourceConfig    = "lang.source.config"
	LangSourceDefault   = "lang.source.default"
	LangSetSuccess      = "lang.set.success"
	LangSetWriteFailed  = "lang.set.write_failed"
	LangInvalidValue    = "lang.invalid_value"
)
