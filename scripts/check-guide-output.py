#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""核对试用指南里的「✅ 预期」块与 CLI 真实输出是否一致。

# 它和 check-guides.sh 有什么不同

`check-guides.sh` 查的是**关键词出现没有**（"输出里必须包含 `弱依赖缺失`"）。
它能证明"这一步跑得通"，但证明不了"指南上抄的那一屏就是它真会打印的那一屏"。

真撞到过：CLI 给弱依赖警告的 💡 那行补了后半句"；如需启用，请确认该组件已发布……"，
指南里还是旧的半句。关键词检查全绿，照着做的人对着屏幕多出来的半行发懵。

所以这里比的是**整块**：指南块里的每一行，都必须原样出现在真实输出里，且顺序一致。

# 比对规则：指南可以少写，不能写得不一样

    指南块的每一行  →  必须与真实输出中的某一行**逐字相等**（右侧空白忽略）
    行与行之间      →  必须保持顺序
    省略标记        →  单独一行的 `...` 或含「省略」的行，可跳过任意多行

方向是单向的：真实输出可以比指南多（警告、后续步骤），但指南**不能**出现
CLI 从没打印过的行。截断、错字、旧文案都会被这一条抓住。

# 为什么只覆盖 00–08 这几篇

只挑**不需要 Docker / minikube / 市场**的步骤：它们在任何机器上都跑得出确定结果，
所以能进 `make lint` 天天跑。要环境的篇交给 `make check-guides` 分层冒烟。
一个跑不动就会被加 `|| true` 的检查，等于没有检查。

# 这个脚本自己会不会坏

会。最危险的坏法是"锚点找不到 → 一条都没比 → 打印一个漂亮的 0 失败"。
所以锚点找不到是**硬失败**（exit 2），而不是跳过；结束时还会核对
真比过的块数与用例数一致。
"""

import os
import re
import shutil
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
GUIDE = os.path.join(ROOT, "试用指南")
BIN = os.path.join(ROOT, "bin", "brickkit")

# 试验场里的组件，与 试用指南/准备.sh 保持一致
COMPONENTS = [
    ("demo-hello", "demo/hello"),
    ("demo-caller", "demo/caller"),
    ("department-tree", "department/tree"),
    ("people-basic", "people/basic"),
    ("auth-password-login", "auth/password-login"),
    ("authorization-rbac", "authorization/rbac"),
    ("erp-backend", "erp/backend"),
    ("portal-user-frontend", "portal/user-frontend"),
    ("infra-redis-event-bus", "infra/redis-event-bus"),
    ("infra-api-docs", "infra/api-docs"),
]

# 回到「重置后五组件」基准状态的四条命令（试用指南 02 §2.5 末尾）
BASELINE = ["init demo-shop", "add demo/caller@1.0.0 --yes", "add people/basic@1.0.0 --yes"]

# 用例：先按 run 里的命令把状态推到位，再跑 check 里那条命令，与指南块比对。
#
#   reset  True 表示先把试验场推倒重来（组件源码也重新拷）
#   run    只执行、不比对的准备命令
#   check  (要比对的命令, 指南文件, 块锚点, 第几个匹配的块)
CASES = [
    {
        "what": "01 init 的输出",
        "reset": True,
        "run": [],
        "check": ("init demo-shop", "01-初始化项目.md", "✅ 项目已初始化：demo-shop", 0),
    },
    {
        "what": "02 add 递归拉依赖",
        "run": [],
        "check": ("add demo/caller@1.0.0", "02-添加与移除组件.md", "📦 添加 demo/caller@1.0.0", 0),
    },
    {
        "what": "02 add --local 一次装全",
        "run": [],
        "check": ("add --local", "02-添加与移除组件.md", "🔍 从本地安装源 local-dev 扫到 10 个组件", 0),
    },
    {
        "what": "03 order 的三段输出",
        "reset": True,
        "run": BASELINE,
        "check": ("order", "03-依赖与启动顺序.md", "📋 组件状态计算：", 0),
    },
    {
        "what": "04 级联禁用",
        "reset": True,
        "run": BASELINE + ["!disable demo/hello"],
        "check": ("order", "04-组件开启模式.md", "📋 组件状态计算：", 0),
    },
    {
        "what": "04 --only 一次性选中",
        "reset": True,
        "run": BASELINE,
        "check": ("up --only people/basic --dry-run", "04-组件开启模式.md",
                  "🎯 --only：只启动 people/basic 及其强依赖", 0),
    },
    {
        "what": "08 改版本号即升级",
        "reset": True,
        "run": BASELINE + ["!upgrade demo/hello"],
        "check": ("up --dry-run", "08-升级与多版本.md",
                  "⬆️ 检测到版本变更（升级流程，004 §3.5.1）：", 0),
    },
]


def fenced_blocks(path):
    """返回文件里所有 ``` 围栏块的内容（按出现顺序，每块是行列表）。"""
    blocks, cur, inside = [], None, False
    for line in open(path, encoding="utf-8"):
        line = line.rstrip("\n")
        if line.startswith("```"):
            if inside:
                blocks.append(cur)
                cur, inside = None, False
            else:
                cur, inside = [], True
            continue
        if inside:
            cur.append(line)
    return blocks


