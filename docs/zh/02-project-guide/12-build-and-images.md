# 构建与镜像

## 构建与部署分开

`brickkit up` **从不构建镜像**。镜像不在，它报错、告诉你运行 `brickkit build`，然后停下。

为什么这么较真：构建是把一份代码变成一个要去运行的东西，是一个需要你决定的动作——这份代码是不是你想发布的那份？
部署则是机械的：把已经确定的镜像按部署文件跑起来。如果 `up` 发现没镜像就顺手构建，那么你工作区里一份改到一半的代码，
就可能在你没打算的时候被打进镜像、跑了起来。两件事分开，`up` 做的事就永远可预期：它只跑已经存在的东西。

## 哪些组件需要本机构建

| 组件 | 镜像从哪来 |
| --- | --- |
| `component.yaml` 写了 `deployment.image`，来自 Git 源或市场 | 拉取：组件作者已经把镜像推到了镜像仓库，你不用构建 |
| `component.yaml` 只写了 `deployment.build`（没有 `image`） | 本机构建：从这个版本的 Git tag 导出源码构建 |
| 来自本地源（`components/`、`shell/` 里正在开发的代码） | 本机构建：从你的工作区构建——不管写没写 `image` |

最后一行值得注意：`add --repo` 把一个组件的源码克隆进 `components/` 之后，它就由本地源提供，镜像要从你的工作区构建，
而不再是拉取作者发布的那个——因为你克隆它，就是要跑你手里的代码。

## `brickkit build`

```bash
brickkit build
```

```text
🔨 构建 demo/hello@1.0.0 → demo-hello:1.0.0
✅ 已构建 demo/hello@1.0.0 → demo-hello:1.0.0
🔨 构建 demo/caller@1.0.0 → demo-caller:1.0.0
✅ 已构建 demo/caller@1.0.0 → demo-caller:1.0.0
```

不带参数，构建项目里所有需要本机构建的组件。只构建一个：

```bash
brickkit build demo/bus
```

```text
🔨 构建 demo/bus@1.0.0 → demo-bus:1.0.0
✅ 已构建 demo/bus@1.0.0 → demo-bus:1.0.0
```

`brickkit build demo/hello` 构建这个组件在项目里的每个版本；`brickkit build demo/hello@1.1.0` 只构建那一个。在组件目录里，
不带参数的 `brickkit build` 只构建这一个组件：

```bash
cd components/demo/lib
brickkit build
```

```text
📁 项目：../../..（shop）
🔨 构建 demo/lib@1.0.0 → demo-lib:1.0.0
⚠️ 警告：demo/lib@1.0.0 的源码里有 git submodule，这里它们是空目录
   目录：third_party/sdk
   建议：BrickKit 从不拉取 submodule；构建要是需要它们，这个组件应当发布现成的镜像（deployment.image），或者让构建不依赖它们
✅ 已构建 demo/lib@1.0.0 → demo-lib:1.0.0
```

中间那条警告说的是 **git submodule**：BrickKit 从不拉取它们，所以在拿来构建的源码里，它们是空目录。Dockerfile 不从里面拷东西时，
镜像没问题；需要它们时，构建会失败——更糟的是构建成功了、里面却少了东西。长久的办法是让组件发布镜像（`deployment.image`），
谁都不用构建它；在克隆下来的仓库里，你也可以自己 `git submodule update --init`。

**镜像已经存在就跳过：**

```text
⏭️  demo/hello@1.1.0：镜像 demo-hello:1.1.0 已存在，跳过（--force 重新构建）
⏭️  demo/hello@1.0.0：镜像 demo-hello:1.0.0 已存在，跳过（--force 重新构建）
```

镜像的 tag 就是组件版本，版本不变 tag 就不变。所以**改了代码却没改版本号时，要加 `--force`**，否则 `build` 认为镜像已经是最新的：

```bash
brickkit build demo/hello@1.1.0 --force
```

```text
🔨 构建 demo/hello@1.1.0 → demo-hello:1.1.0
✅ 已构建 demo/hello@1.1.0 → demo-hello:1.1.0
```

**源码从哪来。** 本地源里正好是这个版本时，用你的工作区；否则从这个版本的 Git tag 导出一份干净的源码来构建——
所以项目里同时有 1.0.0 和 1.1.0 两个版本时，两个镜像各自对应各自的 tag，不会混。

**tag 规则。** 写了 `image` 就用它的名字，没写就由组件 ID 推出（`demo/hello` → `demo-hello`）；tag 永远等于 `metadata.version`。
外壳的镜像还记下了编进去的成员版本；`up` 发现它和外壳声明的成员对不上（改了成员版本却没重建），会拦下来，
见 [外壳升级](../04-shell/07-shell-upgrade.md)。

## 预构建镜像：`image`

组件作者把镜像推到镜像仓库，在 `component.yaml` 里写上它：

```yaml
deployment:
  type: container
  image: registry.example.com/demo/hello:1.0.0
  port: 8080
```

使用者什么都不用构建：`up` 检查镜像在本机、或者能从仓库拉到，然后启动。拉不到时（没登录、没权限、网络不通），`up` 会报错，
并提示你也可以改成本机构建。

## 本机构建：`build`

组件没发布镜像，或者你就是要从源码构建时：

```yaml
deployment:
  type: container
  build:
    context: .
    dockerfile: Dockerfile
  port: 8080
```

`context` 是构建上下文，`dockerfile` 是 Dockerfile 的路径，都相对组件仓库根；不写时分别是 `.` 和 `Dockerfile`。`image` 与 `build` 都写时，本地源里的组件按 `build` 构建，
Git / 市场上的按 `image` 拉取。怎么写 Dockerfile 见 [组件开发者指南](../03-component-guide/README.md)。

## 第一次克隆项目

```bash
git clone https://git.example.com/projects/my-shop.git
cd my-shop
brickkit build
brickkit up
```

`build` 从每个需要本机构建的组件的 Git tag 导出源码构建；有预构建镜像的组件跳过。之后只有在组件版本变了（`upgrade` 之后）
或者你改了本地源里的代码时，才需要再 `build`。
