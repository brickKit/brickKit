package cli

// 本文件实现 brickkit lint：离线的结构校验。
//
// 它没有新增任何规则——只是把散在 up / add / publish 里、早就存在的结构检查，
// 收拢到一个不联网、不需要引擎、不写任何文件的入口。今天没有任何一个命令能对着
// 这两种东西单独跑一遍校验：
//   - 独立的组件仓库（只有 component.yaml、没有 brickkit.yaml）；
//   - 已经 add --local 过的本地组件——add --local 对已在配置里的同版本组件是静默跳过，
//     编辑之后引入的拼写错误，要到跑 up（或 up --dry-run）读到那份文件时才会暴露。
//
// 不做的事（都有明确的理由，见设计书 §3.4）：不解析依赖图、不检查 servedBy 指向的组件
// 是否存在（那要联网，留给 up / add）、不校验 configSchema 里 enum / minimum 对应的值
// （AGENTS.md §9.12：configSchema 是说明书，不是安全闸）。

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/skills"
	"github.com/brickkit/brickkit/internal/source"
)

// newLintCommand 实现 brickkit lint。
func newLintCommand(opts *Options) *cobra.Command {
	var strict bool

	cmd := &cobra.Command{
		Use:     "lint",
		Short:   "离线检查 brickkit.yaml 与 component.yaml 的结构",
		GroupID: groupProject,
		Long: `不联网、不需要 Docker / K8s，只读地把这个目录里的 YAML 结构检查一遍。

两种场景，按当前目录自动判断（与 brickkit skills 同一条规则，两者都有时按项目算）：

  项目（有 brickkit.yaml）
      检查 brickkit.yaml 本身，再检查本地安装源（type: local）目录下的每一份
      component.yaml——不管有没有 add 过。归档目录（.archived/）不检查。
      brickkit.yaml 自己没通过时，本地安装源在哪都不可信，会跳过后一步并说明。
  组件仓库（有 component.yaml、没有 brickkit.yaml）
      只检查这一份 component.yaml。

查的是已有的结构规则：必填字段、类型、未知字段（拼写笔误）、版本号格式、端口范围；
警告有两类——configSchema 里拼错的键（比如 defualt）不会生效，以及配置项名字撞上
平台保留变量。后一类比 up 查得更全：up 只在配置项有默认值、或被 config 覆盖时才会
碰到它，lint 把 configSchema 里声明的每一项都查一遍（与市场发布时同一个范围）。

不查：依赖能不能解析、servedBy 指向的组件在不在（这些要联网，留给 up / add）；
configSchema 里 enum、minimum 之类对应的值（平台不校验值，见 AGENTS.md §9.12）。

有错误时退出码为 1；只有警告时为 0，加 --strict 则警告也算失败，给 CI 门禁用。`,
		Example: `  brickkit lint
  brickkit lint --strict   警告也算失败（CI 门禁）
  brickkit lint --config brickkit.prod.yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLint(opts, strict)
		},
	}

	cmd.Flags().BoolVar(&strict, "strict", false, "警告也算失败（退出码 1），给 CI 门禁用")
	return cmd
}

// lintFile 是对一个文件的检查结论。
type lintFile struct {
	// path 是给人看的路径（相对工作目录）。
	path     string
	errors   []*clierr.Error
	warnings []*clierr.Error
}

func runLint(opts *Options, strict bool) error {
	scope, layout, err := detectScope(opts)
	if err != nil {
		return err
	}

	var files []lintFile
	var notes []string
	if scope == skills.ScopeComponent {
		opts.Printf("📦 组件仓库（有 %s、没有 %s）：只检查 %s\n",
			manifest.FileName, layout.ConfigName(), manifest.FileName)
		files = append(files, lintManifest(opts, filepath.Join(layout.Root, manifest.FileName), ""))
	} else {
		files, notes = lintProject(opts, layout)
	}

	return reportLint(opts, files, notes, strict)
}

// localSkippedNote 说明"本地组件的 component.yaml 没能检查"，与 brickkit.yaml 没通过时的那条对称。
// 原因已经由前面那条错误块说了，这里只交代后果：汇总里的文件数因此比实际少。
const localSkippedNote = "本地安装源枚举失败，已跳过本地组件的 " + manifest.FileName + "（先修好上面那条）"

// lintProject 检查 brickkit.yaml，再检查本地安装源里的每一份 component.yaml。
func lintProject(opts *Options, layout config.Layout) ([]lintFile, []string) {
	head := lintFile{path: displayPath(opts.WorkDir, layout.ConfigPath())}

	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		head.errors = append(head.errors, clierr.As(err))
		return []lintFile{head}, []string{
			"brickkit.yaml 没通过检查，本地安装源在哪都不可信，已跳过本地组件的 " + manifest.FileName,
		}
	}

	// 直接 source.New，不走 newSourceClient：后者会先去读 installer.publicKeys 指向的公钥文件，
	// 而公钥缺失是 up / add 该报的事，不该让一条"离线校验 YAML"的命令因此失败。
	// source.New 本身不联网——三种安装源都是惰性的，只有真去取 Manifest 才会碰网络，lint 从不取。
	//
	// 这是整个 CLI 里唯一不经 newSourceClient（也就是不带签名策略）的取源客户端。这个例外
	// **只安全在** lint 只调 LocalManifestFiles、从不取 Manifest：将来谁在这里加一次
	// client.Manifest，就等于悄悄绕过验签，还会写 .brickkit/manifests/ 缓存、可能联网，
	// "纯只读、不联网"的承诺当场破功。
	client, err := source.New(layout, cfg, source.Options{})
	if err != nil {
		head.errors = append(head.errors, clierr.As(err))
		return []lintFile{head}, []string{localSkippedNote}
	}
	defer func() { _ = client.Close() }()

	found, err := client.LocalManifestFiles()
	if err != nil {
		// 本地源的根目录不存在之类：那是 brickkit.yaml 里 sources[].path 配错了。
		// 枚举遇到第一个出错的源就整体失败，别的本地源里的组件因此一份也没查——汇总里的
		// 文件数会被低估，必须说出来，否则使用者改好 path 之前不知道还有文件没被检查
		head.errors = append(head.errors, clierr.As(err))
		return []lintFile{head}, []string{localSkippedNote}
	}

	files := []lintFile{head}
	for _, f := range found {
		files = append(files, lintManifest(opts, f.Path, f.ID))
	}
	return files, nil
}

// lintManifest 检查一份 component.yaml。dirID 非空时（项目模式）还要核对目录名与 metadata.id 一致。
func lintManifest(opts *Options, path, dirID string) lintFile {
	f := lintFile{path: displayPath(opts.WorkDir, path)}

	raw, err := os.ReadFile(path)
	if err != nil {
		f.errors = append(f.errors, clierr.New(clierr.CodeManifestInvalid,
			"错误：读取 "+manifest.FileName+" 失败").
			WithDetail("路径", f.path).
			WithDetail("原因", err.Error()).
			WithHint("检查文件权限"))
		return f
	}

	m, err := manifest.Parse(raw, f.path)
	if err != nil {
		f.errors = append(f.errors, clierr.As(err))
	} else {
		if dirID != "" && m.Metadata.ID != dirID {
			f.errors = append(f.errors, clierr.New(clierr.CodeManifestInvalid,
				"错误："+manifest.FileName+" 里的组件 ID 与目录名对不上").
				WithDetail("文件", f.path).
				WithDetail("目录名", dirID).
				WithDetail("metadata.id", m.Metadata.ID).
				WithHint("本地安装源按 <scope>/<name>/"+manifest.FileName+" 找组件，两者必须一致"))
		}
		// 是 up 那条警告的超集：up 只在配置项有默认值、或被 config 覆盖时才走到保留变量检查，
		// 这里把 configSchema 里声明的每一项都查一遍——与市场发布时同一个范围，
		// 作者在发布之前就该知道
		for _, w := range inject.ReservedKeyWarnings(m) {
			// 它的块里本来只有组件 ID；检查一堆文件时得告诉人是哪一份
			f.warnings = append(f.warnings, w.WithDetail("来源", f.path))
		}
	}
	// 与 Parse 成败无关：它只依赖 YAML 本身，语法错误时自己返回 nil
	f.warnings = append(f.warnings, manifest.PropertyKeyWarnings(raw, f.path)...)
	return f
}

// reportLint 把结论打印出来，并决定这条命令是成功还是失败。
//
// 报告写 stdout：它是这条命令的产出，不是"命令失败了"的错误。失败时另外返回一个
// 汇总错误，走 stderr 与 JSON 日志行的老路径。
func reportLint(opts *Options, files []lintFile, notes []string, strict bool) error {
	failed, warned := 0, 0
	for _, f := range files {
		if len(f.errors) == 0 && len(f.warnings) == 0 {
			opts.Printf("✅ %s\n", f.path)
			continue
		}
		for _, e := range f.errors {
			opts.Printf("%s", e.Format())
		}
		for _, w := range f.warnings {
			opts.Printf("%s", w.Format())
		}
		if len(f.errors) > 0 {
			failed++
		}
		warned += len(f.warnings)
	}

	for _, n := range notes {
		opts.Printf("ℹ️ %s\n", n)
	}
	opts.Printf("\n📋 检查了 %d 个文件：%d 个有错误，%d 条警告\n", len(files), failed, warned)

	if failed == 0 && (!strict || warned == 0) {
		return nil
	}
	e := clierr.New(clierr.CodeLintFailed, "错误：结构检查未通过").
		WithDetail("已检查", fmt.Sprintf("%d 个文件", len(files)))
	if failed > 0 {
		e = e.WithDetail("有错误", fmt.Sprintf("%d 个文件", failed))
	}
	if strict && warned > 0 {
		e = e.WithDetail("警告", fmt.Sprintf("%d 条（--strict：警告也算失败）", warned))
	}
	return e.WithHint("按上面逐条列出的位置修改，再执行 brickkit lint")
}
