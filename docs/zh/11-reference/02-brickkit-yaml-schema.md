# brickkit.yaml 字段参考

项目用哪些组件、各是哪个精确版本、从哪里取。它是项目的锁文件。怎么用，见 [brickkit.yaml](../01-three-layers/02-brickkit-yaml.md)。

写了不认识的字段当场报错。部署方式（`mode`、`expose`……）不写在这里，写在部署文件里。

## 顶层

| 字段 | 类型 | 必填 | 规则 |
| --- | --- | --- | --- |
| `project` | 字符串 | ✅ | 小写字母、数字、中划线，以字母或数字开头结尾（`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`）；最长 54 个字符（K8s 命名空间最长 63，减去 CLI 加的 `brickkit-` 前缀）。它成为 Docker 网络、Compose 项目与 K8s 命名空间的名字 |

## sources

安装源：去哪里找组件。按声明顺序依次尝试，第一个有这个组件的源说了算。

| 字段 | 类型 | 必填 | 规则 |
| --- | --- | --- | --- |
| `sources[].name` | 字符串 | ✅ | 安装源的名字，在这份文件里唯一 |
| `sources[].type` | 字符串 | ✅ | `git` / `local` / `market` |
| `sources[].baseUrl` | 字符串 | `git` 必填 | 组件 `<scope>/<name>` 的仓库是 `<baseUrl>/<scope>-<name>`（`baseUrl` 末尾的 `/` 可写可不写，CLI 只补一个）；不能以 `-` 开头 |
| `sources[].path` | 字符串 | `local` 必填 | 本机目录，相对项目根；里面按 `<scope>/<name>/component.yaml` 摆放 |
| `sources[].url` | 字符串 | `market` 必填 | 组件市场的 API 地址 |
| `sources[].authToken` | 字符串 | | 市场令牌，通常写 `${VAR}`；已 `brickkit login` 时优先用登录凭据 |
| `sources[].enabled` | 布尔 | | 缺省 `true`；`false` 时这个源不参与查找 |

## components

每个组件版本一个条目。

| 字段 | 类型 | 必填 | 规则 |
| --- | --- | --- | --- |
| `components[].id` | 字符串 | ✅ | 组件 ID，规则同 `component.yaml` 的 `metadata.id` |
| `components[].version` | 字符串 | ✅ | 精确版本。本地源的组件也写 `component.yaml` 里的真实版本 |
| `components[].kind` | 字符串 | | 只能是 `shell` 或不写；由 CLI 在 `add` 外壳时写上，必须与组件的 `shell` 块一致；同一个组件 ID 的所有版本一致 |
| `components[].requiredBy` | 字符串列表 | | 这个版本因哪些组件依赖而留下（兼容版本）；每一项必须是这份文件里的组件 ID，不能是自己 |
| `components[].source.type` | 字符串 | ✅（写了 `source` 时） | `git` / `local` |
| `components[].source.repo` | 字符串 | `git` 必填 | 这个组件的仓库地址（推导不出来、或在别的组织时） |
| `components[].source.path` | 字符串 | `local` 必填 | `local`：组件目录，相对项目根；`git`：组件在仓库里的子目录（monorepo），不能是绝对路径、不能走出仓库 |

版本之间的规则：

- 同一个组件版本只能出现一次。
- 同一个组件 ID 有几个版本时，**恰好一个**不写 `requiredBy`——它是默认版本；其余都写 `requiredBy`。
- 外壳在一个项目里只能有一个版本，也就不能写 `requiredBy`。
- 同一个组件的几行，`source` 必须写得一样（一个组件只有一个仓库）。

## installer

组件市场签名的校验设置。只对来自市场的组件生效。

| 字段 | 类型 | 必填 | 规则 |
| --- | --- | --- | --- |
| `installer.requireSignature` | 布尔 | | 缺省 `true`：没有签名的市场组件不安装 |
| `installer.publicKeys` | 字符串映射 | | 信任的发布者公钥：公钥名（签名里的 `publicKeyRef`）→ 公钥文件路径（相对项目根）。**一把都没配时签名校验完全关闭** |

见 [安全与签名](../06-architecture/08-security-and-signing.md)。
