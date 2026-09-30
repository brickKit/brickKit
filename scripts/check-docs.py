#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""检查现行内容里的引用有没有指向不该指、或不存在的地方。

查三类：

  ① 归档引用      现行代码与文档里还指着已归档的设计书、决策、开发计划与三层重构提案
                  （"005 §5.12"、"D140"、"Step 15"、"P38"、"附录 D"、"试用指南"、
                  "提案 §6.2"、"附录 A24"、"命令表 6"……）
  ② 悬空小节引用  每个 "§" 都要写明是哪份现行文档的小节——"AGENTS.md §9.12"、
                  "RFC 8725 §3.5"——并且那一节真的存在；
                  没写文档名的 "§5.10" 是无主引用（多半指向归档的旧设计书）
  ③ 断链          现行 markdown 的链接指向不存在的文件
  ④ 悬空文档路径  代码、脚本、报错建议里以纯文本写的文档路径
                  （"docs/en/11-reference/01-component-yaml-schema.md"）指向不存在的文件
  ⑤ 入口文件的网页前缀  AGENTS 两份、llms 两份只用相对路径，网页上的前缀只在开头说一次
                  （本地开发的 AI 要的是能直接打开的路径；每条都写全网址还白费 token）

# 为什么需要它

归档的东西会过时：旧设计书里的决定有的已经被推翻，读者顺着引用过去，
读到的是一个不再成立的理由。所以现行内容把理由写在原地；三层重构的提案
（archive/ 下）也一样——它描述的是当时的打算，现行的是代码与 docs/。
① 守住"不再指回去"，② 守住"指向现行文档的地方真的存在"。

这些错误的共同点是**写的时候是对的，之后才坏掉**：重写一节时改了编号、
拆分文件时换了路径，而引用方没人记得跟着改。它们不会让任何测试失败——
直到某天有人顺着引用过去，发现那里什么都没有。

# 这个脚本自己会不会坏

会。同类脚本写的时候错过好几次：正则截断得不对、标题写法没认全、
只找了一种引用写法。症状都是"报出一堆假的缺口"或"报 0 处"，而两者看起来都像成功。

