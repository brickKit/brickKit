"""业务逻辑。

HTTP（FastAPI，8080）与 gRPC（grpcio，9090）都只是这里的薄壳：
两边各写一遍查询，迟早出现"HTTP 说有、gRPC 说没有"。
"""

from __future__ import annotations

import logging
from dataclasses import dataclass

from app.department import DepartmentUnavailable
from app.store import Person, Store

logger = logging.getLogger("app.service")


class StoreUnavailable(RuntimeError):
    """数据存储不可用。"""


@dataclass
class PersonView:
    """对外的人员视图：本组件的数据 + 从 department/tree 补全的部门名。"""

    id: str
    name: str
    department_id: str
    department_name: str
    title: str


class PeopleService:
    def __init__(self, store: Store, departments, events, component_id: str, version: str):
        self._store = store
        self._departments = departments
        self._events = events
        self.component_id = component_id
        self.version = version

    # ------------------------------------------------------------
    # 查询
    # ------------------------------------------------------------

    def list_people(self, department_id: str = "") -> list[PersonView]:
        people = self._load(lambda: self._store.list(department_id))

        # 单次请求内按部门去重：100 个人分布在 5 个部门，只调 5 次 department/tree。
        # 去重只活在这一次调用里，因此不会让数据变旧（见 department.py 的说明）。
        memo: dict[str, str] = {}
        return [self._enrich(p, memo) for p in people]

    def get_person(self, person_id: str) -> PersonView | None:
        person = self._load(lambda: self._store.get(person_id))
        if person is None:
            return None

        view = self._enrich(person)
        # 弱依赖：发不出去也不影响这次查询
        self._publish_safely("people.person.viewed", {"personId": person.id})
        return view

    # ------------------------------------------------------------
    # 内部
    # ------------------------------------------------------------

    def _load(self, query):
        try:
            return query()
        except Exception as exc:  # noqa: BLE001 —— 存储实现可能抛任何异常
            # 真实原因留在服务端日志里，对外只说"暂时不可用"（002 §11.3）
            logger.error("查询人员数据失败", exc_info=exc)
            raise StoreUnavailable("人员数据暂时不可用") from exc

    def _enrich(self, person: Person, memo: dict[str, str] | None = None) -> PersonView:
        """补全部门名。

        强依赖不可用时**如实报错**而不是返回一个没有部门名的人员：
        假装成功会让调用方以为"这个人真的没有部门"。
        """
        if memo is not None and person.department_id in memo:
            department_name = memo[person.department_id]
        else:
            try:
                department_name = self._departments.name_of(person.department_id)
            except DepartmentUnavailable as exc:
                raise DepartmentUnavailable("部门信息暂时不可用") from exc
            if memo is not None:
                memo[person.department_id] = department_name

        return PersonView(
            id=person.id,
            name=person.name,
            department_id=person.department_id,
            department_name=department_name,
            title=person.title,
        )

    def _publish_safely(self, topic: str, payload: dict) -> None:
        try:
            self._events.publish(topic, payload)
        except Exception as exc:  # noqa: BLE001 —— 弱依赖的任何异常都不该冒泡
            logger.warning("事件发布失败，已跳过", extra={"topic": topic, "reason": str(exc)})
