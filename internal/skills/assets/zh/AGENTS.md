# 这个项目用 BrickKit 拼装

> 写给 AI 助手。人也能读，但它存在的理由是让助手不必猜。

BrickKit 是**声明式的组件管理与拼装平台**：每块积木（组件）独立开发、独立部署、独立调用；
你只声明有哪些组件、什么版本，`brickkit` CLI 负责把积木拉来、解析依赖、排启动顺序、
生成部署文件、交给 Docker / Podman / Kubernetes 跑起来——其余都从这张依赖图派生。
平台刻意极简：**没有注册中心、没有常驻服务、没有配置中心、没有网关**——别去找它们，
也别建议加上，那是被明确拒绝过的设计。

⚠️ 「没有网关」不等于「不能用网关」：网关**带外部署**，靠部署条目上的 `labels`
透传（平台原样搬给 Docker labels / K8s annotations，不解释键值）自动发现。
用户要接 Traefik / Prometheus 时走这条路，别劝他放弃。

## 三层文件：每件事只有一个地方写

| 文件 | 回答什么 | 进 Git |
| --- | --- | --- |
| `brickkit.yaml` | **有什么**：项目名、安装源、组件与精确版本。它是锁文件 | 是 |
| `deploy.yaml` | **团队怎么部署**：target、每个组件的部署条目（mode、端口、暴露、副本、外壳成员）、`k8s:` 块、`vars:` 覆盖 | 是 |
| `deploy.local.yaml` | **你个人怎么跑**：`deploy.yaml` 的一份完整副本，本地模式开着时整份替代它 | 否 |
| `config/` | **每个组件拿到什么值**：一个组件版本一份文件，键就是环境变量名；`config/vars.yaml` 放公共变量 | 是（`config/.archive/` 除外） |
| `component.yaml` | 组件自己的 Manifest：依赖、端口、配置项、迁移、镜像 | 在组件仓库里 |

要回答「装了什么」读 `brickkit.yaml`，「怎么跑」读部署文件，「值是多少」读 `config/`。
**依赖关系不在 `brickkit.yaml` 里**——它们写在各组件自己的 `component.yaml`，
`brickkit deps` 把解析好的树打印出来。

判断当前生效的是哪份部署文件：`brickkit local status`。本地模式开着时，所有命令读的是
`deploy.local.yaml`，你改 `deploy.yaml` 不会有任何效果。

## 先读文档，再读源码

- **项目根的 `BRICKKIT.md`** 是项目地图。其中 `<!-- brickkit:managed:begin -->` 到
  `<!-- brickkit:managed:end -->` 之间的表由 CLI 维护（add / remove / upgrade 会重写它），
  列出每个组件和它的文档、契约的缓存路径——别手改这一段，改了下次就被覆盖；段外随便写。
- **每个组件的 `BRICKKIT.md`**（组件定位、依赖、配置指南、契约、外壳声明）缓存在
  `.brickkit/manifests/<scope>/<name>/<版本>/BRICKKIT.md`，旁边是那个版本的 `component.yaml`。
  要知道一个依赖怎么用、要配哪些值，先读这里；它比翻源码快，而且对闭源组件是唯一的说明。

## 五条铁律

1. **版本必须精确。** `1.2.0` 可以，`^1.2` / `~1.2` / `latest` 一律不行。
   范围版本是论证过后拒绝的，不是还没做。
2. **每个组件版本在部署文件里恰好一个条目。** 多了少了都是 `DEPLOY_INCONSISTENT`。
   `add` / `remove` / `upgrade` 会自动同步 `deploy.yaml` 与 `deploy.local.yaml`，
   所以增删组件走命令，别手改三份文件。
3. **配置的键就是环境变量名。** `config/erp-backend.yaml` 里写 `DB_HOST: ...`，
   组件拿到的就是 `DB_HOST`，没有任何大小写或驼峰转换。
4. **健康检查只查本进程。** 别把依赖的可用性写进自己的健康检查——一个组件的抖动会
   级联成整片不健康。冷启动超过默认 60 秒的组件要调大 `startPeriodSeconds`。
5. **启停跟着上层走。** 顶层组件关掉，它下面那一串跟着不启动。想收窄范围就改部署文件
   里顶层条目的 `mode`，别逐个关。

## 别去记参数

**任何命令的参数都去问 `brickkit <命令> --help`。** 这份文件和 `.claude/skills/`
下的技能都刻意不复刻参数清单：复刻一份就是承诺维护两份，而过期的那份会让你
自信地敲出一条 `unknown flag`。

## 哪个技能管什么

`.claude/skills/` 下按任务分了四个技能，Claude Code 会在相关时自动加载：

| 技能 | 管什么 |
| --- | --- |
| `brickkit-assemble` | 增删、升级组件，默认版本与兼容版本，`brickkit.yaml` 锁文件，启停与 `mode`，up / down / status / deps |
| `brickkit-component` | 写或改 `component.yaml`：配置项、保留名、依赖、镜像与构建、外壳、`BRICKKIT.md`、迁移、健康检查、release |
| `brickkit-deploy` | `deploy.yaml` / `deploy.local.yaml` 字段、本地模式、mode debug/local、多环境、`config/` 的值与密钥写法、镜像、外壳成员、K8s |
| `brickkit-troubleshoot` | 报错与症状 → 原因 → 处理，按 `error_code` 定位 |

**想让 Claude Code 也读到这一页导读**（它只读 `CLAUDE.md`，不读本文件），
在你自己的 `CLAUDE.md` 里加一行：

```
@AGENTS.md
```

不加也行，四个技能照样能用。你的 `CLAUDE.md` 是你自己的流程文件，
`brickkit` 不会去动它。

完整规范在仓库：<https://github.com/brickKit/brickKit>，根目录 `AGENTS.zh.md` 是全站压缩件。
