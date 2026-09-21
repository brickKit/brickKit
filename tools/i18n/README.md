# tools/i18n · CLI 多语言迁移工具集

这些是**开发用的小工具**，不编进 CLI（跟 `cmd/gen-schemas` 是同一类）。子项目 2（全量迁移）
用它们把 `internal/` 里约 1400 条用户可见的中文文案迁进了消息目录；留在仓库里，是因为以后
新增一个包、扩展更多语言、或者哪天又要批量改文案时，这套流程还能直接用。

日常改一条文案**不需要**它们：改 `internal/i18n/catalog_{en,zh}.go` 里对应的那一行就行。
新增一条文案：在 `internal/msgid` 加常量，两份目录各写一条，`TestCatalogParity` 会拦漏的。

## 有哪些

| 工具 | 干什么 |
| --- | --- |
| `migrate/`（Go） | 用 `go/parser` 找出 Go 源码里带中文的字符串字面量，按所处的调用（`Printf`、`clierr.New`、`WithDetail`、结构体字段……）判断怎么改，生成 `i18n.T(msgid.X, …)` 改写、msgid 常量和两份目录条目。英文措辞由人写 |
| `fix_imports.py` | 给改写过的文件补 `i18n` / `msgid` 的 import，并删掉编译器点名"没用到"的 import |
| `post_join.py` | 把写死的分隔符 `strings.Join(x, "、")` 换成随语言变的共享 key |
| `rename_keys.py` | 两个包的文案措辞完全一致时，把私有常量提升成共享的（或改名） |
| `failx.py` | 跑测试，把"断言失败"抽成 `文件:行  类型  期望片段` 的清单 |
| `suggest.py` | 按中文片段查目录里的候选译文，帮你决定期望值改成英文的哪一段 |
| `testrewrite.py` | 批量改测试里的期望值：带次数校验、跳过注释行、一个文件失败不影响别的文件 |

## 迁移一个包的流程

```bash
# 1. 抽取（只读，不改源码）。清单里一行一条：编号、位置、所处的调用、中文
go run ./tools/i18n/migrate extract -out /tmp/e.json -listing /tmp/l.txt internal/cli/xxx.go
less /tmp/l.txt

# 2. 写译文文件：每行 `编号<TAB>英文`，`⏎` 表示换行；`@常量名` 表示复用已有的共享 key
#    （比如 `12<TAB>@LabelReason`）。中文英文都相同的条目会自动复用，不用写 @
#    清单末尾标 MANUAL 的是工具不敢自动改的（包级 const、拿字面量做比较的……），不用给译文，手工处理

# 3. 应用。先全部校验（区间不重叠、@ 的常量存在）再统一写，半路失败不会留下改一半的文件
go run ./tools/i18n/migrate apply -entries /tmp/e.json -trans /tmp/t.txt -pkg cli

# 4. 收尾
python3 tools/i18n/post_join.py            # 嵌在别的文案参数里的 Join 分隔符
python3 tools/i18n/fix_imports.py          # 补 / 删 import
gofmt -w internal/msgid internal/i18n internal/cli
go build ./... && go vet ./...

# 5. 迁测试：列出失败的断言，逐条把期望换成英文
python3 tools/i18n/failx.py ./internal/cli/
python3 tools/i18n/suggest.py 已停止
```

`-pkg` 决定 key 前缀和 msgid 文件名：`cli` 生成 `internal/msgid/cli_<文件名>.go`，其他包
（`skills`、`gitrepo`……）生成 `internal/msgid/<包名>.go`。

## 工具会自己处理的

- `Printf` 头尾的换行留在调用点的格式串里（`Printf("\n%s\n", i18n.T(…))`），目录文案不带换行；
- 顺序动词改成位置动词（`%s` → `%[1]s`），零参数时把 `%%` 还原成 `%`（`i18n.T` 无参数时不走 Sprintf）；
- `Newf` / `WithDetailf` / `Addf` 变成不带 f 的版本；`Sprintf` 整个调用被换掉；
- `Errorf` 结尾恰好有一个 `%w` 的，文案进目录、`%w` 留在调用点包装原错误；没有 `%w` 的换成 `errors.New`；
- **嵌套**：Printf 的参数里嵌着另一处中文时，先算内层，外层取参数源码时套上内层的改写；
- 命令的 `Short` / `Long` / `Example` 字段直接用字段名当 key，比取词更好认。

## 工具不做的（要人来）

- **包级 `var` / `const`**：包初始化时语言还没确定，不能在那里调 `i18n.T`。要改成函数
  （`func reasonUnknown() string { return i18n.T(…) }`）或惰性取文案的类型；
- **拿中文字面量做比较**：先想清楚这个值是"标识"还是"显示"。从不显示的标识改成语言中立的英文值；
  要显示的，标识和显示分开（值用中立标识，显示走 `Label()` 之类）；
- 嵌在拼接表达式里的 `strings.Join(x, "、")`：交给 `post_join.py`；
- 测试里的**否定断言**（`NotContains` / `NotEqual` 带中文短语）：改完文案后它们对英文输出永远成立。
  `tests/i18nguard` 会拦住还留着中文的那种；把短语换成对应的英文措辞。

## 测试

`go test ./tools/i18n/...` 在一个临时的迷你仓库里跑完 extract → apply，检查改写结果、
msgid 常量、两份目录、嵌套、`%w`、复用与出错时不改文件。
