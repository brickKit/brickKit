#!/usr/bin/env python3
"""docs/en 与 docs/zh 镜像一致性 + 根目录 README 双语齐全 + llms.txt 链接完整性。

守三件事：① docs/en 下每一份文档，docs/zh 下必须有同一相对路径的对应文件，
反之亦然——对称双语意味着任何一份都不是"翻译附属"，少了一份就是承诺被打破。
② 根目录的 README.md / README.zh.md 必须成对存在——同一条原则用在入口文件
上。③ llms.txt 里每一条 raw.githubusercontent.com 链接指向的文件必须真实
存在——这条呼应 be-assembly-standard 反馈里"结构检查脚本自己也要用真实
反例验证"那条教训：一个检查规则本身也是代码，链接指向的文件被改名/删除时
必须报错，不能悄悄继续"通过"。
"""
import glob
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def mirror_pairs():
    """返回 (docs/en 相对路径, docs/zh 相对路径) 应该成对存在的清单。"""
    en_files = {
        os.path.relpath(p, os.path.join(ROOT, "docs", "en"))
        for p in glob.glob(os.path.join(ROOT, "docs", "en", "**", "*.md"), recursive=True)
    }
    zh_files = {
        os.path.relpath(p, os.path.join(ROOT, "docs", "zh"))
        for p in glob.glob(os.path.join(ROOT, "docs", "zh", "**", "*.md"), recursive=True)
    }
    return en_files, zh_files


def check_mirror():
    en_files, zh_files = mirror_pairs()
    only_en = sorted(en_files - zh_files)
    only_zh = sorted(zh_files - en_files)
    bad = []
    for rel in only_en:
        bad.append(f"docs/en/{rel} 有英文版，docs/zh/{rel} 缺对应中文版")
    for rel in only_zh:
        bad.append(f"docs/zh/{rel} 有中文版，docs/en/{rel} 缺对应英文版")
    return bad


def check_llms_txt_links():
    path = os.path.join(ROOT, "llms.txt")
    text = open(path, encoding="utf-8").read()
    prefix = "https://raw.githubusercontent.com/brickKit/brickKit/main/"
    bad = []
    for m in re.finditer(r"\[([^\]]+)\]\((" + re.escape(prefix) + r"[^)]+)\)", text):
        url = m.group(2)
        rel = url[len(prefix):]
        # raw URL 里目录/文件名做过 URL 编码，还原成真实路径再判断存在性
        from urllib.parse import unquote
        local = os.path.join(ROOT, unquote(rel))
        # 以 / 结尾的是目录类入口链接（比如「旧设计书」整个目录，不指向具体
        # 某一篇），按目录判存在性；其余按文件判——这条区分是必须的，不是
        # 可选的润色：GitHub raw 链接允许指向目录（渲染成 GitHub 的目录浏览
        # 页），如果统一按 os.path.isfile 判断，任何目录类链接都会被判定
        # "文件不存在"，哪怕那个目录真实存在。
        is_dir_link = rel.endswith("/")
        exists = os.path.isdir(local) if is_dir_link else os.path.isfile(local)
        if not exists:
            kind = "目录" if is_dir_link else "文件"
            bad.append(f"llms.txt 链接 {url} 指向的{kind}不存在：{local}")
    return bad


def check_root_readme_pair():
    """根目录的多语言 README 必须成对存在。

    README 不在 docs/en|zh 树下，是单独的一对文件（README.md / README.zh.md），
    mirror_pairs() 那套按目录扫描的逻辑覆盖不到它，所以单独查一次。将来新增
    第三种语言时，这里也要跟着加一行——这条检查本身不会自动发现"该有却没有"
    的语言，只能守住"已经存在的语言必须两两都在"。
    """
    root = os.path.join(ROOT, "README.md")
    zh = os.path.join(ROOT, "README.zh.md")
    bad = []
    if os.path.isfile(root) and not os.path.isfile(zh):
        bad.append("README.md 存在，但 README.zh.md 缺失")
    if os.path.isfile(zh) and not os.path.isfile(root):
        bad.append("README.zh.md 存在，但 README.md 缺失")
    return bad


def main():
    bad = check_mirror() + check_llms_txt_links() + check_root_readme_pair()
    if bad:
        print("❌ 文档双语/链接完整性检查失败：")
        for line in bad:
            print(f"   - {line}")
        sys.exit(1)
    print("✅ docs/en ↔ docs/zh 镜像完整，README 双语齐全，llms.txt 全部链接可解析")


if __name__ == "__main__":
    main()
