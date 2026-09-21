package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/market"
	"github.com/brickkit/brickkit/internal/msgid"
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
	// noPinDigest 跳过把镜像 tag 钉成 digest（P29）。
	noPinDigest bool
}

// newPublishCommand 实现 brickkit publish（004 §3.11）。
func newPublishCommand(opts *Options) *cobra.Command {
	var f publishFlags

	cmd := &cobra.Command{
		Use:     "publish",
		Short:   i18n.T(msgid.CliPublishShort),
		GroupID: groupMarket,
		Long:    i18n.T(msgid.CliPublishLong),
		Example: i18n.T(msgid.CliPublishExample),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPublish(cmd.Context(), opts, f)
		},
	}

	cmd.Flags().StringVar(&f.path, "path", ".", i18n.T(msgid.CliPublishComponentSourceDirectoryContainingComponent))
	cmd.Flags().StringVar(&f.visibility, "visibility", "", i18n.T(msgid.CliPublishVisibilityPublicPrivateDefaultsTo))
	cmd.Flags().StringVar(&f.changelog, "changelog", "", i18n.T(msgid.CliPublishReleaseNotesForThisVersion))
	cmd.Flags().StringVar(&f.market, "market", "", i18n.T(msgid.CliLoginMarketAddressDefaultsToThe))
	cmd.Flags().StringVar(&f.sourceType, "source-type", "",
		i18n.T(msgid.CliPublishSourceTypeGitOpenSource))
	cmd.Flags().StringVar(&f.gitURL, "git-url", "", i18n.T(msgid.CliPublishGitRepositoryAddressOfAn))
	cmd.Flags().BoolVar(&f.sign, "sign", false, i18n.T(msgid.CliPublishSignTheComponentWithCosign))
	cmd.Flags().StringVar(&f.key, "key", "", i18n.T(msgid.CliPublishPathOfTheCosignPrivate, defaultSigningKey))
	cmd.Flags().StringVar(&f.publicKeyRef, "public-key-ref", "",
		i18n.T(msgid.CliPublishPublicKeyRefWrittenInto))
	cmd.Flags().StringVar(&f.signedBy, "signed-by", "", i18n.T(msgid.CliPublishSignerIdentifierForExampleRelease))
	cmd.Flags().BoolVar(&f.noPinDigest, "no-pin-digest", false,
		i18n.T(msgid.CliPublishDonTPinTheImage))
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

	opts.Printf("%s\n", i18n.T(msgid.CliPublishPublishing, pkg.manifest.Metadata.ID, pkg.manifest.Metadata.Version))
	opts.Printf("%s\n", i18n.T(msgid.CliPublishManifestValidationPassed))
	renderWarnings(opts, pkg.warnings)
	opts.Printf("%s\n", i18n.T(msgid.CliPublishImageReferenceIsValid, pkg.manifest.Deployment.Image))

	// ⚠️ 钉 digest 必须在**签名之前**：反过来的话签的是旧 Manifest，
	// 上传的却是钉过的——消费方一律验签失败，而发布者这边一切正常（P29）
	if err := pinImageDigest(ctx, opts, pkg, f); err != nil {
		return err
	}

	// 签名也在联网之前：版本号一旦建出来就不可回收（市场侧 18.14），
	// 不能因为密钥路径写错就烧掉一个有语义的版本号
	if err := signPackage(ctx, opts, pkg, f); err != nil {
		return err
	}

	client := market.New(marketURL, token)
	if err := uploadRelease(ctx, opts, client, pkg, f); err != nil {
		return err
	}

	opts.Printf("%s\n", i18n.T(msgid.CliPublishUploadSucceeded))
	opts.Printf("%s\n", i18n.T(msgid.CliPublishPublished))
	opts.Printf("%s\n", i18n.T(msgid.CliPublishComponent, pkg.manifest.Metadata.ID, pkg.manifest.Metadata.Version))
	if f.visibility != "" {
		opts.Printf("%s\n", i18n.T(msgid.CliPublishVisibility, f.visibility))
	}
	opts.Printf("%s\n", i18n.T(msgid.CliPublishSourceType, pkg.sourceType))
	if pkg.gitURL != "" {
		opts.Printf("%s\n", i18n.T(msgid.CliPublishGitRepository, pkg.gitURL))
	}
	opts.Printf("%s\n", i18n.T(msgid.CliLoginMarketAddress, marketURL))
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
	// warnings 是不阻断发布的提醒（如属性声明里拼错的键）。
	warnings []*clierr.Error
}

