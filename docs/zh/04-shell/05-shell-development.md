# 开发一个外壳

## 从骨架开始

```bash
brickkit new shop/shell --shell
```

```text
✅ 已生成组件骨架：shop/shell
   📄 shell/shop/shell/component.yaml
   📄 shell/shop/shell/BRICKKIT.md
   📄 shell/shop/shell/AGENTS.md
   📄 shell/shop/shell/CLAUDE.md
   📄 shell/shop/shell/README.md
```

外壳写到项目的 `shell/` 下（本地安装源 `local-shells`）：外壳通常是一个项目自己的代码，决定"这个项目把哪些组件合在一起"，随项目提交。
骨架里的 `shell.members` 带一个要换掉的占位成员：

```yaml
shell:
  # TODO：这个外壳编进了哪些成员、各是哪个精确版本（把占位的换掉）
  members:
    - example/member@0.1.0
```

换成真正编进来的成员：

```yaml
shell:
  members:
    - shop/cart@0.1.0
    - shop/stock@0.1.0
```

**编进去的，和这次承载的，是两回事。** `shell.members` 说的是外壳的镜像里有什么，所以至少列一个成员：`members: []` 在
`lint` 和 `add` 时都报 `MANIFEST_INVALID`——什么都没编进去的外壳没有存在的理由。这次哪些成员在它里面跑是另一件事，在部署文件里
把成员条目嵌到外壳条目下面来选，可以一个都不选：那样外壳启动时 `BRICKKIT_SERVED_MEMBERS` 是空字符串，
`BRICKKIT_SERVED_MEMBERS_CONFIG` 是 `[]`（见[JSON 配置注入](02-json-injection.md)）。

所以新外壳要和它的第一个成员一起进项目，不能先进：先把那个成员做好、构建出镜像，在 `shell.members` 里换掉占位的成员，
再 `brickkit add` 外壳（成员会一并写好）。在那之前外壳的骨架留在 `brickkit.yaml` 外面——`brickkit build` 只构建项目里有的组件，
这时也还不认识它。

外壳本身就是一个普通组件：它有自己的 `deployment`、`healthCheck`、镜像、配置。

它的文档也和别的组件一样。`BRICKKIT.md` 是常规的六节；最后一节"外壳声明"列出编进来的成员，每个带精确版本，与 `shell.members`
保持一致。骨架先放着那个占位成员：

```markdown
## 外壳声明

这个组件是外壳。编进的成员（与 component.yaml 的 shell.members 保持一致）：

- `example/member@0.1.0` <!-- TODO: 占位：每个成员做什么、需要外壳提供什么 -->
```

和 `shell.members` 一起换掉，每个成员写清它做什么、需要外壳提供什么。`shell.members` 里有、这一节没提的成员，
`brickkit lint` 会报 `DOC_OUT_OF_STEP`。

## 启动代码

外壳启动时做四件事：读 `BRICKKIT_SERVED_MEMBERS_CONFIG`，按每一项找到编进来的模块，用那一项的配置初始化它，在那一项的端口上替它监听。
`shop/shell` 的全部代码：

```go
// shop/shell：把 shop/cart 与 shop/stock 编进一个进程的外壳。
//
// 真实的外壳会 import 成员的代码；为了短，这里把两个模块的处理函数直接写在外壳里。
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
)

// member 是 BRICKKIT_SERVED_MEMBERS_CONFIG 里的一项。
type member struct {
	ComponentID string            `json:"componentId"`
	Version     string            `json:"version"`
	HTTPPort    int               `json:"httpPort"`
	Config      map[string]string `json:"config"`
}

// modules 是编进这个外壳的模块：组件 ID → 用成员自己的配置建出它的 HTTP 处理器。
var modules = map[string]func(cfg map[string]string) http.Handler{
	"shop/stock": func(cfg map[string]string) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/stock", func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"sku": "A1", "count": 42, "warehouse": cfg["STOCK_WAREHOUSE"]})
		})
		return mux
	},
	"shop/cart": func(cfg map[string]string) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/cart", func(w http.ResponseWriter, _ *http.Request) {
			var s map[string]any
			if resp, err := http.Get(cfg["SHOP_STOCK_ENDPOINT"] + "/api/v1/stock"); err == nil {
				_ = json.NewDecoder(resp.Body).Decode(&s)
				resp.Body.Close()
			}
			json.NewEncoder(w).Encode(map[string]any{"items": []string{"A1"}, "stock": s})
		})
		return mux
	},
}

func main() {
	var members []member
	if err := json.Unmarshal([]byte(os.Getenv("BRICKKIT_SERVED_MEMBERS_CONFIG")), &members); err != nil {
		log.Fatalf("BRICKKIT_SERVED_MEMBERS_CONFIG: %v", err)
	}
	for _, m := range members {
		build, ok := modules[m.ComponentID]
		if !ok {
			log.Fatalf("this shell has no module %s", m.ComponentID) // 编进的成员里没有它：大声失败
		}
		addr := fmt.Sprintf(":%d", m.HTTPPort) // 替成员在它自己的端口上监听
		go func(h http.Handler) { log.Fatal(http.ListenAndServe(addr, h)) }(build(m.Config))
		log.Printf("serving %s@%s on %s", m.ComponentID, m.Version, addr)
	}
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	log.Fatal(http.ListenAndServe(":8080", nil))
}
```

