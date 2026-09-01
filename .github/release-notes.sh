#!/usr/bin/env bash
# 生成 release 页面的正文（《发布与分发》§5 的 publish 阶段用它）。
#
# 抽成单独脚本而不是塞进 workflow 的 run 里：里面有反引号和 heredoc，写在 YAML
# 的块标量里要同时躲开 YAML 缩进、shell 展开和 Markdown 转义三层坑，而且**没法
# 在本地看一眼渲染结果**。抽出来就能：bash .github/release-notes.sh v0.1.0 owner/repo
set -euo pipefail

tag="${1:?用法：release-notes.sh <tag> <owner/repo>}"
repo="${2:?用法：release-notes.sh <tag> <owner/repo>}"
version="${tag#v}"

cat <<NOTES
## 安装

\`\`\`bash
curl -fsSL https://raw.githubusercontent.com/${repo}/main/install.sh | sh
\`\`\`

装脚本会校验 sha256，对不上就拒绝安装。不想走管道的话，先下再看再跑也一样：

\`\`\`bash
curl -fsSLO https://raw.githubusercontent.com/${repo}/main/install.sh
less install.sh && sh install.sh
\`\`\`

有 Go 的话仍然可以 \`go install github.com/brickkit/brickkit/cmd/brickkit@${tag}\`。

## 验签（可选，需要 cosign）

签名是 keyless 的，签名者身份就是发布这一版的那条 workflow：

\`\`\`bash
cosign verify-blob \\
  --certificate checksums.txt.pem --signature checksums.txt.sig \\
  --certificate-identity-regexp "https://github.com/${repo}/" \\
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \\
  checksums.txt
\`\`\`

## 平台

五个产物在发布之前都跑过一遍真二进制（version / init / add / up --dry-run）。
一处边界写在这儿，免得被当成 bug：

- **Windows** 没有安装脚本，手动下 \`brickkit_${version}_windows_amd64.zip\`。
  它只验过不需要 Docker 的那部分命令——GitHub 的 windows runner 跑不了 Linux
  容器，\`up\` 真起容器与 K8s 那条线在那里没法验。

详见《发布与分发》§3.1 与 §10。
NOTES
