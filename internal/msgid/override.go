package msgid

// internal/override 包自己的文案（override.yaml 的解析、单文件校验、跨文件校验与
// 漂移检测）。
const (
	OverrideReadFailed             = "override.read_failed"
	OverrideNotValidYAML           = "override.not_valid_yaml"
	OverrideValidationFailed       = "override.validation_failed"
	OverrideComponentMustBeMapping = "override.component_must_be_mapping"
	OverrideTargetInvalid          = "override.target_invalid"
	OverrideDuplicateComponent     = "override.duplicate_component"
	OverrideCheckFailed            = "override.check_failed"
	OverrideTargetUpgradeRejected  = "override.target_upgrade_rejected"
	OverrideDanglingComponent      = "override.dangling_component"
	OverrideModeK8sForbidden       = "override.mode_k8s_forbidden"
	OverrideTargetDrift            = "override.target_drift"
	OverrideComponentDrift         = "override.component_drift"
)