所以**自检是这个脚本的一部分**（见 self_check）：它先拿已知的样例验证
每一类的解析都没坏，坏了就直接失败，而不是继续跑出一个漂亮的 0。
"""

import os
import re
import subprocess
import sys
import unicodedata
import urllib.parse

# 这些目录本身就是历史或规划，不是"现行内容"。
EXCLUDED_PREFIXES = ("archive/", "docs/superpowers/", ".superpowers/", "llms/")
EXCLUDED_FILES = ("CHANGELOG.md", "scripts/check-docs.py")

# 指向归档内容的写法。每一种都在清理时真出现过。
# 能写窄就写窄：P9 要写的现行文档里会自然出现 "Step 1:"、"P99"、"延后"这类字样。
CJK_AFTER = r"(?=\s*[\u4e00-\u9fff（「、，。)）」])"
ARCHIVED_REF = re.compile(
    r"(?<![\d.])0[01]\d ?§"                     # 旧设计书小节：005 §5.12（也认 JSON 里紧挨 \n 的）
    r"|(?<![\d.\w-])0(?:0[1-9]|1[0-2])(?=[ ]?[\u4e00-\u9fff（])"  # 旧设计书编号（001–012）：004 未规定
    r"|design/0\d\d"                            # 旧设计书路径
    r"|\bD\d{2,3}\b"                            # 旧决策记录：D140
    r"|开发计划"                                 # 旧开发计划
    r"|\bStep ?\d+(?:[-–][\dA-Z]+)?" + CJK_AFTER +  # 旧开发计划的 Step：Step 15-C、Step 12 在……
    r"|\bStep ?\d+[-–][\dA-Z]+"                  # Step 15-C、Step 32–35
    r"|\bP(?:1[1-9]|[2-8]\d)\b" + CJK_AFTER +     # 旧完成记录的延后项 P38（路线图阶段是 P1–P10）
    r"|附录 ?[B-G]\b|附录 [B-G]\."                # 旧设计书附录
    r"|提案 ?§|附录 ?A\d|命令表 ?\d|new_plan/"      # 三层重构的提案、它的附录 A 决议与命令表（已归档）
    # 规划记录（docs/superpowers/ 的规格与实施计划）迟早归档：现行内容只能关联 docs/{en,zh}/，
    # 不写它的路径，也不写它里面的编号——实施计划的 Task N、审查的 Review Focus N / Final review #N
    r"|docs/superpowers/[\w-]|\bReview Focus\b|Final review #\d|\bTask \d+(?:[-–]\d+)?\b"
    r"|试用指南|《开发进度》|开发进度 ?[A-Z]?\d|延后项 ?P\d|延后清单"
    r"|回填 ?P\d|设计书 ?§|设计书 ?\d"
    r"|《发布与分发》|运维指南|《组件合并部署》|Release and Distribution|gap report"
    r"|\bSpec 20\d\d-\d\d-\d\d"                   # 旧 spec
    # 开发计划条目号：注释开头的 // 15.13：、// 15.13 停止，以及断言消息开头的 "36.1：
    r"|^\s*(?://|#|--)\s*\d{1,2}\.\d{1,2}(?:\s*[/、–-]\s*[\d.]+)*(?:\s*[：:]|\s+[\u4e00-\u9fff])"
    r"|\"\d{1,2}\.\d{1,2}(?:\s*[/、–-]\s*[\d.]+)*\s*[：:]"
)

# 合法地用着相同字样、但不是归档引用的地方。每一条都要写清为什么。
ARCHIVED_REF_ALLOW = {
    # 脚本自己的进度输出："Step 1: Cleaning up …"，与开发计划无关
    "scripts/podman/fix-apparmor.sh": re.compile(r'echo ".*Step \d: '),
    # 守卫本身：它举例说明、并列出"帮助文本里不许出现"的写法，必须把那些字样写出来
    "internal/i18n/removed_concepts_test.go": re.compile(r'帮助文本里写"（附录 A4）"|designDocCitations'),
}

# 小节引用：一行里按顺序出现的"文档名"与"§ 编号"。每个 § 归到它前面最近的文档名：
#   AGENTS.md §3.2、§3.3   两个都归 AGENTS.md
#   AGENTS.md §3.1–3.4     区间两端都要存在
# 逗号后面的普通数字（"AGENTS.md §3.2, 2026-09-28"）不是编号：只认紧跟在 § 后面的。
# 已归档的提案也认作文档名：它的 § 由①报成归档引用，这里不再重复报一次"无主"。
SECTION_TOKEN = re.compile(
    r"(?P<owner>提案|附录\s*A\d+|AGENTS\.zh(?:\.md)?|AGENTS(?:\.md)?|RFC\s*\d+)"
    r"|(?P<heading>#+\s*)?§\s*(?P<sec>\d+(?:\.\d+)*)(?:\s*–\s*(?P<to>\d+(?:\.\d+)*))?")

# 这些文件里没写文档名的 § 是自己的小节，不算无主。
SELF_SECTIONED = ("docs/", "tutorials/")
SELF_SECTIONED_FILES = ("AGENTS.md", "AGENTS.zh.md", "README.md", "README.zh.md",
                       "llms.txt", "llms.zh.txt", "CONTRIBUTING.md", "CONTRIBUTING.zh.md")

# 纯文本里的文档路径：docs/en/…、docs/zh/…、docs/{en,zh}/…、docs/{zh,en}/…，tutorials 同理。
DOC_PATH = re.compile(r"\b((?:docs|tutorials)/(?:en|zh|\{en,zh\}|\{zh,en\})/[\w./-]+?\.md)\b")

def live_files():
    """现行内容：仓库里的文本文件（已跟踪的，加上还没 git add、但没被忽略的新文件），
    去掉历史与规划目录。新写的一页还没暂存时也要被检查到——否则写文档的人会被
    "那一页不存在"之类的报错误导。"""
    out = subprocess.run(["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
                         capture_output=True, check=True).stdout
    for raw in out.split(b"\0"):
        if not raw:
            continue
        path = raw.decode("utf-8")
        if path.startswith(EXCLUDED_PREFIXES) or path in EXCLUDED_FILES:
            continue
        if not os.path.isfile(path):
            continue
        yield path


def read_lines(path):
    try:
        with open(path, encoding="utf-8") as f:
            return f.read().split("\n")
    except (OSError, UnicodeDecodeError):
        return None


def heading_numbers(lines):
    """markdown 里带编号的小节：标题（## 4.、### 9.24）与加粗编号的条目（**9.12 Why …**）。"""
    out = set()
    for line in lines:
        m = re.match(r"#{2,6}\s+§?(\d+(?:\.\d+)*)\.?\s", line) or re.match(r"\*\*(\d+(?:\.\d+)+)\.?\s", line)
        if m:
            out.add(m.group(1))
    return out


