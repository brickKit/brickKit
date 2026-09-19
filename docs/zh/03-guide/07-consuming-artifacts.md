# 7. 消费别人的组件

一个组件的 `component.yaml` 可以声明 `artifacts`——调用方要真正对接它需要的文件（一份 OpenAPI 规范、一份 protobuf 契约、一份 SDK），跟 Manifest 本身是分开的（AGENTS.zh.md §6）。这一篇讲这些文件最终落在哪、怎么在完全不安装这个组件的情况下拿到它们，讲一个已经写好的真实组件——它整个存在的意义就是把这类东西展示给别人看；最后一节换个处境：你依赖的组件根本还没做好，怎么先立个桩，带着两边谈定的契约把自己的组件跑起来。

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

接口已经和对方谈妥，可对方的 `demo/hello` 还在开发，或者做完了还没发布；你手上的 `demo/caller` 偏偏强依赖着 `demo/hello@1.0.0`。这时执行 `brickkit add --local` 会直接被拒（后面会亲眼看到），项目卡在一个装不上的依赖上。

干等不划算。在 `demo/caller` 里临时写死几条假数据倒也行得通，可真上游一到，还得回头一处处拆掉。BrickKit 给的是另一条路：`demo/caller` 的代码一行不动，只改"它读到的那个地址，最后指向谁"。

动手之前先认三个词，后文反复用到：

- **契约**：两边事先白纸黑字定下的接口说明——对方会提供哪些网址，每个网址回什么样的数据。这里的契约用 OpenAPI 写，那是一种用 YAML 或 JSON 描述 HTTP 接口的通用格式，很多工具都认它。
- **桩（stub）**：顶班的**组件**。拿通讯录打个比方：桩就是通讯录里替对方新建的那一条——名字（ID）、版本号、接口说明都填全了，只是这个号码后面暂时没有人。
- **mock**：真正守在电话旁的那个人——一个在你机器上跑着、按契约回一些编造数据的**进程**。

BrickKit 只认通讯录，也就是桩；电话有没有人接，它不知道，也不过问。所以整件事里，BrickKit 替你办的是"建条目、登记、把地址指向你的机器"，你自己要办的是"补上契约、找个人来接电话"。用到的全是前面几篇见过的功能，没有新命令；BrickKit 也不提供 `brickkit mock` 这样的命令，理由放在文末，先把路走通。

### 第 1 步：立桩，再改两处

在仓库根目录下新建一个项目目录，让它和 `tests/` 并排（第 1 篇的 `hello-world` 也是这么放的），下面 `../tests/...` 这样的相对路径才对得上。然后把消费方 [`demo/caller`](../../../tests/components/demo-caller/) 的 Manifest 和 openapi.json 拷进去：

```bash
mkdir contract-first && cd contract-first
brickkit init hello-world

mkdir -p components/demo/caller
cp ../tests/components/demo-caller/component.yaml components/demo/caller/
cp ../tests/components/demo-caller/openapi.json components/demo/caller/
```

先看看不立桩会怎样：此刻执行 `brickkit add --local` 会被拒绝，翻遍所有安装源也找不到 `demo/hello`。原因就在 `demo/caller` 的 Manifest 里这一段依赖（节选）：

```yaml
dependencies:
  components:
    - demo/hello@1.0.0
    - id: demo/bus@1.0.0
      optional: true
```

`demo/hello@1.0.0` 既是强依赖，版本又写死，缺了它 `demo/caller` 就装不上。桩要补的就是这个空缺。

立桩只要一条命令：

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

`new` 的产出都在 `components/demo/hello/` 下：一份 `component.yaml` 骨架（不改也能通过校验），一份占位契约 `api/openapi.yaml`。这个位置不是随便挑的——`init` 配好的本地安装源 `local-dev` 扫的就是这个目录，所以稍后 `add --local` 不用另做任何配置就能找到桩。

骨架里最值得留意的是 `artifacts` 这一段：`new` 已经把占位契约登记在这里。登记了，`add` 才会把这份文件当作组件的产物，拷进 `.brickkit/artifacts/`，别人才拿得到（写法跟本篇开头 `demo/hello` 的产物声明是一回事）：

```yaml
artifacts:
  - type: api-contract
    format: openapi
    files:
      - api/openapi.yaml
```

不过骨架离能用还差两处改动，先看最容易翻车的那处。

**版本号。** 如果跳过它直接 `add --local`，会得到这样一段报错（节选，真实输出后面还有四条建议）：

