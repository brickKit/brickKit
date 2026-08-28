#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""确认仓库里没有被提交进来的编译产物。

# 为什么需要它

`tests/components/infra-api-docs/infra-api-docs`（18 MB）与
`tests/components/auth-password-login/auth-password-login`（11 MB）在库里躺了很久：
两个未 strip、带 debug_info 的 ELF，占了 .git 的一多半。它们是有人在组件目录里
`go build` 之后随手 `git add -A` 进去的。

**标准的 Go .gitignore 挡不住这一类**：它只列了 `*.exe` / `*.so` / `*.test`，
而 Linux 上 `go build` 输出的是一个**与目录同名、没有后缀**的文件。
gitignore 表达不了"与所在目录同名"，所以只能一个个列——列漏一个就再来一次。

于是把判据换成"它是不是二进制"，而不是"它叫什么名字"。

# 判据

    可执行文件   看魔数：ELF / Mach-O / PE。源码仓库里不该有这些
    超大文件     超过 SIZE_LIMIT。仓库里最大的正常文件是 AI-CONTEXT.md（约 46 KB），
                 所以 1 MB 的门槛不会误伤，却能挡住 tar 包、数据集、录屏这一类

# 这个脚本自己会不会坏

会。最危险的坏法是"git ls-files 返回空 → 一个文件都没查 → 打印一个漂亮的 0 失败"。
所以拿不到文件清单是**硬失败**，结束时也报出实际检查了多少个文件。
"""

import os
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# 1 MB。仓库里最大的正常文件不到 50 KB，留出两个数量级的余量。
SIZE_LIMIT = 1024 * 1024

# 可执行文件的魔数。
MAGICS = [
    (b"\x7fELF", "ELF（Linux 可执行文件）"),
    (b"MZ", "PE（Windows 可执行文件）"),
    (b"\xfe\xed\xfa\xce", "Mach-O（macOS 可执行文件）"),
    (b"\xfe\xed\xfa\xcf", "Mach-O 64（macOS 可执行文件）"),
    (b"\xcf\xfa\xed\xfe", "Mach-O 64（macOS 可执行文件）"),
    (b"\xca\xfe\xba\xbe", "Mach-O fat（macOS 可执行文件）"),
]


def tracked_files():
    """git 追踪的全部文件。拿不到就硬失败——查了零个文件不是"全都干净"。"""
    try:
        out = subprocess.run(
            ["git", "ls-files", "-z"], cwd=ROOT,
            capture_output=True, check=True).stdout
    except (OSError, subprocess.CalledProcessError) as err:
        sys.exit(f"❌ 读不到 git 追踪的文件清单：{err}")

    names = [n.decode("utf-8") for n in out.split(b"\0") if n]
    if not names:
        sys.exit("❌ git ls-files 返回空——那不是「仓库很干净」，是根本没读到东西")
    return names


def classify(path):
    """返回这个文件为什么不该在仓库里；没问题时返回 None。"""
    try:
        size = os.path.getsize(path)
        with open(path, "rb") as f:
            head = f.read(4)
    except OSError:
        return None  # 读不动（软链、权限）就不管，那不是这个检查该操心的

    for magic, what in MAGICS:
        if head.startswith(magic):
            return f"{what}，{size / 1024 / 1024:.1f} MB"
    if size > SIZE_LIMIT:
        return f"超过 {SIZE_LIMIT // 1024 // 1024} MB（{size / 1024 / 1024:.1f} MB）"
    return None


def main():
    names = tracked_files()

    offenders = []
    checked = 0
    for name in names:
        path = os.path.join(ROOT, name)
        if not os.path.isfile(path):
            continue
        checked += 1
        if reason := classify(path):
            offenders.append((name, reason))

    if offenders:
        print(f"❌ 仓库里有不该提交的文件：{len(offenders)} 个\n")
        for name, reason in offenders:
            print(f"   {name}")
            print(f"     {reason}")
        print("\n   编译产物不该进源码仓库：每个 clone 都要永远付这份代价，")
        print("   而且它一旦进去，即使后来删掉，历史里那一份也还在。")
        print("\n   处理：git rm --cached <文件> && rm <文件>，并把它加进 .gitignore")
        return 1

    print(f"✅ 仓库里没有编译产物与超大文件（检查了 {checked} 个被追踪的文件）")
    return 0


if __name__ == "__main__":
    sys.exit(main())
