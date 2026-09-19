# 7. 消费别人的组件

一个组件的 `component.yaml` 可以声明 `artifacts`——调用方要真正对接它需要的文件（一份 OpenAPI 规范、一份 protobuf 契约、一份 SDK），跟 Manifest 本身是分开的（AGENTS.zh.md §6）。这一篇讲这些文件最终落在哪、怎么在完全不安装这个组件的情况下拿到它们，以及一个已经写好的真实组件——它整个存在的意义就是把这类东西展示给别人看。

## 一条产物声明长什么样

[`demo/hello`](../../../tests/components/demo-hello/) 的 Manifest：

```yaml
artifacts:
  - type: api-docs
    format: openapi
    description: HTTP API 文档
    files:
      - openapi.json
```

`type` 和 `format` 都是自由字符串，平台从不解读它们（AGENTS.zh.md §6）——这里是 `api-docs`/`openapi`，换成一个 gRPC 服务可能就是 `api-contract`/`protobuf`。平台唯一的职责是确保 `files` 里的东西真的能送到要它的人手里。

## `add` 会把它放在哪

前面每一篇的 `brickkit add` 输出其实都已经展示过这件事，只是没有直接指出来：

```
📦 添加 demo/hello@1.0.0
   ├── Manifest ✅
   └── artifacts ✅（1 个文件）
```

```bash
cat .brickkit/artifacts/demo-hello-1-0-0/api-docs/openapi.json
```

```json
{
  "openapi": "3.0.0",
  "info": { "title": "demo/hello", "version": "1.0.0" },
  "paths": {
    "/healthz": { "get": { "summary": "存活检查", "responses": { "200": { "description": "ok" } } } },
    "/api/v1/hello": { "get": { "summary": "问候", "responses": { "200": { "description": "ok" } } } },
    "/api/v1/env": { "get": { "summary": "回显平台注入的环境变量", "responses": { "200": { "description": "ok" } } } }
  }
}
```

一份真实、完整的 OpenAPI 文档，落在这个组件自己的版本化服务名底下——随时可以喂给一个客户端生成器，不需要这个组件的源码，也不需要它真的在跑。

## 什么都不安装，只拿产物

`brickkit fetch` 是专门为这个场景设计的独立命令：你需要另一个项目里某个组件的契约来写客户端，但完全没打算自己去跑这个组件（AGENTS.zh.md §8）：

```bash
brickkit fetch demo/hello@1.0.0
```

```
📦 已下载 demo/hello@1.0.0 的产物（未写入 brickkit.yaml）
   .brickkit/artifacts/demo-hello-1-0-0/
     api-docs/openapi.json

💡 这个组件不会被本项目部署。要连它，把对方给的地址填进依赖方的 config
   （跨项目共用组件）
```

`brickkit.yaml` 的 `components:` 列表完全没动——`fetch` 只写文件，从来不写配置。这正是它跟 `add` 真正的区别：`add` 说的是"我要跑这个"，`fetch` 说的是"我只需要知道它长什么样"。

## 一个整个存在的意义就是干这件事的组件

[`infra/api-docs`](../../../tests/components/infra-api-docs/) 专门用来把别的组件的文档聚合到一个可浏览的入口里——它的每一条依赖都是弱依赖，而且是故意的，因为一个"某个业务组件没装就直接打不开"的文档中心，等于彻底违背了自己存在的意义：

```bash
brickkit up   # 只有 infra/api-docs 自己，它弱依赖的东西一个都没装
curl http://localhost:8095/api/v1/sources
```

```json
{"sources":[
  {"componentId":"auth/password-login","status":"absent","reason":"该组件未安装（平台没有注入它的地址）","kinds":[]},
  {"componentId":"authorization/rbac","status":"absent","reason":"该组件未安装（平台没有注入它的地址）","kinds":[]},
  {"componentId":"department/tree","status":"absent","reason":"该组件未安装（平台没有注入它的地址）","kinds":[]},
  {"componentId":"erp/backend","status":"absent","reason":"该组件未安装（平台没有注入它的地址）","kinds":[]},
  {"componentId":"infra/redis-event-bus","status":"absent","reason":"该组件未安装（平台没有注入它的地址）","kinds":[]},
  {"componentId":"people/basic","status":"absent","reason":"该组件未安装（平台没有注入它的地址）","kinds":[]}
],"total":6}
```

不是报错，也不是一片空白——是一份干净、完整的答复，点名它认得的每一个组件，并且说清楚每一个现在为什么没显示出来。以后随便装上其中一个，同一个接口就会把那一个报成可用，`infra/api-docs` 这一侧完全不需要重启祈祷——它做的事只是读一下它环境里有没有对应的 `*_ENDPOINT` 变量（AGENTS.zh.md §5.3），跟这个系列从第 2 篇开始，每一个弱依赖用的都是同一套机制。

---

下一篇：[管理组件源码](08-component-source.md)——把别人的组件源码克隆下来、只留手边要动的那几个、改完推回去、用完删干净，以及源码跟着项目一起提交时的提交钩子。
