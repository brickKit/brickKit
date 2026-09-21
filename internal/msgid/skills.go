package msgid

// internal/skills/install.go
const (
	SkillsInstallFailedToWrite              = "skills.install.failed_to_write"
	SkillsInstallFailedToRead               = "skills.install.failed_to_read"
	SkillsInstallFailedToReadTheEmbedded    = "skills.install.failed_to_read_the_embedded"
	SkillsInstallFailedToCreateTheDirectory = "skills.install.failed_to_create_the_directory"
)

// internal/skills/lock.go
const (
	SkillsLockFailedToParseSkillsLock     = "skills.lock.failed_to_parse_skills_lock"
	SkillsLockFailedToReadSkillsLock      = "skills.lock.failed_to_read_skills_lock"
	SkillsLockFailedToSerializeSkillsLock = "skills.lock.failed_to_serialize_skills_lock"
	SkillsLockFailedToCreateTheDirectory  = "skills.lock.failed_to_create_the_directory"
)

// State 的显示名（State 本身是语言中立的标识）
const (
	SkillsStateMissing   = "skills.state.missing"
	SkillsStateCurrent   = "skills.state.current"
	SkillsStateOutdated  = "skills.state.outdated"
	SkillsStateModified  = "skills.state.modified"
	SkillsStateUntracked = "skills.state.untracked"
)
