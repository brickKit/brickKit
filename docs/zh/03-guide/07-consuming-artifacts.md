# 7. 消费别人的组件

一个组件的 `component.yaml` 可以声明 `artifacts`——调用方要真正对接它需要的文件（一份 OpenAPI 规范、一份 protobuf 契约、一份 SDK），跟 Manifest 本身是分开的（AGENTS.zh.md §6）。这一篇讲这些文件最终落在哪、怎么在完全不安装这个组件的情况下拿到它们，讲一个已经写好的真实组件——它整个存在的意义就是把这类东西展示给别人看，最后讲依赖的组件还没做好时，怎么先立一个带着约定契约的桩顶上。

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

## 上游还没做好：先立一个桩

设想这样一件事：你在写 `demo/caller`，它要调用 `demo/hello@1.0.0`；可 `demo/hello` 归另一个团队，他们还没做完，或者做完了还没发布。两边不可能同时收工，但有一件事完全可以先谈妥——**接口契约**，也就是把"对方会提供哪些网址、每个网址返回什么样的数据"白纸黑字写下来。这里的契约是一份 OpenAPI 文件（OpenAPI 是描述 HTTP 接口的通用格式：有哪些路径、用什么方法、返回什么）。

不必干等：先造一个替身顶着，真上游到了再换掉。这里有两样东西名字很像，必须分清：

| | 桩（stub） | mock |
| --- | --- | --- |
| 是什么 | 一个**组件**：`components/demo/hello/` 下的一份 `component.yaml` 加一份契约文件。ID、版本号跟真组件一样，但不干活 | 一个**进程**：真在你机器上监听某个端口、按契约返回编造数据的程序 |
| 谁认识它 | BrickKit——依赖图、地址注入都围着它转 | 只有网络——BrickKit 不知道它存在 |
| 谁来做 | `brickkit new` 生成骨架，你补上契约 | 你自己写，或者用现成工具 |

好处很直接：今天就能对着约定好的接口开发、联调 `demo/caller`，真上游到了它一行都不用改。代价也要说清楚：mock 只懂契约里写了的东西，真组件跟契约有出入的地方，你会很晚才撞上；桩是假的，不能留到上线；桩和 mock 是你自己多背的两样东西，得记着回头拆。

### 先交代一个边界：平台只认识桩

BrickKit 不提供 `brickkit mock` 这样的命令，`up` 也没有"缺哪个强依赖就自动补一个替身"的开关。这不是没来得及做，理由有三条：

1. **平台从不读契约的内容。** `artifacts` 里的 `format` 只是个字符串，平台原样带着走，从不解读（AGENTS.zh.md §6）。要"按契约生成 mock"，就得读懂 OpenAPI、protobuf、gRPC，以及以后冒出来的每一种格式，永远做不完；而专门干这件事的现成工具早就有了。平台只负责一件小事：把地址送到你启动的那个进程手里。
2. **自动换替身跟两条原则冲突。** 强依赖缺失本来就该让 `up` 停下来，而不是悄悄糊过去（§5.3）；显式优于隐式（§4）——一次误用，一个只会返回编造数据的组件就被真的部署上去了。下面这条路是显式的：替身写在 `brickkit.yaml` 里，评审看得见；它不生成任何容器；项目一旦切到 Kubernetes，只要 `local: true` 还在就会被拒绝（集群里的 Pod 够不着你机器上的进程，CLI 会直接说明并停下）。
3. **mock 换个名字起，也接不到流量。** `demo/caller` 拿到的地址，是用真组件的版本化服务名拼出来的：`demo-hello-1-0-0`（§5.1）。桩保住了这个名字，`extra_hosts` 再把这个名字指向你的机器；一个用别的名字应答的 mock，根本没有人会去找它。

所以分工是：桩负责"把地址接对"，mock 负责"真有东西在应答"，各管各的。

### 第 1 步：立桩

先在仓库根目录下新建一个项目目录（下面 `../tests/...` 这样的路径，默认它跟 `tests/` 并排，第 1 篇也是这么放的），把消费方 [`demo/caller`](../../../tests/components/demo-caller/) 放进去：

```bash
mkdir contract-first && cd contract-first
brickkit init hello-world

mkdir -p components/demo/caller
cp ../tests/components/demo-caller/component.yaml components/demo/caller/
cp ../tests/components/demo-caller/openapi.json components/demo/caller/
```

