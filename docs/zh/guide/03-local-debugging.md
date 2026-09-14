# 3. 本地调试一个组件

给一个进程挂断点，这个进程就得跑在你自己的机器上，不能在容器里——但它调用的一切都该照常运行，跟正常部署时一模一样，依赖它的一切也该照常工作，感觉不到任何变化。`local: true` 正是让这件事成立的字段（AGENTS.zh.md §5.6）。这一篇把整个链路真实走一遍：`demo/hello` 作为一个普通的操作系统进程跑在主机上，而 `demo/caller`——按照它原本要跑在容器里那样配置——照样能解析并连到它。

## 设置，跟第 2 篇比只差一个字段

跟[第 2 篇](02-what-runs.md)一样的两个组件，只给 `brickkit.yaml` 里 `demo/hello` 那条加一样东西：

```yaml
components:
  - id: demo/hello
    version: 1.0.0
    local: true
    localPort: 8080
  - id: demo/caller
    version: 1.0.0
```

```bash
brickkit up --dry-run
```

```
📋 组件状态计算：
   ✅ demo/hello@1.0.0   启动（demo/caller 需要）
   ✅ demo/caller@1.0.0  启动（顶层）
...
🔧 本地调试（local: true）：
   demo/hello@1.0.0
      不生成容器；请在 IDE 里启动它，监听 localhost:8080
      环境变量：.brickkit/generated/local-debug.demo-hello-1-0-0.env
      VS Code：launch.json 里配 "envFile": "${workspaceFolder}/.brickkit/generated/local-debug.demo-hello-1-0-0.env"
```

`demo/hello` 照样出现在组件状态计算里，照样出现在依赖图里，照样算出了一个真实的地址——它在这个项目里参与的方式什么都没变。变的是**它的代码到底跑在哪儿**。

## 真正生成出了什么

生成的编排文件里只有一个服务——`demo/caller` 的——文件头的注释本身就写着（`组件数：1`），而且它带着完整的地址桥接配置：

```yaml
services:
  demo-caller-1-0-0:
    environment:
      - DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:8080
    extra_hosts:
      - demo-hello-1-0-0:host-gateway
```

`DEMO_HELLO_ENDPOINT` 跟 `demo/hello` 真在自己容器里跑时 `demo/caller` 会拿到的地址字符串完全一样——调用方自己的配置永远不会因为依赖实际跑在哪儿而改变（AGENTS.zh.md §5.1）。真正让这个字符串解析到你机器上的是 `extra_hosts`：`host-gateway` 是 Docker 自己定义的一个特殊值，意思是"从这个容器里看到的宿主机自己的 IP"——所以 `demo-hello-1-0-0` 解析到的是你的笔记本，而不是一个从来没被创建过的容器。

另一份生成出来的文件，正是命令行输出里点名的那份：

```
# .brickkit/generated/local-debug.demo-hello-1-0-0.env
COMPONENT_ID=demo/hello
COMPONENT_VERSION=1.0.0
GREETING=你好
```

跟 `demo/hello` 真的跑在容器里会拿到的环境变量完全一样——`configSchema` 的默认值也在——只是写进了一份文件，让你的 IDE 运行配置指过去就行。

## 真正跑起来

构建 `demo/hello` 的二进制，启动它的时候原样加载这份生成出来的文件：

```bash
go build -o /tmp/demo-hello-local tests/components/demo-hello

set -a; source .brickkit/generated/local-debug.demo-hello-1-0-0.env; set +a
/tmp/demo-hello-local &

curl http://localhost:8080/api/v1/hello
```

```json
{"component":"demo/hello","greeting":"你好","message":"你好，我是 demo/hello@1.0.0","version":"1.0.0"}
```

这就是调试器会挂上断点的那个进程——一个读环境变量、监听 `:8080` 的普通操作系统进程，跟你直接从 IDE 的"运行"按钮启动它没有任何区别。

## 证明一个容器真的能连到它

`demo/caller` 自己的镜像要完全启动起来需要一个真实数据库（第 6 篇会讲怎么绑），但它依赖的那个网络技巧，用一个带着上面 BrickKit 生成出的同一条 `extra_hosts` 的一次性容器就能独立验证：

```bash
docker network create brickkit-hello-world-net
docker run --rm --network brickkit-hello-world-net \
  --add-host demo-hello-1-0-0:host-gateway \
  curlimages/curl curl -s http://demo-hello-1-0-0:8080/api/v1/hello
```

```json
{"component":"demo/hello","greeting":"你好","message":"你好，我是 demo/hello@1.0.0","version":"1.0.0"}
```

一个从来没听说过你主机 IP 的容器，跑在 `demo/caller` 真实会用的那张网络上，连到了一个直接跑在你笔记本上的进程——靠的仅仅是 `brickkit up` 本来就会写进 `demo/caller` 自己服务定义里的那一行配置。

```bash
kill %1   # 停掉本地跑着的 demo/hello 进程
docker network rm brickkit-hello-world-net
```

## 这件事不会改变什么

可以同时把多个组件标成 `local: true`，各自有自己的 `localPort`——这个机制完全不限于一次只调试一个。而且 `demo/caller` 什么都不需要改就能让这一切成立：它照常从环境变量里读 `DEMO_HELLO_ENDPOINT`，压根不知道对面是一个容器还是一台笔记本（AGENTS.zh.md §5.6"零组件代码改动"这条承诺，这里是真验证过的，不只是说说）。

---

这个系列的下一篇（完整规划见[教程索引](README.md)）：同样形状的项目，改部署到 Kubernetes 而不是 Docker——同一份 Manifest、同一张依赖图，生成出来的是完全不同的一套文件。
