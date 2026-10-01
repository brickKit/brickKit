#!/usr/bin/env bash
# 用真的 git 仓库核对 .github/release-notes.sh：
#   - 正文以 tag 里的发版说明开头，Markdown 的 # 标题一行不少（release.sh 用 --cleanup=verbatim 写进去）；
#   - "完整变动"链接指向按版本号排在它前面的那个 tag；
#   - 轻量 tag（没有说明）让流水线失败，而不是发一个没有说明的版本。
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
script="$root/.github/release-notes.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

fail() {
	echo "❌ release-notes.sh：$1" >&2
	exit 1
}

cd "$work"
git init -q
git -c user.name=t -c user.email=t@t commit -q --allow-empty -m one
git -c user.name=t -c user.email=t@t tag -a v0.9.0 -m "old"
git -c user.name=t -c user.email=t@t commit -q --allow-empty -m two
printf '## Highlights\n\n- first\n\n# not a comment\n' >notes.md
git -c user.name=t -c user.email=t@t tag -a v1.0.0 --cleanup=verbatim -F notes.md
git tag v1.0.1-light

out="$(bash "$script" v1.0.0 owner/repo)"
[ "$(printf '%s\n' "$out" | head -1)" = "## Highlights" ] || fail "正文没有以发版说明开头"
printf '%s\n' "$out" | grep -qx '# not a comment' || fail "说明里 # 开头的行丢了"
printf '%s\n' "$out" | grep -q '^## Install' || fail "安装说明没了"
printf '%s\n' "$out" | grep -qF 'https://github.com/owner/repo/compare/v0.9.0...v1.0.0' || fail "完整变动链接不对"

if bash "$script" v1.0.1-light owner/repo >/dev/null 2>&1; then
	fail "轻量 tag 没有说明，却没有失败"
fi
first="$(bash "$script" v0.9.0 owner/repo)"
printf '%s\n' "$first" | grep -q 'Full changelog' && fail "第一个版本没有上一个版本，不该有完整变动链接"

echo "✅ 发版说明：取自 tag、# 标题保留、完整变动链接正确、没说明就失败"
