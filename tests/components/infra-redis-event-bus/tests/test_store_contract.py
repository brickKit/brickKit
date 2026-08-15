"""存储层的**行为契约测试**：fakeredis 与真 Redis 跑同一份用例。

只测 fakeredis 的话，XADD 的 maxlen 参数写错、xrevrange 的顺序理解反了，
单测照样全绿，上了真 Redis 才发现——而事件总线出错的表现往往是
"事件偶尔丢了"，最难查。

设置 EVENT_BUS_TEST_REDIS_ADDR 时跑真 Redis 那一组。
"""

from __future__ import annotations

import os

import fakeredis
import pytest
import redis

from app.store import EventStore

STREAM = "brickkit:events:contract-test"
ENV_REAL_REDIS = "EVENT_BUS_TEST_REDIS_ADDR"


def _fake_store() -> EventStore:
    return EventStore(fakeredis.FakeStrictRedis(decode_responses=True), stream=STREAM, maxlen=100)


def _real_store() -> EventStore:
    addr = os.environ.get(ENV_REAL_REDIS, "")
    if not addr:
        pytest.skip(f"未设置 {ENV_REAL_REDIS}，跳过真 Redis 契约测试")

    host, _, port = addr.rpartition(":")
    client = redis.Redis(
        host=host or "localhost",
        port=int(port or 6379),
        password=os.environ.get("EVENT_BUS_TEST_REDIS_PASSWORD") or None,
        decode_responses=True,
        socket_connect_timeout=3,
    )
    # 每个用例从干净的流开始
    client.delete(STREAM)
    return EventStore(client, stream=STREAM, maxlen=100)


@pytest.fixture(params=["fakeredis", "redis"])
def store(request: pytest.FixtureRequest) -> EventStore:
    return _fake_store() if request.param == "fakeredis" else _real_store()


def event(**overrides) -> dict:
    base = {"type": "erp.order.approved", "actor": "p-001", "subject": "o-1", "time": "2026-01-01T10:00:00Z"}
    base.update(overrides)
    return base


# ============================================================
# 27.1 Redis Streams 收发
# ============================================================


def test_append_returns_entry_id(store: EventStore) -> None:
    entry_id = store.append(event())

    assert entry_id, "XADD 应当返回条目 ID"
    # Redis Stream 的 ID 形如 <毫秒时间戳>-<序号>
    assert "-" in entry_id


def test_read_returns_newest_first(store: EventStore) -> None:
    for i in range(3):
        store.append(event(subject=f"o-{i}"))

    got = store.read(limit=10)
    assert [e["subject"] for e in got] == ["o-2", "o-1", "o-0"]


def test_read_attaches_entry_id(store: EventStore) -> None:
    """读回来的事件要带上 Stream 的条目 ID，便于排障时对上号。"""
    entry_id = store.append(event())

    got = store.read(limit=1)
    assert got[0]["id"] == entry_id


def test_empty_stream_reads_empty(store: EventStore) -> None:
    assert store.read(limit=10) == []


def test_filter_by_type(store: EventStore) -> None:
    store.append(event(type="erp.order.approved"))
    store.append(event(type="people.person.created"))
    store.append(event(type="erp.order.approved", subject="o-2"))

    got = store.read(limit=10, event_type="erp.order.approved")
    assert len(got) == 2
    assert {e["type"] for e in got} == {"erp.order.approved"}


def test_limit_is_honored(store: EventStore) -> None:
    for i in range(5):
        store.append(event(subject=f"o-{i}"))

    assert len(store.read(limit=2)) == 2


def test_extra_fields_survive_round_trip(store: EventStore) -> None:
    """事件总线不理解事件的内容，额外字段必须原样存取。"""
    store.append(event(orderAmount="120000", tenant="acme"))

    got = store.read(limit=1)[0]
    assert got["orderAmount"] == "120000"
    assert got["tenant"] == "acme"


def test_id_field_from_publisher_is_ignored(store: EventStore) -> None:
    """发布方自己填的 id 不能覆盖 Stream 生成的那个。

    否则两条事件可能"撞 ID"，排障时按 ID 去找会找出错的那条。
    """
    entry_id = store.append(event(id="publisher-supplied-id"))

    got = store.read(limit=1)[0]
    assert got["id"] == entry_id
    assert got["id"] != "publisher-supplied-id"


def test_stream_is_trimmed(store: EventStore) -> None:
    """流要被裁剪，否则事件一直累积，迟早把 Redis 撑爆。

    用的是 approximate 裁剪（Redis 的 `~`），实际长度会略多于 maxlen——
    所以这里断言的是"有上界"，不是"精确等于 maxlen"。
    """
    for i in range(300):  # maxlen=100
        store.append(event(subject=f"o-{i}"))

    got = store.read(limit=500)
    assert len(got) < 300, "流没有被裁剪"
    # 最新的那条一定还在
    assert got[0]["subject"] == "o-299"
