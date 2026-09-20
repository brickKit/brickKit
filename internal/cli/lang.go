package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/userconfig"
)

// newLangCommand 实现 brickkit lang 与 brickkit lang set（跟 version 一样
// 是 CLI 自身命令：不分组、不计入"业务命令"数）。
func newLangCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lang",
		Short: i18n.T(msgid.LangCmdShort),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLangShow(opts)
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "set <en|zh>",
		Short: i18n.T(msgid.LangSetCmdShort),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLangSet(opts, args[0])
		},
	})
	return cmd
}

func runLangShow(opts *Options) error {
	lang, source := i18n.Resolve()
	opts.Printf("%s\n", i18n.T(msgid.LangCurrentLine, string(lang), langSourceLabel(source)))
	return nil
}

func runLangSet(opts *Options, value string) error {
	lang, ok := i18n.ParseLang(value)
	if !ok {
		return clierr.Newf(clierr.CodeInvalidArgument, i18n.T(msgid.LangInvalidValue, value, langNamesJoined())).
			WithExit(clierr.ExitUsage)
	}

	if err := userconfig.Save(&userconfig.Config{Lang: string(lang)}); err != nil {
		e := clierr.New(clierr.CodeInternal, i18n.T(msgid.LangSetWriteFailed)).WithCause(err)
		if path, pathErr := userconfig.Path(); pathErr == nil {
			e = e.WithDetail(i18n.T(msgid.LabelPath), path)
		}
		return e
	}

	i18n.SetCurrent(lang)
	opts.Printf("✅ %s\n", i18n.T(msgid.LangSetSuccess, string(lang)))
	return nil
}

func langSourceLabel(s i18n.Source) string {
	switch s {
	case i18n.SourceEnv:
		return i18n.T(msgid.LangSourceEnv)
	case i18n.SourceConfig:
		return i18n.T(msgid.LangSourceConfig)
	default:
		return i18n.T(msgid.LangSourceDefault)
	}
}

func langNamesJoined() string {
	return strings.Join(i18n.LangNames(), ", ")
}
