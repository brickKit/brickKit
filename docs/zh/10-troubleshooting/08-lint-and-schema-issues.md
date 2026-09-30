# lint 与 Schema 问题

三道检查一层比一层查得多：编辑器里的 JSON Schema 只看单个文件的结构；`brickkit lint` 再查字段组合与三层文件之间对不对得上；
`brickkit up --dry-run` 再解析完整的依赖图。三者结论不一致时，多半是因为它们查的范围不同，而不是哪一个错了。

## `lint` 报错，`up` 却正常

**症状**

`brickkit up` 跑得好好的，`brickkit lint` 却报错，比如：

```text
❌ 错误：deploy.local.yaml 已过期，与 brickkit.yaml 的组件对不上
   文件：deploy.local.yaml
   缺少条目：demo/bus
   原因：brickkit.yaml 变了，你的 deploy.local.yaml 没有同步。本地模式现在关着，命令不读它；但下次 brickkit local on 就会读到它
   建议：
   1. 方案 A（推荐）：brickkit local refresh 按 deploy.yaml 重新生成 deploy.local.yaml，旧文件备份为 deploy.local.yaml.bak
   2. 方案 B：在 deploy.local.yaml 里手动增删下面列出的条目
   3. 方案 C：不再需要它，就删掉 deploy.local.yaml（它不会被提交）
```

**原因**

`lint` 查的东西比这一次 `up` 用到的多：

| `lint` 查、这次 `up` 不一定读的 | 为什么 |
| --- | --- |
| `deploy.local.yaml`——**不管本地模式开没开** | 本地模式关着时 `up` 不读它；但你随时可能 `local on`，到那时它必须与 `brickkit.yaml` 一致。团队加了组件、你的个人文件没跟上，就会在这里报出来 |
| 本地源里**每一份** `component.yaml`，不管有没有 `add` 过 | 还没加进项目的组件，`up` 不会读；`lint` 让它在被使用之前就是对的 |
| `--strict` 下：`${VAR}` 在进程环境与 `.env` 里有没有值、`file://` 指向的文件在不在 | Docker 下 `${VAR}` 由 `docker compose` 在启动时才展开，`up` 本身不检查它 |

**解决**

按报错改。个人文件过期：`brickkit local refresh`（本地模式关着时也能用；旧文件备份，列出你以前的本地修改）；不再需要它，删掉即可。
只想检查某一份部署文件：`brickkit lint -f deploy.prod.yaml`。

## `lint` 通过，`up --dry-run` 却报错

**症状**

`lint` 全绿，`up --dry-run` 报 `强依赖缺失`、外壳承载的版本对不上，或者组件找不到。

**原因**

这是有意的分工。`lint` 承诺离线、只读、立即返回，所以它**不解析依赖图**：要解析，就得拿到每个组件的 `component.yaml`，
对 Git 或市场上的组件来说就是联网。依赖在不在、外壳这次承载的成员版本对不对，都要在依赖图上才能判断。

**解决**

在 CI 里两步都跑：`lint --strict` 挡结构问题，`up --dry-run` 挡依赖问题（它要能访问安装源，但不需要能访问集群）。见 [离线检查](../02-project-guide/10-lint-and-checks.md#放进-ci)。

## 编辑器没标红，`lint` 却报错

**原因**

JSON Schema 只描述**单个文件的结构**：字段名、类型、必填、固定取值。下面这些它表达不了，由 `lint` 查：

- 字段之间的组合：`localPort` 要和 `mode: local` / `mode: debug` 一起写；`mode: debug` 只能写在 `deploy.local.yaml`。
- 三层文件之间：每个组件版本在部署文件里恰好一个条目；`$var:` 有定义；`config/` 里的键在组件的 `configSchema` 里。
- 配置项撞上平台保留的名字。

**解决**

按 `lint` 的报错改。编辑器不红只说明单个文件的结构是对的。

## 编辑器标红，`lint` 却通过

**症状与原因**，常见的两种：

- **`config/` 里的冲突块被标成重复键。** 这是正常的：BrickKit 故意用重复键表示升级留下的冲突。不过这种情况下 `lint` 也会报错（冲突未解决），
  见 [配置冲突问题](03-config-conflict-issues.md)。
- **编辑器用的 schema 与你的 CLI 版本不一致。** 按文档接的是 `main` 分支上的 schema，它可能比你装的 CLI 新（多了字段）或旧（少了字段）。
  以 CLI 为准：`lint` 通过就是 CLI 接受的。

**解决**

把 schema 的 URL 从 `main` 换成你所用 CLI 版本的 tag，或者指向仓库里 `schemas/` 下与 CLI 同版本的本地副本。接法见 [JSON Schema](../11-reference/05-json-schemas.md)。

## 编辑器完全没有补全和校验

**原因**

- 没装 YAML 语言服务（VS Code 的 Red Hat YAML 扩展、JetBrains 自带、Neovim 的 yaml-language-server）。
- 文件名没对上映射：设置里映射的是 `deploy.yaml`，而你打开的是 `deploy.prod.yaml`。
- `config/*.yaml` 本来就没有 schema：每个组件的配置项不同，由它自己的 `configSchema` 决定，只有 `lint` 按它检查键名。

**解决**

按 [JSON Schema](../11-reference/05-json-schemas.md) 的方法二，用 `["deploy.yaml", "deploy.*.yaml"]` 这样的通配把所有部署文件都映射上。
