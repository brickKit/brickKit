package msgid

// internal/compose/compose.go
const (
	ComposeHeaderComponents        = "compose.header.components"
	ComposeRenderFailed            = "compose.render_failed"
	ComposeHostPortConflict        = "compose.host_port_conflict"
	ComposeLabelHostPort           = "compose.label.host_port"
	ComposeHintChangeExposePort    = "compose.hint.change_expose_port"
	ComposeHintDropExpose          = "compose.hint.drop_expose"
	ComposeHostLooksLikeService    = "compose.host_looks_like_service"
	ComposeHostReasonDetail        = "compose.host_reason_detail"
	ComposeHintHostOnThisMachine   = "compose.hint.host_on_this_machine"
	ComposeHintHostElsewhere       = "compose.hint.host_elsewhere"
	ComposeHintHostAlreadyAttached = "compose.hint.host_already_attached"
)

// internal/compose/local.go
const (
	ComposeLabelClaimant              = "compose.label.claimant"
	ComposeHintChangePort             = "compose.hint.change_port"
	ComposeHintDebugOneAtATime        = "compose.hint.debug_one_at_a_time"
	ComposeLocalFieldsIgnored         = "compose.local_fields_ignored"
	ComposeLocalNoPortToMapDetail     = "compose.local_no_port_to_map_detail"
	ComposeLabelExposePortWritten     = "compose.label.expose_port_written"
	ComposeExposePortWrittenDetail    = "compose.expose_port_written_detail"
	ComposeLabelActualAddress         = "compose.label.actual_address"
	ComposeActualAddressDetail        = "compose.actual_address_detail"
	ComposeHintListenOnPort           = "compose.hint.listen_on_port"
	ComposeHintDropLocalForPorts      = "compose.hint.drop_local_for_ports"
	ComposeLocalLabelsIgnored         = "compose.local_labels_ignored"
	ComposeLocalNoLabelTargetDetail   = "compose.local_no_label_target_detail"
	ComposeLabelKeysWritten           = "compose.label.keys_written"
	ComposeHintLabelsOnShell          = "compose.hint.labels_on_shell"
	ComposeHintDropLocalForLabels     = "compose.hint.drop_local_for_labels"
	ComposeOwnerExpose                = "compose.owner.expose"
	ComposeOwnerLocalPort             = "compose.owner.local_port"
	ComposeOwnerExtraPort             = "compose.owner.extra_port"
	ComposeOwnerAutoAssigned          = "compose.owner.auto_assigned"
	ComposeOwnerDebugAccess           = "compose.owner.debug_access"
	ComposeOwnerDebugAccessViaShell   = "compose.owner.debug_access_via_shell"
	ComposeEnvHeader                  = "compose.env_header"
	ComposeLocalMigrationSkipped      = "compose.local_migration_skipped"
	ComposeLocalMigrationReasonDetail = "compose.local_migration_reason_detail"
	ComposeHintRunMigrationByHand     = "compose.hint.run_migration_by_hand"
	ComposeHintUseLocalDebugEnv       = "compose.hint.use_local_debug_env"
)

// internal/compose/servedby.go
const (
	ComposeServedMigrationSkipped          = "compose.served_migration_skipped"
	ComposeServedMigrationReasonDetail     = "compose.served_migration_reason_detail"
	ComposeHintShellCoversMigration        = "compose.hint.shell_covers_migration"
	ComposeHintMigrationCommand            = "compose.hint.migration_command"
	ComposeServedHealthCheckNotIndependent = "compose.served_health_check_not_independent"
	ComposeServedHealthCheckReasonDetail   = "compose.served_health_check_reason_detail"
	ComposeServedFieldsIgnored             = "compose.served_fields_ignored"
	ComposeServedFieldsReasonDetail        = "compose.served_fields_reason_detail"
	ComposeHintDropServedBy                = "compose.hint.drop_served_by"
)

// internal/compose/quota.go
const (
	ComposeCPUQuotaInvalid    = "compose.cpu_quota_invalid"
	ComposeMemoryQuotaInvalid = "compose.memory_quota_invalid"
)
