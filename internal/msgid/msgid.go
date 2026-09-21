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
	LabelShell     = "label.shell"

	// 生成文件（docker-compose.yaml、K8s 清单）头注释的公共部分。
	HeaderDoNotEdit   = "header.do_not_edit"
	HeaderOverwritten = "header.overwritten"
	HeaderGeneratedAt = "header.generated_at"
	HeaderProject     = "header.project"

	// ErrorPrefix 是"错误："/"Error: "这个前缀本身，只给"错误标题里要拼进一段
	// 运行时才知道的原文"的场景用（比如把市场服务端返回的 message 接在它后面）。
	// 标题全是固定文案的错误，照旧把前缀直接写进那一条目录文案里，不拼这个 key。
	ErrorPrefix = "prefix.error"

	// ListSeparator 是拼接一串名字（组件 ID、配置项……）时用的分隔符。
	// 中文习惯用"、"，英文习惯用", "——这跟明细行的"："一样，是标点本身
	// 随语言变化，不是随便哪句话里的字面量，所以放进共享 key 里。
	ListSeparator = "list.separator"

	// ClauseSeparator / SemicolonSeparator 是拼接"分句"用的逗号与分号：中文用全角，
	// 英文用半角加空格，同样是标点随语言变。
	ClauseSeparator    = "clause.separator"
	SemicolonSeparator = "semicolon.separator"

	// ServiceNamePlaceholder 是命令示例里"这里换成服务名"的占位符（logs 命令用）。
	ServiceNamePlaceholder = "placeholder.service_name"

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
	LangCmdShort       = "lang.cmd.short"
	LangSetCmdShort    = "lang.set_cmd.short"
	LangCurrentLine    = "lang.current_line"
	LangSourceEnv      = "lang.source.env"
	LangSourceConfig   = "lang.source.config"
	LangSourceDefault  = "lang.source.default"
	LangSetSuccess     = "lang.set.success"
	LangSetWriteFailed = "lang.set.write_failed"
	LangInvalidValue   = "lang.invalid_value"
)

// 跨包共享（改名自各包私有的同文案 key）
const (
	IOFailed            = "io.failed"
	HintCheckDiskAccess = "hint.check_disk_access"
	ActionMkdir         = "action.mkdir"
	ActionWriteFile     = "action.write_file"
)

// 跨包共享（改名自各包私有的同文案 key）
const (
	InvalidComponentID    = "invalid_component_id"
	HintComponentIDFormat = "hint.component_id_format"
)

// 跨包共享（改名自各包私有的同文案 key）
const (
	LabelUsage   = "label.usage"
	LabelExample = "label.example"
)
