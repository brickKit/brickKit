package repo

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brickkit/market-server/internal/model"
)

// Memory 是仓储的进程内实现。
//
// 用途有两个：单元测试（快、无外部依赖），以及"先跑起来看看"的本地模式
// （MARKET_STORE=memory）。它与 PostgreSQL 实现共用同一份行为契约测试。
type Memory struct {
	mu sync.RWMutex

	components map[string]model.Component
	// versions 的键是 <componentID>@<version>
	versions map[string]model.Version
	// artifacts 的键是 <componentID>@<version>，值按写入顺序保存
	artifacts map[string][]model.ArtifactRecord
	policies  map[string][]model.AccessPolicy
	users     map[string]model.User // 键是 userID
	tokens    map[string]model.Token
	audit     []model.AuditEntry
	auditSeq  int64
}

// NewMemory 创建一个空的内存仓储。
func NewMemory() *Memory {
	return &Memory{
		components: map[string]model.Component{},
		versions:   map[string]model.Version{},
		artifacts:  map[string][]model.ArtifactRecord{},
		policies:   map[string][]model.AccessPolicy{},
		users:      map[string]model.User{},
		tokens:     map[string]model.Token{},
	}
}

func versionKey(componentID, version string) string { return componentID + "@" + version }

// ============================================================
// 组件
// ============================================================

func (m *Memory) UpsertComponent(_ context.Context, c *model.Component) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	stored := *c
	stored.Tags = append([]string(nil), c.Tags...)

	if old, ok := m.components[c.ComponentID]; ok {
		stored.CreatedAt = old.CreatedAt
		stored.Downloads = old.Downloads
	} else {
		stored.CreatedAt = now
	}
	stored.UpdatedAt = now
	m.components[c.ComponentID] = stored
	return nil
}

func (m *Memory) GetComponent(_ context.Context, componentID string) (*model.Component, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	c, ok := m.components[componentID]
	if !ok {
		return nil, ErrNotFound
	}
	return copyComponent(c), nil
}

func (m *Memory) ListComponents(_ context.Context, q ComponentQuery) ([]model.Component, error) {
	out := m.filterComponents(q)
	sortComponents(out)
	return paginate(out, q.Page, q.PageSize), nil
}

// CountComponents 统计符合条件的组件数，忽略分页（007 §4.2 的 total）。
func (m *Memory) CountComponents(_ context.Context, q ComponentQuery) (int, error) {
	return len(m.filterComponents(q)), nil
}

// filterComponents 应用除分页外的全部过滤条件。
func (m *Memory) filterComponents(q ComponentQuery) []model.Component {
	m.mu.RLock()
	defer m.mu.RUnlock()

	allowed := toSet(q.AlsoInclude)
	visibilities := toSet(q.Visibilities)

	var out []model.Component
	for _, c := range m.components {
		if c.Status == model.ComponentBlocked && !q.IncludeBlocked {
			continue
		}
		if len(visibilities) > 0 && !visibilities[c.Visibility] && !allowed[c.ComponentID] {
			continue
		}
		if !matchesKeyword(c, q.Keyword) || !hasAllTags(c.Tags, q.Tags) {
			continue
		}
		out = append(out, *copyComponent(c))
	}
	return out
}

func (m *Memory) SetVisibility(_ context.Context, componentID, visibility string) error {
	return m.updateComponent(componentID, func(c *model.Component) { c.Visibility = visibility })
}

func (m *Memory) SetComponentStatus(_ context.Context, componentID, status string) error {
	return m.updateComponent(componentID, func(c *model.Component) { c.Status = status })
}

func (m *Memory) AddDownload(_ context.Context, componentID string, delta int64) error {
	return m.updateComponent(componentID, func(c *model.Component) { c.Downloads += delta })
}

func (m *Memory) updateComponent(componentID string, mutate func(*model.Component)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.components[componentID]
	if !ok {
		return ErrNotFound
	}
	mutate(&c)
	c.UpdatedAt = time.Now().UTC()
	m.components[componentID] = c
	return nil
}

