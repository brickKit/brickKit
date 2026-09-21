package msgid

// internal/inject/reserved.go、internal/inject/inject.go
const (
	InjectReservedConflict       = "inject.reserved_conflict"
	InjectLabelEnvVarName        = "inject.label.env_var_name"
	InjectLabelReservedPattern   = "inject.label.reserved_pattern"
	InjectLabelHandling          = "inject.label.handling"
	InjectReservedHandlingDetail = "inject.reserved_handling_detail"
	InjectHintRenameConfigKey    = "inject.hint.rename_config_key"
	InjectHintRenameExample      = "inject.hint.rename_example"

	InjectRequiredConfigMissing = "inject.required_config_missing"
	InjectLabelMissingConfig    = "inject.label.missing_config"
	InjectMissingConfigDetail   = "inject.missing_config_detail"
	InjectRequiredReasonDetail  = "inject.required_reason_detail"
	InjectHintSetValue          = "inject.hint.set_value"
	InjectHintEnvVarValue       = "inject.hint.env_var_value"

	InjectUnknownConfigKey    = "inject.unknown_config_key"
	InjectUnknownKeyReason    = "inject.unknown_key_reason"
	InjectDidYouMean          = "inject.did_you_mean"
	InjectUnknownKeyImpact    = "inject.unknown_key_impact"
	InjectLabelDeclaredConfig = "inject.label.declared_config"
	InjectUnknownKeyTip       = "inject.unknown_key_tip"

	InjectNoConfigSchema      = "inject.no_config_schema"
	InjectLabelIgnoredKeys    = "inject.label.ignored_keys"
	InjectIgnoredKeysDetail   = "inject.ignored_keys_detail"
	InjectNoSchemaImpact      = "inject.no_schema_impact"
	InjectHintAddConfigSchema = "inject.hint.add_config_schema"
	InjectNoSchemaTip         = "inject.no_schema_tip"
)
