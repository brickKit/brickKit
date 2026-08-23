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

# 分层：core 进 lint，docker 交给 check-guides

core 层只挑**不需要 Docker / minikube / 市场**的步骤：任何机器上都跑得出确定结果，
所以能进 `make lint` 天天跑。要环境的层缺环境时**响亮跳过**，并且不进 lint——
一个跑不动就会被加 `|| true` 的检查，等于没有检查。

# 账目必须是明的

结束时会报"指南里一共有多少个 CLI 输出块、看守了多少个"。
只说"N 个块一致"而不说分母，看起来和"全都守住了"一模一样——
而 05–19 那些要 Docker/k8s/市场/cosign 的篇，至今大半没有看守。

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

# 五组件基准状态。与 `试用指南/准备.sh --baseline` 做的是同一件事——
# 那个脚本也是 init + 这两条 add，两处必须保持一致（03–08 的前置就是它）。
BASELINE = ["init demo-shop", "add demo/caller@1.0.0 --yes", "add people/basic@1.0.0 --yes"]

# 用例：先按 run 里的命令把状态推到位，再跑 check 里那条命令，与指南块比对。
#
#   tier   core（默认，无需外部环境）| docker（要 Docker 与组件镜像）
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
        # 01 §1.3 教的是 reset（不是 reset --last）——那时 .last 还不存在。
        # 这条用例曾经不存在，于是指南里那句"reset --last 之后配置恢复原样"
        # 一直挂着：照着做的人会看到"备份文件不存在"，而配置根本没被恢复。
        "what": "01 §1.3 写坏配置后 reset 回初始状态",
        "reset": True,
        "run": ["init demo-shop", "!corrupt"],
        "check": ("reset", "01-初始化项目.md", "🔄 已恢复 brickkit.yaml 到初始状态", 0),
    },
    {
        "what": "01 §1.3 这时还没有 .last",
        "run": [],
        "check": ("reset --last", "01-初始化项目.md", "❌ 错误：备份文件不存在", 0),
    },
    # ↓ 02 按**指南自己的叙述顺序**连着跑：2.2 add → 2.3 add --local →
    #   2.4 关掉六个 → 2.5 sync → 2.6 remove。
    #
    #   早先这几条各自从干净状态出发，于是校验器走的是一条读者永远走不到的路：
    #   指南里 remove 排在 add --local 之前，而 remove 会删掉组件的源码目录，
    #   下一步 add --local 就少扫到一个组件、直接中止。校验器全绿，指南是断的。
    {
        "what": "02 §2.2 add 递归拉依赖",
        "reset": True,
        "run": ["init demo-shop"],
        "check": ("add demo/caller@1.0.0", "02-添加与移除组件.md", "📦 添加 demo/caller@1.0.0", 0),
    },
    {
        "what": "02 §2.3 add --local 一次装全",
        "run": [],
        "check": ("add --local", "02-添加与移除组件.md", "🔍 从本地安装源 local-dev 扫到 10 个组件", 0),
    },
    {
        "what": "02 §2.4 关掉六个之后算出来的状态",
        "run": ["!paste-components 02-添加与移除组件.md"],
        "check": ("order", "02-添加与移除组件.md", "📋 组件状态计算：", 0),
    },
    {
        "what": "02 §2.5 sync 把六个归档",
        "run": [],
        "check": ("sync", "02-添加与移除组件.md", "📂 工作区整理：", 0),
    },
    {
        "what": "02 §2.5 改回去再 sync 就激活",
        "run": ["!enable infra/redis-event-bus"],
        "check": ("sync", "02-添加与移除组件.md", "📂 工作区整理：", 1),
    },
    {
        "what": "02 §2.6 有依赖方时拦下来",
        "run": [],
        "check": ("remove people/basic@1.0.0", "02-添加与移除组件.md",
                  "❌ 无法移除 people/basic", 0),
    },
    {
        "what": "02 §2.6 移除根组件时连归档的那份一起删",
        "run": [],
        "check": ("remove infra/api-docs@1.0.0", "02-添加与移除组件.md",
                  "✅ 已移除 infra/api-docs@1.0.0", 0),
    },
    # ↓ 02a 从 02 的终点接着走：十个组件都在配置里，源码都在活跃目录。
    #   这两条守的是**开发时最常用的那个动作**——收拢与恢复必须成对可逆，
    #   任何一半的输出对不上，照着做的人就会以为自己的源码丢了。
    {
        "what": "02a §2a.2 sync --only 收拢到两个组件",
        "reset": True,
        "run": ["init demo-shop", "add --local --yes"],
        "check": ("sync --only people/basic", "02a-专注在几个组件上.md",
                  "🎯 --only：只保留 people/basic 及其强依赖（brickkit.yaml 未修改）", 0),
    },
    {
        "what": "02a §2a.4 不带参数的 sync 就是恢复",
        "run": [],
        # 02a 里第一个以「📂」开头的块就是恢复那一屏——收拢那一屏的首行是「🎯」
        "check": ("sync", "02a-专注在几个组件上.md", "📂 工作区整理：", 0),
    },
    {
        "what": "03 order 的三段输出",
        "reset": True,
        "run": BASELINE,
        "check": ("order", "03-依赖与启动顺序.md", "📋 组件状态计算：", 0),
    },
    {
        "what": "04 §4.1 什么都不写时算出来的状态",
        "reset": True,
        "run": BASELINE,
        "check": ("order", "04-组件开启模式.md", "📋 组件状态计算：", 0),
    },
    {
        "what": "04 §4.2 级联禁用",
        "reset": True,
        "run": BASELINE + ["!disable demo/hello"],
        "check": ("order", "04-组件开启模式.md", "📋 组件状态计算：", 1),
    },
    {
        "what": "04 §4.3 钉住撞上被禁用的强依赖",
        "reset": True,
        "run": BASELINE + ["!disable demo/hello", "!pin demo/caller"],
        "check": ("order", "04-组件开启模式.md", "❌ 错误：强依赖 demo/hello 被禁用", 0),
    },
    {
        "what": "04 --only 一次性选中",
        "reset": True,
        "run": BASELINE,
        "check": ("up --only people/basic --dry-run", "04-组件开启模式.md",
                  "🎯 --only：只启动 people/basic 及其强依赖", 0),
    },
    {
        "what": "08 §8.1 改版本号即升级",
        "reset": True,
        "run": BASELINE + ["!upgrade demo/hello"],
        "check": ("up --dry-run", "08-升级与多版本.md",
                  "⬆️ 检测到版本变更（升级流程，004 §3.5.1）：", 0),
    },
    # 8.2 / 8.6 各自重建状态，不能接着 8.1 的现场跑：
    # 升级的判据是"这个版本在本地有没有"，8.1 那次 --dry-run 已经把 2.0.0 拉进缓存了，
    # 再跑一次就不再算"版本变更"，升级摘要整段消失。
    {
        "what": "08 §8.2 升级不会自动改调用方",
        "reset": True,
        "run": BASELINE + ["!upgrade demo/hello"],
        "check": ("up --dry-run", "08-升级与多版本.md", "📋 组件状态计算：", 0),
    },
    {
        "what": "08 §8.6 升级变更摘要",
        "reset": True,
        "run": BASELINE + ["!upgrade demo/hello"],
        "check": ("up --dry-run", "08-升级与多版本.md", "📋 升级变更摘要：", 0),
    },
    {
        "what": "01 项目名不合法",
        "reset": True,
        "run": [],
        "check": ("init 试用", "01-初始化项目.md", "❌ 错误：项目名称不合法", 0),
    },
    {
        # 17 用的是仓库里的示例组件（试用指南/示例组件/notify/webhook），
        # 只走 add，不需要 Docker——所以它是 core 层，不是 docker 层。
        "what": "17 §17.4 把自己写的组件装进项目",
        "reset": True,
        "run": ["init my-project", "!use-sample notify/webhook"],
        "check": ("add notify/webhook@1.0.0", "17-开发自己的组件.md",
                  "📦 添加 notify/webhook@1.0.0", 0),
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


def pin(proj, component_id):
    """给某个组件加一行 enabled: true（04 §4.3 的"钉住"）。"""
    path = os.path.join(proj, "brickkit.yaml")
    s = open(path, encoding="utf-8").read()
    old_entry = f"  - id: {component_id}\n    version: 1.0.0\n"
    if old_entry not in s:
        print(f"❌ 配置里找不到 {component_id}，无法加 enabled: true")
        sys.exit(2)
    open(path, "w", encoding="utf-8").write(
        s.replace(old_entry, old_entry + "    enabled: true\n", 1))


def enable(proj, component_id):
    """把某个组件的 enabled: false 改成 true（02 §2.5 的"改回去再 sync"）。"""
    path = os.path.join(proj, "brickkit.yaml")
    s = open(path, encoding="utf-8").read()
    old = f"  - id: {component_id}\n    version: 1.0.0\n    enabled: false\n"
    if old not in s:
        sys.exit(f"❌ 配置里找不到 enabled: false 的 {component_id}")
    new = f"  - id: {component_id}\n    version: 1.0.0\n    enabled: true\n"
    open(path, "w", encoding="utf-8").write(s.replace(old, new, 1))


def corrupt(proj, _arg=None):
    """往配置末尾追加一行垃圾（01 §1.3 让读者手打的那一步）。"""
    path = os.path.join(proj, "brickkit.yaml")
    with open(path, "a", encoding="utf-8") as f:
        f.write("这是一行手滑写进去的垃圾\n")


def paste_components(proj, filename):
    """把指南里那段"整段替换 components:"的 YAML 真的贴进配置。

    这样被验证的就不只是输出，还有**读者要复制的那段配置本身**——
    贴进去之后算不出指南写的那一屏，就说明那段配置是错的。
    """
    path = os.path.join(GUIDE, filename)
    hits = [b for b in fenced_blocks(path)
            if b and b[0].rstrip() == "components:" and any("enabled: false" in l for l in b)]
    if not hits:
        print(f"❌ {filename} 里找不到那段带 enabled: false 的 components 配置。")
        sys.exit(2)
    block = "\n".join(hits[0]).rstrip() + "\n"

    cfg = os.path.join(proj, "brickkit.yaml")
    s = open(cfg, encoding="utf-8").read()
    start, end = s.index("components:"), s.index("resources:")
    open(cfg, "w", encoding="utf-8").write(s[:start] + block + "\n" + s[end:])


def use_sample(proj, component_id):
    """把仓库里的示例组件放进项目的 components/（17 让读者自己写的那个）。"""
    src = os.path.join(GUIDE, "示例组件", *component_id.split("/"))
    if not os.path.isdir(src):
        print(f"❌ 找不到示例组件 {src}——指南挪过位置而这个脚本没跟上。")
        sys.exit(2)
    dst = os.path.join(proj, "components", *component_id.split("/"))
    os.makedirs(os.path.dirname(dst), exist_ok=True)
    shutil.copytree(src, dst, dirs_exist_ok=True)


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


# 指南里所有 CLI 输出块的行首特征。用来算分母——只报分子不报分母，
# "守住了 15 个"看起来和"全都守住了"一模一样。
OUTPUT_MARKS = ("✅", "📦", "📋", "🔍", "⬆️", "🎯", "⚠️", "❌", "📁",
                "🗑️", "⏭️", "🔏", "📂", "ℹ️", "🚀", "🛑", "📊")


def total_output_blocks():
    """数一数指南里一共有多少个看起来是 CLI 输出的块。"""
    import glob
    n = 0
    for path in glob.glob(os.path.join(GUIDE, "[0-9]*-*.md")):
        for b in fenced_blocks(path):
            if b and b[0].startswith(OUTPUT_MARKS):
                n += 1
    return n


def docker_ready():
    """有 Docker，且两个玩具组件的镜像在本地。"""
    if subprocess.run(["docker", "info"], capture_output=True).returncode != 0:
        return False, "没有可用的 Docker"
    for image in ("brickkit-demo/hello:1.0.0", "brickkit-demo/caller:1.0.0"):
        if subprocess.run(["docker", "image", "inspect", image],
                          capture_output=True).returncode != 0:
            return False, f"缺组件镜像 {image}（见 试用指南/00-准备.md）"
    return True, ""


def main():
    if not os.access(BIN, os.X_OK):
        sys.exit(f"❌ 找不到 {BIN}，先 make build-cli")

    wanted = set(sys.argv[1:]) or {"core"}
    ok_docker, why = docker_ready() if "docker" in wanted else (False, "未选中该层")

    work = tempfile.mkdtemp(prefix="brickkit-guide-")
    proj = None
    compared = 0
    skipped = 0
    problems = []
    shown_skip = set()

    try:
        for case in CASES:
            tier = case.get("tier", "core")
            if tier not in wanted or (tier == "docker" and not ok_docker):
                skipped += 1
                if tier not in shown_skip:
                    reason = why if tier == "docker" and "docker" in wanted else "未选中该层"
                    print(f"⏭  跳过 {tier} 层：{reason}")
                    shown_skip.add(tier)
                continue
            if case.get("reset") or proj is None:
                proj = prepare(work)
            for step in case["run"]:
                if step.startswith("!disable "):
                    disable(proj, step.split(None, 1)[1])
                elif step.startswith("!pin "):
                    pin(proj, step.split(None, 1)[1])
                elif step.startswith("!upgrade "):
                    upgrade(proj, step.split(None, 1)[1])
                elif step.startswith("!paste-components "):
                    paste_components(proj, step.split(None, 1)[1])
                elif step.startswith("!use-sample "):
                    use_sample(proj, step.split(None, 1)[1])
                elif step.startswith("!enable "):
                    enable(proj, step.split(None, 1)[1])
                elif step == "!corrupt":
                    corrupt(proj)
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

    if compared + skipped != len(CASES):
        print(f"❌ 比对 {compared} + 跳过 {skipped} 与用例数 {len(CASES)} 对不上——"
              "有用例被静默漏掉了，这比失败更危险。")
        sys.exit(2)
    planned = sum(1 for c in CASES if c.get("tier", "core") in wanted)
    if planned == 0:
        print(f"⏭  {'、'.join(sorted(wanted))} 层暂无用例，什么都没比对。")
        sys.exit(0)
    if compared == 0:
        print("❌ 选中的层里一个块都没比对成——多半是环境探测或用例表有问题。")
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

    total = total_output_blocks()
    print(f"✅ 指南预期输出：{compared} 个块逐行一致"
          + (f"（另跳过 {skipped} 个用例）" if skipped else ""))
    print(f"   看守账目：指南里共 {total} 个 CLI 输出块，本次看守 {compared} 个。"
          f"其余多在 05–19——那些篇要 Docker / minikube / 市场 / cosign。")


if __name__ == "__main__":
    main()
