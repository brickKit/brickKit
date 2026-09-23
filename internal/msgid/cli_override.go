package msgid

// brickkit up / sync / status 读取 override.yaml 时用到的文案。
// brickkit override 命令自己的帮助文本（Short/Long/Example）在它自己的任务里
// 补，不在这里——这份文件只装"读取 override.yaml 这个动作本身"产生的文案。
const (
	OverrideIgnoredNonDefaultConfig   = "cli.override.ignored_non_default_config"
	CliUpPodmanTargetNotImplemented   = "cli.up.podman_target_not_implemented"
	CliUpPodmanTargetDryRunStillWorks = "cli.up.podman_target_dry_run_still_works"

	CliOverrideShort                   = "cli.override.short"
	CliOverrideLong                    = "cli.override.long"
	CliOverrideExample                 = "cli.override.example"
	CliOverrideRefusesNonDefaultConfig = "cli.override.refuses_non_default_config"
	CliOverrideWriteFailed             = "cli.override.write_failed"
	CliOverrideGitignoreUpdated        = "cli.override.gitignore_updated"
	CliOverrideWritten                 = "cli.override.written"
	CliOverrideDriftNote               = "cli.override.drift_note"
)
