# config/ 目录详解

`config/` 回答"**每个组件拿到哪些环境变量**"。每个组件一份扁平的 YAML 文件，**组件 `configSchema` 里声明了的**
每个键，原样成为一个同名的环境变量。没声明的键会被警告、不会生效；组件根本没有 `configSchema` 时，它的文件里什么都
不会注入（见 [与 `configSchema` 的关系](#与-configschema-的关系)）。

## 目录结构

```text
config/
├── vars.yaml                  公共变量：多个组件共用的值，用 $var:NAME 引用
├── department-tree.yaml       department/tree 默认版本的配置
├── people-basic.yaml          people/basic 默认版本的配置
├── people-basic@0.9.0.yaml    people/basic 0.9.0（只因别的组件依赖而在）的配置
└── .archive/                  remove 时归档的配置（默认不进 Git）
```

## 文件命名

组件 ID 里的 `/` 换成 `-`：`department/tree` → `department-tree.yaml`。

同一个组件有好几个版本时（见 [brickkit.yaml 的默认版本](02-brickkit-yaml.md#默认版本与-requiredby)）：

| 文件 | 属于谁 |
| --- | --- |
| `config/people-basic.yaml` | 默认版本（`brickkit.yaml` 里不带 `requiredBy` 的那一行） |
| `config/people-basic@0.9.0.yaml` | 带 `requiredBy` 的那个版本；`add` 为它加入这个版本时自动生成骨架 |

默认版本同时有两份文件（不带版本号的和 `@默认版本号` 的）是错误：到底该用哪份说不清，CLI 直接拒绝。
不属于任何已声明版本的文件也会被报出来——它多半是某次改版本后留下的孤儿。

## 配置骨架

`add` 按组件 `configSchema` 为它生成一份骨架，分两段：

```yaml
# Component: department/tree@1.0.0
# 这个组件的环境变量，每个键原样注入。
# 公共变量：$var:NAME（config/vars.yaml）· 环境变量：${NAME} · 本地文件：file://path

# === 必填：没有值就无法启动 ===
DATABASE_HOST: ""  # string | PostgreSQL host name
DATABASE_NAME: ""  # string | Database name
DATABASE_USER: ""  # string | User to connect as

# === 可选：注释掉的键使用组件默认值，取消注释即可覆盖 ===
# DATABASE_PASSWORD:  # string | secret | Password to connect with. Write it as a ${VAR} or file:// reference, never as plain text in config/
# DATABASE_PORT: 5432  # integer | PostgreSQL port (默认值)
# LOG_LEVEL: info  # string | Log level (debug | info | warn | error) (默认值)
```

- **必填、没有默认值的项**留一个空值，由你填上。填之前 `up` 会停下来并点名缺哪几项：

  ```text
  ❌ 错误：必填的组件配置没有值
     缺少配置：department/tree@1.0.0 → DATABASE_HOST
     缺少配置：department/tree@1.0.0 → DATABASE_NAME
     缺少配置：department/tree@1.0.0 → DATABASE_USER
     原因：组件在 configSchema.required 里声明了它，又没有给默认值——这一项平台推导不出来，只能由项目提供
  ```

- **可选项写成注释**，注释里带着它的默认值。你没取消注释的项，永远跟随组件当前版本的默认值——组件升级改了默认值，
  你自动拿到新的；只有你真正写下的值才归你管，升级时才可能冲突（见 [升级与配置迁移](../02-project-guide/07-upgrade-and-migration.md)）。
- **声明了 `secret: true` 的项**在注释里标着 `secret`，提醒你用引用而不是明文（见 [敏感值](07-sensitive-values.md)）。

如果 `config/vars.yaml`（或部署文件的 `vars:`）里已经有同名的公共变量，`add` 会问你要不要直接引用它；`--yes` 时一律引用：

```text
🔗 config/department-tree.yaml 的 DATABASE_HOST 引用 config/vars.yaml 里的同名变量（--yes）
```

```yaml
DATABASE_HOST: $var:DATABASE_HOST  # string | PostgreSQL host name
```

已经存在的配置文件，`add` 一个字节都不动。

## `.archive/`：移除时归档，重新添加时恢复

`remove` 一个组件时，它的配置不删，而是带上版本号移进 `config/.archive/`：

```text
➖ 已移除 department/tree@1.0.0
🗄️  配置已归档：config/department-tree.yaml → config/.archive/department-tree@1.0.0.yaml
```

以后重新 `add` 同一个组件，配置从归档里恢复，走的是和升级同一套迁移算法（归档的是旧版本时，按新版本的
`configSchema` 迁移）：

```text
♻️  config/department-tree.yaml 从归档恢复（config/.archive/department-tree@1.0.0.yaml，按新版本迁移）
📝 config/department-tree.yaml
   原样保留：DATABASE_HOST, DATABASE_NAME, DATABASE_USER
```

从归档恢复时，归档里就是你当初写的配置，以它为准，不再问要不要引用公共变量。`.archive/` 默认在 `.gitignore` 里：
它是这台机器上"以后可能还用得着"的东西，不是项目的一部分。

## 与 `configSchema` 的关系

组件的 `configSchema` 是**说明书**：它列出组件认哪些配置项、各是什么类型、默认值是什么、哪些必填、哪些是密钥。
`config/` 里的文件是**实际的值**。

平台检查**键名**，不检查**值**：

- 写了 `configSchema` 里没有的键，`up` 和 `lint` 都会警告——拼错一个字母的键不会报错，只会悄悄不生效，
  所以必须有人说出来：

  ```text
  ⚠️ demo/hello@1.0.0：GREETNG 不在组件的 configSchema 里，不会生效
     文件：config/demo-hello.yaml
     已声明的配置项：GREETING
     建议：是不是想写 GREETING？
  ```

- 组件根本没有声明 `configSchema` 时，它不认任何配置项，配置文件里的内容一项都不注入；`up` 和 `lint` 会警告，
  并列出被忽略的键。

- 值的类型、枚举、范围不校验：组件拿到一个不合法的值时怎么处理（报错退出、回落默认值），是组件自己的事。
  详见 [configSchema 规格](../11-reference/04-config-schema-spec.md)。
