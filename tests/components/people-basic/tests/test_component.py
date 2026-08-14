"""组件对平台的承诺（开发计划 22.2、22.5、22.9、22.10）。

这些不是业务功能，而是"能不能被平台装配"的前提。
"""

from __future__ import annotations

import json
import logging
import pathlib

import grpc
import pytest
import yaml

from app.config import Config, config_from_env, configure_logging
from app.grpc_api import serve_grpc
from app.store import MemoryStore, migrate
from gen.people.v1 import people_pb2, people_pb2_grpc
from tests.test_service import FakeDepartments, FakeEventBus, build_service, seed_people

ROOT = pathlib.Path(__file__).resolve().parent.parent


# ============================================================
# 22.10 extraPorts / 22.9 artifacts
# ============================================================


def load_manifest() -> dict:
    return yaml.safe_load((ROOT / "component.yaml").read_text(encoding="utf-8"))


def test_manifest_declares_extra_grpc_port() -> None:
    """22.10：Python 的 grpcio 不能与 HTTP 共用端口，必须声明 extraPorts。

    平台据此注入 PEOPLE_BASIC_GRPC_ENDPOINT（004 §5.6），
    调用方才知道 gRPC 在哪个端口上。
    """
    deployment = load_manifest()["deployment"]

    assert deployment["port"] == 8080, "主端口用于健康检查与 _ENDPOINT 注入"
    assert deployment["extraPorts"] == [{"name": "grpc", "port": 9090}]


def test_manifest_declares_dependencies() -> None:
    """强依赖 department/tree、弱依赖 infra/redis-event-bus，缺一不可。"""
    deps = load_manifest()["dependencies"]
    components = {str(entry): entry for entry in deps["components"]}

    text = json.dumps(deps, ensure_ascii=False)
    assert "department/tree" in text, f"应声明强依赖 department/tree：{components}"
    assert "infra/redis-event-bus" in text, "应声明弱依赖 infra/redis-event-bus"

    weak = [d for d in deps["components"] if isinstance(d, dict) and d.get("optional")]
    assert weak, "弱依赖必须标 optional: true，否则平台会把它当成必须项"


def test_manifest_declares_artifacts_that_exist() -> None:
    """22.9：声明的产物文件必须真的存在——市场发布时按这个列表逐个上传。"""
    artifacts = load_manifest()["artifacts"]
    types = {a["type"] for a in artifacts}

    assert {"api-contract", "api-docs"} <= types

    for artifact in artifacts:
        for file in artifact["files"]:
            assert (ROOT / file).exists(), f"声明的产物文件不存在：{file}"


def test_dockerfile_runs_as_non_root() -> None:
    text = (ROOT / "Dockerfile").read_text(encoding="utf-8")

    assert "USER " in text, "容器不得以 root 运行（008）"
    assert "USER root" not in text


# ============================================================
# 22.2 gRPC（extraPorts 上的 9090）
# ============================================================


@pytest.fixture()
def grpc_channel():
    """在随机端口上起一个真实的 gRPC 服务器。"""
    service = build_service()
    server, port = serve_grpc(service, port=0)
    try:
        yield grpc.insecure_channel(f"localhost:{port}")
    finally:
        server.stop(None)


def test_grpc_list_people(grpc_channel) -> None:
    stub = people_pb2_grpc.PeopleServiceStub(grpc_channel)

    resp = stub.ListPeople(people_pb2.ListPeopleRequest())

    assert resp.total == 3
    assert [p.id for p in resp.people] == ["p-001", "p-002", "p-003"]
    assert resp.people[0].department_name == "技术中心", "gRPC 出口同样要补全部门名"


def test_grpc_get_person(grpc_channel) -> None:
    stub = people_pb2_grpc.PeopleServiceStub(grpc_channel)

    person = stub.GetPerson(people_pb2.GetPersonRequest(id="p-003"))

    assert person.name == "王五"
    assert person.department_id == "d-hr"


def test_grpc_unknown_person_returns_not_found(grpc_channel) -> None:
    stub = people_pb2_grpc.PeopleServiceStub(grpc_channel)

    with pytest.raises(grpc.RpcError) as excinfo:
        stub.GetPerson(people_pb2.GetPersonRequest(id="nobody"))

    assert excinfo.value.code() == grpc.StatusCode.NOT_FOUND


