#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""把文档围栏块里的中文 CLI 输出行，按消息目录翻译成英文。

为什么有这个脚本：docs/en 的教程与参考里嵌着大量"抄下来的真实输出"。CLI 支持多语言之前
这些输出都是中文；现在英文文档该配英文输出。每一行输出都来自消息目录里的一条文案，
所以不用手翻：把中文行对回目录里的中文模板（`已停止 %[1]s`），取出参数，
再用对应的英文模板拼回去。

怎么匹配一行（按顺序试）：

1. 整行匹配目录里某条文案（含缩进、emoji、定宽对齐 `%-36[1]s`）；多条都匹配取"字面量最长"的；
2. 剥掉结构性的前缀再试：错误块的 `❌ ` / `⚠️ `、`   💡 `、建议的编号 `   1. `、
   YAML/shell 的注释前缀 `# `、纯缩进；
3. 明细行 `   键：值`：键是目录里的标签就翻标签，是 YAML 路径这类数据就原样保留；值递归翻译。

参数里如果还是中文（比如明细的"原因"是另一条文案），会递归翻译。
译不出的行原样保留，并在报告里点名——那些要人来处理（通常是文档里手改过的输出，
或者目录里中英行数对不上的多行文案）。

用法：

    python3 tools/i18n/docs_outputs.py docs/en/03-guide/01-first-project.md          # 干跑：只报告
    python3 tools/i18n/docs_outputs.py --write docs/en/00-quick-start.md ...        # 原地改写
    python3 tools/i18n/docs_outputs.py --tags none,bash,mermaid ...                # 处理哪些围栏（默认 none,bash,mermaid）

