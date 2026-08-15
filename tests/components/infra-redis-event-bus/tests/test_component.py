"""配置、日志与"组件对平台的承诺"。

覆盖 002 §1.4（配置只从环境变量读）、§11（JSON 日志、敏感字段脱敏），
以及 Manifest 声明与实现是否对得上。
"""

from __future__ import annotations

import ast
import json
import logging
from pathlib import Path

import pytest
import yaml

from app.config import DEFAULT_MAXLEN, DEFAULT_STREAM, config_from_env, configure_logging
from app.main import parse_args

ROOT = Path(__file__).resolve().parent.parent


def env_of(pairs: dict[str, str]):
    return lambda key: pairs.get(key)


def complete_env() -> dict[str, str]:
    return {"REDIS_HOST": "redis", "REDIS_PASSWORD": "redis-secret"}


# ============================================================
# 配置
# ============================================================


def test_config_from_env() -> None:
    cfg = config_from_env(env_of(complete_env()))

    assert cfg.redis.addr() == "redis:6379", "REDIS_PORT 缺省应为 6379"
    assert cfg.redis.stream == DEFAULT_STREAM
    assert cfg.redis.maxlen == DEFAULT_MAXLEN


def test_redis_is_required() -> None:
    """这个组件与 authorization/rbac 最大的差别。

    那里 Redis 是加速器，没绑定也能跑；**这里 Redis 是唯一的数据源，
    缺了必须启动失败**——一个连不上存储的事件总线，起来了也只会
    把每一条事件都丢掉，而发布方以为它们都安全落地了。
    """
    with pytest.raises(ValueError) as exc:
        config_from_env(env_of({}))

    assert "REDIS_HOST" in str(exc.value)
    # 要说清楚这个变量由谁负责给，否则使用者不知道该去改哪儿
    assert "cache" in str(exc.value) or "平台" in str(exc.value)


def test_config_never_falls_back_to_localhost() -> None:
    """绝不退化到默认地址：悄悄连到 localhost 会让人以为配好了，
    实际连的根本不是那个 Redis。"""
    with pytest.raises(ValueError):
        config_from_env(env_of({"REDIS_PASSWORD": "x"}))


@pytest.mark.parametrize(
    ("key", "value"),
    [("REDIS_PORT", "abc"), ("STREAM_MAXLEN", "many"), ("STREAM_MAXLEN", "0")],
)
def test_config_rejects_bad_numbers(key: str, value: str) -> None:
    env = complete_env() | {key: value}

    with pytest.raises(ValueError):
        config_from_env(env_of(env))


def test_stream_settings_are_overridable() -> None:
    env = complete_env() | {"STREAM_NAME": "custom:events", "STREAM_MAXLEN": "500"}

    cfg = config_from_env(env_of(env))
    assert cfg.redis.stream == "custom:events"
    assert cfg.redis.maxlen == 500


def test_config_string_has_no_password() -> None:
    """配置摘要会被打进日志，里面不能有口令。"""
    summary = str(config_from_env(env_of(complete_env())))

    assert "redis-secret" not in summary
    # 但该有的定位信息要在，否则这行日志就没用了
    assert "redis:6379" in summary
    assert DEFAULT_STREAM in summary


# ============================================================
# 日志
# ============================================================


def test_logs_are_json_with_component_id(capsys: pytest.CaptureFixture[str]) -> None:
    configure_logging("info", "infra/redis-event-bus")
    logging.getLogger("app.test").info("组件已就绪")

    entry = json.loads(capsys.readouterr().out.strip())
    assert entry["componentId"] == "infra/redis-event-bus"
    assert entry["message"] == "组件已就绪"


def test_logger_redacts_secrets(capsys: pytest.CaptureFixture[str]) -> None:
    """靠"写日志的人记得别写口令"是不可靠的——连接失败时最想打印的
    就是完整的连接信息，而那里面正好带着口令。"""
    configure_logging("info", "infra/redis-event-bus")
    logging.getLogger("app.test").warning(
        "连接失败",
        extra={"extra_fields": {"redis_password": "redis-secret", "stream": "brickkit:events"}},
    )

    out = capsys.readouterr().out
    assert "redis-secret" not in out
    assert "brickkit:events" in out, "流名不是秘密，不该被打码"


def test_log_level_is_configurable(capsys: pytest.CaptureFixture[str]) -> None:
    configure_logging("error", "infra/redis-event-bus")
    logging.getLogger("app.test").info("这条不该出现")

    assert capsys.readouterr().out == ""


