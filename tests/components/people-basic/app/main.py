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
from app.store import PostgresStore

logger = logging.getLogger("app.main")

# 运行模式。迁移容器与主容器用的是同一个镜像，靠参数区分（002 §8.4）。
MODE_SERVE = "serve"
MODE_MIGRATE = "migrate"


def parse_args(argv: list[str]) -> tuple[str, list[str]]:
    """认参数：要么启动服务，要么执行迁移，没有第三种。

    **不认识的参数必须报错，绝不能回落到"那就启动服务吧"。** 否则一个拼错的
    迁移命令会让迁移容器变成服务容器：它永不退出，主服务永远等不到
    "迁移完成"，整个项目卡在 Created——而日志里写着"组件已就绪"（002 §8.5.1）。

    它是纯函数，不读环境变量、不连库：告诉使用者"参数写错了"这件事，
    不该先去连一个可能根本连不上的数据库。
    """
    if not argv:
        return MODE_SERVE, []
    if argv[0] == MODE_MIGRATE:
        return MODE_MIGRATE, argv[1:]
    raise ValueError(
        f"未知的参数：{argv[0]}（可用：不带参数启动服务 | migrate [down [n] | reset]）"
    )


def run_migrate(args: list[str], cfg, store, logger) -> int:
    """处理 migrate 及其子命令。

        migrate           执行尚未执行过的迁移
        migrate down [n]  回退最近 n 个（默认 1）
        migrate reset     全部回退（开发与测试用）

    down / reset 是给开发和测试用的，让人能反复把库搭起来、拆掉。
    生产环境的结构问题请用一个新的 up 迁移去修（002 §8.9）。
    """
    if not args:
        logger.info("开始执行数据库迁移", extra={"config": str(cfg)})
        executed = store.migrate(cfg.component_id)
        logger.info("迁移完成", extra={"applied": executed or "无新增迁移"})
        return 0

    command = args[0]
    if command == "down":
        count = 1
        if len(args) > 1:
            if not args[1].isdigit() or int(args[1]) < 1:
                logger.error("migrate down 的参数必须是正整数", extra={"got": args[1]})
                return 1
            count = int(args[1])
        reverted = store.rollback(cfg.component_id, count)
        logger.info("回退完成", extra={"reverted": reverted or "无可回退的迁移"})
        return 0

    if command == "reset":
        logger.warning("开始全部回退（该操作会删除本组件的表与数据）")
        reverted = store.rollback(cfg.component_id, 0)
        logger.info("已全部回退", extra={"reverted": reverted or "无可回退的迁移"})
        return 0

    logger.error("未知的 migrate 子命令", extra={"command": command, "usage": "down [n] | reset"})
    return 1


def main(argv: list[str]) -> int:
    import os

    try:
        mode, migrate_args = parse_args(argv)
    except ValueError as exc:
        # 参数错误先于日志器存在，直接写 stderr
        print(f'{{"level":"error","message":"组件启动失败","error":"{exc}"}}', file=sys.stderr)
        return 1

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
    if mode == MODE_MIGRATE:
        return run_migrate(migrate_args, cfg, store, logger)

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
