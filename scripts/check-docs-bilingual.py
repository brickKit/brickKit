#!/usr/bin/env python3
"""docs/、tutorials/ 的 en 与 zh 镜像一致性 + 根目录多语言入口文件齐全 + llms 索引链接完整性。

守四件事：① docs/en 下每一份文档，docs/zh 下必须有同一相对路径的对应文件，
反之亦然——对称双语意味着任何一份都不是"翻译附属"，少了一份就是承诺被打破。
② 根目录的一对多语言入口文件必须成对存在——README.md/README.zh.md、
AGENTS.md/AGENTS.zh.md、llms.txt/llms.zh.txt 都是这个形状，少了一份就是
承诺被打破（llms.txt/llms.zh.txt 是两份独立文件，不是同一份文件内分两节——
一份文件塞所有语言的做法在语言种类增多时会线性膨胀，2026-09 改成了跟
README/AGENTS 一样的"每种语言一个文件"）。③ llms 两份与 AGENTS 两份里
每一条相对链接指向的文件必须真实存在——这条呼应
be-assembly-standard 反馈里"结构检查脚本自己也要用真实反例验证"那条教训：
一个检查规则本身也是代码，链接指向的文件被改名/删除时必须报错，不能悄悄
继续"通过"。④ 英文这一侧（docs/en 与 llms.txt）里不许出现中文字符——CLI 支持
多语言之后，英文文档引用的输出、报错标题、示例文件都该是英文；一行中文悄悄
留在英文文档里，读者只会觉得这份文档没做完。确有理由保留的（比如专门讲一个中文
词）加进 ENGLISH_DOCS_ALLOW，并写清理由。
"""
import glob
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


# 两棵对称镜像树：参考文档与教程，各自 en/ 与 zh/ 下的相对路径一一对应。
MIRROR_ROOTS = ["docs", "tutorials"]


def md_files(*parts):
    base = os.path.join(ROOT, *parts)
    return {os.path.relpath(p, base) for p in glob.glob(os.path.join(base, "**", "*.md"), recursive=True)}


def check_mirror():
    bad = []
    for top in MIRROR_ROOTS:
        en_files, zh_files = md_files(top, "en"), md_files(top, "zh")
        for rel in sorted(en_files - zh_files):
            bad.append(f"{top}/en/{rel} 有英文版，{top}/zh/{rel} 缺对应中文版")
        for rel in sorted(zh_files - en_files):
            bad.append(f"{top}/zh/{rel} 有中文版，{top}/en/{rel} 缺对应英文版")
    return bad


LLMS_TXT_FILES = ["llms.txt", "llms.zh.txt"]

# 入口文件：llms 两份是纯索引；AGENTS 两份的文档地图同样是给 AI 顺着打开的。链接一律是相对路径。
RAW_LINK_FILES = LLMS_TXT_FILES + ["AGENTS.md", "AGENTS.zh.md"]

HAN = re.compile(r"[\u4e00-\u9fff\u3000-\u303f\uff00-\uffef]")

# ENGLISH_DOCS_ALLOW：允许出现中文的位置。key 是 (相对路径, 那一行里的一段文字)，value 是理由。
# 加一条是有意识的决定，不是顺手。
ENGLISH_DOCS_ALLOW = {
    ("docs/en/07-cli-reference/README.md", "当前语言：zh"):
        "brickkit lang 的示例：刻意展示切到中文之后 CLI 真实说的话",
    ("docs/en/07-cli-reference/README.md", "语言已设为 zh"):
        "同上",
    ("docs/en/07-cli-reference/README.md", "不支持的语言：fr"):
        "同上",
    ("docs/en/03-component-guide/08-component-doc-spec.md", "`组件定位`"):
        "固定小节的中文标准标题：英文原文的作者写中文译本时要一字不差地照抄，lint 只认这些名字",
    ("docs/en/03-component-guide/08-component-doc-spec.md", "`代码地图`"):
        "同上",
    ("docs/en/03-component-guide/08-component-doc-spec.md", "`项目概述`"):
        "同上",
    ("docs/en/03-component-guide/08-component-doc-spec.md", "`在项目里使用`"):
        "同上",
}


def english_side_files():
    files = []
    for top in MIRROR_ROOTS:
        files += sorted(glob.glob(os.path.join(ROOT, top, "en", "**", "*.md"), recursive=True))
    files.append(os.path.join(ROOT, "llms.txt"))
    return files


# 自检用：一定存在的英文侧文件。拿"找到了几份"当判据会随文档增删漂移
# （重写期间整棵树可能只剩几份），拿一份已知文件当判据不会。
KNOWN_ENGLISH_FILE = "docs/en/11-reference/06-market-api.md"


def check_english_docs_have_no_chinese():
    bad = []
    for path in english_side_files():
        rel = os.path.relpath(path, ROOT)
        for n, line in enumerate(open(path, encoding="utf-8"), 1):
            if not HAN.search(line):
                continue
            if any(rel == r and needle in line for (r, needle) in ENGLISH_DOCS_ALLOW):
                continue
            bad.append(f"{rel}:{n} 英文文档里有中文：{line.strip()[:70]}")
    return bad


