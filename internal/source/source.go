// Package source 实现三种安装源（local / git / market）与 Manifest、artifacts 缓存。
//
// 安装源按 sources 的顺序查找组件，取 Manifest 与产物。
//
// 核心行为：
//
//	优先级   按 brickkit.yaml 中 sources 的顺序依次尝试，靠前的优先
//	开关     enabled: false 的安装源完全跳过（配置有误也不会导致失败）
//	缓存     Manifest → .brickkit/manifests/<scope>/<name>/<版本>/component.yaml（永久，
//	         旁边是来源与签名信封 signature.json、组件带着时还有 BRICKKIT.md）
//	         产物     → .brickkit/artifacts/<版本化服务名>/<type>/<文件路径>
//	刷新     Options.Refresh 忽略缓存强制重新拉取（brickkit add 重复添加同一版本时打开）
//	签名     只有市场源受签名策略约束（verify.go）；本地源与 git 源指向的是
//	         使用者自己的目录与仓库，那里没有"发布者"这个角色
//	来源     Origin 绕开 Manifest 缓存直接问安装源，供 brickkit add --repo 判断
//	         组件是开源（git）还是闭源（registry）
package source

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/envref"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/security"
)

// Options 控制安装源客户端的行为。
type Options struct {
	// Refresh 忽略本地缓存，强制重新拉取。brickkit add 检测到该版本已在配置里时打开它。
	Refresh bool
	// HTTPClient 用于市场安装源。为空时使用带超时的默认客户端。
	HTTPClient *http.Client
	// Now 用于判断 Token 是否过期。为空时使用 time.Now。
	Now func() time.Time
	// Signature 是签名校验策略。零值表示既不强制、也没有可信公钥，
	// 此时完全不校验——这正是还没用上签名的项目的默认处境。
	Signature SignaturePolicy
	// RepoCacheDir 是 git 源的 bare 仓库缓存目录。空表示用户级默认位置
	// <用户缓存目录>/brickkit/repos（附录 A12）；测试用它隔离。
	RepoCacheDir string
}

// SetRefresh 打开/关闭"忽略缓存强制重新拉取"（等价于构造时的 Options.Refresh）。
//
// 存在的理由：brickkit add 不写版本时，要先用这个客户端解析出最新版本，
// 才知道该版本是否已在配置里、进而才知道要不要刷新缓存。为查一次版本再建一个
// 客户端并不划算——git 源会因此重新 clone 一遍整个仓库。
//
// 只能在开始取 Manifest / 产物之前调用（add 的调用点满足这个前提）。
func (c *Client) SetRefresh(refresh bool) { c.opts.Refresh = refresh }

// Fetched 是一次 Manifest 获取的结果。
type Fetched struct {
	// Manifest 是解析并校验通过的组件 Manifest。
	Manifest *manifest.Manifest
	// SourceID 是提供该 Manifest 的安装源 id；命中本地缓存时为空。
	SourceID string
	// FromCache 表示该 Manifest 来自 .brickkit/manifests/ 缓存。
	FromCache bool
	// Signature 是安装源提供的签名，未签名时为 nil。
	Signature *security.Signature
	// Verified 表示签名**真的验过并通过**。
	//
	// 它与"没有报错"不是一回事：没配公钥、发布者不认识时都会放行，但那是
	// "没得验"，不是"验过了"。只有这个字段为 true 才能对外说「签名：✅ 已校验」。
	Verified bool
	// Warnings 是校验过程中的提醒（未配公钥、发布者未声明），不阻断。
	Warnings []*clierr.Error
}

// ArtifactResult 汇总一次产物下载的结果。路径均相对 .brickkit/artifacts/。
type ArtifactResult struct {
	// Downloaded 是本次写入的产物文件。
	Downloaded []string
	// Cached 是已存在于缓存、本次跳过的产物文件。
	Cached []string
	// Warnings 是下载失败的产物。产物文件是开发时辅助，失败不阻断安装。
	Warnings []*clierr.Error
}

// Client 按安装源优先级获取 Manifest 与 artifacts，并维护本地缓存。
//
// 使用完毕必须调用 Close 释放 git 安装源的临时 clone。
type Client struct {
	layout   project.Layout
	opts     Options
	fetchers []fetcher
	// overrides 是 brickkit.yaml 里自己写了 source 的组件：只从那个来源取（提案 §9.3、§9.6）。
	overrides map[string]fetcher
	repos     *repoCache

	sigMu sync.Mutex
	// sigStatuses 按 <id>@<version> 记下每次取 Manifest 的签名校验结果。
	//
	// 记在客户端里而不是层层往上传：一次 add 会连带拉取整棵依赖树的 Manifest，
	// 命令层要报的是"这一趟里哪些验过、哪些没验过"，而不是每个调用点各接一次。
	sigStatuses map[string]SignatureStatus
	sigOrder    []string
}

