#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""跑测试，把"断言失败"抽成 `文件:行  类型  期望片段` 的清单。

改完文案再迁移测试时，失败输出里最有用的就是"哪一行断言、期望的是哪句话"。
go test 的原始输出太长；这里把每个 Error Trace 抽成一行：

    NOT-CONTAIN   实际输出里没有期望的片段 → 多半要把期望换成英文
    OTHER         其余（Equal 不等、次数不对……）→ 显示期望与实际各取前 150 字

用法：python3 tools/i18n/failx.py ./internal/cli/ ./tests/...
"""

import pathlib
import re
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]


def main():
    out = subprocess.run(["go", "test", "-count=1"] + sys.argv[1:], capture_output=True, text=True, cwd=ROOT).stdout
    for block in re.split(r'\n\s*Error Trace:\s*', out)[1:]:
        m = re.match(r'(\S+?):(\d+)', block)
        if not m:
            continue
        loc = f"{m.group(1).split('/' + ROOT.name + '/')[-1]}:{m.group(2)}"
        err = re.search(r'Error:\s*(.*?)\n\s*Test:', block, re.S)
        text = err.group(1) if err else ""
        miss = re.search(r'does not contain "((?:[^"\\]|\\.)*)"', text)
        if miss:
            print(f"{loc}\tNOT-CONTAIN\t{miss.group(1)}")
            continue
        exp = re.search(r'expected:\s*(.*?)\n\s*actual', text, re.S)
        act = re.search(r'actual\s*:\s*(.*?)(\n\s*Diff:|\Z)', text, re.S)
        print(f"{loc}\tOTHER\t{(exp.group(1).strip()[:150] if exp else text.strip()[:150])}"
              f"  ⟶  {(act.group(1).strip()[:150] if act else '')}")


if __name__ == "__main__":
    main()
