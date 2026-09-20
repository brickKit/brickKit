#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""核对现行教程（docs/{en,zh}/03-guide/）里抄下来的 CLI 输出块与真实输出是否一致。

# 这是对旧版本的重写，不是修补

旧版本只核对 `docs/archive/` 里的归档试用指南与设计书——那批文档已经明确
"历史记录，不再要求跟 CLI 保持同步"（brickKit 反馈：不引用已归档文档）。
继续拿它们当活契约，等于逼着每一次改 CLI 文案都要回头去改一份声明"不维护"
的文档，自相矛盾，所以旧版本被整个撤下 `make lint`。

而现行的 `docs/{en,zh}/03-guide/`（13 篇，取代了归档的 23 篇）从来没有被这种
逐行核对覆盖过——它们同样在文中嵌了大量真实 CLI 输出的围栏块，只是没人
守着这些块会不会悄悄过期。这个脚本就是补上这个缺口，机制基本照抄旧版本
（找锚点、逐行比对、省略号跳过任意行数、硬失败而不是静默跳过），换的只是
目标文档树。

# 顺带守一件旧版本从没做过的事：中英文两侧是不是抄的同一份输出

`docs/en/` 与 `docs/zh/` 是两棵独立撰写的文档树（各自成篇，不是逐句互译，
但内容常常平行对应——这是这个仓库实测出来的实际写作习惯，不是刻意要求），
而 CLI 打印的文本本身是中文，不受读者语言影响——所以同一个场景下，两边嵌
的输出块**必须逐字相同**。`check-docs-bilingual.py` 只查两棵树的文件是不是
成对存在，从不看文件内容；这里核对英文那份是否命中真实输出的同时，顺手也
核对中文那份的对应块跟英文那份是否一字不差——一边改了措辞、另一边没跟上，
这里会先发现。

# 只挑不需要 Docker / minikube / 市场 / cosign 的场景

13 篇里能在任何机器上确定性构造的，只是其中一部分：01（部分）、02、03
（部分）、05（部分）、06（部分）、07（部分，含契约先行一节）、08（几乎全部）——04 要 minikube、
09 要市场、10 要 cosign、12 要支持执行策略的 CNI，11/13 的核心内容也要 Docker
真的把容器跑起来。这跟旧版本的分层哲学一致：能确定性构造的进 `make lint`
天天跑，要真实环境的留给人工（或者以后配一个 docker 层的 CI job，见文末的账目）。

第 8 篇（组件源码）是个例外的"几乎全部"：它讲的 add --repo / sync / remove /
restore / 提交钩子都不启动容器，只需要 git。所以它的场景用本地裸仓库当"远端"
（!make-remotes），用真实的 git 命令（!git）造出未提交、未推送、归档、
submodule 这些状态，再拿真实 CLI 的输出与教程逐行比对。

一篇文章里能被抄的输出块通常不止这些——一个场景一旦需要真的启动容器、
真的连数据库、真的等 K8s 探针，这个脚本就没法在任何机器上确定性地跑，只能
放弃，不是漏掉。

# 比对规则（与旧版本相同）：文档可以少写，不能写得不一样

    文档块里的每一行  →  必须与真实输出中的某一行**逐字相等**（右侧空白忽略）
    行与行之间       →  必须保持顺序
    省略标记         →  单独一行的 `...`，可以跳过任意多行

方向是单向的：真实输出可以比文档多（后续步骤、额外提示），文档**不能**
出现 CLI 从没打印过的行。

# 这个脚本自己会不会坏

