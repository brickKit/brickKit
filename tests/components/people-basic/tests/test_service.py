"""Step 22「people/basic」的业务行为测试。

覆盖开发计划 22.1（HTTP）、22.4（健康检查）、22.7（强依赖调用）、22.8（弱依赖安全读取），
以及这个组件存在的意义：**强依赖与弱依赖在故障时的表现必须不一样**。
"""

from __future__ import annotations

import pytest
from fastapi.testclient import TestClient

from app.department import DepartmentUnavailable
from app.http_api import create_app
from app.service import PeopleService
from app.store import MemoryStore, Person

# ============================================================
# 夹具
# ============================================================


def seed_people() -> list[Person]:
    return [
        Person(id="p-001", name="张三", department_id="d-tech", title="后端工程师"),
        Person(id="p-002", name="李四", department_id="d-tech", title="前端工程师"),
        Person(id="p-003", name="王五", department_id="d-hr", title="HR 专员"),
    ]


class FakeDepartments:
    """department/tree 的替身：返回部门名，并记录被调用次数。"""

    def __init__(self, names: dict[str, str] | None = None, fail: bool = False):
        self.names = names if names is not None else {"d-tech": "技术中心", "d-hr": "人力资源部"}
        self.fail = fail
        self.calls = 0

    def name_of(self, department_id: str) -> str:
        self.calls += 1
        if self.fail:
            raise DepartmentUnavailable("department/tree 连不上")
        return self.names.get(department_id, "")


class FakeEventBus:
    """弱依赖 infra/redis-event-bus 的替身。"""

    def __init__(self, enabled: bool = True):
        self.enabled = enabled
        self.published: list[tuple[str, dict]] = []

    def publish(self, topic: str, payload: dict) -> None:
        if not self.enabled:
            return
        self.published.append((topic, payload))


def build_service(
    *,
    store: object | None = None,
    departments: object | None = None,
    events: object | None = None,
) -> PeopleService:
    return PeopleService(
        store=store if store is not None else MemoryStore(seed_people()),
        departments=departments if departments is not None else FakeDepartments(),
        events=events if events is not None else FakeEventBus(),
        component_id="people/basic",
        version="1.0.0",
    )


@pytest.fixture()
def client() -> TestClient:
    return TestClient(create_app(build_service()))


# ============================================================
# 22.1 HTTP API
# ============================================================


def test_list_people(client: TestClient) -> None:
    resp = client.get("/api/v1/people")

    assert resp.status_code == 200
    body = resp.json()
    assert body["total"] == 3
    assert [p["id"] for p in body["people"]] == ["p-001", "p-002", "p-003"]


def test_list_people_filtered_by_department(client: TestClient) -> None:
    resp = client.get("/api/v1/people", params={"departmentId": "d-hr"})

    assert resp.status_code == 200
    assert [p["id"] for p in resp.json()["people"]] == ["p-003"]


def test_get_person(client: TestClient) -> None:
    resp = client.get("/api/v1/people/p-001")

    assert resp.status_code == 200
    body = resp.json()
    assert body["name"] == "张三"
    assert body["departmentId"] == "d-tech"


def test_get_unknown_person_returns_404(client: TestClient) -> None:
    resp = client.get("/api/v1/people/nobody")

    assert resp.status_code == 404
    assert resp.json()["error"]


# ============================================================
# 22.7 强依赖 department/tree
# ============================================================


def test_person_is_enriched_with_department_name() -> None:
    """人员信息里的部门名来自 department/tree，不是自己存一份。

    组件不重复存别人的数据（002 §2.2 数据自治）：部门改名以后，
    这里必须立刻跟着变，而不是等一次数据同步。
    """
    client = TestClient(create_app(build_service()))

    body = client.get("/api/v1/people/p-001").json()

    assert body["departmentName"] == "技术中心"


def test_department_rename_is_reflected_immediately() -> None:
    departments = FakeDepartments()
    client = TestClient(create_app(build_service(departments=departments)))

    assert client.get("/api/v1/people/p-001").json()["departmentName"] == "技术中心"

    departments.names["d-tech"] = "研发中心"

    assert client.get("/api/v1/people/p-001").json()["departmentName"] == "研发中心"


def test_strong_dependency_outage_returns_503() -> None:
    """强依赖挂了要如实报 503。

    这是强依赖与弱依赖的分界线：强依赖不可用时本组件**无法履行契约**，
    必须说出来；假装成功、返回一个没有部门名的人员，才是真正的坑。
    """
    client = TestClient(create_app(build_service(departments=FakeDepartments(fail=True))))

    resp = client.get("/api/v1/people/p-001")

    assert resp.status_code == 503
    assert "部门" in resp.json()["error"]


def test_strong_dependency_outage_also_affects_list() -> None:
    client = TestClient(create_app(build_service(departments=FakeDepartments(fail=True))))

    assert client.get("/api/v1/people").status_code == 503


