#!/bin/bash
# Reset Podman to a clean, working state on Ubuntu/Debian by fixing the
# AppArmor policy gap that blocks `podman rm`/`down` from tearing down its
# rootless network namespace. See the companion doc for the full story and
# a line-by-line explanation of every step below:
#   docs/en/07-patterns/11-podman-environment-checklist.md
#
# ============================================================================
# READ THIS BEFORE RUNNING:
#
#   * This DELETES all of your existing Podman containers, images and
#     volumes (rm -rf ~/.local/share/containers). It resets Podman, it does
#     not patch around your current data.
#   * It needs sudo, and it purges + reinstalls the `podman` and
#     `apparmor-utils` packages via apt.
#   * Verified on Ubuntu/Debian (apt-based systems) only.
#   * It does NOT touch Docker, /etc/docker or /etc/cni — only Podman's own
#     footprint.
#
# Run scripts/podman/check-environment.sh first to confirm you actually have
# this problem before running this.
# ============================================================================

set -e

echo "🧹 Step 1: Cleaning up existing Podman and AppArmor traces..."

# -x matches the process name exactly, so this can't accidentally kill this
# script's own process (whose filename also contains "podman").
pkill -x -9 podman || true
pkill -x -9 pasta || true
pkill -x -9 slirp4netns || true

# This wipes all existing rootless Podman containers/images/volumes.
rm -rf ~/.local/share/containers ~/.config/containers ~/.cache/containers

sudo apt purge -y podman podman-docker apparmor-utils
sudo apt autoremove -y

# Only Podman's own config — deliberately not /etc/cni or /etc/docker.
sudo rm -rf /etc/containers

# --- The key step: correctly unload the stale AppArmor profile -------------
# `apparmor_parser -R <path>` has to READ the file to know which profile
# name to remove from the kernel. Deleting the file first and only then
# calling `-R` on it (a mistake that's easy to make) leaves the kernel's
# copy of the old profile loaded, even though the file on disk is gone.
# Writing a minimal, parseable stub first — then unloading it, then
# deleting it — avoids that trap.
echo 'profile podman /usr/bin/podman flags=(unconfined) {}' | sudo tee /etc/apparmor.d/podman > /dev/null
sudo apparmor_parser -R /etc/apparmor.d/podman 2>/dev/null || true
sudo rm -f /etc/apparmor.d/podman

echo "🚀 Step 2: Installing Podman and apparmor-utils..."
sudo apt update
sudo apt install -y podman apparmor-utils

echo "📝 Step 3: Generating baseline config files..."
sudo mkdir -p /etc/containers

# Without this, pulling an unqualified image reference (e.g. "alpine:latest",
# which is what BrickKit-generated compose files use) either fails outright
# or pops an interactive "which registry?" prompt — fatal for a
# non-interactive `brickkit up`.
echo 'unqualified-search-registries = ["docker.io", "quay.io"]' | sudo tee /etc/containers/registries.conf > /dev/null

# Without a signature-verification policy, Podman refuses to pull anything
# at all. This disables signature verification, matching Docker's own
# default (Docker Content Trust is off unless you turn it on) — not a step
# down in security, just parity with what Docker already does by default.
echo '{"default": [{"type": "insecureAcceptAnything"}]}' | sudo tee /etc/containers/policy.json > /dev/null

echo "🔌 Step 4: Enabling the Podman API socket..."
# `podman compose` shells out to the same docker-compose binary Docker uses,
# and that binary talks to Podman over a Docker-API-compatible socket. It
# isn't running by default — without it, `podman compose up`/`down` fail
# immediately with "failed to connect to the docker API ... no such file or
# directory", before anything AppArmor-related even comes into play.
systemctl --user enable --now podman.socket

echo "🛡️ Step 5: Fixing AppArmor's signal block on pasta (complain mode)..."

# aa-complain is the standard Ubuntu/Debian tool for switching one profile
# into complain mode (log violations, don't enforce them) — safer and less
# error-prone than hand-editing the profile file (it's easy to edit the
# wrong one: `passt` and `pasta` are two distinct binaries in the same
# package with near-identical names and separate profiles).
#
# Trade-off worth knowing: this removes AppArmor's enforcement for the
# `pasta` process specifically — nothing else on the system is affected,
# but `pasta` itself is no longer confined, only monitored.
sudo aa-complain /usr/bin/pasta 2>/dev/null \
  || sudo aa-complain pasta 2>/dev/null \
  || echo "ℹ️  pasta's profile isn't loaded yet — run this step again after your first 'podman run'."

echo "✅ Done. Podman has been reset to a clean, working state."
echo "   Verify with: scripts/podman/check-environment.sh"