构建、启动之后，外壳的日志：

```text
shop-shell-0-1-0-1            | 2026/09/29 12:02:06 serving shop/cart@0.1.0 on :8081
shop-shell-0-1-0-1            | 2026/09/29 12:02:06 serving shop/stock@0.1.0 on :8082
```

几件要照着做的事：

- **每个模块用自己那一项的 `config`**，不要读外壳进程的环境变量——那里没有成员的配置。
- **JSON 里出现了一个外壳没编进的成员，立刻失败。** 那意味着声明与镜像对不上；悄悄跳过它，调用方拿到的地址就指向一个不存在的服务。
- **健康检查只查外壳进程本身。** 一个模块初始化失败，应该让整个外壳启动失败，而不是带着一个死掉的模块报健康。

## 一个进程、几个成员：哪些东西归谁

每个成员单独跑时，它独占一个进程，进程级的东西（全局变量、默认注册表、环境变量、连接上的会话设置）想怎么用都行。进了外壳，
几个成员共用一个进程，这些东西就成了共享的，最常见的错误是"最后一个初始化的赢"——而且不报错，只是悄悄串了。规则是：

| 东西 | 归谁 | 用错了会怎样 |
| --- | --- | --- |
| 配置 | 每个成员一份：它那一项的 `config` | 读 `os.Getenv` 拿不到成员的配置；有人把配置写回进程环境（`setenv`），成员之间互相覆盖 |
| 以文件交付的密钥（成员声明了 `mount: file`） | 每个成员自己的目录：`config` 里那一项是文件的路径（`/run/brickkit/secrets/<成员的服务名>/<键>`），平台已经把文件挂进外壳的容器。外壳不用做任何事，把 `config` 原样交给成员 | 外壳去读这个文件、把内容塞回成员的配置里：成员读到的不再是路径，文件换了它也不知道 |
| 链路追踪 / 指标的 provider 与 `service.name` | 每个成员一份，`service.name` 用成员自己的组件 ID | 设成全局默认，最后初始化的那个赢，所有成员的 trace 归到一个名字下 |
| 指标注册表 | 每个成员一个（或共用一个、每条指标带成员的标签） | 都往默认注册表里注册，第二个模块注册同名指标时直接报错退出 |
| 数据库连接池、数据库角色 | 每个成员自己的池 | 共用一个池，一个成员的慢查询占满连接，别的成员一起超时 |
| 会话级的数据库设置（`SET ROLE`、`search_path`） | 只能是事务级的（`SET LOCAL`），或每次借到连接时重设 | 连接还回池里还带着设置，下一个借到它的成员跑进了别人的 schema。PostgreSQL 的 `ALTER ROLE … SET` 只对登录角色生效，`SET ROLE` 之后不生效 |
| 资源预算（连接数、并发、队列长度） | 每个成员各自有上限 | 一个成员把进程的资源吃光，同一进程里的其他成员一起变慢 |
| 信号处理、日志的输出端 | 外壳，只初始化一次：收到 `SIGTERM` 依次关闭每个成员；日志格式与去向由外壳定，每一行带上成员的组件 ID | 每个模块各装一个信号处理器、各自改全局日志配置，谁先谁后决定了行为 |
| 框架的全局开关（比如 Web 框架的运行模式） | 外壳，只设一次 | 每个模块各设一遍，以最后一个为准 |

