# 4. 让一个组件在本地跑起来，不用你操心

`mode: debug`（[第 3 篇](03-local-debugging.md)）是"你自己启动这个进程——在 IDE 里、带着断点——BrickKit 只负责把地址指过去"。`mode: local` 是同一个"裸进程、不生成容器"想法的另外半边：BrickKit 自己探测怎么启动这个组件、自己拉起它、自己盯着它——不用开 IDE，不用打断点，什么都不用你手动跑。这一篇把这条链路真实走一遍：[`demo/hello`](../../../tests/components/demo-hello/)——第 1 篇把它构建成容器用的正是这份夹具——这次改成作为一个 BrickKit 自己拉起来的普通操作系统进程在跑，另一个终端看得见它，停掉它的方式跟停掉任何一个前台命令一样。

## 设置

一个全新的项目，一个组件——只不过这次组件的**源码**得真的在场，不能只有 Manifest：`mode: local` 要从磁盘上读 `go.mod` 和 `main.go` 才能判断怎么启动它，所以只拷一份 `component.yaml`（这对基于 Docker 的那几篇够用，因为那里 `up` 用到的只是一个预先构建好的镜像）在这里是不够的。

```bash
mkdir hello-local && cd hello-local
brickkit init hello-local

mkdir -p components/demo
cp -r ../tests/components/demo-hello components/demo/hello
brickkit add --local
```

```
🔍 从本地安装源 local-dev 扫到 1 个组件
📦 添加 demo/hello@1.0.0
   ├── Manifest ✅
   └── artifacts ✅（1 个文件）
✅ 已写入 brickkit.yaml（1 个组件）
```

一个字段就能让它变成一个由 BrickKit 本地托管的组件——跟 `mode: debug` 一模一样的写法，只是值不同：

```yaml
components:
  - id: demo/hello
    version: 1.0.0
    mode: local
```

Manifest 里其余的东西什么都没变。`deployment.image` 和 `deployment.port` 还在，也照样合法——而且跟 `mode: debug` 一样，完全用不上：`mode: local` 的组件根本不生成容器（AGENTS.zh.md §5.4）。

## 探测启动命令

```bash
brickkit up --dry-run
```

```
📋 组件状态计算：
   ✅ demo/hello@1.0.0  启动（顶层）

📋 启动顺序（拓扑排序）：
   1. demo-hello-1-0-0  无依赖

可独立启动：demo-hello-1-0-0（无依赖）
📄 已生成：.brickkit/generated/compose.yaml

以下 mode: local 组件本次会启动：
   demo/hello@1.0.0  go run .

💡 --dry-run 只生成文件，未启动任何组件
   查看：cat .brickkit/generated/compose.yaml
```

`--dry-run` 依然是"看看会发生什么，什么都不启动"——对 `mode: local` 组件来说，多出来的这一样东西没有部署文件可看：它探测出来的启动命令。`component.yaml` 里没有任何一处写着 `go run .`。BrickKit 在这个组件的源码目录里看到了 `go.mod`，以及根目录下的 `package main`，剩下的自己判断出来了（AGENTS.zh.md §5.6 有完整的探测表——Node、Java、Python 等语言各自认自己的标记文件）。编排文件照样会生成——一个项目完全可以让 `mode: local` 组件跟普通容器混着用，这里只是恰好一个容器都没有。

## 真正跑起来

```bash
brickkit up
```

```
正在启动 1 个本地组件——按 Ctrl+C 停止
demo-hello-1-0-0 | {"time":"...","level":"INFO","msg":"component started","component":"demo/hello","version":"1.0.0","addr":":8080"}
demo-hello-1-0-0  已监听端口 8080
```

这次不会返回——`up` 会一直待在前台，跟不加 `-d` 的 `docker compose up` 一样，把进程自己的输出实时打出来，每一行都带着服务名前缀。第一行 `demo-hello-1-0-0 |` 是 `main.go` 自己那条结构化日志，原样转发出来的，连 JSON 都没变——BrickKit 不会解析或改写一个本地组件的输出，跟对待容器的输出没有区别；平台自己那条日志约定（AGENTS.zh.md §6：JSON 打到 stdout）是组件自己选择遵守的，不是平台强加给它的。第二行没带竖线前缀，是 BrickKit 自己打的：它一直探测 8080 端口，直到探到有东西真的在那边应答——跟一个容器的健康检查做的是同一件事。

