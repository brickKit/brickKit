#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""检查文档里写的 brickkit 命令与参数是不是真的存在（开发计划 Step 40）。

查两类：

  ① 不存在的命令   文档写了 `brickkit foo`，而 CLI 里没有 foo
  ② 不存在的参数   文档写了 `brickkit up --bar`，而 up 没有 --bar

# 为什么需要它

`make check-docs` 查的是**引用**（小节号、文件链接）有没有指向不存在的地方，
查不到**内容**是否与实现一致。而文档里最容易悄悄过期的恰恰是命令行：
改一个 flag 名、删一个参数，代码和测试都会跟着改，
文档里那行 `brickkit up --old-flag` 却没人记得。

它不会让任何测试失败，也不会让构建报错——直到有人照着文档敲了一遍，
得到 `unknown flag`，然后开始怀疑是自己装错了版本。

# 这个脚本自己会不会坏

会，而且同类脚本在这个项目里已经坏过三次（Step 33 的错误审计、
Step 39 的证据审计，都是先报出一堆假结果）。所以**自检是它的一部分**：
先拿几个确定存在、以及确定不存在的命令/参数验一遍解析，
验不过就直接退出，而不是继续跑出一个漂亮的 0。

一个不会失败的检查等于没有检查。
"""

import glob
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
# 为什么不做它（`brickkit search` 就是——发现归市场前端，007 §17.2）。这类句子
# 同样必须写出那个名字，否则读者根本不知道在说哪件事。
# "不提供 / 不会有"是同一类墓碑的另外两种说法，都是真句子逼出来的：
#   012 §2.21   "为什么平台不提供一个 `brickkit up --consolidated`"
#   组件合并部署 "| `brickkit up --consolidated` 之类的命令 | 没有，也不会有 |"
# 这两句的价值恰恰在于写出那个不存在的参数名。把它们报成"文档写错了"，
# 只会逼人把「明确拒绝做这件事」的论证删掉，而下一个人又会把功能加回来。
TOMBSTONE = re.compile(r"已删除|已作废|删掉|删除了|整个删|移除了|不再支持|早先有过|"
                       r"为什么没有|也没有|刻意不做|不打算做|不提供|不会有|~~")

# 反向检查（"二进制里有、文档里没有"）时豁免的东西。
#
#   help / completion  cobra 自带，不是这个平台的能力
#   --help / --config / --log-level  全局工具参数，不属于任何一条命令的语义
#
# 这份豁免是**白名单**，不是"凡是没写文档的都算豁免"——新增一条命令或参数而
# 忘了写进设计书，就该在这里报出来。
UNDOCUMENTED_OK_CMDS = {"help", "completion"}
UNDOCUMENTED_OK_FLAGS = {"--help", "--config", "--log-level"}

# 自检基线：(命令, 参数, 是否应当存在)
SELF_CHECK = [
    ("up", "--dry-run", True),      # 确定有
    ("publish", "--sign", True),    # 确定有
    ("up", "--no-such-flag", False),  # 确定没有
]

SKIP_DIRS = ("playground", "node_modules", ".tools", "bin", "data")


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
        r = subprocess.run([binary, name, "--help"], capture_output=True, text=True)
        return r.returncode == 0 and "unknown command" not in (r.stderr + r.stdout)

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
    """
    hits = list(re.finditer(r"brickkit ([a-z][a-z-]*)", line))
    out = []
    for n, m in enumerate(hits):
        end = hits[n + 1].start() if n + 1 < len(hits) else len(line)
        rest = line[m.end():end]
        if stop := STOP.search(rest):
            rest = rest[:stop.start()]
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
    for pattern in ["design/**/*.md", "试用指南/**/*.md", "*.md", "deploy/**/*.md",
                    "llms.txt", "internal/skills/assets/**/*.md"]:
        for path in glob.glob(pattern, recursive=True):
            if any(d in path for d in SKIP_DIRS):
                continue
            yield path


def check(surface):
    """正向：文档写的命令/参数，二进制里存在吗。

    顺带记下文档里**出现过**哪些命令与参数，供 undocumented() 做反向检查。
    """
    bad_cmd, bad_flag = [], []
    seen_cmd = seen_flag = 0
    documented = {}

    for path in docs():
        try:
            lines = open(path, encoding="utf-8").read().split("\n")
        except (OSError, UnicodeDecodeError):
            continue

        for i, line in enumerate(lines, 1):
            tomb = TOMBSTONE.search(line)
            for cmd, rest in usages(line):
                seen_cmd += 1

                if cmd not in surface:
                    if not tomb:
                        bad_cmd.append((path, i, cmd))
                    continue
                documented.setdefault(cmd, set())

                for flag in re.findall(r"(?<![\w-])--[a-z][a-z-]*", rest):
                    seen_flag += 1
                    if flag in PLACEHOLDERS or flag in surface[cmd] or tomb:
                        continue
                    bad_flag.append((path, i, f"{cmd} {flag}"))
            # 反向检查（「参数有、文档没写」）用的是**宽松**归属：参数常写在表格、
            # 散文、小节标题底下，离命令很远。这一侧宽松只会漏报，不会误报；
            # 而上面那一侧（报文档写错了）必须严格，否则会冤枉正确的句子。
            for cmd in re.findall(r"brickkit ([a-z][a-z-]*)", line):
                if cmd in surface:
                    documented.setdefault(cmd, set()).update(
                        re.findall(r"(?<![\w-])--[a-z][a-z-]*", line))

        # 「参数：」表格里的行不带 `brickkit xxx`，靠所属小节归属命令
        section = None
        for line in lines:
            m = re.match(r"#+ [0-9.]* ?brickkit ([a-z][a-z-]*)", line)
            if m:
                section = m.group(1) if m.group(1) in surface else None
                continue
            if section:
                if line.startswith("#"):
                    section = None
                    continue
                documented.setdefault(section, set()).update(
                    re.findall(r"(?<![\w-])--[a-z][a-z-]*", line))

    return bad_cmd, bad_flag, seen_cmd, seen_flag, documented


