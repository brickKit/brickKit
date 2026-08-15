#!/usr/bin/env bash
# 试用指南的准备脚本：编译 CLI、准备试验场、检查环境。
#
#   ./试用指南/准备.sh            准备（已存在则保留）
#   ./试用指南/准备.sh --reset    删掉旧试验场，重新准备一份干净的
#
# 做的事情只有三件，没有任何魔法：
#   1. go build 出 试用指南/bin/brickkit
#   2. 把 tests/components/ 下的真实组件按 <scope>/<name> 布局拷进试验场
#      （本地安装源要求这个布局，见 internal/source/local.go）
#   3. 如果装了 minikube 但没有独立的 kubectl，把 minikube 自带的那个链过来
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GUIDE="$ROOT/试用指南"
BIN="$GUIDE/bin"
PLAY="$GUIDE/playground"

if [[ "${1:-}" == "--reset" ]]; then
	echo "🧹 删除旧试验场：$PLAY"
	rm -rf "$PLAY"
fi

# ===== 1. 编译 CLI =====
mkdir -p "$BIN"
echo "🔨 编译 brickkit ..."
(cd "$ROOT" && go build -o "$BIN/brickkit" ./cmd/brickkit)
echo "   $BIN/brickkit"

# ===== 2. 准备试验场 =====
# 组件源码按 <scope>/<name> 布局拷过去：本地安装源用 root/<组件ID> 定位组件。
mkdir -p "$PLAY/components"
copy_component() {
	local src="$ROOT/tests/components/$1" dst="$PLAY/components/$2"
	[[ -d "$dst" ]] && return 0
	mkdir -p "$(dirname "$dst")"
	cp -r "$src" "$dst"
}
copy_component demo-hello demo/hello
copy_component demo-caller demo/caller
copy_component department-tree department/tree
copy_component people-basic people/basic
echo "📦 试验组件已就位：$PLAY/components/"
echo "   demo/hello  demo/caller  department/tree  people/basic"

# ===== 3. kubectl（可选）=====
if ! command -v kubectl >/dev/null 2>&1; then
	CACHED="$(find "$HOME/.minikube/cache" -name kubectl -type f 2>/dev/null | sort | tail -1 || true)"
	if [[ -n "$CACHED" ]]; then
		ln -sf "$CACHED" "$BIN/kubectl"
		echo "☸️  没有独立的 kubectl，已链接 minikube 自带的那个：$BIN/kubectl"
	fi
fi

# ===== 4. 环境检查 =====
echo
echo "🔍 环境："
check() { printf "   %-12s %s\n" "$1" "$(${2} 2>&1 | head -1 || echo '未安装')"; }
check go "go version"
check docker "docker --version"
command -v minikube >/dev/null 2>&1 && check minikube "minikube version --short" || printf "   %-12s 未安装（07 章需要）\n" minikube

cat <<EOF

✅ 准备完成。把这一行加到当前终端（每开一个新终端都要执行一次）：

   export PATH="$BIN:\$PATH"

然后从 01 开始：

   cd "$PLAY"
   less "$GUIDE/01-初始化项目.md"
EOF
