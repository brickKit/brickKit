#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""检查文档引用有没有指向不存在的地方。

查两类：

  ① 悬空小节引用    代码与文档里的 "005 §5.12" 指向设计书里不存在的小节
  ② 断链            markdown 链接指向不存在的文件

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


def main():
    sections = design_sections()
    self_check(sections)
    print(f"✅ 自检通过（解析出 {sum(len(v) for v in sections.values())} 个小节）\n")

    failed = report("悬空小节引用", check_sections(sections))
    failed |= report("文档断链", check_links())

    if failed:
        print("\n引用坏掉的常见原因：重写某一节时改了编号、拆分文件时换了路径。")
        print("修的时候请指向**语义对得上**的那一节，而不是随便找一个存在的号。")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