// SignatureStatus 是某个组件版本的签名校验结果。
type SignatureStatus struct {
	ComponentID string
	Version     string
	// Verified 为 true 表示签名**真的验过并通过**。
	Verified  bool
	Signature *security.Signature
	// Warnings 是放行但需要提醒的情况（未配公钥、发布者未声明）。
	Warnings []*clierr.Error
}

// Ref 返回 people/basic@1.2.0 形式的引用。
func (s SignatureStatus) Ref() string { return s.ComponentID + "@" + s.Version }

// SignatureStatuses 返回本次运行中所有取过的 Manifest 的校验结果，按首次出现顺序。
func (c *Client) SignatureStatuses() []SignatureStatus {
	c.sigMu.Lock()
	defer c.sigMu.Unlock()

	out := make([]SignatureStatus, 0, len(c.sigOrder))
	for _, key := range c.sigOrder {
		out = append(out, c.sigStatuses[key])
	}
	return out
}

// recordSignature 记下一次校验结果（同一组件版本重复取时只记第一次）。
func (c *Client) recordSignature(id, version string, sig *security.Signature, result verifyResult) {
	c.sigMu.Lock()
	defer c.sigMu.Unlock()

	key := id + "@" + version
	if _, seen := c.sigStatuses[key]; seen {
		return
	}
	if c.sigStatuses == nil {
		c.sigStatuses = map[string]SignatureStatus{}
	}
	c.sigStatuses[key] = SignatureStatus{
		ComponentID: id, Version: version,
		Verified: result.verified, Signature: sig, Warnings: result.warnings,
	}
	c.sigOrder = append(c.sigOrder, key)
}

// New 由项目布局与配置构造安装源客户端。enabled: false 的安装源不会被构造。
func New(layout project.Layout, decl *projfile.File, opts Options) (*Client, error) {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: marketTimeout}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	if opts.RepoCacheDir == "" {
		if dir, err := os.UserCacheDir(); err == nil {
			opts.RepoCacheDir = filepath.Join(dir, "brickkit", "repos")
		}
	}
	c := &Client{layout: layout, opts: opts, repos: newRepoCache(opts.RepoCacheDir), overrides: map[string]fetcher{}}
	if decl == nil {
		return c, nil
	}
	for _, s := range decl.EnabledSources() {
		f, err := c.newFetcher(s)
		if err != nil {
			return nil, err
		}
		c.fetchers = append(c.fetchers, f)
	}
	for _, comp := range decl.Components {
		if comp.Source == nil || c.overrides[comp.ID] != nil {
			continue
		}
		c.overrides[comp.ID] = c.componentFetcher(comp.ID, comp.Source)
	}
	return c, nil
}

// componentFetcher 是组件级来源：git 固定仓库（及子目录），或本地的一个组件目录。
func (c *Client) componentFetcher(id string, src *projfile.ComponentSource) fetcher {
	name := i18n.T(msgid.SourceComponentSourceName, id)
	if src.Type == projfile.SourceTypeLocal {
		return &localSource{sourceID: name, configured: src.Path, root: c.resolvePath(src.Path), exact: true}
	}
	return &gitSource{sourceID: name, repoURL: src.Repo, subpath: path.Clean("/" + src.Path)[1:], cache: c.repos}
}

// fetchersFor 返回取这个组件时依次尝试的安装源：组件自己写了来源就只用它。
func (c *Client) fetchersFor(id string) []fetcher {
	if f, ok := c.overrides[id]; ok {
		return []fetcher{f}
	}
	return c.fetchers
}

