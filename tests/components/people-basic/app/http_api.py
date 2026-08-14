"""HTTP 出口（FastAPI，主端口 8080）。

主端口用于健康检查与平台注入的 PEOPLE_BASIC_ENDPOINT；
gRPC 在 extraPorts 声明的 9090 上（见 grpc_api.py）。
"""

from __future__ import annotations

from fastapi import FastAPI, Query, Request
from fastapi.responses import JSONResponse

from app.department import DepartmentUnavailable
from app.service import PeopleService, PersonView, StoreUnavailable


def _to_json(view: PersonView) -> dict:
    return {
        "id": view.id,
        "name": view.name,
        "departmentId": view.department_id,
        "departmentName": view.department_name,
        "title": view.title,
    }


def create_app(service: PeopleService) -> FastAPI:
    app = FastAPI(
        title="people/basic",
        version=service.version,
        description=(
            "人员组件的 HTTP REST 接口。gRPC 在 9090 端口（契约见 "
            "proto/people/v1/people.proto）。"
        ),
    )

    @app.exception_handler(StoreUnavailable)
    async def _store_unavailable(_: Request, exc: StoreUnavailable) -> JSONResponse:
        return JSONResponse(status_code=503, content={"error": str(exc)})

    @app.exception_handler(DepartmentUnavailable)
    async def _department_unavailable(_: Request, exc: DepartmentUnavailable) -> JSONResponse:
        # 强依赖不可用 → 本组件无法履行契约，如实报 503
        return JSONResponse(status_code=503, content={"error": str(exc)})

    @app.get("/healthz", tags=["platform"])
    def healthz() -> dict:
        """健康检查。

        002 §9.4：只检查本进程存活，**不查数据库、不调依赖组件**。
        健康检查一旦连库，数据库抖一下就会让所有组件被判死重启。
        """
        return {"status": "ok", "component": service.component_id, "version": service.version}

    @app.get("/api/v1/people", tags=["people"])
    def list_people(
        departmentId: str = Query(default="", description="只返回该部门的人员"),  # noqa: N803
    ) -> dict:
        people = service.list_people(departmentId)
        return {"people": [_to_json(p) for p in people], "total": len(people)}

    @app.get("/api/v1/people/{person_id}", tags=["people"])
    def get_person(person_id: str):
        person = service.get_person(person_id)
        if person is None:
            return JSONResponse(status_code=404, content={"error": "人员不存在"})
        return _to_json(person)

    return app