会。最危险的坏法是"锚点找不到 → 一条都没比 → 打印一个漂亮的 0 失败"。
所以锚点找不到是**硬失败**（exit 2），而不是跳过；结束时还会核对真比过的
块数与用例数一致，并且报出"这两棵树一共有多少个看起来像 CLI 输出的块，
这次覆盖了多少个"——只报分子不报分母，看起来会跟"全都守住了"一模一样。
"""

import glob
import os
import shlex
import shutil
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
EN_GUIDE = os.path.join(ROOT, "docs", "en", "03-guide")
ZH_GUIDE = os.path.join(ROOT, "docs", "zh", "03-guide")
BIN = os.path.join(ROOT, "bin", "brickkit")

# 用例里用到的组件，来自 tests/components/，与教程正文引用的路径一致。
COMPONENTS = [
    ("demo-hello", "demo/hello"),
    ("demo-caller", "demo/caller"),
]

# 用例：先按 run 里的命令把状态推到位，再跑 check 里那条命令，与教程块比对。
#
#   reset    True 表示先把试验场推倒重来（组件源码也重新拷）
#   run      只执行、不比对的准备命令
#   file     教程文件名（如 "01-first-project.md"），docs/en 与 docs/zh 下同名
#   check    (要比对的命令, 锚点, 第几个匹配的块)
CASES = [
    {
        "what": "01 init 的输出",
        "reset": True,
        "run": [],
        "file": "01-first-project.md",
        "check": ("init hello-world", "✅ 项目已初始化：hello-world", 0),
    },
    {
        "what": "01 单组件 add --local",
        "run": ["!copy-into components/demo/hello demo-hello"],
        "file": "01-first-project.md",
        "check": ("add --local", "🔍 从本地安装源 local-dev 扫到 1 个组件", 0),
    },
    {
        "what": "01 dry-run 的三段输出",
        "run": [],
        "file": "01-first-project.md",
        "check": ("up --dry-run", "🚀 启动项目 hello-world（deploy.target: docker）", 0),
    },
    {
        "what": "02 两组件 add --local，弱依赖缺失警告",
        "reset": True,
        "run": ["init hello-world --no-skills",
                "!copy-into components/demo/hello demo-hello",
                "!copy-into components/demo/caller demo-caller"],
        "file": "02-what-runs.md",
        "check": ("add --local", "🔍 从本地安装源 local-dev 扫到 2 个组件", 0),
    },
    {
        "what": "02 完整 dry-run 画面",
        "run": [],
        "file": "02-what-runs.md",
        "check": ("up --dry-run", "⚠️ 警告：弱依赖缺失：demo/bus@1.0.0", 0),
    },
    {
        "what": "02 关掉强依赖，依赖方跟着不跑",
        "run": ["!disable demo/hello"],
        "file": "02-what-runs.md",
        "check": ("up --dry-run", "📋 组件状态计算：", 0),
    },
    {
        "what": "02 钉住撞上被禁用的强依赖",
        "run": ["!pin demo/caller"],
        "file": "02-what-runs.md",
        "check": ("up --dry-run", "❌ 错误：强依赖 demo/hello 被禁用", 0),
    },
    {
        "what": "03 local: true 的 dry-run 画面",
        "reset": True,
        "run": ["init hello-world --no-skills",
                "!copy-into components/demo/hello demo-hello",
                "!copy-into components/demo/caller demo-caller",
                "add --local",
                "!local-debug demo/hello 8080"],
        "file": "03-local-debugging.md",
        "check": ("up --dry-run", "📋 组件状态计算：", 0),
    },
    {
        "what": "05 存在多个版本时 remove 报歧义",
        "reset": True,
        "run": ["init hello-world --no-skills",
                "!copy-into components/demo/hello demo-hello",
                "!add-second-version demo/hello demo-hello"],
        "file": "05-upgrades-and-versions.md",
        "check": ("remove demo/hello", "❌ demo/hello 存在多个版本（2.0.0, 1.0.0），请指定版本：", 0),
    },
    {
        "what": "05 指定版本后 remove 成功",
        "run": [],
        "file": "05-upgrades-and-versions.md",
        "check": ("remove demo/hello@1.0.0", "✅ 已移除 demo/hello@1.0.0", 0),
    },
    {
        "what": "06 资源 host 看起来像服务名的警告",
        "reset": True,
        "run": ["init hello-world --no-skills",
                "!copy-into components/demo/hello demo-hello",
                "!copy-into components/demo/caller demo-caller",
                "add --local",
                "!bind-resource caller-db guide-pg"],
        "file": "06-assemble-and-break.md",
        "check": ("up --dry-run", "⚠️ 基础资源的 host 看起来是个服务名，容器里可能解析不了", 0),
    },
    {
        "what": "07 fetch 只下产物、不动配置",
        "reset": True,
        "run": ["init hello-world --no-skills", "!copy-into components/demo/hello demo-hello"],
        "file": "07-consuming-artifacts.md",
        "check": ("fetch demo/hello@1.0.0", "📦 已下载 demo/hello@1.0.0 的产物（未写入 brickkit.yaml）", 0),
    },
    # 07 的"上游还没好"一节：桩（new --contract）→ add --local → 桩标 local: true。
    # 只用现有命令，不需要 Docker；桩之后在主机上起什么 mock 工具不归平台管，
    # 也就没有可比对的 brickkit 输出。
    {
        "what": "07 上游还没好：new 出桩",
        "reset": True,
        "run": ["init hello-world --no-skills", "!copy-into components/demo/caller demo-caller"],
        "file": "07-consuming-artifacts.md",
        "check": ("new demo/hello --contract openapi", "✅ 已生成组件骨架：demo/hello", 0),
    },
    # 桩的版本号没改（骨架默认 0.1.0），而消费方要的是精确的 1.0.0：add --local 整个中止、
    # brickkit.yaml 不动，所以不影响下一个场景的起点。这是这条配方里最容易踩的坑，
    # 教程里贴了这段报错，就得有人守着它。
    {
        "what": "07 忘了改桩的版本号：add --local 被挡住",
        "run": [],
        "file": "07-consuming-artifacts.md",
        "check": ("add --local", "❌ 错误：强依赖缺失", 0),
    },
    {
        "what": "07 桩与消费方一起 add --local",
        "run": ["!set-version components/demo/hello 1.0.0"],
        "file": "07-consuming-artifacts.md",
        "check": ("add --local", "🔍 从本地安装源 local-dev 扫到 2 个组件", 0),
    },
    {
        "what": "07 桩接成 local 之后的 dry-run",
        "run": ["!local-debug demo/hello 18081"],
        "file": "07-consuming-artifacts.md",
        "check": ("up --dry-run", "🚀 启动项目 hello-world（deploy.target: docker）", 0),
    },
    # ---- 08 组件源码：只需要 git，不需要 Docker ----
    # 这一组按教程的行文顺序连着跑（同一个项目里一路推进），中间夹着的 !git / !append
    # 步骤对应教程里那些不是 brickkit 输出的命令（改源码、提交、推送）。
    {
        "what": "08 add 默认不克隆源码",
        "reset": True,
        "run": ["!make-remotes", "init workspace-demo --no-skills", "!git-sources"],
        "file": "08-component-source.md",
        "check": ("add demo/caller@1.0.0", "📦 添加 demo/caller@1.0.0", 0),
    },
    {
        "what": "08 --repo 克隆一个已在配置里的组件",
        "run": [],
        "file": "08-component-source.md",
        "check": ("add demo/hello@1.0.0 --repo --yes",
                  "ℹ️ demo/hello@1.0.0 已存在于 brickkit.yaml，--yes 已指定：直接刷新缓存", 0),
    },
    {
        "what": "08 克隆过再 --repo：说源码已经在了，而不是没有 Git 地址",
        "run": [],
        "file": "08-component-source.md",
        "check": ("add demo/hello@1.0.0 --repo --yes",
                  "ℹ️ demo/hello@1.0.0 已存在于 brickkit.yaml，--yes 已指定：直接刷新缓存", 1),
    },
    {
        "what": "08 --repo-all：已有源码的跳过并说理由",
        "run": [],
        "file": "08-component-source.md",
        "check": ("add demo/caller@1.0.0 --repo-all --yes",
                  "ℹ️ demo/caller@1.0.0 已存在于 brickkit.yaml，--yes 已指定：直接刷新缓存", 0),
    },
    {
        "what": "08 关掉 caller，hello 也跟着不启动",
        # 教程「改了源码，怎么推回去」那一节的两条路，顺手走完，让后面的状态与教程一致
        "run": ["!git components/demo/hello checkout -q -b feature/greeting",
                "!append components/demo/hello/main.go // 换一句问候",
                '!git components/demo/hello commit -q -am "调整问候语"',
                "!git components/demo/hello push -q origin feature/greeting",
                "!git components/demo/hello remote add myfork {work}/remotes/hello-fork.git",
                "!git components/demo/hello push -q myfork feature/greeting",
                "!disable demo/caller"],
        "file": "08-component-source.md",
        "check": ("up --dry-run", "📋 组件状态计算：", 0),
    },
    {
        "what": "08 sync 把两个的源码都收进归档",
        "run": [],
        "file": "08-component-source.md",
        "check": ("sync", "📂 工作区整理：", 0),
    },
    {
        "what": "08 钉住 hello 之后的判定",
        "run": ["!pin demo/hello"],
        "file": "08-component-source.md",
        "check": ("up --dry-run", "📋 组件状态计算：", 1),
    },
    {
        "what": "08 sync 把 hello 搬回来",
        "run": [],
        "file": "08-component-source.md",
        "check": ("sync", "📂 工作区整理：", 1),
    },
    {
        "what": "08 删掉 enabled，sync 把 caller 也搬回来",
        "run": ["!clear-enabled"],
        "file": "08-component-source.md",
        "check": ("sync", "📂 工作区整理：", 2),
    },
    {
        "what": "08 remove 被依赖方挡住",
        "run": [],
        "file": "08-component-source.md",
        "check": ("remove demo/hello", "❌ 无法移除 demo/hello", 0),
    },
    {
        "what": "08 remove 拦下未提交的改动",
        "run": ["!append components/demo/caller/main.go // 我正在改这里"],
        "file": "08-component-source.md",
        "check": ("remove demo/caller", "❌ 错误：源码删掉就找不回来了", 0),
    },
    {
        "what": "08 remove 拦下没推的提交",
        "run": ['!git components/demo/caller commit -q -am "wip: 调整 caller"'],
        "file": "08-component-source.md",
        "check": ("remove demo/caller", "❌ 错误：源码删掉就找不回来了", 1),
    },
    {
        "what": "08 推上去之后 remove 放行",
        "run": ["!git components/demo/caller push -q origin main"],
        "file": "08-component-source.md",
        "check": ("remove demo/caller", "✅ 已移除 demo/caller@1.0.0", 0),
    },
    {
        "what": "08 remove 连归档里的那份源码一起删",
        "run": ["!disable demo/hello", "sync"],
        "file": "08-component-source.md",
        "check": ("remove demo/hello", "✅ 已移除 demo/hello@1.0.0", 0),
    },
    {
        "what": "08 项目根就是仓库根：init 顺手装上钩子",
        "reset": True,
        "run": ["!git-init"],
        "file": "08-component-source.md",
        "check": ("init shared-src", "✅ 项目已初始化：shared-src", 0),
    },
    {
        "what": "08 只提交归档的源码：钩子拦下",
        "run": ["!drop-components-ignore",
                "!copy-into components/demo/hello demo-hello",
                "!copy-into components/demo/caller demo-caller",
                "add --local",
                "!git . add -A",
                "!git . commit -q -m 初始",
                "!pin demo/hello", "!disable demo/caller", "sync",
                "!append components/demo/hello/main.go // 调整 hello 的问候",
                "!git . add components/"],
        "file": "08-component-source.md",
        "check": ('!git . commit -m "调整 hello 的问候"',
                  "❌ 提交被拦下：组件源码提交在归档目录里，但 brickkit.yaml 说它该启动", 0),
    },
    {
        "what": "08 restore 还原 enabled，源码结构跟着走",
        "run": ["!git . reset -q components/"],
        "file": "08-component-source.md",
        "check": ("restore", "📄 brickkit.yaml：按最后一次提交还原 enabled（其余改动未动）", 0),
    },
    {
        "what": "08 submodule 挡住 remove（--force 也不放行的那一道）",
        "reset": True,
        "run": ["!make-remotes", "!git-init", "init sub-demo --no-skills", "!drop-components-ignore",
                "!git . -c protocol.file.allow=always submodule add -q ../remotes/hello.git components/demo/hello",
                "add --local", "!git . add -A", "!git . commit -q -m 挂上子模块"],
        "file": "08-component-source.md",
        "check": ("remove demo/hello", "❌ 错误：无法删除组件源码——它是一个已登记的 git submodule", 0),
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


def find_block(path, anchor, nth):
    """取出以 anchor 开头的第 nth 个围栏块。找不到是硬失败，不是跳过。"""
    hits = [b for b in fenced_blocks(path) if b and b[0].rstrip() == anchor]
    if len(hits) <= nth:
        rel = os.path.relpath(path, ROOT)
        print(f"❌ {rel} 里找不到以「{anchor}」开头的第 {nth + 1} 个块。")
        print("   文档改过而这个脚本没跟上——修锚点，别把用例删掉。")
        sys.exit(2)
    return hits[nth]


def is_ellipsis(line):
    return line.strip() in ("...", "…")


def compare(expected, actual):
    """教程块里的每一行必须原样出现在真实输出里，且顺序一致。

    返回 None 表示一致；否则返回 (文档里的那一行, 说明)。
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
    """铺一个干净的试验场：只建目录，组件源码由 !copy-into 按用例需要拷贝。

    "远端"（!make-remotes 造的裸仓库）与项目目录同级，也一起推倒：推送会改
    它们，留着会让下一个场景带着上一个场景的提交。
    """
    proj = os.path.join(work, "proj")
    for stale in (proj, os.path.join(work, "remotes")):
        if os.path.exists(stale):
            shutil.rmtree(stale)
    os.makedirs(proj)
    return proj