func (c *Client) newFetcher(s projfile.Source) (fetcher, error) {
	switch s.Type {
	case projfile.SourceTypeLocal:
		return &localSource{
			sourceID:   s.Name,
			configured: s.Path,
			root:       c.resolvePath(s.Path),
		}, nil
	case projfile.SourceTypeGit:
		return &gitSource{sourceID: s.Name, baseURL: s.BaseURL, cache: c.repos}, nil
	case projfile.SourceTypeMarket:
		return &marketSource{
			sourceID:        s.Name,
			baseURL:         s.URL,
			authToken:       AuthToken(s, c.layout.Root),
			credentialsPath: c.layout.CredentialsPath(),
			client:          c.opts.HTTPClient,
			now:             c.opts.Now,
		}, nil
	default:
		return nil, clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.SourceTypeInvalid, s.Type)).
			WithDetail(i18n.T(msgid.LabelSource), s.Name).
			WithHint(i18n.T(msgid.SourceHintTypeOneOf))
	}
}

// resolvePath 把 brickkit.yaml 中的相对路径按项目根解析。
func (c *Client) resolvePath(path string) string {
	if path == "" {
		return c.layout.Root
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(c.layout.Root, filepath.FromSlash(path))
}

// Close 释放临时资源（git 安装源的临时 clone）。
func (c *Client) Close() error {
	var first error
	all := append([]fetcher{}, c.fetchers...)
	for _, f := range c.overrides {
		all = append(all, f)
	}
	for _, f := range all {
		if err := f.close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Manifest 获取组件 Manifest。
//
// 优先读取 .brickkit/manifests/ 缓存；缓存缺失、损坏或 Options.Refresh 为 true 时
// 按安装源优先级重新拉取，并写回缓存。
func (c *Client) Manifest(ctx context.Context, id, version string) (*Fetched, error) {
	if err := checkRef(id, version); err != nil {
		return nil, err
	}

	cachePath := c.ManifestCachePath(id, version)
	if !c.opts.Refresh && !c.servedByLocalSource(ctx, id, version) {
		if fetched, ok := c.fromCache(cachePath, id, version); ok {
			return fetched, nil
		}
	}

	raw, m, f, sig, err := c.fetchManifest(ctx, id, version)
	if err != nil {
		return nil, err
	}
	sourceID, kind := f.id(), f.kind()

	// 先验后存：验不过的东西绝不能进缓存，否则下一次它就成了"本地已有的那份"
	result, err := c.verifyFrom(kind, raw, sig, id, version)
	if err != nil {
		return nil, err
	}

	if err := writeFileAll(cachePath, raw); err != nil {
		return nil, clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.SourceCacheWriteFailed)).
			WithDetail(i18n.T(msgid.LabelPath), cachePath).
			WithDetail(i18n.T(msgid.LabelReason), err.Error()).
			WithHint(i18n.T(msgid.SourceHintCheckCachePermissions)).
			WithCause(err)
	}
	c.writeCachedSignature(id, version, kind, sig)
	c.cacheDoc(ctx, f, id, version)
	c.recordSignature(id, version, sig, result)

	return &Fetched{
		Manifest: m, SourceID: sourceID,
		Signature: sig, Verified: result.verified, Warnings: result.warnings,
	}, nil
}

// verifyFrom 按安装源类型决定要不要应用签名策略。
//
// **只有市场源受签名约束。** 签名约束的是"从**市场**获取 Manifest 和签名"：
// 本地源指向的是使用者自己硬盘上、正被他编辑的目录，git 源指向的是他自己在
// brickkit.yaml 里写下的仓库——那里根本没有"发布者"这个角色，也就无所谓签名。
//
// 若一并强制，打开 requireSignature 会让所有用本地源开发的项目当场瘫痪，
// 结果只会是大家把它关掉；那才是真正的安全损失。
func (c *Client) verifyFrom(
	kind string, raw []byte, sig *security.Signature, id, version string,
) (verifyResult, error) {
	if kind != projfile.SourceTypeMarket {
		return verifyResult{}, nil
	}
	return c.opts.Signature.verify(raw, sig, id, version)
}

// fromCache 尝试用缓存应答这次请求，连同签名一起校验。
//
// 缓存这条路径不能是校验的后门：只要跳过它，"先在不校验的情况下 add 一次、
// 再打开 requireSignature"就能让一份从未验过的 Manifest 一直被用下去。
//
// 校验不过时**退回去重新拉取**，而不是直接报错。缓存里的东西可能只是旧了
// （公钥轮换过、发布者重新签过），硬报错会让人除了手动删缓存无路可走；
// 而重新拉来的那份同样要过校验，安全性一点没少。
func (c *Client) fromCache(cachePath, id, version string) (*Fetched, bool) {
	raw, m, ok := readCachedManifest(cachePath, id, version)
	if !ok {
		return nil, false
	}

	// 缓存旁边的信封记着"这份是哪种源给的、签名是什么"。信封不在（老缓存）
	// 而策略又要校验时，只能当作缓存未命中去重新拉——凭空假设它来自哪种源，
	// 无论假设成哪一种都会错：假设市场源会误伤 git 缓存，假设非市场源就成了后门。
	envelope, ok := c.readCachedSignature(id, version)
	if !ok {
		if c.opts.Signature.enabled() {
			return nil, false
		}
		envelope = cachedSignature{}
	}
	result, err := c.verifyFrom(envelope.SourceKind, raw, envelope.Signature, id, version)
	if err != nil {
		return nil, false
	}
	sig := envelope.Signature
	c.recordSignature(id, version, sig, result)
	return &Fetched{
		Manifest: m, FromCache: true,
		Signature: sig, Verified: result.verified, Warnings: result.warnings,
	}, true
}

// SignatureCachePath 返回签名缓存路径（与 Manifest 缓存同一个版本目录）。
func (c *Client) SignatureCachePath(id, version string) string {
	return c.layout.CachedSignaturePath(id, version)
}

// Doc 返回缓存里这个组件版本的 BRICKKIT.md；组件没带文档、或还没取过时 ok 为 false。
func (c *Client) Doc(id, version string) (path string, ok bool) {
	path = c.layout.CachedDocPath(id, version)
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
}

// docFetcher 是能提供组件文档 BRICKKIT.md 的安装源（本地源读目录，git 源读 tag，
// 市场走文档端点）。没有文档返回 errNotFound，不算错。
type docFetcher interface {
	docBytes(ctx context.Context, componentID, version string) ([]byte, error)
}

// cacheDoc 把安装源提供的 BRICKKIT.md 写进缓存。拿不到或写不进都不阻断：文档是给人与 AI
// 读的辅助，没有它组件照样能装、能跑。
func (c *Client) cacheDoc(ctx context.Context, f fetcher, id, version string) {
	df, ok := f.(docFetcher)
	if !ok {
		return
	}
	if data, err := df.docBytes(ctx, id, version); err == nil {
		_ = writeFileAll(c.layout.CachedDocPath(id, version), data)
	}
}

// cachedSignature 是签名缓存文件的内容。
//
// 里面必须同时记下**来源类型**：签名策略只约束市场源，而缓存文件本身看不出
// 这份 Manifest 当初是谁给的。只存签名的话，"没有签名"就有了两种解释——
// 市场给的未签名组件（该被 requireSignature 拦住），还是 git 源给的
// （压根不该管）——两者无法区分。
type cachedSignature struct {
	SourceKind string              `json:"sourceKind"`
	Signature  *security.Signature `json:"signature,omitempty"`
}

// readCachedSignature 读取缓存信封；文件不在或读不动时返回 false。
func (c *Client) readCachedSignature(id, version string) (cachedSignature, bool) {
	data, err := os.ReadFile(c.SignatureCachePath(id, version))
	if err != nil {
		return cachedSignature{}, false
	}
	var envelope cachedSignature
	if err := json.Unmarshal(data, &envelope); err != nil {
		return cachedSignature{}, false
	}
	if envelope.Signature != nil && envelope.Signature.Empty() {
		envelope.Signature = nil
	}
	return envelope, true
}

// writeCachedSignature 把来源与签名写到 Manifest 缓存旁边。
//
// 写失败不阻断安装：这份缓存只是为了下次少一次请求，它没了最多是重新拉一遍。
func (c *Client) writeCachedSignature(id, version, kind string, sig *security.Signature) {
	envelope := cachedSignature{SourceKind: kind, Signature: sig}
	if sig != nil && sig.Empty() {
		envelope.Signature = nil
	}
	if data, err := json.Marshal(envelope); err == nil {
		_ = writeFileAll(c.SignatureCachePath(id, version), data)
	}
}

// servedByLocalSource 判断这个组件会不会由某个**本地**安装源提供。
//
// 本地源的 component.yaml 就在使用者硬盘上、正被他编辑；缓存一份快照
// 只会让改动静默地不生效——改了端口、迁移命令或配额之后 `brickkit up`
// 依旧按旧的生成，而且一声不吭。缓存是为了省网络往返，
// 本地源没有网络往返，也就没有缓存的理由。
//
// 只扫到第一个非本地源为止：优先级更高的远程源可能才是真正的提供方，
// 而"远程源有没有这个组件"问不起——那正是缓存存在的原因。
//
// # 文件在、但坏了，也算"由本地源提供"
//
// 判据不能只有 manifestMatches：它在 YAML 解析失败时返回 false，
// 于是"文件坏了"与"这个源没有它"变成同一个答案，调用方退回缓存——
// 使用者改坏了 component.yaml，`up` 却拿上一份好的缓存**照常成功**，
// 一个字都不说。那正是本地源不吃缓存要防的事，只是失败方式更隐蔽：
// 不是"改了没生效"，而是"改错了也没人告诉你"。
//
// 所以只要本地源真的拿得出这个文件，就返回 true，让后面的
// fetchManifest 去解析并把那条语法错误抛出来。
//
// # 文件在、id 却被改成了另一个身份，同样不算"这个源没有"
//
// component.yaml 语法完全合法、只是 metadata.id 被改成跟请求的 id 对不上
// （手滑重命名，而目录和 brickkit.yaml 的引用都没动），是"改错了"的又一种
// 形态，后果和坏 YAML 一样：调用方退回改名前那份缓存，`up` 一声不吭照常成功。
// 目录还在、里面的 id 却已经不认这次请求——就该让 fetchManifest 走一遍、
// 报"未找到"，而不是拿旧缓存顶上。
//
// **但版本对不上要放行缓存**：本地源一个目录只放得下一个版本，把组件升上去
// （改 metadata.version）之后，还依赖旧版本的调用方就只能从缓存里取那一份
// ——这是多版本共存的正常用法，不是"改错了"。
// 所以只在 id 也匹配、单纯版本不同时才继续往下走、允许缓存生效。
func (c *Client) servedByLocalSource(ctx context.Context, id, version string) bool {
	for _, f := range c.fetchersFor(id) {
		if f.kind() != projfile.SourceTypeLocal {
			return false
		}
		raw, err := f.manifestBytes(ctx, id, version)
		if err != nil {
			continue
		}
		if !manifestParses(raw) || !manifestIDMatches(raw, id) || manifestMatches(raw, id, version) {
			return true
		}
	}
	return false
}

// ManifestCachePath 返回 Manifest 缓存路径，如
// .brickkit/manifests/people/basic/1.0.0/component.yaml（提案 §9.4）。
func (c *Client) ManifestCachePath(id, version string) string {
	return c.layout.CachedManifestPath(id, version)
}

// ArtifactDir 返回某个组件版本的产物缓存目录，如
// .brickkit/artifacts/department-tree-1-0-0/。
func (c *Client) ArtifactDir(id, version string) string {
	return filepath.Join(c.layout.ArtifactsDir(), manifest.ServiceName(id, version))
}

// DownloadArtifacts 下载 Manifest 中声明的全部产物到
// .brickkit/artifacts/<版本化服务名>/<type>/<文件路径>。
//
// 已缓存的文件默认跳过；Options.Refresh 为 true 时重新下载。
// 单个文件下载失败只记入 Warnings，不阻断（产物是开发时辅助）。
func (c *Client) DownloadArtifacts(ctx context.Context, m *manifest.Manifest) (*ArtifactResult, error) {
	if m == nil {
		return nil, clierr.New(clierr.CodeInternal, i18n.T(msgid.SourceNoManifest)).WithHint(i18n.T(msgid.HintInternalBug))
	}
	id, version := m.Metadata.ID, m.Metadata.Version
	if err := checkRef(id, version); err != nil {
		return nil, err
	}

	base := c.ArtifactDir(id, version)
	// 与 Manifest 同一条规则：本地源的产物（.proto、openapi.json）也在使用者
	// 硬盘上跟着代码一起改，缓存住只会让调用方按旧契约生成客户端
	useCache := !c.opts.Refresh && !c.servedByLocalSource(ctx, id, version)

	res := &ArtifactResult{}
	for _, art := range m.Artifacts {
		for _, file := range art.Files {
			rel := filepath.Join(manifest.ServiceName(id, version), art.Type, filepath.FromSlash(file))
			dest := filepath.Join(c.layout.ArtifactsDir(), rel)
			if !withinDir(base, dest) {
				// Manifest 校验已禁止越界路径，这里是纵深防御。
				res.Warnings = append(res.Warnings, artifactWarning(id, version, art.Type, file,
					i18n.T(msgid.SourceArtifactEscapes)))
				continue
			}
			if useCache {
				if _, err := os.Stat(dest); err == nil {
					res.Cached = append(res.Cached, rel)
					continue
				}
			}

			data, err := c.fetchArtifact(ctx, id, version, art, file)
			if err != nil {
				res.Warnings = append(res.Warnings, artifactWarning(id, version, art.Type, file,
					reasonOf(err)))
				continue
			}
			if err := writeFileAll(dest, data); err != nil {
				res.Warnings = append(res.Warnings, artifactWarning(id, version, art.Type, file,
					err.Error()))
				continue
			}
			res.Downloaded = append(res.Downloaded, rel)
		}
	}
	return res, nil
}

// Origin 按安装源优先级查询组件的来源信息（开源 git / 闭源 registry）。
//
// 它不走 Manifest 缓存：缓存里存的是 component.yaml，不含 sourceType / gitUrl。
// 只有 brickkit add --repo / --repo-all 需要这个信息。
func (c *Client) Origin(ctx context.Context, id, version string) (*Origin, error) {
	if err := checkRef(id, version); err != nil {
		return nil, err
	}
	if len(c.fetchersFor(id)) == 0 {
		return nil, noSourcesError()
	}

	var failures []failure
	for _, f := range c.fetchersFor(id) {
		origin, err := f.origin(ctx, id, version)
		if err != nil {
			failures = append(failures, failure{sourceID: f.id(), err: err})
			continue
		}
		return origin, nil
	}
	return nil, c.aggregateError(id, version, failures, nil)
}

// fetchManifest 按优先级遍历安装源，返回首个命中的 Manifest
// （原始字节 + 解析结果 + 源 id + 该源提供的签名）。
func (c *Client) fetchManifest(
	ctx context.Context, id, version string,
) ([]byte, *manifest.Manifest, fetcher, *security.Signature, error) {
	if len(c.fetchersFor(id)) == 0 {
		return nil, nil, nil, nil, noSourcesError()
	}

	var failures []failure
	var mismatches []versionMismatch
	for _, f := range c.fetchersFor(id) {
		raw, err := f.manifestBytes(ctx, id, version)
		if err != nil {
			failures = append(failures, failure{sourceID: f.id(), err: err})
			continue
		}
		m, err := manifest.Parse(raw, describe(f, id, version))
		if err != nil {
			failures = append(failures, failure{sourceID: f.id(), err: err})
			continue
		}
		if m.Metadata.ID != id || m.Metadata.Version != version {
			// 该源提供的是另一个组件/版本：等同于"这里没有"，继续下一个源。
			//
			// 但**同一个组件、只是版本不同**要记下来：它与"这里根本没有它"
			// 是两回事，该让人去看的地方也完全不同。详见 versionMismatch。
			if m.Metadata.ID == id {
				mismatches = append(mismatches, versionMismatch{
					sourceID: f.id(), kind: f.kind(), found: m.Metadata.Version,
				})
			}
			failures = append(failures, failure{sourceID: f.id(), err: errNotFound})
			continue
		}
		return raw, m, f, signatureFrom(f, id, version), nil
	}
	return nil, nil, nil, nil, c.aggregateError(id, version, failures, mismatches)
}

// versionMismatch 是"这个源里有这个组件，但版本不是要的那个"。
type versionMismatch struct {
	sourceID string
	kind     string
	// found 是该源实际提供的版本。
	found string
}

// signedFetcher 是能提供签名的安装源。只有市场源实现它——本地源与 git 源
// 指向的是使用者自己的目录与仓库，那里没有"发布者"这个角色（签名约束的是
// "从**市场**获取 Manifest 和签名"）。
type signedFetcher interface {
	signatureFor(componentID, version string) *security.Signature
}

// signatureFrom 取出该安装源刚刚提供的签名；不支持签名的源返回 nil。
func signatureFrom(f fetcher, id, version string) *security.Signature {
	if sf, ok := f.(signedFetcher); ok {
		return sf.signatureFor(id, version)
	}
	return nil
}

// fetchArtifact 按优先级遍历安装源，返回首个能提供该产物文件的内容。
func (c *Client) fetchArtifact(ctx context.Context, id, version string, art manifest.Artifact, file string) ([]byte, error) {
	if len(c.fetchersFor(id)) == 0 {
		return nil, noSourcesError()
	}
	var failures []failure
	for _, f := range c.fetchersFor(id) {
		data, err := f.artifactFile(ctx, id, version, art, file)
		if err != nil {
			failures = append(failures, failure{sourceID: f.id(), err: err})
			continue
		}
		return data, nil
	}
	if err := firstRealError(failures, id+"@"+version); err != nil {
		return nil, err
	}
	// 所有源都只是"没有这个文件"：交给调用方渲染成产物警告，而不是组件未找到。
	return nil, errNotFound
}

// failure 记录某个安装源的失败原因。
type failure struct {
	sourceID string
	err      error
}

// aggregateError 汇总所有安装源的失败。
//
// 只要有一个源是"真失败"（路径不存在、克隆失败、市场不可达……），就把该错误报出来——
// 那通常才是使用者要修的问题；全部都只是"没有"时，报组件未找到。
func (c *Client) aggregateError(
	id, version string, failures []failure, mismatches []versionMismatch,
) error {
	// 真失败（路径不存在、市场不可达……）优先：那才是要先解决的问题
	if err := firstRealError(failures, id+"@"+version); err != nil {
		return err
	}
	if err := versionMismatchError(id, version, mismatches); err != nil {
		return err
	}
	return c.notFoundError(id+"@"+version, failures,
		i18n.T(msgid.SourceHintCheckSourcesConfig),
		i18n.T(msgid.SourceHintCheckPublished),
		i18n.T(msgid.SourceHintCheckVersion),
	)
}

// versionMismatchError 说清楚"源里有这个组件，只是版本不同"。
//
// # 为什么值得单独说一句
//
// 本地安装源的目录结构是 `<root>/<scope>/<name>/component.yaml`——**一个组件 ID
// 只放得下一个版本**。本地开发时把某个组件升上去（改那份
// component.yaml），而别的组件还依赖着旧版本，旧版本就只剩 Manifest 缓存里
// 那一份；缓存一冷（同事 clone 而 .brickkit/manifests 被 gitignore、
// 或者 rm -rf .brickkit），解析立刻断在"强依赖缺失"上。
//
// 而这时 CLI **知道**真相：它读到了那个文件、解析成功了、看见里面写着 2.0.0。
// 从前这个信息被丢掉，只剩一句"该组件在所有安装源中均未找到"，配三条
// （查安装源配置 / 查有没有发布到市场 / 查版本号）没有一条说到点子上——
// 使用者会去翻 sources 配置，而问题在他自己刚改过的那份 component.yaml 里。
func versionMismatchError(id, version string, mismatches []versionMismatch) error {
	if len(mismatches) == 0 {
		return nil
	}

	e := clierr.New(clierr.CodeComponentNotFound, i18n.T(msgid.SourceVersionMismatch)).
		WithDetail(i18n.T(msgid.SourceLabelWantedVersion), id+"@"+version)
	for _, m := range mismatches {
		e = e.WithDetail(i18n.T(msgid.SourceLabelSourceKind, m.sourceID, m.kind), i18n.T(msgid.SourceFoundVersion, m.found))
	}
	return e.WithHint(
		i18n.T(msgid.SourceHintSingleVersionPerDir),
		i18n.T(msgid.SourceHintAdaptDependents, id, version),
		i18n.T(msgid.SourceHintRevertSourceVersion, version),
	)
}

// notFoundError 汇总"所有安装源都没给出结果"的失败。
//
// 先看有没有**真失败**（路径不存在、克隆失败、市场不可达）：有就报它，
// 那才是使用者要解决的问题。全部只是"该源没有"时，才报"组件未找到"。
func (c *Client) notFoundError(ref string, failures []failure, hints ...string) error {
	if err := firstRealError(failures, ref); err != nil {
		return err
	}

	id, _, _ := manifest.SplitRef(ref)
	tried := make([]string, 0, len(c.fetchersFor(id)))
	for _, f := range c.fetchersFor(id) {
		tried = append(tried, i18n.T(msgid.SourceIDWithKind, f.id(), f.kind()))
	}
	return clierr.New(clierr.CodeComponentNotFound, i18n.T(msgid.SourceNotFound)).
		WithDetail(i18n.T(msgid.LabelComponent), ref).
		WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.SourceNotFoundReasonDetail)).
		WithDetail(i18n.T(msgid.SourceLabelTriedSources), strings.Join(tried, i18n.T(msgid.ListSeparator))).
		WithHint(hints...)
}

