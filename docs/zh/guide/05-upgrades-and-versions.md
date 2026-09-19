# 5. 升级，以及让多个版本并存

在这个平台上，"升级"不过是在一个地方改一下版本号——没有单独的升级命令，也没有哪个迁移工具需要知道"之前是什么状态"。这一篇把这件事做两遍：一次是普通的原地升级，一次是故意让旧版本和新版本一起跑——两次用的是完全同一个机制，精确版本锁定（AGENTS.zh.md §5.1），只是用法不一样。

## 原地把版本号改高

从第 1 篇那个单组件项目（`demo/hello@1.0.0`，已经 `add` 过、正在跑着）开始，把本地源的 Manifest 和 `brickkit.yaml` 一起改成 `2.0.0`——版本号和镜像 tag 要一起改，因为 `metadata.version` 和 `deployment.image` 是两个独立的字段，只是恰好需要同步移动：

```yaml
# components/demo/hello/component.yaml
metadata:
  version: 2.0.0
deployment:
  image: brickkit-demo/hello:2.0.0
```

```yaml
# brickkit.yaml
components:
  - id: demo/hello
    version: 2.0.0
```

```bash
docker build -t brickkit-demo/hello:2.0.0 tests/components/demo-hello
brickkit up
```

```
⬆️ 检测到版本变更：
   demo/hello: 1.0.0 → 2.0.0

📋 组件状态计算：
   ✅ demo/hello@2.0.0  启动（顶层）
...
🐳 正在启动（docker）...
   demo-hello-2-0-0             running（healthy）
✅ 全部组件已启动（1 个）
```

```bash
docker compose -p brickkit-hello-world ps -a
```

```
NAME                                       STATUS
brickkit-hello-world-demo-hello-2-0-0-1    Up (healthy)
```

旧的 `demo-hello-1-0-0` 容器在这份列表里根本不存在——不是停了、也不是躺在那儿显示 exited，是彻底不见了。因为版本化服务名从 `demo-hello-1-0-0` 变成了 `demo-hello-2-0-0`，重新生成的编排文件里已经没有旧服务的声明，`brickkit up` 会清理掉因此产生的孤儿容器，不会留一个过时的容器在那儿等你以后才发现。

## 故意让两个版本一起跑

一个 `local` 安装源目录只能放一份 `component.yaml`（一个真实、很容易踩的坑——[外壳该怎么造才合格](../patterns/shell-implementers-guide.md#多版本共存部署形态还能不一样)里在另一个场景真实踩过同一课），所以两个版本需要两个独立的源目录：

```bash
mkdir -p components-v1/demo/hello
cp tests/components/demo-hello/component.yaml components-v1/demo/hello/    # 还是 1.0.0
cp tests/components/demo-hello/openapi.json   components-v1/demo/hello/
docker build -t brickkit-demo/hello:1.0.0 tests/components/demo-hello
```

```yaml
# brickkit.yaml
sources:
  - id: local-dev
    type: local
    path: ./components        # 里面是 2.0.0
  - id: local-dev-v1
    type: local
    path: ./components-v1     # 里面是 1.0.0

components:
  - id: demo/hello
    version: 2.0.0
    expose: true
    exposePort: 8082
  - id: demo/hello
    version: 1.0.0
    expose: true
    exposePort: 8081
```

```bash
brickkit up
```

```
📋 组件状态计算：
   ✅ demo/hello@2.0.0  启动（顶层）
   ✅ demo/hello@1.0.0  启动（顶层）
...
   demo-hello-1-0-0             running（healthy）
   demo-hello-2-0-0             running（healthy）
✅ 全部组件已启动（2 个）
```

两个版本是真正独立的、各自可寻址的容器——不是"同一份部署配一个金丝雀"，是完完整整的两份：

```bash
curl http://localhost:8081/api/v1/hello
curl http://localhost:8082/api/v1/hello
```
```json
{"component":"demo/hello","greeting":"你好","message":"你好，我是 demo/hello@1.0.0","version":"1.0.0"}
{"component":"demo/hello","greeting":"你好","message":"你好，我是 demo/hello@2.0.0","version":"2.0.0"}
```

这两个镜像其实是从完全同一份源码构建出来的——每个响应里报的 `version` 完全来自平台按各自 Manifest 注入的 `COMPONENT_VERSION` 环境变量（AGENTS.zh.md §5.2），不是镜像里写死的东西。`brickkit status` 把两者摆在同一个组件 ID 底下，并排显示：

```bash
brickkit status
```

```
✅ 运行中（2 个组件）
 ┌────────────┬───────┬───────────────────┬───────────────────────────────────────────────┐
 │ 组件       │ 版本  │ 状态              │ 端口                                          │
 ├────────────┼───────┼───────────────────┼───────────────────────────────────────────────┤
 │ demo/hello │ 1.0.0 │ 运行中（healthy） │ 0.0.0.0:8081->8080/tcp, [::]:8081->8080/tcp    │
 │ demo/hello │ 2.0.0 │ 运行中（healthy） │ 0.0.0.0:8082->8080/tcp, [::]:8082->8080/tcp    │
 └────────────┴───────┴───────────────────┴───────────────────────────────────────────────┘
```

## 从多个版本里删掉一个

```bash
brickkit remove demo/hello
```

```
❌ demo/hello 存在多个版本（2.0.0, 1.0.0），请指定版本：
   建议：brickkit remove demo/hello@2.0.0
```

同一个 ID 装了两个版本时，`remove` 不会替你猜是哪一个——而是直接把该跑的命令递给你，而不只是指出这里有歧义：

```bash
brickkit remove demo/hello@1.0.0
```

```
✅ 已移除 demo/hello@1.0.0
```

这条命令跑完之后，`demo-hello-1-0-0` 的容器其实还在跑——`remove` 跟 `add` 一样，永远只写 `brickkit.yaml`（AGENTS.zh.md §8）；运行状态要等下一次 `brickkit up` 才会变，那时候重新生成的部署文件里已经没有这一条了，产生的孤儿容器会被清理掉，跟前面原地升级时看到的一模一样。

---

下一篇：[拼装一个真实的系统，然后故意把它弄坏](06-assemble-and-break.md)——这次配一个真实数据库，故意把组件之间的连接弄坏，看每一种失败模式到底长什么样。