```
❌ 错误：强依赖缺失
   卡在组件：demo/caller@1.0.0（来自 local-dev）
   本次结果：已中止，brickkit.yaml 未修改
   缺失依赖：demo/hello@1.0.0
   原因：安装源里有这个组件，但版本不是要的那个
   安装源 local-dev（local）：这里是 0.1.0
...
```

修法很短：把 `components/demo/hello/component.yaml` 里 `metadata` 下的 `version: 0.1.0` 改成 `version: 1.0.0`。为什么非得一模一样？看报错的最后两行：安装源里有这个组件，可版本是 `0.1.0`，不是 `demo/caller` 要的那个。BrickKit 的依赖只认精确版本，不认 `^1.0.0` 这样的范围（AGENTS.zh.md §9.2），差一位就算另一个组件。

**契约。** 占位契约里的 `paths: {}` 是空的，把它换成两边谈定的内容。示例故意只约定一个接口，后面的 mock 就只需要应付它：

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

### 第 2 步：让 BrickKit 认识这两个组件

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

从上往下读这段输出：先添加的是 `demo/caller`，它名下有一行 `依赖 demo/hello@1.0.0 ✅ 已拉取`——刚才被拒的强依赖找到了，找到的就是桩；接着那条 `⚠️ 警告：弱依赖缺失：demo/bus@1.0.0`，是 `demo/caller` 自己声明的弱依赖（[第 2 篇](02-what-runs.md)讲过：缺了只警告、不阻断），跟桩无关；最后 `demo/hello` 本身作为独立条目被添加，和任何一个从本地安装源扫出来的组件没有区别。`brickkit.yaml` 里现在有这两项。

### 第 3 步：声明"桩不用起容器"

桩不会真的跑，可骨架里还留着 `image: demo/hello:0.1.0`、`port: 8080` 这些 `TODO`——要不要认真填？不用。只要给桩加两行声明，BrickKit 就根本不去读它们：

```yaml
components:
  - id: demo/hello
    version: 1.0.0
    local: true
    localPort: 18081
  - id: demo/caller
    version: 1.0.0
```

`local: true` 是[第 3 篇](03-local-debugging.md)讲过的开关，意思是"这个组件我在自己机器上跑"：BrickKit 不为它生成容器，但它仍留在依赖图里。`localPort` 指定你本机上用哪个端口；18081 只是随手挑的，别撞上已经被占用的端口就行。

```bash
brickkit up --dry-run
```

先交代节选：`...` 省掉的行里有一条 `⚠️ 警告：资源依赖未满足（--dry-run 不阻断）`——`demo/caller` 要一个数据库，本项目还没绑。它与桩无关，第 5 步会回头说它。保留下来的部分是：

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

`image` 和 `port` 真的没被读，有两处证据。其一，🔧 那一段要你在 `localhost:18081` 提供服务：这是 `localPort` 的值，Manifest 里写的 8080 没起作用。其二更硬：把完整的 `brickkit up` 真跑一遍（临时起个 PostgreSQL、照第 6 篇的办法绑上），检测镜像拉取权限那一步照样 `✅ 全部通过`，而这个占位镜像从来没人构建过。

如果你对那句 `请在 IDE 里启动它` 心存疑惑：它沿用的是 `local: true` 最初的用途——在 IDE 里下断点调试。对 mock 来说，谁来监听 18081 无所谓，IDE 里的程序也好，终端里的脚本也好，只要那个端口上有东西在应答就行。

### 第 4 步：确认消费方拿到的地址指向哪里

桩是空的，`demo/caller` 拿到的地址到底指向哪？`up --dry-run` 顺手生成了 `.brickkit/generated/docker-compose.yaml`，从里面把桩的名字捞出来看看（这几行是在本机上真跑 `grep` 得到的，不出自 BrickKit 自己的输出，所以不在脚本的自动核对范围内）：

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

先看 `extra_hosts`。可以把它理解成往容器里手抄一行通讯录："`demo-hello-1-0-0` 的电话，请打宿主机"；`host-gateway` 是 Docker 认识的一个暗号，展开就是"宿主机在容器眼里的 IP"。（[第 3 篇](03-local-debugging.md)把这套机制拆开讲过。）

再看 `DEMO_HELLO_ENDPOINT`：地址的形状跟真 `demo/hello` 完全一样，都是"组件 ID 加精确版本"拼成的版本化服务名，唯一的差别是端口换成了你写的 `localPort`。`demo/caller` 照常读这个变量，对面是真组件还是替身，它无从分辨。

