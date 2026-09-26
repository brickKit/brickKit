package msgid

// internal/compose/compose.go
const (
	ComposeHeaderComponents     = "compose.header.components"
	ComposeRenderFailed         = "compose.render_failed"
	ComposeHostPortConflict     = "compose.host_port_conflict"
	ComposeLabelHostPort        = "compose.label.host_port"
	ComposeHintChangeExposePort = "compose.hint.change_expose_port"
	ComposeHintDropExpose       = "compose.hint.drop_expose"
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

// internal/compose/quota.go
const (
	ComposeCPUQuotaInvalid          = "compose.cpu_quota_invalid"
	ComposeMemoryQuotaInvalid       = "compose.memory_quota_invalid"
	ComposeOwnerHostMember          = "compose.owner_host_member"
	ComposeOwnerHostMemberExtra     = "compose.owner_host_member_extra"
	ComposeSkipWaitForOnBareProcess = "compose.skip_wait_for_on_bare_process"
	ComposeSkipWaitForCoHosted      = "compose.skip_wait_for_co_hosted"
)
