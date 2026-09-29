# 多环境切换

开发机上用 Docker 跑，预发布和生产用 Kubernetes，数据库地址各不相同。BrickKit 的做法只有一条：**每个环境一份完整的部署文件**，
用 `-f` 选。

## `-f` / `--file`

```bash
brickkit up -f deploy.prod.yaml
```

`-f` 指定这一次读哪份部署文件（路径相对项目根）。它是强制的：本地模式开着也不读 `deploy.local.yaml`，文件不存在直接报错，
不会退回默认文件。`up`、`down`、`status`、`sync`、`lint`、`graph` 都接受它。

```text
使用 deploy.prod.yaml（--file）；本次忽略本地模式
🚀 启动项目 my-shop（target: k8s）
```

`brickkit.yaml` 和 `config/` 只有一份，所有环境共用：用哪些组件、哪个版本、业务配置是什么，在每个环境都一样。不同的只有"怎么部署"。

## 一份生产部署文件

```yaml
# deploy.prod.yaml
target: k8s

k8s:
  context: prod-cluster
  namespace: my-shop

vars:
  DB_HOST: pg.prod.internal

components:
  - id: demo/hello
    replicas: 2
  - id: demo/caller
    expose: true
    hostname: shop.example.com
  - id: demo/hello@1.0.0
  - id: demo/bus
```

条目与 `deploy.yaml` 一样必须一个不少地对上 `brickkit.yaml` 的每个组件版本；不同的是这里写的是生产的部署方式：
两个副本、通过 Ingress 用域名对外开放、部署到哪个集群的哪个命名空间。

## `vars:` 让同一份 `config/` 在不同环境取不同的值

`demo/caller` 要连数据库。地址在开发机上是 `localhost`，在生产是 `pg.prod.internal`。配置文件只写一次，用 `$var:` 引用公共变量：

```yaml
# config/demo-caller.yaml
DATABASE_HOST: $var:DB_HOST
```

```yaml
# config/vars.yaml
DB_HOST: localhost
```

生产的部署文件在 `vars:` 里覆盖它（见上面的 `deploy.prod.yaml`）。`$var:` 先查当前部署文件的 `vars:`，再查 `config/vars.yaml`：

```bash
brickkit up -f deploy.prod.yaml --dry-run
```

```text
📄 已生成 12 份清单：.brickkit/generated/k8s/
   命名空间：my-shop
```

生成的 Deployment 里：

```yaml
            - name: DATABASE_HOST
              value: pg.prod.internal
```

不带 `-f` 时（开发机，读 `deploy.yaml`）生成的 `compose.yaml` 里：

```text
      - DATABASE_HOST=localhost
```

生产的数据库口令同理：`config/vars.yaml` 里写 `PG_PASSWORD: ${PG_PASSWORD}`，真值放在部署机器的环境变量里；
K8s 下 CLI 生成清单时求值，并放进生成的 Secret（见 [敏感值](../01-three-layers/07-sensitive-values.md)）。

`--dry-run` 在 K8s 下只生成清单、不连集群。真正部署时，`up` 先确认 `kubectl` 当前的上下文就是部署文件写的 `k8s.context`，
再动手——部错集群是没法撤回的。

## `-f` 与 `--no-local`

| | `--no-local` | `-f <文件>` |
| --- | --- | --- |
| 读哪份 | `deploy.yaml` | 你指定的那份 |
| 本地模式 | 这一次忽略，开关不变 | 这一次忽略，开关不变 |
| 什么时候用 | 本地模式开着，想临时按团队配置跑一次 | 部署到另一个环境 |

## 推荐做法

- **每个环境一份完整、自包含的文件。** 没有继承，没有"基础文件 + 覆盖层"的合并：打开 `deploy.prod.yaml`，看到的就是生产跑的全部。
  代价是几份文件里有重复的条目；换来的是任何一份都不会因为改了别处而悄悄变样。
- **生产文件的改动走代码评审。** Git diff 就是评审：改了哪个环境、改了什么，一目了然。
- **一个集群一份文件。** 要部到另一个集群，就复制一份、改 `k8s.context`；命令行上没有临时换集群的参数。
- **组件列表的变动先改 `brickkit.yaml`。** `add` / `remove` / `upgrade` 同时维护 `deploy.yaml`（以及存在的 `deploy.local.yaml`），
  其它部署文件由你照着补上——它们会在输出末尾点名还没跟上的文件，`lint -f <文件>` 也会报出来。
