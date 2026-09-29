# JSON 配置注入

成员独立运行时，每个成员在自己的容器里拿到自己的环境变量。编进外壳之后它们共用一个进程、一份环境——两个成员都有 `LOG_LEVEL` 怎么办？
平台的做法是：**成员的配置不摊进外壳的环境，而是打包成一份 JSON**，每个成员一项，互不干扰。

## 两个保留变量

外壳容器上多了两个变量：

| 变量 | 内容 |
| --- | --- |
| `BRICKKIT_SERVED_MEMBERS` | 这次承载的成员的服务名，逗号分隔：`shop-cart-0-1-0,shop-stock-0-1-0` |
| `BRICKKIT_SERVED_MEMBERS_CONFIG` | 每个成员一项的 JSON 数组：配置、端口 |

`shop/shell` 拿到的 `BRICKKIT_SERVED_MEMBERS_CONFIG`（格式化后）：

```json
[
  {
    "componentId": "shop/cart",
    "version": "0.1.0",
    "httpPort": 8081,
    "extraPorts": [],
    "config": {
      "SHOP_STOCK_ENDPOINT": "http://shop-shell-0-1-0:8082"
    }
  },
  {
    "componentId": "shop/stock",
    "version": "0.1.0",
    "httpPort": 8082,
    "extraPorts": [],
    "config": {
      "STOCK_WAREHOUSE": "上海仓"
    }
  }
]
```

## 每一项的字段

| 字段 | 说明 |
| --- | --- |
| `componentId`、`version` | 成员是谁、哪个版本 |
| `httpPort` | 成员自己 `component.yaml` 里的主端口：外壳要替它在这个端口上监听 |
| `extraPorts` | 成员的额外端口：`[{"name": "grpc", "port": 9090}]` |
| `config` | 成员独立运行时会拿到的环境变量：它的配置项，加上它依赖的地址（`*_ENDPOINT`）。`COMPONENT_ID`、`COMPONENT_VERSION` 不在这里，它们就是 `componentId`、`version` |

`config` 里的值是**已经求好的**：`$var:` 已经换成公共变量的值，`${VAR}` 已经展开，`file://` 已经读成文件内容。外壳拿到就能用，
不用再做任何替换（为什么要提前求值，见 [特殊字符](06-special-characters.md)）。

## 外壳怎么用它

1. 读 `BRICKKIT_SERVED_MEMBERS_CONFIG`，解析成数组。
2. 对每一项，找到编进来的那个模块，用这一项的 `config` 初始化它——不是用外壳自己的环境。
3. 在 `httpPort`（和每个 `extraPorts`）上替它监听。

```go
type member struct {
	ComponentID string            `json:"componentId"`
	Version     string            `json:"version"`
	HTTPPort    int               `json:"httpPort"`
	Config      map[string]string `json:"config"`
}

var members []member
if err := json.Unmarshal([]byte(os.Getenv("BRICKKIT_SERVED_MEMBERS_CONFIG")), &members); err != nil {
	log.Fatalf("BRICKKIT_SERVED_MEMBERS_CONFIG: %v", err)
}
```

模块原本是 `os.Getenv("STOCK_WAREHOUSE")` 读配置的，编进外壳时改成从传给它的这张表里读——最省事的写法是模块一开始就接收一张配置表，
独立运行时由 `main` 从环境变量填这张表，外壳里由外壳从 JSON 填。

## 零个成员

外壳也可能这次一个成员都不承载（成员都被移出去了）。这时 `BRICKKIT_SERVED_MEMBERS` 是**空字符串**，`BRICKKIT_SERVED_MEMBERS_CONFIG` 是 `[]`——
"变量存在、但为空"，外壳应当一个模块都不初始化。千万不要把它当成"没有这个变量"而回退成"把编进来的模块全部启动"：那两种情况意思相反。

## 为什么不会冲突

每个成员的配置装在自己那一项里：`shop/cart` 的 `LOG_LEVEL` 和 `shop/stock` 的 `LOG_LEVEL` 是两个不同对象里的两个键，谁也盖不了谁。
外壳自己的配置则照常是外壳容器的环境变量（见 [外壳自身配置](08-shell-config.md)），与 JSON 也不冲突。

## 它放在哪

JSON 里装着成员的全部配置，可能包括密钥，所以它按密钥对待：Docker 下写进 `.brickkit/generated/env/<外壳服务名>.env`（文件权限 0600，由 `env_file` 引用），
Kubernetes 下放进生成的 Secret。它不会明文出现在 `compose.yaml` 或 Deployment 里。
