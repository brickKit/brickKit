#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""检查文档里写的 brickkit 命令与参数是不是真的存在。

查四类：

  ① 不存在的命令   文档写了 `brickkit foo`，而 CLI 里没有 foo
  ② 不存在的参数   文档写了 `brickkit up --bar`，而 up 没有 --bar
  ③ 数量过期       文档写「N 个命令」「N 个测试函数」，而真实数目对不上
  ④ 命令参考不全   CLI 有的命令或参数，命令参考（docs/{en,zh}/07-cli-reference/README.md）
                   里没有写

# 为什么需要它

`make check-docs` 查的是**引用**（小节号、文件链接）有没有指向不存在的地方，
查不到**内容**是否与实现一致。而文档里最容易悄悄过期的恰恰是命令行：
改一个 flag 名、删一个参数，代码和测试都会跟着改，
文档里那行 `brickkit up --old-flag` 却没人记得。

它不会让任何测试失败，也不会让构建报错——直到有人照着文档敲了一遍，
得到 `unknown flag`，然后开始怀疑是自己装错了版本。

# 这个脚本自己会不会坏

会，而且同类脚本在这个项目里已经坏过好几次（错误建议的审计、验收证据的审计，
都是先报出一堆假结果）。所以**自检是它的一部分**：
先拿几个确定存在、以及确定不存在的命令/参数验一遍解析，
验不过就直接退出，而不是继续跑出一个漂亮的 0。

