#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""检查文档引用有没有指向不存在的地方。

查三类：

  ① 悬空小节引用    代码与文档里的 "005 §5.12" 指向设计书里不存在的小节
  ② 断链            markdown 链接指向不存在的文件
  ③ 指南编号错位    试用指南某篇的文件号是 09，H1 或篇内小节却写着 19
  ④ 前置对不上      README 索引表说某篇前置是 12，那一篇自己却写着 13

# 为什么需要它

这两类错误的共同点是**写的时候是对的，之后才坏掉**：重写一节时改了编号、
拆分文件时换了路径，而引用方没人记得跟着改。它们不会让任何测试失败，
也不会让构建报错——直到某天有人点进去，发现指向一个不存在的地方。

真撞到过：重写 005 §7 时改了小节编号，留下 5 处指向 `§7.4.1` 的引用，
其中一处是**使用者会看到的错误文案**（"详见 design/005 §7.4.1"）。

# 这个脚本自己会不会坏

会。写它的过程中错了三次——正则截断得不对、把标题里的 "## 2. 完整结构"
当成不合法（数字后面是点不是空格）、只找了一种引用写法。
每次的症状都是"报出一堆假的缺口"或"报 0 处"，而两者看起来都像成功。

所以**自检是这个脚本的一部分**（见 self_check）：它先拿几个已知存在的小节
验证自己的解析没坏，解析不出来就直接失败，而不是继续跑出一个漂亮的 0。
一个不会失败的检查等于没有检查。
"""

import glob
import os
import re
import sys
import urllib.parse
from collections import defaultdict

# 已知一定存在的小节，用来自证解析没坏。
# 挑的是标题写法各不相同的几个：带点的、多级的、纯数字的。
SELF_CHECK = [
    ("003", "2"),        # "## 2. 完整结构"        —— 数字后带点
    ("003", "3.2"),      # "### 3.2 deploy（必须）" —— 数字后带空格
    ("005", "5.13.1"),   # "#### 5.13.1 …"         —— 三级编号
]

SKIP_DIRS = (".git", "playground", "node_modules", ".tools", "bin", "data")


def walk(patterns):
    for pattern in patterns:
        for path in glob.glob(pattern, recursive=True):
            if any(("/" + d + "/") in ("/" + path) or path.startswith(d + "/") for d in SKIP_DIRS):
                continue
            yield path


def design_sections():
    """收集每本设计书里实际存在的小节号：{"005": {"5.1", "5.13.1", …}}"""
    found = defaultdict(set)
    for path in glob.glob("design/[0-9][0-9][0-9]*.md"):
        number = re.match(r"design/(\d{3})", path).group(1)
        with open(path, encoding="utf-8") as f:
            for line in f:
                # 标题写法有两种：`## 2. 完整结构` 与 `### 3.2 deploy`
                m = re.match(r"#{2,6}\s+(\d+(?:\.\d+)*)\.?\s", line)
                if m:
                    found[number].add(m.group(1))
    return found


def self_check(sections):
    """确认解析本身没坏。坏了就直接退出——继续跑只会给出一个假的通过。"""
    broken = [
        f"{doc} §{sec}"
        for doc, sec in SELF_CHECK
        if sec not in sections.get(doc, ())
    ]
    if broken:
        print("❌ 自检失败：解析不出这些**已知存在**的小节：" + "、".join(broken))
        print("   说明这个脚本的标题解析坏了，报出来的结果不可信。先修脚本。")
        sys.exit(2)


def check_sections(sections):
    """① 悬空小节引用。"""
    bad = []
    for path in walk(["internal/**/*.go", "market-server/**/*.go",
                      "design/*.md", "试用指南/*.md", "开发进度/**/*.md", "*.md"]):
        try:
            lines = open(path, encoding="utf-8").read().split("\n")
        except (OSError, UnicodeDecodeError):
            continue
        for i, line in enumerate(lines, 1):
            for doc, sec in re.findall(r"(\d{3})\s*§\s*(\d+(?:\.\d+)*)", line):
                known = sections.get(doc)
                if not known or sec in known:
                    continue
                # 引用父节是允许的：写 §5 而文档里只有 §5.1 / §5.2
                if any(x.startswith(sec + ".") for x in known):
                    continue
                bad.append((path, i, f"{doc} §{sec}"))
    return bad


def check_links():
    """② markdown 断链。"""
    bad = []
    for path in walk(["design/**/*.md", "试用指南/**/*.md",
                      "开发进度/**/*.md", "deploy/**/*.md", "*.md"]):
        root = os.path.dirname(path)
        try:
            lines = open(path, encoding="utf-8").read().split("\n")
        except (OSError, UnicodeDecodeError):
            continue
        for i, line in enumerate(lines, 1):
            for _, href in re.findall(r"\[([^\]]*)\]\(([^)]+)\)", line):
                if href.startswith(("http", "#", "mailto")):
                    continue
                target = urllib.parse.unquote(href.split("#")[0])
                if not target:
                    continue
                if not os.path.exists(os.path.normpath(os.path.join(root, target))):
                    bad.append((path, i, href))
    return bad


def report(title, rows):
    if not rows:
        print(f"✅ {title}：无")
        return 0
    print(f"❌ {title}：{len(rows)} 处")
    for path, line, what in rows:
        print(f"   {path}:{line}  {what}")
    return 1


def guide_section_numbers():
    """试用指南每一篇的 H1 与小节号都必须与文件编号一致。

    2026-08-18 按「理解」重排时，13 篇改了文件名，**篇内小节号却没跟着改**：
    打开「09-多副本与优雅排空」，每个标题都写着 `## 19.1`。
    共 75 处标题 + 21 处引用错位，而当时三道检查一处都没报——

    ①② 抓不到它：`19.1` 在那个文件里**确实存在**，链接也没断。
    错的不是"指向不存在的地方"，而是"号本身不该是这个"。

    所以这里查的是一条不变式：**篇内小节号的前缀 == 文件名里的编号**。
    它便宜、机械，而且正好卡在改号最容易漏的那一步上。
    """
    problems = []
    seen_h1 = seen_sec = 0

    for path in sorted(glob.glob("试用指南/[0-9]*-*.md")):
        tag = os.path.basename(path).split("-")[0]      # "09" / "00a"
        prefixes = set()
        h1 = None
        for line in open(path, encoding="utf-8"):
            if h1 is None and line.startswith("# "):
                h1 = line.rstrip()
            m = re.match(r"#+ (\d+)\.\d", line)
            if m:
                prefixes.add(int(m.group(1)))

        # H1 形如 `# 09 · 多副本与优雅排空`
        m = re.match(r"# (\d+[a-z]?) · ", h1 or "")
        if m:
            seen_h1 += 1
            if m.group(1) != tag:
                problems.append((path, 1, f"文件号是 {tag}，H1 却写着 {m.group(1)}"))

        # 篇内小节号（00-准备、20-排障速查 用无编号小节，正常）
        for p in sorted(prefixes):
            seen_sec += 1
            if p != int(tag.rstrip("ab") or 0):
                problems.append((path, 0, f"文件号是 {tag}，小节却写着 {p}.x"))

    if seen_h1 == 0 or seen_sec == 0:
        print(f"❌ 扫到 H1 {seen_h1} 个、小节前缀 {seen_sec} 个——"
              "有一项是 0，多半是路径或正则不对，而不是文档真没编号。")
        sys.exit(2)
    return problems


def guide_prerequisites():
    """README 索引表的「前置」列，必须与各篇自己写的前置一致。

    2026-08-18 改号时漏了这一列：README 说 14 的前置是 12，而 14 自己写着
    「走完 13-完整装配」——12 正是 13 的**旧编号**。同样的错还有 15/16（←13）、
    18（←16）、11/12（←10）、09（←06），一共 7 处。

    ①②③ 三道检查全都抓不到它：这一列是**裸数字，不是链接**，
    既不是断链，也不是"指向不存在的小节"，更不是篇内小节号错位。
    改号脚本改的是链接标签，于是这一列原封不动地留在了旧编号上。

    这里查的不变式：**README 前置列里出现的每个篇号，都必须在那一篇自己的
    前置行里也出现**。方向是单向的——篇内可以写得更细（"05 做完；Docker 可用；
    镜像已构建"），但 README 不能凭空点名一篇那里根本没提的。
    """
    problems = []

    readme = "试用指南/README.md"
    if not os.path.exists(readme):
        return problems

    # 各篇自己声明的前置：文件号 → 那一行里出现的所有篇号
    declared = {}
    for path in sorted(glob.glob("试用指南/[0-9]*-*.md")):
        tag = os.path.basename(path).split("-")[0]
        head = "".join(open(path, encoding="utf-8").readlines()[:12])
        m = re.search(r"前置[：:]\**\s*(.+)", head)
        declared[tag] = set(re.findall(r"\d{2}", m.group(1))) if m else set()

    # README 四列索引表：| 14 | [链接](…) | 内容 | 前置 |
    rows = 0
    for i, line in enumerate(open(readme, encoding="utf-8"), 1):
        m = re.match(r"^\| (\d{2}[ab]?) \| \[[^]]+\]\([^)]+\) \| [^|]* \| ([^|]*)\|\s*$", line)
        if not m:
            continue
        tag, cell = m.group(1), m.group(2).strip()
        if tag not in declared:
            continue
        rows += 1
        for num in re.findall(r"\d{2}", cell):
            if num not in declared[tag]:
                problems.append((readme, i, f"索引表说 {tag} 的前置含 {num}，"
                                            f"但 {tag} 自己的前置行里没有它"))

    if rows == 0:
        print("❌ README 索引表一行都没解析出来——多半是表格列数或正则不对，"
              "而不是它真的空了。")
        sys.exit(2)
    return problems


# 篇尾那一行的样子：➡️ 下一节：[08-升级与多版本.md](…)
NEXT_MARK = re.compile(r"➡️ 下一节[：:]\s*\[([^\]]+)\]")


def guide_next_chain():
    """每一篇篇尾的「➡️ 下一节」必须指向**编号上的下一篇**。

    2026-08-18 改号时漏掉的第三处——前两处（篇内小节号、README 前置列）
    都已经有看守，而这一条链一直没人查，于是它整整保留了**旧编号时代的路线**：

        05 → 06 → 08 → 10 → 20 →（11）→ ⬅️ 20

    按旧号读一遍就明白：旧 06 是本地调试、旧 07 是 K8s、旧 09 是市场、
    旧 10 是排障速查。改号脚本把链接的**标签和文件名**都改对了，
    所以 ①②③④ 四道检查全绿——断链没有，编号没错位，前置也对得上。
    错的是**顺序本身**。

    代价是实打实的：跟着篇尾走的人在 20 那里到头，一共只看到 8 篇，
    13「完整装配」（八组件真实项目，00a 把它列进两小时主线）一次都到不了；
    而 10 → 20 → 11 → ⬅️ 20 还是个原地打转的环。

    不变式：**除末篇外，每一篇都要有 ➡️ 下一节，且第一个链接指向编号后继。**
    00a / 00b 是查阅篇（README 说明它们没有 ▶️ 操作），不在链上。
    """
    problems = []

    chapters = []
    for path in sorted(glob.glob("试用指南/[0-9]*-*.md")):
        tag = os.path.basename(path).split("-")[0]
        if not tag.isdigit():        # 00a / 00b 是查的，不是照着做的
            continue
        chapters.append((tag, path))

    if len(chapters) < 10:
        print(f"❌ 只扫到 {len(chapters)} 篇正文——多半是文件名规则变了，"
              "而不是指南真的只剩这么几篇。")
        sys.exit(2)

    checked = 0
    for i, (tag, path) in enumerate(chapters):
        text = open(path, encoding="utf-8").read()
        m = re.search(NEXT_MARK, text)
        last = i == len(chapters) - 1

        if last:
            if m:
                problems.append((path, 0, f"{tag} 是最后一篇，不该再有「➡️ 下一节」"))
            continue

        checked += 1
        want_tag, want_path = chapters[i + 1]
        if not m:
            problems.append((path, 0, f"{tag} 篇尾没有「➡️ 下一节」，"
                                      f"读者跟着走会在这里断掉（该指向 {want_tag}）"))
            continue
        got = m.group(1)
        if got != os.path.basename(want_path):
            problems.append((path, 0, f"{tag} 的下一节指向 {got}，"
                                      f"但编号后继是 {os.path.basename(want_path)}"))

    if checked == 0:
        print("❌ 一条「➡️ 下一节」都没比对——多半是标记文案变了。")
        sys.exit(2)
    return problems


def main():
    sections = design_sections()
    self_check(sections)
    print(f"✅ 自检通过（解析出 {sum(len(v) for v in sections.values())} 个小节）\n")

    failed = report("悬空小节引用", check_sections(sections))
    failed |= report("文档断链", check_links())
    failed |= report("指南编号错位", guide_section_numbers())
    failed |= report("指南前置对不上", guide_prerequisites())
    failed |= report("指南「下一节」链断开", guide_next_chain())

    if failed:
        print("\n引用坏掉的常见原因：重写某一节时改了编号、拆分文件时换了路径。")
        print("修的时候请指向**语义对得上**的那一节，而不是随便找一个存在的号。")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