def owner_kind(token):
    """文档名 → 校验时用哪份文档的小节表。"""
    if token.startswith(("提案", "附录")):
        return "archived"
    if token.startswith("AGENTS.zh"):
        return "AGENTS.zh.md"
    if token.startswith("AGENTS"):
        return "AGENTS.md"
    return "external"


def section_refs(line):
    """[(文档, 编号)]；没写文档名的 § 文档记为 None。"""
    out, owner = [], None
    for m in SECTION_TOKEN.finditer(line):
        if m.group("owner"):
            owner = owner_kind(m.group("owner"))
            continue
        if m.group("heading"):  # "## §2 …" 是标题本身（比如测试里的 markdown 样例），不是引用
            continue
        out.append((owner, m.group("sec")))
        if m.group("to"):
            out.append((owner, m.group("to")))
    return out


def self_check():
    """确认每一类的解析本身没坏。坏了就直接退出——继续跑只会给出一个假的通过。"""
    problems = []
    # 归档引用：该抓的要抓到，现行规范引用与路线图阶段号不能误伤
    for sample in ("见 005 §5.12", "\\n\\n002 §9.4", "004 未规定具体数值", "开发进度 D140", "Step 15-C",
                   "Step 12 在命令层", "Step 32–35", "延后项 P38", "附录 D.1", "试用指南 17", "《发布与分发》§5",
                   "运维指南 §5.1", "gap report §2.1", "Spec 2026-09-19 §3.1", "开发计划 §0.2",
                   '"36.1：并发', "// 15.13 停止：", "\t// 16.14：清理旧 Job",
                   "提案 §6.2", "（附录 A24）", "命令表 6", "new_plan/提案.md",
                   "见 docs/superpowers/plans/x.md", "（Review Focus 4）", "（Final review #2）", "Task 1-4 的回落规则",
                   "Task 5：容器"):
        if not ARCHIVED_REF.search(sample):
            problems.append(f"归档引用的正则漏掉了 {sample!r}")
    for sample in ("AGENTS.md §3.2", "路线图 P7b", "a task runner", "Tasks: 3", '("archive/", "docs/superpowers/")', "路线图 P10 的多语言", "HTTP/1.1",
                   "Step 1: create a project", "P99 latency", "这件事延后了", "0.3–0.5 秒", "版本 1.1.0：",
                   "chmod 000 挡不住读取"):
        if ARCHIVED_REF.search(sample):
            problems.append(f"归档引用的正则误伤了 {sample!r}")
    # 小节引用：每个 § 归到前面最近的文档名；逗号后面的普通数字不算编号
    got = section_refs("AGENTS.md §3.2、§3.3 与 AGENTS.md §3.1–3.4, 2026-09-28；提案 §6.2")
    if got != [("AGENTS.md", n) for n in ("3.2", "3.3", "3.1", "3.4")] + [("archived", "6.2")]:
        problems.append(f"小节引用解析错了：{got}")
    got = section_refs('见 §5.10；AGENTS.md §9.12、AGENTS.zh.md §4；RFC 8725 §3.5；"## §2 Core"')
    if got != [(None, "5.10"), ("AGENTS.md", "9.12"), ("AGENTS.zh.md", "4"), ("external", "3.5")]:
        problems.append(f"小节引用的归属错了：{got}")
    got = heading_numbers(["## 4. Twelve principles", "### 9.24 Summary", "**9.12 Why not …**", "**Bold** text",
                           "## §2 核心设计原则（十条）", "### 3.1 文件检索地图"])
    if got != {"4", "9.24", "9.12", "2", "3.1"}:
        problems.append(f"小节编号解析错了：{got}")
    # 链接里的 #锚点：按 GitHub 的规则从标题算出来（中文保留、标点去掉、重名加 -1）
    sample = ["# 标题", "## 三层架构（核心）", "## `brickkit add <id>[@ver]`", "## 三层架构（核心）",
              "```", "## 代码块里的不是标题", "```", '<a id="custom-anchor"></a>', "## Mode: `debug` & Local"]
    got = anchors_of(sample)
    want = {"标题", "三层架构核心", "brickkit-add-idver", "三层架构核心-1", "custom-anchor", "mode-debug--local"}
    if got != want:
        problems.append(f"锚点算错了：多了 {sorted(got - want)}，少了 {sorted(want - got)}")
    # 文档路径：两种写法都要取到，{en,zh} 要展开成两份
    got = doc_paths("见 docs/{en,zh}/11-reference/06-market-api.md 与 docs/zh/x/y.md（英文版把 zh 换成 en）")
    if got != ["docs/en/11-reference/06-market-api.md", "docs/zh/11-reference/06-market-api.md", "docs/zh/x/y.md"]:
        problems.append(f"文档路径解析错了：{got}")
    # 断链：已知存在的要找得到，编造的要找不到
    if not link_exists("README.md", "CONTRIBUTING.md") or link_exists("README.md", "no-such-file.md"):
        problems.append("链接解析坏了")

    if (RAW_PREFIX + "a " + RAW_PREFIX + "b").count(RAW_PREFIX) != 2:
        problems.append("网页前缀的计数坏了")

    if problems:
        print("❌ 自检失败：")
        for p in problems:
            print(f"   {p}")
        print("   说明这个脚本的解析坏了，报出来的结果不可信。先修脚本。")
        sys.exit(2)


