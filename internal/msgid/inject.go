package msgid

// internal/inject/reserved.go、internal/inject/inject.go
const (
	InjectReservedConflict       = "inject.reserved_conflict"
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
)
