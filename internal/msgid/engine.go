package msgid

// internal/engine/compose.go
const (
	EngineRegistryUnreachable      = "engine.registry_unreachable"
	EngineHintCheckNetworkRegistry = "engine.hint.check_network_registry"
	EngineImageUnauthorized        = "engine.image_unauthorized"
	EngineHintDockerLogin          = "engine.hint.docker_login"
	EngineHintCheckPullPermission  = "engine.hint.check_pull_permission"
	EngineImageNotFound            = "engine.image_not_found"
	EngineHintCheckImageField      = "engine.hint.check_image_field"
	EngineHintBuildLocalImage      = "engine.hint.build_local_image"
	EngineBinaryMissing            = "engine.binary_missing"
	EngineHintInstallDocker        = "engine.hint.install_docker"
	EngineExecFailed               = "engine.exec_failed"
	EngineStatusParseFailed        = "engine.status_parse_failed"
	EngineHintComposeV2            = "engine.hint.compose_v2"
	EngineNoneFound                = "engine.none_found"
	EngineLabelTried               = "engine.label.tried"
	EngineHintDryRunOnly           = "engine.hint.dry_run_only"
	EnginePodmanUnsupported        = "engine.podman_unsupported"
	EngineLabelDetected            = "engine.label.detected"
	EnginePodmanDetectedDetail     = "engine.podman_detected_detail"
	EngineLabelStuckAt             = "engine.label.stuck_at"
	EnginePodmanStuckDetail        = "engine.podman_stuck_detail"
	EngineLabelWhyNotHalf          = "engine.label.why_not_half"
	EnginePodmanWhyDetail          = "engine.podman_why_detail"
	EngineHintDryRunNoEngine       = "engine.hint.dry_run_no_engine"
)

// internal/engine/kubectl.go
const (
	EngineMissingSelector          = "engine.missing_selector"
	EngineLabelNamespace           = "engine.label.namespace"
	EngineSelectorReasonDetail     = "engine.selector_reason_detail"
	EngineHintInternalIssue        = "engine.hint.internal_issue"
	EngineKubectlMissing           = "engine.kubectl_missing"
	EngineHintInstallKubectl       = "engine.hint.install_kubectl"
	EngineHintSwitchToDocker       = "engine.hint.switch_to_docker"
	EngineKubectlFailed            = "engine.kubectl_failed"
	EngineMigrationFailed          = "engine.migration_failed"
	EngineLabelEvents              = "engine.label.events"
	EngineLabelLogs                = "engine.label.logs"
	EngineLabelEventsThenLogs      = "engine.label.events_then_logs"
	EngineEventsThenLogsDetail     = "engine.events_then_logs_detail"
	EngineHintMigrationBlocksMain  = "engine.hint.migration_blocks_main"
	EngineHintFixAndRerun          = "engine.hint.fix_and_rerun"
	EngineKubectlOutputUnparseable = "engine.kubectl_output_unparseable"
)