## 从另一个终端看一眼

`demo/hello` 不在任何 BrickKit 能查询的引擎里跑——一个普通操作系统进程没有 `docker ps` 可看。所以在第二个终端里，进到同一个项目目录：

```bash
brickkit status
```

```
📊 项目状态：hello-local（deploy.target: docker）

⬜ 本次没有需要容器化启动的组件


💡 这个项目有一个本地会话在跑（PID 1078727）——去那个终端看，或者在那边 Ctrl+C
```

```bash
brickkit graph
```

```
graph TD
    demo_hello_1_0_0["demo/hello@1.0.0<br/>托管本地"]
    classDef managed fill:#e6ffe6,stroke:#2e8b57;
    class demo_hello_1_0_0 managed
```

```bash
brickkit down
```

```
🛑 停止项目 hello-local
📋 本项目当前没有容器在跑（引擎里一个都没有）
   用 brickkit up 启动

💡 这个项目有一个本地会话在跑（PID 1078727）——去那个终端看，或者在那边 Ctrl+C
```

三个不同的命令，同一句提示，背后是同一个原因：`.brickkit/` 下的一份小小的锁文件——`up` 一开始监管 `mode: local` 组件就创建，它停下来就释放——是唯一能让**第二个**终端知道本地会话存在的东西。`status` 不会把 `demo/hello` 列进容器表（本来就没有容器），但也不会把它报成"没跑"——提示句直接告诉你该去哪儿看。`graph` 给它自己的标签和颜色，钉住、永远不会被置灰，跟一个正在跑的 `mode: debug` 组件拿到的视觉处理一样。三个里最要紧的是 `down`：它会停掉这个项目生成的每一个容器，但它做不到、也不该做到伸进另一个终端的进程树里——提示句让你自己去那边按 Ctrl+C，而不是悄悄什么都不做，留你纳闷 `demo/hello` 怎么还在监听。

## 停掉它

回到第一个终端，`Ctrl+C`：

```
demo-hello-1-0-0 | {"time":"...","level":"INFO","msg":"component exited"}
```

`demo/hello` 自己最后那一行来自它的信号处理（`main.go` 捕获 `SIGINT`/`SIGTERM`，体面关掉自己的 HTTP 服务器，关完才打一行 `component exited`）——BrickKit 要求它停下的方式，跟 `docker stop` 要求一个容器停下没有区别，它也照做了。BrickKit 自己没打额外的"正在关闭"提示，也没有崩溃报告——因为什么都没崩，`up` 以 `0` 退出。这份安静是故意的：崩溃摘要专门用来暴露**意料之外**的退出（下一节），一个完全照要求停下的组件如果也打一份出来，就只是噪音。`mode: local` 组件的进程树也是故意只属于这一个终端会话的——它从不试图活得比启动它的那次 `up` 更久（不像容器，CLI 退出后容器照样接着跑），所以事后没有任何要清理的东西，也没有类似 `docker ps -a` 那种"已停止但还留着"的条目。

## 如果它崩了

上面这一切的前提是"体面地被要求停下"。如果进程自己死了——panic、收到没处理的信号、在 `down`/Ctrl+C 介入之前就以非零状态退出——走的是另一条路：打一份崩溃摘要，带着退出原因和进程自己最近的一段输出，`--crash-lines` 控制这段输出留多少行（`0` 表示只留退出原因，一行输出都不留）。真正制造一次崩溃、读懂这份摘要，属于[故障排除](../08-troubleshooting.md)的范围，不是这里——这一篇要证明的只是"不用你操心"这条路确实按承诺的样子在跑。

---

下一篇：[部署到 Kubernetes](05-kubernetes.md)——同样形状的项目，改部署到 Kubernetes 而不是 Docker。
