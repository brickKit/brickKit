"""people/basic 的入口。

它是一个**连接组件**的雏形：有自己的数据（人员），
强依赖 department/tree 补全部门名，弱依赖 infra/redis-event-bus 发事件。

组件开发约束（002 §1.4）：配置只从环境变量读、/healthz 只检查本进程、
日志为 JSON 输出到 stdout、容器不以 root 运行。
"""

from __future__ import annotations

import logging
import signal
import sys
import threading

import uvicorn

from app.config import GRPC_PORT, HTTP_PORT, config_from_env, configure_logging
from app.department import DepartmentClient
from app.events import build_event_bus
from app.grpc_api import serve_grpc
from app.http_api import create_app
from app.service import PeopleService
from app.store import PostgresStore, migrate

logger = logging.getLogger("app.main")


def main(argv: list[str]) -> int:
    import os

    try:
        cfg = config_from_env(os.environ.get)
    except ValueError as exc:
        # 配置错误发生在日志器建好之前，直接写 stderr
        print(f'{{"level":"error","message":"组件启动失败","error":"{exc}"}}', file=sys.stderr)
        return 1

    configure_logging(cfg.log_level, cfg.component_id)

    try:
        store = PostgresStore(cfg.database.dsn())
    except Exception as exc:  # noqa: BLE001
        logger.error("连接数据库失败", exc_info=exc)
        return 1

    # migrate 子命令：平台在启动组件之前单独跑一次（002 §8.2、005 §6）
    if argv and argv[0] == "migrate":
        logger.info("开始执行数据库迁移", extra={"config": str(cfg)})
        migrate(store)
        logger.info("迁移完成")
        return 0

    departments = DepartmentClient(cfg.department_endpoint)
    service = PeopleService(
        store=store,
        departments=departments,
        events=build_event_bus(cfg.event_bus_endpoint),
        component_id=cfg.component_id,
        version=cfg.version,
    )

    grpc_server, grpc_port = serve_grpc(service, GRPC_PORT)
    logger.info("gRPC 已就绪", extra={"port": grpc_port})

    stopping = threading.Event()

    def _shutdown(*_args) -> None:
        logger.info("收到停止信号，正在关闭")
        stopping.set()

    signal.signal(signal.SIGTERM, _shutdown)
    signal.signal(signal.SIGINT, _shutdown)

    logger.info("组件已就绪", extra={"httpPort": HTTP_PORT, "config": str(cfg)})
    try:
        uvicorn.run(create_app(service), host="0.0.0.0", port=HTTP_PORT, log_config=None)  # noqa: S104
    finally:
        grpc_server.stop(None)
        departments.close()
        store.close()
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
