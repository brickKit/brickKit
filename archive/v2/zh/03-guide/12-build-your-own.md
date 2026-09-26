# 12. 从零开发自己的第一个组件

前面每一篇装的都是本来就已经存在的东西。这一篇真写一个全新的组件——一个最小的 HTTP 计数器，故意小到能完整抄下来——然后用这个系列里其它每一篇一模一样的方式把它跑起来：没有专门的上手路径，就是一份 `component.yaml` 加一个镜像。这里手打 Manifest 是故意的，为了说明它没有任何藏起来的魔法；真要用的话，`brickkit new my-scope/counter` 生成的是同一个形状的文件，已经合法，本该你手打的那些字段留着 TODO 等你填。

## 整个组件，四个文件

```go
// main.go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
)

var (
	mu    sync.Mutex
	count int
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	startAt, _ := strconv.Atoi(envOr("START_AT", "0"))
	count = startAt

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/api/v1/count", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		count++
		current := count
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{
			"component": envOr("COMPONENT_ID", "tutorial/counter"),
			"version":   envOr("COMPONENT_VERSION", "1.0.0"),
			"count":     current,
		})
	})
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

```
# go.mod
module tutorial/counter

go 1.22
```

```dockerfile
# Dockerfile —— 跟这个系列前面用到的每一个夹具同样的多阶段构建形状
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/counter .

FROM alpine:3.20
RUN adduser -D -u 10001 brickkit
COPY --from=build /out/counter /app/counter
USER 10001
EXPOSE 8080
ENTRYPOINT ["/app/counter"]
```

```yaml
# component.yaml
apiVersion: brickkit/v1
kind: Component

metadata:
  id: tutorial/counter
  name: 教程计数器
  version: 1.0.0
  description: 教程用的最小组件，每次调用 /api/v1/count 就加一

configSchema:
  type: object
  properties:
    startAt:
      type: integer
      default: 0
      description: 计数器起始值，注入为环境变量 START_AT

deployment:
  type: container
  image: tutorial/counter:1.0.0
  port: 8080

healthCheck:
  type: http
  path: /healthz
```

这个组件自己的目录里没有任何一份跟具体 `deploy.target` 绑定的文件——生成这一步完全是 CLI 的活，具体生成成什么由**使用**这个组件的那个项目自己选（AGENTS.zh.md §5.5）。

## 构建它、加它、跑它——跟这个系列里其它一切完全一样

```bash
docker build -t tutorial/counter:1.0.0 .

mkdir -p components/tutorial/counter
cp component.yaml components/tutorial/counter/
brickkit add --local
```
```
🔍 从本地安装源 local-dev 扫到 1 个组件
📦 添加 tutorial/counter@1.0.0
   └── Manifest ✅
✅ 已写入 brickkit.yaml（1 个组件）
```

```yaml
components:
  - id: tutorial/counter
    version: 1.0.0
    expose: true
    exposePort: 8085
```

```bash
brickkit up
curl http://localhost:8085/api/v1/count
curl http://localhost:8085/api/v1/count
```
```json
{"component":"tutorial/counter","count":1,"version":"1.0.0"}
{"component":"tutorial/counter","count":2,"version":"1.0.0"}
```

一个这个平台从没见过的全新组件，从四个文件变成一个真正跑着、会计数、能 curl 到的服务，用的是这个系列从第 1 篇开始对 `demo/hello` 用的一模一样的 `add`/`up`。平台自己的机制完全不在乎这个组件一小时前根本不存在。

## 配置覆盖也是一模一样的方式

```yaml
components:
  - id: tutorial/counter
    version: 1.0.0
    expose: true
    exposePort: 8085
    config:
      startAt: 100
```
```bash
brickkit up
curl http://localhost:8085/api/v1/count
```
```json
{"component":"tutorial/counter","count":101,"version":"1.0.0"}
```

`startAt: 100`在容器真实的环境里变成了 `START_AT=100`（AGENTS.zh.md §5.2 的驼峰转大写下划线规则），处理函数第一次读到它就用上了——跟第 1 篇对 `demo/hello` 的 `greeting` 用的是同一套 configSchema 覆盖机制。

## 从这里开始，这个系列前面的一切都直接适用

这个组件不需要任何特殊操作就能接上这个系列讲过的其它内容，因为那些内容从来就不是专属于 `demo/hello` 或 `demo/caller` 的：

- [本地调试一个组件](03-local-debugging.md)用的是同一套方式——`mode: debug`，在自己机器上跑那个二进制，断点照挂不误。
- [从市场发布与安装](10-marketplace.md)和[给组件签名与验签](11-signing.md)也是同一套方式——`brickkit publish --path ./components/tutorial/counter`，签名，装到别的地方去。

这一篇没有引入任何新机制——重点是确认了"从零开发一个组件"真的会把你带到这个系列前面每一篇早就走过的那同一个平台上，不多不少。

---

下一篇：[网络策略与最小权限](13-network-policy.md)——在 Kubernetes 上锁定哪些组件真的能跟哪些说上话。
