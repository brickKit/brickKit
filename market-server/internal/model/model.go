// Package model 定义市场的领域模型与对外契约类型。
//
// 设计依据：007 组件市场设计（§9 API、§10 数据模型、§18 发布校验）。
package model

import (
	"encoding/json"
	"time"
)

// 组件来源类型（007 §11.1）。
const (
	// SourceTypeGit 是开源组件：有 Git 仓库，CLI 可以 clone 源码。
	SourceTypeGit = "git"
	// SourceTypeRegistry 是闭源组件：只有镜像与产物，必须提供 API 契约。
	SourceTypeRegistry = "registry"
)

// 组件可见性（007 §5.1）。
const (
	VisibilityPublic  = "public"
	VisibilityPrivate = "private"
)

// 版本状态（007 §6）。
const (
	// VersionDraft 是发布中的版本：产物还没传完，不可安装。
	VersionDraft = "draft"
	// VersionStable 是正式版本。
	VersionStable = "stable"
	// VersionDeprecated 已弃用：可以安装，但要提示风险。
	VersionDeprecated = "deprecated"
	// VersionBlocked 被阻止：不能安装。
	VersionBlocked = "blocked"
	// VersionDeleted 是软删除标记：已发布的版本不做物理删除。
	VersionDeleted = "deleted"
)

// 组件状态。
const (
	ComponentActive  = "active"
	ComponentBlocked = "blocked"
)

// ArtifactTypeContainer 是镜像引用类产物，它用 reference 而不是 files（007 §10.4）。
const ArtifactTypeContainer = "container"

// ArtifactTypeAPIContract 是 API 契约产物：闭源组件提供 API 时必须有它（002 §5.11）。
const ArtifactTypeAPIContract = "api-contract"

// 错误码。与 007 §18 的响应示例保持一致。
const (
	CodeManifestInvalid                = "MANIFEST_INVALID"
	CodeReservedVariableConflict       = "CONFIG_SCHEMA_RESERVED_VARIABLE_CONFLICT"
	CodeClosedSourceMissingAPIContract = "CLOSED_SOURCE_MISSING_API_CONTRACT"
	CodeVersionExists                  = "VERSION_ALREADY_EXISTS"
	CodeNotFound                       = "NOT_FOUND"
	CodeUnauthorized                   = "UNAUTHORIZED"
	CodeForbidden                      = "FORBIDDEN"
	CodeComponentBlocked               = "COMPONENT_BLOCKED"
	CodeInvalidRequest                 = "INVALID_REQUEST"
	CodeConflict                       = "CONFLICT"
	CodeInternal                       = "INTERNAL"
)

// Problem 是一条字段级校验问题。
type Problem struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// ReservedConflict 是一条保留变量冲突详情（007 §18.1）。
type ReservedConflict struct {
	ConfigKey       string `json:"configKey"`
	EnvVarName      string `json:"envVarName"`
	ConflictPattern string `json:"conflictPattern"`
	Suggestion      string `json:"suggestion"`
}

