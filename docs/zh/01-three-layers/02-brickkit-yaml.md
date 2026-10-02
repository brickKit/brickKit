# brickkit.yaml 详解

`brickkit.yaml` 回答一个问题：**这个项目由什么组成**。它写安装源和组件的精确版本，别的都不写——
怎么部署在部署文件里，配置在 `config/` 里。

## 完整示例

```yaml
project: my-shop                  # 项目名：Docker 网络叫 brickkit-my-shop-net，K8s namespace 缺省叫 brickkit-my-shop

sources:                          # 去哪里找组件，按顺序依次尝试
  - name: org
    type: git
    baseUrl: https://git.example.com/components/   # erp/backend → https://git.example.com/components/erp-backend
  - name: local-dev
    type: local
    path: ./components            # 本机目录：<scope>/<name>/component.yaml

components:
  - id: erp/backend
    version: 1.2.0
  - id: erp/shell
    version: 1.0.0
    kind: shell                   # 外壳，由 CLI 维护
  - id: people/basic
    version: 1.0.0
  - id: people/basic              # 同一个组件的另一个版本：只因 erp/legacy 依赖它而在
    version: 0.9.0
    requiredBy: [erp/legacy]
  - id: vendor/billing
    version: 3.1.0
    source:                       # 这一个组件不按安装源推导，指定仓库
      type: git
      repo: https://github.com/vendor/billing-component.git

installer:
  requireSignature: true          # 市场组件要求签名（缺省就是 true）
```

## 项目名 `project`

必填。只能用小写字母、数字和中划线，以字母或数字开头结尾，最长 54 个字符——Docker 网络
（`brickkit-<project>-net`）和缺省的 Kubernetes namespace（`brickkit-<project>`，部署文件里的 `k8s.namespace`
可以改掉它）都由它命名。54 就是 Kubernetes namespace 的 63 个字符减去 `brickkit-` 前缀。

## 组件条目 `components[]`

| 字段 | 必填 | 含义 |
| --- | --- | --- |
| `id` | ✅ | 组件 ID，`scope/name` |
| `version` | ✅ | 精确版本 `主.次.补丁`；本地源的组件也写它 `component.yaml` 里的真实版本 |
| `kind` | | 只有一个值 `shell`，标记外壳；由 `add` 按组件的 `component.yaml` 写上，`lint` 核对它与 Manifest 一致 |
| `requiredBy` | | 这个版本只因这些组件依赖它才在项目里（见下面的"默认版本"） |
| `source` | | 为这一个组件指定来源，不按安装源推导：`type`（`git` / `local`）；`git` 写 `repo`（Git 地址），可选再写 `path`（组件在这个仓库里的子目录，用于 monorepo，不能指到仓库外面）；`local` 写 `path`（本机目录，相对项目根） |

**条目的顺序由 CLI 维护。** `add` / `remove` / `upgrade` 每次写这个文件，都把 `components` 按组件 ID 的字典序排好
（同一个 scope 的自然挨在一起）；同一个 ID 有几行时，默认版本在前，带 `requiredBy` 的兼容版本按版本号从低到高跟在后面。
所以找一个组件按名字找，不用记它是什么时候加进来的。你手写时放在哪里都可以——顺序不影响什么会运行、怎么运行（只决定 `deps`、`graph` 这类输出里谁先谁后），下一次 `add` 会顺手理好。
条目上方的注释和行尾注释跟着条目一起挪。`sources` 不排：它的顺序是查找的优先级。

### 精确版本：它就是锁文件

版本只接受精确值，`^1.0.0`、`~1.2`、`latest` 都会被拒绝。原因和 `package-lock.json` 一样：范围版本会让
"我这里能跑"和"生产上能跑"装进不同的东西。

更进一步，`brickkit.yaml` **就是**锁：项目里用到的每个组件版本都必须写在这里。某个组件依赖了一个没写进来的
组件时，`up` 报错并告诉你用 `add` 把它加进来；没写进来的**弱**依赖就当作不存在——它的地址变量不会被注入，
组件自己走降级分支。平台从不悄悄替你补上一个组件。

### 默认版本与 `requiredBy`

同一个组件可以同时有好几个版本（两个版本就是两个不同的服务名，互不冲突）。其中**恰好一行不写
`requiredBy`**，那一行就是这个组件的**默认版本**：

- 部署文件里写裸 ID（`- id: people/basic`）的条目，指的是默认版本；
- 不带版本号的配置文件 `config/people-basic.yaml` 属于默认版本；
- 本地源里的源码就是默认版本的代码。

其余版本写 `requiredBy: [<依赖它的组件 ID>]`，一眼就知道它为什么还在。这些都由 CLI 维护：
`add` 为了满足依赖加入另一个版本时写上 `requiredBy`；`upgrade` 换默认版本时调整它；没人再依赖的版本由
`upgrade` / `remove` 清掉。

## 安装源 `sources[]`

| 类型 | 写什么 | 组件从哪来 |
| --- | --- | --- |
| `git` | `baseUrl` | 组件 `erp/backend` 的仓库是 `<baseUrl>erp-backend`；版本就是仓库里的 Git tag |
| `local` | `path` | 本机目录，按 `<scope>/<name>/component.yaml` 摆放；镜像由 `brickkit build` 从这里构建 |
| `market` | `url`（可选 `authToken`） | 组件市场的 API 地址；是可选的基础设施 |

每个源都有必填的 `name`，可以写 `enabled: false` 暂时关掉。查找一个组件时按声明顺序依次尝试，第一个找到的算数。
仓库名推导不出来的组件（第三方组织、名字对不上）在组件条目上用 `source` 单独指定。
Git 源的细节（缓存、tag 规则、鉴权）见 [Git 分发机制](../03-component-guide/10-git-distribution.md)。

## `installer`

只和市场组件的签名有关：`requireSignature`（缺省 `true`）与 `publicKeys`（你信任的发布者公钥，名字 → 公钥文件路径）。
没配任何公钥时，签名校验实际上不生效，CLI 会提醒一次。详见 [安全与签名](../06-architecture/08-security-and-signing.md)。

## `brickkit add` 怎么决定版本

| 你敲的 | 结果 |
| --- | --- |
| `brickkit add erp/backend` | 取安装源里的最新版本（Git 源是最新的版本 tag，本地源是 `component.yaml` 里的版本），写成精确版本 |
| `brickkit add erp/backend@1.2.0` | 就是这个版本 |
| 它的依赖 | 按它 `component.yaml` 里写的精确版本一起加进来；依赖要的版本和项目里已有的默认版本不同时，另起一行并写 `requiredBy` |

已经有默认版本的组件，直接 `add` 另一个版本会被拒绝并提示用 `brickkit upgrade`：换默认版本要连配置一起迁移，
那是 `upgrade` 的事。

## 从旧版单文件里移走的字段

旧版的 `brickkit.yaml` 什么都装。三层之后，这些字段搬了家：

| 旧字段 | 现在在哪 |
| --- | --- |
| `deploy.target`、`deploy.namespace` 等 | 部署文件的 `target` 与 `k8s:` 块 |
| 组件的 `mode`、`localPort`、`expose`、`replicas`、`resources`、`labels` | 部署文件里这个组件的条目 |
| 组件的 `config` | `config/<组件>.yaml` |
| `resources`（数据库、Redis 的连接与绑定） | 取消了：连接信息就是组件的普通配置项，写在 `config/` 里 |
| `servedBy` | 取消了：外壳成员写在部署文件里外壳条目的 `members` 下 |
| `override.yaml` | `deploy.local.yaml` + `brickkit local` |
