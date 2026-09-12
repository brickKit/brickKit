#!/usr/bin/env bash
# 试用指南的准备脚本：编译 CLI、构建镜像、把试验场推到某个**命名状态**。
#
#   ./试用指南/准备.sh              准备（已存在的组件源码保留）
#   ./试用指南/准备.sh --reset      删掉旧试验场，重新准备一份干净的
#   ./试用指南/准备.sh --images     构建全部组件镜像（= make demo-images）
#   ./试用指南/准备.sh --baseline   推到「五组件基准」：03 往后统一用的起点
#
# # 为什么要有"命名状态"
#
# 早先每一篇的前置写的是"N 篇做完"。那意味着状态藏在前面几篇的操作里：
# 中间任何一步留下副作用，后面就全塌，而且塌得很难懂（真发生过——02 §2.6 的
# remove 会删掉组件源码目录，于是后面 add --local 扫到的组件少一个，直接中止）。
#
# 现在每一篇的前置都是**一条命令**。想从哪一篇开始就从哪一篇开始，
# 试花了就再跑一次——reset 是常规手段，不是应急措施。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GUIDE="$ROOT/试用指南"
BIN="$GUIDE/bin"
PLAY="$GUIDE/playground"

MODE="${1:-}"
case "$MODE" in
	"" | --reset | --images | --baseline) ;;
	*)
		echo "❌ 不认识的参数：$MODE"
		echo "   可用：--reset / --images / --baseline（不带参数 = 准备但保留已有源码）"
		exit 2
		;;
esac

# --baseline 隐含 --reset：基准状态的意义就在于"每次都一样"。
# 在旧配置上叠一次 add 得到的东西，取决于旧配置是什么。
if [[ "$MODE" == --reset || "$MODE" == --baseline ]]; then
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
echo "📦 试验组件已就位：$PLAY/components/（10 个）"
if [[ $KEPT -gt 0 ]]; then
	echo "   ℹ️  其中 $KEPT 个组件目录已存在，**保留了你的改动**（没有覆盖）"
	echo "      要回到初始状态：./试用指南/准备.sh --reset"
fi

# ===== 3. 构建镜像（--images）=====
# 交给 make demo-images：镜像名与标签的唯一事实源在 Makefile 里，
# 在这里再抄一份，早晚会和它对不上（08 要的 hello:2.0.0 就差点被漏掉）。
if [[ "$MODE" == --images ]]; then
	echo
	echo "🐳 构建组件镜像（make demo-images，第一次要几分钟）..."
	(cd "$ROOT" && make demo-images)
fi

# ===== 4. kubectl（可选）=====
if ! command -v kubectl >/dev/null 2>&1; then
	CACHED="$(find "$HOME/.minikube/cache" -name kubectl -type f 2>/dev/null | sort | tail -1 || true)"
	if [[ -n "$CACHED" ]]; then
		ln -sf "$CACHED" "$BIN/kubectl"
		echo "☸️  没有独立的 kubectl，已链接 minikube 自带的那个：$BIN/kubectl"
	fi
fi

# ===== 5. 基准状态（--baseline）=====
# 03–08 与 19 统一从这里出发：demo-shop 项目 + 五个组件
# （demo/hello、demo/caller、department/tree、infra/redis-event-bus、people/basic）。
#
# 用 add 而不是直接写一份 brickkit.yaml：那样连"依赖是被自动拉进来的"
# 这件事都一起验了，而写死的配置只能证明解析器认得它。
if [[ "$MODE" == --baseline ]]; then
	echo
	echo "🎯 推到五组件基准状态 ..."
	(
		cd "$PLAY"
		"$BIN/brickkit" init demo-shop --log-level off >/dev/null
		"$BIN/brickkit" add demo/caller@1.0.0 --yes --log-level off >/dev/null
		"$BIN/brickkit" add people/basic@1.0.0 --yes --log-level off >/dev/null
	)
	echo "   项目：$PLAY（demo-shop）"
	echo "   组件：demo/hello  demo/caller  department/tree  infra/redis-event-bus  people/basic"
fi

# ===== 6. 环境检查 =====
echo
echo "🔍 环境："
check() { printf "   %-12s %s\n" "$1" "$(${2} 2>&1 | head -1 || echo '未安装')"; }
check go "go version"
check docker "docker --version"
if command -v minikube >/dev/null 2>&1; then
	check minikube "minikube version --short"
else
	printf "   %-12s 未安装（06 / 09 / 16 / 18 需要）\n" minikube
fi

echo
echo "✅ 准备完成。把这一行加到当前终端（每开一个新终端都要执行一次）："
echo
echo "   export PATH=\"$BIN:\$PATH\""
echo
case "$MODE" in
	--baseline)
		cat <<EOF
基准状态已就位，可以直接开始 03（或 04 / 05 / 07 / 08 / 19）：

   cd "$PLAY"
   brickkit up --dry-run
EOF
		;;
	--images)
		cat <<EOF
镜像已构建。要跑 05 及之后的 Docker 篇，先推到基准状态：

   ./试用指南/准备.sh --baseline
EOF
		;;
	*)
		cat <<EOF
然后从 01 开始：

   cd "$PLAY"
   less "$GUIDE/01-初始化项目.md"

💡 第一次来的话，先把镜像构建了（05 之后每一篇都要）：

   ./试用指南/准备.sh --images
EOF
		;;
esac
