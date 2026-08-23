#!/usr/bin/env bash
# 试用指南冒烟检查：把清单里的步骤真跑一遍，核对输出。
#
#   ./scripts/check-guides.sh            跑能跑的层（缺环境的响亮跳过）
#   ./scripts/check-guides.sh core       只跑 core 层
#
# # 它守什么
#
# 指南里的命令与参数由 make check-cli-docs 保证"存在"，但存在不等于**跑得通**。
# 这个脚本真的执行，并核对输出里出现了该出现的东西。
#
# # 为什么只挑几步，不跑全部 23 篇
#
# 全跑要 Docker + minikube + 市场 + cosign 全在，十几分钟，任何一环境漂移就整片红——
# 那样的检查两周内就会被加上 `|| true`。这里只挑"断了就说明平台真坏了"的步骤。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LIST="$ROOT/tests/guides/清单.tsv"
ONLY="${1:-}"

WORK="$(mktemp -d)"
trap 'cd "$ROOT" 2>/dev/null; [ -n "${PROJ:-}" ] && [ -d "$PROJ" ] && (cd "$PROJ" && "$BIN" down >/dev/null 2>&1); rm -rf "$WORK"' EXIT

# ===== 1. 准备 CLI 与试验组件 =====
BIN="$ROOT/bin/brickkit"
if [[ ! -x "$BIN" ]]; then
	echo "🔨 编译 CLI ..."
	(cd "$ROOT" && go build -o "$BIN" ./cmd/brickkit) || { echo "❌ 编译失败"; exit 2; }
fi

PROJ="$WORK/proj"
mkdir -p "$PROJ"
# 组件按 <scope>/<name> 布局，本地安装源要求这个（internal/source/local.go）
for pair in demo-hello:demo/hello demo-caller:demo/caller; do
	src="$ROOT/tests/components/${pair%%:*}" dst="$PROJ/components/${pair##*:}"
	mkdir -p "$(dirname "$dst")" && cp -r "$src" "$dst"
done

cd "$PROJ"
# 刻意**不**预先 init：清单第一条就是"init 能建出项目"，先建好的话
# 那条验的就成了"重复 init 会报错"，与它自称验的东西不是一回事。

# ===== 2. 环境探测 =====
have_docker=0; docker info >/dev/null 2>&1 && have_docker=1
have_images=1
for i in hello caller; do
	docker image inspect "brickkit-demo/$i:1.0.0" >/dev/null 2>&1 || have_images=0
done
have_k8s=0; command -v minikube >/dev/null 2>&1 && minikube status >/dev/null 2>&1 && have_k8s=1

tier_ok() {
	case "$1" in
		core) return 0 ;;
		docker) [[ $have_docker -eq 1 && $have_images -eq 1 ]] ;;
		k8s) [[ $have_k8s -eq 1 ]] ;;
		*) return 1 ;;
	esac
}
# bind_pg 给项目补一段 PostgreSQL 资源声明并绑定给 demo/caller。
#
# `database` 与 `username` 同名是有意的：postgres 镜像默认建一个与
# POSTGRES_USER 同名的库，因此不需要额外的 CREATE DATABASE 步骤——
# 冒烟检查要能一条命令跑完，不该顺带考验使用者的建库操作。
bind_pg() {
	# 骨架里已经有一行 `resources: []`，必须**替换**它而不是追加——
	# 追加会得到两个 resources 键，YAML 直接判重复键。
	grep -q '^resources: \[\]$' "$PROJ/brickkit.yaml" || return 1
	python3 - "$PROJ/brickkit.yaml" <<-'PYEOF'
		import sys
		path = sys.argv[1]
		body = open(path, encoding="utf-8").read()
		open(path, "w", encoding="utf-8").write(body.replace("resources: []", """resources:
		  - kind: database
		    engine: postgresql
		    id: pg-smoke
		    host: pg
		    port: 5432
		    username: brickkit
		    password: smoke
		    bindings:
		      - componentId: demo/caller
		        database: brickkit""", 1))
	PYEOF
}

tier_why() {
	case "$1" in
		docker) [[ $have_docker -eq 0 ]] && echo "没有可用的 Docker" || echo "缺组件镜像（brickkit-demo/hello:1.0.0 等，见 试用指南/00-准备.md）" ;;
		k8s) echo "minikube 没在跑" ;;
	esac
}

# ===== 3. 逐条执行 =====
pass=0; fail=0; skipped=0; ran_tiers=""
declare -A SKIP_SHOWN

while IFS=$'\t' read -r tier what _env cmd expect; do
	[[ -z "${tier:-}" || "$tier" == \#* ]] && continue
	[[ -n "$ONLY" && "$tier" != "$ONLY" ]] && continue

	if ! tier_ok "$tier"; then
		if [[ -z "${SKIP_SHOWN[$tier]:-}" ]]; then
			echo "⏭  跳过 $tier 层：$(tier_why "$tier")"
			SKIP_SHOWN[$tier]=1
		fi
		skipped=$((skipped + 1))
		continue
	fi
	case " $ran_tiers " in *" $tier "*) ;; *) ran_tiers="$ran_tiers $tier" ;; esac

	# `!` 开头的是**夹具步骤**，不是 brickkit 命令：把项目推到某个状态，
	# 不比对输出。demo/caller 声明了 database 依赖，没有这一步的话
	# `up` 会正确地报"资源依赖未满足"——那不是指南坏了，是夹具不真实。
	if [[ "$cmd" == "!bind-pg" ]]; then
		bind_pg && printf "  ✅ [%s] %s\n" "$tier" "$what" && pass=$((pass + 1)) \
			|| { printf "  ❌ [%s] %s\n" "$tier" "$what"; fail=$((fail + 1)); }
		continue
	fi

	# add 在组件已存在时会交互式确认，给它 --yes；其余命令不认这个参数。
	# 早先写成"先带 --yes 跑、失败再裸跑一次"，结果 init 被执行了两次——
	# 一条命令跑两遍会让后面每一步都建立在错的状态上。
	extra=""
	[[ "$cmd" == add* ]] && extra="--yes"
	# shellcheck disable=SC2086
	out="$("$BIN" $cmd $extra 2>&1 </dev/null)"
	# 从前这里要调 seed-sources.py 给骨架补一段 sources——init 生成的骨架里没有。
	# 现在骨架自带 local-dev → ./components（D527），那一步连同脚本一起去掉了。
	if grep -qF -- "$expect" <<<"$out"; then
		printf "  ✅ [%s] %s\n" "$tier" "$what"
		pass=$((pass + 1))
	else
		printf "  ❌ [%s] %s\n" "$tier" "$what"
		printf "     命令：brickkit %s\n" "$cmd"
		printf "     期望输出里有：%s\n" "$expect"
		printf "     实际最后几行：\n"
		tail -4 <<<"$out" | sed 's/^/       /'
		fail=$((fail + 1))
	fi
done < "$LIST"

# ===== 4. 汇总 =====
echo
if [[ $pass -eq 0 ]]; then
	# 一条都没跑到与"全部通过"输出长得太像，必须区分开
	echo "❌ 一条都没执行（跳过 $skipped 条）。检查环境，或确认清单不是空的。"
	exit 2
fi
if [[ $fail -gt 0 ]]; then
	echo "❌ 指南冒烟：$pass 通过，$fail 失败，$skipped 跳过"
	echo "   指南里写的步骤跑不通了——照着做的人会卡在同一处。"
	exit 1
fi
echo "✅ 指南冒烟：$pass 条通过，$skipped 条跳过（层：$ran_tiers）"