// ============================================================
// 版本
// ============================================================

func (m *Memory) CreateVersion(_ context.Context, v *model.Version) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := versionKey(v.ComponentID, v.Version)
	if _, exists := m.versions[key]; exists {
		return ErrConflict
	}

	stored := *v
	stored.Manifest = append(json.RawMessage(nil), v.Manifest...)
	if stored.PublishedAt.IsZero() {
		stored.PublishedAt = time.Now().UTC()
	}
	m.versions[key] = stored
	return nil
}

func (m *Memory) GetVersion(_ context.Context, componentID, version string) (*model.Version, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	v, ok := m.versions[versionKey(componentID, version)]
	if !ok {
		return nil, ErrNotFound
	}
	return copyVersion(v), nil
}

func (m *Memory) ListVersions(_ context.Context, componentID string) ([]model.Version, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []model.Version
	for _, v := range m.versions {
		if v.ComponentID == componentID {
			out = append(out, *copyVersion(v))
		}
	}
	sortVersionsDesc(out)
	return out, nil
}

func (m *Memory) SetVersionStatus(_ context.Context, componentID, version, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := versionKey(componentID, version)
	v, ok := m.versions[key]
	if !ok {
		return ErrNotFound
	}
	v.Status = status
	m.versions[key] = v
	return nil
}

// ============================================================
// 产物
// ============================================================

func (m *Memory) PutArtifacts(_ context.Context, componentID, version string, artifacts []model.ArtifactRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	stored := make([]model.ArtifactRecord, 0, len(artifacts))
	for _, a := range artifacts {
		a.ComponentID, a.Version = componentID, version
		a.Files = append([]string(nil), a.Files...)
		a.Uploaded = append([]string(nil), a.Uploaded...)
		stored = append(stored, a)
	}
	m.artifacts[versionKey(componentID, version)] = stored
	return nil
}

func (m *Memory) ListArtifacts(_ context.Context, componentID, version string) ([]model.ArtifactRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []model.ArtifactRecord
	for _, a := range m.artifacts[versionKey(componentID, version)] {
		out = append(out, *copyArtifact(a))
	}
	return out, nil
}

func (m *Memory) GetArtifact(_ context.Context, componentID, version, artifactID string) (*model.ArtifactRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, a := range m.artifacts[versionKey(componentID, version)] {
		if a.ArtifactID == artifactID {
			return copyArtifact(a), nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) MarkArtifactUploaded(_ context.Context, componentID, version, artifactID, file string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := versionKey(componentID, version)
	list := m.artifacts[key]
	for i, a := range list {
		if a.ArtifactID != artifactID {
			continue
		}
		if !contains(a.Uploaded, file) {
			a.Uploaded = append(append([]string(nil), a.Uploaded...), file)
			list[i] = a
			m.artifacts[key] = list
		}
		return nil
	}
	return ErrNotFound
}

// ============================================================
// 访问策略
// ============================================================

func (m *Memory) ReplaceAccessPolicies(_ context.Context, componentID string, policies []model.AccessPolicy) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(policies) == 0 {
		delete(m.policies, componentID)
		return nil
	}
	stored := make([]model.AccessPolicy, 0, len(policies))
	for _, p := range policies {
		p.ComponentID = componentID
		stored = append(stored, p)
	}
	m.policies[componentID] = stored
	return nil
}

func (m *Memory) ListAccessPolicies(_ context.Context, componentID string) ([]model.AccessPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return append([]model.AccessPolicy(nil), m.policies[componentID]...), nil
}

// ============================================================
// 用户与令牌
// ============================================================

func (m *Memory) CreateUser(_ context.Context, u *model.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, existing := range m.users {
		if existing.Username == u.Username {
			return ErrConflict
		}
	}
	stored := *u
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = time.Now().UTC()
	}
	m.users[u.UserID] = stored
	return nil
}