FIXTURE_BY_SLUG = dict(COMPONENTS)


def copy_into(proj, dst_rel, slug):
    """把 tests/components/<slug> 整个拷进 <proj>/<dst_rel>（02 §2.2 一类的 setup）。"""
    src = os.path.join(ROOT, "tests", "components", slug)
    dst = os.path.join(proj, dst_rel)
    os.makedirs(os.path.dirname(dst), exist_ok=True)
    shutil.copytree(src, dst)


def set_version(proj, dir_rel, version):
    """把 <proj>/<dir_rel>/component.yaml 里的 version 改掉（brickkit new 出来的骨架默认是 0.1.0）。"""
    path = os.path.join(proj, dir_rel, "component.yaml")
    s = open(path, encoding="utf-8").read()
    old = "  version: 0.1.0\n"
    if old not in s:
        sys.exit(f"❌ {path} 里找不到 version: 0.1.0，无法改成 {version}")
    open(path, "w", encoding="utf-8").write(s.replace(old, f"  version: {version}\n", 1))


def disable(proj, component_id):
    """给某个组件加一行 enabled: false。"""
    path = os.path.join(proj, "brickkit.yaml")
    s = open(path, encoding="utf-8").read()
    old = f"  - id: {component_id}\n    version: 1.0.0\n"
    if old not in s:
        sys.exit(f"❌ 配置里找不到 {component_id}，无法加 enabled: false")
    open(path, "w", encoding="utf-8").write(s.replace(old, old + "    enabled: false\n", 1))


