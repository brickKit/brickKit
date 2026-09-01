#!/bin/sh
# BrickKit CLI 安装脚本（《发布与分发》§6）
#
#   curl -fsSL https://raw.githubusercontent.com/brickKit/brickKit/main/install.sh | sh
#
# 环境变量：
#   BRICKKIT_VERSION      装哪个版本，如 v0.1.0（默认：最新 release）
#   BRICKKIT_INSTALL_DIR  装到哪（默认：/usr/local/bin，不可写则 ~/.local/bin）
#   BRICKKIT_BASE_URL     产物从哪下（默认：GitHub Releases；测试用 file:// 覆盖）
#
# 只管 macOS 与 Linux。Windows 请手动下 zip——23 篇试用指南全是 bash，
# Windows 上的 Docker 流程一次都没验过，装脚本不假装支持它。
#
# 这里刻意用 POSIX sh 而不是 bash：Alpine 之类的镜像里 /bin/sh 是 dash，
# 而"装 CLI"恰恰是那种要在最简陋的环境里跑通的事。
set -eu

REPO="brickKit/brickKit"
BASE_URL="${BRICKKIT_BASE_URL:-https://github.com/${REPO}/releases/download}"

info() { printf '%s\n' "$1"; }
die() {
	printf '❌ %s\n' "$1" >&2
	shift
	for line in "$@"; do printf '   %s\n' "$line" >&2; done
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || die "缺少 $1" "$2"
}

# ---- 1. 认平台 ----
#
# 认不出来就报错列出支持的四个，不猜。猜错的下场是装上一个跑不了的二进制，
# 而报错信息会指向 exec format error——那时候没人会想到是安装脚本猜的。
os="$(uname -s)"
arch="$(uname -m)"
case "$os" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*)
	die "不支持的系统：${os}" \
		"install.sh 只管 linux 与 darwin（macOS）" \
		"Windows 请到 https://github.com/${REPO}/releases 手动下 windows_amd64.zip"
	;;
esac
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*)
	die "不支持的架构：${arch}" \
		"预编译产物只有 amd64 与 arm64" \
		"其他架构请自己编：go install github.com/brickkit/brickkit/cmd/brickkit@latest"
	;;
esac

need curl "先装 curl 再来"
need tar "先装 tar 再来"

# sha256 工具在两个平台上名字不一样：Linux 是 sha256sum，macOS 是 shasum -a 256。
if command -v sha256sum >/dev/null 2>&1; then
	sha_cmd="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
	sha_cmd="shasum -a 256"
else
	die "找不到 sha256sum 或 shasum" \
		"校验和是这个脚本唯一的安全保证，没有它就不装——这不是可以跳过的一步"
fi

# ---- 2. 定版本 ----
version="${BRICKKIT_VERSION:-}"
if [ -z "$version" ]; then
	# 不用 GitHub API：它对未认证请求限流 60 次/小时，共用出口 IP 的公司网络
	# 很容易撞上。/releases/latest 会 302 到 /releases/tag/vX.Y.Z，从 Location
	# 里取版本号不消耗任何配额。
	version="$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
		"https://github.com/${REPO}/releases/latest" 2>/dev/null |
		sed -n 's#.*/tag/\(.*\)$#\1#p')"
	[ -n "$version" ] || die "查不到最新版本" \
		"网络不通，或者这个仓库还没有发布过任何 release" \
		"指定版本再试：BRICKKIT_VERSION=v0.1.0 sh install.sh"
fi
# 产物名里的版本号不带 v
bare_version="${version#v}"

archive="brickkit_${bare_version}_${os}_${arch}.tar.gz"

# ---- 3. 下载 ----
tmp="$(mktemp -d)"
# trap 保证异常退出也清干净——尤其是校验失败那条路，绝不能把半个包留在盘上
# 让人以为"下下来了，手动装一下就行"。
trap 'rm -rf "$tmp"' EXIT INT TERM

info "▶ 下载 ${archive}（${version}）"
curl -fsSL -o "${tmp}/${archive}" "${BASE_URL}/${version}/${archive}" ||
	die "下载失败：${BASE_URL}/${version}/${archive}" \
		"确认这个版本存在：https://github.com/${REPO}/releases"
curl -fsSL -o "${tmp}/checksums.txt" "${BASE_URL}/${version}/checksums.txt" ||
	die "下载 checksums.txt 失败" "没有校验和就不装"

# ---- 4. 校验 ----
#
# 只挑自己这一个包的那一行来校，而不是 sha256sum -c 整个 checksums.txt——
# 后者会因为"另外四个包不在本地"而失败，那种失败会被人当成噪音学会忽略。
info "▶ 校验 sha256"
expected="$(grep " ${archive}\$" "${tmp}/checksums.txt" | awk '{print $1}')"
[ -n "$expected" ] || die "checksums.txt 里没有 ${archive} 这一行" \
	"产物与校验和文件对不上，这不正常，别装"
actual="$(cd "$tmp" && $sha_cmd "$archive" | awk '{print $1}')"
if [ "$expected" != "$actual" ]; then
	rm -f "${tmp}/${archive}"
	die "校验失败：${archive}" \
		"期望 ${expected}" \
		"实际 ${actual}" \
		"包可能被篡改，或者下载不完整。已删掉，没有安装。"
fi

# ---- 5. 装到哪 ----
if [ -n "${BRICKKIT_INSTALL_DIR:-}" ]; then
	install_dir="$BRICKKIT_INSTALL_DIR"
	mkdir -p "$install_dir" 2>/dev/null ||
		die "创建不了 ${install_dir}" "换一个 BRICKKIT_INSTALL_DIR"
	[ -w "$install_dir" ] || die "${install_dir} 不可写" "换一个 BRICKKIT_INSTALL_DIR"
elif [ -w /usr/local/bin ]; then
	install_dir=/usr/local/bin
else
	# 不替用户 sudo。要提权是他自己的决定，不该由一条 curl | sh 代劳。
	install_dir="${HOME}/.local/bin"
	mkdir -p "$install_dir" ||
		die "创建不了 ${install_dir}" "用 BRICKKIT_INSTALL_DIR 指一个能写的目录"
fi

tar -xzf "${tmp}/${archive}" -C "$tmp" brickkit ||
	die "解包失败：${archive}"
# 先装到同目录的临时名再 mv：直接覆盖一个正在运行的二进制会得到
# "text file busy"，而 mv 是原子的。
mv "${tmp}/brickkit" "${install_dir}/.brickkit.new" ||
	die "写不进 ${install_dir}"
chmod 0755 "${install_dir}/.brickkit.new"
mv "${install_dir}/.brickkit.new" "${install_dir}/brickkit"

# ---- 6. 说清楚装到哪了 ----
info ""
info "✅ ${install_dir}/brickkit"
info "   $("${install_dir}/brickkit" version --log-level off 2>/dev/null | head -1)"

case ":${PATH}:" in
*":${install_dir}:"*) ;;
*)
	info ""
	info "⚠️  ${install_dir} 不在 PATH 里，加上这一行（写进 ~/.bashrc 或 ~/.zshrc）："
	info ""
	info "    export PATH=\"${install_dir}:\$PATH\""
	;;
esac
