# 创建项目

一个 BrickKit 项目就是一个普通目录，里面放着三层文件，外加几个约定好的子目录。`brickkit init` 把它们一次建好。

## `brickkit init <名字>`：新建

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
```

项目名只能用小写字母、数字和中划线，以字母或数字开头结尾——它会成为 Docker 网络（`brickkit-my-shop`）和 Kubernetes
命名空间的名字。

生成的 `brickkit.yaml` 已经声明了两个**本地安装源**，另外两种安装源写成了注释，取消注释、填上地址就能用：

```yaml
# brickkit.yaml —— 这个项目由什么组成：安装源与各组件的精确版本。
# 与代码一样共享、评审：请提交它。
project: my-shop

# 安装源，按声明顺序依次尝试
sources:
  - name: local-dev
    type: local
    path: ./components      # brickkit init 已经建好了这个目录
  - name: local-shells
    type: local
    path: ./shell           # 外壳（kind: shell）是项目自己的代码：brickkit new --shell 写到这里
  # 更多安装源：取消注释并填上地址
  # - name: company-git
  #   type: git
  #   baseUrl: https://git.example.com/components/
  # - name: market
  #   type: market
  #   url: https://market.example.com/api/v1

components: []
```

"安装源"就是"去哪里找组件"：本地源是本机的一个目录，Git 源是一组 Git 仓库（组件 `demo/hello` 的仓库是
`<baseUrl>demo-hello`，版本就是 tag），市场是一个组件市场服务。按声明顺序依次尝试，**第一个有这个组件的源说了算**。

`deploy.yaml` 只有部署目标，`config/vars.yaml` 是空的公共变量文件——加了组件之后它们才会长出内容。

### 生成的 `.gitignore`

```text
# BrickKit CLI 的工作目录：缓存、生成物、登录凭据
.brickkit/

# 个人部署文件及其备份（brickkit local on）
deploy.local.yaml
deploy.local.yaml.bak

# config 里用 file:// 引用的密钥文件，以及 .env 文件
.secrets/
.env

# 已移除组件的配置文件（留着以后重新 add 用）
config/.archive/

# 为开发克隆下来的组件源码（每个组件是独立的 Git 仓库）
components/
```

每一条都有理由：`.brickkit/` 是随时能重建的缓存；`deploy.local.yaml` 是你个人的；`.secrets/` 与 `.env` 放真正的密钥；
`config/.archive/` 是本机的后悔药；`components/` 下的每个组件都是**自己的** Git 仓库（一个组件一个仓库），不该被项目仓库再收一遍。

### 不要 AI 助手技能：`--no-skills`

`init` 默认装入给 AI 助手读的技能文件（`.claude/skills/`、`AGENTS.md`），它们跟着项目提交，让团队里每个人的 AI 助手都懂这个项目。
不需要就加 `--no-skills`；以后想要，执行 `brickkit skills update`。`init` 从不碰你自己的 `CLAUDE.md`。

## 补全式：在已有目录里执行 `brickkit init`

不带名字执行时，`init` 在**当前目录**缺什么补什么。两个典型场景：

- 一个已有的项目仓库要接入 BrickKit；
- 一个组件仓库想给自己建一个本地联调工作台（见 [组件开发者指南](../03-component-guide/05-local-dev-fractal.md)）。

规矩是"缺的创建，有的不动"。目录不是空的，就先把计划打印出来，等你确认：

```bash
cd legacy-shop
brickkit init
```

```text
当前目录已有文件，brickkit init 将：
   ✅ 创建  brickkit.yaml
   ✅ 创建  deploy.yaml
   ✅ 创建  config/vars.yaml
   ✅ 创建  config/.gitkeep
   ✅ 创建  shell/.gitkeep
   ✅ 创建  components/
   ✅ 创建  BRICKKIT.md
   ⚠️  .gitignore 缺少：.brickkit/、deploy.local.yaml、deploy.local.yaml.bak、.secrets/、.env、config/.archive/、components/（不替你改——请自己补上）
