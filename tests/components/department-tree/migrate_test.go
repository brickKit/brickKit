// 本文件测试迁移执行器（开发计划 21.6）。
//
// 迁移是**有版本的 SQL 文件**，不是埋在代码里的建表语句：
// 只有这样，"1.0.0 到 2.0.0 改了什么"才是看得见、可评审、可回溯的。
//
// 需要真实 PostgreSQL（SQL 迁移无法在内存实现上执行），
// 未设置 DEPARTMENT_TEST_DATABASE_URL 时自动跳过。
package main

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
)

const testComponentID = "department/tree"

func testDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("DEPARTMENT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("未设置 DEPARTMENT_TEST_DATABASE_URL，跳过迁移集成测试")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("连接数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// 每个用例从干净的库开始
	if _, err := db.Exec(`DROP TABLE IF EXISTS departments, people, schema_migrations`); err != nil {
		t.Fatalf("清库失败：%v", err)
	}
	return db
}

func appliedVersions(t *testing.T, db *sql.DB, componentID string) []string {
	t.Helper()

	rows, err := db.Query(
		`SELECT version FROM schema_migrations WHERE component_id = $1 ORDER BY version`,
		componentID)
	if err != nil {
		t.Fatalf("查询迁移记录失败：%v", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			t.Fatalf("扫描失败：%v", err)
		}
		out = append(out, version)
	}
	return out
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
		t.Fatalf("统计 %s 失败：%v", table, err)
	}
	return count
}

func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()

	var exists bool
	err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = $1)`, table).Scan(&exists)
	if err != nil {
		t.Fatalf("查询表信息失败：%v", err)
	}
	return exists
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ============================================================
// 21.6 向上迁移
// ============================================================

func TestMigrationsApplyInOrder(t *testing.T) {
	db := testDB(t)

	if err := applyMigrations(context.Background(), db, testComponentID, embeddedMigrations()); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}

	want := []string{"0001_init", "0002_seed_departments"}
	if got := appliedVersions(t, db, testComponentID); !equal(got, want) {
		t.Fatalf("迁移必须按文件名顺序执行：期望 %v，实际 %v", want, got)
	}
	if got := countRows(t, db, "departments"); got != 4 {
		t.Fatalf("期望 4 个初始部门，实际 %d", got)
	}
}

// 容器每次重启都会再跑一遍迁移：第二次必须什么都不做。
func TestMigrationsAreNotReapplied(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	if err := applyMigrations(ctx, db, testComponentID, embeddedMigrations()); err != nil {
		t.Fatalf("首次迁移失败：%v", err)
	}
	if _, err := db.Exec(`UPDATE departments SET name = '研发中心' WHERE id = 'd-tech'`); err != nil {
		t.Fatalf("改数据失败：%v", err)
	}

	if err := applyMigrations(ctx, db, testComponentID, embeddedMigrations()); err != nil {
		t.Fatalf("重复迁移必须成功，实际失败：%v", err)
	}

	if got := len(appliedVersions(t, db, testComponentID)); got != 2 {
		t.Fatalf("重复执行不该新增迁移记录，实际 %d 条", got)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM departments WHERE id = 'd-tech'`).Scan(&name); err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	if name != "研发中心" {
		t.Fatalf("已执行过的迁移不该再跑一遍（它把改过的数据盖回去了）：%s", name)
	}
}

// 新增一个迁移文件时，只执行新的那个 ——
// 这正是"组件升级带来结构变更"的路径（Step 38 升级测试依赖它）。
func TestOnlyNewMigrationsAreApplied(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	base := []migration{{
		Version: "0001_init",
		Up: `CREATE TABLE departments (id TEXT PRIMARY KEY, name TEXT NOT NULL,
			parent_id TEXT NOT NULL DEFAULT '', level INT NOT NULL DEFAULT 1)`,
		Down: `DROP TABLE departments`,
	}}
	if err := applyMigrations(ctx, db, testComponentID, base); err != nil {
		t.Fatalf("首次迁移失败：%v", err)
	}

	upgraded := append(base, migration{
		Version: "0002_add_code",
		Up:      `ALTER TABLE departments ADD COLUMN code TEXT NOT NULL DEFAULT ''`,
		Down:    `ALTER TABLE departments DROP COLUMN code`,
	})
	if err := applyMigrations(ctx, db, testComponentID, upgraded); err != nil {
		t.Fatalf("增量迁移失败：%v", err)
	}

	versions := appliedVersions(t, db, testComponentID)
	if len(versions) != 2 || versions[1] != "0002_add_code" {
		t.Fatalf("期望只补执行 0002_add_code，实际 %v", versions)
	}
}

