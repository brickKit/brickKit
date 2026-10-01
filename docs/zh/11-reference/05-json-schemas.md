# JSON Schema

## `schemas/` 目录

仓库的 `schemas/` 下有三份 JSON Schema（draft-07），描述三种文件的结构：

| 文件 | 描述 |
| --- | --- |
| `schemas/component.schema.json` | 组件的 `component.yaml` |
| `schemas/brickkit.schema.json` | 项目的 `brickkit.yaml` |
| `schemas/deploy.schema.json` | 部署文件：`deploy.yaml`、`deploy.local.yaml`、`deploy.<环境>.yaml` |

它们是从 CLI 的 Go 结构体生成的，不是手写的：结构体改了，重新生成、一起提交；`make lint`（其中的 `check-schemas`，每次发布前也会跑）拦住"改了结构体却忘了重新生成"。
所以 schema 里的字段与 CLI 真正接受的字段逐一对应。

## 接进编辑器

装了 YAML 语言服务的编辑器（VS Code 的 Red Hat YAML 扩展、JetBrains 系列、Neovim 的 yaml-language-server……）接上 schema 之后：
字段名有补全，类型不对、必填字段没写、拼错的字段名立刻标红——与 `brickkit lint` 报的是同一批结构问题，只是在你打字的时候就看到。

**方法一：文件顶部加一行注释。** 对单个文件生效，谁打开都一样：

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/brickKit/brickKit/main/schemas/deploy.schema.json
target: docker
components:
  - id: demo/hello
```

`component.yaml` 用 `component.schema.json`，`brickkit.yaml` 用 `brickkit.schema.json`。

**方法二：编辑器设置里按文件名映射。** VS Code 的 `settings.json`：

```json
{
  "yaml.schemas": {
    "https://raw.githubusercontent.com/brickKit/brickKit/main/schemas/component.schema.json": "component.yaml",
    "https://raw.githubusercontent.com/brickKit/brickKit/main/schemas/brickkit.schema.json": "brickkit.yaml",
    "https://raw.githubusercontent.com/brickKit/brickKit/main/schemas/deploy.schema.json": ["deploy.yaml", "deploy.*.yaml"]
  }
}
```

放进项目的 `.vscode/settings.json`，整个团队打开项目就都有。想固定在某个 CLI 版本上，把 URL 里的 `main` 换成那个版本的 tag，或者指向本地的一份副本。

## 覆盖什么，不覆盖什么

JSON Schema 只描述**单个文件的结构**：字段名、类型、必填、固定的取值（`target` 只能是 `docker` / `podman` / `k8s` 这类）。

不覆盖的，归别的检查：

| 规则 | 谁查 |
| --- | --- |
| 字段之间的组合（`localPort` 要配 `mode`、`exposePort` 要配 `expose`、`mode: debug` 只能在 `deploy.local.yaml`） | `brickkit lint` |
| 配置项撞上平台保留的名字、`configSchema` 某一项里的笔误 | `brickkit lint` |
| 三层文件之间对不对得上（每个组件版本一个部署条目、`$var:` 有定义、`config/` 的键在 `configSchema` 里） | `brickkit lint` |
| 依赖能不能解析、外壳承载的成员版本对不对 | `brickkit up --dry-run` |
| `config/*.yaml` 里的配置值 | 没有 schema：每个组件的配置项不同，由它自己的 `configSchema` 决定，`lint` 按它检查键名 |

编辑器里不红，不等于 `lint` 能过；`lint` 能过，也不等于 `up --dry-run` 能过——三者一层比一层查得多。