// firstRealError 返回第一个"真失败"（路径不存在、克隆失败、市场不可达……）的错误，
// 补上组件引用后返回；全部只是"该源没有"时返回 nil。
//
// 返回的是副本：安装源可能缓存自己的失败（如 git clone 只做一次），
// 就地追加明细会让同一个错误对象在多次调用后越积越长。
func firstRealError(failures []failure, ref string) error {
	for _, f := range failures {
		if isNotFound(f.err) {
			continue
		}
		e := clierr.As(f.err)
		dup := *e
		dup.Details = append(append([]clierr.Detail{}, e.Details...), clierr.Detail{Key: i18n.T(msgid.LabelComponent), Value: ref})
		return &dup
	}
	return nil
}

func noSourcesError() error {
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.SourceNoSources)).
		WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.SourceNoSourcesReasonDetail)).
		WithHint(
			i18n.T(msgid.SourceHintConfigureSource),
			i18n.T(msgid.SourceHintLocalDevSource),
		)
}

// checkRef 校验组件引用。ID 与版本要用于拼接缓存文件名与目录名，必须先合法。
func checkRef(id, version string) error {
	if problem := manifest.ComponentIDProblem(id); problem != "" {
		return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.InvalidComponentID, id)).
			WithDetail(i18n.T(msgid.LabelReason), problem).
			WithHint(i18n.T(msgid.HintComponentIDFormat))
	}
	if !manifest.IsExactVersion(version) {
		return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.SourceInvalidVersion, version)).
			WithDetail(i18n.T(msgid.LabelComponent), id).
			WithHint(i18n.T(msgid.SourceHintExactVersionOnly))
	}
	return nil
}