def pin(proj, component_id):
    """给某个组件加一行 enabled: true。"""
    path = os.path.join(proj, "brickkit.yaml")
    s = open(path, encoding="utf-8").read()
    old = f"  - id: {component_id}\n    version: 1.0.0\n"
    if old not in s:
        sys.exit(f"❌ 配置里找不到 {component_id}，无法加 enabled: true")
    open(path, "w", encoding="utf-8").write(s.replace(old, old + "    enabled: true\n", 1))


def local_debug(proj, component_id, port):
    """把某个组件改成 local: true（03 的场景：只调试，不 expose）。"""
    path = os.path.join(proj, "brickkit.yaml")
    s = open(path, encoding="utf-8").read()
    old = f"  - id: {component_id}\n    version: 1.0.0\n"
    if old not in s:
        sys.exit(f"❌ 配置里找不到 {component_id}，无法改成 local: true")
    extra = f"    local: true\n    localPort: {port}\n"
    open(path, "w", encoding="utf-8").write(s.replace(old, old + extra, 1))


def add_second_version(proj, component_id, slug):
    """装出"同一个 ID 两个版本共存"的局面（05 的场景），顺序照抄文档：

    先把 components/ 里那份原地升到 2.0.0（对应文档里"原地升级"那一节留下的
    状态），add --local 一次；再另建一个 local 安装源指向一份没动过的 1.0.0
    源码副本，显式 add 那个精确版本。两次都是真实的 add，不是手改
    brickkit.yaml 伪造出来的状态——brickkit.yaml 里 2.0.0 在前、1.0.0 在后，
    与文档展示的顺序一致，因为 add 是按声明顺序追加的。
    """
    scope, name = component_id.split("/")
    main_manifest = os.path.join(proj, "components", scope, name, "component.yaml")
    s = open(main_manifest, encoding="utf-8").read()
    s = s.replace("version: 1.0.0", "version: 2.0.0", 1)
    open(main_manifest, "w", encoding="utf-8").write(s)
    run_cli(proj, "add --local --yes")

    v1_dir = os.path.join(proj, "components-v1", scope, name)
    shutil.copytree(os.path.join(ROOT, "tests", "components", slug), v1_dir)

    cfg = os.path.join(proj, "brickkit.yaml")
    s = open(cfg, encoding="utf-8").read()
    old = "  - id: local-dev\n    type: local\n    path: ./components # brickkit init 已经建好这个目录\n"
    if old not in s:
        sys.exit("❌ brickkit.yaml 里的 sources 段不是预期的样子，add_second_version 需要更新")
    new = old + "  - id: local-dev-v1\n    type: local\n    path: ./components-v1\n"
    open(cfg, "w", encoding="utf-8").write(s.replace(old, new, 1))
    run_cli(proj, f"add {component_id}@1.0.0 --yes")


