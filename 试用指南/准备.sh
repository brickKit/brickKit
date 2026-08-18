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
KEPT=0
copy_component() {
	local src="$ROOT/tests/components/$1" dst="$PLAY/components/$2"
	# 已存在就保留：试验场是给你随便改的，重跑本脚本不该把你的改动冲掉。
	# 但要**说出来**——否则改过组件之后重跑一次，会以为已经恢复成初始状态了。
	if [[ -d "$dst" ]]; then
		KEPT=$((KEPT + 1))
		return 0
	fi
	mkdir -p "$(dirname "$dst")"
	cp -r "$src" "$dst"
}
# 两个玩具组件：讲依赖、级联、多版本时用它们，起得快、看得清
copy_component demo-hello demo/hello
copy_component demo-caller demo/caller
# 八个真实组件：完整装配、强弱依赖、双协议、缓存、事件、前端、文档聚合
copy_component department-tree department/tree
copy_component people-basic people/basic
copy_component auth-password-login auth/password-login
copy_component authorization-rbac authorization/rbac
copy_component erp-backend erp/backend
copy_component portal-user-frontend portal/user-frontend
copy_component infra-redis-event-bus infra/redis-event-bus
copy_component infra-api-docs infra/api-docs
echo "📦 试验组件已就位：$PLAY/components/"
if [[ $KEPT -gt 0 ]]; then
	echo "   ℹ️  其中 $KEPT 个组件目录已存在，**保留了你的改动**（没有覆盖）"
	echo "      要回到初始状态：./试用指南/准备.sh --reset"
fi
echo "   玩具：demo/hello  demo/caller"
echo "   真实：department/tree  people/basic  auth/password-login  authorization/rbac"
echo "         erp/backend  portal/user-frontend  infra/redis-event-bus  infra/api-docs"

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
