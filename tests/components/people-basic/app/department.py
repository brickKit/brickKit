"""department/tree 客户端（强依赖）。

这是"契约即产物"的实际用法：department/tree 把 proto 作为 api-contract 发布，
本组件用 `brickkit add` 拿到它（落在 .brickkit/artifacts/department-tree-1-0-0/
api-contract/ 下），vendored 到 proto/vendor/ 后生成 gRPC 客户端。

跨语言就在这里体现：Go 写的服务端，Python 写的客户端，靠同一份 proto 对话。
"""

from __future__ import annotations

import logging

import grpc

from gen.vendor.department.v1 import department_pb2, department_pb2_grpc

logger = logging.getLogger("app.department")

# 单次调用超时。强依赖不可用时要**快速失败**，
# 让调用方立刻看到 503，而不是一直挂着等到网关超时。
CALL_TIMEOUT_SECONDS = 3.0


class DepartmentUnavailable(RuntimeError):
    """department/tree 不可用。"""


class DepartmentClient:
    """按部门 ID 查名称。

    **不做跨请求缓存**，这是踩过坑之后的决定：最初这里缓存了部门名，
    结果真容器验证时把 department/tree 停掉，接口照样返回 200——
    缓存把"强依赖已经挂了"这件事盖住了，同时也让"部门改名立刻生效"
    变成了一句空话（进程不重启就永远看不到新名字）。

    列表接口的 N+1 问题在服务层用**单次请求内的去重**解决（见 service.py），
    那既不会重复调用，也不会让数据变旧。
    """

    def __init__(self, endpoint: str):
        # 平台注入的地址形如 http://department-tree-1-0-0:8080，
        # gRPC 要的是 host:port，去掉 scheme
        self._target = endpoint.removeprefix("http://").removeprefix("https://").rstrip("/")
        self._channel = grpc.insecure_channel(self._target)
        self._stub = department_pb2_grpc.DepartmentServiceStub(self._channel)

    def close(self) -> None:
        self._channel.close()

    def name_of(self, department_id: str) -> str:
        if not department_id:
            return ""

        try:
            dept = self._stub.GetDepartment(
                department_pb2.GetDepartmentRequest(id=department_id),
                timeout=CALL_TIMEOUT_SECONDS,
            )
        except grpc.RpcError as exc:
            if exc.code() == grpc.StatusCode.NOT_FOUND:
                # 部门被删了：这不是依赖故障，人员本身仍然可读
                return ""
            logger.warning("调用 department/tree 失败", extra={"departmentId": department_id})
            raise DepartmentUnavailable(str(exc.code())) from exc

        return dept.name
