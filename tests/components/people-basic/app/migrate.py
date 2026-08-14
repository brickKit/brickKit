"""迁移执行器。

迁移是**有版本的 SQL 文件**（migrations/*.up.sql 与 *.down.sql），
不是埋在代码里的建表语句：只有这样，"1.0.0 到 2.0.0 改了什么"
才是看得见、可评审、可回溯的。

执行方式各语言可以不同（002 §8.7 允许 Django / Rails / Flyway / 脚本…），
但不变量是一样的：幂等、原子、有序、按组件隔离。
"""

from __future__ import annotations

import logging
import pathlib
from dataclasses import dataclass

import psycopg

logger = logging.getLogger("app.migrate")

# 迁移脚本目录。002 §8.4：脚本和业务代码打包在同一个镜像里。
MIGRATIONS_DIR = pathlib.Path(__file__).resolve().parent.parent / "migrations"

# 主键是 (component_id, version) 而不是 version：**版本号是每个组件各自的**，
# 两个组件都会有 0001_init。这是真容器测出来的问题——共用一个数据库时，
# 先跑的组件会把后跑的顶掉，后者的表根本建不出来。
SCHEMA_MIGRATIONS_TABLE = """
CREATE TABLE IF NOT EXISTS schema_migrations (
    component_id TEXT NOT NULL,
    version      TEXT NOT NULL,
    applied_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (component_id, version)
)
"""


@dataclass(frozen=True)
class Migration:
    """一次结构变更。

    version 取自文件名，同时是执行顺序与去重依据。
    down 是回退脚本：**给开发与测试用**，让人能反复把库搭起来、拆掉。
    """

    version: str
    up: str
    down: str = ""


def load_migrations(directory: pathlib.Path = MIGRATIONS_DIR) -> list[Migration]:
    """读出全部迁移脚本，按版本排序。

    文件命名：<版本>.up.sql 与 <版本>.down.sql 成对出现。
    """
    if not directory.is_dir():
        raise FileNotFoundError(f"迁移脚本目录不存在：{directory}")

    scripts: dict[str, dict[str, str]] = {}
    for path in sorted(directory.glob("*.sql"), key=lambda p: p.name):
        # 0001_init.up.sql → stem 是 0001_init.up
        version, _, direction = path.stem.rpartition(".")
        if not version:
            version, direction = path.stem, "up"
        scripts.setdefault(version, {})[direction] = path.read_text(encoding="utf-8")

    return [
        Migration(version=version, up=parts.get("up", ""), down=parts.get("down", ""))
        for version, parts in sorted(scripts.items())
    ]


def applied_versions(conn: psycopg.Connection, component_id: str) -> set[str]:
    with conn.cursor() as cur:
        cur.execute(
            "SELECT version FROM schema_migrations WHERE component_id = %s", (component_id,)
        )
        return {row[0] for row in cur.fetchall()}


def _ensure_table(conn: psycopg.Connection) -> None:
    with conn.cursor() as cur:
        cur.execute(SCHEMA_MIGRATIONS_TABLE)


def _warn_if_shared(conn: psycopg.Connection, component_id: str) -> None:
    """库里有别的组件的迁移记录时提醒一句。

    不阻断：共用一个库在本地调试时确实方便，按组件隔离之后也不会再互相顶掉。
    但 002 §2.2 的数据自治要求每个组件有自己的库。
    """
    with conn.cursor() as cur:
        cur.execute(
            "SELECT DISTINCT component_id FROM schema_migrations WHERE component_id <> %s",
            (component_id,),
        )
        others = [row[0] for row in cur.fetchall()]
    if others:
        logger.warning(
            "该数据库里还有其他组件的表",
            extra={"others": ", ".join(others), "advice": "002 §2.2：每个组件用自己的数据库，见 README"},
        )


def apply_migrations(
    conn: psycopg.Connection, component_id: str, migrations: list[Migration]
) -> list[str]:
    """按顺序执行尚未执行过的迁移，返回本次执行了哪些版本。

    - **幂等**：已执行过的版本不会再跑一遍，容器重启是安全的；
    - **原子**：迁移与它的版本记录在同一个事务里，失败整条回滚
      （先执行后记录会留下一个窗口，进程正好挂在这里就会重复执行）；
    - **有序**：按版本号从小到大，失败即中止，不跳过继续。
    """
    _ensure_table(conn)
    _warn_if_shared(conn, component_id)

    done = applied_versions(conn, component_id)
    executed: list[str] = []

    for migration in migrations:
        if migration.version in done:
            continue
        try:
            with conn.transaction():
                with conn.cursor() as cur:
                    cur.execute(migration.up)
                    cur.execute(
                        "INSERT INTO schema_migrations (component_id, version) VALUES (%s, %s)",
                        (component_id, migration.version),
                    )
        except Exception as exc:
            raise RuntimeError(f"执行迁移 {migration.version} 失败：{exc}") from exc

        logger.info("迁移已执行", extra={"version": migration.version})
        executed.append(migration.version)

    return executed


def rollback_migrations(
    conn: psycopg.Connection, component_id: str, migrations: list[Migration], count: int = 1
) -> list[str]:
    """回退已执行的迁移（undo），返回本次回退了哪些版本。

    count 为回退几个，0 表示全部回退。按版本**倒序**执行：
    顺序反了会出现"表已经删了，再去删表里的数据"。

    这是给开发与测试用的：反复把库搭起来、拆掉。
    生产环境请用新的 up 迁移修问题（002 §8.9）。
    """
    _ensure_table(conn)

    done = applied_versions(conn, component_id)
    pending = [m for m in reversed(migrations) if m.version in done]
    if count > 0:
        pending = pending[:count]

    reverted: list[str] = []
    for migration in pending:
        if not migration.down.strip():
            # 假装回退成功会让迁移记录与真实结构对不上，比报错危险得多
            raise RuntimeError(f"迁移 {migration.version} 没有提供 down 脚本，无法回退")
        try:
            with conn.transaction():
                with conn.cursor() as cur:
                    cur.execute(migration.down)
                    cur.execute(
                        "DELETE FROM schema_migrations WHERE component_id = %s AND version = %s",
                        (component_id, migration.version),
                    )
        except Exception as exc:
            raise RuntimeError(f"回退迁移 {migration.version} 失败：{exc}") from exc

        logger.info("迁移已回退", extra={"version": migration.version})
        reverted.append(migration.version)

    return reverted
