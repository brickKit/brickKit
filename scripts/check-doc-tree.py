#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""检查文档里画的 `.brickkit/` 目录树，与 CLI 真的会创建的东西一致。

# 为什么需要它

这个仓库已经有三道守卫，覆盖的都是**能机械比对**的部分：

    check-docs          小节引用与文件链接指不指向真实存在的地方
    check-cli-docs      文档（和 --help）里的命令与参数存不存在
    check-guide-output  试用指南的「✅ 预期」与 CLI 真实输出逐行一致

漏在缝里的是**散文里的结构**——目录树就是最典型的一种。`brickkit reset`
连同整套备份机制删掉之后，四份文档的目录树里还画着 `.brickkit/backup/`，
其中一处（003）与本文件 §7.2「为什么这里没有 backup/」直接打架，
另一处（试用指南 01）是让读者刚 init 完对着核对的表——四行里有一行是假的。
三道守卫一条都没响：目录树既不是链接，也不是命令，更不是 CLI 输出。

# 判据从哪来

不写死名单。合法名字从 `internal/config/layout.go` 推导：凡是
`l.path(DirBrickkit, X)` 这种形状的方法，X 就是 `.brickkit/` 下的一项。
代码里加一类缓存目录，这里自动跟着认；删掉一类，文档里还画着就会被抓住。

# 这个脚本自己会不会坏

会。同类脚本在这个项目里坏过三次（先报出一堆假结果）。所以自检是它的一部分，
四条都过不了就直接退出，而不是继续跑出一个漂亮的 0：

  ① 常量解析出来的集合非空，且含 manifests / artifacts / generated
  ② 真跑一次 brickkit init，实际创建的目录必须都在解析出来的集合里
  ③ 文档里至少扫到一棵 `.brickkit/` 树（扫不到多半是树的画法变了）
  ④ 拿一棵含 backup/ 的假树喂进去，必须被判成不合法