# ============================================================
# 22.8 弱依赖 infra/redis-event-bus
# ============================================================


def test_weak_dependency_absent_does_not_break_anything() -> None:
    """弱依赖缺失时组件照常工作（002 §3.4）。

    平台**完全不注入** INFRA_REDIS_EVENT_BUS_ENDPOINT，
    组件必须用 os.environ.get() 安全读取，而不是直接下标取值。
    """
    events = FakeEventBus(enabled=False)
    client = TestClient(create_app(build_service(events=events)))

    resp = client.get("/api/v1/people")

    assert resp.status_code == 200
    assert events.published == []


def test_weak_dependency_present_publishes_event() -> None:
    events = FakeEventBus(enabled=True)
    service = build_service(events=events)
    client = TestClient(create_app(service))

    client.get("/api/v1/people/p-001")

    assert len(events.published) == 1
    topic, payload = events.published[0]
    assert topic == "people.person.viewed"
    assert payload["personId"] == "p-001"


def test_event_bus_failure_does_not_break_the_request() -> None:
    """弱依赖出错也不能影响主流程——这正是"弱"的含义。"""

    class BrokenBus:
        def publish(self, topic: str, payload: dict) -> None:
            raise RuntimeError("Redis 连接被拒绝")

    client = TestClient(create_app(build_service(events=BrokenBus())))

    assert client.get("/api/v1/people/p-001").status_code == 200


# ============================================================
# 22.4 健康检查
# ============================================================


def test_healthz_returns_200(client: TestClient) -> None:
    resp = client.get("/healthz")

    assert resp.status_code == 200
    assert resp.json()["status"] == "ok"


def test_healthz_does_not_touch_database_or_dependencies() -> None:
    """002 §9.4：/healthz 只检查本进程存活。

    健康检查一旦连库或调依赖，数据库抖一下就会让所有组件被判死重启，
    把一次故障放大成整个系统雪崩。
    """

    class ExplodingStore:
        def __init__(self) -> None:
            self.calls = 0

        def list(self, department_id: str = "") -> list[Person]:
            self.calls += 1
            raise RuntimeError("数据库连接已断开")

        def get(self, person_id: str) -> Person | None:
            self.calls += 1
            raise RuntimeError("数据库连接已断开")

    store = ExplodingStore()
    departments = FakeDepartments(fail=True)
    client = TestClient(create_app(build_service(store=store, departments=departments)))

    resp = client.get("/healthz")

    assert resp.status_code == 200
    assert store.calls == 0, "健康检查不该访问数据库"
    assert departments.calls == 0, "健康检查不该调用依赖组件"


def test_store_failure_is_reported_as_503() -> None:
    class ExplodingStore:
        def list(self, department_id: str = "") -> list[Person]:
            raise RuntimeError("数据库连接已断开")

        def get(self, person_id: str) -> Person | None:
            raise RuntimeError("数据库连接已断开")

    client = TestClient(create_app(build_service(store=ExplodingStore())))

    resp = client.get("/api/v1/people")

    assert resp.status_code == 503
    # 002 §11.3：错误信息不向外暴露内部实现细节
    assert "数据库连接已断开" not in resp.text


# ============================================================
# 22.6 OpenAPI
# ============================================================


def test_openapi_document_is_served(client: TestClient) -> None:
    """22.6：OpenAPI 由 FastAPI 自动生成，随组件一起提供。"""
    resp = client.get("/openapi.json")

    assert resp.status_code == 200
    doc = resp.json()
    assert doc["info"]["title"]
    for path in ("/api/v1/people", "/healthz"):
        assert path in doc["paths"], f"OpenAPI 应描述 {path}"


# ============================================================
# 依赖调用的次数与新鲜度
# ============================================================


def test_list_deduplicates_department_lookups_within_one_request() -> None:
    """一次列表请求里，同一个部门只问 department/tree 一次。

    3 个人分布在 2 个部门 → 2 次调用，而不是 3 次。
    """
    departments = FakeDepartments()
    client = TestClient(create_app(build_service(departments=departments)))

    client.get("/api/v1/people")

    assert departments.calls == 2, f"d-tech 与 d-hr 各一次，实际 {departments.calls} 次"


def test_lookups_are_not_cached_across_requests() -> None:
    """跨请求不缓存部门名。

    这条是被真容器打脸打出来的：最初 DepartmentClient 里有个永不过期的缓存，
    结果把 department/tree 停掉之后接口照样 200——缓存把"强依赖挂了"盖住了，
    "部门改名立刻生效"也成了空话。
    """
    departments = FakeDepartments()
    client = TestClient(create_app(build_service(departments=departments)))

    client.get("/api/v1/people/p-001")
    first_round = departments.calls
    client.get("/api/v1/people/p-001")

    assert departments.calls > first_round, "第二次请求必须重新问一遍，不能吃缓存"