它的 Manifest 里有这么一段依赖：

```yaml
dependencies:
  components:
    - demo/hello@1.0.0
    - id: demo/bus@1.0.0
      optional: true
```

`demo/hello@1.0.0` 是写死了版本号的强依赖。这时执行 `brickkit add --local` 会被拒绝：所有安装源里都还没有 `demo/hello`，没有东西可装。

立桩：

```bash
brickkit new demo/hello --contract openapi
```

```
✅ 已生成组件骨架：demo/hello
   📄 components/demo/hello/component.yaml
   📄 components/demo/hello/api/openapi.yaml

下一步：
  改完骨架里的 TODO
  brickkit add --local               把它加进 brickkit.yaml（本地安装源里能扫到它的话）
  brickkit up --dry-run               校验能不能通过
```

`components/demo/hello/` 就是 `init` 配好的 `local-dev` 安装源本来就会扫的目录。里面多了两个文件：一份原样就能通过校验的 `component.yaml` 骨架，和一份占位的契约 `api/openapi.yaml`。骨架已经把这份契约登记进了 `artifacts`（跟本篇开头 `demo/hello` 的产物声明是同一种写法），所以 `add` 之后它会跟别的组件的产物一样，被拷到 `.brickkit/artifacts/` 下：

```yaml
artifacts:
  - type: api-contract
    format: openapi
    files:
      - api/openapi.yaml
```

骨架还要改两处才算一个能用的桩。

**版本号。** 骨架默认是 `0.1.0`，而 `demo/caller` 写的是 `1.0.0`——依赖只认精确版本，不认范围（AGENTS.zh.md §9.2）。把 `components/demo/hello/component.yaml` 里 `metadata` 下的 `version: 0.1.0` 改成 `version: 1.0.0`。忘了改，`add --local` 会整个中止，并告诉你原因。下面是这段报错的节选，真实输出后面还有几条建议，其中一条讲的正是这个改法：

```
❌ 错误：强依赖缺失
   卡在组件：demo/caller@1.0.0（来自 local-dev）
   本次结果：已中止，brickkit.yaml 未修改
   缺失依赖：demo/hello@1.0.0
   原因：安装源里有这个组件，但版本不是要的那个
   安装源 local-dev（local）：这里是 0.1.0
...
```

**契约。** 把 `api/openapi.yaml` 里的占位内容换成两个团队谈妥的那一份。这里只有一个接口，故意写得很小：

```yaml
openapi: 3.0.3
info:
  title: demo/hello
  version: 1.0.0
paths:
  /api/v1/hello:
    get:
      summary: 问候
      responses:
        "200":
          description: 返回一句问候
          content:
            application/json:
              schema:
                type: object
                properties:
                  greeting:
                    type: string
                    example: 你好，我是桩
                  from:
                    type: string
                    example: mock
```

骨架里其余的 `TODO`（`name`、`description`、`image`、`port`）先不用管，第 3 步会看到桩根本用不上它们。

### 第 2 步：把两个组件一起加进来

```bash
brickkit add --local
```

```
🔍 从本地安装源 local-dev 扫到 2 个组件
📦 添加 demo/caller@1.0.0
   ├── Manifest ✅
   ├── 依赖 demo/hello@1.0.0 ✅ 已拉取（artifacts 1 个文件）
   └── artifacts ✅（1 个文件）
⚠️ 警告：弱依赖缺失：demo/bus@1.0.0
   影响组件：demo/caller@1.0.0
   原因：该组件在所有安装源中均未找到
   影响：该组件的环境变量 DEMO_BUS_ENDPOINT 不会被注入
   💡 弱依赖降级由组件自行处理；如需启用，请确认该组件已发布并可从安装源获取
📦 添加 demo/hello@1.0.0
   ├── Manifest ✅
   └── artifacts ✅（1 个文件）
✅ 已写入 brickkit.yaml（2 个组件）
```

桩跟 `demo/caller` 一样，是从本地安装源里扫出来的普通组件；`demo/caller` 的强依赖这次有着落了，就是那行 `依赖 demo/hello@1.0.0 ✅ 已拉取`。中间那条 `demo/bus` 的警告是 `demo/caller` 自己的弱依赖（[第 2 篇](02-what-runs.md)讲过），跟桩没有关系。现在 `brickkit.yaml` 里已经有这两个组件了。

