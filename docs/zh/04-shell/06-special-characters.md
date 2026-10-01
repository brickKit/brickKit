# 特殊字符

## 问题在哪

成员的配置要装进一个 JSON 字符串，再作为一个环境变量交给外壳。配置值里什么都可能有：多行的 PEM 私钥、引号、反斜杠、`$`。
如果平台只是把 `${VAR}` 原样写进 JSON、留给 Docker Compose 启动时去替换，那么 Compose 做的是**不懂 JSON 的纯文本替换**：
一个带引号或换行的值替换进去，JSON 就坏了，外壳启动时解析失败——而且要到运行时才露面。

## 平台怎么做

**提前求值。** 生成部署文件时，CLI 就把每个成员配置的值求出来：

| 写法 | 生成时 |
| --- | --- |
| 字面量 | 原样 |
| `$var:NAME` | 换成公共变量的值 |
| `${VAR}` | 从进程环境、其次项目根目录的 `.env` 取值展开；取不到就大声失败 |
| `file://路径` | 读成文件的内容 |
| `{ existingSecret, key }` | 拒绝：值只在集群里，CLI 读不到，而 JSON 需要值 |

**再做 JSON 编码。** 求好的值按 JSON 规则编码：引号、反斜杠、换行都被转义。

**再按落地的地方转义一次。** Docker 下 JSON 写进 0600 的 env 文件，`$` 写成 `$$`，这样 Compose 不会再对它做任何替换；
Kubernetes 下它进生成的 Secret。

## 一个例子

`shop/stock` 的仓库名放在一个文件里，内容有引号、`$` 和两行：

```text
上海仓 "一号库"
价格 $5
```

```yaml
# config/shop-stock.yaml
STOCK_WAREHOUSE: file://.secrets/warehouse.txt
```

外壳的 env 文件里：

```text
BRICKKIT_SERVED_MEMBERS_CONFIG="[{\"componentId\":\"shop/cart\",\"version\":\"0.1.0\",\"httpPort\":8081,\"extraPorts\":[],\"config\":{\"SHOP_ORDER_ENDPOINT\":\"http://shop-order-0-1-0:8083\",\"SHOP_STOCK_ENDPOINT\":\"http://shop-shell-0-1-0:8082\"}},{\"componentId\":\"shop/stock\",\"version\":\"0.1.0\",\"httpPort\":8082,\"extraPorts\":[],\"config\":{\"STOCK_WAREHOUSE\":\"上海仓 \\\"一号库\\\"\\n价格 $$5\\n\"}}]"
```

外壳里的 `shop/stock` 模块拿到的值，逐字节就是文件的内容：

```json
{"count":42,"sku":"A1","warehouse":"上海仓 \"一号库\"\n价格 $5\n"}
```

## 装不进 JSON 的值：大声失败

JSON 只能装文本。值不是合法的 UTF-8（比如一个二进制文件）时，编码会把非法字节悄悄换成替换字符——成员拿到一份被改过的值。平台在生成时就拦下：

```text
❌ 错误：外壳成员 shop/stock@0.1.0 的配置项 STOCK_WAREHOUSE 不是合法的 UTF-8 文本
   原因：JSON 只能装文本；二进制字节会被悄悄替换掉
   建议：二进制内容请先 base64 编码，由组件自己解码
```

`lint` 也做同样的检查（引用的文件、变量取不到时，`--strict` 下报出来），所以这类问题在 CI 里就能发现，不必等到部署。

提前求值也有一个后果：外壳成员配置里的 `${VAR}` 在 Docker 下也是在 **CLI 运行时**取值的，而不是 `docker compose` 启动时——运行 `brickkit up` 的那个环境里要有这些变量。
