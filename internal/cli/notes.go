package cli

// 本文件是 release 与 publish 共用的发版说明参数：--notes <文字> 或 --notes-file <文件>。
//
// 说明是可选的——不写照样发，跟从前一样；写了，使用者 upgrade 时会看到它（git 来源读 tag 里的说明，
// 市场来源读市场存的那一份）。内容是 Markdown，原样保留。

import (
	"os"
	"path/filepath"
	"strings"

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
		if err := notesTextIsAFile(n.text, opts); err != nil {
			return "", err
		}
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

// notesTextIsAFile 拒绝交给 --notes 的一个文件：多半是把 --notes 当成了 --notes-file。照样发出去的话，说明就是那串路径，
// 要到使用者 upgrade 时才看得见，而且改不了（tag 已经推送，市场上的版本不再变）。
//
// 只认两种：一行、正好是一个存在的文件（相对当前目录）；或者一个词（没有空白）、以 .md / .markdown / .txt 结尾——
// 没有这个文件，多半是路径写错了。真想把这样一串字当说明的人可以写进文件、用 --notes-file，所以拒绝的代价很小。
func notesTextIsAFile(text string, opts *Options) error {
	if text == "" || strings.ContainsAny(text, "\r\n") {
		return nil
	}
	path := text
	if !filepath.IsAbs(path) {
		path = filepath.Join(opts.WorkDir, path)
	}
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliNotesIsAFile, text)).
			WithExit(clierr.ExitUsage).WithHint(i18n.T(msgid.CliNotesHintUseNotesFile, text))
	}
	switch strings.ToLower(filepath.Ext(text)) {
	case ".md", ".markdown", ".txt":
		if !strings.ContainsAny(text, " \t") {
			return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliNotesLooksLikeAFile, text)).
				WithExit(clierr.ExitUsage).WithHint(i18n.T(msgid.CliNotesHintLooksLikeAFile))
		}
	}
	return nil
}
