#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""把 strings.Join(x, "、") 这类写死的中文分隔符换成随语言变的共享 key。

为什么单独一步：这种 Join 常常嵌在别的中文文案的参数里（`Printf("…%s", strings.Join(xs, "、"))`），
让 migrate 工具逐条处理很别扭；模式又非常固定，所以用一条正则统一处理：

    、  →  i18n.T(msgid.ListSeparator)       中文顿号 / 英文 ", "
    ，  →  i18n.T(msgid.ClauseSeparator)     中文逗号 / 英文 ", "
    ；  →  i18n.T(msgid.SemicolonSeparator)  中文分号 / 英文 "; "

跑完再跑 fix_imports.py。

用法：python3 tools/i18n/post_join.py [文件名……]      （默认处理 internal/cli 下所有非测试文件）
"""

import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
SEPARATORS = {"、": "ListSeparator", "，": "ClauseSeparator", "；": "SemicolonSeparator"}


def main():
    only = set(sys.argv[1:])
    total = 0
    for f in sorted((ROOT / "internal/cli").glob("*.go")):
        if f.name.endswith("_test.go") or (only and f.name not in only):
            continue
        src = f.read_text(encoding="utf-8")
        new, count = src, 0
        for sep, key in SEPARATORS.items():
            pat = re.compile(r'(strings\.Join\((?:[^()"]|\([^()]*\))+?), "' + sep + r'"\)')
            new, c = pat.subn(r'\1, i18n.T(msgid.' + key + '))', new)
            count += c
        if count:
            f.write_text(new, encoding="utf-8")
            total += count
            print("改写", f.name, count)
    print("合计", total)


if __name__ == "__main__":
    main()
