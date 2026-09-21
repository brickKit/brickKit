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
	ShellServedByTargetNotRunning         = "shell.served_by_target_not_running"
	ShellNotRunningReasonDetail           = "shell.not_running_reason_detail"
	ShellHintCheckShellEnabled            = "shell.hint.check_shell_enabled"
	ShellHintRemoveServedBy               = "shell.hint.remove_served_by"
	ShellEndpointCollision                = "shell.endpoint_collision"
	ShellLabelVariableName                = "shell.label.variable_name"
	ShellEndpointCollisionReasonDetail    = "shell.endpoint_collision_reason_detail"
	ShellHintAlignVersions                = "shell.hint.align_versions"
	ShellConfigVarCollision               = "shell.config_var_collision"
	ShellConfigVarCollisionReasonDetail   = "shell.config_var_collision_reason_detail"
	ShellHintAlignConfigValues            = "shell.hint.align_config_values"
)

// shell 补漏：不经过 clierr、直接当数据显示给用户的文案
const (
	ShellOwnerShellItself = "shell.owner.shell_itself"
	ShellOwnerComponent   = "shell.owner.component"
)

// 跨包共享（改名自各包私有的同文案 key）
const (
	ServedMigrationSkipped          = "shell.served.migration_skipped"
	ServedHealthCheckNotIndependent = "shell.served.health_check_not_independent"
	ServedFieldsIgnored             = "shell.served.fields_ignored"
	HintShellCoversMigration        = "shell.served.hint.shell_covers_migration"
	HintMigrationCommand            = "shell.served.hint.migration_command"
	HintDropServedBy                = "shell.served.hint.drop_served_by"
)
