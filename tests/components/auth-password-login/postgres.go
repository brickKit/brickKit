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
	_, err := p.db.ExecContext(ctx, `DROP TABLE IF EXISTS credentials, schema_migrations`)
	return err
}

func (p *postgresStore) GetByUsername(ctx context.Context, username string) (Credential, error) {
	var c Credential
	err := p.db.QueryRowContext(ctx,
		`SELECT username, person_id, password_hash FROM credentials WHERE username = $1`, username).
		Scan(&c.Username, &c.PersonID, &c.PasswordHash)

	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, ErrCredentialNotFound
	}
	return c, err
}

// Upsert 写入或更新一条凭据（改密码、初始化账号用）。
func (p *postgresStore) Upsert(ctx context.Context, c Credential) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO credentials (username, person_id, password_hash)
		VALUES ($1, $2, $3)
		ON CONFLICT (username) DO UPDATE
		SET person_id     = EXCLUDED.person_id,
		    password_hash = EXCLUDED.password_hash,
		    updated_at    = NOW()`,
		c.Username, c.PersonID, c.PasswordHash)
	return err
}
