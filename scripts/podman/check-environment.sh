#!/bin/bash
# Diagnose whether this machine's rootless Podman setup can cleanly stop and
# remove containers. It does NOT change anything — it only reads state and,
# to get a real answer, runs one throwaway container and removes it again.
#
# Background: on Ubuntu, AppArmor's default policy can block the `pasta`
# helper process (rootless Podman's userspace network translator) from
# receiving the SIGTERM that `podman rm` sends it during teardown. `up` and
# normal traffic work fine; only stopping/removing fails. See the companion
# doc for the full story:
#   docs/en/07-patterns/11-podman-environment-checklist.md
#
# Exit code: 0 = looks healthy, 1 = the known issue was reproduced,
# 2 = could not run the check at all (podman missing, etc).

set -u

pass() { printf '✅ %s\n' "$1"; }
warn() { printf '⚠️  %s\n' "$1"; }
fail() { printf '❌ %s\n' "$1"; }

echo "== Podman environment check =="
echo

if ! command -v podman >/dev/null 2>&1; then
  fail "podman is not installed — nothing to check."
  exit 2
fi
pass "podman found: $(podman --version 2>&1)"

# --- Snap-sandboxed terminal check -----------------------------------------
# If this shell was launched from a snap-packaged app (e.g. VS Code installed
# via snap), $XDG_DATA_HOME may be redirected to a per-revision path like
# ~/snap/<app>/<revision>/.local/share. Podman records its rootless storage
# path and refuses to start once that revision number changes underneath it.
# This is unrelated to the AppArmor issue below, but produces a confusingly
# different error ("database configuration mismatch"), so it's worth ruling
# out first.
if [[ "${XDG_DATA_HOME:-}" == *"/snap/"* ]]; then
  warn "\$XDG_DATA_HOME points inside a snap sandbox path: $XDG_DATA_HOME"
  warn "This can make Podman refuse to start with a 'database configuration mismatch' error."
  warn "Fix: export XDG_DATA_HOME=\"\$HOME/.local/share\" and XDG_CONFIG_HOME=\"\$HOME/.config\" in your shell's rc file."
else
  pass "\$XDG_DATA_HOME does not look snap-redirected."
fi

if ! podman info >/dev/null 2>&1; then
  fail "'podman info' itself fails — fix that before continuing:"
  podman info 2>&1 | tail -5
  exit 2
fi
pass "'podman info' runs cleanly."

# --- podman.socket check -----------------------------------------------
# `podman compose` shells out to the same docker-compose binary Docker
# uses, which talks to Podman over a Docker-API-compatible socket. It's
# not on by default — without it, `podman compose up`/`down` fail before
# anything else (AppArmor included) gets a chance to matter.
if systemctl --user is-active --quiet podman.socket 2>/dev/null; then
  pass "podman.socket is active (needed for 'podman compose')."
else
  warn "podman.socket is not active — 'podman compose' will fail to connect."
  warn "Fix: systemctl --user enable --now podman.socket"
fi

# --- The actual reproduction test -------------------------------------------
# Reading config files can't tell you whether the AppArmor denial actually
# fires — it depends on the loaded kernel policy, not just what's on disk.
# The only reliable check is to actually create and tear down a container.
echo
echo "Running a real create+remove cycle to check teardown reliability..."
NAME="brickkit-podman-check-$$"

cleanup() { podman rm -f "$NAME" >/dev/null 2>&1 || true; }
trap cleanup EXIT

if ! podman run -d --name "$NAME" --network bridge alpine:latest sleep 30 >/dev/null 2>&1; then
  fail "Could not even start a test container — investigate 'podman run' directly."
  exit 2
fi

if podman rm -f "$NAME" >/tmp/brickkit-podman-check.log 2>&1; then
  pass "Container removed cleanly. This machine does not show the known issue."
  echo
  echo "Nothing to do — you don't need scripts/podman/fix-apparmor.sh."
  exit 0
else
  fail "Removing the container failed:"
  tail -5 /tmp/brickkit-podman-check.log
  echo
  if grep -qi "rootless netns" /tmp/brickkit-podman-check.log; then
    fail "This matches the known AppArmor/pasta signal issue."
    echo
    echo "Next step: read docs/en/07-patterns/11-podman-environment-checklist.md"
    echo "and consider running scripts/podman/fix-apparmor.sh (review it first — it's destructive)."
  else
    warn "This doesn't match the known signature (no 'rootless netns' in the error)."
    echo "This looks like a different problem — the fix script below is unlikely to help."
  fi
  exit 1
fi
