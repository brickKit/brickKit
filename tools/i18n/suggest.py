#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""按中文片段在消息目录里查候选译文。

迁移测试期望值时，失败清单里给的是中文片段（如 "已停止"）。这个脚本列出目录里
包含该片段的条目，连同英文原文，帮你决定期望该改成英文的哪一段。

用法：python3 tools/i18n/suggest.py 已停止 "没有容器在跑"
"""

import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
ENTRY = re.compile(r'^\s*msgid\.(\w+):\s*("(?:[^"\\]|\\.)*"),?\s*$', re.M)


def load(name):
    text = (ROOT / "internal/i18n" / name).read_text(encoding="utf-8")
    return {m.group(1): json.loads(m.group(2)) for m in ENTRY.finditer(text)}


def main():
    zh, en = load("catalog_zh.go"), load("catalog_en.go")
    for frag in sys.argv[1:]:
        print("##", frag)
        for key, value in list(zh.items()):
            if frag in value:
                print("   ", key)
                print("      zh:", value[:160].replace("\n", "⏎"))
                print("      en:", en.get(key, "")[:160].replace("\n", "⏎"))


if __name__ == "__main__":
    main()
