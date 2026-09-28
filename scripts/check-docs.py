#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""检查现行内容里的引用有没有指向不该指、或不存在的地方。

查三类：

  ① 归档引用      现行代码与文档里还指着已归档的旧设计书、旧决策、旧开发计划
                  （"005 §5.12"、"D140"、"Step 15"、"P38"、"附录 D"、"试用指南"……）
  ② 悬空规范引用  "提案 §6.2"、"附录 A24" 指向 new_plan/提案.md 里不存在的小节或决议
  ③ 断链          现行 markdown 的链接指向不存在的文件

# 为什么需要它

归档的东西会过时：旧设计书里的决定有的已经被推翻，读者顺着引用过去，
读到的是一个不再成立的理由。所以现行内容要么把理由写在原地，要么指向
现行规范（new_plan/提案.md，它的附录 A 优先于正文）。① 守住"不再指回去"，
② 守住"指向现行规范的地方真的存在"。

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
import urllib.parse

SPEC = "new_plan/提案.md"

# 这些目录本身就是历史或规划，不是"现行内容"。
EXCLUDED_PREFIXES = ("archive/", "docs/superpowers/", "new_plan/", ".superpowers/")
EXCLUDED_FILES = ("CHANGELOG.md", "scripts/check-docs.py")

# 指向归档内容的写法。每一种都在清理时真出现过。
ARCHIVED_REF = re.compile(
    r"(?<![\d.])0[01]\d ?§"    # 旧设计书小节：005 §5.12（前面可以紧挨字母，如 JSON 里的 \n002 §9.4）
    r"|design/0\d\d"           # 旧设计书路径
    r"|\bD\d{2,3}\b"           # 旧决策记录：D140
    r"|开发计划 ?\d"           # 旧开发计划条目
    r"|\bStep ?\d"             # 旧开发计划的 Step
    r"|\bP\d{2}\b"             # 旧完成记录里的延后项：P38（路线图阶段是 P1–P10，一位数）
    r"|附录 ?[B-G]\b|附录 [B-G]\."  # 旧设计书附录（现行规范只有附录 A）
    r"|试用指南|开发进度|延后项|延后清单"
    r"|回填 ?P\d|设计书 ?§|设计书 ?\d"
)

# 合法地用着相同字样、但不是归档引用的地方。每一条都要写清为什么。
ARCHIVED_REF_ALLOW = {
    # 脚本自己的进度输出："Step 1: Cleaning up …"，与开发计划无关
    "scripts/podman/fix-apparmor.sh": re.compile(r'echo ".*Step \d: '),
}

# 规范引用：提案 §6.2、提案 §9.3、§9.6、提案 §6.2–6.6
SPEC_REF = re.compile(r"提案\s*§\s*(\d+(?:\.\d+)*)((?:\s*[、，,–-]\s*§?\s*\d+(?:\.\d+)*)*)")
SPEC_REF_MORE = re.compile(r"\d+(?:\.\d+)*")
# 附录 A 决议：附录 A24、附录 A1、A16、附录 A4、A20、A24
APPENDIX_REF = re.compile(r"附录\s*A(\d+)((?:\s*[、，,/–-]\s*A\d+)*)")
APPENDIX_MORE = re.compile(r"A(\d+)")

CN_NUM = {"一": 1, "二": 2, "三": 3, "四": 4, "五": 5, "六": 6, "七": 7, "八": 8, "九": 9, "十": 10}


def cn_to_int(s):
    """十六 → 16、二十 → 20：规范的章用中文数字编号。"""
    if s == "十":
        return 10
    if s.startswith("十"):
        return 10 + CN_NUM[s[1]]
    if s.endswith("十"):
        return CN_NUM[s[0]] * 10
    if "十" in s:
        a, b = s.split("十")
        return CN_NUM[a] * 10 + CN_NUM[b]
    return CN_NUM[s]


def live_files():
    """现行内容：git 跟踪的文本文件，去掉历史与规划目录。"""
    out = subprocess.run(["git", "ls-files", "-z"], capture_output=True, check=True).stdout
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


def spec_anchors():
    """规范里真实存在的小节号与附录决议号。"""
    sections, decisions = set(), set()
    with open(SPEC, encoding="utf-8") as f:
        for line in f:
            m = re.match(r"#{2,6}\s+(\d+(?:\.\d+)*)\.?\s", line)
            if m:
                sections.add(m.group(1))
            m = re.match(r"#{2,6}\s+([一二三四五六七八九十]+)、", line)
            if m:
                sections.add(str(cn_to_int(m.group(1))))
            m = re.match(r"\|\s*A(\d+)\s*\|", line)
            if m:
                decisions.add(m.group(1))
    return sections, decisions


