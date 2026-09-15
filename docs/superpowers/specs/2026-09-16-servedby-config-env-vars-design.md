# 设计书：servedBy 成员自己的 config 值，改走独立的组件前缀环境变量，不再塞进 `BRICKKIT_SERVED_MEMBERS_CONFIG` 的 JSON 内部

## 0. 背景

`BRICKKIT_SERVED_MEMBERS_CONFIG`（2026-09-15 实现，commit d3d6905——这次
是 bounded 改动，未走 spec 文档，设计只留在对话记录里）是 servedBy 提案
一的落地：外壳容器上一个保留变量，JSON 数组，
每个元素对应一个被收编成员的 `componentId`/`version`/`httpPort`/`extraPorts`/
`config`（合并后的自身配置，原始 key → 值）。

上线次日，`be-assembly-standard`（真实使用 servedBy 的外部项目）反馈：当某个
servedBy 成员的 config 值在 `brickkit.yaml` 里写的是 `${VAR}` 占位符（密钥类
配置的标准写法，比如 PEM 私钥），且这个变量只存在于项目的 `.env` 文件里、没有
`export` 到跑 `brickkit up` 的那个 shell 时，这个占位符会原样流入
`BRICKKIT_SERVED_MEMBERS_CONFIG` 的 JSON（此时 `json.Marshal` 编码合法）。但
`docker compose up` 真正启动时，会对整份生成好的 `docker-compose.yaml` 做
**无 JSON 结构感知的全文本 `${VAR}` 替换**，把真实的多行 PEM 值糊进这个 JSON
字符串内部，破坏 JSON 结构，外壳容器 crash-loop。

反馈本身给出了两个方向（提前展开 / 转义防御），详见 §2——两个都被否决。

## 1. 根因（比反馈本身的描述更深一层）

三个已确认的事实，缺一不可才能解释这个 bug：

1. `internal/config/parse.go` 的 `ParseConfig` 在解析阶段就会展开 `${VAR}`
   （`expandEnvNode`），但**只查 `os.LookupEnv`**（跑 `brickkit up` 那个
   进程自己的环境），从不读 `.env` 文件。密钥只放 `.env`、没 `export` 到
   CLI 自己的 shell 是完全正常、常见的工作流（这正是 docker compose 自己
   `${VAR}` + `.env` 机制存在的意义）——这种情况下展开必然失败，占位符原样
   保留。
2. `internal/cli/up_k8s.go` 已经有一套"进程环境优先、`.env` 兜底"的解析机制
   （`envLookup`/`readDotEnv`），但**只接给了 K8s Secret 生成**——K8s 侧
   在生成阶段就把真实值解析好、直接写进 Secret，所以 K8s 部署形态完全不会
   撞上这个 bug。
3. `internal/compose/compose.go` 的 `Options.Lookup` 字段注释明确写着：
   "compose 文件本身刻意保留占位符：那份文件会被人打开看、进 git diff，
   明文密码进去就等于泄露，而 docker compose 会自己从 `.env` 展开。"——
   Docker Compose 路径**刻意不用**这套"进程环境 + `.env`"解析去展开生成
   文件本身的值，只用来生成 local-debug 文件。这是一条已经论证过的既有
   安全原则，不是遗漏。

`BRICKKIT_SERVED_MEMBERS_CONFIG` 是第一个把"可能仍是 `${VAR}` 占位符的值"
塞进一个必须保持结构完整的字符串（JSON）内部的机制——正是这个"结构嵌套"
本身，把一个此前良性的设计（占位符留到 docker compose 运行时才展开）变成
了破坏性的。

## 2. 被否决的方向

**方向一（反馈原话）：提前展开再编码进 JSON。** 直接违反 §1 第 3 点的既有
原则——真实密钥会被烘焙进 `.brickkit/generated/docker-compose.yaml`。就算
这个目录默认 `.gitignore`，这仍是推翻一条已经明确论证过的安全姿态，不是
"复用现成逻辑"这么轻的改动。

**方向二（反馈原话）：`json.Marshal` 前把 `$` 转义成 `$$`，纯防御。** 能让
JSON 不被撑坏，但代价是这个占位符**永远解不开**——docker compose 看到
`$${VAR}` 只会当字面量处理，外壳最终拿到的 `config` 值会是字面文本
`${VAR}`，不是真实密钥。把"crash-loop"换成了"静默拿到错误的值"，反而更难
排查，不是真正的修复。

