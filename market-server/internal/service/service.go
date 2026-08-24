// Package service 实现市场的业务编排：发布、查询、下载、可见性、状态与审计。
//
// 设计依据：007 §5（可见性与权限）、§6（版本状态）、§9（API 行为）、§16（审计）、§18（发布校验）。
//
// 分层约定：
//
//	validator  只判断"这份 Manifest 合不合规"
//	repo       只管存取
//	service    编排 + 鉴权 + 审计，是唯一知道"谁能做什么"的地方
//	handler    只做 HTTP 协议转换，不含业务判断
package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/brickkit/market-server/internal/model"
	"github.com/brickkit/market-server/internal/repo"
	"github.com/brickkit/market-server/internal/storage"
	"github.com/brickkit/market-server/internal/validator"
)

// DefaultTokenTTL 是登录令牌的默认有效期（004 §5.3：CLI 会检查 expiresAt）。
const DefaultTokenTTL = 30 * 24 * time.Hour

// maxScanComponents 是"遍历 private 组件判断可见性"时的上限，防止无界扫描。
const maxScanComponents = 1000

// Options 是服务的可选配置。字段留空时用默认值；测试可注入时钟与 ID 生成器。
type Options struct {
	TokenTTL time.Duration
	Now      func() time.Time
	NewID    func() string
	// BcryptCost 是密码哈希成本。留空用 bcrypt 默认值；
	// 测试里调到最低，否则每个用例都要为哈希多花几百毫秒。
	BcryptCost int
}

// Service 是市场的业务编排层。
type Service struct {
	repo       repo.Repository
	store      storage.ArtifactStore
	tokenTTL   time.Duration
	now        func() time.Time
	newID      func() string
	bcryptCost int
}

// New 构造服务。
func New(r repo.Repository, store storage.ArtifactStore, opts Options) *Service {
	s := &Service{
		repo:       r,
		store:      store,
		tokenTTL:   opts.TokenTTL,
		now:        opts.Now,
		newID:      opts.NewID,
		bcryptCost: opts.BcryptCost,
	}
	if s.bcryptCost <= 0 {
		s.bcryptCost = bcrypt.DefaultCost
	}
	if s.tokenTTL <= 0 {
		s.tokenTTL = DefaultTokenTTL
	}
	if s.now == nil {
		s.now = func() time.Time { return time.Now().UTC() }
	}
	if s.newID == nil {
		s.newID = randomID
	}
	return s
}

// ============================================================
// 发布
// ============================================================

// Publish 发布一个新版本（007 §17.2、§18）。
//
// 顺序：认证 → Manifest 校验 → 归属权检查 → 建组件 → 建版本 → 落产物元数据 → 审计。
// 校验放在最前面：不合规的东西根本不该在库里留下任何痕迹。
func (s *Service) Publish(
	ctx context.Context, id *Identity, componentID string, req model.PublishRequest,
) (*model.Version, error) {
	if err := requireAuth(id, "发布组件"); err != nil {
		return nil, err
	}

	manifest, err := validator.Validate(req)
	if err != nil {
		return nil, err
	}
	// 结构上就不可能有效的签名当场退回，不让它在库里留下痕迹
	if err := req.Signature.Validate(); err != nil {
		return nil, err
	}
	if manifest.Metadata.ID != componentID {
		return nil, model.Errorf(model.CodeInvalidRequest,
			"组件 ID 与 Manifest 中的 metadata.id 不一致").
			WithDetail("path", componentID).
			WithDetail("manifest", manifest.Metadata.ID)
	}

	existing, err := s.repo.GetComponent(ctx, componentID)
	switch {
	case err == nil:
		if err := requireOwner(id, existing, "发布该组件的新版本"); err != nil {
			return nil, err
		}
	case errors.Is(err, repo.ErrNotFound):
		// 首次创建这个组件：这时才检查命名空间归属（007 §14.2）。
		// 已存在的组件走上面的 requireOwner——那条更严格，也更具体。
		if err := requireNamespace(id, componentID); err != nil {
			return nil, err
		}
		existing = nil
	default:
		return nil, internalError(err)
	}

	component := s.buildComponent(existing, id, componentID, req, manifest)
	if err := s.repo.UpsertComponent(ctx, component); err != nil {
		return nil, internalError(err)
	}

	status := req.Status
	if status == "" {
		status = model.VersionDraft
	}
	version := &model.Version{
		ComponentID: componentID,
		Version:     manifest.Metadata.Version,
		Status:      status,
		Manifest:    req.Manifest,
		Changelog:   req.Changelog,
		PublishedAt: s.now(),
		PublishedBy: id.Username,
		Signature:   req.Signature,
	}
	if err := s.repo.CreateVersion(ctx, version); err != nil {
		if errors.Is(err, repo.ErrConflict) {
			// 18.14：版本号不可重复，也不可回收（软删除的版本同样占位）
			return nil, model.Errorf(model.CodeVersionExists, "该版本已存在，版本号不可重复发布").
				WithDetail("componentId", componentID).
				WithDetail("version", manifest.Metadata.Version)
		}
		return nil, internalError(err)
	}

	if err := s.repo.PutArtifacts(ctx, componentID, version.Version,
		artifactRecords(componentID, version.Version, manifest.Artifacts)); err != nil {
		return nil, internalError(err)
	}

	s.audit(ctx, &model.AuditEntry{
		Action: model.ActionVersionPublished, ComponentID: componentID,
		Version: version.Version, Operator: id.Username, Result: model.ResultSuccess,
	})
	return version, nil
}

