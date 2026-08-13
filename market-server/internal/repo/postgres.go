package repo

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/brickkit/market-server/internal/model"
)

// schemaSQL 是库表定义（007 §10）。跟着二进制走，部署时不用额外分发 SQL 文件。
//
//go:embed schema.sql
var schemaSQL string

// PostgreSQL 的唯一键冲突错误码。
const pgUniqueViolation = "23505"

// Postgres 是仓储的 PostgreSQL 实现。
type Postgres struct {
	db *sql.DB
}

// NewPostgres 连接数据库。dsn 形如
// postgres://user:pass@host:5432/brickkit_market?sslmode=disable
func NewPostgres(dsn string) (*Postgres, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Postgres{db: db}, nil
}

// Migrate 建表。幂等：启动时执行一次即可，重复执行不报错。
func (p *Postgres) Migrate(ctx context.Context) error {
	_, err := p.db.ExecContext(ctx, schemaSQL)
	return err
}

// TruncateAll 清空全部数据。只给测试用——生产环境没有任何地方调用它。
func (p *Postgres) TruncateAll(ctx context.Context) error {
	_, err := p.db.ExecContext(ctx, `TRUNCATE
		components, component_tags, component_versions, artifacts,
		access_policies, users, tokens, audit_logs, download_records
		RESTART IDENTITY CASCADE`)
	return err
}

// Close 关闭连接池。
func (p *Postgres) Close() error { return p.db.Close() }

// ============================================================
// 组件
// ============================================================