def test_grpc_and_http_agree(grpc_channel) -> None:
    """两种协议看到的数据必须一致——一份业务逻辑，两个出口。"""
    from fastapi.testclient import TestClient

    from app.http_api import create_app

    stub = people_pb2_grpc.PeopleServiceStub(grpc_channel)
    over_grpc = [p.id for p in stub.ListPeople(people_pb2.ListPeopleRequest()).people]

    client = TestClient(create_app(build_service()))
    over_http = [p["id"] for p in client.get("/api/v1/people").json()["people"]]

    assert over_grpc == over_http


# ============================================================
# 22.5 迁移
# ============================================================


def test_migration_is_idempotent() -> None:
    """容器每次重启都会再跑一次迁移，第二次失败等于服务再也起不来。"""
    store = MemoryStore([])

    migrate(store)
    migrate(store)

    people = store.list()
    assert people, "迁移应写入初始人员数据"
    assert len({p.id for p in people}) == len(people), "重复迁移不得产生重复数据"


# ============================================================
# 配置与日志（002 §1.4、§11）
# ============================================================


def test_config_comes_from_environment() -> None:
    env = {
        "COMPONENT_ID": "people/basic",
        "COMPONENT_VERSION": "2.0.0",
        "DATABASE_HOST": "pg.internal",
        "DATABASE_PORT": "6432",
        "DATABASE_NAME": "people",
        "DATABASE_USER": "people",
        "DATABASE_PASSWORD": "s3cret",
        "DEPARTMENT_TREE_ENDPOINT": "http://department-tree-1-0-0:8080",
        "LOG_LEVEL": "debug",
    }

    cfg = config_from_env(env.get)

    assert cfg.component_id == "people/basic"
    assert cfg.version == "2.0.0"
    assert cfg.database.host == "pg.internal"
    assert cfg.database.port == 6432
    assert cfg.department_endpoint == "http://department-tree-1-0-0:8080"
    assert cfg.log_level == "debug"


def test_missing_strong_dependency_endpoint_is_an_error() -> None:
    """强依赖的地址没注入 → 启动就失败。

    强依赖缺失时组件根本无法履行契约，让它起来只会在第一个请求时才炸。
    """
    env = {
        "DATABASE_HOST": "pg",
        "DATABASE_NAME": "people",
        "DATABASE_USER": "u",
        "DATABASE_PASSWORD": "p",
    }

    with pytest.raises(ValueError) as excinfo:
        config_from_env(env.get)

    assert "DEPARTMENT_TREE_ENDPOINT" in str(excinfo.value)


def test_weak_dependency_endpoint_is_optional() -> None:
    """22.8：弱依赖用 os.environ.get() 安全读取，缺失不是错误。"""
    env = {
        "DATABASE_HOST": "pg",
        "DATABASE_NAME": "people",
        "DATABASE_USER": "u",
        "DATABASE_PASSWORD": "p",
        "DEPARTMENT_TREE_ENDPOINT": "http://department-tree-1-0-0:8080",
    }

    cfg = config_from_env(env.get)

    assert cfg.event_bus_endpoint is None


def test_missing_database_config_is_an_error() -> None:
    with pytest.raises(ValueError) as excinfo:
        config_from_env({}.get)

    message = str(excinfo.value)
    for name in ("DATABASE_HOST", "DATABASE_NAME", "DATABASE_USER"):
        assert name in message


def test_config_repr_hides_password() -> None:
    cfg = config_from_env(
        {
            "DATABASE_HOST": "pg",
            "DATABASE_NAME": "people",
            "DATABASE_USER": "u",
            "DATABASE_PASSWORD": "super-secret-password",
            "DEPARTMENT_TREE_ENDPOINT": "http://d:8080",
        }.get
    )

    assert "super-secret-password" not in str(cfg)
    assert "pg" in str(cfg)


def test_logs_are_json_with_component_id(capsys: pytest.CaptureFixture[str]) -> None:
    """22.x / 002 §11：日志必须是 JSON，且带 componentId。"""
    configure_logging("info", "people/basic")

    logging.getLogger("app").info("人员已加载", extra={"count": 3})

    captured = capsys.readouterr()
    entry = json.loads(captured.out.strip().splitlines()[-1])
    assert entry["componentId"] == "people/basic"
    assert entry["message"] == "人员已加载"
    assert entry["level"] == "info"


def test_logger_redacts_passwords(capsys: pytest.CaptureFixture[str]) -> None:
    configure_logging("info", "people/basic")

    logging.getLogger("app").info("连接数据库", extra={"password": "super-secret"})

    assert "super-secret" not in capsys.readouterr().out
