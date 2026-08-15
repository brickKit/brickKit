package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/market"
	"github.com/brickkit/brickkit/internal/security"
	"github.com/brickkit/brickkit/internal/source"
)

// publishFlags 是 brickkit publish 的参数（004 §3.11、010 §7.3）。
type publishFlags struct {
	path         string
	visibility   string
	changelog    string
	market       string
	sourceType   string
	gitURL       string
	sign         bool
	key          string
	publicKeyRef string
	signedBy     string
}

// newPublishCommand 实现 brickkit publish（004 §3.11）。
func newPublishCommand(opts *Options) *cobra.Command {
	var f publishFlags

	cmd := &cobra.Command{
		Use:     "publish",
		Short:   "发布组件到市场（需先 brickkit login）",
		GroupID: groupMarket,
		Long: `把组件发布到市场（004 §3.11）。

行为：
  1. 检查登录状态（credentials 或 sources.authToken），未登录报错
  2. 读取组件目录中的 component.yaml 并校验 Manifest 格式
  3. 检查镜像引用是否有效，并确认 artifacts 声明的文件都在
  4. 建 draft 版本 → 上传产物 → 转 stable
  5. 设置可见性（--visibility）

发布分三步是有意的：版本转 stable 时市场会校验"文件与 artifacts 声明一致"，
先建 draft 才能保证不会出现"已 stable 但文件没传齐"的半成品。

--path 支持归档目录，例如 ./components/.archived/erp/backend。`,
		Example: `  brickkit publish --path ./components/people/basic
  brickkit publish --path ./components/people/basic --visibility private
  brickkit publish --path ./components/people/basic --changelog "新增人员状态字段"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPublish(cmd.Context(), opts, f)
		},
	}

	cmd.Flags().StringVar(&f.path, "path", ".", "组件源码目录（含 component.yaml）")
	cmd.Flags().StringVar(&f.visibility, "visibility", "", "可见性：public | private（默认沿用市场侧设置）")
	cmd.Flags().StringVar(&f.changelog, "changelog", "", "本次版本的更新说明")
	cmd.Flags().StringVar(&f.market, "market", "", "市场地址（默认取 brickkit.yaml 中的 market 安装源）")
	cmd.Flags().StringVar(&f.sourceType, "source-type", "",
		"来源类型：git（开源）| registry（闭源）。默认按组件目录的 git remote 推断")
	cmd.Flags().StringVar(&f.gitURL, "git-url", "", "开源组件的 Git 仓库地址（默认取组件目录的 origin）")
	cmd.Flags().BoolVar(&f.sign, "sign", false, "对组件签名后发布（cosign）")
	cmd.Flags().StringVar(&f.key, "key", "", "cosign 私钥路径（默认 "+defaultSigningKey+"）")
	cmd.Flags().StringVar(&f.publicKeyRef, "public-key-ref", "",
		"写进签名的公钥 ref（默认按 --key 的 .key → .pub 推导）")
	cmd.Flags().StringVar(&f.signedBy, "signed-by", "", "签名者标识，如 release-bot@example.com")
	return cmd
}

func runPublish(ctx context.Context, opts *Options, f publishFlags) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if err := validateVisibility(f.visibility); err != nil {
		return err
	}
	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	marketURL, err := resolveMarketURL(layout, f.market)
	if err != nil {
		return publishAuthHint(err)
	}
	token, err := resolvePublishToken(opts, layout, marketURL)
	if err != nil {
		return err
	}

	// 所有本地检查都在联网之前做完：任何一项不过关，市场里都不该留下痕迹
	pkg, err := loadPublishPackage(f)
	if err != nil {
		return err
	}

	opts.Printf("📤 发布 %s@%s\n", pkg.manifest.Metadata.ID, pkg.manifest.Metadata.Version)
	opts.Printf("   ✅ Manifest 校验通过\n")
	opts.Printf("   ✅ 镜像引用有效：%s\n", pkg.manifest.Deployment.Image)

	// 签名也在联网之前：版本号一旦建出来就不可回收（市场侧 18.14），
	// 不能因为密钥路径写错就烧掉一个有语义的版本号
	if err := signPackage(ctx, opts, pkg, f); err != nil {
		return err
	}

	client := market.New(marketURL, token)
	if err := uploadRelease(ctx, opts, client, pkg, f); err != nil {
		return err
	}

	opts.Printf("   ✅ 上传成功\n")
	opts.Printf("🎉 发布完成\n")
	opts.Printf("   组件：%s@%s\n", pkg.manifest.Metadata.ID, pkg.manifest.Metadata.Version)
	if f.visibility != "" {
		opts.Printf("   可见性：%s\n", f.visibility)
	}
	opts.Printf("   来源类型：%s\n", pkg.sourceType)
	if pkg.gitURL != "" {
		opts.Printf("   Git 仓库：%s\n", pkg.gitURL)
	}
	opts.Printf("   市场地址：%s\n", marketURL)
	return nil
}

// publishPackage 是一份校验完毕、可以上传的发布包。
type publishPackage struct {
	root     string
	manifest *manifest.Manifest
	// signature 是 --sign 生成的签名（008 §8.3），未签名时为 nil。
	signature *security.Signature
	// document 是 component.yaml 转成的 JSON，原样上传：
	// 走结构体转一手会把市场认识、而 CLI 还没建模的字段丢掉。
	document json.RawMessage
	// files 是 artifacts 声明的文件（相对路径 → 内容）。
	files      map[string][]byte
	fileOrder  []string
	sourceType string
	gitURL     string
}

// loadPublishPackage 读组件目录并做全部本地校验。
func loadPublishPackage(f publishFlags) (*publishPackage, error) {
	root, err := filepath.Abs(f.path)
	if err != nil {
		root = f.path
	}
	manifestPath := filepath.Join(root, manifestFileName)

	if _, err := os.Stat(manifestPath); err != nil {
		return nil, clierr.New(clierr.CodeManifestInvalid, "错误：组件目录中没有 component.yaml").
			WithDetail("路径", manifestPath).
			WithHint(
				"用 --path 指向组件源码目录（含 component.yaml）",
				"归档目录也可以，例如 --path ./components/.archived/people/basic",
			)
	}

	m, err := manifest.ParseFile(manifestPath)
	if err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if err := checkImageReference(m.Deployment.Image); err != nil {
		return nil, err
	}

	document, err := manifestDocument(manifestPath)
	if err != nil {
		return nil, err
	}

	pkg := &publishPackage{root: root, manifest: m, document: document, files: map[string][]byte{}}
	if err := pkg.loadArtifactFiles(); err != nil {
		return nil, err
	}
	pkg.sourceType, pkg.gitURL = resolveOrigin(root, f)
	return pkg, nil
}

// loadArtifactFiles 把 artifacts 声明的文件读进内存。
//
// 在建版本之前就全部读出来：等传到一半才发现少文件，市场里会留下一个
// 永远转不了 stable 的 draft 版本。
func (p *publishPackage) loadArtifactFiles() error {
	for _, artifact := range p.manifest.Artifacts {
		for _, file := range artifact.Files {
			if _, seen := p.files[file]; seen {
				continue
			}
			path := filepath.Join(p.root, filepath.FromSlash(file))
			content, err := os.ReadFile(path)
			if err != nil {
				return clierr.New(clierr.CodeManifestInvalid, "错误：artifacts 声明的文件不存在").
					WithDetail("组件", p.manifest.Metadata.ID).
					WithDetail("产物类型", artifact.Type).
					WithDetail("文件", file).
					WithDetail("查找路径", path).
					WithHint(
						"确认文件已生成（如 proto / openapi 需要先构建）",
						"或修正 component.yaml 中 artifacts.files 的路径",
					).WithCause(err)
			}
			p.files[file] = content
			p.fileOrder = append(p.fileOrder, file)
		}
	}
	return nil
}

// manifestDocument 把 component.yaml 原样转成 JSON。
func manifestDocument(path string) (json.RawMessage, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, clierr.New(clierr.CodeManifestInvalid, "错误：读取 component.yaml 失败").
			WithDetail("路径", path).WithCause(err)
	}

	var document any
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, clierr.New(clierr.CodeManifestInvalid, "错误：component.yaml 不是合法 YAML").
			WithDetail("路径", path).WithCause(err)
	}

	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, clierr.New(clierr.CodeManifestInvalid, "错误：component.yaml 无法转换为 JSON").
			WithDetail("路径", path).WithCause(err)
	}
	return encoded, nil
}

// uploadRelease 执行发布三步：建 draft → 上传产物 → 转 stable。
func uploadRelease(
	ctx context.Context, opts *Options, client *market.Client, pkg *publishPackage, f publishFlags,
) error {
	id := pkg.manifest.Metadata.ID
	version := pkg.manifest.Metadata.Version

	err := client.CreateVersion(ctx, id, market.PublishRequest{
		Version:    version,
		Status:     versionStatusDraft,
		Manifest:   pkg.document,
		SourceType: pkg.sourceType,
		GitURL:     pkg.gitURL,
		Changelog:  f.changelog,
		Signature:  pkg.signature,
	})
	if err != nil {
		return err
	}

	if len(pkg.fileOrder) > 0 {
		if err := uploadArtifacts(ctx, client, pkg); err != nil {
			return err
		}
		opts.Printf("   ✅ artifacts 上传成功（%d 个文件）\n", len(pkg.fileOrder))
	}

	// 转 stable 时市场会校验文件是否与 artifacts 声明一致，这一步过了才算真发布
	if err := client.SetVersionStatus(ctx, id, version, versionStatusStable); err != nil {
		return err
	}

	// 可见性放在最后：先改可见性再发布，中间那段时间组件处于"存在但可见性未定"的状态
	if f.visibility != "" {
		if err := client.SetVisibility(ctx, id, f.visibility); err != nil {
			return err
		}
	}
	return nil
}

// uploadArtifacts 按市场登记的产物条目逐个文件上传。
func uploadArtifacts(ctx context.Context, client *market.Client, pkg *publishPackage) error {
	id := pkg.manifest.Metadata.ID
	version := pkg.manifest.Metadata.Version

	entries, err := client.ListArtifacts(ctx, id, version)
	if err != nil {
		return err
	}

	// 文件 → artifactId。市场按 Manifest 登记产物，所以这里一定能对上；
	// 对不上说明市场与 CLI 对 Manifest 的理解出现了分歧，必须直说。
	target := map[string]string{}
	for _, entry := range entries {
		for _, file := range entry.Files {
			target[file] = entry.ID
		}
	}

	for _, file := range pkg.fileOrder {
		artifactID, ok := target[file]
		if !ok {
			return clierr.New(clierr.CodeManifestInvalid, "错误：市场没有登记该产物文件").
				WithDetail("组件", id+"@"+version).
				WithDetail("文件", file).
				WithHint("确认市场服务版本与 CLI 兼容，或稍后重试")
		}
		if err := client.UploadArtifact(ctx, id, version, artifactID, file, pkg.files[file]); err != nil {
			return err
		}
	}
	return nil
}

// resolvePublishToken 按 004 §5.3 的优先级取 Token：
// .brickkit/credentials（登录态）> brickkit.yaml 的 sources.authToken。
func resolvePublishToken(opts *Options, layout config.Layout, marketURL string) (string, error) {
	creds, err := source.LoadCredentials(layout.CredentialsPath())
	if err != nil {
		return "", err
	}
	if creds != nil && creds.Token != "" && creds.MatchesMarket(marketURL) {
		if creds.Expired(opts.now()) {
			return "", clierr.New(clierr.CodeTokenExpired, "错误：Token 已过期").
				WithDetail("过期时间", creds.ExpiresAt.Format(timeLayoutRFC3339)).
				WithHint("重新执行 brickkit login 登录市场")
		}
		return creds.Token, nil
	}

	// 登录态不可用时回落到配置里的 authToken
	if token := configAuthToken(layout, marketURL); token != "" {
		return token, nil
	}

	return "", clierr.New(clierr.CodeAuthRequired, "错误：发布失败：未登录").
		WithDetail("市场", marketURL).
		WithHint(
			"执行 brickkit login 登录市场",
			"或在 brickkit.yaml 中配置 sources.authToken",
		)
}

// configAuthToken 取该市场在 brickkit.yaml 中配置的 authToken。
func configAuthToken(layout config.Layout, marketURL string) string {
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return ""
	}
	for _, s := range cfg.Sources {
		if s.Type != config.SourceTypeMarket || !s.IsEnabled() {
			continue
		}
		if sameMarket(s.URL, marketURL) {
			return s.AuthToken
		}
	}
	return ""
}

func sameMarket(a, b string) bool {
	return strings.TrimRight(strings.TrimSpace(a), "/") == strings.TrimRight(strings.TrimSpace(b), "/")
}

// resolveOrigin 决定来源类型与 Git 地址（007 §11）。
//
// 显式参数优先；否则看组件目录是不是一个有 origin 的 Git 仓库：
// 有就是开源（git），没有就按闭源（registry）走镜像分发。
func resolveOrigin(root string, f publishFlags) (sourceType, gitURL string) {
	gitURL = strings.TrimSpace(f.gitURL)
	if gitURL == "" {
		gitURL = gitRemoteURL(root)
	}

	sourceType = strings.TrimSpace(f.sourceType)
	if sourceType == "" {
		if gitURL != "" {
			sourceType = sourceTypeGit
		} else {
			sourceType = sourceTypeRegistry
		}
	}
	if sourceType != sourceTypeGit {
		// 闭源组件不对外给仓库地址
		gitURL = ""
	}
	return sourceType, gitURL
}

// gitRemoteURL 读组件目录的 origin 地址；不是 Git 仓库时返回空字符串。
func gitRemoteURL(root string) string {
	cmd := exec.Command("git", "-C", root, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// validateVisibility 校验 --visibility 的取值。
func validateVisibility(visibility string) error {
	switch visibility {
	case "", visibilityPublic, visibilityPrivate:
		return nil
	default:
		return clierr.New(clierr.CodeInvalidArgument, "错误：--visibility 取值不合法").
			WithDetail("当前值", visibility).
			WithDetailf("可选值", "%s | %s", visibilityPublic, visibilityPrivate).
			WithExit(clierr.ExitUsage)
	}
}

// publishAuthHint 把"找不到市场地址"的提示换成发布语境下的说法。
func publishAuthHint(err error) error {
	cliErr := clierr.As(err)
	if cliErr == nil {
		return err
	}
	return cliErr.WithHint("或用 --market 指定要发布到哪个市场")
}

const (
	manifestFileName   = "component.yaml"
	versionStatusDraft = "draft"
	// versionStatusStable 是"可被安装"的状态（007 §6.1）。
	versionStatusStable = "stable"
	visibilityPublic    = "public"
	visibilityPrivate   = "private"
	sourceTypeGit       = "git"
	sourceTypeRegistry  = "registry"
	timeLayoutRFC3339   = "2006-01-02T15:04:05Z07:00"
)
