# BrickKit Documentation / BrickKit 文档

This is a way-finding index by intent, not by document type. The documentation
itself lives in two fully symmetric, independently-written trees —
[`en/`](en/) and [`zh/`](zh/) — not a base language plus translations. Pick
your language below, then every link points at the matching file in that tree.

这是一个按"我想做什么"组织的导航页，不是按文档类型分类。文档本体分别在两棵
**完全对称、各自独立撰写**的目录树里——[`en/`](en/) 和 [`zh/`](zh/)——不是
"一种语言是原文、另一种是译本"的关系。先选你要读的语言，之后每条链接都指向
那棵树里对应的文件。

---

## English

### 🚀 I want to get started
- **Get something running in 5 minutes** → [Quick Start](en/00-quick-start.md)
  *(the repo's own `demo/hello` fixture, empty directory to a curl-able container)*

### 🧠 I want to understand the core mechanics
- **Core concepts and terminology** → [Core Concepts](en/01-concepts.md)
  *(component IDs, exact versions, required/optional dependencies — the basic contract)*
- **How it works internally** → [Architecture overview](en/06-architecture/00-overview.md)
  *(why "no registry" and "platform minimalism" hold together as one pipeline)*
- **Why it's shaped this way, and what each principle refused** → [Design principles and trade-offs](en/06-architecture/01-design-principles.md)
  *(the one idea underneath; a numbered guide to the engineering ideas BrickKit uses or deliberately leaves alone — DDD, GitOps, TDD, hexagonal architecture — each with the AI-development pain it maps to, what BrickKit does, what it deliberately doesn't do, and how an AI copes; and the argument behind each of the twelve principles)*
- **How BrickKit compares to Compose/Helm/Kustomize/etc.** → [Comparison](en/02-comparison.md)

### 🛠️ I want to develop a new component
- **Build your first component from scratch** → [Guide: Build your first component](en/03-guide/10-build-your-own.md)
  *(a minimal, complete walkthrough: Manifest, `os.environ.get()`, `/healthz`)*
- **A deep Go component template with a database** → [Go component template](en/04-go-component-template.md)
  *(a real, tested component — migrations, a multi-stage Dockerfile, the works)*
- **Every pattern, organized by role** → [Patterns index](en/07-patterns/README.md)
- **Write a client against the marketplace's HTTP API** → [Market API reference](en/09-market-api.md)

### 🤖 I want to use AI to develop components
- **AI-assisted development, with prompt examples** → [AI-assisted development](en/05-ai-development.md)
  *(scaffold → contract → tests → debug, plus what AI still can't do for you)*

### 🛡️ I want production-grade, advanced patterns
- **Upgrades and running multiple versions side by side** → [Guide: Upgrades and versions](en/03-guide/05-upgrades-and-versions.md)
  *(a real v1/v2 coexistence, and the `remove` error when an ID is ambiguous)*
- **Common failures and weak-dependency degradation** → [Troubleshooting](en/08-troubleshooting.md)
  *(symptom → real cause → fix, plus the missing-optional-dependency warning from [Guide 2](en/03-guide/02-what-runs.md))*

### 📖 The full walkthrough
- **12-part tutorial series, in order** → [Tutorial series](en/03-guide/README.md)

## 中文

### 🚀 我想快速上手
- **5 分钟内跑起来点什么** → [Quick Start](zh/00-quick-start.md)
  *(仓库自带的 `demo/hello` 夹具，从空目录到一个可以 curl 通的容器)*

### 🧠 我想理解核心机制
- **核心概念与术语表** → [核心概念](zh/01-concepts.md)
  *(组件 ID、精确版本、强/弱依赖——最基础的契约)*
- **架构总览与设计原理** → [架构总览](zh/06-architecture/00-overview.md)
  *("无注册中心"和"平台极简"怎么在同一条流水线里落地)*
- **为什么这样设计，每条原则拒绝了什么** → [设计原则与取舍](zh/06-architecture/01-design-principles.md)
  *(贯穿一切的那个想法；BrickKit 用到或刻意没用的工程想法——DDD、GitOps、TDD、六边形架构——逐个编号介绍，每个都讲清对应 AI 开发的什么痛点、BrickKit 怎么做、BrickKit 不做什么、AI 怎么应对；以及十二条原则各自的论证)*
- **对比 BrickKit 和 Compose/Helm/Kustomize 等** → [对比](zh/02-comparison.md)

### 🛠️ 我想开发新组件
- **从零开发第一个组件** → [指南：从零开发第一个组件](zh/03-guide/10-build-your-own.md)
  *(一个极简但完整的例子：Manifest、`os.environ.get()`、`/healthz`)*
- **带数据库的 Go 组件深度模板** → [Go 组件模板](zh/04-go-component-template.md)
  *(一个真实的、有测试覆盖的组件——数据库迁移、多阶段 Dockerfile，一应俱全)*
- **按角色查看全部 Patterns** → [Patterns 索引](zh/07-patterns/README.md)
- **对着市场的 HTTP API 写客户端** → [Market API 参考](zh/09-market-api.md)

### 🤖 我想用 AI 辅助开发
- **AI 辅助开发指南，含 Prompt 示例** → [AI 辅助开发指南](zh/05-ai-development.md)
  *(骨架 → 契约 → 测试 → 调试，以及 AI 目前还替代不了的部分)*

### 🛡️ 我想了解生产级的进阶用法
- **升级与多版本共存** → [指南：升级与多版本共存](zh/03-guide/05-upgrades-and-versions.md)
  *(真实的 v1/v2 并存，以及 ID 跨版本有歧义时 `remove` 的真实报错)*
- **常见故障与弱依赖降级** → [故障排除](zh/08-troubleshooting.md)
  *(症状 → 真实原因 → 解决，弱依赖缺失的真实警告见[教程第 2 篇](zh/03-guide/02-what-runs.md))*

### 📖 完整教程
- **12 篇系列教程，按顺序读** → [教程系列](zh/03-guide/README.md)

---

Both trees follow the same layout —
`{en,zh}/<architecture|guide|patterns>/<same relative path>` — so once you
know a document's location in one language, you know it in the other too.
两棵树的目录结构完全一致——`{en,zh}/<architecture|guide|patterns>/相同的相对
路径`——知道一篇文档在一种语言下的位置，就知道它在另一种语言下的位置。
