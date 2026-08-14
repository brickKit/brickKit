"""人员数据的存取。

组件只存自己的数据（002 §2.2 数据自治）：这里只有人员，
部门信息属于 department/tree，用到时去调它，不在这里存副本。
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol

import psycopg


@dataclass
class Person:
    id: str
    name: str
    department_id: str
    title: str = ""


class Store(Protocol):
    """人员数据的存取接口。"""

    def list(self, department_id: str = "") -> list[Person]:
        """返回人员，按 ID 排序；department_id 非空时只返回该部门的人。"""

    def get(self, person_id: str) -> Person | None:
        """按 ID 查询，不存在时返回 None。"""


def initial_people() -> list[Person]:
    """随迁移写入的初始人员。

    department_id 对应 department/tree 迁移里的部门 ID：
    两个组件的样例数据是对得上的，装配起来才有东西可看。
    """
    return [
        Person(id="p-001", name="张三", department_id="d-tech", title="后端工程师"),
        Person(id="p-002", name="李四", department_id="d-tech", title="前端工程师"),
        Person(id="p-003", name="王五", department_id="d-hr", title="HR 专员"),
        Person(id="p-004", name="赵六", department_id="d-backend", title="后端工程师"),
    ]


def migrate(store: Store) -> None:
    """建表并写入初始数据（002 §8.2）。

    必须幂等：容器每次重启都会再跑一遍，第二次失败等于服务再也起不来。
    """
    schema = getattr(store, "ensure_schema", None)
    upsert = getattr(store, "upsert", None)
    if schema is None or upsert is None:
        raise TypeError("该存储实现不支持迁移")

    schema()
    for person in initial_people():
        upsert(person)


# ============================================================
# 内存实现
# ============================================================


class MemoryStore:
    """内存实现，供测试与本地把玩使用。"""

    def __init__(self, seed: list[Person] | None = None):
        self._items: dict[str, Person] = {p.id: p for p in (seed or [])}

    def ensure_schema(self) -> None:
        return None

    def upsert(self, person: Person) -> None:
        self._items[person.id] = person

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

    def ensure_schema(self) -> None:
        with self._conn.cursor() as cur:
            cur.execute(
                """
                CREATE TABLE IF NOT EXISTS people (
                    id            TEXT PRIMARY KEY,
                    name          TEXT NOT NULL,
                    department_id TEXT NOT NULL DEFAULT '',
                    title         TEXT NOT NULL DEFAULT ''
                )
                """
            )
            cur.execute(
                "CREATE INDEX IF NOT EXISTS idx_people_department ON people (department_id)"
            )

    def upsert(self, person: Person) -> None:
        with self._conn.cursor() as cur:
            cur.execute(
                """
                INSERT INTO people (id, name, department_id, title)
                VALUES (%s, %s, %s, %s)
                ON CONFLICT (id) DO UPDATE
                SET name = EXCLUDED.name,
                    department_id = EXCLUDED.department_id,
                    title = EXCLUDED.title
                """,
                (person.id, person.name, person.department_id, person.title),
            )

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