func (m *Memory) GetUserByUsername(_ context.Context, username string) (*model.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, u := range m.users {
		if u.Username == username {
			copied := u
			return &copied, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) GetUserByID(_ context.Context, userID string) (*model.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	u, ok := m.users[userID]
	if !ok {
		return nil, ErrNotFound
	}
	copied := u
	return &copied, nil
}

func (m *Memory) SetUserAdmin(_ context.Context, userID string, isAdmin bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	u, ok := m.users[userID]
	if !ok {
		return ErrNotFound
	}
	u.IsAdmin = isAdmin
	m.users[userID] = u
	return nil
}

func (m *Memory) CreateToken(_ context.Context, t *model.Token) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	stored := *t
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = time.Now().UTC()
	}
	m.tokens[t.Token] = stored
	return nil
}

func (m *Memory) GetToken(_ context.Context, token string) (*model.Token, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	t, ok := m.tokens[token]
	if !ok {
		return nil, ErrNotFound
	}
	copied := t
	return &copied, nil
}

// DeleteToken 是幂等的：登出两次不该报错。
func (m *Memory) DeleteToken(_ context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.tokens, token)
	return nil
}

func (m *Memory) DeleteTokensOfUser(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, tok := range m.tokens {
		if tok.UserID == userID {
			delete(m.tokens, key)
		}
	}
	return nil
}

func (m *Memory) SetUserPassword(_ context.Context, userID, passwordHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	u, ok := m.users[userID]
	if !ok {
		return ErrNotFound
	}
	u.PasswordHash = passwordHash
	m.users[userID] = u
	return nil
}

// ============================================================
// 审计
// ============================================================

func (m *Memory) AppendAudit(_ context.Context, e *model.AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.auditSeq++
	e.AuditID = "audit-" + strconv.FormatInt(m.auditSeq, 10)
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	m.audit = append(m.audit, *e)
	return nil
}

func (m *Memory) ListAudit(_ context.Context, q AuditQuery) ([]model.AuditEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []model.AuditEntry
	// 倒序遍历：审计按时间倒序返回
	for i := len(m.audit) - 1; i >= 0; i-- {
		e := m.audit[i]
		if q.ComponentID != "" && e.ComponentID != q.ComponentID {
			continue
		}
		if q.Action != "" && e.Action != q.Action {
			continue
		}
		out = append(out, e)
		if q.Limit > 0 && len(out) >= q.Limit {
			break
		}
	}
	return out, nil
}

// Close 对内存实现无事可做。
func (m *Memory) Close() error { return nil }

// ============================================================
// 辅助
// ============================================================

func copyComponent(c model.Component) *model.Component {
	c.Tags = append([]string(nil), c.Tags...)
	return &c
}

func copyVersion(v model.Version) *model.Version {
	v.Manifest = append(json.RawMessage(nil), v.Manifest...)
	return &v
}

func copyArtifact(a model.ArtifactRecord) *model.ArtifactRecord {
	a.Files = append([]string(nil), a.Files...)
	a.Uploaded = append([]string(nil), a.Uploaded...)
	return &a
}

func matchesKeyword(c model.Component, keyword string) bool {
	if keyword == "" {
		return true
	}
	k := strings.ToLower(keyword)
	return strings.Contains(strings.ToLower(c.ComponentID), k) ||
		strings.Contains(strings.ToLower(c.Name), k) ||
		strings.Contains(strings.ToLower(c.Description), k)
}

func hasAllTags(tags, required []string) bool {
	if len(required) == 0 {
		return true
	}
	have := toSet(tags)
	for _, t := range required {
		if !have[t] {
			return false
		}
	}
	return true
}

func toSet(items []string) map[string]bool {
	if len(items) == 0 {
		return nil
	}
	set := make(map[string]bool, len(items))
	for _, i := range items {
		set[i] = true
	}
	return set
}

func contains(items []string, target string) bool {
	for _, i := range items {
		if i == target {
			return true
		}
	}
	return false
}

// paginate 按 1 起始的页码切片。
func paginate(items []model.Component, page, pageSize int) []model.Component {
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if page <= 0 {
		page = 1
	}
	start := (page - 1) * pageSize
	if start >= len(items) {
		return nil
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}
