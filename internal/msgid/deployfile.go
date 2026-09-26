package msgid

// internal/deployfile（deploy.yaml / deploy.local.yaml / -f 指定的部署文件）的文案。
const (
	DeployfileComponentMustBeMapping = "deployfile.component_must_be_mapping"
	DeployfileVarNameInvalid         = "deployfile.var_name_invalid"
	DeployfileEntryDuplicate         = "deployfile.entry_duplicate"
	DeployfileDebugOnlyLocal         = "deployfile.debug_only_local"
	DeployfileModeInvalid            = "deployfile.mode_invalid"
	DeployfileLocalPortNeedsMode     = "deployfile.local_port_needs_mode"
	DeployfileMemberSelf             = "deployfile.member_self"
	DeployfileMemberDuplicate        = "deployfile.member_duplicate"
	DeployfileFieldIgnoredForTarget  = "deployfile.field_ignored_for_target"
	DeployfileMemberMustBeMapping    = "deployfile.member_must_be_mapping"
	DeployfileMemberNested           = "deployfile.member_nested"
	DeployfileSkipWaitForVersioned   = "deployfile.skip_wait_for_versioned"
	DeployfileSkipWaitForSelf        = "deployfile.skip_wait_for_self"
	DeployfileSkipWaitForDuplicate   = "deployfile.skip_wait_for_duplicate"
)
