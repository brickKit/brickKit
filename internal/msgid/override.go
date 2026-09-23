package msgid

// internal/override 包自己的文案（override.yaml 的解析与单文件校验）。
const (
	OverrideReadFailed             = "override.read_failed"
	OverrideNotValidYAML           = "override.not_valid_yaml"
	OverrideValidationFailed       = "override.validation_failed"
	OverrideComponentMustBeMapping = "override.component_must_be_mapping"
	OverrideTargetInvalid          = "override.target_invalid"
	OverrideDuplicateComponent     = "override.duplicate_component"
)
