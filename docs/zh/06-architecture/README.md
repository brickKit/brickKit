# 架构与深度机制

**写给谁**：想弄懂 BrickKit 内部怎么运转的人——排查一个想不通的行为、评估一个设计提议、或者给 BrickKit 本身写代码。
只是用它，读 [项目管理者指南](../02-project-guide/README.md) 和 [组件开发者指南](../03-component-guide/README.md) 就够了。

**建议的读法**：先读 [01 架构总览](01-pipeline-overview.md)，知道一次 `up` 经过哪几站；再按需跳到对应的一站。
想知道"为什么是这样、为什么不那样"，读 [05 设计原则与取舍](05-design-principles.md)。

| 篇目 | 讲什么 |
| --- | --- |
| [01 架构总览](01-pipeline-overview.md) | 从三层声明到运行中的容器：流水线的每一站 |
| [02 依赖解析](02-dependency-resolution.md) | 拓扑排序、菱形依赖、循环依赖、多版本 |
| [03 环境变量注入契约](03-env-injection-contract.md) | 平台会注入的每一个变量、命名规则、求值时机 |
| [04 部署文件生成](04-deploy-file-generation.md) | 生成的 `compose.yaml` 与 Kubernetes 清单长什么样 |
| [05 设计原则与取舍](05-design-principles.md) | 贯穿一切的想法、十五个工程想法、十条原则 |
| [06 Git 仓库缓存](06-bare-repo-mechanism.md) | bare 仓库、读取优先级、离线能力 |
| [07 缓存设计](07-cache-design.md) | `.brickkit/` 里有什么、什么可以删 |
| [08 安全与签名](08-security-and-signing.md) | 签名与验签、公钥放哪、网络策略与它的限制 |
| [09 错误码参考](09-error-codes.md) | 每个错误码、它背后的具体情形、该不该重试 |
