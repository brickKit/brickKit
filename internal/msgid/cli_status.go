package msgid

// internal/cli/status.go
const (
	CliStatusVersion                       = "cli.status.version"
	CliStatusNotCreated                    = "cli.status.not_created"
	CliStatusRunning                       = "cli.status.running"
	CliStatusQueryTheStatus                = "cli.status.query_the_status"
	CliStatusLocalAddress                  = "cli.status.local_address"
	CliStatusMsg                           = "cli.status.msg"
	CliStatusTheCurrentProjectHasNo        = "cli.status.the_current_project_has_no"
	CliStatusRunning2                      = "cli.status.running_2"
	CliStatusRunningButUnhealthy           = "cli.status.running_but_unhealthy"
	CliStatusShort                         = "cli.status.short"
	CliStatusRunningComponents             = "cli.status.running_components"
	CliStatusExitedExitCode                = "cli.status.exited_exit_code"
	CliStatusNotStartedComponents          = "cli.status.not_started_components"
	CliStatusNoComponentsNeedToBe          = "cli.status.no_components_need_to_be"
	CliStatusOrAddOneFromAn                = "cli.status.or_add_one_from_an"
	CliStatusNotRunningComponents          = "cli.status.not_running_components"
	CliStatusLocalhostIdeDebugMode         = "cli.status.localhost_ide_debug_mode"
	CliStatusLocalDebuggingLocalTrueNot    = "cli.status.local_debugging_local_true_not"
	CliStatusNoComponentsAreRunningPerhaps = "cli.status.no_components_are_running_perhaps"
	CliStatusPortUnknownNoLocalportIs      = "cli.status.port_unknown_no_localport_is"
	CliStatusAddAllTheComponentsUnder      = "cli.status.add_all_the_components_under"
	CliStatusProjectStatusDeployTarget     = "cli.status.project_status_deploy_target"
	CliStatusTheDependencyGraphCouldNot    = "cli.status.the_dependency_graph_could_not"
	CliStatusViewTheLogsToFind             = "cli.status.view_the_logs_to_find"
	CliStatusRunningAndResourceStatusAre   = "cli.status.running_and_resource_status_are"
	CliStatusLocalSessionRunning           = "cli.status.local_session_running"
	CliStatusLong                          = "cli.status.long"
)

// 手工处理的条目
const (
	CliStatusReasonUnknown         = "cli.status.reason_unknown"
	CliStatusViaOverrideYaml       = "cli.status.via_override_yaml"
	CliStatusViaOverrideYamlSuffix = "cli.status.via_override_yaml_suffix"
	CliStatusInShell               = "cli.status.in_shell"
)
