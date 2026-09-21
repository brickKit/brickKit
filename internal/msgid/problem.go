package msgid

// ProblemSet 里 manifest 与 config 共用的校验措辞（两处措辞完全一致才放这里）
const (
	ProblemLabelTypeMismatch     = "problem.label.type_mismatch"
	ProblemLabelParseFailed      = "problem.label.parse_failed"
	ProblemTopLevelMustBeMapping = "problem.top_level_must_be_mapping"
	ProblemMustBeArray           = "problem.must_be_array"
	ProblemMustBeMapping         = "problem.must_be_mapping"
	ProblemPortOutOfRange        = "problem.port_out_of_range"
	ProblemResourceKindUnknown   = "problem.resource_kind_unknown"
	ProblemValidationFailed      = "problem.validation_failed"
	ProblemHintCheckPermissions  = "problem.hint.check_permissions"
	ProblemHintCheckSyntax       = "problem.hint.check_syntax"
	ProblemMustBeOneOfThree      = "problem.must_be_one_of_three"
)
