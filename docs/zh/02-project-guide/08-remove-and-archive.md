# 移除组件与归档

## `brickkit remove` 先检查什么

**有好几个版本时，必须写明删哪一个：**

```text
❌ 错误：demo/hello 有好几个版本（1.1.0, 1.0.0），要写明删哪一个
   建议：例如 brickkit remove demo/hello@1.1.0
```

**还有组件强依赖它时，不删：**

```text
❌ 错误：demo/hello@1.0.0 还被这些组件强依赖：demo/caller@1.0.0
   建议：先移除（或升级）依赖它的组件，再移除它
```

删了它，依赖它的组件就起不来——与其让你在下一次 `up` 时才发现，不如现在就停下。

**只被弱依赖时，照删，并告诉你影响：**

```bash
brickkit remove demo/bus
```

```text
➖ 已移除 demo/bus@1.0.0
ℹ️  demo/caller@1.0.0 弱依赖 demo/bus@1.0.0：它照样运行，只是不再拿到 demo/bus@1.0.0 的地址
🗄️  配置已归档：config/demo-bus.yaml → config/.archive/demo-bus@1.0.0.yaml
📝 已写：brickkit.yaml, deploy.yaml, deploy.local.yaml
```

## 移除做了什么

- `brickkit.yaml` 里的声明删掉；
- 部署文件里它的条目删掉（`deploy.yaml`，以及存在的 `deploy.local.yaml`；其它 `-f` 用的部署文件会在输出里点名，自己同步）；
- 它的配置**不删**，移进 `config/.archive/<组件>@<版本>.yaml`；
- 只因它而保留的兼容版本（`requiredBy` 只剩它的那些）一并移除；
- 删的是外壳时，它承载的成员挪回部署文件的顶层，独立运行。

**默认版本转正。** 删掉默认版本、而这个组件还剩一个版本时，剩下的那个成为新的默认版本，它的配置文件也改回不带版本号的名字：

```bash
brickkit remove demo/hello@1.1.0
```

```text
➖ 已移除 demo/hello@1.1.0
   ⭐ demo/hello@1.0.0 转正成默认版本
🗄️  配置已归档：config/demo-hello.yaml → config/.archive/demo-hello@1.1.0.yaml
📝 已写：brickkit.yaml, deploy.yaml, deploy.local.yaml
```

## 归档与恢复

`config/.archive/` 是本机的后悔药（不进 Git）。以后重新 `add` 同一个组件，配置从归档里恢复，而不是给你一份空骨架：

```bash
brickkit add demo/bus
```

```text
🔎 demo/bus 没写版本，最新的是 1.0.0（安装源 company-git）
➕ 加入 demo/bus@1.0.0
   ✅ demo/bus@1.0.0
📝 已写：brickkit.yaml, deploy.yaml, deploy.local.yaml
♻️  config/demo-bus.yaml 从归档恢复（config/.archive/demo-bus@1.0.0.yaml，按新版本迁移）
📝 config/demo-bus.yaml
   原样保留：GREETING
📦 产物：1 个文件，在 .brickkit/artifacts/
```

恢复走的是和升级同一套[迁移规则](07-upgrade-and-migration.md#配置怎么迁移)，不是直接复制：你加回来的可能是另一个版本，
新版本删掉的项、新增的项、改了默认值的项都按那张表处理。归档里有好几个版本时，取同版本的那一份；没有就取不高于这次版本的最高一份；再没有才取最高的那份。

## 源码目录

组件的**最后一个版本**被移除时，`components/` 下它的源码目录（以及 `components/.archived/` 里归档的那一份）也一起删掉——
不删的话，它就成了一个没人认领、`sync` 也不再管的目录。

但删之前先确认删了还找得回来：

```text
❌ 错误：源码目录删了就找不回来，没有删
   组件：demo/hello
   目录：components/demo/hello/
   原因：有未提交的改动（含未跟踪的文件）
   建议：
   1. 先保存好：提交并推送到远端，或者把目录挪到别处
   2. 确定不要了：brickkit remove demo/hello --force
```

有未提交的改动、或者有没推送到远端的提交，就停下。确定不要了，加 `--force`：

```text
➖ 已移除 demo/hello@1.0.0
🗄️  配置已归档：config/demo-hello.yaml → config/.archive/demo-hello@1.0.0.yaml
📝 已写：brickkit.yaml, deploy.yaml, deploy.local.yaml
🗑️  源码目录已删：components/demo/hello/
```

`remove` 是"彻底移除"：声明、部署条目、源码都走；只有配置留在归档里，因为它可能是你花时间调出来的值。
