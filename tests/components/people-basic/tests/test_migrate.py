"""迁移执行器的测试（开发计划 22.5）。

迁移是**有版本的 SQL 文件**，不是埋在代码里的建表语句。
文件加载与排序不需要数据库；实际执行需要真实 PostgreSQL，
未设置 PEOPLE_TEST_DATABASE_URL 时自动跳过。
"""

from __future__ import annotations

import os
import pathlib

import psycopg
import pytest

from app.migrate import Migration, apply_migrations, load_migrations, rollback_migrations

ROOT = pathlib.Path(__file__).resolve().parent.parent


# ============================================================
# 迁移文件本身（不需要数据库）
# ============================================================


def test_migrations_are_shipped_with_the_component() -> None:
    """002 §8.4：迁移脚本和业务代码打包在同一个镜像里。

    这条测试跑在容器内，因此它同时验证了"SQL 文件真的被 COPY 进镜像了"——
    靠"记得改 Dockerfile"是不可靠的。
    """
    migrations = load_migrations()

    assert len(migrations) >= 2, f"期望至少两个迁移文件，实际 {len(migrations)}"
    assert migrations[0].version == "0001_init"
    for migration in migrations:
        assert migration.up.strip(), f"迁移 {migration.version} 内容为空"


def test_migration_versions_are_unique_and_sorted() -> None:
    migrations = load_migrations()
    versions = [m.version for m in migrations]

    assert len(set(versions)) == len(versions), f"迁移版本重复：{versions}"
    assert versions == sorted(versions), f"迁移必须按版本递增：{versions}"


def test_missing_migrations_directory_is_an_error() -> None:
    """目录不在就明确报错，而不是"没有迁移"这种静默通过。"""
    with pytest.raises(FileNotFoundError):
        load_migrations(ROOT / "does-not-exist")


# ============================================================
# 实际执行（需要真实 PostgreSQL）
# ============================================================


@pytest.fixture()
def conn():
    dsn = os.environ.get("PEOPLE_TEST_DATABASE_URL")
    if not dsn:
        pytest.skip("未设置 PEOPLE_TEST_DATABASE_URL，跳过迁移集成测试")

    connection = psycopg.connect(dsn, autocommit=True)
    with connection.cursor() as cur:
        cur.execute("DROP TABLE IF EXISTS people, departments, schema_migrations")
    try:
        yield connection
    finally:
        connection.close()


COMPONENT = "people/basic"


def applied(conn: psycopg.Connection, component_id: str = COMPONENT) -> list[str]:
    with conn.cursor() as cur:
        cur.execute(
            "SELECT version FROM schema_migrations WHERE component_id = %s ORDER BY version",
            (component_id,),
        )
        return [row[0] for row in cur.fetchall()]


def table_exists(conn: psycopg.Connection, table: str) -> bool:
    with conn.cursor() as cur:
        cur.execute(
            """SELECT EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = 'public' AND table_name = %s)""",
            (table,),
        )
        return cur.fetchone()[0]


def count(conn: psycopg.Connection, table: str) -> int:
    with conn.cursor() as cur:
        cur.execute(f"SELECT COUNT(*) FROM {table}")
        return cur.fetchone()[0]


def test_migrations_apply_in_order(conn) -> None:
    executed = apply_migrations(conn, COMPONENT, load_migrations())

    assert executed == ["0001_init", "0002_seed_people"]
    assert applied(conn) == ["0001_init", "0002_seed_people"]
    assert count(conn, "people") == 4


def test_migrations_are_not_reapplied(conn) -> None:
    """容器每次重启都会再跑一遍迁移：第二次必须什么都不做。"""
    apply_migrations(conn, COMPONENT, load_migrations())
    with conn.cursor() as cur:
        cur.execute("UPDATE people SET name = '张三丰' WHERE id = 'p-001'")

    executed = apply_migrations(conn, COMPONENT, load_migrations())

    assert executed == [], "重复执行不该再跑任何迁移"
    assert count(conn, "people") == 4

    with conn.cursor() as cur:
        cur.execute("SELECT name FROM people WHERE id = 'p-001'")
        assert cur.fetchone()[0] == "张三丰", "已执行过的迁移不该把改过的数据盖回去"


def test_only_new_migrations_are_applied(conn) -> None:
    """新增迁移文件时只执行新的那个——组件升级带来结构变更的路径。"""
    base = [Migration(version="0001_init", up="CREATE TABLE people (id TEXT PRIMARY KEY)")]
    apply_migrations(conn, COMPONENT, base)

    upgraded = base + [
        Migration(version="0002_add_email", up="ALTER TABLE people ADD COLUMN email TEXT")
    ]
    executed = apply_migrations(conn, COMPONENT, upgraded)

    assert executed == ["0002_add_email"]
    with conn.cursor() as cur:
        cur.execute(
            """SELECT EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'people' AND column_name = 'email')"""
        )
        assert cur.fetchone()[0], "新迁移应已加上 email 列"