第 ④ 条是关键：一个不会失败的检查等于没有检查。
"""

import glob
import os
import re
import shutil
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LAYOUT = os.path.join(ROOT, "internal", "config", "layout.go")

SKIP_DIRS = ("playground", "node_modules", ".tools", "bin", "data", ".git")

# 树里出现、但不是"CLI 创建的东西"的行，跳过而不是报错。
# 只有这一类：省略号。别的都该老实对上。
ELLIPSIS = ("...", "…", "省略")


def allowed_names():
    """从 layout.go 推导 `.brickkit/` 下的合法名字。

    两步：先把 const 块里的 `名字 = "值"` 收成表，再找出
    `l.path(DirBrickkit, X)` 里的 X，翻成它的字面值。

    返回 (合法名字集合, 翻不出来的参数)。第二项交给自检——一个翻不出来的
    参数意味着这里少认了一项 `.brickkit/` 下的东西，而少认的后果是
    **文档里正确的那一行被误报成错的**，比漏报更糟。
    """
    src = open(LAYOUT, encoding="utf-8").read()
    consts = dict(re.findall(r'(?m)^\s*(\w+)\s*=\s*"([^"]*)"', src))
    args = re.findall(r"l\.path\(DirBrickkit,\s*([^)]+)\)", src)

    names, unresolved = set(), []
    for arg in (a.strip() for a in args):
        if arg in consts:
            names.add(consts[arg])
        else:
            unresolved.append(arg)
    return names, unresolved


def real_init_entries(binary):
    """真跑一次 init，返回 `.brickkit/` 下实际出现的东西。"""
    work = tempfile.mkdtemp(prefix="brickkit-tree-")
    try:
        r = subprocess.run([binary, "init", "treecheck"], cwd=work,
                           capture_output=True, text=True)
        if r.returncode != 0:
            print("❌ 自检失败：brickkit init 跑不起来\n" + r.stdout + r.stderr)
            sys.exit(2)
        return set(os.listdir(os.path.join(work, ".brickkit")))
    finally:
        shutil.rmtree(work, ignore_errors=True)


def fenced_blocks(path):
    blocks, cur, inside = [], None, False
    for line in open(path, encoding="utf-8"):
        line = line.rstrip("\n")
        if line.startswith("```"):
            if inside:
                blocks.append(cur)
            cur, inside = [], not inside
            continue
        if inside:
            cur.append(line)
    return blocks


# 树的画法：缩进（空格 / 不换行空格 / 竖线）+ ├── 或 └── + 名字。
# 文档里混着普通空格与 U+00A0，所以按**字符列**数，不按空格数。
BRANCH = re.compile(r"[├└]──\s*")


def name_at(line):
    """返回 (名字起始列, 名字)。不是树的一行则返回 (None, None)。"""
    m = BRANCH.search(line)
    if not m:
        return None, None
    name = line[m.end():].strip()
    # 砍掉行尾注释：`manifests/         ← Manifest 缓存` / `# 说明`
    name = re.split(r"\s|←|#", name, maxsplit=1)[0]
    return m.end(), name


def children_of_brickkit(block):
    """在一个围栏块里，找出 `.brickkit/` 的**直接**子项。"""
    out = []
    for i, line in enumerate(block):
        col, name = name_at(line)
        if name is None or name.rstrip("/") != ".brickkit":
            continue

        child_col = None
        for sub in block[i + 1:]:
            c, n = name_at(sub)
            if n is None:
                continue
            if c <= col:          # 退回到同级或更外层，这棵子树结束
                break
            if child_col is None:
                child_col = c     # 第一个更深的就是"直接子项"那一列
            if c == child_col:
                out.append(n)
        # 一个块里可能画了不止一棵树，继续找
    return out


def scan(allowed):
    """返回 [(文件, 名字)]：文档里画了、而 CLI 不会创建的东西。"""
    bad, seen = [], 0
    for path in glob.glob(os.path.join(ROOT, "**", "*.md"), recursive=True):
        rel = os.path.relpath(path, ROOT)
        if any(part in SKIP_DIRS for part in rel.split(os.sep)):
            continue
        for block in fenced_blocks(path):
            for name in children_of_brickkit(block):
                if any(e in name for e in ELLIPSIS):
                    continue
                seen += 1
                if name.rstrip("/") not in allowed:
                    bad.append((rel, name))
    return bad, seen


# 自检 ④ 用的假树：含一个早已删除的 backup/。
FAKE_TREE = [
    "my-project/",
    "├── brickkit.yaml",
    "├── .brickkit/",
    "│   ├── backup/            ← 配置备份",
    "│   ├── manifests/",
    "└── components/",
]


def self_check(allowed, unresolved, binary):
    """确认这个脚本读代码、读文档的方式没坏。

    **刻意不写死任何目录名。** 从前这里断言过 manifests / artifacts /
    generated 必须解析得出来——那是第三份真相：把 DirArtifacts 的值改成别的
    （一次完全合法的重命名），自检会报"脚本坏了"，而脚本其实好好的。
    判据换成三条与名字无关的不变量。
    """
    problems = []
    if not allowed:
        problems.append("layout.go 里一项都没解析出来")
    for arg in unresolved:
        problems.append(f"l.path(DirBrickkit, {arg}) 里的参数翻不成字面值")

    real = real_init_entries(binary)
    for entry in real:
        if entry not in allowed:
            problems.append(f"init 真的创建了 {entry}，而解析结果里没有它")

    found = children_of_brickkit(FAKE_TREE)
    if "backup/" not in found:
        problems.append(f"假树里的 backup/ 没被认出来（认出来的是 {found}）")

    if problems:
        print("❌ 自检失败：" + "；".join(problems))
        print("   说明这个脚本读代码或读文档的方式坏了，报出来的结果不可信。先修脚本。")
        sys.exit(2)
    return real


def main():
    binary = sys.argv[1] if len(sys.argv) > 1 else os.path.join(ROOT, "bin", "brickkit")
    binary = os.path.abspath(binary)
    allowed, unresolved = allowed_names()
    real = self_check(allowed, unresolved, binary)
    print(f"✅ 自检通过（layout.go 认得 {len(allowed)} 项，init 实际创建 {len(real)} 项）\n")

    bad, seen = scan(allowed)
    if seen == 0:
        print("❌ 文档里一棵 .brickkit/ 目录树都没扫到——多半是树的画法变了，"
              "而不是文档里真的没有。")
        sys.exit(2)

    if bad:
        print(f"❌ 文档里画了 CLI 不会创建的东西：{len(bad)} 处")
        for rel, name in sorted(set(bad)):
            print(f"   {rel}  .brickkit/{name}")
        print(f"   → 合法的只有：{'、'.join(sorted(allowed))}")
        print("\n读者照着核对会发现对不上——而目录树既不是链接也不是命令，"
              "别的检查一条都不会响。")
        sys.exit(1)

    print(f"✅ .brickkit/ 目录树：{seen} 处子项全部对得上")


if __name__ == "__main__":
    main()
