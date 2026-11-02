#!/bin/bash
# Reset Podman to a clean, working state on Ubuntu/Debian by fixing the
# AppArmor policy gap that blocks `podman rm`/`down` from tearing down its
# rootless network namespace. See the companion doc for the full story and
# a line-by-line explanation of every step below:
#   docs/en/10-troubleshooting/01-up-down-issues.md
#
# ============================================================================
# READ THIS BEFORE RUNNING:
#
#   * This DELETES all of your existing Podman containers, images and
#     volumes (rm -rf ~/.local/share/containers). It resets Podman, it does
#     not patch around your current data.
#   * It needs sudo, and it purges + reinstalls the `podman` package via apt.
#   * It edits two AppArmor files: it restores /etc/apparmor.d/podman when
#     missing, and adds one signal rule to /etc/apparmor.d/usr.bin.pasta.
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

sudo apt purge -y podman podman-docker
sudo apt autoremove -y

# Only Podman's own config — deliberately not /etc/cni or /etc/docker.
sudo rm -rf /etc/containers

echo "🚀 Step 2: Installing Podman..."
sudo apt update
sudo apt install -y podman

# --- Keep the podman AppArmor profile ---------------------------------------
# The apparmor package ships /etc/apparmor.d/podman: `flags=(unconfined)` with
# `userns`. It restricts nothing, but on systems that restrict unprivileged user
# namespaces (Ubuntu 23.10 and later) it is what lets rootless Podman create its
# user namespace when it is started outside another permissive profile — by the
# boot unit, over ssh, from a plain terminal. Without it those fail with
# "failed to reexec: Permission denied". An earlier version of this script
# deleted the profile; dpkg doesn't bring back a deleted conffile on its own,
# so restore it when it's missing (--force-confmiss restores missing conffiles,
# --force-confold keeps the ones you edited), then load it.
echo "🛡️ Step 3: Making sure the podman AppArmor profile is in place..."
if [[ ! -e /etc/apparmor.d/podman ]]; then
  sudo apt-get install -y --reinstall \
    -o Dpkg::Options::=--force-confmiss -o Dpkg::Options::=--force-confold apparmor
fi
if [[ -e /etc/apparmor.d/podman ]]; then
  sudo apparmor_parser -r /etc/apparmor.d/podman
else
  echo "ℹ️  This apparmor package ships no podman profile; nothing to restore."
fi

echo "📝 Step 4: Generating baseline config files..."
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

echo "🔌 Step 5: Enabling the Podman API socket..."
# `podman compose` shells out to the same docker-compose binary Docker uses,
# and that binary talks to Podman over a Docker-API-compatible socket. It
# isn't running by default — without it, `podman compose up`/`down` fail
# immediately with "failed to connect to the docker API ... no such file or
# directory", before anything AppArmor-related even comes into play.
systemctl --user enable --now podman.socket

echo "🛡️ Step 6: Letting pasta receive podman's SIGTERM..."

# When the last container on a bridge network goes, rootless Podman sends
# pasta (the helper carrying that network) SIGTERM. pasta's AppArmor profile,
# as the passt package ships it, doesn't allow receiving a signal from the
# `podman` profile, so the teardown fails with "rootless netns: kill network
# process: permission denied". One rule fixes exactly that:
#
#   signal (receive) set=(term) peer=podman,
#
# Verified on Ubuntu 26.04 (Podman 5.7.0, passt 0.0~git20260120): with the
# shipped profile the teardown fails and the kernel logs the denied signal;
# with this rule added it succeeds, and pasta stays enforced. The profile has
# no local include, so the rule goes into the packaged file — a later passt
# upgrade asks whether to keep it. Edit pasta's profile, not passt's: they are
# two binaries in the same package with near-identical names. An earlier
# version of this script put pasta into complain mode instead; that flag is
# removed here, so the profile is enforced again.
PASTA=/etc/apparmor.d/usr.bin.pasta
if [[ -f "$PASTA" ]]; then
  sudo sed -E -i '/^profile pasta /{s/, ?complain//;s/flags=\(complain, ?/flags=(/;s/ flags=\(complain\)//}' "$PASTA"
  grep -q 'signal (receive) set=(term) peer=podman' "$PASTA" \
    || sudo sed -i '/^profile pasta .*{$/a\  signal (receive) set=(term) peer=podman,' "$PASTA"
  sudo apparmor_parser -r "$PASTA"
else
  echo "ℹ️  No pasta AppArmor profile on this system; nothing to change."
fi

echo "✅ Done. Podman has been reset to a clean, working state."
echo "   Verify with: scripts/podman/check-environment.sh"
