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

// migrate 执行尚未执行过的迁移脚本（migrations/*.up.sql）。
func (p *postgresStore) migrate(ctx context.Context, componentID string) error {
	return applyMigrations(ctx, p.db, componentID, embeddedMigrations())
}

// rollback 回退已执行的迁移（migrations/*.down.sql）。count 为 0 表示全部回退。
func (p *postgresStore) rollback(ctx context.Context, componentID string, count int) error {
	return rollbackMigrations(ctx, p.db, componentID, embeddedMigrations(), count)
}

// dropAll 清空数据。只给测试用。
func (p *postgresStore) dropAll(ctx context.Context) error {
	_, err := p.db.ExecContext(ctx, `DROP TABLE IF EXISTS departments, schema_migrations`)
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
