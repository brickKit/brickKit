package main

import (
	"context"
	"database/sql"
	"errors"

	_ "github.com/lib/pq"
)

// postgresStore 是 PostgreSQL 实现。
//
// 连接信息全部来自平台注入的 DATABASE_* 环境变量（006 §5.1），
// 组件自己不知道也不关心数据库跑在哪。
type postgresStore struct {
	db *sql.DB
}

func newPostgresStore(dsn string) (*postgresStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &postgresStore{db: db}, nil
}

func (p *postgresStore) Close() error { return p.db.Close() }

// ensureSchema 建表。用 IF NOT EXISTS，重启幂等。
func (p *postgresStore) ensureSchema(ctx context.Context) error {
	_, err := p.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS departments (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			parent_id  TEXT NOT NULL DEFAULT '',
			level      INT  NOT NULL DEFAULT 1
		)`)
	if err != nil {
		return err
	}
	_, err = p.db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_departments_parent ON departments (parent_id)`)
	return err
}

// upsert 写入或更新一个部门。
func (p *postgresStore) upsert(ctx context.Context, d Department) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO departments (id, name, parent_id, level)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE
		SET name = EXCLUDED.name, parent_id = EXCLUDED.parent_id, level = EXCLUDED.level`,
		d.ID, d.Name, d.ParentID, d.Level)
	return err
}

// dropAll 清空数据。只给测试用。
func (p *postgresStore) dropAll(ctx context.Context) error {
	_, err := p.db.ExecContext(ctx, `DROP TABLE IF EXISTS departments`)
	return err
}

func (p *postgresStore) List(ctx context.Context, parentID string) ([]Department, error) {
	query := `SELECT id, name, parent_id, level FROM departments`
	args := []any{}
	if parentID != "" {
		query += ` WHERE parent_id = $1`
		args = append(args, parentID)
	}
	query += ` ORDER BY id`

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return scanDepartments(rows)
}

func (p *postgresStore) Get(ctx context.Context, id string) (Department, error) {
	var d Department
	err := p.db.QueryRowContext(ctx,
		`SELECT id, name, parent_id, level FROM departments WHERE id = $1`, id).
		Scan(&d.ID, &d.Name, &d.ParentID, &d.Level)

	if errors.Is(err, sql.ErrNoRows) {
		return Department{}, ErrNotFound
	}
	return d, err
}

// Subtree 用递归 CTE 一次查完整棵子树。
//
// 在数据库里递归而不是把整表读出来在内存里爬：组织架构可以很大，
// 而且这样"子树"的语义只有一份实现。
func (p *postgresStore) Subtree(ctx context.Context, rootID string) ([]Department, error) {
	if _, err := p.Get(ctx, rootID); err != nil {
		return nil, err
	}

	rows, err := p.db.QueryContext(ctx, `
		WITH RECURSIVE subtree AS (
			SELECT id, name, parent_id, level FROM departments WHERE id = $1
			UNION ALL
			SELECT d.id, d.name, d.parent_id, d.level
			FROM departments d
			JOIN subtree s ON d.parent_id = s.id
		)
		SELECT id, name, parent_id, level FROM subtree ORDER BY id`, rootID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return scanDepartments(rows)
}

func scanDepartments(rows *sql.Rows) ([]Department, error) {
	var out []Department
	for rows.Next() {
		var d Department
		if err := rows.Scan(&d.ID, &d.Name, &d.ParentID, &d.Level); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