最后回答"为什么各出现两次"：一次属于 `demo/caller` 自己的容器，一次属于它的迁移容器。`demo/caller` 声明了数据库迁移，启动前会先跑一个一次性的容器（与主容器同一个镜像）去做这件事，这个容器同样得知道上游在哪（见 AGENTS.zh.md §5.5、§6）。

### 第 5 步：起 mock，看请求真的到达

这一步不归 BrickKit 管，另外需要 Docker 和 python3。最省事的 mock 是个静态文件服务器：把契约里约定的响应存成一个文件，文件路径与接口路径一致，这个接口就等于有了回应。

```bash
mkdir -p mock/api/v1
echo '{"greeting": "你好，我是桩", "from": "mock"}' > mock/api/v1/hello
python3 -m http.server 18081 --directory mock
```

服务器会一直占着这个终端，另开一个终端继续。

接下来让消费方容器去调它。这里不走 `brickkit up`：`demo/caller` 声明了要数据库，`up` 在没绑资源时不放行（第 3 步那条被 `...` 省掉的"资源依赖未满足"说的就是它，第 6 篇会真绑一个）。好在它的镜像手动起来时并不碰数据库，所以直接跑镜像，带上上一步 `grep` 里的那两样东西：

```bash
docker build -t brickkit-demo/caller:1.0.0 ../tests/components/demo-caller

docker run -d --name contract-first-caller \
  --add-host demo-hello-1-0-0:host-gateway \
  -e DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:18081 \
  brickkit-demo/caller:1.0.0

docker exec contract-first-caller wget -qO- http://localhost:8080/api/v1/call
```

这两个参数其实是上面 `grep` 结果的"手工版"：`--add-host` 对应 `extra_hosts` 那一行，`-e` 对应 `DEMO_HELLO_ENDPOINT` 那一行，只是 BrickKit 生成 compose 时替你写，这里改成命令行自己写。最后一条请求打的是 `demo/caller` 自己的 `/api/v1/call`：它收到后会转头去调依赖，也就是我们的 mock，再把拿到的东西放进 `upstream` 字段返回——第 6 篇验证依赖链时也用过它：

```
{"component":"demo/caller","endpoint":"http://demo-hello-1-0-0:18081","upstream":{"from":"mock","greeting":"你好，我是桩"},"version":"1.0.0"}
```

输出里的 `upstream` 就是那份静态文件的内容：消费方容器真的越过了"容器网络 → 宿主机"这道边界，把它取了回来。

还有第二个证据在 mock 那个终端里：`python3 -m http.server` 给每个请求记一行日志，行首是客户端 IP。你自己在主机上 curl，这里写的是 `127.0.0.1`；刚才那次请求的 IP 却是 Docker 内网里的容器地址（下面这行的 `172.17.0.2` 只是我们这次的值，你的会不同）：

```
172.17.0.2 - - [19/Sep/2026 18:13:53] "GET /api/v1/hello HTTP/1.1" 200 -
```

**监听地址，是这一步最容易翻车的地方。** 只监听 `127.0.0.1` 的 mock，你在主机上 curl 它一切正常，消费方容器却会拿到 `502`——这一点也用 `python3 -m http.server 18081 --bind 127.0.0.1 --directory mock` 实测过。`127.0.0.1` 叫回环地址，意思是"这台机器自己找自己"，绑在它上面的程序只接受本机内部发起的连接，而容器是从机器"外面"连进来的。`python3 -m http.server` 不带 `--bind` 时监听所有网卡，所以上面没事；换用别的 mock 工具，先确认它绑在哪个地址上。

别忘了这次"通过"的含义：它只证明消费方与契约里约定的样子对得上。真组件在契约之外、或与契约有出入的地方，要等它真做好了才看得见。

更完整的一次验证：临时起个 PostgreSQL，照第 6 篇的写法绑给 `demo/caller`，再给 `demo/caller` 加上 `expose: true`，然后真的执行 `brickkit up`。结果只有 `demo/caller` 一个容器被起来，桩不在其列；从主机 `curl` 它的 `/api/v1/call`，`upstream` 与上面一致。

如果接口有几十个，手写回应就不现实了，这时该让工具去读契约，由它按契约生成回应。Prism 就是这类工具之一：第三方软件，与 BrickKit 无关；下面这条命令在写作时用 `5.16.0` 版本验证过，它把契约里的 `example` 当作回答（其余参数以它自己的文档为准）。另外留意，`npx --yes` 会不经确认就下载并运行第三方代码：

```bash
npx --yes @stoplight/prism-cli@5.16.0 mock -p 18081 -h 0.0.0.0 components/demo/hello/api/openapi.yaml
```

