# 故障排除

出了问题，按场景找到对应的一篇，再按症状找到那一节。每一节都是同一个格式：

- **症状**：你看到了什么——一段报错、一个没生效的配置、一个连不上的地址。
- **原因**：为什么会这样。
- **解决**：怎么改。

| 场景 | 文档 |
| --- | --- |
| `brickkit up` / `down` 失败，组件起不来 | [up / down 常见问题](01-up-down-issues.md) |
| 在 IDE 里调试组件、本机进程 | [本地调试问题](02-local-debug-issues.md) |
| `config/` 里的冲突块、`$var:` 找不到、配置没生效 | [配置冲突问题](03-config-conflict-issues.md) |
| 从 Git 仓库取组件失败 | [Git 鉴权问题](04-git-auth-issues.md) |
| `brickkit build`、镜像 | [构建问题](05-build-issues.md) |
| 外壳与成员 | [外壳问题](06-shell-issues.md) |
| 数据库迁移 | [迁移问题](07-migration-issues.md) |
| `brickkit lint`、编辑器里的 JSON Schema、`AGENTS.md` 的组件表 | [lint 与 Schema 问题](08-lint-and-schema-issues.md) |

**报错里有错误码时**，先查 [错误码参考](../06-architecture/09-error-codes.md)：它按错误码列出了 CLI 会打出的每一个错误标题，
直接拿标题在那一页里搜就能找到原因与解决办法。这个模块补的是错误码覆盖不到的那部分——没有报错、但结果不对的情况，以及报错背后更长的来龙去脉。
