package msgid

// JSON 日志（stderr）里的 message 字段。日志的键名（command、elapsed_ms、
// error_code……）是机器读的稳定接口，永远是英文，不进目录；只有 message
// 这句给人看的话跟着语言走。脚本要按稳定标识分支，用 error_code，不用 message。
const (
	LogCommandStarted        = "log.command_started"
	LogCommandFinished       = "log.command_finished"
	LogCommandFailed         = "log.command_failed"
	LogWorkspaceTidied       = "log.workspace_tidied"
	LogProjectInitialized    = "log.project_initialized"
	LogSkillsInstalled       = "log.skills_installed"
	LogSkillsRefreshed       = "log.skills_refreshed"
	LogDeployFilesGenerated  = "log.deploy_files_generated"
	LogProjectStarted        = "log.project_started"
	LogProjectStopped        = "log.project_stopped"
	LogLoggedOut             = "log.logged_out"
	LogComponentAdded        = "log.component_added"
	LogComponentRemoved      = "log.component_removed"
	LogLocalComponentsAdded  = "log.local_components_added"
	LogSourceCloneDone       = "log.source_clone_done"
	LogK8sManifestsGenerated = "log.k8s_manifests_generated"
	LogOrphansPruned         = "log.orphans_pruned"
	LogArtifactsFetched      = "log.artifacts_fetched"
	LogPruneQueryFailed      = "log.prune_query_failed"
	LogPruneFailed           = "log.prune_failed"
)
