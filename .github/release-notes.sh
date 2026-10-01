#!/usr/bin/env bash
# 生成 release 页面的正文（发布流水线的 publish 阶段用它）。
#
# 抽成单独脚本而不是塞进 workflow 的 run 里：里面有反引号和 heredoc，写在 YAML
# 的块标量里要同时躲开 YAML 缩进、shell 展开和 Markdown 转义三层坑，而且**没法
# 在本地看一眼渲染结果**。抽出来就能：bash .github/release-notes.sh v0.1.0 owner/repo
set -euo pipefail

tag="${1:?用法：release-notes.sh <tag> <owner/repo>}"
repo="${2:?用法：release-notes.sh <tag> <owner/repo>}"
version="${tag#v}"

# 正文的开头是这个版本的发版说明：make release 写进带注释的 tag 里的那段 Markdown。
# 轻量 tag 的 %(contents) 是它指向的那个提交的说明，不是发版说明——所以先确认它是带注释的 tag。
# 没有说明就失败：流水线停在发布之前，宁可不发，也不发一个说不清改了什么的版本。
if [ "$(git for-each-ref "refs/tags/${tag}" --format='%(objecttype)')" != "tag" ]; then
	echo "❌ ${tag} 不是带注释的 tag（或本地没有它）：没有发版说明。用 make release VERSION=${version} NOTES=<文件> 发版" >&2
	exit 1
fi
body="$(git for-each-ref "refs/tags/${tag}" --format='%(contents)')"
if [ -z "${body//[[:space:]]/}" ]; then
	echo "❌ ${tag} 的发版说明是空的" >&2
	exit 1
fi
# 上一个版本：按版本号排序紧挨着它的那个 v* tag（不靠提交历史，浅克隆也找得到）
previous="$(git tag -l 'v*' --sort=-v:refname | awk -v t="$tag" 'found { print; exit } $0 == t { found = 1 }')"

printf '%s\n\n' "$body"

cat <<NOTES
## Install

\`\`\`bash
curl -fsSL https://raw.githubusercontent.com/${repo}/main/install.sh | sh
\`\`\`

The install script verifies the sha256 checksum and refuses to install on a mismatch.
If you'd rather not pipe curl into sh, downloading first and inspecting it before
running works just as well:

\`\`\`bash
curl -fsSLO https://raw.githubusercontent.com/${repo}/main/install.sh
less install.sh && sh install.sh
\`\`\`

With Go installed, \`go install github.com/brickkit/brickkit/cmd/brickkit@${tag}\` also works.

## Verify the signature (optional, requires cosign)

Signing is keyless — the signer's identity is the workflow that published this release:

\`\`\`bash
cosign verify-blob \\
  --bundle checksums.txt.sigstore.json \\
  --certificate-identity-regexp "https://github.com/${repo}/" \\
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \\
  checksums.txt
\`\`\`

## Platforms

All five artifacts were smoke-tested against the real binary before release
(version / init / add / up --dry-run). One boundary is worth calling out here so
it isn't mistaken for a bug:

- **Windows** has no install script — download \`brickkit_${version}_windows_amd64.zip\`
  manually. It was only smoke-tested for the commands that don't need Docker: GitHub's
  Windows runner can't run Linux containers, so the \`up\` path that actually starts
  containers or talks to a K8s cluster couldn't be verified there.
NOTES

if [ -n "$previous" ]; then
	printf '\n**Full changelog**: https://github.com/%s/compare/%s...%s\n' "$repo" "$previous" "$tag"
fi