// loadPublishPackage 读组件目录并做全部本地校验。
func loadPublishPackage(f publishFlags) (*publishPackage, error) {
	root, err := filepath.Abs(f.path)
	if err != nil {
		root = f.path
	}
	manifestPath := filepath.Join(root, manifestFileName)

	if _, err := os.Stat(manifestPath); err != nil {
		return nil, clierr.New(clierr.CodeManifestInvalid, i18n.T(msgid.CliPublishErrorThereIsNoComponent)).
			WithDetail(i18n.T(msgid.LabelPath), manifestPath).
			WithHint(
				i18n.T(msgid.CliPublishPointPathAtTheComponent),
				i18n.T(msgid.CliPublishTheArchiveDirectoryWorksToo),
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
	if raw, err := os.ReadFile(manifestPath); err == nil {
		pkg.warnings = manifest.PropertyKeyWarnings(raw, manifestPath)
	}
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
				return clierr.New(clierr.CodeManifestInvalid, i18n.T(msgid.CliPublishErrorAFileDeclaredUnder)).
					WithDetail(i18n.T(msgid.LabelComponent), p.manifest.Metadata.ID).
					WithDetail(i18n.T(msgid.CliPublishArtifactType), artifact.Type).
					WithDetail(i18n.T(msgid.LabelFile), file).
					WithDetail(i18n.T(msgid.CliPublishLookedAt), path).
					WithHint(
						i18n.T(msgid.CliPublishMakeSureTheFileHas),
						i18n.T(msgid.CliPublishOrCorrectThePathUnder),
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
		return nil, clierr.New(clierr.CodeManifestInvalid, i18n.T(msgid.CliPublishErrorFailedToReadComponent)).
			WithDetail(i18n.T(msgid.LabelPath), path).WithCause(err)
	}

	var document any
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, clierr.New(clierr.CodeManifestInvalid, i18n.T(msgid.CliPublishErrorComponentYamlIsNot)).
			WithDetail(i18n.T(msgid.LabelPath), path).WithCause(err)
	}

	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, clierr.New(clierr.CodeManifestInvalid, i18n.T(msgid.CliPublishErrorComponentYamlCouldNot)).
			WithDetail(i18n.T(msgid.LabelPath), path).WithCause(err)
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
	switch {
	case market.IsVersionExists(err):
		// 上一次没发完留下的 draft？能续就续（见 resumable）
		if err := resumable(ctx, client, pkg); err != nil {
			return err
		}
		opts.Printf("%s\n", i18n.T(msgid.CliPublishResumingThisVersionWasCreated))
	case err != nil:
		return err
	}

	if len(pkg.fileOrder) > 0 {
		if err := uploadArtifacts(ctx, client, pkg); err != nil {
			return err
		}
		opts.Printf("%s\n", i18n.T(msgid.CliPublishArtifactsUploadedFiles, i18n.Count(msgid.CountFiles, len(pkg.fileOrder))))
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

// resumable 判断"这个版本已经存在"能不能接着往下发；不能时给出该说的那句话。
//
// # 为什么值得救
//
// 发布是三步：建版本（draft）→ 逐个上传产物 → 转 stable（004 §3.11）。第一步一旦
// 成功，那个版本号就**永久占住了**——版本不可回收，软删除也占位（007 §6.4）。
// 于是网络在第二步抖一下，使用者就只剩"跳一个版本号"这一条路，而中断的原因
// 跟他毫无关系。服务端本来就支持接着发（draft 可以继续上传产物再转 stable），
// 缺的只是 CLI 这一侧。
//
// # 两个前提，缺一不可
//
//	状态还是 draft        stable / deprecated / blocked 都是真的发布过了，
//	                      那时"续传"等于偷偷改一个已经在用的版本
//	Manifest 逐字节相同   draft 里登记的是**上一次**那份。组件改过之后用同一个
//	                      版本号再发，闷头续传会把旧 Manifest 配上新产物发出去——
//	                      比烧掉版本号更糟，因为它悄无声息地成功了
func resumable(ctx context.Context, client *market.Client, pkg *publishPackage) error {
	id, version := pkg.manifest.Metadata.ID, pkg.manifest.Metadata.Version

	info, err := client.FindVersion(ctx, id, version)
	if err != nil {
		return err
	}
	if info == nil || info.Status != versionStatusDraft {
		status := "stable"
		if info != nil {
			status = info.Status
		}
		return clierr.New(clierr.CodeConfigConflict,
			i18n.T(msgid.CliPublishErrorHasAlreadyBeenPublished, id, version)).
			WithDetail(i18n.T(msgid.CliPublishStatusOnTheMarket), status).
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.CliPublishOncePublishedAVersionNumber)).
			WithHint(
				i18n.T(msgid.CliPublishUseAnotherVersionNumberChange),
				i18n.T(msgid.CliPublishIfYouOnlyWantTo),
			)
	}

	remote, err := client.FetchManifest(ctx, id, version)
	if err != nil {
		return err
	}
	if !sameJSON(remote, pkg.document) {
		return clierr.New(clierr.CodeConfigConflict,
			i18n.T(msgid.CliPublishErrorWasCreatedLastTime, id, version)).
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.CliPublishResumingCanOnlyUploadThe)).
			WithHint(
				i18n.T(msgid.CliPublishUseAnotherVersionNumberChange),
				i18n.T(msgid.CliPublishIfYouReallyWantTo),
			)
	}
	return nil
}

