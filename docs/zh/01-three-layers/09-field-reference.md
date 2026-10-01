# 字段速查

三个文件（外加组件的 `component.yaml`）能写的全部字段，一页看完。每个字段的精确类型与校验规则见
[参考手册](../11-reference/README.md)；编辑器补全用 `schemas/` 下的 JSON Schema，见
[JSON Schema](../11-reference/05-json-schemas.md)。

## brickkit.yaml

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `project` | ✅ | 项目名：小写字母、数字、中划线 |
| `sources[].name` | ✅ | 安装源的名字 |
| `sources[].type` | ✅ | `git` / `local` / `market` |
| `sources[].baseUrl` | git 必填 | 组件 `a/b` 的仓库是 `<baseUrl>a-b` |
| `sources[].path` | local 必填 | 本机目录 |
| `sources[].url` | market 必填 | 组件市场的 API 地址 |
| `sources[].authToken` | | 市场令牌（通常写 `${VAR}`，或用 `brickkit login`） |
| `sources[].enabled` | | 缺省 `true` |
| `components[].id` | ✅ | `scope/name` |
| `components[].version` | ✅ | 精确版本 |
| `components[].kind` | | `shell`（CLI 维护） |
| `components[].requiredBy` | | 这个版本因哪些组件依赖而在 |
| `components[].source` | | 单独指定来源：`type`（`git` / `local`）、`repo`、`path` |
| `installer.requireSignature` | | 市场组件要求签名，缺省 `true` |
| `installer.publicKeys` | | 信任的发布者公钥：名字 → 公钥文件路径 |

## deploy.yaml / deploy.local.yaml

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `target` | ✅ | `docker` / `podman` / `k8s` |
| `focus` | | 只能写在 `deploy.local.yaml` 里：只运行这个组件（从源码跑）和它需要的组件——见[在项目里就地开发](../02-project-guide/04-focus-run.md) |
| `vars` | | 覆盖 `config/vars.yaml` 的同名公共变量 |
| `k8s.context` / `k8s.namespace` / `k8s.createNamespace` | | 部署到哪个集群、哪个命名空间 |
| `k8s.podSecurity` | | `restricted`：按 Pod Security 的 restricted 级别生成 |
| `k8s.imagePullSecrets` | | 拉镜像用的 Secret 名 |
| `k8s.ingressClass` / `k8s.ingressAnnotations` | | Ingress 的 class 与注解 |
| `k8s.networkPolicy.*` | | 按依赖图生成 NetworkPolicy：`enabled`、`ingressController`、`allowFrom[]`、`egress` |
| `k8s.serviceAccount.enabled` | | 每个组件一个不挂令牌的 ServiceAccount |
| `components[].id` | ✅ | 裸 ID（默认版本）或 `id@版本` |
| `components[].mode` | | `enabled` / `disable` / `local` / `debug`（`debug` 只能在 `deploy.local.yaml`） |
| `components[].localPort` | | `local` / `debug` 时（或写在 `focus` 组件的条目上）本机进程的端口 |
| `components[].expose` / `exposePort` | | 对外开放；Docker 下的宿主机端口 |
| `components[].hostname` / `tlsSecret` | | K8s Ingress 的域名与证书 |
| `components[].replicas` | | K8s 副本数 |
| `components[].resources` | | `requests` / `limits` 的 `cpu`、`memory` |
| `components[].serviceAccountName` | | K8s 下用已有的 ServiceAccount |
| `components[].labels` | | 原样透传的标签 |
| `components[].skipWaitFor` | | 启动时不等的强依赖 |
| `components[].members[]` | | 外壳承载的成员，每个成员是一个完整条目（同上字段，不能再嵌 `members`） |

## config/*.yaml

没有固定字段：键就是组件 `configSchema` 里声明的配置项名，也就是容器里的环境变量名。值可以是字面量、
`$var:NAME`、带 `${VAR}` 的字符串、`file://路径`，或 `{ existingSecret: 名字, key: 键 }`。
`config/vars.yaml` 的键由你定，值不能是 `$var:`。