def doc_paths(line):
    out = []
    for m in DOC_PATH.finditer(line):
        path = m.group(1)
        if "{" in path:
            out += [path.replace(re.search(r"\{[^}]*\}", path).group(0), lang) for lang in ("en", "zh")]
        else:
            out.append(path)
    return out


def check_doc_paths(files):
    """④ 代码、脚本、报错建议里以纯文本写的文档路径必须真实存在。

    markdown 链接由 ③ 管；这里管的是**不是链接**的路径——报错建议里的
    "完整字段参考见 docs/zh/…"、脚本里的 "Next step: read docs/en/…"。
    它们不会被任何链接检查看到，而读者正是照着它们去找文档的。
    """
    bad = []
    for path in files:
        lines = read_lines(path)
        if lines is None:
            continue
        for i, line in enumerate(lines, 1):
            for doc in doc_paths(line):
                if not os.path.isfile(doc):
                    bad.append((path, i, doc))
    return bad


def check_archived(files):
    """① 归档引用。"""
    bad = []
    for path in files:
        lines = read_lines(path)
        if lines is None:
            continue
        allow = ARCHIVED_REF_ALLOW.get(path)
        for i, line in enumerate(lines, 1):
            m = ARCHIVED_REF.search(line)
            if m and not (allow and allow.search(line)):
                bad.append((path, i, f"{m.group(0)!r}  {line.strip()[:80]}"))
    return bad


def check_section_refs(files):
    """② 悬空 / 无主的小节引用。引用父节是允许的：写 §3 而文档里只有 §3.1 / §3.2。"""
    anchors = {"AGENTS.md": heading_numbers(read_lines("AGENTS.md") or []),
               "AGENTS.zh.md": heading_numbers(read_lines("AGENTS.zh.md") or [])}
    bad = []
    for path in files:
        lines = read_lines(path)
        if lines is None:
            continue
        self_sectioned = path.startswith(SELF_SECTIONED) or path in SELF_SECTIONED_FILES
        for i, line in enumerate(lines, 1):
            for doc, sec in section_refs(line):
                if doc is None:
                    if not self_sectioned:
                        bad.append((path, i, f"§{sec} 没写是哪份文档的小节"))
                    continue
                if doc in ("external", "archived"):  # 归档的由①报
                    continue
                known = anchors[doc]
                if sec not in known and not any(x.startswith(sec + ".") for x in known):
                    bad.append((path, i, f"{doc} §{sec} 不存在"))
    return bad


def github_slug(text):
    """GitHub 给标题生成锚点的规则：去掉 markdown 记号，转小写，只留字母（含中文）、
    数字、下划线、连字符和空格，再把空格换成连字符。"""
    text = re.sub(r"!?\[([^\]]*)\]\([^)]*\)", r"\1", text)  # 链接与图片只留文字
    text = text.lower()
    kept = "".join(c for c in text
                   if unicodedata.category(c)[0] in "LMN" or c in "-_ ")
    return kept.replace(" ", "-")