func (p *Postgres) UpsertComponent(ctx context.Context, c *model.Component) error {
	return p.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO components
				(component_id, name, description, vendor, visibility, source_type, git_url, status, owner_id, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9, NOW())
			ON CONFLICT (component_id) DO UPDATE SET
				name = EXCLUDED.name,
				description = EXCLUDED.description,
				vendor = EXCLUDED.vendor,
				visibility = EXCLUDED.visibility,
				source_type = EXCLUDED.source_type,
				git_url = EXCLUDED.git_url,
				status = EXCLUDED.status,
				owner_id = EXCLUDED.owner_id,
				updated_at = NOW()`,
			c.ComponentID, c.Name, c.Description, c.Vendor, c.Visibility,
			c.SourceType, c.GitURL, c.Status, c.OwnerID)
		if err != nil {
			return err
		}

		// 标签是覆盖写，不是追加
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM component_tags WHERE component_id = $1`, c.ComponentID); err != nil {
			return err
		}
		for _, tag := range c.Tags {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO component_tags (component_id, tag) VALUES ($1,$2)
				 ON CONFLICT DO NOTHING`, c.ComponentID, tag); err != nil {
				return err
			}
		}
		return nil
	})
}

const componentColumns = `component_id, name, COALESCE(description,''), COALESCE(vendor,''),
	visibility, source_type, COALESCE(git_url,''), status, owner_id, downloads, created_at, updated_at`

func (p *Postgres) GetComponent(ctx context.Context, componentID string) (*model.Component, error) {
	row := p.db.QueryRowContext(ctx,
		`SELECT `+componentColumns+` FROM components WHERE component_id = $1`, componentID)

	c, err := scanComponent(row)
	if err != nil {
		return nil, err
	}
	tags, err := p.tagsOf(ctx, componentID)
	if err != nil {
		return nil, err
	}
	c.Tags = tags
	return c, nil
}

func (p *Postgres) tagsOf(ctx context.Context, componentID string) ([]string, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT tag FROM component_tags WHERE component_id = $1 ORDER BY tag`, componentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func (p *Postgres) ListComponents(ctx context.Context, q ComponentQuery) ([]model.Component, error) {
	var (
		where []string
		args  []any
	)
	add := func(clause string, values ...any) {
		where = append(where, clause)
		args = append(args, values...)
	}

	if !q.IncludeBlocked {
		add(`status <> '` + model.ComponentBlocked + `'`)
	}
	if len(q.Visibilities) > 0 {
		args = append(args, pq.Array(q.Visibilities), pq.Array(q.AlsoInclude))
		where = append(where, `(visibility = ANY($`+strconv.Itoa(len(args)-1)+
			`) OR component_id = ANY($`+strconv.Itoa(len(args))+`))`)
	}
	if q.Keyword != "" {
		args = append(args, "%"+strings.ToLower(q.Keyword)+"%")
		i := strconv.Itoa(len(args))
		where = append(where, `(LOWER(component_id) LIKE $`+i+
			` OR LOWER(name) LIKE $`+i+` OR LOWER(COALESCE(description,'')) LIKE $`+i+`)`)
	}
	for _, tag := range q.Tags {
		args = append(args, tag)
		where = append(where,
			`EXISTS (SELECT 1 FROM component_tags t WHERE t.component_id = components.component_id AND t.tag = $`+
				strconv.Itoa(len(args))+`)`)
	}

	query := `SELECT ` + componentColumns + ` FROM components`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	query += ` ORDER BY component_id`

	pageSize := q.PageSize
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	page := q.Page
	if page <= 0 {
		page = 1
	}
	args = append(args, pageSize, (page-1)*pageSize)
	query += ` LIMIT $` + strconv.Itoa(len(args)-1) + ` OFFSET $` + strconv.Itoa(len(args))

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []model.Component
	for rows.Next() {
		c, err := scanComponent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range out {
		tags, err := p.tagsOf(ctx, out[i].ComponentID)
		if err != nil {
			return nil, err
		}
		out[i].Tags = tags
	}
	return out, nil
}

func (p *Postgres) SetVisibility(ctx context.Context, componentID, visibility string) error {
	return p.exec1(ctx,
		`UPDATE components SET visibility = $2, updated_at = NOW() WHERE component_id = $1`,
		componentID, visibility)
}

func (p *Postgres) SetComponentStatus(ctx context.Context, componentID, status string) error {
	return p.exec1(ctx,
		`UPDATE components SET status = $2, updated_at = NOW() WHERE component_id = $1`,
		componentID, status)
}

func (p *Postgres) AddDownload(ctx context.Context, componentID string, delta int64) error {
	return p.exec1(ctx,
		`UPDATE components SET downloads = downloads + $2 WHERE component_id = $1`,
		componentID, delta)
}

// ============================================================
// 版本
// ============================================================

func (p *Postgres) CreateVersion(ctx context.Context, v *model.Version) error {
	publishedAt := v.PublishedAt
	if publishedAt.IsZero() {
		publishedAt = time.Now().UTC()
		v.PublishedAt = publishedAt
	}

	_, err := p.db.ExecContext(ctx, `
		INSERT INTO component_versions
			(component_id, version, status, manifest_json, changelog, published_at, published_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		v.ComponentID, v.Version, v.Status, []byte(v.Manifest), v.Changelog, publishedAt, v.PublishedBy)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

func (p *Postgres) GetVersion(ctx context.Context, componentID, version string) (*model.Version, error) {
	row := p.db.QueryRowContext(ctx, `
		SELECT component_id, version, status, manifest_json, COALESCE(changelog,''),
		       published_at, COALESCE(published_by,'')
		FROM component_versions WHERE component_id = $1 AND version = $2`, componentID, version)
	return scanVersion(row)
}

func (p *Postgres) ListVersions(ctx context.Context, componentID string) ([]model.Version, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT component_id, version, status, manifest_json, COALESCE(changelog,''),
		       published_at, COALESCE(published_by,'')
		FROM component_versions WHERE component_id = $1`, componentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []model.Version
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 版本号排序交给 Go：SQL 里按字符串排会把 1.10.0 排在 1.2.0 前面
	sortVersionsDesc(out)
	return out, nil
}

func (p *Postgres) SetVersionStatus(ctx context.Context, componentID, version, status string) error {
	return p.exec1(ctx,
		`UPDATE component_versions SET status = $3 WHERE component_id = $1 AND version = $2`,
		componentID, version, status)
}

// ============================================================
// 产物
// ============================================================

func (p *Postgres) PutArtifacts(ctx context.Context, componentID, version string, artifacts []model.ArtifactRecord) error {
	return p.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM artifacts WHERE component_id = $1 AND version = $2`,
			componentID, version); err != nil {
			return err
		}
		for i, a := range artifacts {
			files, err := json.Marshal(a.Files)
			if err != nil {
				return err
			}
			uploaded, err := json.Marshal(a.Uploaded)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO artifacts
					(component_id, version, artifact_id, ordinal, type, format, description, reference, file_list, uploaded_files)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
				componentID, version, a.ArtifactID, i, a.Type, a.Format, a.Description, a.Reference,
				files, uploaded); err != nil {
				return err
			}
		}
		return nil
	})
}

const artifactColumns = `component_id, version, artifact_id, type, COALESCE(format,''),
	COALESCE(description,''), COALESCE(reference,''), file_list, uploaded_files`

func (p *Postgres) ListArtifacts(ctx context.Context, componentID, version string) ([]model.ArtifactRecord, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT `+artifactColumns+` FROM artifacts
		 WHERE component_id = $1 AND version = $2 ORDER BY ordinal`, componentID, version)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []model.ArtifactRecord
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (p *Postgres) GetArtifact(ctx context.Context, componentID, version, artifactID string) (*model.ArtifactRecord, error) {
	row := p.db.QueryRowContext(ctx,
		`SELECT `+artifactColumns+` FROM artifacts
		 WHERE component_id = $1 AND version = $2 AND artifact_id = $3`,
		componentID, version, artifactID)
	return scanArtifact(row)
}

func (p *Postgres) MarkArtifactUploaded(ctx context.Context, componentID, version, artifactID, file string) error {
	current, err := p.GetArtifact(ctx, componentID, version, artifactID)
	if err != nil {
		return err
	}
	if contains(current.Uploaded, file) {
		return nil
	}
	uploaded, err := json.Marshal(append(current.Uploaded, file))
	if err != nil {
		return err
	}
	return p.exec1(ctx,
		`UPDATE artifacts SET uploaded_files = $4
		 WHERE component_id = $1 AND version = $2 AND artifact_id = $3`,
		componentID, version, artifactID, uploaded)
}

// ============================================================
// 访问策略
// ============================================================

func (p *Postgres) ReplaceAccessPolicies(ctx context.Context, componentID string, policies []model.AccessPolicy) error {
	return p.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM access_policies WHERE component_id = $1`, componentID); err != nil {
			return err
		}
		for _, policy := range policies {
			permission := policy.Permission
			if permission == "" {
				permission = "read"
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO access_policies (component_id, target_type, target_id, permission)
				VALUES ($1,$2,$3,$4)`,
				componentID, policy.TargetType, policy.TargetID, permission); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p *Postgres) ListAccessPolicies(ctx context.Context, componentID string) ([]model.AccessPolicy, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT component_id, target_type, target_id, permission
		 FROM access_policies WHERE component_id = $1 ORDER BY policy_id`, componentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []model.AccessPolicy
	for rows.Next() {
		var policy model.AccessPolicy
		if err := rows.Scan(&policy.ComponentID, &policy.TargetType, &policy.TargetID, &policy.Permission); err != nil {
			return nil, err
		}
		out = append(out, policy)
	}
	return out, rows.Err()
}

// ============================================================
// 用户与令牌
// ============================================================

func (p *Postgres) CreateUser(ctx context.Context, u *model.User) error {
	createdAt := u.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
		u.CreatedAt = createdAt
	}
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO users (user_id, username, email, password_hash, org_id, is_admin, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		u.UserID, u.Username, u.Email, u.PasswordHash, u.OrgID, u.IsAdmin, createdAt)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

const userColumns = `user_id, username, COALESCE(email,''), password_hash, COALESCE(org_id,''), is_admin, created_at`

func (p *Postgres) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	return scanUser(p.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE username = $1`, username))
}

