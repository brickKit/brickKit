package msgid

// install：add / remove 的计划（internal/install）。
const (
	InstallUpgradeShellStillNeeded    = "install.upgrade_shell_still_needed"
	InstallHintUpgradeShellDependents = "install.hint_upgrade_shell_dependents"
	InstallUpgradeKindChange          = "install.upgrade_kind_change"
	InstallHintKindChange             = "install.hint_kind_change"
	InstallNoteDowngrade              = "install.note_downgrade"
	InstallUpgradeNotDefault          = "install.upgrade_not_default"
	InstallHintUpgradeDependents      = "install.hint_upgrade_dependents"
	InstallAddOtherVersion            = "install.add_other_version"
	InstallHintUpgradeInstead         = "install.hint_upgrade_instead"
	InstallHintAddDependent           = "install.hint_add_dependent"
	InstallNoteShellHostsOtherVersion = "install.note_shell_hosts_other_version"
	InstallNoteMemberUnderOtherShell  = "install.note_member_under_other_shell"
	InstallNoteOptionalDependent      = "install.note_optional_dependent"
	InstallRemoveHasDependents        = "install.remove_has_dependents"
	InstallHintRemoveDependentsFirst  = "install.hint_remove_dependents_first"
	InstallRemoveDefaultAmbiguous     = "install.remove_default_ambiguous"
	InstallLabelRemainingVersions     = "install.label_remaining_versions"
	InstallRemainingVersion           = "install.remaining_version"
	InstallHintRemoveOthersFirst      = "install.hint_remove_others_first"
	InstallHintUpgradeToChooseDefault = "install.hint_upgrade_to_choose_default"
)
