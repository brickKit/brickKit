package msgid

// internal/shell/shell.go
const (
	ShellPortConflict                     = "shell.port_conflict"
	ShellLabelPortOwner                   = "shell.label.port_owner"
	ShellHintPortsMustDiffer              = "shell.hint.ports_must_differ"
	ShellServedByTargetNotFound           = "shell.served_by_target_not_found"
	ShellLabelServedByTarget              = "shell.label.served_by_target"
	ShellServedByTargetNotInProjectDetail = "shell.served_by_target_not_in_project_detail"
	ShellHintCheckServedByValue           = "shell.hint.check_served_by_value"
	ShellLabelVariableName                = "shell.label.variable_name"
)

// shell 补漏：不经过 clierr、直接当数据显示给用户的文案
const (
	ShellOwnerShellItself = "shell.owner.shell_itself"
	ShellOwnerComponent   = "shell.owner.component"
)

// 跨包共享（改名自各包私有的同文案 key）
const (
	// ServedByFallbackStandalone：外壳没跑，成员这次按自己的镜像独立部署
	// （不是外壳那份代码）。
	ServedByFallbackStandalone          = "shell.served.fallback_standalone"
	ServedByFallbackReasonDetail        = "shell.served.fallback_reason_detail"
	HintFallbackEnableShellToMergeAgain = "shell.served.hint.fallback_enable_shell_to_merge_again"
	ShellKindWithoutBlock               = "shell.kind_without_block"
	ShellBlockWithoutKind               = "shell.block_without_kind"
	ShellHintAddKind                    = "shell.hint_add_kind"
	ShellHintRemoveKind                 = "shell.hint_remove_kind"
	ShellMemberNotHostable              = "shell.member_not_hostable"
	ShellLabelCanHost                   = "shell.label_can_host"
	ShellHintMemberNotHostable          = "shell.hint_member_not_hostable"
	ShellMemberVersionsMismatch         = "shell.member_versions_mismatch"
	ShellMemberVersionMismatch          = "shell.member_version_mismatch"
	ShellHintUpgradeShell               = "shell.hint_upgrade_shell"
	ShellHintMoveMemberOut              = "shell.hint_move_member_out"
	ShellHintKeepBothVersions           = "shell.hint_keep_both_versions"
	ShellHintPinDeclaredVersion         = "shell.hint_pin_declared_version"
	ShellMemberValueUnresolved          = "shell.member_value_unresolved"
	ShellMemberValueUnresolvedReason    = "shell.member_value_unresolved_reason"
	ShellHintSetVariable                = "shell.hint_set_variable"
	ShellMemberExistingSecret           = "shell.member_existing_secret"
	ShellMemberExistingSecretReason     = "shell.member_existing_secret_reason"
	ShellHintExistingSecret             = "shell.hint_existing_secret"
	ShellMemberInvalidUTF8              = "shell.member_invalid_utf8"
	ShellMemberInvalidUTF8Reason        = "shell.member_invalid_utf8_reason"
	ShellHintInvalidUTF8                = "shell.hint_invalid_utf8"
	ShellMergeCycle                     = "shell.merge_cycle"
	ShellMergeCycleInShell              = "shell.merge_cycle_in_shell"
	ShellLabelMergeCycleEdge            = "shell.label_merge_cycle_edge"
	ShellMergeCycleReason               = "shell.merge_cycle_reason"
	ShellHintMergeCycleHostBoth         = "shell.hint_merge_cycle_host_both"
	ShellHintMergeCycleOptional         = "shell.hint_merge_cycle_optional"
	ShellSkipWaitForInvalid             = "shell.skip_wait_for_invalid"
	ShellSkipWaitForNotRequired         = "shell.skip_wait_for_not_required"
	ShellSkipWaitForNoRequired          = "shell.skip_wait_for_no_required"
	ShellHintMergeCycleSkipWait         = "shell.hint_merge_cycle_skip_wait"
	ShellHintMergeCycleSkipWaitOne      = "shell.hint_merge_cycle_skip_wait_one"
	ShellHintSkipWaitForOnMember        = "shell.hint_skip_wait_for_on_member"
)