// APIError 是市场对外的统一错误结构（007 §18 的 error 对象）。
type APIError struct {
	// Code 是机器可读的错误码。
	Code string `json:"code"`
	// Message 是一句话说明。
	Message string `json:"message"`
	// Details 承载结构化详情（problems / conflicts / componentId ...）。
	Details map[string]any `json:"details,omitempty"`
	// Status 是建议的 HTTP 状态码，0 表示由调用方按错误码决定。
	Status int `json:"-"`
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// WithDetail 追加一条详情。
func (e *APIError) WithDetail(key string, value any) *APIError {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	e.Details[key] = value
	return e
}

// Errorf 构造一个 APIError。
func Errorf(code, message string) *APIError {
	return &APIError{Code: code, Message: message}
}

// ============================================================
// 请求 / 响应契约
// ============================================================

// PublishRequest 是发布新版本的请求体（007 §3.7）。
type PublishRequest struct {
	Version    string          `json:"version"`
	Status     string          `json:"status,omitempty"`
	Manifest   json.RawMessage `json:"manifest"`
	SourceType string          `json:"sourceType"`
	GitURL     string          `json:"gitUrl,omitempty"`
	Changelog  string          `json:"changelog,omitempty"`
	Visibility string          `json:"visibility,omitempty"`
}

// Component 是组件记录（007 §10.1）。
type Component struct {
	ComponentID string    `json:"componentId"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Vendor      string    `json:"vendor,omitempty"`
	Visibility  string    `json:"visibility"`
	SourceType  string    `json:"sourceType"`
	GitURL      string    `json:"gitUrl,omitempty"`
	Status      string    `json:"status"`
	OwnerID     string    `json:"ownerId"`
	Tags        []string  `json:"tags,omitempty"`
	Downloads   int64     `json:"downloads"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Version 是组件版本记录（007 §10.3）。
type Version struct {
	ComponentID string          `json:"componentId"`
	Version     string          `json:"version"`
	Status      string          `json:"status"`
	Manifest    json.RawMessage `json:"manifest"`
	Changelog   string          `json:"changelog,omitempty"`
	PublishedAt time.Time       `json:"publishedAt"`
	PublishedBy string          `json:"publishedBy"`
}

// Installable 判断该版本能否被安装（007 §6：blocked 不能安装，deleted 视同不存在）。
func (v *Version) Installable() bool {
	return v.Status == VersionStable || v.Status == VersionDeprecated
}

// ArtifactRecord 是产物记录（007 §10.4）。
type ArtifactRecord struct {
	ArtifactID  string   `json:"id"`
	ComponentID string   `json:"componentId"`
	Version     string   `json:"version"`
	Type        string   `json:"type"`
	Format      string   `json:"format,omitempty"`
	Description string   `json:"description,omitempty"`
	Reference   string   `json:"reference,omitempty"`
	Files       []string `json:"files,omitempty"`
	// Uploaded 记录已经上传到对象存储的文件（相对路径）。
	Uploaded []string `json:"-"`
}

// User 是市场用户（007 §9.5）。
type User struct {
	UserID       string    `json:"userId"`
	Username     string    `json:"username"`
	Email        string    `json:"email,omitempty"`
	PasswordHash string    `json:"-"`
	OrgID        string    `json:"orgId,omitempty"`
	IsAdmin      bool      `json:"isAdmin,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Organization 是一个组织（007 §9.5、§10）。
//
// 组织存在的意义只有一个：把 private 组件按组织授权出去（007 §5.3 的
// allowedOrganizations）。因此**成员关系就是授权本身**，只能由组织所有者
// 或市场管理员建立——绝不能由使用者在注册时自报。
type Organization struct {
	OrgID   string `json:"orgId"`
	Name    string `json:"name"`
	OwnerID string `json:"ownerId"`
	// Members 只在查询单个组织时填充。
	Members   []string  `json:"members,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Token 是登录后签发的访问令牌（007 §9.6 Bearer Token）。
type Token struct {
	Token     string    `json:"token"`
	UserID    string    `json:"userId"`
	Username  string    `json:"username"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

// AccessPolicy 是 private 组件的访问策略（007 §10.5）。
type AccessPolicy struct {
	ComponentID string `json:"componentId"`
	TargetType  string `json:"targetType"` // user / organization
	TargetID    string `json:"targetId"`
	Permission  string `json:"permission"`
}

// 访问策略的目标类型。
const (
	TargetUser         = "user"
	TargetOrganization = "organization"
)

// AuditEntry 是一条审计日志（007 §16.2）。
type AuditEntry struct {
	AuditID     string    `json:"auditId"`
	Action      string    `json:"action"`
	ComponentID string    `json:"componentId,omitempty"`
	Version     string    `json:"version,omitempty"`
	Operator    string    `json:"operator"`
	Time        time.Time `json:"time"`
	Result      string    `json:"result"`
	Detail      string    `json:"detail,omitempty"`
}

// 审计动作（007 §16.1）。
const (
	ActionVersionPublished    = "component.version.published"
	ActionVersionStatus       = "component.version.status_changed"
	ActionVersionDeleted      = "component.version.deleted"
	ActionVisibilityChanged   = "component.visibility_changed"
	ActionAccessChanged       = "component.access_changed"
	ActionArtifactUploaded    = "component.artifact.uploaded"
	ActionArtifactDownload    = "component.artifact.downloaded"
	ActionOrganizationCreated = "organization.created"
	ActionMemberAdded         = "organization.member_added"
	ActionUserRegistered      = "user.registered"
	ActionUserLogin           = "user.login"
)

// 审计结果。
const (
	ResultSuccess = "success"
	ResultFailure = "failure"
)
