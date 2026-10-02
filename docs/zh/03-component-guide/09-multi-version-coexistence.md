# 多版本共存

## 服务名带着版本

每个组件版本运行时有一个**版本化服务名**：组件 ID 里的 `/` 和 `.` 换成 `-`，再接上精确版本。

| 组件版本 | 服务名 | 别人拿到的地址 |
| --- | --- | --- |
| `demo/hello@1.0.0` | `demo-hello-1-0-0` | `http://demo-hello-1-0-0:8080` |
| `demo/hello@1.1.0` | `demo-hello-1-1-0` | `http://demo-hello-1-1-0:8080` |

这个地址在 Docker 和 Kubernetes 上一模一样（Docker Compose 的服务名、Kubernetes 的 Service 名都是它）。两个版本是两个名字，
DNS 天然把它们分开：同时运行，互不冲突，不需要任何额外的机制。

地址变量的**名字**不带版本（`DEMO_HELLO_ENDPOINT`），**值**带版本。调用方永远知道自己连的是哪个版本，没有"悄悄升级"这回事。

## 什么时候会出现两个版本

`demo/caller@1.0.0` 声明依赖 `demo/hello@1.0.0`，`demo/quote@0.1.0` 声明依赖 `demo/hello@1.1.0`。两个组件都在同一个项目里时，
两个版本的 `demo/hello` 都会被加进来——依赖的版本不一致**不是错误**：

```yaml
# brickkit.yaml
components:
  - id: demo/hello
    version: 1.1.0
  - id: demo/quote
    version: 0.1.0
  - id: demo/hello
    version: 1.0.0
    requiredBy: [demo/caller]
```

不带 `requiredBy` 的那个是**默认版本**；带 `requiredBy` 的是"因为谁要它而留下"的兼容版本，一眼可知为何存在。`add`、`upgrade` 维护这些标记：
没人再要的兼容版本会被清掉。

生成的部署文件里（节选），每个调用方连的是它自己声明的那个版本：

```text
  demo-caller-1-0-0:
      - DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:8080
  demo-hello-1-0-0:
      - COMPONENT_VERSION=1.0.0
  demo-hello-1-1-0:
      - COMPONENT_VERSION=1.1.0
  demo-quote-0-1-0:
      - DEMO_HELLO_ENDPOINT=http://demo-hello-1-1-0:8080
```

## 每个版本一份配置

| 文件 | 属于 |
| --- | --- |
| `config/demo-hello.yaml` | 默认版本（1.1.0） |
| `config/demo-hello@1.0.0.yaml` | 兼容版本 1.0.0 |

不带版本号的文件永远跟着默认版本走；默认版本移动时（`upgrade`），配置按迁移规则搬过去，旧版本如果还被依赖，它原来的配置改名成带版本号的文件留下
（见 [升级与配置迁移](../02-project-guide/07-upgrade-and-migration.md)）。部署文件里也一样：裸 ID 的条目（`- id: demo/hello`）是默认版本，
其余版本各有自己的 `id@版本` 条目。

## 作为组件作者要知道的

- **一个组件的 `dependencies` 里，同一个组件 ID 只能出现一次。** 地址变量名不带版本，写两个版本会撞在同一个变量上。多版本共存是**项目层面**的能力：
  不同的组件各自依赖不同的版本。
- **两个版本可能共用一个数据库。** 它们各跑各的迁移；数据层兼容不兼容（新版本加的列旧版本认不认、旧版本写的数据新版本读不读得了），是组件作者的责任。
  迁移状态表的主键要带上组件标识，否则两个组件共用一个库时迁移记录会互相覆盖。
- **不兼容的改动要提主版本号。** 使用方靠版本号判断能不能升；你悄悄在修订号里改了接口，调用方就在不知情的情况下坏掉。

## 什么时候需要共存，什么时候不需要

需要：

- **不同的组件依赖不同的版本**，而其中一个还没来得及跟上——共存是一个缓冲，给它升级的时间。
- **灰度升级**：新版本先给一部分调用方用，旧版本继续服务其余的。

不需要：所有调用方都升级完了，旧版本没有任何 `requiredBy`，它自然会被 `upgrade` / `remove` 清掉。共存是一个过渡状态，不是目标——每多一个版本，
就多一份镜像、一份配置、一组要照看的容器。

有一种情况旧版本不会自己消失：依赖方**版本号没变**，却把依赖改钉到了新版本（在本地源里开发时常见）。`upgrade` 看到依赖方已是最新、
什么都不做，带 `requiredBy` 的那一行就一直留着。`brickkit lint` 会指出来，并给出要执行的命令：

```text
ℹ️ demo/hello@1.0.0 只因 demo/caller 才在项目里，而按它们现在的 component.yaml 已经没有谁依赖它——要移除：brickkit remove demo/hello@1.0.0
```
