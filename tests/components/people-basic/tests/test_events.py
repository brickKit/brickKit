"""发往 infra/redis-event-bus 的请求，形状要是总线收的那种。

`tests/test_service.py` 用的是替身总线，只核对"发了什么事件"；这里起一个真的 HTTP 服务，
核对线上的请求体。总线的契约（它的 `BRICKKIT.md` 与 `openapi.json`）：`POST /api/v1/events`，
`type` 必填且不能为空，`actor`、`subject`、`time` 可选，其余字段原样存下；形状不对回 422。
而 `HTTPEventBus` 对 422 只记一条警告——请求体写错了，事件就悄悄丢了，没有别的地方会发现。
"""

from __future__ import annotations

import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import pytest
from fastapi.testclient import TestClient

from app.events import HTTPEventBus
from app.http_api import create_app
from tests.test_service import build_service

# 总线认的字段；别的字段它也收，但本组件没有理由发
BUS_FIELDS = {"type", "actor", "subject", "time"}


class FakeBus:
    """记下收到的每个请求体，按总线的规则回 202 或 422。"""

    def __init__(self) -> None:
        self.received: list[dict] = []
        bus = self

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self) -> None:  # noqa: N802 —— http.server 的方法名
                length = int(self.headers.get("Content-Length", "0"))
                body = json.loads(self.rfile.read(length) or b"{}")
                bus.received.append({"path": self.path, "body": body})
                ok = isinstance(body.get("type"), str) and body["type"] != ""
                self.send_response(202 if ok else 422)
                self.end_headers()

            def log_message(self, *args) -> None:
                return None

        self._server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.endpoint = f"http://127.0.0.1:{self._server.server_address[1]}"
        self._thread = threading.Thread(target=self._server.serve_forever, daemon=True)

    def __enter__(self) -> FakeBus:
        self._thread.start()
        return self

    def __exit__(self, *exc) -> None:
        self._server.shutdown()
        self._server.server_close()


@pytest.fixture()
def bus():
    with FakeBus() as fake:
        yield fake


def test_request_body_follows_the_bus_contract(bus: FakeBus) -> None:
    HTTPEventBus(bus.endpoint).publish("people.person.viewed", "p-001")

    assert len(bus.received) == 1
    request = bus.received[0]
    assert request["path"] == "/api/v1/events"
    assert request["body"] == {"type": "people.person.viewed", "subject": "p-001"}
    assert set(request["body"]) <= BUS_FIELDS


def test_viewing_a_person_reaches_the_bus(bus: FakeBus) -> None:
    """从 HTTP 查询一路到线上：查一个人，总线收到一条它会收下（202）的事件。"""
    client = TestClient(create_app(build_service(events=HTTPEventBus(bus.endpoint))))

    assert client.get("/api/v1/people/p-003").status_code == 200

    assert [r["body"] for r in bus.received] == [{"type": "people.person.viewed", "subject": "p-003"}]


def test_a_rejected_event_does_not_raise(bus: FakeBus) -> None:
    """总线回 422（或任何错误）时只记警告，不抛给调用方——可选依赖不能拖垮查询。"""
    HTTPEventBus(bus.endpoint).publish("", "p-001")

    assert len(bus.received) == 1