# git 一律不读使用者自己的全局配置：别人的 alias、core.hooksPath、提交签名
# 都会让同一个场景在不同机器上跑出不同的结果。身份用固定值，提交才不会因为
# "没配 user.name"失败。
GIT_ENV = {
    "GIT_CONFIG_GLOBAL": "/dev/null",
    "GIT_CONFIG_SYSTEM": "/dev/null",
    "GIT_AUTHOR_NAME": "demo", "GIT_AUTHOR_EMAIL": "demo@example.com",
    "GIT_COMMITTER_NAME": "demo", "GIT_COMMITTER_EMAIL": "demo@example.com",
    "GIT_TERMINAL_PROMPT": "0",
}

# 13 篇教程里嵌的真实输出快照全部是中文——这在 CLI 本身还没有语言设置时
# 天然成立，不需要显式声明。CLI 多语言支持（docs/superpowers/specs/
# 2026-09-20-cli-i18n-design.md）引入 BRICKKIT_LANG 之后，CLI 的默认语言
# 变成了英文，这里就必须显式钉住中文，否则真实输出会跟着跑这个脚本的
# 环境（或跑测试的人自己机器上的全局配置）里设了什么语言而变，快照比对
# 就成了追一个移动的目标。放在字典最后一位覆盖 os.environ，保证跟谁的
# 机器、谁的环境变量都没关系。等子项目 3 把某些场景改成"docs/en 配真实
# 英文输出"之后，再给那些场景单独传 env={"BRICKKIT_LANG": "en"} 覆盖它。
CLI_ENV_DEFAULTS = {
    "BRICKKIT_LANG": "zh",
}


