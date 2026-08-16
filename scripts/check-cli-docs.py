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
        out = subprocess.run([binary] + args + ["--help"],
                             capture_output=True, text=True).stdout
        return set(re.findall(r"--[a-z][a-z-]*", out))

    root = subprocess.run([binary, "--help"], capture_output=True, text=True).stdout
    # 候选：分组列表里的 `  add   添加组件`，以及示例里的 `brickkit add ...`
    candidates = set(re.findall(r"^ {2,4}([a-z][a-z-]*) {2,}\S", root, re.M))
    candidates |= set(re.findall(r"brickkit ([a-z][a-z-]*)", root))

    surface = {"": flags([])}
    for name in sorted(candidates):
        if probe(name):
            surface[name] = flags([name]) | surface[""]
    return surface


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


def docs():
    for pattern in ["design/**/*.md", "试用指南/**/*.md", "*.md", "deploy/**/*.md"]:
        for path in glob.glob(pattern, recursive=True):
            if any(d in path for d in SKIP_DIRS):
                continue
            yield path


def check(surface):
    bad_cmd, bad_flag = [], []
    seen_cmd = seen_flag = 0

    for path in docs():
        try:
            lines = open(path, encoding="utf-8").read().split("\n")
        except (OSError, UnicodeDecodeError):
            continue

        for i, line in enumerate(lines, 1):
            for m in re.finditer(r"brickkit ([a-z][a-z-]*)((?: [^\s`|]+)*)", line):
                cmd, rest = m.group(1), m.group(2)
                seen_cmd += 1

                if cmd not in surface:
                    bad_cmd.append((path, i, cmd))
                    continue

                for flag in re.findall(r"(?<![\w-])--[a-z][a-z-]*", rest):
                    seen_flag += 1
                    if flag in PLACEHOLDERS or flag in surface[cmd]:
                        continue
                    bad_flag.append((path, i, f"{cmd} {flag}"))

    return bad_cmd, bad_flag, seen_cmd, seen_flag


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

    surface = cli_surface(binary)
    self_check(surface)
    print(f"✅ 自检通过（解析出 {len(surface) - 1} 个命令）\n")

    bad_cmd, bad_flag, seen_cmd, seen_flag = check(surface)

    # 扫到 0 处问题和根本没扫到东西，输出长得一模一样。把数目报出来，
    # 一份"检查通过"才有意义。
    if seen_cmd == 0:
        print("❌ 一处 brickkit 命令用法都没扫到——多半是文档路径或正则不对，"
              "而不是文档里真的没有命令。")
        sys.exit(2)
    print(f"   （检查了 {seen_cmd} 处命令用法、{seen_flag} 处参数）\n")

    failed = report("文档写了不存在的命令", bad_cmd, "命令被改名或删掉了，文档没跟着改")
    failed |= report("文档写了不存在的参数", bad_flag, "参数被改名或删掉了，文档没跟着改")

    if failed:
        print("\n照着文档敲一遍会得到 unknown flag/command——"
              "而使用者多半会以为是自己装错了版本。")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
