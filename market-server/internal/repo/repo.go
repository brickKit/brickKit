// Package repo 定义市场的数据访问层。
//
// 设计依据：007 §10 市场数据模型。
//
// 这里只定义**接口与查询条件**，具体实现有两个：
//
//	memory.go    进程内实现：单元测试与"先跑起来看看"的本地模式
//	postgres.go  生产实现：PostgreSQL（007 §10 的库表）
//
// 两个实现共用一份行为契约测试（repo_test.go），保证语义完全一致——
// 否则测试全绿、上了真库就出问题。
//
// 分层约定：仓储只管"存与取"，鉴权（谁能看到 private 组件）在服务层。
// 但可见性作为**查询条件**下推到这里，避免"先分页再过滤"的错误结果。
package repo

import (
	"context"
	"errors"

	"github.com/brickkit/market-server/internal/model"
)

// 仓储层的语义错误。上层据此翻译成 404 / 409。
var (
	// ErrNotFound 表示记录不存在。
	ErrNotFound = errors.New("记录不存在")
	// ErrConflict 表示唯一键冲突（版本重复、用户名重复等）。
	ErrConflict = errors.New("记录已存在")
)

// ComponentQuery 是组件搜索条件（007 §4.2）。
type ComponentQuery struct {
	// Keyword 匹配组件 ID、名称与描述。
	Keyword string
	// Tags 为交集过滤：组件必须带上全部给定标签。
	Tags []string
	// Visibilities 限定可见性（空表示不限）。
	Visibilities []string
	// AlsoInclude 是额外允许出现的组件 ID：调用者有权访问的 private 组件。
	AlsoInclude []string
	// IncludeBlocked 为 true 时包含已下架组件（管理用途）。
	IncludeBlocked bool
	// Page 从 1 开始；PageSize <= 0 时使用 DefaultPageSize。
	Page     int
	PageSize int
}

// DefaultPageSize 是搜索的默认分页大小（007 §4.2 示例用 20）。
const DefaultPageSize = 20

// AuditQuery 是审计日志查询条件（007 §16）。
type AuditQuery struct {
	ComponentID string
	Action      string
	Limit       int
}

// Repository 是市场的数据访问接口。
type Repository interface {
	// ---- 组件 ----

	// UpsertComponent 新建或更新组件记录（首次发布时创建）。
	UpsertComponent(ctx context.Context, c *model.Component) error
	// GetComponent 按 ID 查询组件，不存在时返回 ErrNotFound。
	GetComponent(ctx context.Context, componentID string) (*model.Component, error)
	// ListComponents 按条件搜索组件。
	ListComponents(ctx context.Context, q ComponentQuery) ([]model.Component, error)
	// CountComponents 统计符合条件的组件总数，忽略分页（007 §4.2 的 total）。
	CountComponents(ctx context.Context, q ComponentQuery) (int, error)
	// SetVisibility 设置可见性（007 §9.4）。
	SetVisibility(ctx context.Context, componentID, visibility string) error
	// SetComponentStatus 设置组件状态（active / blocked）。
	SetComponentStatus(ctx context.Context, componentID, status string) error
	// AddDownload 累加下载计数。
	AddDownload(ctx context.Context, componentID string, delta int64) error

	// ---- 版本 ----

	// CreateVersion 创建版本；同 ID 同版本已存在时返回 ErrConflict（18.14）。
	CreateVersion(ctx context.Context, v *model.Version) error
	// GetVersion 查询单个版本，不存在时返回 ErrNotFound。
	GetVersion(ctx context.Context, componentID, version string) (*model.Version, error)
	// ListVersions 列出组件的全部版本（含 draft / deleted，由上层过滤）。
	ListVersions(ctx context.Context, componentID string) ([]model.Version, error)
	// SetVersionStatus 变更版本状态（含软删除：status = deleted）。
	SetVersionStatus(ctx context.Context, componentID, version, status string) error

	// ---- 产物 ----

	// PutArtifacts 覆盖写入某个版本的产物元数据（发布时一次写全）。
	PutArtifacts(ctx context.Context, componentID, version string, artifacts []model.ArtifactRecord) error
	// ListArtifacts 列出某个版本的产物。
	ListArtifacts(ctx context.Context, componentID, version string) ([]model.ArtifactRecord, error)
	// GetArtifact 按产物 ID 查询，不存在时返回 ErrNotFound。
	GetArtifact(ctx context.Context, componentID, version, artifactID string) (*model.ArtifactRecord, error)
	// MarkArtifactUploaded 记录某个产物文件已上传到对象存储。
	MarkArtifactUploaded(ctx context.Context, componentID, version, artifactID, file string) error

	// ---- 访问策略 ----

	// ReplaceAccessPolicies 覆盖某个组件的访问策略（007 §9.4）。
	ReplaceAccessPolicies(ctx context.Context, componentID string, policies []model.AccessPolicy) error
	// ListAccessPolicies 查询访问策略。
	ListAccessPolicies(ctx context.Context, componentID string) ([]model.AccessPolicy, error)

	// ---- 组织（007 §9.5）----

	// CreateOrganization 创建组织；ID 已存在时返回 ErrConflict。
	CreateOrganization(ctx context.Context, o *model.Organization) error
	// GetOrganization 按 ID 查询，不存在时返回 ErrNotFound。
	GetOrganization(ctx context.Context, orgID string) (*model.Organization, error)
	// ListOrganizations 列出全部组织，按创建时间排序。
	ListOrganizations(ctx context.Context) ([]model.Organization, error)
	// SetUserOrg 设置用户所属组织（组织成员关系的唯一写入口）。
	SetUserOrg(ctx context.Context, userID, orgID string) error

	// ---- 用户与令牌 ----

	// CreateUser 注册用户；用户名已存在时返回 ErrConflict。
	CreateUser(ctx context.Context, u *model.User) error
	// GetUserByUsername 按用户名查询。
	GetUserByUsername(ctx context.Context, username string) (*model.User, error)
	// GetUserByID 按用户 ID 查询。
	GetUserByID(ctx context.Context, userID string) (*model.User, error)
	// SetUserAdmin 设置管理员标记（运维指南 §6.5 启动引导用）。
	SetUserAdmin(ctx context.Context, userID string, isAdmin bool) error
	// SetUserPassword 更新口令哈希（运维指南 §9 Q5 的重置路径）。
	SetUserPassword(ctx context.Context, userID, passwordHash string) error
	// CreateToken 保存签发的访问令牌。
	CreateToken(ctx context.Context, t *model.Token) error
	// GetToken 按令牌串查询，不存在时返回 ErrNotFound。
	GetToken(ctx context.Context, token string) (*model.Token, error)
	// DeleteToken 注销令牌。
	DeleteToken(ctx context.Context, token string) error
	// DeleteTokensOfUser 吊销某个用户的全部令牌（改口令时用）。
	DeleteTokensOfUser(ctx context.Context, userID string) error

	// ---- 审计 ----

	// AppendAudit 追加一条审计日志。审计只能追加，不能修改或删除（007 §16.3）。
	AppendAudit(ctx context.Context, e *model.AuditEntry) error
	// ListAudit 查询审计日志，按时间倒序。
	ListAudit(ctx context.Context, q AuditQuery) ([]model.AuditEntry, error)

	// Close 释放底层资源。
	Close() error
}
