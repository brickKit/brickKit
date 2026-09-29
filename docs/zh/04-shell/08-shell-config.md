# 外壳自身配置

## 外壳也是一个组件

外壳有它自己的配置：日志级别、它自己的连接池、它自己对外的端口。它像任何组件一样在 `component.yaml` 里声明 `configSchema`，
使用方在 `config/<外壳>.yaml` 里填值：

```yaml
# shell/shop/shell/component.yaml
configSchema:
  type: object
  properties:
    SHELL_LOG_LEVEL:
      type: string
      default: info
      description: 外壳自己的日志级别
```

## 两种配置，两条通道

| | 给谁 | 怎么到达 |
| --- | --- | --- |
| 外壳自己的配置 | 外壳进程 | 普通环境变量，和任何组件一样 |
| 成员的配置 | 外壳里的各个模块 | 打包在 `BRICKKIT_SERVED_MEMBERS_CONFIG` 里（见 [JSON 配置注入](02-json-injection.md)） |

生成的外壳容器上两者并存：

```yaml
    env_file:
      - path: .brickkit/generated/env/shop-shell-0-2-0.env
    environment:
      - BRICKKIT_SERVED_MEMBERS=shop-cart-0-1-0,shop-stock-0-2-0
      - COMPONENT_ID=shop/shell
      - COMPONENT_VERSION=0.2.0
      - SHELL_LOG_LEVEL=info
```

`SHELL_LOG_LEVEL` 是外壳自己的；成员的配置在 env 文件里的那份 JSON 中，不会与它冲突。
外壳的 `COMPONENT_ID` / `COMPONENT_VERSION` 是外壳自己的——成员的 ID 与版本在 JSON 每一项的 `componentId` / `version` 里。

外壳的 `configSchema` 同样不能用平台保留的名字，其中包括 `BRICKKIT_SERVED_MEMBERS` 与 `BRICKKIT_SERVED_MEMBERS_CONFIG`。

## 外壳内部的通信

成员之间在外壳里怎么调用——走各自的端口、走进程内的函数调用、还是一条内部的消息总线——平台不介入。
平台给的地址（`SHOP_STOCK_ENDPOINT=http://shop-shell-0-1-0:8082`）一定能用：外壳确实在那个端口上替 `shop/stock` 监听。
外壳作者想省掉这一跳网络，可以在外壳里认出"这个地址指向我自己的某个模块"，改成进程内调用——那是外壳自己的优化，平台不理解也不需要理解。