# ============================================================
# 参数
# ============================================================


def test_parse_args_rejects_unknown() -> None:
    """拼错的命令不能悄悄变成正常启动（与 002 §8.5.1 同一条道理）。"""
    with pytest.raises(ValueError):
        parse_args(["migrate"])

    parse_args([])  # 不带参数是正常的


# ============================================================
# 组件对平台的承诺
# ============================================================


def test_component_yaml_declares_cache_resource() -> None:
    manifest = yaml.safe_load((ROOT / "component.yaml").read_text(encoding="utf-8"))

    resources = manifest["dependencies"]["resources"]
    assert any(r["kind"] == "cache" and r["engine"] == "redis" for r in resources)
    # 它是叶子组件：不依赖任何其他组件
    assert "components" not in manifest["dependencies"]


def test_component_yaml_has_no_migration() -> None:
    """事件存在 Redis 里，没有表，也就没有迁移。

    声明一个 migration 会让平台白起一个迁移容器。
    """
    manifest = yaml.safe_load((ROOT / "component.yaml").read_text(encoding="utf-8"))
    assert "migration" not in manifest


def test_declared_artifacts_exist() -> None:
    manifest = yaml.safe_load((ROOT / "component.yaml").read_text(encoding="utf-8"))

    for artifact in manifest.get("artifacts", []):
        for name in artifact.get("files", []):
            assert (ROOT / name).exists(), f"component.yaml 声明了 {name}，但文件不存在"


def test_openapi_covers_implemented_endpoints() -> None:
    doc = json.loads((ROOT / "openapi.json").read_text(encoding="utf-8"))

    for path in ("/healthz", "/api/v1/events"):
        assert path in doc["paths"], f"openapi.json 缺少端点 {path}"
    # 调用方（erp/backend）发的是 POST，读的是 GET，两个都要在
    assert "post" in doc["paths"]["/api/v1/events"]
    assert "get" in doc["paths"]["/api/v1/events"]


def test_dockerfile_runs_as_non_root() -> None:
    dockerfile = (ROOT / "Dockerfile").read_text(encoding="utf-8")

    assert "USER 10001" in dockerfile
    assert "USER root" not in dockerfile


def test_dockerfile_has_healthcheck_tool() -> None:
    """健康检查跑在容器**内部**，用的必须是镜像里真有的命令（002 §9.6）。

    python:slim 既没有 wget 也没有 curl——不装的话，组件明明跑得好好的，
    平台却判它 unhealthy，依赖方永远等不到它。people/basic 真跑起来撞到过。
    """
    dockerfile = (ROOT / "Dockerfile").read_text(encoding="utf-8")
    assert "curl" in dockerfile


def _code_strings(source: str) -> list[str]:
    """取出源码里**真正参与运行**的字符串字面量，跳过文档字符串。

    直接在整份源码里 grep "localhost" 是不行的：注释与文档字符串里
    经常要提到这些地址（"绝不退化到 localhost"就是一句必须写下来的说明）。
    粗匹配会一直误报，而一个总在误报的守卫，最后只会被人删掉——
    于是真正的硬编码也就没人拦了。
    """
    tree = ast.parse(source)

    docstrings: set[int] = set()
    for node in ast.walk(tree):
        if isinstance(node, (ast.Module, ast.ClassDef, ast.FunctionDef, ast.AsyncFunctionDef)):
            doc = ast.get_docstring(node, clean=False)
            if doc is not None and node.body:
                first = node.body[0]
                if isinstance(first, ast.Expr) and isinstance(first.value, ast.Constant):
                    docstrings.add(id(first.value))

    return [
        node.value
        for node in ast.walk(tree)
        if isinstance(node, ast.Constant)
        and isinstance(node.value, str)
        and id(node) not in docstrings
    ]


def test_no_hardcoded_addresses() -> None:
    """源码里不能写死地址：写死之后平台注入的 REDIS_HOST 会被静静忽略。"""
    for name in ("main.py", "config.py", "store.py", "http_api.py"):
        source = (ROOT / "app" / name).read_text(encoding="utf-8")

        for literal in _code_strings(source):
            for forbidden in ("localhost", "127.0.0.1", "redis:6379"):
                assert forbidden not in literal, (
                    f"{name} 的代码里出现了硬编码地址 {forbidden}：{literal!r}"
                )