// 迁移失败要整条回滚，并且**不记录版本**：
// 记了版本下次就跳过，数据库会停在一个半吊子状态上。
func TestFailedMigrationRollsBackAndIsNotRecorded(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	broken := []migration{
		{Version: "0001_init", Up: `CREATE TABLE departments (id TEXT PRIMARY KEY)`},
		{Version: "0002_broken", Up: `CREATE TABLE good (id TEXT); 这不是合法的 SQL;`},
	}

	err := applyMigrations(ctx, db, testComponentID, broken)
	if err == nil {
		t.Fatal("非法 SQL 应导致迁移失败")
	}
	if !strings.Contains(err.Error(), "0002_broken") {
		t.Fatalf("错误里要指出是哪个迁移失败了：%v", err)
	}
	if got := appliedVersions(t, db, testComponentID); len(got) != 1 {
		t.Fatalf("失败的迁移不该被记录，实际：%v", got)
	}
	if tableExists(t, db, "good") {
		t.Fatal("失败的迁移必须整条回滚，不能留下半截结果")
	}
}

// ============================================================
// 多个组件共用一个数据库
// ============================================================

// 两个组件的版本号都从 0001_init 开始 ——
// 迁移记录必须按**组件**隔离，否则先跑的那个会把后跑的顶掉。
//
// 这条是真容器测出来的：把两个组件迁移到同一个库时，
// people/basic 的 0001_init 被当成"已执行"跳过，
// 然后 0002 往一张根本没建出来的表里插数据，直接失败。
func TestMigrationsOfDifferentComponentsDoNotCollide(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	departmentMigrations := []migration{
		{Version: "0001_init", Up: `CREATE TABLE departments (id TEXT PRIMARY KEY)`,
			Down: `DROP TABLE departments`},
	}
	peopleMigrations := []migration{
		{Version: "0001_init", Up: `CREATE TABLE people (id TEXT PRIMARY KEY)`,
			Down: `DROP TABLE people`},
	}

	if err := applyMigrations(ctx, db, "department/tree", departmentMigrations); err != nil {
		t.Fatalf("department/tree 迁移失败：%v", err)
	}
	if err := applyMigrations(ctx, db, "people/basic", peopleMigrations); err != nil {
		t.Fatalf("people/basic 迁移失败（版本号撞车了）：%v", err)
	}

	if !tableExists(t, db, "departments") || !tableExists(t, db, "people") {
		t.Fatal("两个组件的表都应该建出来")
	}
	if got := appliedVersions(t, db, "department/tree"); !equal(got, []string{"0001_init"}) {
		t.Fatalf("department/tree 的迁移记录不对：%v", got)
	}
	if got := appliedVersions(t, db, "people/basic"); !equal(got, []string{"0001_init"}) {
		t.Fatalf("people/basic 的迁移记录不对：%v", got)
	}
}

// 回退只影响自己的组件。
func TestRollbackDoesNotAffectOtherComponents(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	other := []migration{{Version: "0001_init",
		Up: `CREATE TABLE people (id TEXT PRIMARY KEY)`, Down: `DROP TABLE people`}}
	if err := applyMigrations(ctx, db, "people/basic", other); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	if err := applyMigrations(ctx, db, testComponentID, embeddedMigrations()); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}

	if err := rollbackMigrations(ctx, db, testComponentID, embeddedMigrations(), 0); err != nil {
		t.Fatalf("回退失败：%v", err)
	}

	if !tableExists(t, db, "people") {
		t.Fatal("回退 department/tree 不该动到 people/basic 的表")
	}
	if got := appliedVersions(t, db, "people/basic"); len(got) != 1 {
		t.Fatalf("另一个组件的迁移记录不该被删：%v", got)
	}
}

// ============================================================
// 回退（undo / revert）
// ============================================================

// 回退最后一个迁移：数据没了，但表还在。
func TestRollbackLastMigration(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := applyMigrations(ctx, db, testComponentID, embeddedMigrations()); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}

	if err := rollbackMigrations(ctx, db, testComponentID, embeddedMigrations(), 1); err != nil {
		t.Fatalf("回退失败：%v", err)
	}

	if got := appliedVersions(t, db, testComponentID); !equal(got, []string{"0001_init"}) {
		t.Fatalf("应只剩 0001_init，实际 %v", got)
	}
	if !tableExists(t, db, "departments") {
		t.Fatal("只回退 0002 的话表应该还在")
	}
	if got := countRows(t, db, "departments"); got != 0 {
		t.Fatalf("0002 的初始数据应已被删除，实际还剩 %d 条", got)
	}
}