### 第 3 步：告诉 BrickKit，这个组件你自己来跑

什么都不写的话，BrickKit 会照桩 Manifest 里的 `image:` 去起容器，可那是个指向虚无的占位值。`local: true`（[第 3 篇](03-local-debugging.md)）的意思是"这个组件我在自己机器上跑，别为它生成容器，但它仍留在依赖图里"。`localPort` 指定它占用你机器上的哪个端口：

```yaml
components:
  - id: demo/hello
    version: 1.0.0
    local: true
    localPort: 18081
  - id: demo/caller
    version: 1.0.0
```

```bash
brickkit up --dry-run
```

下面是节选，`...` 表示省掉的行：

```
🚀 启动项目 hello-world（deploy.target: docker）
...
📋 组件状态计算：
   ✅ demo/hello@1.0.0   启动（demo/caller 需要）
   ✅ demo/caller@1.0.0  启动（顶层）
...
🔧 本地调试（local: true）：
   demo/hello@1.0.0
      不生成容器；请在 IDE 里启动它，监听 localhost:18081
      环境变量：.brickkit/generated/local-debug.demo-hello-1-0-0.env
      VS Code：launch.json 里配 "envFile": "${workspaceFolder}/.brickkit/generated/local-debug.demo-hello-1-0-0.env"
📄 已生成：.brickkit/generated/docker-compose.yaml
...
```

桩仍然出现在状态计算和依赖图里，变的是两点：不生成容器；要求它监听 `localhost:18081`——这是 `localPort`，不是桩自己 Manifest 里写的 `8080`。对 `local: true` 的组件，`image` 和 `port` 压根不会被用到，所以骨架里那些 `TODO` 才可以不改。（真正的 `brickkit up` 也不会去检查桩的镜像：照第 6 篇的办法临时起一个 PostgreSQL 绑上之后实跑，镜像检查照样通过，尽管那个占位镜像从来没人构建过。）提示里说"在 IDE 里启动"，是因为 `local: true` 本来是为调试设计的；对 mock 来说，这只是"你自己在本机上启动的任何程序"。

### 第 4 步：看消费方拿到了什么

`up --dry-run` 顺手写出了 `.brickkit/generated/docker-compose.yaml`，在里面找桩的名字（这是 `grep` 的真实输出，不是 BrickKit 自己打印的，所以跟上面几块不同，没有脚本替你核对）：

```bash
grep -n "DEMO_HELLO_ENDPOINT\|extra_hosts\|demo-hello-1-0-0" .brickkit/generated/docker-compose.yaml
```

```
27:      - DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:18081
28:    extra_hosts:
29:      - demo-hello-1-0-0:host-gateway
50:      - DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:18081
51:    extra_hosts:
52:      - demo-hello-1-0-0:host-gateway
```

关键的就是这两样，各出现两次：一次在 `demo/caller` 自己的容器里，一次在它的迁移容器里（迁移容器与主容器用同一个镜像，见 AGENTS.zh.md §5.5、§6）。

- `DEMO_HELLO_ENDPOINT`：地址的写法跟真 `demo/hello` 一模一样，都是版本化服务名，只是端口换成了你的 `localPort`。
- `extra_hosts`：让这个名字真能解析。`host-gateway` 是 Docker 的特殊值，意思是"宿主机——从容器里看过去的地址"。于是 `demo-hello-1-0-0` 指向你的机器，18081 就是 mock 该听的端口。这套机制[第 3 篇](03-local-debugging.md)拆开讲过。

`demo/caller` 照常读 `DEMO_HELLO_ENDPOINT`，完全不知道对面是个替身。

### 第 5 步：起 mock，验证消费方真的连到了它

这一步不归 BrickKit 管，另外需要 Docker 和 python3。最省事的 mock 是个静态文件服务器：把契约里约定的那份响应存成文件，让文件路径跟接口路径一致：

```bash
mkdir -p mock/api/v1
echo '{"greeting": "你好，我是桩", "from": "mock"}' > mock/api/v1/hello
python3 -m http.server 18081 --directory mock
```

让它一直开着，另开一个终端继续。

这个服务器默认监听所有网卡，这一点要紧：消费方是从容器里——也就是从这台机器"外面"——连进来的，只监听 `127.0.0.1` 的程序收不到这种连接。两种情况都试过：只听 `127.0.0.1` 时，主机上直接访问有回应，消费方却拿到 `502`（够不着它的依赖）；听所有网卡时，消费方拿到了 mock 的回答。换用别的 mock 工具时，先确认它监听的地址。

