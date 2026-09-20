#!/bin/sh
# BrickKit CLI install script (see the "Release and Distribution" design doc, §6)
#
#   curl -fsSL https://raw.githubusercontent.com/brickKit/brickKit/main/install.sh | sh
#
# Environment variables:
#   BRICKKIT_VERSION      which version to install, e.g. v0.1.0 (default: latest release)
#   BRICKKIT_INSTALL_DIR  where to install (default: /usr/local/bin, falling back to
#                          ~/.local/bin if that isn't writable)
#   BRICKKIT_BASE_URL     where to fetch artifacts from (default: GitHub Releases;
#                          override with a file:// URL for testing)
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