def find_block(filename, anchor, nth):
    """取出以 anchor 开头的第 nth 个围栏块。找不到是硬失败，不是跳过。"""
    path = os.path.join(GUIDE, filename)
    hits = [b for b in fenced_blocks(path) if b and b[0].rstrip() == anchor]
    if len(hits) <= nth:
        # 硬失败（2），与"内容对不上"（1）区分开：前者是脚本自己坏了/脱节了，
        # 后者是指南该改。两种都不能静默跳过——跳过和通过长得一模一样。
        print(f"❌ {filename} 里找不到以「{anchor}」开头的第 {nth + 1} 个块。")
        print("   指南改过而这个脚本没跟上——修锚点，别把用例删掉。")
        sys.exit(2)
    return hits[nth]


def is_ellipsis(line):
    t = line.strip()
    return t in ("...", "…") or "省略" in t


def compare(expected, actual):
    """指南块的每一行必须原样出现在真实输出里，且顺序一致。

    返回 None 表示一致；否则返回 (指南里的那一行, 说明)。
    """
    lines = [l.rstrip() for l in actual.splitlines()]
    i = 0
    for want in expected:
        want = want.rstrip()
        if not want or is_ellipsis(want):
            continue
        for j in range(i, len(lines)):
            if lines[j] == want:
                i = j + 1
                break
        else:
            # 找一行最像的，帮着看清是"整行没有"还是"只差几个字"
            near = ""
            key = want.strip()[:12]
            for l in lines:
                if key and key in l:
                    near = l
                    break
            hint = f"真实输出里最接近的一行：{near}" if near else "真实输出里没有任何相似的行"
            return want, hint
    return None


def prepare(work):
    """铺一个干净的试验场：组件源码就位，但**不** init（用例自己 init）。"""
    proj = os.path.join(work, "proj")
    if os.path.exists(proj):
        shutil.rmtree(proj)
    for src, dst in COMPONENTS:
        target = os.path.join(proj, "components", *dst.split("/"))
        os.makedirs(os.path.dirname(target), exist_ok=True)
        shutil.copytree(os.path.join(ROOT, "tests", "components", src), target)
    return proj


def disable(proj, component_id):
    """给某个组件加一行 enabled: false（04 §4.1 让读者手改的那一步）。"""
    path = os.path.join(proj, "brickkit.yaml")
    s = open(path, encoding="utf-8").read()
    old = f"  - id: {component_id}\n    version: 1.0.0\n"
    if old not in s:
        sys.exit(f"❌ 配置里找不到 {component_id}，无法加 enabled: false")
    open(path, "w", encoding="utf-8").write(s.replace(old, old + "    enabled: false\n", 1))


def upgrade(proj, component_id):
    """把组件源码与配置里的版本一起升到 2.0.0（08 §8.1 那两条 sed）。"""
    cy = os.path.join(proj, "components", *component_id.split("/"), "component.yaml")
    s = open(cy, encoding="utf-8").read()
    s = re.sub(r"(?m)^  version: 1\.0\.0$", "  version: 2.0.0", s)
    s = s.replace("brickkit-demo/hello:1.0.0", "brickkit-demo/hello:2.0.0")
    open(cy, "w", encoding="utf-8").write(s)

    path = os.path.join(proj, "brickkit.yaml")
    s = open(path, encoding="utf-8").read()
    open(path, "w", encoding="utf-8").write(
        s.replace(f"  - id: {component_id}\n    version: 1.0.0",
                  f"  - id: {component_id}\n    version: 2.0.0", 1))


def run_cli(proj, args):
    r = subprocess.run([BIN] + args.split(), cwd=proj, stdin=subprocess.DEVNULL,
                       capture_output=True, text=True)
    return r.stdout + r.stderr


def main():
    if not os.access(BIN, os.X_OK):
        sys.exit(f"❌ 找不到 {BIN}，先 make build-cli")

    work = tempfile.mkdtemp(prefix="brickkit-guide-")
    proj = None
    compared = 0
    problems = []

    try:
        for case in CASES:
            if case.get("reset") or proj is None:
                proj = prepare(work)
            for step in case["run"]:
                if step == "!disable demo/hello":
                    disable(proj, "demo/hello")
                elif step == "!upgrade demo/hello":
                    upgrade(proj, "demo/hello")
                else:
                    run_cli(proj, step)

            cmd, filename, anchor, nth = case["check"]
            expected = find_block(filename, anchor, nth)
            actual = run_cli(proj, cmd)
            compared += 1

            bad = compare(expected, actual)
            if bad:
                line, hint = bad
                problems.append((case["what"], filename, cmd, line, hint))
    finally:
        shutil.rmtree(work, ignore_errors=True)

    if compared != len(CASES):
        print(f"❌ 只比对了 {compared} 个块，用例有 {len(CASES)} 个——"
              "有用例被静默跳过了，这比失败更危险。")
        sys.exit(2)

    if problems:
        print(f"❌ 指南预期输出对不上：{len(problems)} 处")
        for what, filename, cmd, line, hint in problems:
            print(f"   试用指南/{filename}（{what}）")
            print(f"     命令：brickkit {cmd}")
            print(f"     指南里写着：{line}")
            print(f"     {hint}")
        print("\n指南里的「✅ 预期」是手抄的快照，CLI 文案一改它就过期。")
        print("请以**真实输出**为准改指南，而不是反过来。")
        sys.exit(1)

    print(f"✅ 指南预期输出：{compared} 个块逐行一致")


if __name__ == "__main__":
    main()