成员单独跑时，这些规则也成立，所以照着它写的成员代码，进不进外壳都不用改。

### 监控指标

成员进了外壳，仍然在自己的端口上提供服务，它自己的服务名也仍然解析得到：Docker / Podman 上是外壳容器的网络别名，
K8s 上是一个选中外壳 Pod 的 Service（见下一节）。所以每个成员照旧在自己的端口上提供 `/metrics`（用它自己的注册表），
监控按"成员的服务名 + 成员的端口"去抓就行：Prometheus 加入项目网络后写静态目标，或在 K8s 上按成员的 Service 抓。

行不通的只有一种做法：靠容器的 label（或 Pod 的 `prometheus.io/port` 注解）自动发现。一个外壳只有一个容器、一个 Pod，
只能声明一个端口；成员的 `labels` 在外壳里也不生效。要用这种方式，就让外壳把各成员的指标汇总到外壳自己的一个端点上，
每条指标带上成员的组件 ID 作标签。

## 地址怎么指过来

调用方不知道对面是外壳。平台在两处替它把地址接上：

**别人拿到的地址指向外壳。** 外壳外面的 `shop/order` 依赖 `shop/stock`，它拿到的是：

```text
      - SHOP_STOCK_ENDPOINT=http://shop-shell-0-1-0:8082
```

外壳里的成员之间也一样：`shop/cart` 在 JSON 里拿到的 `SHOP_STOCK_ENDPOINT` 同样是 `http://shop-shell-0-1-0:8082`。端口是成员自己的端口——
外壳正是在那里替它监听。

**成员的服务名也解析到外壳。** Docker 下外壳容器挂上了每个成员的服务名作为网络别名：

```yaml
    networks:
      brickkit-net:
        aliases:
          - shop-cart-0-1-0
          - shop-stock-0-1-0
```

Kubernetes 上每个被承载的成员有一个同名的 Service，选中外壳的 Pod。两种情况下，按服务名找成员的东西——别的容器、种子数据脚本、
跨组件测试——都不用改就能找到外壳。

**进程内怎么分发，是外壳自己的事。** 请求到了外壳的某个端口之后交给哪个模块、要不要按路径或主机名再分——平台不介入。
平台只保证请求能到达外壳的正确端口；外壳里是一个模块一个端口，还是一个端口按路径分发，都由外壳作者决定。

**端口不能冲突。** 外壳自己的端口和每个成员的端口最终都在同一个容器（或 Pod）里监听，平台在生成之前核对：

```text
❌ 错误：外壳 shop/shell@0.2.0 上有两个组件都要用端口 8082
   占用方：组件 shop/cart@0.1.0
   占用方：组件 shop/stock@0.2.0
   建议：这几个组件最终都跑在同一个外壳容器/Pod 里，端口必须互不相同
```

## 成员的迁移不在外壳里跑

成员声明了 `migration.command` 时，平台用**成员自己的镜像**、成员自己的配置单独跑它的迁移，跑成功之后才启动外壳：

```yaml
  shop-shell-0-1-0:
    depends_on:
      shop-stock-0-1-0-migration:
        condition: service_completed_successfully
```

所以外壳不必知道怎么迁移每个成员；而每个成员**都必须有自己的镜像**（`deployment.image` 或 `deployment.build`），哪怕它平时总在外壳里跑。
详见 [与外壳的交互](../05-migration/04-shell-interaction.md)。

## 本地联调

外壳也是一个组件，可以用同样的方式开发：在外壳目录里 `brickkit init` 建一个工作台，或者在项目里把外壳条目写成 `mode: debug` / `mode: local`，
让它以本机进程运行（见 [分形的本地开发](../03-component-guide/05-local-dev-fractal.md)）。

外壳以本机进程运行时，它加载的是成员**本地仓库里的代码**（`components/` 下克隆的那份）。所以 `up` 核对一条链：外壳这次承载的成员版本
等于外壳 `component.yaml` 声明编进的版本；成员有本地仓库时，仓库 `component.yaml` 里的版本也必须就是它，而且它得是这个成员的默认版本。
对不上就报错，并提示 `brickkit upgrade <成员>@<仓库版本>` 或者把仓库切到对应的 tag——而不是让你调试一份和声明不符的代码。