func (p *Postgres) GetUserByID(ctx context.Context, userID string) (*model.User, error) {
	return scanUser(p.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE user_id = $1`, userID))
}

func (p *Postgres) CreateToken(ctx context.Context, t *model.Token) error {
	createdAt := t.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
		t.CreatedAt = createdAt
	}
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO tokens (token, user_id, username, expires_at, created_at)
		VALUES ($1,$2,$3,$4,$5)`,
		t.Token, t.UserID, t.Username, t.ExpiresAt, createdAt)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

func (p *Postgres) GetToken(ctx context.Context, token string) (*model.Token, error) {
	row := p.db.QueryRowContext(ctx,
		`SELECT token, user_id, username, expires_at, created_at FROM tokens WHERE token = $1`, token)

	var t model.Token
	err := row.Scan(&t.Token, &t.UserID, &t.Username, &t.ExpiresAt, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	t.ExpiresAt = t.ExpiresAt.UTC()
	t.CreatedAt = t.CreatedAt.UTC()
	return &t, nil
}

// DeleteToken 是幂等的：删不存在的令牌不算错误。
func (p *Postgres) DeleteToken(ctx context.Context, token string) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM tokens WHERE token = $1`, token)
	return err
}

// ============================================================
// 审计
// ============================================================

func (p *Postgres) AppendAudit(ctx context.Context, e *model.AuditEntry) error {
	at := e.Time
	if at.IsZero() {
		at = time.Now().UTC()
		e.Time = at
	}
	var id int64
	err := p.db.QueryRowContext(ctx, `
		INSERT INTO audit_logs (action, component_id, version, operator, result, detail, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING audit_id`,
		e.Action, nullable(e.ComponentID), nullable(e.Version), e.Operator, e.Result,
		nullable(e.Detail), at).Scan(&id)
	if err != nil {
		return err
	}
	e.AuditID = "audit-" + strconv.FormatInt(id, 10)
	return nil
}

func (p *Postgres) ListAudit(ctx context.Context, q AuditQuery) ([]model.AuditEntry, error) {
	var (
		where []string
		args  []any
	)
	if q.ComponentID != "" {
		args = append(args, q.ComponentID)
		where = append(where, `component_id = $`+strconv.Itoa(len(args)))
	}
	if q.Action != "" {
		args = append(args, q.Action)
		where = append(where, `action = $`+strconv.Itoa(len(args)))
	}

	query := `SELECT audit_id, action, COALESCE(component_id,''), COALESCE(version,''),
	                 operator, result, COALESCE(detail,''), created_at FROM audit_logs`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	query += ` ORDER BY audit_id DESC`
	if q.Limit > 0 {
		args = append(args, q.Limit)
		query += ` LIMIT $` + strconv.Itoa(len(args))
	}

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []model.AuditEntry
	for rows.Next() {
		var (
			e  model.AuditEntry
			id int64
		)
		if err := rows.Scan(&id, &e.Action, &e.ComponentID, &e.Version,
			&e.Operator, &e.Result, &e.Detail, &e.Time); err != nil {
			return nil, err
		}
		e.AuditID = "audit-" + strconv.FormatInt(id, 10)
		e.Time = e.Time.UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

// ============================================================
// 辅助
// ============================================================

// scanner 让单行与多行查询共用同一套扫描代码。
type scanner interface {
	Scan(dest ...any) error
}

func scanComponent(s scanner) (*model.Component, error) {
	var c model.Component
	err := s.Scan(&c.ComponentID, &c.Name, &c.Description, &c.Vendor, &c.Visibility,
		&c.SourceType, &c.GitURL, &c.Status, &c.OwnerID, &c.Downloads, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.CreatedAt, c.UpdatedAt = c.CreatedAt.UTC(), c.UpdatedAt.UTC()
	return &c, nil
}

func scanVersion(s scanner) (*model.Version, error) {
	var (
		v        model.Version
		manifest []byte
	)
	err := s.Scan(&v.ComponentID, &v.Version, &v.Status, &manifest,
		&v.Changelog, &v.PublishedAt, &v.PublishedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.Manifest = json.RawMessage(manifest)
	v.PublishedAt = v.PublishedAt.UTC()
	return &v, nil
}

func scanArtifact(s scanner) (*model.ArtifactRecord, error) {
	var (
		a              model.ArtifactRecord
		files, uploads []byte
	)
	err := s.Scan(&a.ComponentID, &a.Version, &a.ArtifactID, &a.Type, &a.Format,
		&a.Description, &a.Reference, &files, &uploads)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := unmarshalStrings(files, &a.Files); err != nil {
		return nil, err
	}
	if err := unmarshalStrings(uploads, &a.Uploaded); err != nil {
		return nil, err
	}
	return &a, nil
}

func scanUser(s scanner) (*model.User, error) {
	var u model.User
	err := s.Scan(&u.UserID, &u.Username, &u.Email, &u.PasswordHash, &u.OrgID, &u.IsAdmin, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.CreatedAt = u.CreatedAt.UTC()
	return &u, nil
}

func unmarshalStrings(raw []byte, dst *[]string) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

// exec1 执行一条更新语句，影响行数为 0 时返回 ErrNotFound。
func (p *Postgres) exec1(ctx context.Context, query string, args ...any) error {
	res, err := p.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// tx 在事务里执行 fn，出错自动回滚。
func (p *Postgres) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func isUniqueViolation(err error) bool {
	var pgErr *pq.Error
	return errors.As(err, &pgErr) && string(pgErr.Code) == pgUniqueViolation
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
