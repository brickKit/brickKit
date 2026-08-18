#!/usr/bin/env python3
"""给 brickkit init 生成的骨架补一个本地安装源（骨架里没有 sources，见 试用指南 02）。

# 一个踩过的坑

判断"已经有 sources 了"不能写 `"sources:" in s`——骨架里有 `resources: []`，
它**包含子串 `sources:`**，于是这个判断永远为真，脚本每次都提前退出、什么也不做，
而且不报错。按行首判断才对。
"""
import io, sys

path = sys.argv[1]
s = io.open(path, encoding="utf-8").read()

if any(line.startswith("sources:") for line in s.splitlines()):
    sys.exit(0)

if "components: []" not in s:
    sys.exit("seed-sources：骨架里找不到 `components: []`，无法插入安装源")

s = s.replace("components: []",
              "sources:\n  - id: local-dev\n    type: local\n    path: ./components\n\ncomponents: []", 1)
io.open(path, "w", encoding="utf-8").write(s)