def anchors_of(lines):
    """一份 markdown 里所有可以链接的锚点：每个标题（重名的依次加 -1、-2），
    以及手写的 <a id="…"> / <a name="…">。代码块里的 # 不是标题。"""
    out, seen, fenced = set(), {}, False
    for line in lines:
        if line.lstrip().startswith("```"):
            fenced = not fenced
            continue
        if fenced:
            continue
        for m in re.finditer(r'<a\s+(?:id|name)="([^"]+)"', line):
            out.add(m.group(1))
        m = re.match(r"^(#{1,6})\s+(.*?)\s*#*\s*$", line)
        if not m:
            continue
        slug = github_slug(m.group(2))
        n = seen.get(slug, 0)
        seen[slug] = n + 1
        out.add(slug if n == 0 else f"{slug}-{n}")
    return out


_anchor_cache = {}


def anchor_exists(path, fragment):
    if path not in _anchor_cache:
        _anchor_cache[path] = anchors_of(read_lines(path) or [])
    return urllib.parse.unquote(fragment).lower() in _anchor_cache[path]


def link_exists(path, href):
    target = urllib.parse.unquote(href.split("#")[0])
    return os.path.exists(os.path.normpath(os.path.join(os.path.dirname(path), target)))


# 手写的入口文件只用相对路径；网页上的前缀只在开头说一次（本地的 AI 要的是能直接打开的路径）
RAW_PREFIX = "https://raw.githubusercontent.com/brickKit/brickKit/main/"
RAW_ONCE_FILES = ("AGENTS.md", "AGENTS.zh.md", "llms.txt", "llms.zh.txt")


def check_raw_prefix_once():
    """⑤ 入口文件里网页前缀只出现一次。"""
    bad = []
    for path in RAW_ONCE_FILES:
        n = sum(line.count(RAW_PREFIX) for line in read_lines(path) or [])
        if n != 1:
            bad.append((path, 0, f"网页前缀出现了 {n} 次，应当只在开头说明一次、其余一律写相对路径"))
    return bad


def check_links(files):
    """③ 现行 markdown 的断链：目标文件不存在，或 #锚点 在目标文件里不存在。"""
    bad = []
    for path in files:
        if not path.endswith(".md"):
            continue
        lines = read_lines(path)
        if lines is None:
            continue
        for i, line in enumerate(lines, 1):
            # 行内代码不是链接：`[a-z0-9]([a-z0-9-]*[a-z0-9])?` 这类正则文本长得像 [文字](目标)
            line = re.sub(r"`[^`]*`", "", line)
            for _, href in re.findall(r"\[([^\]]*)\]\(([^)\s]+)\)", line):
                if href.startswith(("http", "mailto")):
                    continue
                target, _, fragment = href.partition("#")
                if target:
                    if not link_exists(path, target):
                        bad.append((path, i, href))
                        continue
                    target_path = os.path.normpath(os.path.join(os.path.dirname(path), urllib.parse.unquote(target)))
                else:
                    target_path = path
                # 只有 markdown 才有标题锚点；目录、图片等不查
                if fragment and target_path.endswith(".md") and not anchor_exists(target_path, fragment):
                    bad.append((path, i, f"{href}（锚点不存在）"))
    return bad


def report(title, rows):
    if not rows:
        print(f"✅ {title}：无")
        return 0
    print(f"❌ {title}：{len(rows)} 处")
    for path, line, what in rows:
        print(f"   {path}:{line}  {what}")
    return 1


def main():
    self_check()
    print("✅ 自检通过\n")

    files = list(live_files())
    failed = report("归档引用", check_archived(files))
    failed |= report("悬空 / 无主的小节引用", check_section_refs(files))
    failed |= report("文档断链", check_links(files))
    failed |= report("悬空文档路径", check_doc_paths(files))
    failed |= report("入口文件的网页前缀", check_raw_prefix_once())

    if failed:
        print("\n归档引用：把理由写在原地（旧设计书、三层重构的提案与附录都已归档，不再指回去）。")
        print("悬空引用与断链：请指向**语义对得上**的那一处，而不是随便找一个存在的号。")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
