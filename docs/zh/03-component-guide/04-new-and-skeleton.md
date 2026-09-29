# 创建组件骨架

## `brickkit new`

```bash
brickkit new demo/quote --path demo-quote --contract openapi
```

```text
✅ 已生成组件骨架：demo/quote
   📄 demo-quote/component.yaml
   📄 demo-quote/BRICKKIT.md
   📄 demo-quote/api/openapi.yaml

下一步：
  改完骨架里的 TODO
  cd demo-quote && brickkit init   给它建本地联调工作台（补全式：已有的文件不动）
  brickkit lint                     检查 component.yaml 能不能通过
```

| 写法 | 写到哪 | 什么时候用 |
| --- | --- | --- |
| `brickkit new demo/quote` | 项目的 `components/demo/quote/` | 在一个项目里写一个新组件；本地安装源本来就按这个布局扫描，写完 `brickkit add --local` 就进了项目 |
| `brickkit new demo/quote --path demo-quote` | `demo-quote/`，不再套 `<scope>/<name>` 那一层 | 组件自己一个仓库（推荐：一个组件一个仓库） |
| `brickkit new erp/shell --shell` | 项目的 `shell/erp/shell/` | 外壳：`component.yaml` 带一个要换掉的占位成员 |

`--contract openapi` 或 `--contract proto` 顺带生成一份契约占位文件，并登记在 `artifacts` 里。

## 生成了什么

**`component.yaml`**：一份已经能通过校验的骨架，每一处要改的地方都标着 `TODO`：

```yaml
# demo/quote —— 由 brickkit new 生成的骨架
# 下面每一处 TODO 都要改成真的；结构本身已经能通过 brickkit up --dry-run 的校验
apiVersion: brickkit/v1
kind: Component

metadata:
  id: demo/quote
  name: quote # TODO：改成人看的展示名
  version: 0.1.0
  description: TODO：一句话说清楚这个组件做什么

artifacts:
  - type: api-contract
    format: openapi
    files:
      - api/openapi.yaml

deployment:
  type: container
  build: # 用 brickkit build 本地构建（up 从不自动构建）；发布了预构建镜像后再加 image: <仓库>/<名称>
    context: .
    dockerfile: Dockerfile
  port: 8080 # TODO：换成组件实际监听的端口

healthCheck:
  type: http
  path: /healthz
  # 冷启动超过默认的 60 秒（很重的 Spring Boot / Django 预加载 / .NET 首次 JIT 等）
  # 要写 startPeriodSeconds，否则 K8s 下会永久 CrashLoopBackOff
```

**`BRICKKIT.md`**：五个标准区块（组件定位、依赖说明、配置指南、契约索引、外壳声明），等你填，见 [组件文档](08-component-doc-spec.md)。

**契约占位**：`api/openapi.yaml`，一份空的 OpenAPI。

**不生成的东西**：Dockerfile、源码、`go.mod` 之类。平台不替你选语言和框架；生成之后也不会自动 `add` 进任何项目——写进 `brickkit.yaml`
是一次单独的、可审阅的动作。

## 从骨架到能跑的组件

`demo/quote` 每次返回一句名言，前面带上 `demo/hello` 的问候语。

**1. 填 `component.yaml`。** 写上名字、描述、依赖和配置项：

```yaml
metadata:
  id: demo/quote
  name: Quote
  version: 0.1.0
  description: 每次返回一句名言，带上 demo/hello 的问候语

dependencies:
  components:
    - demo/hello@1.1.0

configSchema:
  type: object
  properties:
    QUOTE_PREFIX:
      type: string
      default: "今日名言："
      description: 名言前面加的前缀
```

**2. 写代码。** 依赖的地址和自己的配置都从环境变量读：

```go
// demo/quote：每次返回一句名言，前面带上 demo/hello 的问候语。
package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"os"
)

var quotes = []string{"简单胜于复杂", "显式优于隐式", "能跑起来的比完美的好"}

func main() {
	hello := os.Getenv("DEMO_HELLO_ENDPOINT") // 平台注入的依赖地址
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	http.HandleFunc("/api/v1/quote", func(w http.ResponseWriter, _ *http.Request) {
		greeting := "（demo/hello 不可达）"
		if resp, err := http.Get(hello + "/api/v1/hello"); err == nil {
			var body struct{ Greeting string }
			_ = json.NewDecoder(resp.Body).Decode(&body)
			resp.Body.Close()
			greeting = body.Greeting
		}
		json.NewEncoder(w).Encode(map[string]string{
			"greeting": greeting,
			"quote":    os.Getenv("QUOTE_PREFIX") + quotes[rand.Intn(len(quotes))],
		})
	})
	log.Println("demo/quote listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
```

几件值得照着做的事：

- 依赖的地址（`DEMO_HELLO_ENDPOINT`）由平台注入，代码里不写死任何地址——同一份代码在 Docker 和 Kubernetes 上都能跑。
- 依赖暂时不可达时照常服务，只是少一部分内容：怎么降级是组件自己的事。
- `/healthz` 只说明本进程活着，不去探 `demo/hello`。

**3. 写 Dockerfile。**

```dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod *.go ./
RUN CGO_ENABLED=0 go build -o /out/quote .

FROM alpine:3.20
RUN adduser -D -u 10001 app
COPY --from=build /out/quote /app/quote
USER 10001
ENTRYPOINT ["/app/quote"]
```

镜像里要有 `wget` 或 `curl`（Docker 的 HTTP 健康检查用它们；Alpine 自带 `wget`），并且不要以 root 运行。

**4. 填 `BRICKKIT.md` 和契约**，见 [组件文档](08-component-doc-spec.md) 与 [契约与产物](06-artifacts-and-contracts.md)。

**5. 检查。**

```bash
cd demo-quote
brickkit lint
```

```text
📦 组件仓库（有 component.yaml、没有 brickkit.yaml）：只检查 component.yaml
✅ component.yaml

📋 检查了 1 个文件：0 个有错误，0 条警告
```

**6. 跑起来。** 在组件仓库里建一个本地联调工作台，连着它的依赖一起跑，见 [分形的本地开发](05-local-dev-fractal.md)。

**7. 发布。** 见 [发布](07-release-workflow.md)。
