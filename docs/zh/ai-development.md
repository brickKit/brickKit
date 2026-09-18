# AI 辅助开发指南

BrickKit 的组件模型天然适合 AI 辅助开发。这篇讲清楚为什么，以及具体怎么用。

还不熟悉组件契约本身？[从零开发第一个组件](guide/10-build-your-own.md) 走一遍
AI 生成的组件同样要遵守的那几条基础规则（Manifest、环境变量、健康检查）——
建议先读一遍再把活交给 AI，这样才判断得出它写的东西到底对不对。

## 为什么 BrickKit 适合 AI 写代码

### 1. 组件粒度匹配 AI 的上下文窗口

实测项目自带的 10 个真实组件（`tests/components/`），代码行数在 200 到 3500 行之间。这种规模，加上 `component.yaml` 划出的明确契约边界，意味着 AI 可以一次性完整读取并理解一个组件，不需要在一个动辄几十万行的单体代码库里连蒙带猜。

### 2. 契约先行，AI 有明确的"边界"

每个组件的 `component.yaml` 声明了：

- 它依赖哪些组件（精确版本，强依赖/弱依赖）
- 它提供哪些端口（HTTP/gRPC）
- 它需要哪些配置（`configSchema`）
- 它需要哪些资源（数据库、Redis 等）

AI 不需要理解整个系统——只要读当前组件的 `component.yaml`，加上它依赖的组件对外发布的 API 契约（`artifacts` 里的 `api-contract`，通常是 `openapi.json`），就能写出一个完整、可独立运行的组件。

### 3. 环境变量注入，AI 不用处理配置复杂性

AI 生成的组件代码只需要这样读配置：

```python
import os

# 强依赖
dep_endpoint = os.environ.get("DEP_ENDPOINT")

# 弱依赖：必须用安全的读取方式
optional_endpoint = os.environ.get("OPTIONAL_ENDPOINT")
if not optional_endpoint:
    # 降级逻辑——弱依赖缺失时这个变量根本不会被注入，
    # 用 os.environ["OPTIONAL_ENDPOINT"] 会直接 KeyError 崩溃
    pass

# 自身配置
page_size = int(os.environ.get("PAGE_SIZE", "20"))
```

AI 完全不用理解服务发现、配置中心、注册 SDK 这些概念——这些平台已经用环境变量注入解决了。

### 4. 精确版本 + 多版本共存，AI 的"破坏性修改"变得安全

AI 生成的 v2 可以直接和 v1 并存：

- `my-component-1-0-0` 和 `my-component-2-0-0` 是两个独立的容器/服务名
- 依赖 v1 的其他组件不受影响
- 可以慢慢把调用方迁移过去，不需要一次性切换

## 怎么用 AI 辅助开发组件

### 步骤 1：让 AI 理解你的项目

`brickkit init` 会自动在项目里生成 `.claude/skills/` 目录，装好几个 AI 助手技能文件。把项目根目录的 `AGENTS.md` 也喂给 AI——它压缩了整个平台的定位、术语、设计原则，AI 读完就有了判断力，不用你每次都重新解释一遍"BrickKit 是什么"。

### 步骤 2：让 AI 生成组件骨架

Prompt 示例：

```
我要开发一个 BrickKit 组件：
- ID: my-scope/user-profile
- 版本: 1.0.0
- 语言: Python (FastAPI)
- 依赖: people/basic@1.0.0（强依赖）、infra/redis@1.0.0（弱依赖）
- 配置: page_size (integer, 默认 20)
- 端口: 8080 (HTTP)
- 健康检查: /healthz

请生成：
1. component.yaml
2. main.py
3. Dockerfile
4. migrations/001_init.sql
```

### 步骤 3：让 AI 生成 API 契约

```
根据刚才生成的 user-profile 组件的 main.py，生成 OpenAPI 3.0 规范
（openapi.json），包含所有端点的请求/响应 schema。
```

有了这份契约，依赖这个组件的其他组件（不管是 AI 写的还是人写的）就不需要读 `user-profile` 的源码。

### 步骤 4：让 AI 生成测试

```
根据 user-profile 组件的 main.py 和 openapi.json，生成：
1. 单元测试
2. 集成测试（至少覆盖 /healthz）
3. 契约测试（验证实现和 openapi.json 对得上）
```

### 步骤 5：让 AI 帮你调试

```
我的 user-profile 组件启动后报错：
KeyError: 'PEOPLE_BASIC_ENDPOINT'

请帮我分析可能的原因。
```

（这条错误的真正原因大概率是弱依赖没起来、或者组件代码用了 `os.environ[]` 而不是 `os.environ.get()`——见 [故障排除](troubleshooting.md)。）

## 最佳实践

### 一次只让 AI 写一个组件

不要让 AI 同时写多个互相依赖的组件——每写完一个、跑通了，再写下一个。这样每一步都有真实反馈，而不是堆出一堆互相印证不了的代码。

### 让 AI 读 API 契约，而不是读依赖方的源码

当你让 AI 写一个依赖 `people/basic` 的组件时，让它读 `people/basic` 的 `openapi.json`，不要让它去翻 `people/basic` 的实现代码。这样 AI 生成的代码只依赖契约本身，符合组件自治原则——`people/basic` 内部怎么重构都不会波及它。

### 让 AI 写完就立刻写测试

测试是验证 AI 生成的代码是否真的对的最直接方式，等到后面一起补往往就不了了之。

## 局限性

### AI 替代不了的

- **领域模型设计：** 一个功能到底该拆成几个组件、边界画在哪，这需要你（和领域专家）判断，AI 没有这个业务上下文
- **数据一致性决策：** 哪些操作需要事务、哪些可以最终一致，这是业务判断，不是代码风格问题
- **性能调优：** AI 生成的代码通常"正确但不是最优"，真实负载下的调优需要你自己来

### AI 容易踩的坑

- **过度设计：** 给一个简单功能堆出很多层抽象，需要你主动要求简化
- **错误处理只覆盖 happy path：** 弱依赖缺失、迁移失败这类边界情况容易被漏掉
- **测试覆盖不全：** AI 生成的测试经常只测正常场景，边界场景需要你自己补

## 深入阅读

- [组件设计准则](patterns/component-design.md) — 动手写组件之前先做的领域研究
- [分层测试](patterns/testing.md) — AI 生成测试时该覆盖哪几层
- [AGENTS.md](../../AGENTS.md) — 喂给 AI 的全平台压缩件