// buildComponent 根据 Manifest 与请求组装组件记录。
// 已存在的组件保留原有的 owner 与可见性：发布新版本不该悄悄改变这两样。
func (s *Service) buildComponent(
	existing *model.Component, id *Identity, componentID string,
	req model.PublishRequest, manifest *model.Manifest,
) *model.Component {
	c := &model.Component{
		ComponentID: componentID,
		Name:        manifest.Metadata.Name,
		Description: manifest.Metadata.Description,
		Vendor:      manifest.Metadata.Vendor,
		Visibility:  model.VisibilityPublic,
		SourceType:  req.SourceType,
		GitURL:      req.GitURL,
		Status:      model.ComponentActive,
		OwnerID:     id.UserID,
		Tags:        manifest.Tags,
	}
	if existing != nil {
		c.OwnerID = existing.OwnerID
		c.Visibility = existing.Visibility
		c.Status = existing.Status
	}
	if req.Visibility != "" {
		c.Visibility = req.Visibility
	}
	return c
}

// artifactRecords 把 Manifest 中的 artifacts 声明落成产物记录。
func artifactRecords(componentID, version string, artifacts []model.Artifact) []model.ArtifactRecord {
	records := make([]model.ArtifactRecord, 0, len(artifacts))
	for i, a := range artifacts {
		records = append(records, model.ArtifactRecord{
			ArtifactID:  "art-" + strconv.Itoa(i),
			ComponentID: componentID,
			Version:     version,
			Type:        a.Type,
			Format:      a.Format,
			Description: a.Description,
			Reference:   a.Reference,
			Files:       a.Files,
		})
	}
	return records
}

// ============================================================
// 产物
// ============================================================

// UploadArtifact 上传一个产物文件（007 §9.3）。
func (s *Service) UploadArtifact(
	ctx context.Context, id *Identity,
	componentID, version, artifactID, file string, r io.Reader, size int64,
) error {
	component, err := s.loadComponent(ctx, componentID)
	if err != nil {
		return err
	}
	if err := requireOwner(id, component, "上传产物"); err != nil {
		return err
	}
	if _, err := s.loadVersion(ctx, componentID, version); err != nil {
		return err
	}

	artifact, err := s.repo.GetArtifact(ctx, componentID, version, artifactID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return model.Errorf(model.CodeNotFound, "产物不存在："+artifactID)
		}
		return internalError(err)
	}
	// 上传的文件必须与 Manifest 声明一致（007 §18.2）
	if !containsString(artifact.Files, file) {
		return model.Errorf(model.CodeInvalidRequest, "该文件未在 Manifest 中声明："+file).
			WithDetail("declared", artifact.Files)
	}

	key := storage.ObjectKey(componentID, version, artifact.Type, file)
	if err := s.store.Put(ctx, key, r, size); err != nil {
		return internalError(err)
	}
	if err := s.repo.MarkArtifactUploaded(ctx, componentID, version, artifactID, file); err != nil {
		return internalError(err)
	}

	s.audit(ctx, &model.AuditEntry{
		Action: model.ActionArtifactUploaded, ComponentID: componentID, Version: version,
		Operator: id.Username, Result: model.ResultSuccess, Detail: file,
	})
	return nil
}

