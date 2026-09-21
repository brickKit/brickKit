#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""批量改测试里的期望值（中文 → 英文）的小工具箱。

文案迁移之后，测试里成百上千条 `assert.Contains(t, out, "已添加")` 要换成英文。
三个函数，各自的用法不同：

    apply(path, [(旧, 新[, 期望次数])])      对一个文件做整字面量或子串替换。
        跳过纯注释行（注释里常出现同一个引号短语，会把次数算错）；
        次数对不上时**不写文件**并报出来，别的文件不受影响——静默多改或漏改比不改更糟。
    apply_lines(path, {行号: (旧, 新)})        精确到行，用于同一个短语在文件里有多种含义的时候。
    run([(旧, 新)], files=None)              对 internal/cli 下所有测试文件（或指定文件）
        套一份短语表，长的优先，只动非注释行，并统计每条命中几次；没命中的会点名。

用法（在仓库根）：

    PYTHONPATH=tools/i18n python3 - <<'PY'
    from testrewrite import apply, apply_lines, run
    apply("internal/cli/down_test.go", [('"已停止"', '"stopped"')])
    run([("📋 组件状态计算：", "📋 Component state calculation:")])
    PY

提醒：改完一定再跑一遍"否定断言"清扫（tests/i18nguard 会拦下还留着中文的
NotContains）——对英文输出，中文短语的否定断言永远成立，等于检查悄悄消失了。
"""

import pathlib

ROOT = pathlib.Path(__file__).resolve().parents[2]


def apply(path, pairs):
    p = ROOT / path
    lines = p.read_text(encoding="utf-8").split("\n")

    def code(i):
        return not lines[i].strip().startswith("//")

    for item in pairs:
        old, new = item[0], item[1]
        n = item[2] if len(item) > 2 else None
        if "\n" in old:  # 跨行的旧串：退回整篇文本替换
            text = "\n".join(lines)
            found = text.count(old)
            if found < 1 or (n is not None and found != n):
                print("失败", path, repr(old[:60]), "找到", found, "期望", n)
                return False
            lines = text.replace(old, new).split("\n")
            continue
        hits = [i for i in range(len(lines)) if code(i) and old in lines[i]]
        found = sum(lines[i].count(old) for i in hits)
        if not hits or (n is not None and found != n):
            print("失败", path, repr(old[:60]), "找到", found, "期望", n)
            return False
        for i in hits:
            lines[i] = lines[i].replace(old, new)
    p.write_text("\n".join(lines), encoding="utf-8")
    print("ok", path, len(pairs), "条")
    return True


def apply_lines(path, edits):
    p = ROOT / path
    lines = p.read_text(encoding="utf-8").split("\n")
    for ln, (old, new) in edits.items():
        assert old in lines[ln - 1], (path, ln, old, lines[ln - 1])
        lines[ln - 1] = lines[ln - 1].replace(old, new)
    p.write_text("\n".join(lines), encoding="utf-8")
    print("ok", path, len(edits), "行")


def run(pairs, files=None):
    pairs = sorted(pairs, key=lambda x: -len(x[0]))
    hits = {zh: 0 for zh, _ in pairs}
    targets = sorted((ROOT / "internal/cli").glob("*_test.go")) if files is None else [ROOT / x for x in files]
    for f in targets:
        lines = f.read_text(encoding="utf-8").split("\n")
        changed = False
        for i, line in enumerate(lines):
            if line.strip().startswith("//"):
                continue
            new = line
            for zh, en in pairs:
                if zh in new:
                    hits[zh] += new.count(zh)
                    new = new.replace(zh, en)
            if new != line:
                lines[i] = new
                changed = True
        if changed:
            f.write_text("\n".join(lines), encoding="utf-8")
    for zh, n in hits.items():
        if n == 0:
            print("没命中:", zh)
    print("合计替换", sum(hits.values()), "处")
