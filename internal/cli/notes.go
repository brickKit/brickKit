package cli

// 本文件是 release 与 publish 共用的发版说明参数：--notes <文字> 或 --notes-file <文件>。
//
// 说明是可选的——不写照样发，跟从前一样；写了，使用者 upgrade 时会看到它（git 来源读 tag 里的说明，
// 市场来源读市场存的那一份）。内容是 Markdown，原样保留。

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// notesFlags 是一条命令上的两个发版说明参数。
type notesFlags struct {
	text, file string
}

func (n *notesFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&n.text, "notes", "", i18n.T(msgid.CliNotesFlagNotes))
	cmd.Flags().StringVar(&n.file, "notes-file", "", i18n.T(msgid.CliNotesFlagNotesFile))
}

// given 判断这次有没有给说明（两个参数里的任何一个）。
func (n *notesFlags) given(cmd *cobra.Command) bool {
	return cmd.Flags().Changed("notes") || cmd.Flags().Changed("notes-file")
}

// read 返回这次的发版说明：--notes 的文字，或 --notes-file 的内容（相对当前目录）；都没给时为空。
func (n *notesFlags) read(cmd *cobra.Command, opts *Options) (string, error) {
	if cmd.Flags().Changed("notes") && cmd.Flags().Changed("notes-file") {
		return "", clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliNotesBoth)).
			WithExit(clierr.ExitUsage).WithHint(i18n.T(msgid.CliNotesHintUseOne))
	}
	if !cmd.Flags().Changed("notes-file") {
		return n.text, nil
	}
	path := n.file
	if !filepath.IsAbs(path) {
		path = filepath.Join(opts.WorkDir, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliNotesUnreadable, opts.display(path))).
			WithExit(clierr.ExitUsage).WithDetail(i18n.T(msgid.LabelReason), err.Error()).WithCause(err).
			WithHint(i18n.T(msgid.CliNotesHintCheckPath))
	}
	return string(data), nil
}
