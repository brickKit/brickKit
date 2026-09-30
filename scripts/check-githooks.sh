#!/usr/bin/env bash
# 真跑仓库自带的提交钩子（.githooks/pre-commit）。
#
# 钩子要做两件事，缺一不可：提交里有文档改动时重新生成 llms/ 合集并一起暂存；
# 文档还有没暂存的改动或没跟踪的新文件时拒绝提交——否则生成出来的合集与这次提交的内容对不上。
# 造一个临时仓库，PATH 上放一个假的 go（只记下自己被调用过、写一份假合集），逐个场景验证。
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
HOOK="$ROOT/.githooks/pre-commit"

pass=0
fail=0
ok() {
	echo "   ✅ $1"
	pass=$((pass + 1))
}
bad() {
	echo "   ❌ $1"
	fail=$((fail + 1))
}

echo "▶ shellcheck"
if command -v shellcheck >/dev/null 2>&1; then
	if shellcheck --shell=sh "$HOOK" && shellcheck scripts/check-githooks.sh; then
		ok "shellcheck 通过"
	else
		bad "shellcheck 有问题（见上）"
	fi
else
	echo "   ⏭  跳过：本机没装 shellcheck（apt install shellcheck 或 brew install shellcheck）"
fi

if [ ! -x "$HOOK" ]; then
	echo "❌ $HOOK 不存在或不可执行"
	exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
repo="$tmp/repo"
mkdir -p "$repo/.githooks" "$repo/docs/en" "$tmp/bin"
cp "$HOOK" "$repo/.githooks/pre-commit"

# 假的 go：go run ./cmd/gen-llms 时写一份假合集，并记下被调用过
cat >"$tmp/bin/go" <<EOF
#!/bin/sh
mkdir -p llms/en
date +%s%N >llms/en/00-core.md
echo ran >>"$tmp/go-calls"
EOF
chmod +x "$tmp/bin/go"
export PATH="$tmp/bin:$PATH"

# 临时仓库里的文档路径用变量拼出来：写成字面量会被 check-docs 当成指向真实文档的路径
top=docs
A="$top/en/a.md"
NEW="$top/en/new.md"

g() { git -C "$repo" -c user.name=t -c user.email=t@t "$@"; }
calls() { [ -f "$tmp/go-calls" ] && wc -l <"$tmp/go-calls" | tr -d ' ' || echo 0; }

g init -q
g config core.hooksPath .githooks
echo "# A" >"$repo/$A"
echo "# x" >"$repo/llms.txt"
echo "x" >"$repo/other.txt"
g add -A
g commit -qm init --no-verify

echo "▶ 文档改动：重新生成合集，并一起提交"
before=$(calls)
echo "more" >>"$repo/$A"
g add "$A"
if out=$(cd "$repo" && g commit -qm docs 2>&1); then
	if [ "$(calls)" -gt "$before" ]; then ok "生成器跑了"; else bad "生成器没跑"; fi
	if g show --name-only --format= HEAD | grep -q "llms/en/00-core.md"; then ok "合集在这次提交里"; else bad "合集没进这次提交"; fi
else
	bad "提交失败：$out"
fi

echo "▶ 只改了别的文件：什么都不做"
before=$(calls)
echo "y" >>"$repo/other.txt"
g add other.txt
if (cd "$repo" && g commit -qm other >/dev/null 2>&1) && [ "$(calls)" -eq "$before" ]; then
	ok "没跑生成器，提交照常"
else
	bad "改的不是文档，却跑了生成器或提交失败"
fi

echo "▶ 文档只暂存了一部分：拒绝"
echo "staged" >>"$repo/$A"
g add "$A"
echo "unstaged" >>"$repo/$A"
if out=$(cd "$repo" && g commit -qm half 2>&1); then
	bad "半暂存的文档却提交成功了"
else
	if echo "$out" | grep -q "$A"; then ok "拒绝了，并点名 $A"; else bad "拒绝了，但没点名文件：$out"; fi
fi
g add "$A"
g commit -qm half-done --no-verify

echo "▶ 有没跟踪的新文档：拒绝"
echo "# new" >"$repo/$NEW"
echo "again" >>"$repo/$A"
g add "$A"
if out=$(cd "$repo" && g commit -qm untracked 2>&1); then
	bad "有没跟踪的新文档，却提交成功了"
else
	if echo "$out" | grep -q "$NEW"; then ok "拒绝了，并点名 $NEW"; else bad "拒绝了，但没点名文件：$out"; fi
fi
rm "$repo/$NEW"

echo "▶ git commit <路径>（只提交这几个文件）：拒绝"
# 这种模式下 git 让钩子对着一个临时暂存区跑：钩子 git add 的合集进了这次提交，真正的暂存区却还是旧的，
# 下一个毫不相干的提交会把旧合集悄悄写回去
echo "only" >>"$repo/$A"
if out=$(cd "$repo" && g commit -qm only -- "$A" 2>&1); then
	bad "带路径的提交却成功了：真正的暂存区会留着旧合集"
else
	if echo "$out" | grep -q "不带路径"; then ok "拒绝了，并说明要不带路径地提交"; else bad "拒绝了，但没说怎么办：$out"; fi
fi
g add "$A"
g commit -qm only-done --no-verify

echo "▶ 规划文档（docs/superpowers/）不归钩子管"
before=$(calls)
mkdir -p "$repo/$top/superpowers/specs"
echo "# draft" >"$repo/$top/superpowers/specs/draft.md"
echo "# plan" >"$repo/$top/superpowers/plan.md"
g add "$top/superpowers/plan.md"
if (cd "$repo" && g commit -qm plan >/dev/null 2>&1) && [ "$(calls)" -eq "$before" ]; then
	ok "只提交规划文档：没跑生成器，没被没跟踪的草稿拦下"
else
	bad "规划文档的提交被拦下，或者跑了生成器"
fi
echo "draft staged" >>"$repo/$A"
g add "$A"
if (cd "$repo" && g commit -qm with-draft >/dev/null 2>&1); then
	ok "有没跟踪的规划草稿时，文档提交照常"
else
	bad "一份没跟踪的规划草稿拦下了文档提交"
fi

echo "▶ 没有 go：提醒一句，提交照常"
# PATH 只放钩子与 git 用得到的几样（这台机器的 go 可能就在 /usr/bin 里，不能直接用系统 PATH）
mkdir -p "$tmp/nogo"
for t in git sed sh; do ln -s "$(command -v "$t")" "$tmp/nogo/$t"; done
echo "no go" >>"$repo/$A"
g add "$A"
if out=$(cd "$repo" && PATH="$tmp/nogo" g commit -qm nogo 2>&1); then
	if echo "$out" | grep -q "make lint"; then ok "提醒了，提交成功"; else bad "提交成功，但没提醒：$out"; fi
else
	bad "没有 go 时不该拦下提交：$out"
fi

echo
if [ "$fail" -gt 0 ]; then
	echo "❌ 提交钩子检查：${pass} 过 / ${fail} 败"
	exit 1
fi
echo "✅ 提交钩子检查全过（${pass} 项）"
