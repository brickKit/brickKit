# AI 辅助开发工作流

任务一开始是一个需求而不是一个组件时——"用户应该能……"——先弄清它归哪个组件、合不合理、按什么顺序改，见
[判断需求、制定计划](03-judging-a-requirement.md)。下面的步骤从你已经知道要写或要改哪个组件开始。

## 写一个新组件

1. **读上下文。** 项目根的 `AGENTS.md`（团队约定，末尾是组件表）与 `brickkit.yaml`：项目里已经有什么、新组件要守哪些规矩、放在哪。
2. **读依赖。** 新组件要调用的每个组件：它的 `BRICKKIT.md`、`configSchema`、契约。只读直接依赖。
3. **生成骨架。** `brickkit new <scope>/<name>`（独立仓库用 `--path`，要契约占位用 `--contract openapi|proto`）。
4. **先写 `component.yaml`。** `dependencies`（精确版本）、`configSchema`（键就是环境变量名，密钥标 `secret: true`，平台推导不出的写成必填）、
   `deployment`、`healthCheck`、需要的话 `migration`。然后 `brickkit lint`。
5. **写契约。** 先把接口写进契约文件——调用方可以同时开工，你也有了验收标准。
6. **写代码。** 依赖地址从 `*_ENDPOINT` 读；弱依赖的变量可能不存在，用 `.get()` 读并写明降级；健康检查只查本进程；入口对不认识的参数立刻报错退出。
7. **写 Dockerfile。** 镜像里要有 `wget` 或 `curl`（HTTP 健康检查用），不以 root 运行。
8. **写组件的文档。** 给使用者的 `BRICKKIT.md`——六节，写 `component.yaml` 说不出来的东西；给下一个开发者的 `AGENTS.md`——代码地图、
   怎么构建和测试、设计取舍；再加上 `README.md` 的第一行。`brickkit lint` 会列出还剩的每一处占位。见
   [组件的文档](../03-component-guide/08-component-doc-spec.md) 与 [组件文档（AI 视角）](04-component-doc-spec.md)。
9. **跑起来。** `brickkit add --local`、`brickkit build`、`brickkit up`；或者在组件仓库里建工作台联调（见 [分形的本地开发](../03-component-guide/05-local-dev-fractal.md)）。
10. **测试。** 先写测试、看它变红，再让实现变绿（见 [测试策略](../09-patterns/02-testing-strategy.md)）。

## 改一个已有组件

1. **读它的 `BRICKKIT.md`、`AGENTS.md` 与 `component.yaml`。** 尤其 `configSchema`、`dependencies`，以及 `AGENTS.md` 里的代码地图和易错点。
2. **判断是不是破坏性改动。** 删接口、改字段含义、改配置项的名字或含义、改默认值——都会影响使用方。
3. **改代码、改契约、改文档。** 放在同一个提交里：使用方看得到的东西变了改 `BRICKKIT.md`，代码地图或某个取舍变了改 `AGENTS.md`。
   别让文档落后于实现——文档和 `component.yaml` 对不上了（`DOC_OUT_OF_STEP`）、指向的代码挪走了（`DOC_PATH_MISSING`），`brickkit lint` 都会说。
4. **提版本号。** 改 `metadata.version`（它是版本号唯一的来源）：只改实现提修订号，加东西提次版本号，破坏性改动提主版本号。
5. **发布。** 提交、推送、`brickkit release`。
6. **在项目里升级。** `brickkit upgrade <组件>`；有配置冲突时由人决定保留哪个值。

**改默认值要慎重**：使用方没写过的键会自动跟随新默认值——这正是你想要的；写过的键会变成一处冲突，要他逐条决定。

## 在大项目里干活

项目有几十个组件、而任务只涉及其中一个时，别把它们全启动。

1. **用焦点运行。** 在组件目录里 `brickkit up`（或在项目任何位置 `brickkit up --focus <id>`），就只从源码运行这个组件和它需要的；
   每个组件旁边打印的原因说明了它为什么启动或不启动。`brickkit up --all` 回到整个项目。见[在项目里就地开发](../02-project-guide/04-focus-run.md)。
2. **在你所在的位置运行命令。** 每个项目命令都会往上找项目；不带参数的 `build`、`deps` 指的就是"这个组件"。
3. **显式地让版本往前走。** 你在 `components/` 下某个组件里调高了 `metadata.version`，`up` 会停下来，直到项目跟上：运行它建议的
   `brickkit upgrade <id>@<版本>`，并读一遍它报告的配置迁移。不会因为某个目录变了就跟着变。
4. **只保留一个 `components/`。** 永远不要把组件克隆或复制进另一个组件的目录；`up`、`lint`、`sync` 会拒绝嵌套的副本，BrickKit
   也从不替你挪——删之前先问人：那里可能是他改动的唯一一份。

## 不需要做的事

- **不需要读整个代码库。** 组件边界写在文件里，读当前组件和它的直接依赖就够。
- **不需要猜服务地址。** 它是 `http://<版本化服务名>:<端口>`，由平台注入。
- **不需要猜变量名。** 依赖地址按组件 ID 推；配置项的名字就是 `configSchema` 的键。
- **不需要写部署脚本。** 部署文件由 CLI 生成；你只改三层文件。
- **不需要记命令参数。** 用 `brickkit <命令> --help`。

## 该交给人的事

- 组件边界怎么划（这是领域判断）。
- 升级时配置冲突保留哪个值。
- 引入新的第三方组件、往 `installer.publicKeys` 加信任的公钥。
- 是否开启网络策略、ServiceAccount 隔离、`podSecurity: restricted`。
- 生产部署文件的改动。