// ListArtifacts 列出某个版本的产物（供 CLI 下载前查询）。
func (s *Service) ListArtifacts(
	ctx context.Context, id *Identity, componentID, version string,
) ([]model.ArtifactRecord, error) {
	if _, err := s.readableVersion(ctx, id, componentID, version); err != nil {
		return nil, err
	}
	records, err := s.repo.ListArtifacts(ctx, componentID, version)
	if err != nil {
		return nil, internalError(err)
	}
	return records, nil
}

// DownloadArtifact 下载一个产物文件，并记录下载与审计（007 §16.1）。
func (s *Service) DownloadArtifact(
	ctx context.Context, id *Identity, componentID, version, artifactID, file string,
) (io.ReadCloser, error) {
	if _, err := s.readableVersion(ctx, id, componentID, version); err != nil {
		return nil, err
	}

	artifact, err := s.repo.GetArtifact(ctx, componentID, version, artifactID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, model.Errorf(model.CodeNotFound, "产物不存在："+artifactID)
		}
		return nil, internalError(err)
	}
	if !containsString(artifact.Files, file) {
		return nil, model.Errorf(model.CodeNotFound, "该产物不包含文件："+file)
	}

	reader, err := s.store.Get(ctx, storage.ObjectKey(componentID, version, artifact.Type, file))
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			return nil, model.Errorf(model.CodeNotFound, "产物文件尚未上传："+file)
		}
		return nil, internalError(err)
	}

	if err := s.repo.AddDownload(ctx, componentID, 1); err != nil {
		return nil, internalError(err)
	}
	s.audit(ctx, &model.AuditEntry{
		Action: model.ActionArtifactDownload, ComponentID: componentID, Version: version,
		Operator: operatorOf(id), Result: model.ResultSuccess, Detail: artifact.Type + "/" + file,
	})
	return reader, nil
}

// ============================================================
// 查询
// ============================================================

// ManifestView 是 Manifest 查询结果，附带 CLI 需要的来源信息（007 §11）。
type ManifestView struct {
	ComponentID string          `json:"componentId"`
	Version     string          `json:"version"`
	Status      string          `json:"status"`
	Manifest    json.RawMessage `json:"manifest"`
	SourceType  string          `json:"sourceType"`
	GitURL      string          `json:"gitUrl,omitempty"`
	// Signature 随 Manifest 一起返回：CLI 在 add 时只请求这一个端点（007 §4.5），
	// 签名不跟着回来，使用者就得再猜一次它在哪儿——或者干脆验不了。
	Signature *model.Signature `json:"signature,omitempty"`
}

// GetManifest 获取某个版本的 Manifest（007 §4.5）。
func (s *Service) GetManifest(
	ctx context.Context, id *Identity, componentID, version string,
) (*ManifestView, error) {
	component, err := s.loadReadableComponent(ctx, id, componentID)
	if err != nil {
		return nil, err
	}
	v, err := s.installableVersion(ctx, id, component, version)
	if err != nil {
		return nil, err
	}

	return &ManifestView{
		ComponentID: componentID,
		Version:     v.Version,
		Status:      v.Status,
		Manifest:    v.Manifest,
		SourceType:  component.SourceType,
		GitURL:      component.GitURL,
		Signature:   v.Signature,
	}, nil
}

