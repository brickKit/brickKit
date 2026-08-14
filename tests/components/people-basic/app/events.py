"""infra/redis-event-bus 客户端（弱依赖）。

002 §3.4 的弱依赖约定：平台在该组件未启动时**完全不注入**
INFRA_REDIS_EVENT_BUS_ENDPOINT，因此这里必须用"取不到就降级"的方式读，
而不是假设变量一定存在。

弱依赖出错也绝不能影响主流程——这正是"弱"的含义。
"""

from __future__ import annotations

import json
import logging
import urllib.error
import urllib.request

logger = logging.getLogger("app.events")

PUBLISH_TIMEOUT_SECONDS = 1.0


class NullEventBus:
    """事件总线没启动时用的空实现：什么都不做，也不报错。"""

    def publish(self, topic: str, payload: dict) -> None:
        return None


class HTTPEventBus:
    """通过 HTTP 向事件总线投递事件。"""

    def __init__(self, endpoint: str):
        self._endpoint = endpoint.rstrip("/")

    def publish(self, topic: str, payload: dict) -> None:
        body = json.dumps({"topic": topic, "payload": payload}).encode("utf-8")
        request = urllib.request.Request(
            f"{self._endpoint}/api/v1/events",
            data=body,
            headers={"Content-Type": "application/json"},
        )
        try:
            with urllib.request.urlopen(request, timeout=PUBLISH_TIMEOUT_SECONDS):
                return None
        except (urllib.error.URLError, OSError) as exc:
            # 事件发不出去只记一条警告：调用方的查询本身没有任何问题
            logger.warning("事件发布失败，已跳过", extra={"topic": topic, "reason": str(exc)})
            return None


def build_event_bus(endpoint: str | None):
    """按平台是否注入了地址来决定用哪个实现。"""
    if not endpoint:
        logger.info("未注入事件总线地址，事件发布将被跳过（弱依赖降级）")
        return NullEventBus()
    return HTTPEventBus(endpoint)