**都不采纳的共同原因**：两个方向都在"把值塞进 JSON 内部"这个前提下打转，
真正该改的是这个前提本身。

## 3. 设计目标

1. 从根上消除"任何可能是 `${VAR}` 的文本被嵌进一个结构化字符串内部"这一整
   类问题，不是只修好这一个变量。
2. 完整保留 §1 第 3 点那条既有安全原则：Docker Compose 生成文件不烘焙真实
   密钥，占位符留给 docker compose 运行时展开。
3. 不引入"这个值看起来像不像密钥"之类的启发式判断——统一处理每个成员自己
   的每一个 config 值，不区分。
4. 复用平台已有的命名与碰撞检测机制，不新发明一套。

## 4. 架构方案

**给外壳里每个成员自己的每个 config 值，各自生成一条独立的、按组件 ID 加
前缀命名的环境变量，`${VAR}` 占位符语义完全不变，交给 docker compose 自己
展开——跟一个独立部署组件自己的 config 注入是同一件事，只是多了一层
"按组件 ID 命名空间隔离"。`BRICKKIT_SERVED_MEMBERS_CONFIG` 的 JSON 不再
携带值本身，只携带"这个 key 对应哪个变量名"。**

### 4.1 命名规则

不新造算法，拼接两个已有的导出函数：

```
{变量名} = manifest.EnvPrefix(member.Ref.ID) + "_" + inject.EnvVarName(key)
```

- `manifest.EnvPrefix`（`internal/manifest/envvar.go`）：组件 ID → 前缀，
  跟 `*_ENDPOINT` 用的同一个函数（`/`、`.` → `_`，全大写）。
- `inject.EnvVarName`（`internal/inject/reserved.go`）：驼峰 key → 大写
  下划线，标准组件自己的 config 注入已经在用。

示例：`mdm/customer` 的 `pgSchema` → `MDM_CUSTOMER_PG_SCHEMA`；
`infra/iam-casdoor` 的 `appTokenSigningKeyPem` →
`INFRA_IAM_CASDOOR_APP_TOKEN_SIGNING_KEY_PEM`。

这个规则跟 §9.23（依赖别名被拒绝的理由）同一个精神：变量名从数据双向可
计算，见到 `componentId` + 原始 key 就能推出变量名，见到变量名也能猜出
大概是哪个组件的。

### 4.2 为什么这不是"被否决过的摊平合并"

`docs/superpowers/specs/2026-09-13-shell-served-by-design.md` §6 否决的是
"把成员自己的 config **不带前缀地**摊平合并进外壳共享环境"——`pgSchema`
两个模块相撞是真实案例。这里每一条新变量都带**组件 ID 前缀**，结构性地
不可能撞名（除非同一个组件 ID 的两个不同版本恰好声明了同名 key 且值不同，
见 §4.3）。前缀化之后，"两个独立模块用同一个通用 key 名表示不同东西"这个
否决理由已经不成立。

### 4.3 碰撞处理：复用 `mergeGroup` 现成的检测逻辑

这些新变量最终也进 `Group.Env`（外壳共享的摊平环境表），需要跟 `*_ENDPOINT`
变量同一套"同名同值放过、同名不同值报错"规则——包括一个已有测试证明合法
的碰撞点：**同一个外壳收编了同一个组件的两个版本**
（`TestResolveGroupsTwoVersionsOfSameComponentUnderOneShell`）。这个命名
规则不含版本号（跟 `*_ENDPOINT` 一致），如果两个版本都声明了同名 config
key 但值不同，需要报错、点名双方——复用 `mergeGroup` 里 `endpointCollisionError`
同一套报错形状，不新写一套。

### 4.4 `BRICKKIT_SERVED_MEMBERS_CONFIG` 的新形状

`config` 字段改名为 `configEnvVars`，语义从"key → 值"变成"key → 变量名"：

```json
{
  "componentId": "infra/iam-casdoor",
  "version": "1.0.0",
  "httpPort": 8080,
  "extraPorts": [],
  "configEnvVars": {
    "appTokenSigningKeyPem": "INFRA_IAM_CASDOOR_APP_TOKEN_SIGNING_KEY_PEM"
  }
}
```

