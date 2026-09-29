# 契约与产物

## 组件的边界是它的契约

别人调用你的组件，需要知道的只有一件事：它提供什么接口。**契约**就是把这件事写成机器能读的文件——OpenAPI、proto、GraphQL schema、
一份事件格式说明。有了它，调用方可以生成客户端代码、写 mock、做联调，而不必读你的源码。

这对 AI 写代码尤其要紧：让 AI 写一个调用方，给它一份契约，它就知道每个接口的路径、参数和返回；给它一整个源码仓库，它得先猜哪些是对外的。

所以**先写契约，再写实现**：接口在契约里定下来，调用方和实现方可以同时开工；实现改了什么，对照契约就知道是不是破坏了约定。

## `artifacts`：把契约交出去

```yaml
artifacts:
  - type: api-contract
    format: openapi
    description: HTTP 接口
    files:
      - api/openapi.yaml
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `type` | ✅ | 这一组文件是什么：`api-contract`、`sdk`、`docs`……自由字符串 |
| `files` | ✅ | 文件路径，相对仓库根 |
| `format` | | `openapi`、`protobuf`、`json`……自由字符串 |
| `description` | | 给人看的说明 |

平台不理解 `type` 与 `format`，也不解析契约的内容——它只负责把 `files` 原样送到使用方手里。所以你可以交出任何格式的契约。

使用方 `add` 你的组件时，这些文件落到他项目的 `.brickkit/artifacts/<版本化服务名>/<type>/` 下，项目地图 `BRICKKIT.md` 里列着路径：

```text
| demo/quote | 0.1.0 | `.brickkit/manifests/demo/quote/0.1.0/BRICKKIT.md` | `.brickkit/artifacts/demo-quote-0-1-0/` |
```

目录名带着版本：调用方的客户端是照哪个版本的契约写的，一眼可知。

## 契约里写什么

`demo/quote` 的契约：

```yaml
openapi: 3.0.3
info:
  title: demo/quote
  version: 0.1.0
  description: 每次返回一句名言，带上 demo/hello 的问候语
paths:
  /api/v1/quote:
    get:
      summary: 取一句名言
      responses:
        "200":
          description: 问候语与名言
          content:
            application/json:
              schema:
                type: object
                properties:
                  greeting: { type: string }
                  quote: { type: string }
```

- `info.version` 跟着组件版本走。
- 每个对外接口都在里面；内部接口不写——写了就成了约定，以后就不能随便改。
- 契约与实现在同一个仓库、同一个 tag 里发布：拿到哪个版本的组件，就拿到哪个版本的契约。

**闭源组件更要提供契约。** 使用方看不到源码，契约是他能看到的唯一的接口说明。

## `brickkit fetch`：只要契约，不装组件

另一个项目要调用 `demo/quote`，但 `demo/quote` 由你的项目部署——他只需要契约来写客户端，把组件装进他的项目只会在他那边再部署一份。
这时用 `fetch`：

```bash
brickkit fetch demo/quote
```

```text
🔎 未指定版本，解析到 demo/quote@0.1.0（来自安装源 company-git，类型 git）
📦 已下载 demo/quote@0.1.0 的产物（未写入 brickkit.yaml）
   .brickkit/artifacts/demo-quote-0-1-0/
     api-contract/api/openapi.yaml
```

它只下载 `component.yaml` 和契约文件：不改 `brickkit.yaml`，不生成部署文件，不拉镜像，不启动任何东西。

跨项目调用时，平台不会为对方的组件注入 `*_ENDPOINT`——它不在你的依赖图里。调用方要把对方的地址声明成自己的一项配置
（`configSchema` 里一个必填、没有默认值的键），由使用方在 `config/` 里填上（见 [configSchema 设计准则](03-config-schema-design.md#好的设计)）。

`.brickkit/` 不进 Git：队友在自己机器上执行同一条 `fetch`；要让契约随项目提交，把需要的文件复制到你们自己的目录里。