def self_check():
    """解析坏了会安静地全部通过，比没有检查更糟——同 check-doc-tree.py 的自我防御。"""
    if not HAN.search("你好") or HAN.search("hello"):
        print("❌ 自检失败：中文字符的正则认不出中文（或把英文当成了中文）。")
        sys.exit(2)
    found = {os.path.relpath(p, ROOT) for p in english_side_files()}
    if KNOWN_ENGLISH_FILE not in found:
        print(f"❌ 自检失败：英文侧文档里找不到 {KNOWN_ENGLISH_FILE}——glob 多半坏了。"
              "（这份文件真被挪走了的话，换一份一定存在的文件填进 KNOWN_ENGLISH_FILE。）")
        sys.exit(2)
    if not check_mirror_detects_gap():
        print("❌ 自检失败：一份只有英文版的假文档没被镜像检查认出来。")
        sys.exit(2)
    for name in LLMS_TXT_FILES:
        text = open(os.path.join(ROOT, name), encoding="utf-8").read()
        count = sum(1 for m in REL_LINK.finditer(text) if "://" not in m.group(2))
        if count < 5:
            print(f"❌ 自检失败：{name} 里只解析出 {count} 条相对链接——正则多半坏了，而不是链接真的这么少。")
            sys.exit(2)


def check_mirror_detects_gap():
    """造一对只缺中文版的临时文件，镜像检查必须报出来。"""
    import tempfile
    global ROOT
    saved = ROOT
    with tempfile.TemporaryDirectory() as tmp:
        gap = "x/a.md"
        os.makedirs(os.path.join(tmp, "docs", "en", "x"))
        os.makedirs(os.path.join(tmp, "docs", "zh", "x"))
        open(os.path.join(tmp, "docs", "en", gap), "w").close()
        ROOT = tmp
        try:
            return any(f"docs/zh/{gap}" in line for line in check_mirror())
        finally:
            ROOT = saved


REL_LINK = re.compile(r"\[([^\]]+)\]\(([^)\s#]+)(#[^)]*)?\)")


def check_llms_txt_links():
    """入口文件（llms 两份、AGENTS 两份）里的每条相对链接都要指向真实存在的文件或目录。"""
    from urllib.parse import unquote
    bad = []
    for name in RAW_LINK_FILES:
        text = open(os.path.join(ROOT, name), encoding="utf-8").read()
        for m in REL_LINK.finditer(text):
            target = m.group(2)
            if "://" in target or target.startswith("mailto:"):
                continue
            local = os.path.join(ROOT, unquote(target))
            # 以 / 结尾的是目录类入口链接，按目录判存在性；其余按文件判
            is_dir_link = target.endswith("/")
            exists = os.path.isdir(local) if is_dir_link else os.path.isfile(local)
            if not exists:
                kind = "目录" if is_dir_link else "文件"
                bad.append(f"{name} 链接 {target} 指向的{kind}不存在：{local}")
    return bad


def check_root_language_pair(en_name, zh_name):
    """根目录的一对多语言入口文件必须成对存在（README.md/README.zh.md、
    AGENTS.md/AGENTS.zh.md、llms.txt/llms.zh.txt、CONTRIBUTING.md/CONTRIBUTING.zh.md 都是这个形状）。

    这类文件不在 docs/en|zh 树下，是仓库根目录单独的一对，mirror_pairs() 那套
    按目录扫描的逻辑覆盖不到它们，所以单独查一次。将来新增第三种语言时，
    这里也要跟着给每一对加一行——这条检查本身不会自动发现"该有却没有"的
    语言，只能守住"已经存在的语言必须两两都在"。
    """
    en = os.path.join(ROOT, en_name)
    zh = os.path.join(ROOT, zh_name)
    bad = []
    if os.path.isfile(en) and not os.path.isfile(zh):
        bad.append(f"{en_name} 存在，但 {zh_name} 缺失")
    if os.path.isfile(zh) and not os.path.isfile(en):
        bad.append(f"{zh_name} 存在，但 {en_name} 缺失")
    return bad


def main():
    self_check()
    bad = (
        check_mirror()
        + check_llms_txt_links()
        + check_english_docs_have_no_chinese()
        + check_root_language_pair("README.md", "README.zh.md")
        + check_root_language_pair("AGENTS.md", "AGENTS.zh.md")
        + check_root_language_pair("llms.txt", "llms.zh.txt")
        + check_root_language_pair("CONTRIBUTING.md", "CONTRIBUTING.zh.md")
    )
    if bad:
        print("❌ 文档双语/链接完整性检查失败：")
        for line in bad:
            print(f"   - {line}")
        sys.exit(1)
    print("✅ docs、tutorials 的 en ↔ zh 镜像完整，README/AGENTS/llms/CONTRIBUTING 双语齐全，llms 索引全部链接可解析，英文文档里没有中文")


if __name__ == "__main__":
    main()
