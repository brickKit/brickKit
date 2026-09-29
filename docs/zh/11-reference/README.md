# 参考手册

精确的规格：每个字段的类型、是否必填、缺省值、校验器真正套用的规则。适合查一个具体问题，不适合从头读。

和 [字段速查](../01-three-layers/09-field-reference.md) 的区别：速查一页看完全部字段、每个一句话；这里每个字段都写全规则，而且与代码里的结构逐条对应——
结构体多一个字段、文档漏写一行，测试就会失败。

| 篇目 | 讲什么 |
| --- | --- |
| [01 component.yaml](01-component-yaml-schema.md) | 组件 Manifest 的每个字段 |
| [02 brickkit.yaml](02-brickkit-yaml-schema.md) | 项目声明的每个字段 |
| [03 部署文件](03-deploy-yaml-schema.md) | `deploy.yaml` / `deploy.local.yaml` / `deploy.<环境>.yaml` 的每个字段 |
| [04 configSchema 规格](04-config-schema-spec.md) | 配置说明书的写法；平台检查键名、不检查值 |
| [05 JSON Schema](05-json-schemas.md) | `schemas/` 下的三份 JSON Schema，怎么接进编辑器 |
| [06 Market API](06-market-api.md) | 组件市场的 HTTP 接口（可选的基础设施） |