// describe 生成该 Manifest 的来源描述，用于解析错误提示。
func describe(f fetcher, id, version string) string {
	return i18n.T(msgid.SourceDescribe, f.id(), f.kind(), id, version)
}

// readCachedManifest 读取并校验缓存的 Manifest。缓存缺失或损坏时返回 ok=false，
// 由调用方重新从安装源拉取——缓存不该成为故障源。
func readCachedManifest(path, id, version string) ([]byte, *manifest.Manifest, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false
	}
	m, err := manifest.Parse(data, path)
	if err != nil {
		return nil, nil, false
	}
	if m.Metadata.ID != id || m.Metadata.Version != version {
		return nil, nil, false
	}
	return data, m, true
}

// artifactWarning 生成"产物下载失败"警告（⚠️，不阻断，退出码 0）。
func artifactWarning(id, version, artType, file, reason string) *clierr.Error {
	return clierr.Warn(clierr.CodeNetworkUnreachable, i18n.T(msgid.SourceArtifactDownloadFailed)).
		WithDetail(i18n.T(msgid.LabelComponent), id+"@"+version).
		WithDetail(i18n.T(msgid.SourceLabelArtifact), artType+" / "+file).
		WithDetail(i18n.T(msgid.LabelReason), reason).
		WithTip(i18n.T(msgid.SourceTipArtifactsOptional))
}

