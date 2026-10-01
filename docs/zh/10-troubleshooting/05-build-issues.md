# 构建问题

`brickkit build` 构建需要在本机构建的镜像；`brickkit up` 从不构建。背景见 [构建与镜像](../02-project-guide/12-build-and-images.md)。

## 改了代码，跑起来还是旧的

**症状**

改了本地源里一个组件的代码，`brickkit build` 之后 `up`，行为没变。`build` 的输出是：

```text
⏭️  demo/hello@1.1.0：镜像 demo-hello:1.1.0 已存在，跳过（--force 重新构建）
```

**原因**

镜像的 tag 就是组件的版本号。版本号没变，tag 就没变，`build` 认为镜像已经是最新的，跳过了。

**解决**

开发中改了代码、没改版本号时，加 `--force`：

```bash
brickkit build demo/hello@1.1.0 --force
```

然后 `brickkit up`——`up` 看到镜像变了，会重建这个容器。要发布的改动，提版本号（`metadata.version`）再构建，就不需要 `--force`。

## `up` 说镜像还没构建

**症状**

```text
❌ 错误：这些镜像要在本机构建，还没有构建
   demo/hello@1.0.0：demo-hello:1.0.0
   demo/caller@1.0.0：demo-caller:1.0.0
   建议：
   1. up 从不自动构建（构建与部署分离）：先运行 brickkit build
   2. 或者只构建其中一个：brickkit build demo/hello@1.0.0
   3. 或者只构建其中一个：brickkit build demo/caller@1.0.0
```

**原因**

这些组件来自本地源，或者 `component.yaml` 只写了 `deployment.build`、没写 `image`——镜像要在本机构建，而本机还没有。
常见于刚克隆项目、刚 `upgrade` 到新版本、刚用 `add --repo` 把一个组件的源码克隆进 `components/`（之后它由本地源提供，镜像从你的工作区构建，不再拉取作者发布的那个）。

**解决**

`brickkit build`（全部）或按提示只构建其中一个。`up` 不顺手构建，是为了不把你工作区里改到一半的代码悄悄打进镜像跑起来。

## 镜像拉不到

**症状**

`up` 报 `错误：镜像拉取未授权` 或 `错误：镜像不存在`。

**原因**

组件写了 `deployment.image`，镜像在一个你没登录、没权限的仓库里，或者地址写错了、这个 tag 根本没推上去。

**解决**

- 没登录：`docker login <仓库>`。
- 镜像确实不存在：找组件作者确认镜像地址与 tag；组件同时写了 `build` 时，也可以把它的源码克隆进本地源（`add --repo`）改成本机构建。

## Dockerfile 有错

**症状**

`brickkit build` 失败，报错里带着 `docker build` 自己最后的输出。下面是 `COPY` 了一个不存在的文件（节选"输出"的末尾）：

```text
🔨 构建 shop/order@0.1.0 → shop-order:0.1.0
❌ 错误：docker 执行失败
   命令：docker build -t shop-order:0.1.0 -f components/shop/order/Dockerfile --label io.brickkit.build=local --label io.brickkit.component=shop/order --label io.brickkit.version=0.1.0 components/shop/order
   输出：#6 DONE 0.0s
      ……
      Dockerfile:3
      --------------------
      1 |     FROM golang:1.22-alpine AS build
      2 |     WORKDIR /src
      3 | >>> COPY missing.go go.mod *.go ./
      4 |     RUN CGO_ENABLED=0 go build -o /out/order .
      5 |
      --------------------
      ERROR: failed to build: failed to solve: failed to compute cache key: failed to calculate checksum of ref nw5giryoc7te33iudojbosk8l::j0da4oshnnvnwz567z08ob8g4: "/missing.go": not found
   组件：shop/order@0.1.0
```

`>>>` 指着出错的那一行。"命令"一行就是 BrickKit 实际执行的 `docker build`，可以原样复制出来单独跑。

**原因**

构建由 `docker build` 完成，BrickKit 只是按 `component.yaml` 的 `deployment.build` 给它传构建上下文和 Dockerfile 路径。两者都相对**组件仓库根**：
`context` 默认 `.`，`dockerfile` 默认 `Dockerfile`。常见的错：`dockerfile` 写成了相对 `context` 的路径；`COPY` 引用了构建上下文之外的文件。

**解决**

按 `docker build` 的原话修 Dockerfile。想单独复现，在组件仓库根目录执行一次同样的 `docker build`，不经过 BrickKit。

## submodule 目录是空的

**症状**

`build` 在构建之前给出警告，接着构建因为缺文件失败——或者构建成功了，组件却行为异常：

```text
⚠️ 警告：demo/lib@1.0.0 的源码里有 git submodule，这里它们是空目录
   目录：third_party/sdk
   建议：BrickKit 从不拉取 submodule；构建要是需要它们，这个组件应当发布现成的镜像（deployment.image），或者让构建不依赖它们
```

**原因**

BrickKit 从不拉取 git submodule——仓库缓存里不拉，从 tag 导出源码来构建时不拉，`add --repo` 也不拉（见
[bare 仓库机制](../06-architecture/06-bare-repo-mechanism.md#从不拉取-git-submodule)）。目录在，是空的。

**解决**

- 长久的办法在组件那一侧：发布镜像（`deployment.image`），谁都不用从源码构建它。
- 如果你是从 `components/` 下克隆的仓库构建，自己在里面 `git submodule update --init`；那份工作区正好是这个版本时，`build` 用的就是它。
- 或者让构建不依赖它们。

## 镜像太大

**症状**

镜像几百 MB 甚至上 GB，构建、推送、拉取都慢。

**原因与解决**

与 BrickKit 无关，是 Dockerfile 的写法。常用的几条：

- **多阶段构建**：编译在一个带完整工具链的阶段，最终镜像只拷贝产物。Go 组件的最终镜像可以只有十几 MB。
- **选小的基础镜像**：`alpine`、`*-slim`。
- **`.dockerignore`** 排除 `.git`、`node_modules`、测试数据。

只有一条要留意：Docker 下健康检查在容器**里面**经由 `/bin/sh` 执行——`type: http` 要镜像里有 `wget` 或 `curl`，`type: tcp` 要有 `nc`。
换成根本没有 shell 的基础镜像（`scratch`、distroless）之后，**两种类型都过不了**：容器会一直被判为不健康，而组件自己的日志看起来一切正常。
这时最终阶段改为基于 `busybox` 或 `alpine`（多几 MB，带着 `sh`、`wget`、`nc`）；镜像必须保持没有 shell 的话，就写 `healthCheck.type: none`，
接受依赖方只等容器启动。K8s 下探针从容器外面发起，不需要这些工具。见 [up / down 常见问题](01-up-down-issues.md#组件日志正常平台却说它不健康)。