外壳启动器读这份 JSON 拿到"这个成员声明了哪些 config key、分别对应哪个
环境变量名"，再去外壳自己的进程环境读那条独立变量的值——不再从 JSON
字符串内部抠值。字段改名（而不是复用 `config` 但悄悄换语义）是故意的，
避免读代码的人误以为这还是"直接给你值"。

零个成员时依然是 `"[]"`（跟改动前一致，`ServedMembersConfig()` 这条既有
约定不变）。

### 4.5 为什么 JSON 里不会再出现 `${` 文本

`componentId`/`version` 受 Manifest 校验的字符集约束（`scope/name` 与
`major.minor.patch`，不可能含 `$`/`{`/`}`）；`extraPorts` 来自
`component.yaml`（组件作者写的，不是 `brickkit.yaml` 的用户可配置项）；
`configEnvVars` 的值是确定性算出来的标识符（`EnvPrefix`/`EnvVarName` 的
输出）。四类叶子值没有一类能承载用户在 `brickkit.yaml` 里写的原始文本，
这一类问题从根上不存在了，不是"这一个变量修好了"。

## 5. 代码改动点

- `internal/shell/shell.go`
  - `Member` 结构体：`Config map[string]string`（原始 key → 值）保留，
    额外算出的东西不是新字段，而是 `mergeGroup`/一个新的合并步骤直接消费
    它，产出两样东西：① 并入 `Group.Env` 的新增变量（走碰撞检测）；
    ② `configEnvVars` 索引（`servedMemberConfigEntry.ConfigEnvVars
    map[string]string`，替换掉原来的 `Config map[string]string`）。
  - `servedMemberConfigEntry` 结构体：`Config` 字段改名
    `ConfigEnvVars`，`json:"configEnvVars"`。
  - 需要一个新函数（或扩展 `mergeGroup`）：给每个成员的每个 config 项算出
    §4.1 的变量名，和 `*_ENDPOINT` 变量一起走同一套碰撞检测，一起进
    `Group.Env`。
  - `ServedMembersConfig()`：`Config` 改成读 `configEnvVars`（key → 算出
    的变量名，不是值）。
- `internal/inject/inject.go`：`Var.Key` 字段（上一版加的）继续用，不需要
  再改。
- `internal/manifest/envvar.go`、`internal/inject/reserved.go`：
  `EnvPrefix`/`EnvVarName` 已存在，直接复用，不改。
- 文档：`docs/{en,zh}/patterns/shell-implementers-guide.md`、
  `docs/{en,zh}/architecture/cli-reference.md`（如果涉及）、
  `AGENTS.md`/`AGENTS.zh.md` 里已经写的 `BRICKKIT_SERVED_MEMBERS_CONFIG`
  描述，需要同步改成新形状（这些是 2026-09-15 那次改动刚写的，直接改
  成新的，不用保留"旧版本"说明）。

## 6. 测试策略

- `internal/shell/shell_test.go`：
  - 新变量名计算是否正确（`EnvPrefix`+`EnvVarName` 拼接）。
  - 新变量是否真的进了 `Group.Env`（`Apply()` 之后能在外壳环境里查到）。
  - 同一个外壳收编同一组件两个版本、config 同名同值 → 通过；同名不同值
    → 报错，错误信息点名双方（复用现有碰撞测试的写法）。
  - `configEnvVars` 的值是变量名，不是原始配置值。
  - `${VAR}` 占位符（无论解析成功与否）都不出现在 JSON 里——用一个含
    `${SOME_SECRET}` 的 config 值跑一遍，断言最终 JSON 字符串里找不到
    `${` 子串。
- `internal/inject/*`：不需要新增（`Var.Key` 已有测试覆盖）。

## 7. 明确不做的事

- 不给 `configSchema` 新增"这个字段是密钥"的声明能力（`secret: true` 之
  类）——那是一个独立的、影响 Manifest schema 本身和市场校验器的更大功能，
  不在这次修复范围内。
- 不改变资源连接变量（`DATABASE_*` 等）在 servedBy 下的处理方式——这次
  只涉及 `component.config`，资源绑定的等价判定与 K8s Secret 生成不受
  影响。
- 不改动 `internal/cli/up_secrets.go` 现有的明文密钥警告
  （`isHardcodedSecret`/`warnConfigSecrets`）——那两个警告判断的是
  "brickkit.yaml 原文有没有写 `${ENV_VAR}`"，这个语义在本次改动前后没有
  变化。
