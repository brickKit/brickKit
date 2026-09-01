#!/usr/bin/env bash
# 检查 install.sh 真的能装、也真的会拒绝装（《发布与分发》§7）。
#
# # 为什么这个检查必须存在
#
# install.sh 是**唯一一条不需要 Go 就能拿到 CLI 的路**，而它坏掉的方式最难
# 被发现：校验和逻辑退化成"永远通过"不会有任何症状，直到某天真有一个包被
# 换掉。所以这里不只验"能装成功"，还专门把校验和改坏，确认它**真的会失败**。
#
# 一个不会失败的校验等于没有校验。
#
# 造一个 file:// 的假 release（只出本机平台这一个包，因为快），让 install.sh
# 走完整条真实路径：下载 → 校验 → 解包 → 安装 → 报告。
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
FAKE_VERSION="v9.9.9"
FAKE_BARE="9.9.9"

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

# ---------- 1. shellcheck（有就跑，没有就响亮跳过）----------
echo "▶ shellcheck"
if command -v shellcheck >/dev/null 2>&1; then
	if shellcheck --shell=sh install.sh && shellcheck scripts/release.sh scripts/check-install-sh.sh; then
		ok "shellcheck 通过"
	else
		bad "shellcheck 有问题（见上）"
	fi
else
	# 不假装通过。装法：apt install shellcheck / brew install shellcheck
	echo "   ⏭  跳过：本机没装 shellcheck（apt install shellcheck 或 brew install shellcheck）"
fi

# ---------- 2. 造假 release ----------
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

os="$(uname -s)"
case "$os" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*)
	echo "❌ 这个检查只能在 linux / darwin 上跑，当前是 ${os}"
	exit 1
	;;
esac
arch="$(uname -m)"
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*)
	echo "❌ 这个检查只能在 amd64 / arm64 上跑，当前是 ${arch}"
	exit 1
	;;
esac

echo "▶ 造一个 file:// 假 release（${os}/${arch}）"
stage="$tmp/stage"
rel="$tmp/release/$FAKE_VERSION"
mkdir -p "$stage" "$rel" "$tmp/bin"
go build -trimpath -ldflags "-X 'github.com/brickkit/brickkit/internal/version.Version=${FAKE_VERSION}'" \
	-o "$stage/brickkit" ./cmd/brickkit ||
	{
		echo "❌ 构建不出用来测的二进制"
		exit 1
	}
archive="brickkit_${FAKE_BARE}_${os}_${arch}.tar.gz"
tar -czf "$rel/$archive" -C "$stage" brickkit
(cd "$rel" && sha256sum "$archive" >checksums.txt)
ok "假 release 就绪：$archive"

run_install() {
	# $1 = 装到哪；其余环境变量由调用方 export
	env BRICKKIT_VERSION="$FAKE_VERSION" \
		BRICKKIT_BASE_URL="file://$tmp/release" \
		BRICKKIT_INSTALL_DIR="$1" \
		sh "$ROOT/install.sh" 2>&1
}

# ---------- 3. 正常路径 ----------
echo "▶ 正常安装"
if out="$(run_install "$tmp/bin")"; then
	if [ -x "$tmp/bin/brickkit" ]; then
		got="$("$tmp/bin/brickkit" version --log-level off | head -1)"
		if [ "$got" = "BrickKit CLI ${FAKE_VERSION}" ]; then
			ok "装上了，且版本号对得上：$got"
		else
			bad "装上了，但版本号是「$got」，期望「BrickKit CLI ${FAKE_VERSION}」"
		fi
	else
		bad "退出码是 0，但 $tmp/bin/brickkit 不存在（假装成功了）"
	fi
else
	bad "正常安装失败：$out"
fi

# ---------- 4. 校验和被改坏 ----------
#
# 这一条是整个脚本存在的理由。
echo "▶ 校验和被改坏时必须拒绝安装"
sed -i.bak 's/^./0/' "$rel/checksums.txt" # 把第一个字符改掉，其余不动
if out="$(run_install "$tmp/bin2")"; then
	bad "校验和是错的，它却装成功了——这正是这个检查要防的那种坏法"
else
	if echo "$out" | grep -q "校验失败"; then
		ok "拒绝安装，且报的是校验失败"
	else
		# 它失败了，但不是因为校验——比如下载就挂了。那样的话，就算校验逻辑
		# 整个删掉这一条也照样"通过"，等于没测。
		bad "拒绝安装了，但原因不是校验失败：$(echo "$out" | head -2 | tr '\n' ' ')"
	fi
	if [ -e "$tmp/bin2/brickkit" ]; then
		bad "校验失败了，却还是往 $tmp/bin2 里留下了东西"
	else
		ok "没有留下任何半成品"
	fi
fi
mv "$rel/checksums.txt.bak" "$rel/checksums.txt"

# ---------- 5. 版本不存在 ----------
echo "▶ 版本不存在时报得清楚"
if out="$(env BRICKKIT_VERSION=v0.0.404 BRICKKIT_BASE_URL="file://$tmp/release" \
	BRICKKIT_INSTALL_DIR="$tmp/bin3" sh "$ROOT/install.sh" 2>&1)"; then
	bad "版本不存在，它却成功了"
elif echo "$out" | grep -q "下载失败"; then
	ok "报了下载失败，并带上了 URL"
else
	bad "失败了，但没说清是下载失败：$(echo "$out" | head -2 | tr '\n' ' ')"
fi

# ---------- 6. PATH 提示 ----------
echo "▶ 装到不在 PATH 的目录时要提醒"
if run_install "$tmp/bin4" | grep -q "不在 PATH 里"; then
	ok "提醒了"
else
	bad "没提醒——装完了却敲不到 brickkit，最容易被当成没装上"
fi

echo
if [ "$fail" -gt 0 ]; then
	echo "❌ install.sh 检查：${pass} 过 / ${fail} 败"
	exit 1
fi
echo "✅ install.sh 检查全过（${pass} 项）"
