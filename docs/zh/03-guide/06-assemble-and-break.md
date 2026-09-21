# 6. 拼装一个真实的系统，然后故意把它弄坏

到目前为止，每一篇都在真实数据库这一步刹住了车——`demo/caller` 的资源依赖一直只是一条要读的警告，没有真的绑过。这一篇把这个口子补上：`demo/hello` 和 `demo/caller` 真的接上一个 PostgreSQL，完整跑通，然后故意用两种不同的方式把它弄坏，看每一种失败到底长什么样——不是从源码推测出来的，是真观察到的。

## 一个真实的数据库，容器里能连到

```bash
docker run -d --name guide-pg -p 15432:5432 \
  -e POSTGRES_PASSWORD=devpass -e POSTGRES_DB=callerdb \
  postgres:16-alpine
```

把 `demo/caller` 的资源绑定指向它，用 `host.docker.internal`——Docker 自己定义的"从容器里看宿主机"这个名字——端口用宿主机上真正发布出来的那个：

```yaml
resources:
  - kind: database
    engine: postgresql
    id: caller-db
    host: host.docker.internal
    port: 15432
    username: postgres
    password: ${DB_PASSWORD}
    bindings:
      - componentId: demo/caller
        database: callerdb
```

如果这里写成一个看起来像服务名的裸名字（比如 `host: guide-pg`——如果你习惯了 Compose 服务之间靠名字互相解析，这个写法很自然会想到），在触达 Docker 之前就会被拦下来：

```
⚠️ 基础资源的 host 看起来是个服务名，容器里可能解析不了
   资源：caller-db
   host：guide-pg
   原因：平台不部署基础资源，compose 里不会有叫这个名字的 service
   建议：
   1. 资源跑在本机时写 host: host.docker.internal（平台会自动补 extra_hosts）
   2. 资源跑在别处时写它的 IP 或域名
```

## 真正启动它

```bash
export DB_PASSWORD=devpass
brickkit up
```

```
📌 以下基础资源需要先跑起来（平台不代为部署）：
   caller-db    postgresql   host.docker.internal:15432  供 demo/caller 使用
🔧 启动前会执行的数据库迁移（失败则该组件不会启动）：
   demo/caller@1.0.0  /app/caller migrate
🐳 正在启动（docker）...
   demo-hello-1-0-0             running（healthy）
   demo-caller-1-0-0            running（healthy）
✅ 全部组件已启动（2 个）
```

这次迁移是真的跑在真实数据库上——不再有"资源未满足"的警告，因为它已经不再是未满足的了。确认整条链路真的通了：

```bash
curl http://localhost:8090/api/v1/call
```
```json
{"component":"demo/caller","endpoint":"http://demo-hello-1-0-0:8080","upstream":{"component":"demo/hello","greeting":"Hello","message":"Hello, I'm demo/hello@1.0.0","version":"1.0.0"},"version":"1.0.0"}
```

一次真实的跨容器调用，不是缓存的或者编造的响应——`demo/caller` 刚刚真的连到了 `demo/hello`，正在把拿到的东西展示给你看。

## 弄坏它：停掉它真正调用的那个依赖

```bash
docker stop brickkit-hello-world-demo-hello-1-0-0-1
curl -w "\nHTTP %{http_code}\n" http://localhost:8090/api/v1/call
```
```json
{"endpoint":"http://demo-hello-1-0-0:8080","error":"call to demo/hello failed: Get \"http://demo-hello-1-0-0:8080/api/v1/hello\": dial tcp: lookup demo-hello-1-0-0 on 127.0.0.11:53: server misbehaving"}
```
```
HTTP 502
```

一个真实的 `502`，带着一条真实、具体的错误信息，点名到底是哪次调用失败了、为什么——这是 `demo/caller` 自己的错误处理代码，不是平台替它生成的任何东西；中间没有任何通信治理层能替你拦住这次失败、重试它或者替它降级（AGENTS.zh.md §4.1）。再看看 `demo/caller` 自己：

```bash
docker inspect brickkit-hello-world-demo-caller-1-0-0-1 --format '{{.State.Health.Status}}'
```
```
healthy
```

`demo/caller` 报的是**健康**，而它真正依赖的那个东西已经彻底挂了——这不是 bug，正是"健康检查只检查这个进程自己"这条规则（AGENTS.zh.md §10）存在的全部意义。如果 `demo/caller` 自己的 `/healthz` 会去 ping 一下 `demo/hello` 来决定自己的状态，一个依赖挂了就会连 `demo/caller` 也被莫名其妙重启——而重启什么都修不好，因为问题从来都不在 `demo/caller` 自己的进程里。

```bash
docker start brickkit-hello-world-demo-hello-1-0-0-1
curl http://localhost:8090/api/v1/call   # 恢复正常响应，不需要任何额外操作
```

## 换一种方式弄坏它：启动之前就把数据库拿掉

```bash
docker stop guide-pg
docker rm brickkit-hello-world-demo-caller-1-0-0-1
brickkit up
```

```
❌ 错误：docker 执行失败
   命令：docker compose ... up -d --wait --remove-orphans demo-hello-1-0-0 demo-caller-1-0-0
   输出：Container brickkit-hello-world-demo-caller-1-0-0-migration-1 Error
         service "demo-caller-1-0-0-migration" didn't complete successfully: exit 1
         Container brickkit-hello-world-demo-hello-1-0-0-1 Healthy
```

这跟上一种失败的形状完全不一样，也是故意的：这次不是一个跑着的组件在请求时报错，而是 `up` 这条命令本身直接失败，带着非零退出码——因为迁移容器连不上数据库、以退出码 1 结束，而"迁移失败会挡住主服务"（AGENTS.zh.md §5.5）意味着 `demo/caller` 根本没被创建出来。`demo/hello` 没有任何数据库依赖，照样启动、照样健康——一个组件的资源故障不会牵连到一个完全不相关的组件上。

## 清理

```bash
brickkit down
docker rm -f guide-pg
```

---

下一篇：[消费别人的组件](07-consuming-artifacts.md)——一个组件发布出来给别人发现和使用的东西，产物和 API 文档，从消费方的视角看。
