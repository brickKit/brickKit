# 7. Consume Someone Else's Component

A component's `component.yaml` can declare `artifacts` — files a caller needs to actually integrate with it (an OpenAPI spec, a protobuf contract, an SDK), separate from the Manifest itself (AGENTS.md §6). This article covers where those files actually end up, how to grab them without installing the component at all, and a real, already-built component whose entire job is displaying exactly this kind of thing for others.

## What an artifact declaration looks like

[`demo/hello`](../../../tests/components/demo-hello/)'s Manifest:

```yaml
artifacts:
  - type: api-docs
    format: openapi
    description: HTTP API 文档
    files:
      - openapi.json
```

`type` and `format` are free-form strings the platform never interprets (AGENTS.md §6) — `api-docs`/`openapi` here, `api-contract`/`protobuf` elsewhere for a gRPC service. The platform's only job is making sure `files` actually gets to whoever asks for it.

## Where `add` puts it

Every earlier article's `brickkit add` output already showed this happening, just without pointing at it directly:

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

A real, complete OpenAPI document, sitting on disk under the component's own versioned service name — ready to feed to a client generator, without needing the component's source, and without needing it to even be running.

## Getting artifacts without installing anything

`brickkit fetch` is a separate command specifically for this: when you need another project's component's contract to write a client against, but you have no intention of running that component yourself (AGENTS.md §8):

```bash
brickkit fetch demo/hello@1.0.0
```

```
📦 已下载 demo/hello@1.0.0 的产物（未写入 brickkit.yaml）
   .brickkit/artifacts/demo-hello-1-0-0/
     api-docs/openapi.json

💡 这个组件不会被本项目部署。要连它，把对方给的地址填进依赖方的 config
   （003 §4.9 跨项目共用组件）
```

`brickkit.yaml`'s `components:` list is untouched — `fetch` writes files, never config. This is the real difference from `add`: `add` says "I want to run this," `fetch` says "I just need to know its shape."

## A component whose entire job is exactly this

[`infra/api-docs`](../../../tests/components/infra-api-docs/) exists specifically to aggregate other components' docs into one browsable entry point — and every one of its dependencies is optional, on purpose, because a documentation hub that refuses to load just because one business component isn't installed defeats its own point:

```bash
brickkit up   # infra/api-docs alone, nothing it optionally depends on installed
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

Not an error, not a blank page — a clean, complete answer naming every component it knows how to display and exactly why each one isn't showing up right now. Install any of them later, and the same endpoint would report that one as available instead, with no restart-and-hope involved on `infra/api-docs`'s side — it's just reading `*_ENDPOINT` variables that either are or aren't in its environment (AGENTS.md §5.3), the same mechanism every optional dependency in this series has used from Article 2 onward.

---

Next in this series (see [the guide index](README.md)): publishing a component to a marketplace, and installing it from there instead of a local source.
