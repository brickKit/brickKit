package msgid

// internal/cli/hooks.go
const (
	CliHooksErrorFailedToLocateThe         = "cli.hooks.error_failed_to_locate_the"
	CliHooksErrorFailedToCreateThe         = "cli.hooks.error_failed_to_create_the"
	CliHooksErrorFailedToWriteThe          = "cli.hooks.error_failed_to_write_the"
	CliHooksErrorAPreCommitHook            = "cli.hooks.error_a_pre_commit_hook"
	CliHooksLinesToAdd                     = "cli.hooks.lines_to_add"
	CliHooksThePlatformNeverOverwritesYour = "cli.hooks.the_platform_never_overwrites_your"
	CliHooksJustAddTheLinesAbove           = "cli.hooks.just_add_the_lines_above"
	CliHooksIfYouAreSureThat               = "cli.hooks.if_you_are_sure_that"
)

// 手工处理的条目（不适合自动改写）
const (
	CliHooksHeaderComment    = "cli.hooks.header_comment"
	CliHooksBrickkitNotFound = "cli.hooks.brickkit_not_found"
)
