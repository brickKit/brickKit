"""infra/redis-event-bus 的业务行为测试。

覆盖开发计划 27.1（Redis Streams 收发）、27.2 / 27.3（健康检查）。

这个组件与 authorization/rbac 形成一个刻意的对照：两者都绑 Redis，
但角色完全不同——那里 Redis 是**加速器**（挂了照常回源），这里 Redis 是
**唯一的数据源**（挂了就什么也做不了，必须如实报 503）。
同一种资源，两种截然不同的故障处理，这正是本组件想验证的东西之一。
"""

from __future__ import annotations

import fakeredis
import pytest
from fastapi.testclient import TestClient

from app.config import Config, RedisConfig
from app.http_api import create_app
from app.store import EventStore

STREAM = "brickkit:events"


@pytest.fixture()
def store() -> EventStore:
    """用 fakeredis 做替身。真 Redis 上的行为由 test_store_contract 保证。"""
    return EventStore(fakeredis.FakeStrictRedis(decode_responses=True), stream=STREAM, maxlen=1000)


@pytest.fixture()
def client(store: EventStore) -> TestClient:
    cfg = Config(
        component_id="infra/redis-event-bus",
        version="1.0.0",
        log_level="info",
        redis=RedisConfig(host="redis", port=6379, password="", stream=STREAM, maxlen=1000),
    )
    return TestClient(create_app(store, cfg))


def sample_event(**overrides) -> dict:
    """erp/backend 真正发出来的那种事件（见它的 eventbus.go）。"""
    event = {
        "type": "erp.order.approved",
        "actor": "p-001",
        "subject": "o-1",
        "time": "2026-01-01T10:00:00Z",
    }
    event.update(overrides)
    return event


# ============================================================
# 27.1 收发事件
# ============================================================


def test_publish_then_read_back(client: TestClient) -> None:
    """发出去的事件要能读回来，且内容不变。"""
    resp = client.post("/api/v1/events", json=sample_event())
    assert resp.status_code == 202, resp.text

    body = resp.json()
    assert body["id"], "要把 Stream 里的条目 ID 回给发布者，便于排障时对上号"

    listed = client.get("/api/v1/events").json()
    assert listed["total"] == 1
    got = listed["events"][0]
    assert got["type"] == "erp.order.approved"
    assert got["actor"] == "p-001"
    assert got["subject"] == "o-1"
    assert got["id"] == body["id"]


def test_publish_returns_202_not_200(client: TestClient) -> None:
    """202 而不是 200：事件已收下，但消费是异步的。

    用 200 会让发布方以为"已经被处理了"，而实际上只是进了流。
    """
    assert client.post("/api/v1/events", json=sample_event()).status_code == 202


def test_events_are_returned_newest_first(client: TestClient) -> None:
    """最新的在前：查事件几乎总是为了看"刚才发生了什么"。"""
    for i in range(3):
        client.post("/api/v1/events", json=sample_event(subject=f"o-{i}"))

    events = client.get("/api/v1/events").json()["events"]
    assert [e["subject"] for e in events] == ["o-2", "o-1", "o-0"]


def test_limit_is_honored(client: TestClient) -> None:
    for i in range(5):
        client.post("/api/v1/events", json=sample_event(subject=f"o-{i}"))

    events = client.get("/api/v1/events?limit=2").json()["events"]
    assert [e["subject"] for e in events] == ["o-4", "o-3"]


def test_filter_by_type(client: TestClient) -> None:
    """按类型过滤：一条流上跑着所有组件的事件，不过滤等于让消费方自己筛。"""
    client.post("/api/v1/events", json=sample_event(type="erp.order.approved"))
    client.post("/api/v1/events", json=sample_event(type="people.person.created"))

    events = client.get("/api/v1/events?type=erp.order.approved").json()["events"]
    assert len(events) == 1
    assert events[0]["type"] == "erp.order.approved"


def test_empty_stream_returns_empty_list(client: TestClient) -> None:
    """一条事件都没有时返回 []，不是 null——弱类型的调用方遍历 null 会直接崩。"""
    body = client.get("/api/v1/events").json()
    assert body["events"] == []
    assert body["total"] == 0


# ============================================================
# 请求校验
# ============================================================


def test_type_is_required(client: TestClient) -> None:
    """没有 type 的事件毫无意义：消费方靠它决定要不要处理。"""
    resp = client.post("/api/v1/events", json={"actor": "p-001", "subject": "o-1"})
    assert resp.status_code == 422


