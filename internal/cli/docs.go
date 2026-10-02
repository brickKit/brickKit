package cli

// 本文件实现 brickkit docs：CLI 自带的文档。跟 version、lang 一样是 CLI 自身的命令，不分组、不算进业务命令数。
//
// 文档编进二进制（internal/docpages），所以读到的永远是正在跑的这个版本的文档，不联网也读得到；
// 报错建议用 brickkit docs <页> 指过来，在项目里干活的人和 AI 不必去翻 BrickKit 的仓库。

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/docpages"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/version"
)

func newDocsCommand(opts *Options) *cobra.Command {
	var lang string
	cmd := &cobra.Command{
		Use:               "docs [<page>]",
		Short:             i18n.T(msgid.CliDocsShort),
		Long:              i18n.T(msgid.CliDocsLong, docpages.Repository, version.Display()),
		Example:           i18n.T(msgid.CliDocsExample, exampleOtherLang()),
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeDocPages(&lang),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDocs(opts, lang, args)
		},
	}
	cmd.Flags().StringVar(&lang, "lang", "", i18n.T(msgid.CliDocsLangFlag, strings.Join(docpages.Langs(), ", ")))
	_ = cmd.RegisterFlagCompletionFunc("lang", func(_ *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return withPrefix(docpages.Langs(), toComplete), noFileComp
	})
	return cmd
}

func runDocs(opts *Options, lang string, args []string) error {
	lang, err := docsLang(lang)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		printDocsIndex(opts, lang)
		return nil
	}
	body, err := docpages.Read(lang, args[0])
	if err != nil {
		var ids []string
		for _, p := range docpages.List(lang) {
			ids = append(ids, p.ID)
		}
		return withDidYouMean(clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliDocsNotFound, args[0])).
			WithHint(i18n.T(msgid.CliDocsHintList)).WithExit(clierr.ExitUsage), docpages.Normalize(args[0]), ids)
	}
	opts.Printf("%s", body)
	if !strings.HasSuffix(body, "\n") {
		opts.Printf("\n")
	}
	return nil
}

// docsLang：--lang 给了就用它，否则用 CLI 的语言（没有那种语言的文档时用英文）。
func docsLang(flag string) (string, error) {
	if flag == "" {
		if l := string(i18n.Current()); docpages.HasLang(l) {
			return l, nil
		}
		return "en", nil
	}
	if !docpages.HasLang(flag) {
		return "", clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliDocsBadLang, flag, strings.Join(docpages.Langs(), ", "))).
			WithHint(i18n.T(msgid.HintForExample, "brickkit docs --lang "+docpages.Langs()[0])).WithExit(clierr.ExitUsage)
	}
	return flag, nil
}

// printDocsIndex 列出全部页：每个顶层目录一组，组之间空一行。
func printDocsIndex(opts *Options, lang string) {
	pages := docpages.List(lang)
	width := 0
	for _, p := range pages {
		width = max(width, len(p.ID))
	}
	opts.Printf("%s\n", i18n.T(msgid.CliDocsListHeader, version.Display(), lang))
	group := ""
	for _, p := range pages {
		top, _, _ := strings.Cut(p.ID, "/")
		if top != group {
			opts.Printf("\n")
			group = top
		}
		opts.Printf("  %-*s  %s\n", width, p.ID, p.Title)
	}
}

// completeDocPages 补全页 ID。
func completeDocPages(lang *string) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		l, err := docsLang(*lang)
		if len(args) > 0 || err != nil {
			return nil, noFileComp
		}
		var ids []string
		for _, p := range docpages.List(l) {
			ids = append(ids, p.ID)
		}
		return withPrefix(ids, toComplete), noFileComp
	}
}