def run_git(cwd, args):
    """跑一条 git，返回 stdout+stderr（不管成败——失败的输出正是有时要比对的东西）。

    env 里也带 CLI_ENV_DEFAULTS：git commit 可能触发 pre-commit hook，
    hook 脚本会调用 brickkit（比如 restore --check），继承的是这个
    子进程的环境——不带上就是同一个"追着系统默认语言跑"的问题，只是
    换了一条从 hook 而不是直接调用触发的路径。
    """
    r = subprocess.run(["git"] + args, cwd=cwd, stdin=subprocess.DEVNULL,
                       capture_output=True, text=True, env={**os.environ, **GIT_ENV, **CLI_ENV_DEFAULTS})
    return r.stdout + r.stderr


def git_must(cwd, args):
    """准备阶段的 git：失败就直接退出，别让一个坏掉的前置悄悄变成后面的假失败。"""
    r = subprocess.run(["git"] + args, cwd=cwd, stdin=subprocess.DEVNULL,
                       capture_output=True, text=True, env={**os.environ, **GIT_ENV, **CLI_ENV_DEFAULTS})
    if r.returncode != 0:
        sys.exit(f"❌ 准备阶段 git {' '.join(args)} 失败（在 {cwd}）：{r.stderr.strip()}")


def make_remotes(work):
    """13 的舞台：两个夹具组件各做成一个裸仓库（work/remotes/<name>.git），外加
    一个空的 hello-fork.git。与教程「准备」那一节的命令一一对应。"""
    remotes = os.path.join(work, "remotes")
    os.makedirs(remotes, exist_ok=True)
    for name in ("hello", "caller"):
        seed = os.path.join(work, f"seed-{name}")
        shutil.copytree(os.path.join(ROOT, "tests", "components", f"demo-{name}"), seed)
        git_must(seed, ["init", "-q", "-b", "main"])
        git_must(seed, ["add", "."])
        git_must(seed, ["commit", "-q", "-m", f"demo/{name} 1.0.0"])
        git_must(work, ["clone", "-q", "--bare", seed, os.path.join(remotes, f"{name}.git")])
        shutil.rmtree(seed)
    git_must(work, ["init", "-q", "--bare", "-b", "main", os.path.join(remotes, "hello-fork.git")])


def git_sources(proj):
    """把两个 git 源追加在 init 生成的 local-dev 后面（13 的「准备」那一节）。"""
    cfg = os.path.join(proj, "brickkit.yaml")
    s = open(cfg, encoding="utf-8").read()
    old = "    path: ./components # brickkit init 已经建好这个目录\n"
    if old not in s:
        # init 的骨架里注释是对齐过的；对不上时用更宽松的锚点，再不行就报错
        m = "    path: ./components      # brickkit init 已经建好这个目录\n"
        if m not in s:
            sys.exit("❌ brickkit.yaml 里的 local-dev 段不是预期的样子，git_sources 需要更新")
        old = m
    add = ("  - id: hello-remote\n    type: git\n    url: ../remotes/hello.git\n"
           "  - id: caller-remote\n    type: git\n    url: ../remotes/caller.git\n")
    open(cfg, "w", encoding="utf-8").write(s.replace(old, old + add, 1))


