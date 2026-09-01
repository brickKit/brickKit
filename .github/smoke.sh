#!/usr/bin/env bash
# 发布前的冒烟：拿**真正要发出去的那个二进制**跑一遍（《发布与分发》§3.1）。
#
# 只覆盖不需要 Docker 的那一半：version / init / add --local / up --dry-run。
# 起容器与 K8s 那条线由试用指南在 Linux 上覆盖，这里要确认的是另一件事——
# 这个平台的二进制**根本跑得起来**。exec format error、缺动态库、路径分隔符
# 搞错，都会在这四条命令里现形。
#
# 用法：bash .github/smoke.sh <brickkit 路径> [组件源码目录]
set -euo pipefail

BK="${1:?用法：smoke.sh <brickkit 路径> [组件源码目录]}"
# 用仓库里真实的自测组件，不现编一个——现编的 manifest 会跟着 schema 悄悄过期，
# 而它过期时冒烟会失败在"组件写错了"，不是"二进制坏了"，浪费一次发布。
SRC="${2:-$(cd "$(dirname "$0")/.." && pwd)/tests/components/demo-hello}"
[ -f "$SRC/component.yaml" ] || {
	echo "❌ 找不到组件源码：$SRC/component.yaml" >&2
	exit 1
}

chmod +x "$BK" 2>/dev/null || true

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cd "$work"

echo "▶ version"
"$BK" version --log-level off

echo "▶ init"
# init 在**当前目录**里初始化，不新建子目录——所以先进一个空目录再 init
"$BK" init smoke-shop --log-level off

# init 已经把 components/ 配成了本地安装源 local-dev，按 <scope>/<name> 放进去即可
# 整份拷过去而不是只拷 component.yaml：manifest 里声明了 openapi.json 这个
# 产物，只拷 yaml 的话 add 会打印一条"产物下载失败"的警告——冒烟日志里出现
# 警告，下一个人就得花时间确认它是不是真问题。
mkdir -p components/demo/hello
cp -R "$SRC"/. components/demo/hello/

echo "▶ add --local"
"$BK" add --local --yes --log-level off

echo "▶ up --dry-run"
"$BK" up --dry-run --log-level off

echo "✅ 冒烟通过"
