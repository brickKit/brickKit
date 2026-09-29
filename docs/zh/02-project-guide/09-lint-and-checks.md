# 离线检查

## `brickkit lint`

不联网、不需要 Docker 或 Kubernetes、不改任何文件，几秒钟把项目里的 YAML 检查一遍：

```bash
brickkit lint
```

```text
✅ brickkit.yaml
✅ deploy.yaml
✅ deploy.local.yaml
✅ 跨文件：brickkit.yaml ↔ deploy.yaml ↔ config/
✅ 跨文件：brickkit.yaml ↔ deploy.local.yaml ↔ config/
✅ components/demo/hello/component.yaml

📋 检查了 6 个文件：0 个有错误，0 条警告
```

在项目目录里（有 `brickkit.yaml`），它依次查：

| 查什么 | 例子 |
| --- | --- |
| `brickkit.yaml` 的结构 | 必填字段、类型、版本号只能是精确版本 |
| 部署文件的结构 | 未知字段（拼写笔误）、`mode: debug` 写进了 `deploy.yaml`、`localPort` 没配 `mode`、宿主机端口冲突 |
| 三层对不对得上 | 每个组件版本在部署文件里恰好一个条目；外壳成员写在外壳下面；`config/` 的文件对得上组件 |
| `config/` 的引用 | `$var:` 都有定义；没有升级遗留的冲突块（重复键） |
| 配置与 `configSchema` | 必填项有值；写下的键在 schema 里（拼错的键不会生效）；外壳成员的值能编码进外壳 |
| 本地源里的 `component.yaml` | 每一份都查，不管有没有 `add` 过（`.archived/` 不查） |

`deploy.local.yaml` 存在时也查——不管本地模式开没开，它都必须与 `brickkit.yaml` 一致。给了 `-f` 就只查那一份部署文件。
`brickkit.yaml` 自己没通过时，后面的都不可信，跳过并说明原因。

在组件仓库里（有 `component.yaml`、没有 `brickkit.yaml`），只查那一份 `component.yaml`。

一个典型的错误：

```text
✅ brickkit.yaml
❌ 错误：deploy.yaml 校验失败
   文件：deploy.yaml
   components[1].exposed：未知字段（第 8 行），是不是想写 expose？
   建议：完整字段说明：docs/zh/11-reference/03-deploy-yaml-schema.md（英文版把 zh 换成 en）
✅ deploy.local.yaml
✅ components/demo/hello/component.yaml

📋 检查了 4 个文件：1 个有错误，0 条警告
❌ 错误：结构检查未通过
   已检查：4 个文件
   有错误：1 个文件
   建议：按上面逐条列出的位置修改，再执行 brickkit lint
```

未知字段是错误而不是警告：`exposed: true` 写错了，组件就不会对外开放，而且不会有任何别的地方告诉你。

## `--strict`

`--strict` 多查一类东西——引用的**值**在不在：进程环境与 `.env` 里都找不到的 `${VAR}`、文件不存在的 `file://`。
并且警告也算失败：

```bash
brickkit lint --strict
```

```text
⚠️ 警告：demo/caller@1.0.0 的 DATABASE_NAME 指向的 file:// 文件不存在
   路径：.secrets/dbname
   建议：file:// 的路径相对项目根；这类文件不要进 Git（比如放在 .secrets/ 下）
⚠️ 警告：demo/caller@1.0.0 的 DATABASE_USER 引用的环境变量在这里没有设置
   变量：DB_USER
   建议：先查进程环境，再查项目根目录的 .env——在其中一处设好它
```

```text
📋 检查了 6 个文件：0 个有错误，2 条警告
❌ 错误：结构检查未通过
   已检查：6 个文件
   警告：2 条（--strict：警告也算失败）
   建议：按上面逐条列出的位置修改，再执行 brickkit lint
```

不带 `--strict` 时这两项不查：在开发机上，生产的口令本来就不在环境里。

## 退出码

| 结果 | 退出码 |
| --- | --- |
| 没有问题，或只有警告 | 0 |
| 有错误 | 1 |
| `--strict` 下有警告 | 1 |

## `lint` 不查什么

- **依赖能不能解析、外壳与成员对不对得上。** 这要把所有组件的 `component.yaml` 都拿到、建出完整的依赖图——对 Git 或市场上的组件来说就是联网。
  `lint` 承诺离线、只读、立即返回，所以不建这张图。这些留给 `brickkit up --dry-run`：它反正要解析依赖图，缺了强依赖、外壳声明对不上都会点名报错。
- **配置的值合不合 `configSchema` 的约束**（`enum`、`minimum`、`pattern`……）。平台只检查键名、不检查值：值错了，组件自己会大声报错；
  键名错了则什么都不会发生，只是你的配置悄悄没生效——所以只查后者。见 [configSchema 规范](../11-reference/04-config-schema-spec.md)。

## 放进 CI

```bash
brickkit lint --strict
brickkit lint --strict -f deploy.prod.yaml
brickkit up --dry-run -f deploy.prod.yaml
```

- `lint --strict` 作为门禁：结构错误和未定义的引用都挡在合并之前。CI 里要有这些 `${VAR}` 的值（或者这一步只查结构，去掉 `--strict`）。
- 每个环境的部署文件用 `-f` 各查一遍：默认只查 `deploy.yaml`。
- 再加一步 `up --dry-run`，把依赖解析也查了——它需要能访问安装源，但不需要能访问集群。
