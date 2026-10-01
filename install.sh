#!/bin/sh
# BrickKit CLI install script
#
#   curl -fsSL https://raw.githubusercontent.com/brickKit/brickKit/main/install.sh | sh
#
# Environment variables:
#   BRICKKIT_VERSION      which version to install, e.g. v0.1.0 (default: latest release)
#   BRICKKIT_INSTALL_DIR  where to install (default: /usr/local/bin, falling back to
#                          ~/.local/bin if that isn't writable)
#   BRICKKIT_BASE_URL     where to fetch artifacts from (default: GitHub Releases;
#                          override with a file:// URL for testing)
#   BRICKKIT_NO_COMPLETION  set to anything to skip installing shell completion
#
# macOS and Linux only. On Windows, download the zip by hand — all 23 hands-on
# guides are bash, the Docker workflow has never once been verified on
# Windows, and this script doesn't pretend to support it.
#
# POSIX sh is used here on purpose, not bash: on images like Alpine, /bin/sh
# is dash, and "installing the CLI" is exactly the kind of thing that has to
# work in the most bare-bones environment there is.
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
	command -v "$1" >/dev/null 2>&1 || die "Missing $1" "$2"
}

# ---- 1. Detect the platform ----
#
# Error out and list the four supported combinations rather than guess.
# Guessing wrong means installing a binary that can't run, and the error
# that follows will point at "exec format error" — by then nobody will
# suspect the install script's guess.
os="$(uname -s)"
arch="$(uname -m)"
case "$os" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*)
	die "Unsupported OS: ${os}" \
		"install.sh only supports linux and darwin (macOS)" \
		"On Windows, download windows_amd64.zip by hand from https://github.com/${REPO}/releases"
	;;
esac
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*)
	die "Unsupported architecture: ${arch}" \
		"Prebuilt artifacts only cover amd64 and arm64" \
		"For other architectures, build it yourself: go install github.com/brickkit/brickkit/cmd/brickkit@latest"
	;;
esac

need curl "install curl first, then try again"
need tar "install tar first, then try again"

# The sha256 tool has a different name on each platform: sha256sum on Linux,
# shasum -a 256 on macOS.
if command -v sha256sum >/dev/null 2>&1; then
	sha_cmd="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
	sha_cmd="shasum -a 256"
else
	die "Neither sha256sum nor shasum was found" \
		"The checksum is this script's only security guarantee — it refuses to install without one. This step can't be skipped."
fi

# ---- 2. Pin a version ----
version="${BRICKKIT_VERSION:-}"
if [ -z "$version" ]; then
	# Not using the GitHub API: it rate-limits unauthenticated requests to
	# 60/hour, which company networks sharing one outbound IP hit easily.
	# /releases/latest redirects (302) to /releases/tag/vX.Y.Z, and reading
	# the version out of that Location header costs no quota at all.
	version="$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
		"https://github.com/${REPO}/releases/latest" 2>/dev/null |
		sed -n 's#.*/tag/\(.*\)$#\1#p')"
	[ -n "$version" ] || die "Could not determine the latest version" \
		"Either the network is unreachable, or this repo has no releases yet" \
		"Pin a version and try again: BRICKKIT_VERSION=v0.1.0 sh install.sh"
fi
# The version in the artifact name has no leading "v"
bare_version="${version#v}"

archive="brickkit_${bare_version}_${os}_${arch}.tar.gz"

# ---- 3. Download ----
tmp="$(mktemp -d)"
# The trap guarantees cleanup even on an abnormal exit — especially on the
# checksum-failure path, which must never leave a half-installed package on
# disk that looks like "it downloaded fine, just finish installing it by hand".
trap 'rm -rf "$tmp"' EXIT INT TERM

info "▶ Downloading ${archive} (${version})"
curl -fsSL -o "${tmp}/${archive}" "${BASE_URL}/${version}/${archive}" ||
	die "Download failed: ${BASE_URL}/${version}/${archive}" \
		"Confirm this version exists: https://github.com/${REPO}/releases"
curl -fsSL -o "${tmp}/checksums.txt" "${BASE_URL}/${version}/checksums.txt" ||
	die "Failed to download checksums.txt" "Won't install without a checksum"

# ---- 4. Verify ----
#
# Only checks this one package's line, rather than running `sha256sum -c`
# over the whole checksums.txt — the latter would fail just because the
# other four packages aren't present locally, and that kind of failure is
# the sort people learn to ignore as noise.
info "▶ Verifying sha256"
expected="$(grep " ${archive}\$" "${tmp}/checksums.txt" | awk '{print $1}')"
[ -n "$expected" ] || die "checksums.txt has no line for ${archive}" \
	"The artifact and the checksum file don't match — that's not normal, don't install"
actual="$(cd "$tmp" && $sha_cmd "$archive" | awk '{print $1}')"
if [ "$expected" != "$actual" ]; then
	rm -f "${tmp}/${archive}"
	die "Checksum verification failed: ${archive}" \
		"Expected ${expected}" \
		"Got      ${actual}" \
		"The package may have been tampered with, or the download is incomplete. It has been deleted; nothing was installed."
fi

# ---- 5. Where to install ----
if [ -n "${BRICKKIT_INSTALL_DIR:-}" ]; then
	install_dir="$BRICKKIT_INSTALL_DIR"
	mkdir -p "$install_dir" 2>/dev/null ||
		die "Could not create ${install_dir}" "Pick a different BRICKKIT_INSTALL_DIR"
	[ -w "$install_dir" ] || die "${install_dir} is not writable" "Pick a different BRICKKIT_INSTALL_DIR"
