"""gRPC 出口（grpcio，额外端口 9090）。

Python 的 grpcio 无法与 HTTP 共用一个端口（Go 组件可以，见 department/tree），
因此按 009 的建议在 component.yaml 里用 extraPorts 声明 9090。
平台据此注入 PEOPLE_BASIC_GRPC_ENDPOINT，调用方才知道 gRPC 在哪。
"""

from __future__ import annotations

from concurrent import futures

import grpc
from grpc_reflection.v1alpha import reflection

from app.department import DepartmentUnavailable
from app.service import PeopleService, PersonView, StoreUnavailable
from gen.people.v1 import people_pb2, people_pb2_grpc

MAX_WORKERS = 8


def _to_proto(view: PersonView) -> people_pb2.Person:
    return people_pb2.Person(
        id=view.id,
        name=view.name,
        department_id=view.department_id,
        department_name=view.department_name,
        title=view.title,
    )


class PeopleServicer(people_pb2_grpc.PeopleServiceServicer):
    """gRPC 服务实现：只做协议转换，业务逻辑在 PeopleService 里。"""

    def __init__(self, service: PeopleService):
        self._service = service

    def ListPeople(self, request, context):  # noqa: N802 —— gRPC 生成的方法名
        try:
            people = self._service.list_people(request.department_id)
        except (StoreUnavailable, DepartmentUnavailable) as exc:
            context.abort(grpc.StatusCode.UNAVAILABLE, str(exc))
        return people_pb2.ListPeopleResponse(
            people=[_to_proto(p) for p in people], total=len(people)
        )

    def GetPerson(self, request, context):  # noqa: N802
        try:
            person = self._service.get_person(request.id)
        except (StoreUnavailable, DepartmentUnavailable) as exc:
            context.abort(grpc.StatusCode.UNAVAILABLE, str(exc))
        if person is None:
            context.abort(grpc.StatusCode.NOT_FOUND, "人员不存在")
        return _to_proto(person)


def serve_grpc(service: PeopleService, port: int) -> tuple[grpc.Server, int]:
    """在指定端口上启动 gRPC 服务。port=0 表示随机端口（测试用）。"""
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=MAX_WORKERS))
    people_pb2_grpc.add_PeopleServiceServicer_to_server(PeopleServicer(service), server)

    # 反射：让 grpcurl 不带 .proto 也能列出并调用服务，排障时最省事
    reflection.enable_server_reflection(
        (people_pb2.DESCRIPTOR.services_by_name["PeopleService"].full_name,
         reflection.SERVICE_NAME),
        server,
    )

    bound = server.add_insecure_port(f"[::]:{port}")
    server.start()
    return server, bound