## component.yaml

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `apiVersion`、`kind` | ✅ | `brickkit/v1`、`Component` |
| `metadata.id` / `name` / `version` / `description` | ✅ | 组件 ID、名字、精确版本、描述 |
| `metadata.vendor` / `license` / `apiDocs` / `repository` | | 发布者、许可证、API 文档地址、组件仓库或主页（项目组件表的"主页"一列） |
| `tags` | | 检索用的标签 |
| `artifacts[]` | | 契约：`type`、`format`、`description`、`files` |
| `dependencies.components[]` | | 依赖：`id@版本`；弱依赖写 `optional: true` |
| `configSchema` | | 配置说明书：`properties.<键>` 的 `type`、`default`、`description`、`secret`、`enum`、`minimum`、`maximum`、`pattern`、`items`；`required` |
| `deployment.type` | ✅ | 固定 `container` |
| `deployment.image` / `deployment.build` | 二选一或都写 | 拉取的镜像，或本机构建的 `context` 与 `dockerfile` |
| `deployment.port` | ✅ | 主端口 |
| `deployment.extraPorts[]` | | 额外端口：`name`、`port`。每个额外端口给调用方一个 `<ID>_<端口名>_ENDPOINT`，端口名转大写、`-` 换成 `_`（`people/basic` 的端口 `admin-api` → `PEOPLE_BASIC_ADMIN_API_ENDPOINT`） |
| `deployment.resources` | | 建议的配额 |
| `deployment.labels` | | 透传的标签 |
| `migration.command` | | 数据库迁移命令（数组） |
| `healthCheck.type` | ✅ | `http` / `tcp` / `none` |
| `healthCheck.path` | `http` 时必填 | HTTP 路径，以 `/` 开头 |
| `healthCheck.startPeriodSeconds` | | 启动宽限秒数（缺省 60） |
| `shell.members` | | 外壳编进的成员，精确版本 `id@版本` |
| `local.language` / `local.runCommand` | | `mode: local` 时指定语言或直接给出启动命令 |

写法与设计准则见 [component.yaml 字段参考](../03-component-guide/02-component-yaml-reference.md)。

## 常见的错误写法

| 错误写法 | 为什么不行 | 正确写法 |
| --- | --- | --- |
| `version: ^1.2.0` / `latest` | 只接受精确版本 | `version: 1.2.0` |
| `version: local` | 本地源也写真实版本，否则依赖的精确匹配会落空 | 写 `component.yaml` 里的版本号 |
| `brickkit.yaml` 里写 `expose`、`mode` | 那是部署方式 | 写在部署文件这个组件的条目上 |
| `deploy.yaml` 里写 `mode: debug` | 它是个人事实，不进 Git | 写在 `deploy.local.yaml` |
| 只写 `localPort` 不写 `mode` | `localPort` 只对本机进程有意义 | 配 `mode: local` 或 `mode: debug`（`focus` 组件的条目不用写：它按 `local` 跑） |
| 配置键写成 `dbHost` 而组件读 `DB_HOST` | 键就是环境变量名，原样注入 | 与 `configSchema` 里的键一字不差 |
| `DATABASE_URL: $var:PG_HOST:5432` | `$var:` 必须是整个值 | 把整串值作为一个公共变量写进 `config/vars.yaml`（`PG_URL: jdbc:postgresql://pg.internal:5432/people`），引用 `$var:PG_URL`；或者把各段分开引用。字符串里的 `${PG_HOST}` 读的是进程环境变量和 `.env`，不是 `config/vars.yaml` |
| 密钥写成明文 | `config/` 进 Git | `${VAR}` 或 `file://` |
| 同一个组件版本在部署文件里写了两个条目 | 每个版本恰好一个条目 | 删掉多余的那个 |
| 外壳成员写在顶层、另外列一份成员 ID | 成员关系只有一个来源 | 成员条目嵌在外壳条目的 `members` 下 |