def test_time_is_filled_when_missing(client: TestClient) -> None:
    """发布方没带时间戳时由本组件补上。

    宁可用"收到的时间"，也不要一条没有时间的事件——排障时时间往往是
    唯一能把几个组件的日志对起来的东西。
    """
    resp = client.post("/api/v1/events", json={"type": "erp.order.approved", "actor": "p-001"})
    assert resp.status_code == 202

    event = client.get("/api/v1/events").json()["events"][0]
    assert event["time"], "缺失的时间戳应当由本组件补上"


def test_extra_fields_are_preserved(client: TestClient) -> None:
    """发布方带了额外字段就原样存下来。

    事件总线不理解事件的内容（002 的一贯立场：平台不解析业务语义）。
    丢掉不认识的字段，等于逼所有发布方都来改这个组件。
    """
    client.post("/api/v1/events", json=sample_event(orderAmount="120000"))

    event = client.get("/api/v1/events").json()["events"][0]
    assert event.get("orderAmount") == "120000"


# ============================================================
# 27.2 / 27.3 健康检查
# ============================================================


def test_healthz_returns_200(client: TestClient) -> None:
    assert client.get("/healthz").status_code == 200


def test_healthz_does_not_touch_redis() -> None:
    """27.3：健康检查不碰 Redis。

    Redis 在这里是**唯一的数据源**，比 rbac 那边更容易让人想去探一探。
    但健康检查只回答"本进程还活着吗"（002 §9.4）：去探 Redis 的话，
    Redis 一抖，编排系统就把这些本身完全正常的容器全杀掉重启——
    而重启并不会让 Redis 变好，只会让恢复之后还要多等一轮拉起。
    """

    class ExplodingStore:
        """一碰就炸的存储：healthz 只要碰了它，测试立刻失败。"""

        def append(self, *_args, **_kwargs):
            raise AssertionError("healthz 不该写 Redis")

        def read(self, *_args, **_kwargs):
            raise AssertionError("healthz 不该读 Redis")

        def ping(self):
            raise AssertionError("healthz 不该 ping Redis")

    cfg = Config(
        component_id="infra/redis-event-bus",
        version="1.0.0",
        log_level="info",
        redis=RedisConfig(host="redis", port=6379, password="", stream=STREAM, maxlen=1000),
    )
    client = TestClient(create_app(ExplodingStore(), cfg))

    assert client.get("/healthz").status_code == 200


# ============================================================
# Redis 不可用：这里它是数据源，不是加速器
# ============================================================


def test_publish_reports_503_when_redis_is_down() -> None:
    """Redis 挂了必须如实报 503，**不能假装收下了**。

    这与 authorization/rbac 正好相反：那里 Redis 是加速器，挂了照常回源；
    这里它是唯一的数据源，假装成功等于把事件悄悄丢掉——而发布方会以为
    事件已经安全落地，再也不会重发。
    """

    class BrokenStore:
        def append(self, *_args, **_kwargs):
            raise ConnectionError("connection refused")

        def read(self, *_args, **_kwargs):
            raise ConnectionError("connection refused")

    cfg = Config(
        component_id="infra/redis-event-bus",
        version="1.0.0",
        log_level="info",
        redis=RedisConfig(host="redis", port=6379, password="", stream=STREAM, maxlen=1000),
    )
    client = TestClient(create_app(BrokenStore(), cfg))

    resp = client.post("/api/v1/events", json=sample_event())
    assert resp.status_code == 503

    # 底层细节不外泄
    assert "connection refused" not in resp.text.lower()
    assert "redis" not in resp.json()["error"].lower() or "暂时不可用" in resp.json()["error"]


def test_read_reports_503_when_redis_is_down() -> None:
    class BrokenStore:
        def append(self, *_args, **_kwargs):
            raise ConnectionError("connection refused")

        def read(self, *_args, **_kwargs):
            raise ConnectionError("connection refused")

    cfg = Config(
        component_id="infra/redis-event-bus",
        version="1.0.0",
        log_level="info",
        redis=RedisConfig(host="redis", port=6379, password="", stream=STREAM, maxlen=1000),
    )
    client = TestClient(create_app(BrokenStore(), cfg))

    assert client.get("/api/v1/events").status_code == 503
