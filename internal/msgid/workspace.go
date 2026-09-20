package msgid

// internal/workspace/workspace.go
const (
	WorkspaceLabelLocation             = "workspace.label.location"
	WorkspaceLabelFrom                 = "workspace.label.from"
	WorkspaceLabelTo                   = "workspace.label.to"

	WorkspaceCloneFailedDirExists      = "workspace.clone_failed_dir_exists"
	WorkspaceActiveDirReasonDetail     = "workspace.active_dir_reason_detail"
	WorkspaceHintDeleteOrRename        = "workspace.hint.delete_or_rename"
	WorkspaceHintAlreadyHaveSource     = "workspace.hint.already_have_source"

	WorkspaceCloneFailedArchived       = "workspace.clone_failed_archived"
	WorkspaceArchivedReasonDetail      = "workspace.archived_reason_detail"
	WorkspaceHintRunSync               = "workspace.hint.run_sync"
	WorkspaceHintEditInPlace           = "workspace.hint.edit_in_place"

	WorkspaceCannotCreateSourceDir     = "workspace.cannot_create_source_dir"

	WorkspaceCloneFailed               = "workspace.clone_failed"
	WorkspaceHintCheckNetworkAndURL    = "workspace.hint.check_network_and_url"
	WorkspaceHintCheckAccess           = "workspace.hint.check_access"

	WorkspaceDeleteSourceDirFailed     = "workspace.delete_source_dir_failed"
	WorkspaceHintCheckDirPermission    = "workspace.hint.check_dir_permission"

	WorkspaceRemoveBlockedSubmodule    = "workspace.remove_blocked_submodule"
	WorkspaceRemoveBlockedReasonDetail = "workspace.remove_blocked_reason_detail"
	WorkspaceHintManualDeinit          = "workspace.hint.manual_deinit"
	WorkspaceHintManualRmCache         = "workspace.hint.manual_rm_cache"
	WorkspaceHintManualCleanModules    = "workspace.hint.manual_clean_modules"

	WorkspaceMoveTargetExists         = "workspace.move_target_exists"
	WorkspaceHintCheckTargetContents  = "workspace.hint.check_target_contents"
	WorkspaceHintNoAutoDecision       = "workspace.hint.no_auto_decision"

	WorkspaceActionCreateDir          = "workspace.action.create_dir"
	WorkspaceActionMoveDir            = "workspace.action.move_dir"
	WorkspaceMoveErrorTemplate        = "workspace.move_error_template"
	WorkspaceHintCheckDirAndDisk      = "workspace.hint.check_dir_and_disk"

	WorkspaceMoveBlockedSubmodule      = "workspace.move_blocked_submodule"
	WorkspaceMoveBlockedReasonDetail   = "workspace.move_blocked_reason_detail"
	WorkspaceHintManualMoveEquivalent  = "workspace.hint.manual_move_equivalent"
	WorkspaceHintConfirmThenSync       = "workspace.hint.confirm_then_sync"
)
