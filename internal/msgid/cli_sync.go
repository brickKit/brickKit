package msgid

// internal/cli/sync.go
const (
	CliSyncShort                                  = "cli.sync.short"
	CliSyncLong                                   = "cli.sync.long"
	CliSyncExample                                = "cli.sync.example"
	CliSyncTheWorkspaceNeedsNoTidying             = "cli.sync.the_workspace_needs_no_tidying"
	CliSyncThereIsNoComponentSource               = "cli.sync.there_is_no_component_source"
	CliSyncWorkspaceTidying                       = "cli.sync.workspace_tidying"
	CliSyncSActive                                = "cli.sync.s_active"
	CliSyncReason                                 = "cli.sync.reason"
	CliSyncWorkspaceTidiedActiveArchivedActivated = "cli.sync.workspace_tidied_active_archived_activated"
)

// 手工处理的条目（不适合自动改写）
const (
	CliSyncReasonStopped  = "cli.sync.reason_stopped"
	CliSyncReasonRestored = "cli.sync.reason_restored"
)
