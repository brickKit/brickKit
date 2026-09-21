#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""给改写过的 Go 文件补 i18n / msgid 的 import，并删掉编译器说"没用到"的 import。

migrate apply 会给它改过的文件补 import，但别的手工改写（正则、post_join.py）不会；
另外把 fmt.Errorf 换成 errors.New 之类的改写会让某些 import 变得没用。跑完批量改写后
跑一遍这个，再 gofmt 就能编译了。

用法：python3 tools/i18n/fix_imports.py [包目录……]      （默认 internal/cli）
"""

import pathlib
import re
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
I18N = '"github.com/brickkit/brickkit/internal/i18n"'
MSGID = '"github.com/brickkit/brickkit/internal/msgid"'


def add(src, imp):
    if imp in src:
        return src
    m = re.search(r'\t"github.com/brickkit/brickkit/internal/[a-z0-9]+"\n', src)
    if not m:
        m = re.search(r'import \(\n', src)
    return src[:m.end()] + "\t" + imp + "\n" + src[m.end():]


def main():
    for pkg in sys.argv[1:] or ["internal/cli"]:
        for f in sorted((ROOT / pkg).glob("*.go")):
            src = f.read_text(encoding="utf-8")
            new = src
            if "i18n.T(" in new:
                new = add(new, I18N)
            if "msgid." in new:
                new = add(new, MSGID)
            if new != src:
                f.write_text(new, encoding="utf-8")
                print("补 import:", f.relative_to(ROOT))

    # 编译器点名的"导入了没用"逐条删掉；一轮可能露出下一层，最多重复几次
    for _ in range(6):
        out = subprocess.run(["go", "build", "./..."], cwd=ROOT, capture_output=True, text=True)
        bad = re.findall(r'^(\S+\.go):(\d+):\d+: "([^"]+)" imported and not used', out.stderr, re.M)
        if not bad:
            break
        for path, line, imp in bad:
            p = ROOT / path
            lines = p.read_text(encoding="utf-8").split("\n")
            i = int(line) - 1
            if imp in lines[i]:
                lines[i] = None
            p.write_text("\n".join(l for l in lines if l is not None), encoding="utf-8")
            print("删除没用的 import:", path, imp)


if __name__ == "__main__":
    main()