只动围栏块里的行，不碰正文。翻完之后用 scripts/check-guide-output.py 对着真实的英文输出核对。
"""

import argparse
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
CJK = re.compile(r'[一-鿿　-〿＀-￯]')
HAN = re.compile(r'[\u4e00-\u9fff]')
ENTRY = re.compile(r'^\s*msgid\.(\w+):\s*("(?:[^"\\]|\\.)*"),?\s*$', re.M)
# 位置动词：%[1]s、%-36[1]s、%5.1[1]f、%%
VERB = re.compile(r'%%|%([-+# 0]*)(\d*)(?:\.(\d+))?\[(\d+)\]([a-zA-Z])')


def load_catalog(name):
    text = (ROOT / "internal/i18n" / name).read_text(encoding="utf-8")
    return {m.group(1): json.loads(m.group(2)) for m in ENTRY.finditer(text)}


class Template:
    """一条"中文行 ↔ 英文行"的模板（多行文案已按行拆开、行数对得上才收）。"""

    def __init__(self, key, zh, en):
        self.key, self.zh, self.en = key, zh, en
        self.regex, self.widths, self.literal = self._compile(zh)

    @staticmethod
    def _compile(zh):
        pattern, pos, widths, literal = "", 0, {}, 0
        for m in VERB.finditer(zh):
            lit = zh[pos:m.start()]
            pattern += re.escape(lit)
            literal += len(lit.strip())
            pos = m.end()
            if m.group(0) == "%%":
                pattern += "%"
                literal += 1
                continue
            flags, width, _, idx, _ = m.groups()
            if width:
                widths[int(idx)] = (flags, int(width))
            pattern += r"(.*?)"
        lit = zh[pos:]
        pattern += re.escape(lit)
        literal += len(lit.strip())
        return re.compile("^" + pattern + "$", re.S), widths, literal

    def match(self, line):
        m = self.regex.match(line)
        if not m:
            return None
        # 位置动词的下标与捕获组顺序一致吗？中文模板里 %[n] 的出现顺序可能不是 1,2,3
        order = [int(x.group(4)) for x in VERB.finditer(self.zh) if x.group(0) != "%%"]
        args = {}
        for idx, value in zip(order, m.groups()):
            args[idx] = value
        return args


def build_templates(zh, en):
    templates = []
    for key, z in zh.items():
        e = en.get(key)
        if e is None:
            continue
        zl, el = z.split("\n"), e.split("\n")
        if len(zl) != len(el):
            continue  # 中英行数对不上，无法逐行对应
        for zi, ei in zip(zl, el):
            # 只收"中文那一行真的带中文"的：全是符号/命令的行两种语言一样，不用翻
            if CJK.search(zi):
                t = Template(key, zi, ei)
                # 字面量里汉字太少的模板（比如 "%[1]s（%[2]s）"、"、%[1]s"）会误配一大堆，不收
                lit = re.sub(r'%(?:%|[-+# 0]*\d*(?:\.\d+)?\[\d+\][a-zA-Z])', "", zi)
                if len(HAN.findall(lit)) >= 2 or len(re.findall(r'[A-Za-z]', lit)) >= 4:
                    templates.append(t)
    return templates


class Translator:
    def __init__(self):
        zh, en = load_catalog("catalog_zh.go"), load_catalog("catalog_en.go")
        self.templates = build_templates(zh, en)
        self.zh, self.en = zh, en
        # 标签：整条目录文案就是一个短词（"组件"、"原因"、"文件"……），明细行的键要翻它
        self.labels = {}
        for key, z in zh.items():
            if "\n" not in z and "%" not in z and len(z) <= 12 and key in en:
                self.labels.setdefault(z, en[key])
        self._whole, self._tr = {}, {}
        self.detail_en = en["DetailLine"]
        self.hint_single_zh = zh["HintLabelSingle"]

    # ---- 单行翻译 ----

    def whole(self, line):
        """整行匹配目录里的一条文案；多条都匹配取字面量最长的。"""
        if line in self._whole:
            return self._whole[line]
        self._whole[line] = self._whole_uncached(line)
        return self._whole[line]

    def _whole_uncached(self, line):
        best = None
        for t in self.templates:
            args = t.match(line)
            if args is None:
                continue
            if best is None or t.literal > best[0].literal:
                best = (t, args)
        if best is None:
            return None
        t, args = best
        return self.render(t, args)

    def render(self, t, args):
        out, pos = "", 0
        for m in VERB.finditer(t.en):
            out += t.en[pos:m.start()]
            pos = m.end()
            if m.group(0) == "%%":
                out += "%"
                continue
            flags, width, _, idx, verb = m.groups()
            value = args.get(int(idx), "")
            # 参数本身如果还是中文（比如另一条文案），递归翻
            if CJK.search(value):
                value = self.translate(value) or value
            # 中文模板里带定宽对齐的：抹掉中文侧填充的空格，按英文模板的宽度重新对齐
            if int(idx) in t.widths or width:
                value = value.rstrip()
                w = int(width) if width else t.widths[int(idx)][1]
                left = "-" in (flags or "") or (int(idx) in t.widths and "-" in t.widths[int(idx)][0])
                value = value.ljust(w) if left else value.rjust(w)
            out += value
        out += t.en[pos:]
        return out

    def translate(self, line, allow_split=True):
        """翻一行；翻不出返回 None。"""
        key = (line, allow_split)
        if key not in self._tr:
            self._tr[key] = None  # 防止自递归
            self._tr[key] = self._translate(line, allow_split)
        return self._tr[key]

    def _translate(self, line, allow_split):
        if not CJK.search(line):
            return line
        got = self.whole(line)
        if got is not None:
            return got

        # 结构前缀：❌ / ⚠️ / 💡 / 树形符号 / 缩进 / 编号 / 注释
        for pat in (r'^(❌ )(.*)$', r'^(⚠️ )(.*)$', r'^(\s*💡 )(.*)$', r'^(\s*[├└]── )(.*)$', r'^(\s*\d+\. )(.*)$',
                    r'^(\s*# )(.*)$', r'^(\s*// )(.*)$', r'^(\s+)(.*)$'):
            m = re.match(pat, line, re.S)
            if m and m.group(2) != line:
                rest = self.translate(m.group(2))
                if rest is not None:
                    return m.group(1) + rest

        # 由代码拼出来的对齐列：按 2 个以上空格切开，逐段翻译，分隔原样保留
        got = self.columns(line)
        if got is not None:
            return got

        # 明细行：键：值
        m = re.match(r'^(\s*)(.+?)：(.*)$', line)
        if m:
            indent, key, value = m.groups()
            k = self.labels.get(key)
            if k is None and not CJK.search(key):
                k = key  # YAML 路径之类的数据键，原样保留
            v = self.translate(value) if CJK.search(value) else value
            if k is not None and v is not None:
                return indent + self.detail_en.replace("%[1]s", k).replace("%[2]s", v)

        # 两条消息接在同一行（Printf 没换行）：在某处切成两半，两半都能翻才算
        return self.split_two(line) if allow_split else None

    def columns(self, line):
        parts = re.split(r'(\s{2,})', line)
        if len(parts) < 3:
            return None
        out = []
        for part in parts:
            if not part.strip() or not CJK.search(part):
                out.append(part)
                continue
            new = self.translate(part)
            if new is None or CJK.search(new):
                return None
            out.append(new)
        return "".join(out)

    def split_two(self, line):
        best = None
        for i in range(2, len(line) - 1):
            left, right = line[:i], line[i:]
            if not (CJK.search(left) or CJK.search(right)):
                continue
            l = self.translate(left, False) if CJK.search(left) else left
            if l is None or CJK.search(l):
                continue
            r = self.translate(right, False) if CJK.search(right) else right
            if r is None or CJK.search(r):
                continue
            score = len(HAN.findall(left)) * len(HAN.findall(right))
            if best is None or score > best[0]:
                best = (score, l + r)
        return best[1] if best else None


# ---- 框线表格 ----

def display_width(text):
    return sum(2 if (0x4e00 <= ord(c) <= 0x9fff or 0x3000 <= ord(c) <= 0x303f or 0xff00 <= ord(c) <= 0xffef) else 1
               for c in text)


TABLE_LINE = re.compile(r'^(\s*)([┌│├└].*[┐│┤┘])$')


def translate_table(block, translator):
    """把一张框线表格（若干连续行）按英文单元格重新排版。失败返回 None。"""
    indent = TABLE_LINE.match(block[0]).group(1)
    rows = []
    for line in block:
        m = TABLE_LINE.match(line)
        if not m or m.group(2)[0] != "│":
            continue
        cells = [c.strip() for c in m.group(2).strip("│").split("│")]
        new = []
        for c in cells:
            if CJK.search(c):
                t = translator.translate(c)
                if t is None or CJK.search(t):
                    return None
                c = t
            new.append(c)
        rows.append(new)
    widths = [max(display_width(r[i]) for r in rows) for i in range(len(rows[0]))]

    def border(l, mid, r):
        return indent + l + mid.join("─" * (w + 2) for w in widths) + r

    def row(cells):
        return indent + "│" + "│".join(" " + c + " " * (widths[i] - display_width(c)) + " " for i, c in enumerate(cells)) + "│"

    out = [border("┌", "┬", "┐"), row(rows[0]), border("├", "┼", "┤")]
    out += [row(r) for r in rows[1:]]
    out.append(border("└", "┴", "┘"))
    return out


# ---- 文档处理 ----

def process(path, translator, tags, write):
    lines = pathlib.Path(path).read_text(encoding="utf-8").split("\n")
    tag, out, done, left = None, [], 0, []
    skip_until = 0
    for i, line in enumerate(lines, 1):
        if i <= skip_until:
            continue
        if line.startswith("```"):
            tag = None if tag is not None else (line[3:].strip() or "none")
            out.append(line)
            continue
        if tag in tags and TABLE_LINE.match(line) and line.lstrip().startswith("┌"):
            j = i
            while j < len(lines) and TABLE_LINE.match(lines[j]):
                j += 1
            block = lines[i - 1:j]
            if any(CJK.search(l) for l in block):
                new = translate_table(block, translator)
                if new is not None:
                    out.extend(new)
                    done += len(block)
                    skip_until = j
                    continue
                left.extend((i + k, l, None) for k, l in enumerate(block) if CJK.search(l))
            out.extend(block)
            skip_until = j
            continue
        if tag in tags and CJK.search(line):
            new = translator.translate(line)
            if new is not None and not CJK.search(new):
                out.append(new)
                done += 1
                continue
            left.append((i, line, new))
        out.append(line)
    if write and done:
        pathlib.Path(path).write_text("\n".join(out), encoding="utf-8")
    return done, left


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("files", nargs="+")
    ap.add_argument("--write", action="store_true", help="原地改写（默认只报告）")
    ap.add_argument("--tags", default="none,bash,mermaid", help="处理哪些围栏（none 表示没写语言的）")
    args = ap.parse_args()

    tags = set(args.tags.split(","))
    translator = Translator()
    total_done = total_left = 0
    for f in args.files:
        done, left = process(f, translator, tags, args.write)
        total_done += done
        total_left += len(left)
        print(f"{f}: 译 {done} 行，剩 {len(left)} 行")
        for i, line, partial in left:
            print(f"    {i}: {line}")
            if partial and partial != line:
                print(f"       部分译文: {partial}")
    print(f"合计：译 {total_done} 行，剩 {total_left} 行" + ("（已写入）" if args.write else "（干跑，未写入）"))
    return 0


if __name__ == "__main__":
    sys.exit(main())
