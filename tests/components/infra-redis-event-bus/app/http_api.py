"""HTTP 出口。

契约由**调用方**先定下：erp/backend 早在 Step 25 就在往
`POST {endpoint}/api/v1/events` 发事件了（见它的 eventbus.go）。
这里必须照着实现，不能反过来要求已经上线的调用方改。
"""

from __future__ import annotations

import logging
from datetime import datetime, timezone
from typing import Any

from fastapi import FastAPI, Query, Request
from fastapi.responses import JSONResponse
from pydantic import BaseModel, ConfigDict, Field

from app.config import Config

logger = logging.getLogger("app.http_api")

# 一次最多读多少条。不设上限的话，一个 limit=1000000 就能把内存吃干净
MAX_LIMIT = 500
DEFAULT_LIMIT = 50


class PublishRequest(BaseModel):
    """一条待发布的事件。

    `extra="allow"`：发布方带的额外字段原样收下。事件总线不理解事件的内容
    （002 的一贯立场：平台不解析业务语义），丢掉不认识的字段等于逼所有
    发布方都来改这个组件。
    """

    model_config = ConfigDict(extra="allow")

    type: str = Field(min_length=1, description="事件类型，如 erp.order.approved")
    actor: str = ""
    subject: str = ""
    time: str = ""


def create_app(store: Any, cfg: Config) -> FastAPI:
    app = FastAPI(
        title="infra/redis-event-bus",
        version=cfg.version,
        description="基于 Redis Streams 的事件总线。它是很多组件的**弱依赖**："
        "缺席时那些组件应当照常工作，只是不发事件。",
    )

    @app.get("/healthz")
    def healthz() -> dict[str, str]:
        """健康检查只回答"本进程还活着吗"（002 §9.4、开发计划 27.3）。

        **不 ping Redis。** Redis 在这里是唯一的数据源，比别处更容易让人
        想去探一探；但去探的话，Redis 一抖，编排系统就把这些本身完全正常的
        容器全杀掉重启——而重启并不会让 Redis 变好，只会让恢复之后
        还要多等一轮拉起。
        """
        return {"status": "ok"}

    @app.post("/api/v1/events", status_code=202)
    def publish(event: PublishRequest) -> Any:
        payload = event.model_dump()
        # 发布方没带时间戳时由本组件补上：宁可用"收到的时间"，
        # 也不要一条没有时间的事件——排障时时间往往是唯一能把
        # 几个组件的日志对起来的东西
        if not payload.get("time"):
            payload["time"] = datetime.now(timezone.utc).isoformat()

        try:
            entry_id = store.append(payload)
        except Exception:
            # Redis 是唯一的数据源，连不上就必须如实报——**不能假装收下了**。
            # 假装成功等于把事件悄悄丢掉，而发布方会以为它已经安全落地
            logger.warning("写入事件失败", extra={"extra_fields": {"type": payload.get("type")}})
            return _unavailable()

        # 202 而不是 200：事件已收下，但消费是异步的。
        # 用 200 会让发布方以为"已经被处理了"
        return JSONResponse(status_code=202, content={"id": entry_id, "type": payload["type"]})

    @app.get("/api/v1/events")
    def list_events(
        limit: int = Query(DEFAULT_LIMIT, ge=1, le=MAX_LIMIT),
        type: str | None = Query(None, description="按事件类型过滤"),
    ) -> Any:
        try:
            events = store.read(limit=limit, event_type=type)
        except Exception:
            logger.warning("读取事件失败")
            return _unavailable()

        # events 为空时返回 []，不是 null——弱类型的调用方遍历 null 会直接崩
        return {"events": events, "total": len(events)}

    @app.exception_handler(500)
    def _on_error(_request: Request, _exc: Exception) -> JSONResponse:
        return JSONResponse(status_code=500, content={"error": "服务内部错误"})

    return app


def _unavailable() -> JSONResponse:
    """统一的 503。**不带底层原因**：把 "connection refused" 透出去，
    既帮不上调用方，又把内部拓扑告诉了外面。"""
    return JSONResponse(
        status_code=503,
        content={"error": "事件存储暂时不可用，请稍后重试"},
    )
