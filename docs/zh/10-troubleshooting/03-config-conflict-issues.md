# 配置冲突问题

## 重复的键：`up` 拒绝启动

**症状**

```text
❌ 错误：检测到未解决的配置冲突
   文件：config/demo-hello.yaml
   配置项：GREETING
   第 8 行：你好（你之前的值，1.0.0）
   第 9 行：Hi（1.1.0 的建议值）
   建议：
   1. 用纯文本编辑器打开文件，保留你要的那一行，删掉另一行和上方的注释，然后重新执行命令
   2. 不要对这个文件用 yq 或编辑器的"格式化文档"：它们会悄悄丢掉其中一个重复键，冲突没解决就消失了
   💡 编辑器可能把这个文件标成非法 YAML——这是正常的：BrickKit 故意写了重复键，让冲突不可能被忽略
```

编辑器也可能把这个文件标红，说有重复的键。

**原因**

`upgrade` 时，你改过的一个配置项、组件作者在新版本里也改了它的默认值。谁对只有你知道，所以两个值都写进文件，成了两行同名的键：

```yaml
# ⚠️ 配置冲突：升级到 1.1.0 时这个键的建议值变了。
# 保留一行、删掉另一行和本注释；在那之前 brickkit 拒绝启动。
GREETING: 你好  # brickkit:conflict current 1.0.0
GREETING: Hi  # brickkit:conflict proposed 1.1.0
```

手写时不小心写了两行同名的键，也是同一个报错。

**解决**

用纯文本编辑器打开文件，留下你要的那一行，删掉另一行和上面两行说明。行尾的 `# brickkit:conflict …` 标记可以一起删掉：

```yaml
GREETING: 你好
```

然后重新执行命令。背景见 [升级与配置迁移](../02-project-guide/06-upgrade-and-migration.md#解决冲突)。

## 用 yq 或"格式化文档"之后，冲突报错消失了，值却不对

**症状**

对冲突文件执行了 `yq`，或者在编辑器里点了"格式化文档"。之后 `up` 不再报冲突，组件跑了起来，但读到的值不是你想要的——常常是新版本的默认值。

**原因**

YAML 规范没有定义重复键该怎么处理。大多数第三方工具在读的时候**悄悄丢掉其中一个**（通常是前一个），再把剩下的写回去。
冲突块于是被"解决"了，只是替你选了一个值，而你并不知道选的是哪个。这正是 BrickKit 故意用重复键表示冲突、并在报错里提醒不要格式化的原因。

**解决**

1. 把文件恢复到格式化之前：没提交过就 `git checkout -- config/<组件>.yaml`；已经提交了，从 Git 历史里找回那个版本。
   升级前的原文件还在 `config/.archive/` 里时，也可以对照它。
2. 用**纯文本编辑器**（关掉 YAML 插件的自动格式化）手动留下你要的那一行。
3. 以后对 `config/` 下有冲突块的文件，不要用任何会重新序列化 YAML 的工具。

## `$var:` 引用的变量不存在

**症状**

```text
❌ 错误：配置文件引用了哪里都没有定义的公共变量
   未定义的引用：config/demo-hello.yaml: GREETING → $var:NOPE
   建议：在 config/vars.yaml（或部署文件的 vars:）里定义这个变量
```

**原因**

`$var:NAME` 先查当前部署文件的 `vars:`，再查 `config/vars.yaml`，两处都没有。常见的几种：变量名拼错；变量只写在了 `deploy.prod.yaml` 的 `vars:` 里，
而这次跑的是 `deploy.yaml`；本地模式开着，`deploy.local.yaml` 是很久以前复制的，没有团队后来加的 `vars:`。

**解决**

在 `config/vars.yaml`（所有环境共用）或当前部署文件的 `vars:`（只这个环境）里定义它。不确定这次读的是哪份部署文件：`brickkit local status`。
`brickkit lint` 也会报同样的问题，适合在提交前发现。

## 配置写了，却没有生效

**症状**

在 `config/` 里写了一项，组件运行起来仍是默认行为。`up` 的输出里有一条警告：

```text
⚠️ demo/hello@1.0.0：GREETTING 不在组件的 configSchema 里，不会生效
   文件：config/demo-hello.yaml
   已声明的配置项：GREETING
   建议：是不是想写 GREETING？
```

**原因**

键名拼错了，或者组件的 `configSchema` 里根本没有这一项。配置项的名字就是注入的环境变量名，一个不在 `configSchema` 里的键，平台不会注入——
组件读不到它，就走自己的默认分支，照常运行。这类错误不会让任何东西崩溃，所以平台用警告把它说出来。

**解决**

按"是不是想写"的建议改名。组件有哪些配置项，看它的 `component.yaml` 的 `configSchema`，或者 `config/<组件>.yaml` 骨架里注释掉的那些行。

## 环境变量会不会被别处悄悄覆盖

**不会。** 一个组件拿到的每个配置值，都能从文件里直接读出来：`config/<组件>.yaml` 写了什么就是什么；写的是 `$var:NAME`，就去当前部署文件的 `vars:`
与 `config/vars.yaml` 找；写的是 `${VAR}`，就是进程环境或 `.env` 里的值。没有全局默认层、没有继承、没有别的文件会在背后替换它——所见即所得。

唯一的例外是配置项撞上了平台保留的名字（`COMPONENT_ID`、`PORT`、以 `_ENDPOINT` 结尾的名字等），这时平台的值优先，并且会警告，
见 [环境变量注入契约](../06-architecture/03-env-injection-contract.md#保留的名字)。

想确认组件最终拿到了什么：`brickkit up --dry-run`，再看 `.brickkit/generated/compose.yaml` 里那个服务的 `environment`（K8s 下是生成的 Deployment）。
