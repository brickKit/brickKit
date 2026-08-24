// Package source 实现三种安装源（local / git / market）与 Manifest、artifacts 缓存。
//
// 设计依据：003 §6 安装源配置详解、004 §3.3 brickkit add、007 §9.1 市场 API。
//
// 核心行为：
//
//	优先级   按 brickkit.yaml 中 sources 的顺序依次尝试，靠前的优先（003 §6.5）
//	开关     enabled: false 的安装源完全跳过（配置有误也不会导致失败）
//	缓存     Manifest → .brickkit/manifests/<id>-<版本>.yaml
//	         产物     → .brickkit/artifacts/<版本化服务名>/<type>/<文件路径>
//	刷新     Options.Refresh 忽略缓存强制重新拉取（brickkit add 重复添加同一版本时打开）
//	签名     只有市场源受签名策略约束（verify.go）；本地源与 git 源指向的是
//	         使用者自己的目录与仓库，那里没有"发布者"这个角色（008 §8.5.2）
//	来源     Origin 绕开 Manifest 缓存直接问安装源，供 brickkit add --repo 判断
//	         组件是开源（git）还是闭源（registry）
package source

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
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
	// Signature 是签名校验策略（008 §8.5）。零值表示既不强制、也没有可信公钥，
	// 此时完全不校验——这正是还没用上签名的项目的默认处境。
	Signature SignaturePolicy
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
	// Signature 是安装源提供的签名（008 §8.3），未签名时为 nil。
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
	// Warnings 是下载失败的产物。产物文件是开发时辅助，失败不阻断安装（004 §10.1）。
	Warnings []*clierr.Error
}

// Client 按安装源优先级获取 Manifest 与 artifacts，并维护本地缓存。
//
// 使用完毕必须调用 Close 释放 git 安装源的临时 clone。
type Client struct {
	layout   config.Layout
	opts     Options
	fetchers []fetcher

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
	// Verified 为 true 表示签名**真的验过并通过**（008 §8.4）。
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
func New(layout config.Layout, cfg *config.Config, opts Options) (*Client, error) {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: marketTimeout}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	c := &Client{layout: layout, opts: opts}
	if cfg == nil {
		return c, nil
	}
	for _, s := range cfg.EnabledSources() {
		f, err := c.newFetcher(s)
		if err != nil {
			return nil, err
		}
		c.fetchers = append(c.fetchers, f)
	}
	return c, nil
}

