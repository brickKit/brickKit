"""infra/redis-event-bus 的入口。

它是**很多组件的弱依赖**：people/basic 与 erp/backend 都声明了
`optional: true`。这意味着它缺席时那些组件应当照常工作，只是不发事件
（003 §4.3）——所以这个组件的可用性要求其实比看上去低，
但它自己**不能假装成功**：收不下的事件必须如实报错，
否则发布方会以为事件已经安全落地，再也不会重发。

与 authorization/rbac 的一个刻意对照：两者都绑 Redis，但那里 Redis 是
**加速器**（挂了照常回源），这里是**唯一的数据源**（挂了就 503）。

组件开发约束（002 §1.4）：配置只从环境变量读、/healthz 只检查本进程、
日志为 JSON 输出到 stdout、容器不以 root 运行。
"""

from __future__ import annotations

import logging
import os
import sys

import redis
import uvicorn

from app.config import HTTP_PORT, config_from_env, configure_logging
from app.http_api import create_app
from app.store import EventStore

logger = logging.getLogger("app.main")


def parse_args(argv: list[str]) -> None:
    """本组件不接受任何参数。

    **不认识的参数必须报错，绝不能默默启动服务**（与 002 §8.5.1 同一条道理）：
    一个拼错的命令悄悄变成正常启动，是最难查的一类问题。

    注意它没有 migrate 子命令——事件存在 Redis 里，没有表，
    因此 Manifest 里也没有 migration 字段。
    """
    if argv:
        raise ValueError(f"未知的参数：{argv[0]}（本组件不接受任何参数）")


def main(argv: list[str] | None = None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    try:
        parse_args(argv)
        cfg = config_from_env(os.environ.get)
    except ValueError as exc:
        # 启动阶段的失败先于日志器存在，直接写 stderr
        print(f'{{"level":"error","message":"组件启动失败","error":"{exc}"}}', file=sys.stderr)
        return 1

    configure_logging(cfg.log_level, cfg.component_id)

    client = redis.Redis(
        host=cfg.redis.host,
        port=cfg.redis.port,
        password=cfg.redis.password or None,
        decode_responses=True,
        # 连不上时快速失败：让调用方尽早拿到 503，而不是把连接挂在那儿
        socket_connect_timeout=3,
        socket_timeout=3,
    )
    store = EventStore(client, stream=cfg.redis.stream, maxlen=cfg.redis.maxlen)

    # **不在这里 ping Redis**：Redis 还没起来时本组件照样应该能启动，
    # 等真正收到事件时再报 503。启动时就连不上直接退出的话，
    # 编排系统会把它反复重启，而它其实只是在等另一个容器就绪
    logger.info("组件已就绪", extra={"extra_fields": {"addr": f":{HTTP_PORT}", "config": str(cfg)}})

    uvicorn.run(
        create_app(store, cfg),
        host="0.0.0.0",  # noqa: S104 —— 容器内必须监听所有网卡，否则同网络的组件连不上
        port=HTTP_PORT,
        log_config=None,
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
