package i18n

import "github.com/brickkit/brickkit/internal/msgid"

var en = map[string]string{
	msgid.DetailLine:      "%[1]s: %[2]s",
	msgid.HintLabelSingle: "Suggestion: %[1]s",
	msgid.HintLabelMulti:  "Suggestions:",

	msgid.VersionManifestLine:  "Supported Manifest version: %[1]s",
	msgid.VersionTargetsLine:   "Supported deploy targets: %[1]s",
	msgid.VersionCommitLine:    "Git commit: %[1]s",
	msgid.VersionBuildDateLine: "Build date: %[1]s",

	msgid.ProjectMissing:           "Error: project config file not found",
	msgid.LabelPath:                "Path",
	msgid.ProjectMissingHintInit:   "Run brickkit init <project-name> inside the project directory to initialize it",
	msgid.ProjectMissingHintConfig: "Or point --config at the correct config file path",

	msgid.LangCmdShort:       "Show the CLI's current display language",
	msgid.LangSetCmdShort:    "Set the CLI's display language",
	msgid.LangCurrentLine:    "Current language: %[1]s (source: %[2]s)",
	msgid.LangSourceEnv:      "BRICKKIT_LANG environment variable",
	msgid.LangSourceConfig:   "global config file",
	msgid.LangSourceDefault:  "default",
	msgid.LangSetSuccess:     "Language set to %[1]s",
	msgid.LangSetWriteFailed: "Failed to save language preference",
	msgid.LangInvalidValue:   "Unsupported language: %[1]s (supported: %[2]s)",
}
