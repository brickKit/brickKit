#!/usr/bin/env bash
# 打 tag 并推送，触发 GitHub Actions 发布（《发布与分发》§4.2）。
#
# 用法：make release VERSION=0.1.0
#
# # 为什么检查这么多条
#
# push 一个 tag 会立刻发布一个**公开** release，别人当场就能下走。撤不干净——
# 删了 tag 和 release 页面，已经下载的那份还在，而且 GitHub 的 CDN 与各种镜像
# 也不保证跟着消失。所以宁可在 push 之前多拦几次，也不要事后补救。
#
# 每一条不过都**不 push 任何东西**，包括 tag 本身：本地打了远端没推，
# 下次再跑就会撞上"tag 已存在"，反而更难收拾。所以顺序是先全查完，再打再推。
set -euo pipefail

REPO_SLUG="brickKit/brickKit"

die() {
	echo "❌ $1" >&2
	shift
	for line in "$@"; do echo "   $line" >&2; done
	echo "   —— 没有打 tag，也没有 push 任何东西。" >&2
	exit 1
}

raw_version="${1:-}"
version="${raw_version#v}"
tag="v${version}"

# ---- 1. 版本号格式 ----
#
# VERSION 不传时，Makefile 会把 git describe 的结果送进来（比如 0.1.0-dev 或
# v0.1.0-3-g1234abc）。那些都不是能发布的版本号，必须在这里挡掉——否则会打出
# 一个形如 v0.1.0-dev 的 tag，而 CI 会当真去发布它。
if ! [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	die "版本号 '${raw_version}' 不是 X.Y.Z" \
		"用法：make release VERSION=0.1.0" \
		"（不传 VERSION 时 Makefile 会拿 git describe 的结果顶上，那个不能用来发布）"
fi

# ---- 2. 分支 ----
branch="$(git rev-parse --abbrev-ref HEAD)"
if [ "$branch" != "main" ]; then
	die "当前在 ${branch} 分支，发布只从 main 打 tag" \
		"先把改动合进 main 再发布"
fi

# ---- 3. 工作区干净 ----
#
# 不只是洁癖：LDFLAGS 里的 COMMIT 取自 HEAD，工作区脏意味着发出去的二进制
# 里刻的那个 commit 不等于它真正的源码。
if [ -n "$(git status --porcelain)" ]; then
	die "工作区不干净" \
		"发出去的二进制里刻的 commit 取自 HEAD，工作区脏就对不上源码" \
		"先提交或 stash：git status"
fi

# ---- 4. 与 origin/main 同步 ----
echo "▶ git fetch origin"
git fetch --quiet origin main --tags
local_head="$(git rev-parse HEAD)"
remote_head="$(git rev-parse origin/main)"
if [ "$local_head" != "$remote_head" ]; then
	die "本地 main 与 origin/main 不一致" \
		"本地 ${local_head:0:7} / 远端 ${remote_head:0:7}" \
		"CI 是从远端那个 commit 构建的，两边不一样就会发出你没测过的东西" \
		"先 git pull / git push"
fi

# ---- 5. tag 还不存在 ----
if git rev-parse -q --verify "refs/tags/${tag}" >/dev/null; then
	die "本地已经有 tag ${tag}" "改用下一个版本号，或先 git tag -d ${tag}"
fi
if [ -n "$(git ls-remote --tags origin "refs/tags/${tag}")" ]; then
	die "远端已经有 tag ${tag}" \
		"这个版本已经发过了——版本号只发一次，改用下一个"
fi

# ---- 6. 检查全绿 ----
#
# 放在确认之前：宁可让人多等几分钟，也不要让人点了 y 之后才发现要重来。
echo "▶ make lint test-unit"
"${MAKE:-make}" --no-print-directory lint test-unit \
	|| die "lint 或单元测试没过" "上面有具体是哪一条。修完再发布"

# ---- 确认 ----
echo
echo "即将发生的事："
echo "  1. 打 tag ${tag}（指向 ${local_head:0:7}）"
echo "  2. git push origin ${tag}"
echo "  3. GitHub Actions 随即构建五个平台的产物，并发布一个**公开** release"
echo
echo "     https://github.com/${REPO_SLUG}/releases/tag/${tag}"
echo
if [ "${CONFIRM:-}" = "yes" ]; then
	echo "CONFIRM=yes，跳过确认。"
elif [ -t 0 ]; then
	read -r -p "确认发布 ${tag}？[y/N] " answer
	case "$answer" in
	y | Y | yes) ;;
	*) die "已取消" ;;
	esac
else
	# 管道、CI、后台任务里没有终端可以问。这里默认放行的话，一个手滑的
	# `echo | make release` 就会发出一个公开版本。
	die "非交互环境不默认放行" "确实要发的话显式写：CONFIRM=yes make release VERSION=${version}"
fi

# ---- 打 tag 并推送 ----
git tag -a "$tag" -m "BrickKit ${tag}"
git push origin "$tag"

echo
echo "✅ 已推送 ${tag}，CI 接手了。"
echo "   构建过程：https://github.com/${REPO_SLUG}/actions"
echo "   发布结果：https://github.com/${REPO_SLUG}/releases/tag/${tag}"
echo
echo "   产物齐了之后，装一份验一下："
echo "   curl -fsSL https://raw.githubusercontent.com/${REPO_SLUG}/main/install.sh | sh"