继续？[y/N] 
```

几点值得注意：

- **已有的 `.gitignore` 只校验、不修改。** 缺的每一条都大声警告：缺了它们，个人部署文件和密钥会被提交。它不替你改，是因为那份文件是你的，
  里面可能有它看不懂的规则。
- **`--yes` 跳过确认**（计划照样打印出来），给 CI 用。
- **项目名**取 `--name`，否则沿用已有 `brickkit.yaml` 的 `project`，否则取目录名。
- **最后按 `up` 的方式装载一遍项目**；装载不了，命令就失败——补全出来的必须是能用的项目。

```bash
brickkit init --yes
```

```text
当前目录已有文件，brickkit init 将：
   ✅ 创建  brickkit.yaml
   ✅ 创建  deploy.yaml
   ✅ 创建  config/vars.yaml
   ✅ 创建  config/.gitkeep
   ✅ 创建  shell/.gitkeep
   ✅ 创建  components/
   ✅ 创建  BRICKKIT.md
   ⚠️  .gitignore 缺少：.brickkit/、deploy.local.yaml、deploy.local.yaml.bak、.secrets/、.env、config/.archive/、components/（不替你改——请自己补上）
✅ 项目已补全：legacy-shop
⚠️ 警告：.gitignore 缺少必需条目——个人部署文件和密钥可能被提交
   文件：.gitignore
   缺少：.brickkit/
   缺少：deploy.local.yaml
   缺少：deploy.local.yaml.bak
   缺少：.secrets/
   缺少：.env
   缺少：config/.archive/
   缺少：components/
   建议：把缺的每一行手动加进 .gitignore（brickkit init 从不修改已有的 .gitignore）
   📁 .claude/skills/      AI 助手技能（4 个）
   📄 AGENTS.md            AI 助手项目导读
   🪝 .git/hooks/pre-commit 提交前检查组件结构
   ✅ 收尾校验通过：项目可以装载
```

项目根就是 Git 仓库根时，`init` 顺带装上提交前检查（见 [源码管理](11-sync-and-restore.md#提交前检查)）。

## 克隆一个已有的项目

队友已经建好了项目、推到了 Git 上。你**不需要** `init`：

```bash
git clone https://git.example.com/projects/my-shop.git
cd my-shop
brickkit build
brickkit up
```

`.brickkit/` 不在仓库里，但它只是缓存：第一次运行时，CLI 按 `brickkit.yaml` 从安装源重新取回每个组件的
`component.yaml` 和契约文件。`components/` 同样不在仓库里——`up` 只需要组件的 `component.yaml`，不需要源码；
要改哪个组件的代码，再用 `brickkit add <id>@<版本> --repo` 把它克隆下来（见 [添加组件](02-add-and-component-install.md#克隆源码--repo--repo-all)）。
`build` 那一步只在组件需要本机构建镜像时才有事可做，见 [构建与镜像](12-build-and-images.md)。

## 项目结构约定

```text
my-shop/
├── brickkit.yaml         用哪些组件、哪个版本（提交）
├── deploy.yaml           怎么部署（提交）
├── deploy.local.yaml     你个人的部署文件（不提交，brickkit local on 生成）
├── config/               组件的业务配置与公共变量（提交）
│   └── .archive/         已移除组件的配置（不提交）
├── components/           本地安装源：你正在开发、或为了改而克隆下来的组件源码（不提交）
├── shell/                本地安装源：外壳，项目自己的代码（提交）
├── BRICKKIT.md           项目地图
└── .brickkit/            CLI 的缓存与生成物（不提交）
```

- **`components/`** 放组件源码。每个组件是一个独立的 Git 仓库，按 `<scope>/<name>/` 摆放——本地源就按这个布局扫描。
  想让组件源码跟项目一起进版本库，把 `components/` 从 `.gitignore` 去掉，并用 `brickkit init --hooks` 装上提交前检查。
- **`shell/`** 放外壳：把几个组件编进一个进程的组件（见 [外壳机制](../04-shell/README.md)）。外壳是这个项目自己的代码，随项目提交。
- **`config/`** 放每个组件的配置，一个组件一个文件（见 [config/ 目录](../01-three-layers/05-config-directory.md)）。
