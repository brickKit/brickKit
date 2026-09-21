#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""把某个包私有的 msgid 常量提升成跨包共享的（或反过来改名）。

两个包的文案措辞完全一致时，不该各存一份：把其中一个常量改名、挪到共享文件，
另一处直接引用它。这个模块做机械部分：从原来声明它的 msgid 文件里删掉、写进目标文件
（新的 key 字符串值），并把目录与全部 Go 源码里的旧标识符换成新的。文案本身不动。

用法（在仓库根）：

    PYTHONPATH=tools/i18n python3 - <<'PY'
    from rename_keys import rename
    rename([("ConfigIOFailed", "IOFailed", "io.failed", "msgid")])
    PY

参数：(旧常量名, 新常量名, 新 key 值, 目标 msgid 文件名（不带 .go）)。
"""

import json
import pathlib
import re

ROOT = pathlib.Path(__file__).resolve().parents[2]


def rename(renames):
    moved = {}
    for old, new, key, target in renames:
        for f in (ROOT / "internal/msgid").glob("*.go"):
            src = f.read_text(encoding="utf-8")
            pat = re.compile(r'^\t' + old + r'\s*=\s*"[^"]*"\n', re.M)
            if pat.search(src):
                f.write_text(pat.sub("", src), encoding="utf-8")
                break
        else:
            raise SystemExit(f"找不到常量 {old}")
        moved.setdefault(target, []).append((new, key))

    for target, items in moved.items():
        p = ROOT / f"internal/msgid/{target}.go"
        block = "\n// 跨包共享（改名自各包私有的同文案 key）\nconst (\n" + "".join(
            f"\t{n} = {json.dumps(k, ensure_ascii=False)}\n" for n, k in items) + ")\n"
        p.write_text(p.read_text(encoding="utf-8") + block, encoding="utf-8")

    mapping = {o: n for o, n, _, _ in renames}
    pat = re.compile(r'\bmsgid\.(' + "|".join(mapping) + r')\b')
    touched = 0
    for top in ("internal", "cmd", "tests"):
        for f in (ROOT / top).rglob("*.go"):
            src = f.read_text(encoding="utf-8")
            new = pat.sub(lambda m: "msgid." + mapping[m.group(1)], src)
            if new != src:
                f.write_text(new, encoding="utf-8")
                touched += 1
    print("改名", len(renames), "个 key，涉及", touched, "个文件（记得 gofmt internal/msgid internal/i18n）")