// ListVersions 列出组件的版本（默认隐藏 draft 与已删除版本）。
func (s *Service) ListVersions(
	ctx context.Context, id *Identity, componentID string,
) ([]model.Version, error) {
	component, err := s.loadReadableComponent(ctx, id, componentID)
	if err != nil {
		return nil, err
	}

	versions, err := s.repo.ListVersions(ctx, componentID)
	if err != nil {
		return nil, internalError(err)
	}

	owner := isOwner(id, component)
	out := make([]model.Version, 0, len(versions))
	for _, v := range versions {
		if v.Status == model.VersionDeleted {
			continue
		}
		if v.Status == model.VersionDraft && !owner {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

// ComponentDetail 是组件详情（007 §4.3）。
type ComponentDetail struct {
	Component     model.Component `json:"component"`
	Versions      []string        `json:"versions"`
	LatestVersion string          `json:"latestVersion,omitempty"`
}

// GetComponent 查询组件详情。
func (s *Service) GetComponent(
	ctx context.Context, id *Identity, componentID string,
) (*ComponentDetail, error) {
	component, err := s.loadReadableComponent(ctx, id, componentID)
	if err != nil {
		return nil, err
	}
	versions, err := s.ListVersions(ctx, id, componentID)
	if err != nil {
		return nil, err
	}

	detail := &ComponentDetail{Component: *component}
	for _, v := range versions {
		detail.Versions = append(detail.Versions, v.Version)
	}
	// ListVersions 已按版本号倒序，第一个就是最新版本
	if len(detail.Versions) > 0 {
		detail.LatestVersion = detail.Versions[0]
	}
	return detail, nil
}

// SearchResult 是一页搜索结果（007 §4.2）。
type SearchResult struct {
	// Items 是当前页的组件，永远非 nil。
	Items []model.Component `json:"items"`
	// Total 是符合条件的总数（忽略分页），前端据此算页数。
	Total int `json:"total"`
}

// SearchComponents 搜索组件（007 §4.2）。
//
// 可见性作为查询条件下推到仓储层：先分页再过滤会导致翻页缺条目。
func (s *Service) SearchComponents(
	ctx context.Context, id *Identity, q repo.ComponentQuery,
) (*SearchResult, error) {
	allowed, err := s.visibleComponentIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	q.Visibilities = []string{model.VisibilityPublic}
	q.AlsoInclude = allowed
	q.IncludeBlocked = false

	components, err := s.repo.ListComponents(ctx, q)
	if err != nil {
		return nil, internalError(err)
	}
	total, err := s.repo.CountComponents(ctx, q)
	if err != nil {
		return nil, internalError(err)
	}

	if components == nil {
		components = []model.Component{}
	}
	return &SearchResult{Items: components, Total: total}, nil
}

// ============================================================
// 状态与可见性
// ============================================================

// SetVersionStatus 变更版本状态（007 §6.3）。
//
// 权限：stable / deprecated 由所有者变更；blocked 只有市场管理员。
func (s *Service) SetVersionStatus(
	ctx context.Context, id *Identity, componentID, version, status string,
) error {
	if !validVersionStatus(status) {
		return model.Errorf(model.CodeInvalidRequest, "版本状态不合法："+status)
	}

	component, err := s.loadComponent(ctx, componentID)
	if err != nil {
		return err
	}
	if status == model.VersionBlocked {
		if err := requireAdmin(id, "阻止组件版本"); err != nil {
			return err
		}
	} else if err := requireOwner(id, component, "变更版本状态"); err != nil {
		return err
	}

	if _, err := s.loadVersion(ctx, componentID, version); err != nil {
		return err
	}
	if status == model.VersionStable {
		if err := s.ensureArtifactsUploaded(ctx, componentID, version); err != nil {
			return err
		}
	}

	if err := s.repo.SetVersionStatus(ctx, componentID, version, status); err != nil {
		return internalError(err)
	}
	s.audit(ctx, &model.AuditEntry{
		Action: model.ActionVersionStatus, ComponentID: componentID, Version: version,
		Operator: id.Username, Result: model.ResultSuccess, Detail: status,
	})
	return nil
}

// ensureArtifactsUploaded 校验声明的产物文件都已上传（007 §18.2）。
func (s *Service) ensureArtifactsUploaded(ctx context.Context, componentID, version string) error {
	records, err := s.repo.ListArtifacts(ctx, componentID, version)
	if err != nil {
		return internalError(err)
	}

	var missing []string
	for _, a := range records {
		for _, file := range a.Files {
			if !containsString(a.Uploaded, file) {
				missing = append(missing, file)
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return model.Errorf(model.CodeConflict,
		"以下产物文件尚未上传，不能标记为 stable："+joinStrings(missing, "、")).
		WithDetail("missing", missing)
}

// DeleteVersion 删除版本。已发布的版本只做**软删除**（18.24）：
// 历史必须可追溯，而且版本号不能被回收后指向不同的内容。
func (s *Service) DeleteVersion(ctx context.Context, id *Identity, componentID, version string) error {
	component, err := s.loadComponent(ctx, componentID)
	if err != nil {
		return err
	}
	if err := requireOwner(id, component, "删除版本"); err != nil {
		return err
	}
	if _, err := s.loadVersion(ctx, componentID, version); err != nil {
		return err
	}

	if err := s.repo.SetVersionStatus(ctx, componentID, version, model.VersionDeleted); err != nil {
		return internalError(err)
	}
	s.audit(ctx, &model.AuditEntry{
		Action: model.ActionVersionDeleted, ComponentID: componentID, Version: version,
		Operator: id.Username, Result: model.ResultSuccess,
	})
	return nil
}

// SetVisibility 设置组件可见性（007 §9.4）。
func (s *Service) SetVisibility(ctx context.Context, id *Identity, componentID, visibility string) error {
	if visibility != model.VisibilityPublic && visibility != model.VisibilityPrivate {
		return model.Errorf(model.CodeInvalidRequest, "可见性只能是 public 或 private")
	}
	component, err := s.loadComponent(ctx, componentID)
	if err != nil {
		return err
	}
	if err := requireOwner(id, component, "变更可见性"); err != nil {
		return err
	}

	if err := s.repo.SetVisibility(ctx, componentID, visibility); err != nil {
		return internalError(err)
	}
	s.audit(ctx, &model.AuditEntry{
		Action: model.ActionVisibilityChanged, ComponentID: componentID,
		Operator: id.Username, Result: model.ResultSuccess, Detail: visibility,
	})
	return nil
}

// SetComponentStatus 下架 / 恢复组件。只有市场管理员能做（007 §15）。
func (s *Service) SetComponentStatus(ctx context.Context, id *Identity, componentID, status string) error {
	if status != model.ComponentActive && status != model.ComponentBlocked {
		return model.Errorf(model.CodeInvalidRequest, "组件状态只能是 active 或 blocked")
	}
	if err := requireAdmin(id, "变更组件状态"); err != nil {
		return err
	}
	if _, err := s.loadComponent(ctx, componentID); err != nil {
		return err
	}

	if err := s.repo.SetComponentStatus(ctx, componentID, status); err != nil {
		return internalError(err)
	}
	s.audit(ctx, &model.AuditEntry{
		Action: model.ActionVersionStatus, ComponentID: componentID,
		Operator: id.Username, Result: model.ResultSuccess, Detail: "component:" + status,
	})
	return nil
}

// SetAccessPolicies 覆盖组件的访问策略（007 §9.4）。
func (s *Service) SetAccessPolicies(
	ctx context.Context, id *Identity, componentID string, policies []model.AccessPolicy,
) error {
	component, err := s.loadComponent(ctx, componentID)
	if err != nil {
		return err
	}
	if err := requireOwner(id, component, "变更访问策略"); err != nil {
		return err
	}
	for _, p := range policies {
		if p.TargetType != model.TargetUser && p.TargetType != model.TargetOrganization {
			return model.Errorf(model.CodeInvalidRequest, "访问策略的 targetType 只能是 user 或 organization")
		}
		if p.TargetID == "" {
			return model.Errorf(model.CodeInvalidRequest, "访问策略必须指定 targetId")
		}
	}

	if err := s.repo.ReplaceAccessPolicies(ctx, componentID, policies); err != nil {
		return internalError(err)
	}
	s.audit(ctx, &model.AuditEntry{
		Action: model.ActionAccessChanged, ComponentID: componentID,
		Operator: id.Username, Result: model.ResultSuccess,
	})
	return nil
}

// ListAccessPolicies 查询访问策略。
func (s *Service) ListAccessPolicies(
	ctx context.Context, id *Identity, componentID string,
) ([]model.AccessPolicy, error) {
	component, err := s.loadComponent(ctx, componentID)
	if err != nil {
		return nil, err
	}
	if err := requireOwner(id, component, "查询访问策略"); err != nil {
		return nil, err
	}
	policies, err := s.repo.ListAccessPolicies(ctx, componentID)
	if err != nil {
		return nil, internalError(err)
	}
	return policies, nil
}

// ============================================================
// 审计
// ============================================================

// ListAudit 查询审计日志。里面有"谁下载了什么"，属于敏感信息，必须登录才能看。
func (s *Service) ListAudit(
	ctx context.Context, id *Identity, q repo.AuditQuery,
) ([]model.AuditEntry, error) {
	if err := requireAuth(id, "查询审计日志"); err != nil {
		return nil, err
	}
	entries, err := s.repo.ListAudit(ctx, q)
	if err != nil {
		return nil, internalError(err)
	}
	return entries, nil
}

// audit 追加审计日志。审计失败不该让主流程失败——
// 主操作已经成功了，回滚它反而更糟；这里记录但不阻断。
func (s *Service) audit(ctx context.Context, e *model.AuditEntry) {
	if e.Time.IsZero() {
		e.Time = s.now()
	}
	_ = s.repo.AppendAudit(ctx, e)
}

// ============================================================
// 内部辅助
// ============================================================

// loadComponent 读取组件，不存在时返回 404。
func (s *Service) loadComponent(ctx context.Context, componentID string) (*model.Component, error) {
	c, err := s.repo.GetComponent(ctx, componentID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, model.Errorf(model.CodeNotFound, "组件不存在："+componentID)
		}
		return nil, internalError(err)
	}
	return c, nil
}

// loadReadableComponent 读取组件并检查可见性。
func (s *Service) loadReadableComponent(
	ctx context.Context, id *Identity, componentID string,
) (*model.Component, error) {
	c, err := s.loadComponent(ctx, componentID)
	if err != nil {
		return nil, err
	}
	if err := s.requireRead(ctx, id, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) loadVersion(ctx context.Context, componentID, version string) (*model.Version, error) {
	v, err := s.repo.GetVersion(ctx, componentID, version)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, model.Errorf(model.CodeNotFound, "版本不存在："+componentID+"@"+version)
		}
		return nil, internalError(err)
	}
	return v, nil
}

// readableVersion 是"可见 + 可安装"的组合检查。
func (s *Service) readableVersion(
	ctx context.Context, id *Identity, componentID, version string,
) (*model.Version, error) {
	component, err := s.loadReadableComponent(ctx, id, componentID)
	if err != nil {
		return nil, err
	}
	return s.installableVersion(ctx, id, component, version)
}

// installableVersion 检查版本能否被安装（007 §6）：
//
//	blocked   → 明确告知被阻止，而不是含糊的 404
//	deleted   → 对外等同于不存在
//	draft     → 只有所有者能看到（还没传完产物）
func (s *Service) installableVersion(
	ctx context.Context, id *Identity, component *model.Component, version string,
) (*model.Version, error) {
	if component.Status == model.ComponentBlocked {
		return nil, blockedError(component.ComponentID, version, "该组件已被市场下架，不能安装")
	}

	v, err := s.loadVersion(ctx, component.ComponentID, version)
	if err != nil {
		return nil, err
	}

	switch {
	case v.Status == model.VersionBlocked:
		return nil, blockedError(component.ComponentID, version, "该版本已被市场阻止，不能安装")
	case v.Status == model.VersionDeleted:
		return nil, model.Errorf(model.CodeNotFound, "版本不存在："+component.ComponentID+"@"+version)
	case v.Status == model.VersionDraft && !isOwner(id, component):
		return nil, model.Errorf(model.CodeNotFound, "版本不存在："+component.ComponentID+"@"+version)
	}
	return v, nil
}

func blockedError(componentID, version, message string) error {
	return model.Errorf(model.CodeComponentBlocked, message).
		WithDetail("componentId", componentID).
		WithDetail("version", version)
}

func isOwner(id *Identity, c *model.Component) bool {
	return id != nil && !id.Anonymous && (id.IsAdmin || id.UserID == c.OwnerID)
}

func validVersionStatus(status string) bool {
	switch status {
	case model.VersionDraft, model.VersionStable, model.VersionDeprecated, model.VersionBlocked:
		return true
	}
	return false
}

func operatorOf(id *Identity) string {
	if id == nil || id.Anonymous {
		return "anonymous"
	}
	return id.Username
}

// internalError 把底层错误包成对外的 500，不泄漏实现细节（004 §10）。
func internalError(err error) error {
	return model.Errorf(model.CodeInternal, "市场内部错误").WithDetail("cause", err.Error())
}

func containsString(items []string, target string) bool {
	for _, i := range items {
		if i == target {
			return true
		}
	}
	return false
}

func joinStrings(items []string, sep string) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += sep
		}
		out += item
	}
	return out
}