// sameJSON 比较两份 JSON 的**语义**是否相同（键序与空白不算数）。
func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
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
			return clierr.New(clierr.CodeManifestInvalid, i18n.T(msgid.CliPublishErrorTheMarketHasNo)).
				WithDetail(i18n.T(msgid.LabelComponent), id+"@"+version).
				WithDetail(i18n.T(msgid.LabelFile), file).
				WithHint(i18n.T(msgid.CliPublishMakeSureTheMarketService))
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
			return "", clierr.New(clierr.CodeTokenExpired, i18n.T(msgid.SourceTokenExpired)).
				WithDetail(i18n.T(msgid.SourceLabelExpiresAt), creds.ExpiresAt.Format(timeLayoutRFC3339)).
				WithHint(i18n.T(msgid.SourceHintLoginAgain))
		}
		return creds.Token, nil
	}

	// 登录态不可用时回落到配置里的 authToken
	if token := configAuthToken(layout, marketURL); token != "" {
		return token, nil
	}

	return "", clierr.New(clierr.CodeAuthRequired, i18n.T(msgid.CliPublishErrorPublishingFailedNotLogged)).
		WithDetail(i18n.T(msgid.CliPublishMarket), marketURL).
		WithHint(
			i18n.T(msgid.MarketHintLogin),
			i18n.T(msgid.CliPublishOrConfigureSourcesAuthtokenIn),
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
		return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliPublishErrorInvalidVisibilityValue)).
			WithDetail(i18n.T(msgid.CliPublishCurrentValue), visibility).
			WithDetailf(i18n.T(msgid.CliPublishAllowedValues), "%s | %s", visibilityPublic, visibilityPrivate).
			WithExit(clierr.ExitUsage)
	}
}

// publishAuthHint 把"找不到市场地址"的提示换成发布语境下的说法。
func publishAuthHint(err error) error {
	cliErr := clierr.As(err)
	if cliErr == nil {
		return err
	}
	return cliErr.WithHint(i18n.T(msgid.CliPublishOrSpecifyWhichMarketTo))
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