用完清理：`docker rm -f contract-first-caller`，mock 那个终端按 Ctrl-C。

### 为什么没有 `brickkit mock`

最自然的想法是：契约都在手上了，BrickKit 能不能直接生成 mock？或者给 `up` 加个 `--with-mocks`，缺哪个强依赖就自动补个替身？都没有做，三条理由，按分量从重到轻：

**一，自动补替身，太容易补到线上去。** 强依赖缺失时，BrickKit 的既定做法是停下来报错，逼你看见这个缺口（AGENTS.zh.md §5.3）。要是有个开关能悄悄补上替身，一次误操作，线上就多出一个对所有请求都回编造数据、还照样回 200 的"下游"，不会有任何报警。本文这条路有几道保险：桩标着 `local: true`，`deploy.target` 一旦改成 `k8s`，`up` 就直接拒绝并停下——集群里的 Pod（Kubernetes 里承载容器的最小单位，可以粗略当成"集群里的一个容器实例"）够不着你个人机器上的进程；留在 Docker 上，桩也不生成容器，不会有假服务被拉起来；而且替身明明白白写在 `brickkit.yaml` 里，评审时看得到。

**二，mock 换个名字，没人会去找它。** `demo/caller` 手里的地址是真组件的 ID 加精确版本，也就是 `demo-hello-1-0-0`（AGENTS.zh.md §5.1）。桩保住了这个名字，`extra_hosts` 再把它指到你的机器上。回到通讯录的比方：存的是"demo-hello 1.0.0"的号码，接电话的人就不能自称别的名字，否则没人会拨给他。

**三，读懂契约是个无底洞。** `artifacts` 里的 `format` 只是个字符串，BrickKit 原样带着、从不解读（AGENTS.zh.md §6）。要生成 mock，就得先读懂 OpenAPI；下个月有人用 protobuf，再下个月是 GraphQL、AsyncAPI……永远追不完。专门干这件事的现成工具已经不少，BrickKit 只需要保证地址送得到。

### 真上游做好了：把桩拆干净

拆桩最容易踩的坑，不是忘了拆，而是拆了以为已经好了。做完"删桩目录、去掉 `local: true`"这两步，`brickkit up --dry-run` 照样顺利通过，可生成出来的 compose 里，`demo/hello` 用的仍是桩的占位镜像 `demo/hello:0.1.0`——拿一个本地 Git 仓库冒充真上游，实际跑出来的就是这个结果。

原因在缓存。来自组件市场或 Git 源的组件，Manifest 不是每次运行都重新去取，而是读 `.brickkit/manifests/` 里存的那一份（AGENTS.zh.md §2.3），而 `demo/hello@1.0.0` 存的还是桩的。（`local` 类型的安装源是例外：它每次都重读目录，不走缓存，所以真上游若也是本地源，就没有这个问题。）

完整的拆法是下面四行（第二行是手工改 YAML，不是命令）：

```bash
rm -rf components/demo/hello                  # 桩的源码
# 手工编辑 brickkit.yaml：删掉 demo/hello 那一项下的 local: true 和 localPort
rm -rf .brickkit/artifacts/demo-hello-1-0-0   # add 当初拷下来的桩契约
brickkit add demo/hello@1.0.0 --yes           # 前提：sources 里有个带着真 demo/hello@1.0.0 的安装源
```

第三行防的是一件更隐蔽的事：桩的契约副本不删，就会跟真组件的产物并排留在那个目录里，将来有人拿它生成客户端，读到的就是占位内容。它必须排在第四行之前，否则会把刚刷新下来的真产物一并删掉。

第四行里的 `--yes`：这个组件已经在 `brickkit.yaml` 里，所以 `add` 不会重新登记，只会问一句"要不要刷新缓存"（不带 `--yes` 时是个 `[y/N]` 提示），`--yes` 就是替你答"要"。答完之后，它从第一个仍持有该组件的安装源重新读 Manifest 和产物——桩目录已经删了，这个源不会再是 `local-dev`。

最后一句提醒：桩和 mock 是开发期的权宜之计，只能证明"消费方跟约定好的契约对得上"。真上游做好之后，跨组件的验证仍然要打真实的依赖，而不是继续靠替身——[分层测试](../07-patterns/01-testing.md)那篇里有专门一节讲这个理由。

---

下一篇：[管理组件源码](08-component-source.md)——把别人的组件源码克隆下来、只留手边要动的那几个、改完推回去、用完删干净，以及源码跟着项目一起提交时的提交钩子。
