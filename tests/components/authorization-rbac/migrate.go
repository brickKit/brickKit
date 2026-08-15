package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// migrationFiles 是随二进制一起打包的迁移脚本。
//
// 002 §8.4 要求"迁移脚本和主业务代码打包在同一个镜像中"。用 go:embed
// 比拷贝目录更进一步：脚本直接进二进制，不可能出现"镜像里漏了 SQL 文件"。
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

// migration 是一次结构变更。
//
// Version 取自文件名（去掉 .up.sql / .down.sql），它同时是执行顺序与去重依据。
// Down 是回退脚本：**给开发与测试用**，让人能反复把库搭起来、拆掉。
// 生产环境的结构问题应当用一个新的 up 迁移去修（002 §8.9：不做破坏性操作）。
type migration struct {
	Version string
	Up      string
	Down    string
}

// schemaMigrationsTable 记录已执行过的版本。
//
// 主键是 (component_id, version) 而不是 version：**版本号是每个组件各自的**，
// 两个组件都会有 0001_init。这是真容器测出来的问题——共用一个数据库时，
// 先跑的组件会把后跑的顶掉，后者的表根本建不出来。
const schemaMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	component_id TEXT NOT NULL,
	version      TEXT NOT NULL,
	applied_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (component_id, version)
)`

// embeddedMigrations 读出全部迁移脚本，按版本排序。
//
// 文件命名：<版本>.up.sql 与 <版本>.down.sql 成对出现。
func embeddedMigrations() []migration {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		// go:embed 保证了目录存在，走到这里说明构建被人为破坏了
		panic("读取内嵌迁移脚本失败：" + err.Error())
	}

	byVersion := map[string]*migration{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		data, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			panic("读取内嵌迁移脚本失败：" + err.Error())
		}

		version, direction := splitMigrationName(name)
		m, ok := byVersion[version]
		if !ok {
			m = &migration{Version: version}
			byVersion[version] = m
		}
		if direction == "down" {
			m.Down = string(data)
		} else {
			m.Up = string(data)
		}
	}

	out := make([]migration, 0, len(byVersion))
	for _, m := range byVersion {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out
}

// splitMigrationName 把 0001_init.up.sql 拆成 ("0001_init", "up")。
func splitMigrationName(name string) (version, direction string) {
	base := strings.TrimSuffix(name, ".sql")
	if idx := strings.LastIndex(base, "."); idx >= 0 {
		return base[:idx], base[idx+1:]
	}
	return base, "up"
}

// applyMigrations 按顺序执行尚未执行过的迁移（002 §8）。
//
// 三条不变量：
//   - **幂等**：已执行过的版本不会再跑一遍，因此容器重启是安全的；
//   - **原子**：每个迁移连同它的版本记录在同一个事务里，失败整条回滚；
//   - **有序**：按版本号从小到大，且失败即中止，不跳过继续。
func applyMigrations(ctx context.Context, db *sql.DB, componentID string, migrations []migration) error {
	if err := ensureMigrationsTable(ctx, db); err != nil {
		return err
	}
	warnIfDatabaseIsShared(ctx, db, componentID)

	applied, err := appliedSet(ctx, db, componentID)
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if applied[m.Version] {
			continue
		}
		err := inTx(ctx, db, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, m.Up); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx,
				`INSERT INTO schema_migrations (component_id, version) VALUES ($1, $2)`,
				componentID, m.Version)
			return err
		})
		if err != nil {
			return fmt.Errorf("执行迁移 %s 失败：%w", m.Version, err)
		}
	}
	return nil
}

// rollbackMigrations 回退已执行的迁移（undo）。
//
// count 为回退几个，0 表示全部回退。按版本**倒序**执行：
// 顺序反了会出现"表已经删了，再去删表里的数据"。
//
// 这是给开发与测试用的：反复把库搭起来、拆掉。生产环境请用新的 up 迁移修问题。
func rollbackMigrations(
	ctx context.Context, db *sql.DB, componentID string, migrations []migration, count int,
) error {
	if err := ensureMigrationsTable(ctx, db); err != nil {
		return err
	}

	applied, err := appliedSet(ctx, db, componentID)
	if err != nil {
		return err
	}

	// 从最新的往回撤
	pending := make([]migration, 0, len(migrations))
	for i := len(migrations) - 1; i >= 0; i-- {
		if applied[migrations[i].Version] {
			pending = append(pending, migrations[i])
		}
	}
	if count > 0 && count < len(pending) {
		pending = pending[:count]
	}

	for _, m := range pending {
		if strings.TrimSpace(m.Down) == "" {
			// 假装回退成功会让迁移记录与真实结构对不上，比报错危险得多
			return fmt.Errorf("迁移 %s 没有提供 down 脚本，无法回退", m.Version)
		}
		err := inTx(ctx, db, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, m.Down); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx,
				`DELETE FROM schema_migrations WHERE component_id = $1 AND version = $2`,
				componentID, m.Version)
			return err
		})
		if err != nil {
			return fmt.Errorf("回退迁移 %s 失败：%w", m.Version, err)
		}
	}
	return nil
}

func ensureMigrationsTable(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schemaMigrationsTable); err != nil {
		return fmt.Errorf("创建 schema_migrations 表失败：%w", err)
	}
	return nil
}

// warnIfDatabaseIsShared 在库里发现别的组件的迁移记录时提醒一句。
//
// 不阻断：共用一个库在本地调试时确实方便，而且按组件隔离之后也不会再互相顶掉。
// 但 002 §2.2 的数据自治要求每个组件有自己的库——共用意味着一个组件能读到
// 另一个组件的表。
func warnIfDatabaseIsShared(ctx context.Context, db *sql.DB, componentID string) {
	rows, err := db.QueryContext(ctx,
		`SELECT DISTINCT component_id FROM schema_migrations WHERE component_id <> $1`, componentID)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()

	var others []string
	for rows.Next() {
		var other string
		if err := rows.Scan(&other); err == nil {
			others = append(others, other)
		}
	}
	if len(others) > 0 {
		slog.Warn("该数据库里还有其他组件的表",
			"others", strings.Join(others, ", "),
			"建议", "002 §2.2 数据自治：每个组件用自己的数据库，见组件 README")
	}
}

func appliedSet(ctx context.Context, db *sql.DB, componentID string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT version FROM schema_migrations WHERE component_id = $1`, componentID)
	if err != nil {
		return nil, fmt.Errorf("查询已执行的迁移失败：%w", err)
	}
	defer func() { _ = rows.Close() }()

	applied := map[string]bool{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = true
	}
	return applied, rows.Err()
}

// inTx 在事务里执行 fn。
//
// 迁移与它的版本记录必须同事务：先执行后记录会在两者之间留下一个窗口，
// 进程正好在这里挂掉的话，下次启动会把这个迁移再跑一遍。
func inTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