def clear_enabled(proj):
    """删掉 brickkit.yaml 里所有 enabled: 行——回到"不写"，也就是跟着上层走。"""
    cfg = os.path.join(proj, "brickkit.yaml")
    lines = open(cfg, encoding="utf-8").read().split("\n")
    kept = [l for l in lines if not l.strip().startswith("enabled:")]
    open(cfg, "w", encoding="utf-8").write("\n".join(kept))


def append_line(proj, rel, text):
    """往文件末尾追加一行（教程里的 `echo '...' >> file`）。"""
    with open(os.path.join(proj, rel), "a", encoding="utf-8") as f:
        f.write(text + "\n")


def drop_components_ignore(proj):
    """把 init 追加进 .gitignore 的 components/ 规则删掉——"源码跟着项目一起提交"。"""
    path = os.path.join(proj, ".gitignore")
    s = open(path, encoding="utf-8").read()
    rule = "# 组件源码目录（每个组件是独立的 Git 仓库，不提交到项目仓库）\ncomponents/\n\n"
    if rule not in s:
        sys.exit("❌ .gitignore 里找不到 components/ 那段规则，drop_components_ignore 需要更新")
    open(path, "w", encoding="utf-8").write(s.replace(rule, "", 1))


def git_step(proj, work, spec):
    """`!git <相对项目的目录> <git 参数……>`：参数按 shell 规则切分，{work} 换成试验场根。"""
    directory, rest = spec.split(None, 1)
    args = [a.replace("{work}", work) for a in shlex.split(rest)]
    return run_git(os.path.join(proj, directory), args)


def bind_resource(proj, resource_id, host):
    """给项目加一条数据库资源声明，host 写成一个看起来像服务名的值（06 的场景）。

    只用来触发生成阶段的"host 看起来是个服务名"警告——不需要这个资源真的
    连得上，dry-run 不会真的去连。
    """
    cfg = os.path.join(proj, "brickkit.yaml")
    s = open(cfg, encoding="utf-8").read()
    if "resources: []" not in s:
        sys.exit("❌ brickkit.yaml 里没有找到 resources: []，bind_resource 需要更新")
    block = (
        f"resources:\n"
        f"  - kind: database\n"
        f"    engine: postgresql\n"
        f"    id: {resource_id}\n"
        f"    host: {host}\n"
        f"    port: 5432\n"
        f"    username: postgres\n"
        f"    password: ${{DB_PASSWORD}}\n"
        f"    bindings:\n"
        f"      - componentId: demo/caller\n"
        f"        database: callerdb\n"
    )
    open(cfg, "w", encoding="utf-8").write(s.replace("resources: []", block.rstrip("\n"), 1))


def run_cli(proj, args, env=None):
    full_env = {**os.environ, **GIT_ENV, **CLI_ENV_DEFAULTS}
    if env:
        full_env.update(env)
    r = subprocess.run([BIN] + args.split(), cwd=proj, stdin=subprocess.DEVNULL,
                       capture_output=True, text=True, env=full_env)
    return r.stdout + r.stderr


# 教程里所有 CLI 输出块的行首特征。用来算分母——只报分子不报分母，
# "守住了 N 个"看起来和"全都守住了"一模一样。
OUTPUT_MARKS = ("✅", "📦", "📋", "⬆️", "🎯", "⚠️", "❌", "📁",
                "🗑️", "⏭️", "🔏", "📂", "ℹ️", "🚀", "🛑", "📊", "🔍", "💡", "🔧", "🐳")


def count_output_blocks(pattern):
    n = 0
    for path in glob.glob(pattern):
        for b in fenced_blocks(path):
            if b and b[0].startswith(OUTPUT_MARKS):
                n += 1
    return n