一个不会失败的检查等于没有检查。
"""

import os
import re
import subprocess
import sys

# 文档里常见的占位符与示意写法，不是真参数。
PLACEHOLDERS = {"--...", "--flag", "--选项"}

# 墓碑句：文档**刻意**提到一个已经删掉的命令或参数，说明为什么不再有它。
# 这类句子的价值恰恰在于写出那个不存在的名字——把它当成"文档写错了"来报，
# 只会逼人把删除记录一起删掉，而下一个人又会把功能加回来。
# 只认同一行内的删除措辞：范围窄，不至于把真的笔误一起放过去。
#
# "刻意不做"是与"已删除"并列的另一类墓碑：那个命令**从来没有过**，而文档要写清
# 为什么不做它（`brickkit search` 就是——发现归市场前端）。这类句子
# 同样必须写出那个名字，否则读者根本不知道在说哪件事。
# "不提供 / 不会有"是同一类墓碑的另外两种说法，都是真句子逼出来的：
#   "为什么平台不提供一个 `brickkit up --consolidated`"
#   "| `brickkit up --consolidated` 之类的命令 | 没有，也不会有 |"
# 这两句的价值恰恰在于写出那个不存在的参数名。把它们报成"文档写错了"，
# 只会逼人把「明确拒绝做这件事」的论证删掉，而下一个人又会把功能加回来。
TOMBSTONE = re.compile(r"已删除|已作废|删掉|删除了|整个删|移除了|不再支持|早先有过|"
                       r"为什么没有|也没有|刻意不做|不打算做|不提供|不会有|~~|"
                       # 英文文档的同一类说法
                       r"\b(?:[Dd]eleted|[Rr]emoved|[Nn]o longer (?:exists|supported)|[Tt]here is no|"
                       r"[Dd]eliberately (?:no|not)|[Nn]ever (?:had|existed))\b")

# 反向检查（"二进制里有、文档里没有"）时豁免的东西。
#
#   help / completion  cobra 自带，不是这个平台的能力
#   --help  cobra 自带
#
# 这份豁免是**白名单**，不是"凡是没写文档的都算豁免"——新增一条命令或参数而
# 忘了写进命令参考，就该在这里报出来。（--log-level 是全局参数，命令参考照样要讲它。）
UNDOCUMENTED_OK_CMDS = {"help", "completion"}
UNDOCUMENTED_OK_FLAGS = {"--help"}

# 命令参考：每条命令、每个参数都必须在这里写到。两种语言各一份，各自完整。
CLI_REFERENCES = ["docs/en/07-cli-reference/README.md", "docs/zh/07-cli-reference/README.md"]

# 历史与规划目录不是"现行文档"。
EXCLUDED_PREFIXES = ("archive/", "docs/superpowers/", "new_plan/", ".superpowers/")

# 自检基线：(命令, 参数, 是否应当存在)
SELF_CHECK = [
    ("up", "--dry-run", True),      # 确定有
    ("publish", "--sign", True),    # 确定有
    ("up", "--no-such-flag", False),  # 确定没有
    ("skills update", "--lang", True),  # 子命令自己的参数
]



def cli_surface(binary):
    """问 CLI 要它自己的命令与参数：{命令: {参数集合}}，全局参数放在 ""。

    命令名**不靠解析帮助文本认定**——帮助是分组排版的（"项目命令："下面
    缩进两格），排版一改解析就悄悄少认几个命令，而少认的后果是
    "文档写了不存在的命令"这一类**全部漏报**。

    所以这里只把帮助文本当**候选来源**，每个候选再真跑一次
    `brickkit <名字> --help` 让 CLI 自己确认。解析漏了会被自检抓住，
    解析多认了会被探测否掉。
    """
    def probe(name):
        r = subprocess.run([binary] + name.split() + ["--help"], capture_output=True, text=True)
        return r.returncode == 0 and "unknown command" not in (r.stderr + r.stdout)

    def subcommands(name):
        """帮助里 "Available Commands:" 下列出的子命令名。"""
        out = subprocess.run([binary, name, "--help"], capture_output=True, text=True).stdout
        m = re.search(r"(?ms)^Available Commands:\n(.*?)(?:\n\S|\Z)", out)
        if not m:
            return []
        return [n for n in re.findall(r"(?m)^ {2}([a-z][a-z-]*) {2,}", m.group(1))
                if n not in COBRA_BUILTINS]

    def flags(args):
        """只认**参数定义行**，不扫整段帮助文本。

        帮助里的描述会提到别的命令的参数（add 的 Long 里写着"可用
        brickkit reset --last 撤销"）。整段扫的话，`--last` 就成了 add 的参数。
        正向检查里这只是让"存在集合"偏大、少报几条；反向检查里却是致命的——
        它会凭空造出 `init --repo` 这种根本不存在的参数。

        定义行的形状是固定的：缩进 + 可选的短参 + `--名字`。
        """
        out = subprocess.run([binary] + args + ["--help"],
                             capture_output=True, text=True).stdout
        return set(re.findall(r"(?m)^\s+(?:-\w, )?(--[a-z][a-z-]*)", out))

    root = subprocess.run([binary, "--help"], capture_output=True, text=True).stdout
    # 候选：分组列表里的 `  add   添加组件`，以及示例里的 `brickkit add ...`
    candidates = set(re.findall(r"^ {2,4}([a-z][a-z-]*) {2,}\S", root, re.M))
    candidates |= set(re.findall(r"brickkit ([a-z][a-z-]*)", root))

    surface, phantom = {"": flags([])}, []
    for name in sorted(candidates):
        if probe(name):
            surface[name] = flags([name]) | surface[""]
            # 子命令（skills update、local on……）有自己的参数：键写成 "skills update"。
            # 同样只把帮助里的列表当候选，每个再真跑一次 --help 确认。
            if name not in COBRA_BUILTINS:
                for sub in subcommands(name):
                    if probe(f"{name} {sub}"):
                        surface[f"{name} {sub}"] = flags([name, sub]) | surface[""]
        else:
            # 帮助文本里提到、CLI 里却没有——`brickkit --help` 是全产品被读得
            # 最多的一段文字，第一屏就教人敲一条报 unknown command 的命令。
            #
            # 从前这里只是 `if probe(name)`，探不到的**静默丢弃**：候选表只用来
            # 给"存在集合"瘦身，从没人问过"丢掉的是什么"。`brickkit order` 删除时
            # 文档全清干净了，root.go 自己的 Example 却留了一行，正是这么漏过去的。
            # 文档那边有守卫所以没漏，CLI 自己这边没有，所以漏了。
            phantom.append(name)
    return surface, phantom


def self_check(surface):
    """确认解析没坏。坏了就退出——继续跑只会给出一个假的通过。"""
    problems = []
    for cmd, flag, expected in SELF_CHECK:
        if cmd not in surface:
            problems.append(f"解析不出命令 {cmd}")
        elif (flag in surface[cmd]) != expected:
            problems.append(
                f"{cmd} {flag}：应当{'存在' if expected else '不存在'}，"
                f"而解析结果是{'存在' if flag in surface[cmd] else '不存在'}")
    # 反向检查要真能报出缺口：一份只写了 up、没写 --dry-run 的命令参考，
    # 与一份什么都没写的命令参考，都必须被点名
    fake = {CLI_REFERENCES[0]: {"up": set(), "": set()}, CLI_REFERENCES[1]: {}}
    missing = {(ref, what) for ref, _, what in undocumented(surface, fake)}
    for want in ((CLI_REFERENCES[0], "brickkit up --dry-run"), (CLI_REFERENCES[1], "brickkit up")):
        if want not in missing:
            problems.append(f"反向检查没报出 {want[0]} 缺 {want[1]}")
    # 一份把每条命令自己的参数都写到、全局参数只写一处的命令参考，必须一个缺口都不报
    full = {cmd: flags - surface[""] for cmd, flags in surface.items() if cmd}
    full[""] = set(surface[""])
    extra = undocumented(surface, {ref: full for ref in CLI_REFERENCES})
    if extra:
        problems.append(f"写全了的命令参考仍被报缺口（多半是全局参数被要求逐条命令重写）：{extra[:3]}")
    # 布局规定的命令参考写法：带编号、带反引号的标题，底下的「#### 参数」不结束小节
    sample = ["### 6. `brickkit add <id>[@ver]`", "", "```bash", "# 模式 A：代码块里的注释不是标题", "```",
              "#### 参数", "| `--repo` | 同时 clone |",
              "### 7. `brickkit remove <id>`", "| `--force` | 强制 |"]
    *_, got = scan_lines("<自检>", sample, surface)
    if "--repo" not in got.get("add", set()) or "--force" in got.get("add", set()) \
            or "--force" not in got.get("remove", set()):
        problems.append(f"小节标题归属参数错了：{got}")
    # 子命令的参数归子命令，不能被当成父命令写错了参数
    bc, bf, *_, got = scan_lines("<自检>", ["brickkit skills update --lang zh"], surface)
    if bc or bf or "--lang" not in got.get("skills update", set()):
        problems.append(f"子命令的参数没认出来：{bc} {bf} {got}")
    # 已删除命令的"墓碑"两种语言都要认：英文文档写 Deleted / Removed，不能被当成写了不存在的命令
    for tomb in ("| Removed | `brickkit override` | Deleted: replaced by `brickkit local` |",
                 "| 已删除 | `brickkit override` | 已删除：改成 `brickkit local` |"):
        bc, *_ = scan_lines("<自检>", [tomb], surface)
        if bc:
            problems.append(f"删除标记没认出来：{tomb}")
    # 代码块里的注释行不是命令；代码块里真写出来的命令照查
    bc, *_ = scan_lines("<自检>", ["```yaml", "# until then brickkit refuses to start.", "```"], surface)
    if bc:
        problems.append(f"代码块里的注释被当成了命令：{bc}")
    bc, *_ = scan_lines("<自检>", ["```bash", "brickkit refuses", "```"], surface)
    if not bc:
        problems.append("代码块里写错的命令没被报出来")
    bc, *_ = scan_lines("<自检>", ["Run `brickkit override` to refresh it."], surface)
    if not bc:
        problems.append("不带删除标记的已删除命令没被报出来")
    if problems:
        print("❌ 自检失败：" + "；".join(problems))
        print("   说明这个脚本读 CLI 的方式坏了，报出来的结果不可信。先修脚本。")
        sys.exit(2)


# 命令行到此为止的边界：中文（后面是散文）、`→`（后面是"等价于另一条命令"）、
# `|`（markdown 表格的下一格）。命令行本身不会出现这三样。
STOP = re.compile(r"[\u3000-\u303f\u4e00-\u9fff\uff00-\uffef]|→|\|")


def usages(line):
    """把一行拆成若干「命令 → 紧跟它的那段命令行」。

    两条规则，都是被真实文档逼出来的：

    **① 参数归给离它最近的那条命令。** 一行里出现多条命令是常事：

        1. 先执行一次 brickkit add / brickkit remove，之后即可用 brickkit reset --last 回退

    整段扫的话 `--last` 会被算成 `add` 的参数，于是报一个根本不存在的
    `add --last`——而这句话完全正确，它是 CLI 自己打印的建议。
    （cli_surface 解析 --help 时早就踩过同一个坑，见那里的注释。）

    **② 遇到中文就停。** 散文里提到参数名不等于"把它敲在那条命令后面"：

        `brickkit up` 一键启动。想改源码就加 `--repo`

    这里的 `--repo` 属于上一句的 `add`，而离它最近的命令是 `up`。
    只按"最近"归属会把它报成 `up --repo`。命令行里不会出现中文，
    因此第一个中文字符就是"命令行到此为止"的可靠边界。

    同理还有两个边界：`→`（"`brickkit up` → `docker compose up -d --wait`"
    里的 `--wait` 是 compose 的）和 `|`（markdown 表格的下一格）。

    **③ 行内代码里的命令，只认自己那段代码与紧随其后的纯参数代码段**（见下面的实现注释）
    ——英文文档没有"第一个中文字符"这个边界，全靠这一条收住。
    """
    # 前面不能紧挨着名字字符：`__start_brickkit brickkit`（bash 的 complete -p 输出）里的
    # "brickkit brickkit" 不是一条叫 brickkit 的命令
    hits = list(re.finditer(r"(?<![\w-])brickkit ([a-z][a-z-]*)", line))
    out = []
    for n, m in enumerate(hits):
        end = hits[n + 1].start() if n + 1 < len(hits) else len(line)
        rest = line[m.end():end]
        if stop := STOP.search(rest):
            rest = rest[:stop.start()]
        # **③ 行内代码里的命令，只认自己那段代码，外加紧随其后、通篇只有参数的代码段。**
        # 英文文档里没有"第一个中文字符"这个边界，`brickkit up` reports `Error: …`
        # (under Docker, `up -d --wait` …) 整句都会被当成命令行，把 docker compose 的
        # --wait 算成 up 的参数。命令在行内代码里（它前面的反引号是奇数个）时：
        # 它自己那段代码算；后面的散文不算；后面的别的代码段，只有"以 - 开头"（就是
        # 一个参数，像 `--strict`）才算——`up -d --wait` 以命令词开头，是另一条命令。
        if line.count("`", 0, m.start()) % 2 == 1 and "`" in rest:
            parts = rest.split("`")
            keep = [parts[0]] + [p for p in parts[2::2] if p.strip().startswith("-")]
            rest = " ".join(keep)
        out.append((m.group(1), rest))
    return out


def docs():
    # llms.txt 不是 .md，但它是**喂给 AI 助手的摘要**——里面写错一个命令名或
    # 一个数字，AI 就会照着教用户敲一条不存在的命令。它比任何一份 .md 都
    # 更该被守着。（真漏过：命令数目那条守卫上线时，llms.txt 里写的还是
    # "11 个命令 + version"，而扫描范围没覆盖它。）
    #
    # internal/skills/assets/ 是内嵌进 CLI、由 brickkit init 直接装进用户项目的
    # AI 助手技能。它们也算文档，而且是**最会被照着敲**的一类：读者是 AI 助手，
    # 它不会像人一样怀疑"是不是我装错了版本"，只会自信地把假参数敲下去。
    # 而且这些文件不在用户仓库里，用户改不了——说谎只能在这里被拦住。
    out = subprocess.run(["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard",
                          "*.md", "llms.txt", "llms.*.txt"],
                         capture_output=True, check=True).stdout
    for raw in out.split(b"\0"):
        path = raw.decode("utf-8")
        if not path or path.startswith(EXCLUDED_PREFIXES) or not os.path.isfile(path):
            continue
        yield path


FLAG = re.compile(r"(?<![\w-])--[a-z][a-z-]*")

# 小节标题里的命令：`## brickkit up`、`### 6. `brickkit add <id>[@ver]``、`#### 1.2 brickkit local on`。
SECTION = re.compile(r"^(#+)\s*(?:\d+(?:\.\d+)*\.?\s*)?`?brickkit ([a-z][a-z-]*)(?: ([a-z][a-z-]*))?")


def command_key(surface, cmd, word):
    """cmd 后面紧跟的词是它的子命令时，返回 "cmd 子命令"，否则返回 cmd。"""
    if word and f"{cmd} {word}" in surface:
        return f"{cmd} {word}"
    return cmd


def scan_lines(path, lines, surface):
    """扫一份文档：返回 (写错的命令, 写错的参数, 命令用法数, 参数数, {命令: {参数}})。"""
    bad_cmd, bad_flag = [], []
    seen_cmd = seen_flag = 0
    documented = {}

    in_fence = False
    for i, line in enumerate(lines, 1):
        if line.lstrip().startswith("```"):
            in_fence = not in_fence
            continue
        # 代码块里 # 开头的是注释（shell 注释、CLI 生成的 YAML 注释），不是要敲的命令：
        # "# … until then brickkit refuses to start." 说的是 CLI 的行为，不是一条叫 refuses 的命令
        if in_fence and line.lstrip().startswith("#"):
            continue
        tomb = TOMBSTONE.search(line)
        for cmd, rest in usages(line):
            seen_cmd += 1

            if cmd not in surface:
                if not tomb:
                    bad_cmd.append((path, i, cmd))
                continue
            # `brickkit skills update --lang zh`：--lang 属于子命令 skills update
            m = re.match(r"\s+([a-z][a-z-]*)", rest)
            key = command_key(surface, cmd, m.group(1) if m else None)
            if key != cmd:
                rest = rest[m.end():]
            documented.setdefault(key, set())

            for flag in FLAG.findall(rest):
                seen_flag += 1
                if flag in PLACEHOLDERS or flag in surface[key] or tomb:
                    continue
                bad_flag.append((path, i, f"{key} {flag}"))
        # 反向检查（「参数有、文档没写」）用的是**宽松**归属：参数常写在表格、
        # 散文、小节标题底下，离命令很远。这一侧宽松只会漏报，不会误报；
        # 而上面那一侧（报文档写错了）必须严格，否则会冤枉正确的句子。
        for cmd, word in re.findall(r"(?<![\w-])brickkit ([a-z][a-z-]*)(?: ([a-z][a-z-]*))?", line):
            if cmd in surface:
                documented.setdefault(command_key(surface, cmd, word), set()).update(FLAG.findall(line))

    # 「参数」表格里的行不带 `brickkit xxx`，靠所属小节归属命令。
    # 小节只在遇到**同级或更高级**的标题时结束：命令小节底下的「#### 参数」不算结束。
    section = level = None
    fenced = False
    for line in lines:
        if line.lstrip().startswith("```"):
            fenced = not fenced
        # 代码块里以 # 开头的是 shell 注释，不是标题
        head = None if fenced else re.match(r"^(#+)\s", line)
        m = None if fenced else SECTION.match(line)
        if m:
            key = command_key(surface, m.group(2), m.group(3))
            section = key if key in surface else None
            level = len(m.group(1))
            continue
        if head and level is not None and len(head.group(1)) <= level:
            section = level = None
            continue
        if section:
            documented.setdefault(section, set()).update(FLAG.findall(line))

    # 全局参数不属于任何一条命令：整份文件里出现过就算写了，记在 "" 名下
    documented[""] = set(FLAG.findall("\n".join(lines)))
    return bad_cmd, bad_flag, seen_cmd, seen_flag, documented


def check(surface):
    """正向：文档写的命令/参数，二进制里存在吗。

    顺带按文件记下文档里**出现过**哪些命令与参数：{路径: {命令: {参数}}}，
    供 undocumented() 对着命令参考做反向检查。
    """
    bad_cmd, bad_flag = [], []
    seen_cmd = seen_flag = 0
    by_path = {}

    for path in docs():
        try:
            lines = open(path, encoding="utf-8").read().split("\n")
        except (OSError, UnicodeDecodeError):
            continue
        bc, bf, sc, sf, documented = scan_lines(path, lines, surface)
        bad_cmd += bc
        bad_flag += bf
        seen_cmd += sc
        seen_flag += sf
        by_path[path] = documented

    return bad_cmd, bad_flag, seen_cmd, seen_flag, by_path


# COBRA_BUILTINS 是 cobra 自带、不属于"BrickKit 的命令集"的那几个。
# 文档说"N 个命令"指的是业务命令，不含它们。
COBRA_BUILTINS = {"completion", "help"}

# COUNT_CLAIM 匹配文档里的数量声明：「11 个命令」「10 个命令 + version」「12 条命令」，
# 以及英文那一侧的 "16 commands"（英文文档一直有这样的声明，却从没被核对过）。
#
# 量词写两个（个 / 条）不是凑数：第一版只认"个命令"，而 llms.txt 与 README
# 里写的恰好是"12 条命令"——守卫上线当天就漏了两处。文档是人写的，
# 同一件事换个量词很正常，认死一个等于给自己留个后门。
COUNT_CLAIM = re.compile(r"(\d+)\s*(?:[个条]命令|commands\b)")


def check_command_count(surface):
    """文档里写的"N 个命令"必须与真实数目一致。

    # 为什么值得单独查

    这个数字没有任何东西守着，而它散在好几份文档里。复核时实测三处三个数：
    导航文档说"11 个命令"、AI 导读说"10 个命令 + version"、
    llms.txt 说"11 个命令 + version"（那是 12）。

    命令有没有、参数对不对都有守卫，唯独"一共几个"没有——而导航文档是读者
    拿来当索引核对的；llms.txt 是喂给 AI 助手的摘要，数错了它会
    照着编出一个不存在的命令来凑数。

    只查业务命令数（不含 cobra 自带的 completion / help）。声明里
    "+ version" 那部分不参与比对——那是各文档自己的措辞。version 与 lang
    都是 CLI 自身命令（跟业务无关），不计入"业务命令"数——这是多语言设计时
    定下的口径，不是凑数字。
    """
    NON_BUSINESS_COMMANDS = {"version", "lang"}
    real = len({name for name in surface if name and " " not in name
                and name not in COBRA_BUILTINS and name not in NON_BUSINESS_COMMANDS})

    bad = []
    for path in docs():
        for i, line in enumerate(open(path, encoding="utf-8"), 1):
            for m in COUNT_CLAIM.finditer(line):
                if int(m.group(1)) != real:
                    bad.append((path, i, m.group(0), line.strip()[:60]))
    return real, bad


# TEST_COUNT_CLAIM 匹配文档里的测试数量声明：「1728 个测试函数」
# 「2,000+ test functions」。数字里的千位逗号要能认，紧跟数字的 `+` 要能认——
# 那代表这条声明是"下限"（"2000+ 个"），不是精确值，后面单独处理。
TEST_COUNT_CLAIM = re.compile(r"([\d,]+)(\+?)\s*(?:个测试函数|test functions?)")


def real_test_function_count():
    """数一下仓库里真实的 `^func Test` 数量（跨 internal/、market-server/ 等全部模块）。"""
    n = 0
    out = subprocess.run(["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "*_test.go"], capture_output=True, check=True).stdout
    for raw in out.split(b"\0"):
        path = raw.decode("utf-8")
        if not path or path.startswith(EXCLUDED_PREFIXES) or not os.path.isfile(path):
            continue
        with open(path, encoding="utf-8") as f:
            for line in f:
                if line.startswith("func Test"):
                    n += 1
    return n


def check_test_count(real):
    """文档里写的「N 个测试函数」必须与真实数目对得上。

    # 为什么值得单独查

    复核时实测过三处三个数：README 写的是 1762，AGENTS.md 写的是 1728，
    `^func Test` 的真实数目是 2019——三个都不一样，说明这个数字从来没有
    自动化校验过，纯靠手改维护，改动一多就没人记得同步。跟 `check_command_count`
    是同一类问题：没有任何东西守着的数字，迟早会在某一份文档里率先烂掉。

    「2000+」「2,000+」这类写法算下限声明，真实数目只要不小于它就算数——
    测试数量只会随开发增长，用下限描述本来就是为了不用每次都精确对齐；
    但没写 `+` 的精确数字（比如历史存档之外的地方写「1728」）必须精确相等，
    差一个都说明这句话已经不真实了。
    """
    bad = []
    for path in docs():
        for i, line in enumerate(open(path, encoding="utf-8"), 1):
            for m in TEST_COUNT_CLAIM.finditer(line):
                claimed = int(m.group(1).replace(",", ""))
                is_floor = m.group(2) == "+"
                ok = real >= claimed if is_floor else real == claimed
                if not ok:
                    bad.append((path, i, m.group(0), line.strip()[:60]))
    return bad


def undocumented(surface, by_path):
    """反向：二进制里有、而命令参考里没写的命令与参数。每份命令参考各查各的。

    正向检查挡的是"照着文档敲会 unknown flag"；这一条挡的是**反过来**——
    新增了能力却没写进命令参考，使用者根本不知道它存在。两个方向都要有人守：
    只查正向时，`publish` 悄悄长出 5 个参数而文档一个都没写，没有任何东西会报。

    只对着命令参考查，不对着"全部文档"查：散落在教程里的一次提及不算写过——
    命令参考是**详尽**的那一份，读者就是去那里查一个参数是干什么的。
    全局参数（根命令的）写在命令参考里任意一处即可。
    """
    missing = []
    for ref in CLI_REFERENCES:
        if ref not in by_path:
            missing.append((ref, 0, "命令参考还不存在"))
            continue
        documented = by_path[ref]
        written = documented.get("", set())
        for cmd, flags in sorted(surface.items()):
            if cmd in UNDOCUMENTED_OK_CMDS:
                continue
            if cmd and cmd not in documented:
                missing.append((ref, 0, f"brickkit {cmd}"))
                continue
            # 全局参数（--log-level）只要求在命令参考里写一处，不要求每条命令下都写一遍
            own = flags if cmd == "" else flags - surface[""]
            for flag in sorted(own):
                if flag in UNDOCUMENTED_OK_FLAGS:
                    continue
                if (flag in written) if cmd == "" else (flag in documented[cmd]):
                    continue
                missing.append((ref, 0, " ".join(x for x in ("brickkit", cmd, flag) if x)))
    return missing


def report(title, rows, hint):
    if not rows:
        print(f"✅ {title}：无")
        return 0
    print(f"❌ {title}：{len(rows)} 处")
    for path, line, what in sorted(set(rows)):
        print(f"   {path}:{line}  {what}")
    print(f"   → {hint}")
    return 1


def main():
    binary = sys.argv[1] if len(sys.argv) > 1 else "./bin/brickkit"

    surface, phantom = cli_surface(binary)
    self_check(surface)
    print(f"✅ 自检通过（解析出 {len(surface) - 1} 个命令）\n")

    # 先查 CLI 自己的帮助文本，再查文档。顺序是有意的：`brickkit --help`
    # 比任何一份文档都被读得多，它错了比文档错了更要紧。
    if phantom:
        print(f"❌ brickkit --help 里提到了不存在的命令：{len(phantom)} 个")
        for name in phantom:
            print(f"   brickkit {name}")
        print("   → 命令被删掉了，帮助文本（root.go 的 Long / Example）没跟着改")
        print("\n这是使用者读到的第一屏文字，照着敲会得到 unknown command。")
        sys.exit(1)
    print("✅ 帮助文本提到的命令都存在\n")

    bad_cmd, bad_flag, seen_cmd, seen_flag, by_path = check(surface)
    missing = undocumented(surface, by_path)

    # 扫到 0 处问题和根本没扫到东西，输出长得一模一样。把数目报出来，
    # 一份"检查通过"才有意义。
    if seen_cmd == 0:
        print("❌ 一处 brickkit 命令用法都没扫到——多半是文档路径或正则不对，"
              "而不是文档里真的没有命令。")
        sys.exit(2)
    print(f"   （检查了 {seen_cmd} 处命令用法、{seen_flag} 处参数）\n")

    # 每一类都报完再退出：只报第一类，后面几类的问题就被藏住了
    failed = 0
    real, bad_count = check_command_count(surface)
    if bad_count:
        failed = 1
        print(f"❌ 文档里的命令数目对不上：{len(bad_count)} 处（真实是 {real} 个业务命令）")
        for path, line_no, claim, text in bad_count:
            print(f"   {path}:{line_no}  写着「{claim}」")
            print(f"     {text}")
        print("   → 增删命令时改了实现与各处说明，唯独这个数字没人动\n")
    else:
        print(f"✅ 命令数目：文档与实现一致（{real} 个业务命令）\n")

    real_tests = real_test_function_count()
    bad_test_count = check_test_count(real_tests)
    if bad_test_count:
        failed = 1
        print(f"❌ 文档里的测试数量对不上：{len(bad_test_count)} 处（真实是 {real_tests} 个）")
        for path, line_no, claim, text in bad_test_count:
            print(f"   {path}:{line_no}  写着「{claim}」")
            print(f"     {text}")
        print("   → 测试数量只涨不跌，精确数字迟早过期；不想每次都同步就改成"
              "「N+ 个测试函数」这种下限写法\n")
    else:
        print(f"✅ 测试数量：文档里的声明与实际一致（{real_tests} 个测试函数）\n")

    failed |= report("文档写了不存在的命令", bad_cmd, "命令被改名或删掉了，文档没跟着改")
    failed |= report("文档写了不存在的参数", bad_flag, "参数被改名或删掉了，文档没跟着改")

    failed |= report("命令参考没写全", missing, "新增了命令或参数，命令参考没跟着写")

    if bad_cmd or bad_flag:
        print("\n照着文档敲一遍会得到 unknown flag/command——"
              "而使用者多半会以为是自己装错了版本。")
    if missing:
        print("\n这些能力使用者只能靠 --help 撞见——命令参考里查不到。")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
