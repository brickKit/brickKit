# 快速开始（5 分钟）

从一个空目录开始，建一个项目，放进一个组件，构建它的镜像，启动，用 `curl` 访问它，改一个配置
看它生效，最后停掉。下面每一条命令、每一段输出都是真跑出来的。

用的组件是仓库自带的测试夹具 `demo/hello`：一个最小的 HTTP 服务，回一句问候语。它不是业务组件，
正因为小，才适合第一次看清平台做了什么。

## 前置条件

- 装好 `brickkit`（见 [README 的安装一节](../../../README.zh.md#安装)），`brickkit version` 能打印版本；
- Docker 20.10+（含 Compose V2）在跑；
- 本机的 8080 端口空着（被占了的话，第五步里把 `expose` 换成别的端口，见那一步的说明）；
- 想看中文输出：`brickkit lang set zh`。

另外把 BrickKit 仓库克隆下来——夹具组件在它的 `tests/components/` 里：

```bash
git clone https://github.com/brickKit/brickKit.git
```

## 第一步：创建项目

```bash
brickkit init my-shop
```

```text
✅ 项目已初始化：my-shop
   📄 brickkit.yaml        项目配置
   📄 deploy.yaml          怎么部署（团队文件，要提交）
   📁 config/              组件配置与公共变量
   📁 components/          组件源码（已配为本地安装源 local-dev）
   📁 shell/               外壳（kind: shell），项目自己的代码（本地安装源 local-shells）
   📁 .brickkit/           CLI 工作目录
   📄 BRICKKIT.md          项目地图：有哪些组件、文档在哪
   📁 .claude/skills/      AI 助手技能（4 个）
   📄 AGENTS.md            AI 助手项目导读
   💡 组件源码要跟项目一起进 Git 的话：brickkit init --hooks 装上提交前检查

下一步：
  cd my-shop
  brickkit add --local                     把 components/ 下的组件全加进来
  brickkit add <scope>/<name>@<version>    从安装源添加组件（先在 brickkit.yaml 的 sources: 里启用一个）
  brickkit up                              一键启动
```

`init <名字>` 新建一个同名目录，三层文件的骨架都在里面：

```text
my-shop/
├── brickkit.yaml     有哪些组件（现在是空的）
├── deploy.yaml       怎么部署：target: docker
├── config/
│   └── vars.yaml     公共变量（现在是空的）
├── components/       本地安装源：你自己的组件源码放这里
├── shell/            本地安装源：外壳组件
├── BRICKKIT.md       项目地图
├── .brickkit/        CLI 的缓存与生成物（不进 Git）
└── .gitignore
```

`brickkit.yaml` 里已经声明好了两个**本地安装源**：`./components` 与 `./shell`。安装源就是"去哪里找组件"，
本地源是本机的一个目录，其它的还有 Git 仓库和组件市场。

## 第二步：添加组件

把夹具复制进项目的本地源（本地源里的组件按 `<scope>/<name>/component.yaml` 摆放），然后添加它：

```bash
cd my-shop
mkdir -p components/demo
cp -r ../brickKit/tests/components/demo-hello components/demo/hello
brickkit add demo/hello
```

```text
🔎 demo/hello 没写版本，最新的是 1.0.0（安装源 local-dev）
➕ 加入 demo/hello@1.0.0
   ✅ demo/hello@1.0.0
📝 已写：brickkit.yaml, deploy.yaml
📝 配置骨架：config/demo-hello.yaml
📦 产物：1 个文件，在 .brickkit/artifacts/
```

一次 `add` 改了三层文件：

- `brickkit.yaml` 多了一行精确版本——这就是锁：

  ```yaml
  components:
    - id: demo/hello
      version: 1.0.0
  ```

- `deploy.yaml` 多了这个组件的部署条目（现在什么都没写，就是"按默认方式跑"）：

  ```yaml
  components:
    - id: demo/hello
  ```

- `config/demo-hello.yaml` 是按组件的 `configSchema` 生成的配置骨架。可选、有默认值的项写成注释：

  ```yaml
  # === 可选：注释掉的键使用组件默认值，取消注释即可覆盖 ===
  # GREETING: Hello  # string | The greeting the component answers with (默认值)
  ```

组件的 API 契约（这里是一份 `openapi.json`）也被下载到了 `.brickkit/artifacts/`，写调用方时用得上。

## 第三步：构建镜像

先直接启动试试：

```bash
brickkit up
```

```text
🚀 启动项目 my-shop（target: docker）
📋 组件状态计算：
   ✅ demo/hello@1.0.0  启动（顶层）

📋 启动顺序（拓扑排序）：
   1. demo-hello-1-0-0  无依赖

可独立启动：demo-hello-1-0-0（无依赖）
📄 已生成：.brickkit/generated/compose.yaml
❌ 错误：这些镜像要在本机构建，还没有构建
   demo/hello@1.0.0：brickkit-demo/hello:1.0.0
   建议：
   1. up 从不自动构建（构建与部署分离）：先运行 brickkit build，或者只构建其中一个：
   2. brickkit build demo/hello@1.0.0
```

它失败了，而且说得很清楚为什么：本地源里的组件是**正在开发的代码**，镜像必须从它构建；而
`brickkit up` **从不自动构建**。构建是你的显式动作，部署是平台的机械操作——这样 `up` 永远不会
悄悄把一份你没打算发布的代码打进镜像。

```bash
brickkit build
```

```text
🔨 构建 demo/hello@1.0.0 → brickkit-demo/hello:1.0.0
✅ 已构建 demo/hello@1.0.0 → brickkit-demo/hello:1.0.0
```

什么时候需要 `build`：组件来自本地源，或者组件没有写 `deployment.image`（只写了怎么构建）。从 Git 仓库或
市场添加、写了 `image` 的组件，镜像是拉取的，不用构建。镜像的 tag 永远和组件版本一致。

## 第四步：启动

默认情况下组件**不对宿主机开放端口**——没声明的就不可达。要从本机 `curl` 它，在 `deploy.yaml` 里给它打开：

```yaml
components:
  - id: demo/hello
    expose: true        # 映射到宿主机的 8080
```

8080 被占用的话，再加一行 `exposePort: 18080`，下面的 `curl` 改用 18080。

```bash
brickkit up
```

```text
🚀 启动项目 my-shop（target: docker）
📋 组件状态计算：
   ✅ demo/hello@1.0.0  启动（顶层）

📋 启动顺序（拓扑排序）：
   1. demo-hello-1-0-0  无依赖

可独立启动：demo-hello-1-0-0（无依赖）
📄 已生成：.brickkit/generated/compose.yaml

🐳 正在启动（docker）...
   demo-hello-1-0-0             running（healthy）
✅ 全部组件已启动（1 个）

💡 查看状态：brickkit status
   查看日志：docker compose -p brickkit-my-shop logs -f
```

`demo-hello-1-0-0` 是这个组件的**版本化服务名**：组件 ID 里的 `/` 和 `.` 换成 `-`，再接上精确版本号。
别的组件要调用它，拿到的地址就是 `http://demo-hello-1-0-0:8080`——本地 Docker 和 Kubernetes 上一模一样。

## 第五步：验证

```bash
curl http://localhost:8080/api/v1/hello
```

```json
{"component":"demo/hello","greeting":"Hello","message":"Hello, I'm demo/hello@1.0.0","version":"1.0.0"}
```

```bash
brickkit status
```

```text
📊 项目状态：my-shop（target: docker）

✅ 运行中（1 个组件）
 ┌────────────┬───────┬───────────────────┬─────────────────────────────────────────────┐
 │ 组件       │ 版本  │ 状态              │ 端口                                        │
 ├────────────┼───────┼───────────────────┼─────────────────────────────────────────────┤
 │ demo/hello │ 1.0.0 │ 运行中（healthy） │ 0.0.0.0:8080->8080/tcp, [::]:8080->8080/tcp │
 └────────────┴───────┴───────────────────┴─────────────────────────────────────────────┘
```

## 第六步：修改配置看效果

在 `config/demo-hello.yaml` 里取消那行注释、换一个值：

```yaml
GREETING: 你好
```

配置文件里的键就是组件拿到的环境变量名，原样注入。重新 `up`：

```bash
brickkit up
```

```text
🐳 正在启动（docker）...
   demo-hello-1-0-0             running（healthy）
✅ 全部组件已启动（1 个）
```

（前面几段和上一次一样，这里略去。）

```bash
curl http://localhost:8080/api/v1/hello
```

```json
{"component":"demo/hello","greeting":"你好","message":"你好, I'm demo/hello@1.0.0","version":"1.0.0"}
```

没有配置中心、没有热更新：改文件，重新 `up`，容器按新的环境变量重建。

## 第七步：停止

```bash
brickkit down
```

```text
🛑 停止项目 my-shop
✅ 已停止全部组件

💡 数据卷未删除，数据库数据仍然保留
   需要彻底清理时手动执行：docker volume rm <卷名>
   重新启动：brickkit up
```

`down` 从不删数据卷：误删数据的代价太大，真要清理由你手动做。

## 下一步去哪

- 这几个文件分别管什么、还能写什么：[三层架构详解](../01-three-layers/README.md)
- 加更多组件、本地调试、多环境：[项目管理者指南](../02-project-guide/README.md)
- 写你自己的组件：[组件开发者指南](../03-component-guide/README.md)
- 每条命令的全部参数：[CLI 命令参考](../07-cli-reference/README.md)