def main():
    if not os.access(BIN, os.X_OK):
        sys.exit(f"❌ 找不到 {BIN}，先 make build-cli")

    work = tempfile.mkdtemp(prefix="brickkit-guide-")
    proj = None
    compared = 0
    problems = []
    mismatched_zh = []

    try:
        for case in CASES:
            if case.get("reset") or proj is None:
                proj = prepare(work)
            for step in case["run"]:
                if step.startswith("!copy-into "):
                    _, dst_rel, slug = step.split(None, 2)
                    copy_into(proj, dst_rel, slug)
                elif step.startswith("!set-version "):
                    _, dir_rel, version = step.split(None, 2)
                    set_version(proj, dir_rel, version)
                elif step.startswith("!disable "):
                    disable(proj, step.split(None, 1)[1])
                elif step.startswith("!pin "):
                    pin(proj, step.split(None, 1)[1])
                elif step.startswith("!local-debug "):
                    _, component_id, port = step.split(None, 2)
                    local_debug(proj, component_id, port)
                elif step.startswith("!add-second-version "):
                    _, component_id, slug = step.split(None, 2)
                    add_second_version(proj, component_id, slug)
                elif step.startswith("!bind-resource "):
                    _, resource_id, host = step.split(None, 2)
                    bind_resource(proj, resource_id, host)
                elif step == "!make-remotes":
                    make_remotes(work)
                elif step == "!git-sources":
                    git_sources(proj)
                elif step == "!clear-enabled":
                    clear_enabled(proj)
                elif step == "!git-init":
                    git_must(proj, ["init", "-q", "-b", "main"])
                elif step == "!drop-components-ignore":
                    drop_components_ignore(proj)
                elif step.startswith("!append "):
                    _, rel, text = step.split(None, 2)
                    append_line(proj, rel, text)
                elif step.startswith("!git "):
                    git_step(proj, work, step[len("!git "):])
                else:
                    run_cli(proj, step)

            cmd, anchor, nth = case["check"]
            en_path = os.path.join(EN_GUIDE, case["file"])
            zh_path = os.path.join(ZH_GUIDE, case["file"])
            expected = find_block(en_path, anchor, nth)
            zh_expected = find_block(zh_path, anchor, nth)

            env = {"DB_PASSWORD": "devpass"} if "bind-resource" in " ".join(case["run"]) else None
            if cmd.startswith("!git "):
                actual = git_step(proj, work, cmd[len("!git "):])
            else:
                actual = run_cli(proj, cmd, env=env)
            compared += 1

            bad = compare(expected, actual)
            if bad:
                line, hint = bad
                problems.append((case["what"], case["file"], cmd, line, hint))

            if expected != zh_expected:
                mismatched_zh.append((case["what"], case["file"], anchor))
    finally:
        shutil.rmtree(work, ignore_errors=True)

    if compared != len(CASES):
        print(f"❌ 比对 {compared} 与用例数 {len(CASES)} 对不上——"
              "有用例被静默漏掉了，这比失败更危险。")
        sys.exit(2)

    if problems:
        print(f"❌ 教程预期输出对不上：{len(problems)} 处")
        for what, filename, cmd, line, hint in problems:
            print(f"   docs/en/03-guide/{filename}（{what}）")
            print(f"     命令：brickkit {cmd}")
            print(f"     教程里写着：{line}")
            print(f"     {hint}")
        print("\n文档里抄下来的输出是手写快照，CLI 文案一改它就过期。")
        print("请以**真实输出**为准改文档，而不是反过来。")
        sys.exit(1)

    if mismatched_zh:
        print(f"❌ docs/en 与 docs/zh 抄的不是同一份输出：{len(mismatched_zh)} 处")
        for what, filename, anchor in mismatched_zh:
            print(f"   {filename}（{what}），锚点：{anchor}")
        print("\n两棵树是独立撰写的，但 CLI 打印的文本本身是中文，不受读者语言")
        print("影响——同一个场景下两边嵌的输出块必须逐字相同。改了一边、另一边")
        print("没跟上。")
        sys.exit(1)

    en_total = count_output_blocks(os.path.join(EN_GUIDE, "[0-9]*-*.md"))
    zh_total = count_output_blocks(os.path.join(ZH_GUIDE, "[0-9]*-*.md"))
    print(f"✅ 教程里的 CLI 输出：{compared} 个场景逐行一致，docs/en 与 docs/zh 抄的是同一份")
    print(f"   docs/en/03-guide：共 {en_total} 个输出块，本次看守 {compared} 个场景"
          f"（其余大多要 Docker 真的把容器跑起来，或要 minikube / 市场 / cosign）")
    print(f"   docs/zh/03-guide：共 {zh_total} 个输出块，同上")


if __name__ == "__main__":
    main()
