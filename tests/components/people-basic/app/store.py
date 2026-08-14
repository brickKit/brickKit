"""人员数据的存取。

组件只存自己的数据（002 §2.2 数据自治）：这里只有人员，
部门信息属于 department/tree，用到时去调它，不在这里存副本。
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol

import psycopg

from app.migrate import apply_migrations, load_migrations, rollback_migrations


@dataclass
class Person:
    id: str
    name: str
    department_id: str
    title: str = ""


class Store(Protocol):
    """人员数据的存取接口。

    注意：**建表与初始数据不在这里**，它们是 migrations/*.sql（002 §8）。
    内存实现只是测试替身，用 MemoryStore(seed) 直接给数据即可。
    """

    def list(self, department_id: str = "") -> list[Person]:
        """返回人员，按 ID 排序；department_id 非空时只返回该部门的人。"""

    def get(self, person_id: str) -> Person | None:
        """按 ID 查询，不存在时返回 None。"""


# ============================================================
# 内存实现
# ============================================================


class MemoryStore:
    """内存实现，供测试与本地把玩使用。"""

    def __init__(self, seed: list[Person] | None = None):
        self._items: dict[str, Person] = {p.id: p for p in (seed or [])}

    def list(self, department_id: str = "") -> list[Person]:
        items = [
            p for p in self._items.values()
            if not department_id or p.department_id == department_id
        ]
        return sorted(items, key=lambda p: p.id)

    def get(self, person_id: str) -> Person | None:
        return self._items.get(person_id)


# ============================================================
# PostgreSQL 实现
# ============================================================


class PostgresStore:
    """PostgreSQL 实现。连接信息全部来自平台注入的 DATABASE_*（006 §5.1）。"""

    def __init__(self, dsn: str):
        self._dsn = dsn
        self._conn = psycopg.connect(dsn, autocommit=True)

    def close(self) -> None:
        self._conn.close()

    def migrate(self, component_id: str) -> list[str]:
        """执行尚未执行过的迁移脚本（migrations/*.up.sql）。"""
        return apply_migrations(self._conn, component_id, load_migrations())

    def rollback(self, component_id: str, count: int = 1) -> list[str]:
        """回退已执行的迁移（migrations/*.down.sql）。count 为 0 表示全部回退。"""
        return rollback_migrations(self._conn, component_id, load_migrations(), count)

    def list(self, department_id: str = "") -> list[Person]:
        query = "SELECT id, name, department_id, title FROM people"
        params: tuple = ()
        if department_id:
            query += " WHERE department_id = %s"
            params = (department_id,)
        query += " ORDER BY id"

        with self._conn.cursor() as cur:
            cur.execute(query, params)
            return [Person(*row) for row in cur.fetchall()]

    def get(self, person_id: str) -> Person | None:
        with self._conn.cursor() as cur:
            cur.execute(
                "SELECT id, name, department_id, title FROM people WHERE id = %s",
                (person_id,),
            )
            row = cur.fetchone()
            return Person(*row) if row else None
