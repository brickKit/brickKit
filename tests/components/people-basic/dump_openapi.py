"""把 FastAPI 自动生成的 OpenAPI 文档导出为 openapi.json。

22.6 要求组件提供 OpenAPI 文档，而它同时是发布到市场的 api-docs 产物。
用生成而不是手写：手写的文档迟早和代码对不上。
"""

import json
import pathlib

from app.http_api import create_app
from app.service import PeopleService
from app.store import MemoryStore


class _NoDependencies:
    def name_of(self, department_id: str) -> str:
        return ""

    def publish(self, topic: str, payload: dict) -> None:
        return None


app = create_app(
    PeopleService(
        store=MemoryStore([]),
        departments=_NoDependencies(),
        events=_NoDependencies(),
        component_id="people/basic",
        version="1.0.0",
    )
)

doc = app.openapi()
pathlib.Path("openapi.json").write_text(
    json.dumps(doc, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
)
print(f"✅ openapi.json 已生成（{len(doc['paths'])} 个路径）")
