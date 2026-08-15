package main

import (
	"context"
	"database/sql"

	"github.com/lib/pq"
	_ "github.com/lib/pq"
)

// postgresStore 是 PostgreSQL 实现。
//
// 连接信息全部来自平台注入的 DATABASE_* 环境变量（006 §5.1）。
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

func (p *postgresStore) migrate(ctx context.Context, componentID string) error {
	return applyMigrations(ctx, p.db, componentID, embeddedMigrations())
}

func (p *postgresStore) rollback(ctx context.Context, componentID string, count int) error {
	return rollbackMigrations(ctx, p.db, componentID, embeddedMigrations(), count)
}

// dropAll 清空数据。只给测试用。
func (p *postgresStore) dropAll(ctx context.Context) error {
	_, err := p.db.ExecContext(ctx,
		`DROP TABLE IF EXISTS role_grants, role_permissions, roles, schema_migrations`)
	return err
}

// Roles 一次查出全部角色及其权限。
//
// 用 array_agg 在库里聚合而不是查两张表回来自己拼：角色数量很少，
// 一次往返比 N+1 次查询划算得多，而且"角色的权限"这个语义只有一份实现。
func (p *postgresStore) Roles(ctx context.Context) ([]Role, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT r.id, r.name,
		       COALESCE(ARRAY_AGG(rp.permission ORDER BY rp.permission)
		                FILTER (WHERE rp.permission IS NOT NULL), '{}') AS permissions
		FROM roles r
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		GROUP BY r.id, r.name
		ORDER BY r.id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Name, pq.Array(&r.Permissions)); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GrantsFor 一次查出授予该人员、以及授予其所在部门的全部授权。
//
// 两者一次查完而不是分两次：授权判断是最高频的调用，少一次往返
// 就是每个请求都少一次。
func (p *postgresStore) GrantsFor(ctx context.Context, personID, departmentID string) ([]Grant, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT role_id, subject_type, subject_id
		FROM role_grants
		WHERE (subject_type = $1 AND subject_id = $2)
		   OR ($4 <> '' AND subject_type = $3 AND subject_id = $4)
		ORDER BY role_id`,
		SubjectPerson, personID, SubjectDepartment, departmentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Grant
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.RoleID, &g.SubjectType, &g.SubjectID); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