// reasonOf 把一个错误压成一行原因，用于产物下载警告。
//
// 警告只有一行"原因"可用，因此标题与"原因"明细都要保留：
// 只留标题会丢掉状态码/系统报错，只留明细则看不出是哪一环出的问题。
func reasonOf(err error) string {
	if isNotFound(err) {
		return i18n.T(msgid.SourceNoArtifactAnywhere)
	}
	e := clierr.As(err)
	title := strings.TrimPrefix(e.Message, i18n.T(msgid.ErrorPrefix))
	for _, d := range e.Details {
		if d.Key == i18n.T(msgid.LabelReason) {
			return i18n.T(msgid.DetailLine, title, d.Value)
		}
	}
	return title
}

// withinDir 判断 path 是否位于 dir 之内（防路径穿越）。
func withinDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// writeFileAll 写文件并按需创建父目录。
func writeFileAll(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// AuthToken 返回 market 安装源配置的 Token：展开其中的 ${VAR}
// （进程环境优先，其次 root 下的 .env）。拉组件与 publish 共用这一处。
func AuthToken(s projfile.Source, root string) string {
	return expandToken(s.AuthToken, envref.Lookup(root))
}

// expandToken 展开 token 里的 ${VAR}。
//
// 有任何一个引用取不到就当没配 Token：把字面的 "${VAR}" 当 Bearer 发出去，
// 市场只会回一个看不出原因的 401；没有 Token 时走的是"请先 login"那条说得清的路。
func expandToken(token string, lookup func(string) (string, bool)) string {
	for _, name := range envref.Names(token) {
		if _, ok := lookup(name); !ok {
			return ""
		}
	}
	return envref.Expand(token, lookup)
}