func (c *Client) newFetcher(s config.Source) (fetcher, error) {
	switch s.Type {
	case config.SourceTypeLocal:
		return &localSource{
			sourceID:   s.ID,
			configured: s.Path,
			root:       c.resolvePath(s.Path),
		}, nil
	case config.SourceTypeGit:
		return &gitSource{sourceID: s.ID, url: s.URL, ref: s.Ref}, nil
	case config.SourceTypeMarket:
		return &marketSource{
			sourceID:        s.ID,
			baseURL:         s.URL,
			authToken:       s.AuthToken,
			credentialsPath: c.layout.CredentialsPath(),
			client:          c.opts.HTTPClient,
			now:             c.opts.Now,
		}, nil
	default:
		return nil, clierr.Newf(clierr.CodeConfigInvalid, "错误：安装源类型不合法：%s", s.Type).
			WithDetail("安装源", s.ID).
			WithHint("type 必须是 market、git 或 local 之一（003 §6.1）")
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
	for _, f := range c.fetchers {
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

	raw, m, sourceID, kind, sig, err := c.fetchManifest(ctx, id, version)
	if err != nil {
		return nil, err
	}

	// 先验后存：验不过的东西绝不能进缓存，否则下一次它就成了"本地已有的那份"
	result, err := c.verifyFrom(kind, raw, sig, id, version)
	if err != nil {
		return nil, err
	}

	if err := writeFileAll(cachePath, raw); err != nil {
		return nil, clierr.New(clierr.CodeConfigInvalid, "错误：写入 Manifest 缓存失败").
			WithDetail("路径", cachePath).
			WithDetail("原因", err.Error()).
			WithHint("检查 .brickkit/manifests/ 目录权限").
			WithCause(err)
	}
	c.writeCachedSignature(id, version, kind, sig)
	c.recordSignature(id, version, sig, result)

	return &Fetched{
		Manifest: m, SourceID: sourceID,
		Signature: sig, Verified: result.verified, Warnings: result.warnings,
	}, nil
}

// verifyFrom 按安装源类型决定要不要应用签名策略。
//
// **只有市场源受签名约束。** 008 §8.4 说的是"从**市场**获取 Manifest 和签名"：
// 本地源指向的是使用者自己硬盘上、正被他编辑的目录，git 源指向的是他自己在
// brickkit.yaml 里写下的仓库——那里根本没有"发布者"这个角色，也就无所谓签名。
//
// 若一并强制，打开 requireSignature 会让所有用本地源开发的项目当场瘫痪，
// 结果只会是大家把它关掉；那才是真正的安全损失。
func (c *Client) verifyFrom(
	kind string, raw []byte, sig *security.Signature, id, version string,
) (verifyResult, error) {
	if kind != config.SourceTypeMarket {
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

// SignatureCachePath 返回签名缓存路径，与 Manifest 缓存同目录同名，后缀 .sig.json。
func (c *Client) SignatureCachePath(id, version string) string {
	return strings.TrimSuffix(c.ManifestCachePath(id, version), ".yaml") + ".sig.json"
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
// 本地源没有网络往返，也就没有缓存的理由（004 §7.5）。
//
// 只扫到第一个非本地源为止：优先级更高的远程源可能才是真正的提供方，
// 而"远程源有没有这个组件"问不起——那正是缓存存在的原因。
func (c *Client) servedByLocalSource(ctx context.Context, id, version string) bool {
	for _, f := range c.fetchers {
		if f.kind() != config.SourceTypeLocal {
			return false
		}
		raw, err := f.manifestBytes(ctx, id, version)
		if err == nil && manifestMatches(raw, id, version) {
			return true
		}
	}
	return false
}

// ManifestCachePath 返回 Manifest 缓存路径，如
// .brickkit/manifests/people-basic-1.0.0.yaml（003 §7.1）。
func (c *Client) ManifestCachePath(id, version string) string {
	name := strings.ReplaceAll(id, "/", "-") + "-" + version + ".yaml"
	return filepath.Join(c.layout.ManifestsDir(), name)
}

// ArtifactDir 返回某个组件版本的产物缓存目录，如
// .brickkit/artifacts/department-tree-1-0-0/（003 §7.1）。
func (c *Client) ArtifactDir(id, version string) string {
	return filepath.Join(c.layout.ArtifactsDir(), manifest.ServiceName(id, version))
}

// DownloadArtifacts 下载 Manifest 中声明的全部产物到
// .brickkit/artifacts/<版本化服务名>/<type>/<文件路径>。
//
// 已缓存的文件默认跳过；Options.Refresh 为 true 时重新下载。
// 单个文件下载失败只记入 Warnings，不阻断（004 §10.1：产物是开发时辅助）。
func (c *Client) DownloadArtifacts(ctx context.Context, m *manifest.Manifest) (*ArtifactResult, error) {
	if m == nil {
		return nil, clierr.New(clierr.CodeInternal, "错误：未提供 Manifest")
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
				// Manifest 校验已禁止越界路径（002 §2.3），这里是纵深防御（008）。
				res.Warnings = append(res.Warnings, artifactWarning(id, version, art.Type, file,
					"产物路径越出组件的产物目录"))
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

// Origin 按安装源优先级查询组件的来源信息（开源 git / 闭源 registry，007 §11）。
//
// 它不走 Manifest 缓存：缓存里存的是 component.yaml，不含 sourceType / gitUrl。
// 只有 brickkit add --repo / --repo-all 需要这个信息。
func (c *Client) Origin(ctx context.Context, id, version string) (*Origin, error) {
	if err := checkRef(id, version); err != nil {
		return nil, err
	}
	if len(c.fetchers) == 0 {
		return nil, noSourcesError()
	}

	var failures []failure
	for _, f := range c.fetchers {
		origin, err := f.origin(ctx, id, version)
		if err != nil {
			failures = append(failures, failure{sourceID: f.id(), err: err})
			continue
		}
		return origin, nil
	}
	return nil, c.aggregateError(id, version, failures)
}

// fetchManifest 按优先级遍历安装源，返回首个命中的 Manifest
// （原始字节 + 解析结果 + 源 id + 该源提供的签名）。
func (c *Client) fetchManifest(
	ctx context.Context, id, version string,
) ([]byte, *manifest.Manifest, string, string, *security.Signature, error) {
	if len(c.fetchers) == 0 {
		return nil, nil, "", "", nil, noSourcesError()
	}

	var failures []failure
	for _, f := range c.fetchers {
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
			failures = append(failures, failure{sourceID: f.id(), err: errNotFound})
			continue
		}
		return raw, m, f.id(), f.kind(), signatureFrom(f, id, version), nil
	}
	return nil, nil, "", "", nil, c.aggregateError(id, version, failures)
}

// signedFetcher 是能提供签名的安装源。只有市场源实现它——本地源与 git 源
// 指向的是使用者自己的目录与仓库，那里没有"发布者"这个角色（008 §8.4 说的是
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
	if len(c.fetchers) == 0 {
		return nil, noSourcesError()
	}
	var failures []failure
	for _, f := range c.fetchers {
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
// 那通常才是使用者要修的问题；全部都只是"没有"时，报 004 §10.2 的组件未找到。
func (c *Client) aggregateError(id, version string, failures []failure) error {
	return c.notFoundError(id+"@"+version, failures,
		"检查安装源配置（brickkit.yaml → sources）",
		"确认组件是否已发布到市场",
		"确认版本号是否正确",
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

	tried := make([]string, 0, len(c.fetchers))
	for _, f := range c.fetchers {
		tried = append(tried, f.id()+"（"+f.kind()+"）")
	}
	return clierr.New(clierr.CodeComponentNotFound, "错误：组件未找到").
		WithDetail("组件", ref).
		WithDetail("原因", "该组件在所有安装源中均未找到").
		WithDetail("已尝试的安装源", strings.Join(tried, "、")).
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
		dup.Details = append(append([]clierr.Detail{}, e.Details...), clierr.Detail{Key: "组件", Value: ref})
		return &dup
	}
	return nil
}

func noSourcesError() error {
	return clierr.New(clierr.CodeConfigInvalid, "错误：没有可用的安装源").
		WithDetail("原因", "brickkit.yaml 中未配置 sources，或全部 sources 都是 enabled: false").
		WithHint(
			"在 brickkit.yaml → sources 中配置至少一个安装源（003 §6）",
			"本地开发可用 type: local 指向 ./components",
		)
}

// checkRef 校验组件引用。ID 与版本要用于拼接缓存文件名与目录名，必须先合法。
func checkRef(id, version string) error {
	if problem := manifest.ComponentIDProblem(id); problem != "" {
		return clierr.Newf(clierr.CodeInvalidArgument, "错误：组件 ID 不合法：%s", id).
			WithDetail("原因", problem).
			WithHint("组件 ID 格式为 <scope>/<name>，如 people/basic（002 §2.3）")
	}
	if !manifest.IsExactVersion(version) {
		return clierr.Newf(clierr.CodeInvalidArgument, "错误：版本号不合法：%s", version).
			WithDetail("组件", id).
			WithHint("必须是精确版本 major.minor.patch，如 1.0.0；不接受 ^ 或 ~ 等范围约束（002 §7.1）")
	}
	return nil
}

// describe 生成该 Manifest 的来源描述，用于解析错误提示。
func describe(f fetcher, id, version string) string {
	return f.id() + "（" + f.kind() + "）：" + id + "@" + version
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
	return clierr.Warn(clierr.CodeNetworkUnreachable, "警告：产物下载失败，已跳过").
		WithDetail("组件", id+"@"+version).
		WithDetail("产物", artType+" / "+file).
		WithDetail("原因", reason).
		WithTip("产物文件是开发时辅助，不影响运行；可稍后重新执行 brickkit add <组件> 重试")
}

// reasonOf 把一个错误压成一行原因，用于产物下载警告。
//
// 警告只有一行"原因"可用，因此标题与"原因"明细都要保留：
// 只留标题会丢掉状态码/系统报错，只留明细则看不出是哪一环出的问题。
func reasonOf(err error) string {
	if isNotFound(err) {
		return "所有安装源中都没有该产物文件"
	}
	e := clierr.As(err)
	title := strings.TrimPrefix(e.Message, "错误：")
	for _, d := range e.Details {
		if d.Key == "原因" {
			return title + "：" + d.Value
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
