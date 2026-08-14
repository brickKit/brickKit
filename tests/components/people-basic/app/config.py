"""配置与日志。

组件的配置**只来自环境变量**（002 §1.4、006 §5.1）：
组件不知道也不该知道自己被部署在哪、连的是哪个库。
"""

from __future__ import annotations

import json
import logging
import sys
from dataclasses import dataclass
from typing import Callable
from urllib.parse import quote

# 主端口（HTTP）与额外端口（gRPC），与 component.yaml 一致。
HTTP_PORT = 8080
GRPC_PORT = 9090

# 日志里一律打码的字段（002 §11.3）。
SENSITIVE_KEYS = ("password", "token", "secret", "dsn", "key")


@dataclass(frozen=True)
class DatabaseConfig:
    host: str
    port: int
    name: str
    user: str
    password: str

    def dsn(self) -> str:
        """拼出连接串。口令做 URL 转义：强口令里的 @ : / 会把 DSN 拆到错误的主机上。"""
        return (
            f"postgresql://{quote(self.user, safe='')}:{quote(self.password, safe='')}"
            f"@{self.host}:{self.port}/{self.name}"
        )


@dataclass(frozen=True)
class Config:
    component_id: str
    version: str
    log_level: str
    database: DatabaseConfig
    # 强依赖 department/tree 的地址：平台按依赖关系注入（004 §5.6）。
    department_endpoint: str
    # 弱依赖 infra/redis-event-bus：**缺失是正常的**，平台完全不注入这个变量。
    event_bus_endpoint: str | None

    def __str__(self) -> str:
        return (
            f"component={self.component_id}@{self.version} "
            f"database={self.database.host}:{self.database.port}/{self.database.name} "
            f"user={self.database.user} department={self.department_endpoint} "
            f"eventBus={self.event_bus_endpoint or '（未注入，事件发布将跳过）'} "
            f"logLevel={self.log_level}"
        )


def config_from_env(lookup: Callable[[str], str | None]) -> Config:
    """从环境变量读配置。

    缺失项一次全部报出，且**绝不退化到默认地址**：悄悄连到 localhost
    会让人以为配好了，实际连的根本不是那个库。
    """

    def get(key: str) -> str:
        return (lookup(key) or "").strip()

    database = {
        "DATABASE_HOST": get("DATABASE_HOST"),
        "DATABASE_NAME": get("DATABASE_NAME"),
        "DATABASE_USER": get("DATABASE_USER"),
    }
    # 强依赖的地址与数据库同等重要：没有它，本组件无法履行契约
    department_endpoint = get("DEPARTMENT_TREE_ENDPOINT")

    missing = sorted(name for name, value in database.items() if not value)
    if not department_endpoint:
        missing.append("DEPARTMENT_TREE_ENDPOINT")
    if missing:
        raise ValueError(
            "缺少必需的环境变量：" + ", ".join(missing) +
            "（这些变量由平台按 brickkit.yaml 的资源绑定与依赖关系注入，见 004 §5.6、006 §5）"
        )

    raw_port = get("DATABASE_PORT")
    try:
        port = int(raw_port) if raw_port else 5432
    except ValueError as exc:
        raise ValueError(f"DATABASE_PORT 必须是整数（当前是 {raw_port!r}）") from exc

    return Config(
        component_id=get("COMPONENT_ID") or "people/basic",
        version=get("COMPONENT_VERSION") or "1.0.0",
        log_level=get("LOG_LEVEL") or "info",
        database=DatabaseConfig(
            host=database["DATABASE_HOST"],
            port=port,
            name=database["DATABASE_NAME"],
            user=database["DATABASE_USER"],
            password=get("DATABASE_PASSWORD"),
        ),
        department_endpoint=department_endpoint,
        # 002 §3.4：弱依赖用 get() 安全读取，缺失不是错误
        event_bus_endpoint=get("INFRA_REDIS_EVENT_BUS_ENDPOINT") or None,
    )


# ============================================================
# 日志（002 §11）
# ============================================================


class JSONFormatter(logging.Formatter):
    """把日志渲染成 JSON，并统一给敏感字段打码。

    靠"写日志的人记得别写口令"是不可靠的：连接失败时最想打印的就是 DSN，
    而 DSN 里正好带着口令。所以在出口处统一挡掉。
    """

    def __init__(self, component_id: str):
        super().__init__()
        self.component_id = component_id

    def format(self, record: logging.LogRecord) -> str:
        entry = {
            "time": self.formatTime(record, "%Y-%m-%dT%H:%M:%SZ"),
            "level": record.levelname.lower(),
            "componentId": self.component_id,
            "message": record.getMessage(),
        }

        standard = set(logging.LogRecord("", 0, "", 0, "", (), None).__dict__)
        standard.update({"message", "asctime", "taskName"})
        for key, value in record.__dict__.items():
            if key in standard:
                continue
            entry[key] = "***" if _is_sensitive(key) else value

        if record.exc_info:
            entry["error"] = self.formatException(record.exc_info).splitlines()[-1]
        return json.dumps(entry, ensure_ascii=False)


def _is_sensitive(key: str) -> bool:
    lowered = key.lower()
    return any(marker in lowered for marker in SENSITIVE_KEYS)


def configure_logging(level: str, component_id: str) -> None:
    """配置根日志器：JSON 格式、输出到 stdout（002 §11.3）。"""
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(JSONFormatter(component_id))

    root = logging.getLogger()
    root.handlers = [handler]
    root.setLevel(getattr(logging, level.upper(), logging.INFO))