// count=0 表示全部回退：库回到干净状态，方便反复测试。
func TestRollbackAllMigrations(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := applyMigrations(ctx, db, testComponentID, embeddedMigrations()); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}

	if err := rollbackMigrations(ctx, db, testComponentID, embeddedMigrations(), 0); err != nil {
		t.Fatalf("全部回退失败：%v", err)
	}

	if got := appliedVersions(t, db, testComponentID); len(got) != 0 {
		t.Fatalf("迁移记录应已清空，实际 %v", got)
	}
	if tableExists(t, db, "departments") {
		t.Fatal("表应已被删除")
	}
}

// 回退按版本**倒序**执行：先撤 0002 再撤 0001。
// 顺序反了会出现"表已经删了，再去删表里的数据"。
func TestRollbackRunsInReverseOrder(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := applyMigrations(ctx, db, testComponentID, embeddedMigrations()); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}

	// 全量回退能成功本身就说明了顺序：若先撤 0001（DROP TABLE），
	// 再撤 0002（DELETE FROM departments）必然失败
	if err := rollbackMigrations(ctx, db, testComponentID, embeddedMigrations(), 0); err != nil {
		t.Fatalf("回退顺序不对：%v", err)
	}
}

// 回退再向上迁移，回到原样 —— 这是"反复搭起来、拆掉"的基础。
func TestMigrateAfterRollbackRestoresEverything(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	if err := applyMigrations(ctx, db, testComponentID, embeddedMigrations()); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	if err := rollbackMigrations(ctx, db, testComponentID, embeddedMigrations(), 0); err != nil {
		t.Fatalf("回退失败：%v", err)
	}
	if err := applyMigrations(ctx, db, testComponentID, embeddedMigrations()); err != nil {
		t.Fatalf("重新迁移失败：%v", err)
	}

	if got := countRows(t, db, "departments"); got != 4 {
		t.Fatalf("重新迁移后数据应回来，实际 %d 条", got)
	}
}

// 没写 down 脚本的迁移不能假装回退成功 ——
// 那会让迁移记录与真实结构对不上，比报错危险得多。
func TestRollbackWithoutDownScriptIsAnError(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	oneWay := []migration{{Version: "0001_init",
		Up: `CREATE TABLE departments (id TEXT PRIMARY KEY)`}} // 没有 Down
	if err := applyMigrations(ctx, db, testComponentID, oneWay); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}

	err := rollbackMigrations(ctx, db, testComponentID, oneWay, 1)

	if err == nil {
		t.Fatal("没有 down 脚本时应报错")
	}
	if !strings.Contains(err.Error(), "0001_init") || !strings.Contains(err.Error(), "down") {
		t.Fatalf("错误要说清是哪个迁移缺 down 脚本：%v", err)
	}
	if got := appliedVersions(t, db, testComponentID); len(got) != 1 {
		t.Fatalf("回退失败时不该删掉迁移记录：%v", got)
	}
}

// 没有可回退的迁移时是空操作，不报错。
func TestRollbackOnEmptyDatabaseIsNoop(t *testing.T) {
	db := testDB(t)

	if err := rollbackMigrations(context.Background(), db, testComponentID,
		embeddedMigrations(), 1); err != nil {
		t.Fatalf("空库回退应是空操作，实际报错：%v", err)
	}
}

// ============================================================
// 迁移文件本身（不需要数据库）
// ============================================================

func TestMigrationsAreEmbedded(t *testing.T) {
	migrations := embeddedMigrations()

	if len(migrations) < 2 {
		t.Fatalf("期望至少两个迁移，实际 %d", len(migrations))
	}
	for _, m := range migrations {
		if strings.TrimSpace(m.Up) == "" {
			t.Fatalf("迁移 %s 缺少 up 脚本", m.Version)
		}
		if strings.TrimSpace(m.Down) == "" {
			t.Fatalf("迁移 %s 缺少 down 脚本（测试与开发要靠它反复重建）", m.Version)
		}
	}
	if migrations[0].Version != "0001_init" {
		t.Fatalf("首个迁移应是 0001_init，实际 %s", migrations[0].Version)
	}
}

func TestMigrationVersionsAreUniqueAndSorted(t *testing.T) {
	migrations := embeddedMigrations()

	seen := map[string]bool{}
	for i, m := range migrations {
		if seen[m.Version] {
			t.Fatalf("迁移版本重复：%s", m.Version)
		}
		seen[m.Version] = true
		if i > 0 && migrations[i-1].Version >= m.Version {
			t.Fatalf("迁移必须按版本递增：%s 出现在 %s 之后", m.Version, migrations[i-1].Version)
		}
	}
}
