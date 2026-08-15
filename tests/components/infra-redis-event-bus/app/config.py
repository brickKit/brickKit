"""配置与日志。

组件的配置**只来自环境变量**（002 §1.4、006 §5.1）：
组件不知道也不该知道自己被部署在哪、连的是哪个 Redis。
"""

from __future__ import annotations

import json
import logging
import sys
from dataclasses import dataclass
from typing import Callable

# 主端口，与 component.yaml 的 deployment.port 一致。
HTTP_PORT = 8080

# 日志里一律打码的字段（002 §11.3）。
SENSITIVE_KEYS = ("password", "token", "secret", "dsn")

# 事件流的默认名与默认长度上限。
DEFAULT_STREAM = "brickkit:events"
DEFAULT_MAXLEN = 10000


@dataclass(frozen=True)
class RedisConfig:
    host: str
    port: int
    password: str
    stream: str
    maxlen: int

    def addr(self) -> str:
        return f"{self.host}:{self.port}"


@dataclass(frozen=True)
class Config:
    component_id: str
    version: str
    log_level: str
    redis: RedisConfig

    def __str__(self) -> str:
        """可安全写进日志的摘要：有地址与流名，**没有口令**。"""
        return (
            f"component={self.component_id}@{self.version} "
            f"redis={self.redis.addr()} stream={self.redis.stream} "
            f"maxlen={self.redis.maxlen} logLevel={self.log_level}"
        )


def config_from_env(getenv: Callable[[str], str | None]) -> Config:
    """从环境变量读配置。

    缺失项一次全部报出，且**绝不退化到默认地址**：悄悄连到 localhost
    会让人以为配好了，实际连的根本不是那个 Redis。

    与 authorization/rbac 的一个关键差别：那里 Redis 是加速器，没绑定也能跑；
    **这里 Redis 是唯一的数据源，缺了就必须启动失败**——一个连不上存储的
    事件总线，起来了也只会把每一条事件都丢掉。
    """

    def get(key: str) -> str:
        return (getenv(key) or "").strip()

    host = get("REDIS_HOST")
    if not host:
        raise ValueError(
            "缺少必需的配置：REDIS_HOST（由平台按 cache 资源绑定注入，见 006 §5.2）。"
            "本组件把 Redis 作为唯一的数据源，没有它无法工作"
        )

    port = 6379
    if raw := get("REDIS_PORT"):
        try:
            port = int(raw)
        except ValueError as exc:
            raise ValueError(f"REDIS_PORT 必须是整数（当前是 {raw!r}）") from exc

    maxlen = DEFAULT_MAXLEN
    if raw := get("STREAM_MAXLEN"):
        try:
            maxlen = int(raw)
        except ValueError as exc:
            raise ValueError(f"STREAM_MAXLEN 必须是整数（当前是 {raw!r}）") from exc
        if maxlen < 1:
            raise ValueError(f"STREAM_MAXLEN 必须是正整数（当前是 {raw!r}）")

    return Config(
        component_id=get("COMPONENT_ID") or "infra/redis-event-bus",
        version=get("COMPONENT_VERSION") or "1.0.0",
        log_level=get("LOG_LEVEL") or "info",
        redis=RedisConfig(
            host=host,
            port=port,
            password=get("REDIS_PASSWORD"),
            stream=get("STREAM_NAME") or DEFAULT_STREAM,
            maxlen=maxlen,
        ),
    )


class JSONFormatter(logging.Formatter):
    """JSON 日志（002 §11）。每条都带 componentId：一个项目里跑着十几个组件，
    没有这个字段就没法在聚合日志里把它们分开。"""

    def __init__(self, component_id: str) -> None:
        super().__init__()
        self._component_id = component_id

    def format(self, record: logging.LogRecord) -> str:
        entry = {
            "time": self.formatTime(record, "%Y-%m-%dT%H:%M:%SZ"),
            "level": record.levelname.lower(),
            "componentId": self._component_id,
            "message": record.getMessage(),
        }
        for key, value in getattr(record, "extra_fields", {}).items():
            entry[key] = _redact(key, value)
        return json.dumps(entry, ensure_ascii=False)


def _redact(key: str, value: object) -> object:
    """口令一类的字段一律打码。

    靠"写日志的人记得别写口令"是不可靠的——连接失败时最想打印的就是
    完整的连接信息，而那里面正好带着口令。所以在出口处统一挡掉。
    """
    lowered = key.lower()
    return "***" if any(s in lowered for s in SENSITIVE_KEYS) else value


def configure_logging(level: str, component_id: str) -> None:
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(JSONFormatter(component_id))

    root = logging.getLogger()
    root.handlers = [handler]
    root.setLevel(getattr(logging, level.upper(), logging.INFO))