`demo/caller` 的镜像在 `brickkit up` 下需要一个真数据库才能完整起来（[第 6 篇](06-assemble-and-break.md)会绑一个）；但手动起它时，它的 web 服务并不需要数据库。所以这里直接跑镜像，带上上一步 `grep` 里那两样东西：

```bash
docker build -t brickkit-demo/caller:1.0.0 ../tests/components/demo-caller

docker run -d --name contract-first-caller \
  --add-host demo-hello-1-0-0:host-gateway \
  -e DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:18081 \
  brickkit-demo/caller:1.0.0

docker exec contract-first-caller wget -qO- http://localhost:8080/api/v1/call
```

`--add-host` 是 `docker run` 里对应 `extra_hosts` 的写法，`-e` 就是被注入的那个变量。`/api/v1/call` 会让 `demo/caller` 去调它的依赖，并把对方的回答原样带回来（第 6 篇也用过它）：

```
{"component":"demo/caller","endpoint":"http://demo-hello-1-0-0:18081","upstream":{"from":"mock","greeting":"你好，我是桩"},"version":"1.0.0"}
```

`upstream` 就是 mock 的回答——一个自以为在跟 `demo/hello` 说话的组件，在容器里取回来的。跑 mock 的那个终端里，这次请求会留下一行日志，客户端地址不是 `127.0.0.1`，说明请求来自容器（地址本身在你的机器上会不一样）：

```
172.17.0.2 - - [19/Sep/2026 18:13:53] "GET /api/v1/hello HTTP/1.1" 200 -
```

另外还用第 6 篇的办法临时起了一个 PostgreSQL，完整跑过一遍 `brickkit up`：只有 `demo/caller` 会启动（桩没有东西可启动），对它 `curl`（照前几篇的写法给它加了 `expose: true`）得到同样的 `upstream`。

想让 mock 直接按契约文件应答，就别自己写文件了，换成读契约的工具。比如 Prism——第三方工具，跟 BrickKit 无关；下面是写这篇时实际用的命令行（参数以它自己的文档为准）。它把契约里的 `example` 当作回答返回了：

```bash
npx --yes @stoplight/prism-cli mock -p 18081 -h 0.0.0.0 components/demo/hello/api/openapi.yaml
```

用完清理：`docker rm -f contract-first-caller`，mock 那个终端按 Ctrl-C。

### 真上游到了：把桩换回来

拆桩要动几处，再敲一条命令——而最容易被漏掉的恰恰是这条命令：

1. 删掉桩的源码目录 `components/demo/hello/`。
2. 把 `brickkit.yaml` 里 `demo/hello` 那一项的 `local: true` 与 `localPort` 删掉。
3. 删掉 `.brickkit/artifacts/demo-hello-1-0-0/`——那是 `add` 当初拷下来的桩契约。
4. 确认 `sources:` 里有一个带着真 `demo/hello@1.0.0` 的安装源，然后执行 `brickkit add demo/hello@1.0.0 --yes`。

最后这条不能省。来自组件市场或 Git 源的组件，Manifest 不是每次运行都重新去取，而是读 `.brickkit/manifests/` 下缓存的那一份（AGENTS.zh.md §2.3）；而 `demo/hello@1.0.0` 缓存着的，仍然是桩的。配好了 Git 源、只做第 1、2 步的话，`brickkit up --dry-run` 照样悄无声息地跑通，生成的服务却用着桩的占位镜像 `demo/hello:0.1.0`。组件已经在 `brickkit.yaml` 里了，所以 `add` 会先问要不要刷新这份缓存，`--yes` 就是替你答"要"；随后它会从第一个仍持有该组件的安装源重新读 Manifest（桩目录没了，那就不再是 `local-dev`）。这一步是拿一个本地裸仓库冒充真上游的 Git 源试出来的。第 3 步则是防一件更隐蔽的事：不删的话，桩的占位契约会一直留在 `.brickkit/artifacts/demo-hello-1-0-0/api-contract/` 下，以后有人从这个目录生成客户端，读到的就是占位内容。

---

下一篇：[管理组件源码](08-component-source.md)——把别人的组件源码克隆下来、只留手边要动的那几个、改完推回去、用完删干净，以及源码跟着项目一起提交时的提交钩子。