def test_failed_migration_rolls_back_and_is_not_recorded(conn) -> None:
    """失败的迁移要整条回滚且不记录版本。

    记了版本下次就会跳过，数据库会永远停在一个半吊子状态上。
    """
    broken = [
        Migration(version="0001_init", up="CREATE TABLE people (id TEXT PRIMARY KEY)"),
        Migration(version="0002_broken", up="CREATE TABLE good (id TEXT); 这不是合法的 SQL;"),
    ]

    with pytest.raises(RuntimeError) as excinfo:
        apply_migrations(conn, COMPONENT, broken)

    assert "0002_broken" in str(excinfo.value), "错误里要指出是哪个迁移失败了"
    assert applied(conn) == ["0001_init"], "失败的迁移不该被记录"

    with conn.cursor() as cur:
        cur.execute(
            "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'good')"
        )
        assert not cur.fetchone()[0], "失败的迁移必须整条回滚，不能留下半截结果"


# ============================================================
# 多个组件共用一个数据库
# ============================================================


def test_migrations_of_different_components_do_not_collide(conn) -> None:
    """两个组件的版本号都从 0001_init 开始。

    迁移记录必须按**组件**隔离——这条是真容器测出来的：
    把两个组件迁移到同一个库时，后跑的那个的 0001_init 被当成"已执行"跳过，
    然后 0002 往一张根本没建出来的表里插数据，直接失败。
    """
    apply_migrations(
        conn, "department/tree",
        [Migration(version="0001_init", up="CREATE TABLE departments (id TEXT PRIMARY KEY)",
                   down="DROP TABLE departments")],
    )
    apply_migrations(
        conn, "people/basic",
        [Migration(version="0001_init", up="CREATE TABLE people (id TEXT PRIMARY KEY)",
                   down="DROP TABLE people")],
    )

    assert table_exists(conn, "departments") and table_exists(conn, "people")
    assert applied(conn, "department/tree") == ["0001_init"]
    assert applied(conn, "people/basic") == ["0001_init"]


# ============================================================
# 回退（undo / revert）
# ============================================================


def test_rollback_last_migration(conn) -> None:
    apply_migrations(conn, COMPONENT, load_migrations())

    reverted = rollback_migrations(conn, COMPONENT, load_migrations(), count=1)

    assert reverted == ["0002_seed_people"]
    assert applied(conn) == ["0001_init"]
    assert table_exists(conn, "people"), "只回退 0002 的话表应该还在"
    assert count(conn, "people") == 0, "0002 的初始数据应已被删除"


def test_rollback_all_migrations(conn) -> None:
    """count=0 全部回退：库回到干净状态，方便反复测试。"""
    apply_migrations(conn, COMPONENT, load_migrations())

    reverted = rollback_migrations(conn, COMPONENT, load_migrations(), count=0)

    assert reverted == ["0002_seed_people", "0001_init"], "必须倒序回退"
    assert applied(conn) == []
    assert not table_exists(conn, "people")


def test_migrate_after_rollback_restores_everything(conn) -> None:
    """回退再向上迁移，回到原样——"反复搭起来、拆掉"的基础。"""
    apply_migrations(conn, COMPONENT, load_migrations())
    rollback_migrations(conn, COMPONENT, load_migrations(), count=0)
    apply_migrations(conn, COMPONENT, load_migrations())

    assert count(conn, "people") == 4


def test_rollback_without_down_script_is_an_error(conn) -> None:
    """没写 down 脚本的迁移不能假装回退成功。

    那会让迁移记录与真实结构对不上，比报错危险得多。
    """
    one_way = [Migration(version="0001_init", up="CREATE TABLE people (id TEXT PRIMARY KEY)")]
    apply_migrations(conn, COMPONENT, one_way)

    with pytest.raises(RuntimeError) as excinfo:
        rollback_migrations(conn, COMPONENT, one_way, count=1)

    assert "0001_init" in str(excinfo.value) and "down" in str(excinfo.value)
    assert applied(conn) == ["0001_init"], "回退失败时不该删掉迁移记录"


def test_rollback_on_empty_database_is_noop(conn) -> None:
    assert rollback_migrations(conn, COMPONENT, load_migrations(), count=1) == []


def test_every_migration_has_a_down_script() -> None:
    """每个迁移都要能回退——开发与测试要靠它反复重建。"""
    for migration in load_migrations():
        assert migration.down.strip(), f"迁移 {migration.version} 缺少 down 脚本"