elif [ -w /usr/local/bin ]; then
	install_dir=/usr/local/bin
else
	# Never sudo on the user's behalf. Elevating privileges is their own
	# decision to make, not something a curl | sh one-liner should do for them.
	install_dir="${HOME}/.local/bin"
	mkdir -p "$install_dir" ||
		die "Could not create ${install_dir}" "Point BRICKKIT_INSTALL_DIR at a directory you can write to"
fi

tar -xzf "${tmp}/${archive}" -C "$tmp" brickkit ||
	die "Failed to extract ${archive}"
# Install under a temp name in the same directory, then mv: overwriting a
# running binary in place fails with "text file busy", while mv is atomic.
mv "${tmp}/brickkit" "${install_dir}/.brickkit.new" ||
	die "Could not write to ${install_dir}"
chmod 0755 "${install_dir}/.brickkit.new"
mv "${install_dir}/.brickkit.new" "${install_dir}/brickkit"

# ---- 6. Report where it landed ----
info ""
info "✅ ${install_dir}/brickkit"
info "   $("${install_dir}/brickkit" version --log-level off 2>/dev/null | head -1)"

case ":${PATH}:" in
*":${install_dir}:"*) ;;
*)
	info ""
	info "⚠️  ${install_dir} is not on PATH — add this line (to ~/.bashrc or ~/.zshrc):"
	info ""
	info "    export PATH=\"${install_dir}:\$PATH\""
	;;
esac

# ---- 7. Shell completion ----
#
# Completion is the shell's own feature: it loads a function registered for
# `brickkit` from a directory it already searches. A program can't change a
# shell that is already running, so the most an installer can do is put the
# file where the shell looks — what Homebrew and apt packages do. Never edit
# rc files: they are the user's own. A completion that can't be written is
# reported and skipped; the CLI itself is already installed.
bk="${install_dir}/brickkit"

# put_completion <shell> <file> [note]: write one shell's completion file.
put_completion() {
	if mkdir -p "$(dirname "$2")" 2>/dev/null && "$bk" completion "$1" >"$2" 2>/dev/null; then
		info "   $(printf '%-5s' "$1") ${2}${3:-}"
		return 0
	fi
	rm -f "$2" 2>/dev/null
	info "   ⚠️  $1: could not write $2 — see: brickkit completion $1 --help"
	return 1
}

# zsh_fpath_hint: what ~/.zshrc needs so zsh finds ~/.zsh/completions — read, never edited.
# compinit reads $fpath once, when it runs, so the fpath line must come before the line that
# turns completion on. Oh My Zsh's `source $ZSH/oh-my-zsh.sh` runs compinit itself, and so does
# a hand-written compinit: then the one fpath line goes above that line, named by number.
zsh_fpath_hint() {
	rc="${ZDOTDIR:-$HOME}/.zshrc"
	if [ -f "$rc" ] && grep -v '^[[:space:]]*#' "$rc" | grep -q 'zsh/completions'; then
		info "           ~/.zshrc already adds ~/.zsh/completions to fpath — nothing to change"
		return
	fi
	at=""
	if [ -f "$rc" ]; then
		at="$(grep -nE 'oh-my-zsh\.sh|compinit' "$rc" | grep -vE '^[0-9]+:[[:space:]]*#' | head -1)"
	fi
	if [ -n "$at" ]; then
		n="${at%%:*}"
		line="$(printf '%s' "${at#*:}" | sed 's/^[[:space:]]*//')"
		info "   Add this one line to ~/.zshrc, above line ${n} (${line}) —"
		info "   that line turns completion on, so the folder has to be on fpath before it:"
		info "           fpath=(~/.zsh/completions \$fpath)"
	else
		info "   Add these two lines to the end of ~/.zshrc:"
		info "           fpath=(~/.zsh/completions \$fpath)"
		info "           autoload -Uz compinit && compinit"
	fi
}

# zsh_site_dir: the first writable site-functions directory on zsh's own
# fpath (Homebrew's and /usr/local's are there). Asking zsh beats guessing
# paths per distro, and a file there needs no ~/.zshrc change.
zsh_site_dir() {
	zsh -fc 'print -l $fpath' 2>/dev/null | while IFS= read -r d; do
		case "$d" in
		*/site-functions)
			if [ -d "$d" ] && [ -w "$d" ]; then
				printf '%s\n' "$d"
				return 0
			fi
			;;
		esac
	done
	return 0
}

info ""
if [ -n "${BRICKKIT_NO_COMPLETION:-}" ]; then
	info "Shell completion skipped (BRICKKIT_NO_COMPLETION is set)"
else
	info "Shell completion:"
	if command -v bash >/dev/null 2>&1; then
		put_completion bash "${XDG_DATA_HOME:-${HOME}/.local/share}/bash-completion/completions/brickkit" \
			" (loaded by the bash-completion package)" || true
	fi
	if command -v zsh >/dev/null 2>&1; then
		zdir="$(zsh_site_dir)"
		if [ -n "$zdir" ]; then
			put_completion zsh "${zdir}/_brickkit" || true
		elif put_completion zsh "${HOME}/.zsh/completions/_brickkit"; then
			zsh_fpath_hint
		fi
	fi
	if command -v fish >/dev/null 2>&1; then
		put_completion fish "${XDG_CONFIG_HOME:-${HOME}/.config}/fish/completions/brickkit.fish" || true
	fi
	if command -v pwsh >/dev/null 2>&1; then
		info "   PowerShell: add  brickkit completion powershell | Out-String | Invoke-Expression  to \$PROFILE"
	fi
	info "   Open a new terminal (or run: exec \$SHELL) for completion to take effect."
fi
