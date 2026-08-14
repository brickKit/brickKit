"""组件入口对参数的处理。

这看着像小事，实则是平台级的坑：迁移容器与主容器用的是**同一个镜像**，
只靠命令行参数区分。入口把不认识的参数当成"那就启动服务吧"的话，
一个拼错的迁移命令就会让迁移容器永不退出，主服务永远等不到"迁移完成"，
整个项目卡在 Created——而日志里写着"组件已就绪"，看起来一切正常。
真跑起来撞到过，见 002 §8.5.1。
"""

from __future__ import annotations

import pytest

from app.main import MODE_MIGRATE, MODE_SERVE, parse_args


def test_no_arguments_means_serve() -> None:
    assert parse_args([]) == (MODE_SERVE, [])


def test_migrate_arguments_are_passed_through() -> None:
    assert parse_args(["migrate", "down", "2"]) == (MODE_MIGRATE, ["down", "2"])


def test_unknown_argument_is_rejected() -> None:
    """核心用例：不认识的参数必须报错，绝不能回落到"启动服务"。"""
    with pytest.raises(ValueError) as excinfo:
        parse_args(["migrate-does-not-exist"])

    # 错误信息里要带上那个参数，才知道是哪里拼错了
    assert "migrate-does-not-exist" in str(excinfo.value)


def test_arguments_are_validated_without_environment() -> None:
    """参数校验必须发生在读环境变量、连数据库之前。

    否则为了告诉使用者"参数写错了"，得先去连一个可能根本连不上的库，
    给出的却是一句误导的"连接数据库失败"。parse_args 是纯函数，
    这条用例把这个约束钉住。
    """
    with pytest.raises(ValueError):
        parse_args(["bogus"])