# COBRA_BUILTINS 是 cobra 自带、不属于"BrickKit 的命令集"的那几个。
# 文档说"N 个命令"指的是业务命令，不含它们。
COBRA_BUILTINS = {"completion", "help"}

# COUNT_CLAIM 匹配文档里的数量声明：「11 个命令」「10 个命令 + version」「12 条命令」。
#
# 量词写两个（个 / 条）不是凑数：第一版只认"个命令"，而 llms.txt 与 README
# 里写的恰好是"12 条命令"——守卫上线当天就漏了两处。文档是人写的，
# 同一件事换个量词很正常，认死一个等于给自己留个后门。
COUNT_CLAIM = re.compile(r"(\d+)\s*[个条]命令")

# FROZEN_DOCS 是已冻结的历史记录：它们描述的是"当初做完时是什么样"，
# 不该被迫跟着现状变——那正是"冻结"的含义（见 开发计划.md 的头部说明）。
#
# 只对**数量声明**豁免。命令名与参数名照查：那两条是"文档写了不存在的东西"，
# 冻结的文档同样不该指向一个不存在的命令——它会把考古的人引向虚空。
FROZEN_DOCS = ("开发计划.md", "开发进度/")


def check_command_count(surface):
    """文档里写的"N 个命令"必须与真实数目一致。

    # 为什么值得单独查

    这个数字没有任何东西守着，而它散在好几份文档里。复核时实测三处三个数：
    000 说"11 个命令"、AI-CONTEXT 说"10 个命令 + version"、
    llms.txt 说"11 个命令 + version"（那是 12）。

    命令有没有、参数对不对都有守卫，唯独"一共几个"没有——而 000 是**导航
    文档**，读者拿它当索引核对；llms.txt 是喂给 AI 助手的摘要，数错了它会
    照着编出一个不存在的命令来凑数。

    只查业务命令数（不含 cobra 自带的 completion / help）。声明里
    "+ version" 那部分不参与比对——那是各文档自己的措辞。
    """
    real = len({name for name in surface if name and name not in COBRA_BUILTINS
                and name != "version"})

    bad = []
    for path in docs():
        if any(path.startswith(f) for f in FROZEN_DOCS):
            continue
        for i, line in enumerate(open(path, encoding="utf-8"), 1):
            for m in COUNT_CLAIM.finditer(line):
                if int(m.group(1)) != real:
                    bad.append((path, i, m.group(0), line.strip()[:60]))
    return real, bad


def undocumented(surface, documented):
    """反向：二进制里有、而文档里一次都没出现的命令与参数。

    正向检查挡的是"照着文档敲会 unknown flag"；这一条挡的是**反过来**——
    新增了能力却没写进任何文档，使用者根本不知道它存在。两个方向都要有人守：
    只查正向时，`publish` 悄悄长出 5 个参数而设计书一个都没写，没有任何东西会报。
    """
    miss_cmd, miss_flag = [], []
    for cmd, flags in sorted(surface.items()):
        if cmd == "":            # 根命令的全局参数
            continue
        if cmd in UNDOCUMENTED_OK_CMDS:
            continue
        if cmd not in documented:
            miss_cmd.append(("（全部文档）", 0, cmd))
            continue
        for flag in sorted(flags):
            if flag in UNDOCUMENTED_OK_FLAGS or flag in documented[cmd]:
                continue
            miss_flag.append(("（全部文档）", 0, f"{cmd} {flag}"))
    return miss_cmd, miss_flag


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

    bad_cmd, bad_flag, seen_cmd, seen_flag, documented = check(surface)
    miss_cmd, miss_flag = undocumented(surface, documented)

    # 扫到 0 处问题和根本没扫到东西，输出长得一模一样。把数目报出来，
    # 一份"检查通过"才有意义。
    if seen_cmd == 0:
        print("❌ 一处 brickkit 命令用法都没扫到——多半是文档路径或正则不对，"
              "而不是文档里真的没有命令。")
        sys.exit(2)
    print(f"   （检查了 {seen_cmd} 处命令用法、{seen_flag} 处参数）\n")

    real, bad_count = check_command_count(surface)
    if bad_count:
        print(f"❌ 文档里的命令数目对不上：{len(bad_count)} 处（真实是 {real} 个业务命令）")
        for path, line_no, claim, text in bad_count:
            print(f"   {path}:{line_no}  写着「{claim}」")
            print(f"     {text}")
        print("   → 增删命令时改了实现与各处说明，唯独这个数字没人动")
        sys.exit(1)
    print(f"✅ 命令数目：文档与实现一致（{real} 个业务命令）\n")

    failed = report("文档写了不存在的命令", bad_cmd, "命令被改名或删掉了，文档没跟着改")
    failed |= report("文档写了不存在的参数", bad_flag, "参数被改名或删掉了，文档没跟着改")
    failed |= report("命令有、文档没写", miss_cmd, "新增了命令却没写进任何文档")
    failed |= report("参数有、文档没写", miss_flag, "新增了参数却没写进任何文档")

    if bad_cmd or bad_flag:
        print("\n照着文档敲一遍会得到 unknown flag/command——"
              "而使用者多半会以为是自己装错了版本。")
    if miss_cmd or miss_flag:
        print("\n这些能力使用者只能靠 --help 撞见——文档里一次都没出现过。")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