def self_check(sections, decisions):
    """确认每一类的解析本身没坏。坏了就直接退出——继续跑只会给出一个假的通过。"""
    problems = []
    # 规范解析：挑写法各不相同的几个——中文章号、两级、三级、附录决议首尾
    for sec in ("6", "16", "6.2", "8.9"):
        if sec not in sections:
            problems.append(f"解析不出规范里已知存在的 §{sec}")
    for dec in ("1", "24"):
        if dec not in decisions:
            problems.append(f"解析不出规范里已知存在的附录 A{dec}")
    # 归档引用：该抓的要抓到，现行规范引用与路线图阶段号不能误伤
    for sample in ("见 005 §5.12", "\\n\\n002 §9.4", "开发进度 D140", "Step 15-C", "延后项 P38", "附录 D.1", "试用指南 17"):
        if not ARCHIVED_REF.search(sample):
            problems.append(f"归档引用的正则漏掉了 {sample!r}")
    for sample in ("提案 §6.2", "附录 A24", "路线图 P7b", "HTTP/1.1"):
        if ARCHIVED_REF.search(sample):
            problems.append(f"归档引用的正则误伤了 {sample!r}")
    # 规范引用：列举与区间里的每个号都要取到
    got = spec_numbers("提案 §9.3、§9.6 与 提案 §6.2–6.6")
    if got != ["9.3", "9.6", "6.2", "6.6"]:
        problems.append(f"规范引用解析错了：{got}")
    got = appendix_numbers("附录 A1、A16，以及附录 A24")
    if got != ["1", "16", "24"]:
        problems.append(f"附录引用解析错了：{got}")
    # 断链：已知存在的要找得到，编造的要找不到
    if not link_exists("README.md", "CONTRIBUTING.md") or link_exists("README.md", "no-such-file.md"):
        problems.append("链接解析坏了")

    if problems:
        print("❌ 自检失败：")
        for p in problems:
            print(f"   {p}")
        print("   说明这个脚本的解析坏了，报出来的结果不可信。先修脚本。")
        sys.exit(2)


def spec_numbers(line):
    out = []
    for m in SPEC_REF.finditer(line):
        out.append(m.group(1))
        out.extend(SPEC_REF_MORE.findall(m.group(2)))
    return out


def appendix_numbers(line):
    out = []
    for m in APPENDIX_REF.finditer(line):
        out.append(m.group(1))
        out.extend(APPENDIX_MORE.findall(m.group(2)))
    return out


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


def check_spec_refs(files, sections, decisions):
    """② 悬空规范引用。引用父节是允许的：写 §8 而规范里只有 §8.1 / §8.2。"""
    bad = []
    for path in files:
        lines = read_lines(path)
        if lines is None:
            continue
        for i, line in enumerate(lines, 1):
            for sec in spec_numbers(line):
                if sec not in sections and not any(x.startswith(sec + ".") for x in sections):
                    bad.append((path, i, f"提案 §{sec}"))
            for dec in appendix_numbers(line):
                if dec not in decisions:
                    bad.append((path, i, f"附录 A{dec}"))
    return bad


def link_exists(path, href):
    target = urllib.parse.unquote(href.split("#")[0])
    return os.path.exists(os.path.normpath(os.path.join(os.path.dirname(path), target)))


def check_links(files):
    """③ 现行 markdown 的断链。"""
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
                if href.startswith(("http", "#", "mailto")):
                    continue
                if not href.split("#")[0]:
                    continue
                if not link_exists(path, href):
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


def main():
    sections, decisions = spec_anchors()
    self_check(sections, decisions)
    print(f"✅ 自检通过（规范里解析出 {len(sections)} 个小节、{len(decisions)} 条附录决议）\n")

    files = list(live_files())
    failed = report("归档引用", check_archived(files))
    failed |= report("悬空规范引用", check_spec_refs(files, sections, decisions))
    failed |= report("文档断链", check_links(files))

    if failed:
        print("\n归档引用：把理由写在原地，或改指现行规范（提案 §x / 附录 Ax）。")
        print("悬空引用与断链：请指向**语义对得上**的那一处，而不是随便找一个存在的号。")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
