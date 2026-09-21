package msgid

// gitrepo 补漏：不经过 clierr、直接当数据显示给用户的文案
const (
	GitrepoNotRepo        = "gitrepo.not_repo"
	GitrepoHooksDirFailed = "gitrepo.hooks_dir_failed"
)

// git 命令失败的错误前后缀（中间夹着被包装的底层错误）
const (
	GitrepoQueryFailedPrefix = "gitrepo.query_failed_prefix"
	GitrepoQueryFailedSuffix = "gitrepo.query_failed_suffix"
)
