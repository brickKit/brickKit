"""事件存储：Redis Streams。

为什么是 Stream 而不是 Pub/Sub：Pub/Sub 是"发出去就没了"——没有消费者在线时
消息直接丢弃，也无法回看。Stream 会把事件留在流里，消费方可以晚一点来读、
可以从某个 ID 之后接着读，排障时还能直接翻最近发生了什么。

**这里的 Redis 是唯一的数据源，不是加速器。** 与 authorization/rbac 正好相反：
那里连不上 Redis 就回源查库，这里连不上就什么也做不了，必须如实报错。
"""

from __future__ import annotations

import json
from typing import Any

import redis

# 事件里由本组件管理、不允许发布方覆盖的字段。
RESERVED_FIELDS = ("id",)


class EventStore:
    """把事件读写到一条 Redis Stream 上。"""

    def __init__(self, client: redis.Redis, stream: str, maxlen: int) -> None:
        self._client = client
        self._stream = stream
        self._maxlen = maxlen

    def append(self, event: dict[str, Any]) -> str:
        """把事件追加到流上，返回条目 ID。

        用 maxlen + approximate 裁剪：事件流会一直长，不裁剪迟早把 Redis 撑爆。
        approximate（`~`）让 Redis 按整节点裁剪，比精确裁剪快得多，
        代价只是实际长度会略多于 maxlen——对一条演示用的事件流完全够。
        """
        return self._client.xadd(
            self._stream,
            _encode(event),
            maxlen=self._maxlen,
            approximate=True,
        )

    def read(self, limit: int, event_type: str | None = None) -> list[dict[str, Any]]:
        """读最近的事件，最新的在前。

        按类型过滤在**读出来之后**做：Redis Stream 本身不支持按字段过滤。
        为此多读一些再筛——limit 是"最终要几条"，不是"从流里读几条"。
        """
        # 有过滤时多读几倍，尽量让筛完还能凑够 limit 条。
        # 上限 1000 是为了别在一条很长的流上一次读太多
        fetch = min(limit * 10, 1000) if event_type else limit
        entries = self._client.xrevrange(self._stream, count=fetch)

        out: list[dict[str, Any]] = []
        for entry_id, fields in entries:
            event = _decode(fields)
            event["id"] = entry_id
            if event_type and event.get("type") != event_type:
                continue
            out.append(event)
            if len(out) >= limit:
                break
        return out


def _encode(event: dict[str, Any]) -> dict[str, str]:
    """把事件编码成 Stream 的字段表。

    Stream 的字段值只能是字符串/字节。非字符串的值转成 JSON 存，
    读的时候再还原——**不丢字段**：事件总线不理解事件的内容，
    丢掉不认识的字段等于逼所有发布方都来改这个组件。
    """
    fields: dict[str, str] = {}
    for key, value in event.items():
        if key in RESERVED_FIELDS:
            continue
        fields[key] = value if isinstance(value, str) else json.dumps(value, ensure_ascii=False)
    return fields


def _decode(fields: dict[str, str]) -> dict[str, Any]:
    """还原字段表。存进去是什么样，读出来就是什么样。"""
    out: dict[str, Any] = {}
    for key, value in fields.items():
        out[key] = value
    return out
