#!/bin/bash
# Diagnose whether this machine's Podman setup can run a BrickKit project.
# It does NOT change anything — it only reads state and, to get real answers,
# runs a few throwaway containers (and one throwaway network) and removes them.
#
# It checks, in order:
#   - a snap-redirected $XDG_DATA_HOME (rootless; breaks Podman after upgrades)
#   - podman.socket, which `podman compose` talks through: the user unit when
#     run as a normal user, the system unit when run as root
#   - containers on one network can reach each other. Rootful Podman on a host
#     that also runs Docker fails this silently: Docker sets the FORWARD policy
#     to DROP, and that drop is final for Podman's bridge too
#   - containers stop and remove cleanly. On Ubuntu, AppArmor's default policy
#     can block the `pasta` helper (rootless Podman's userspace network) from
#     receiving the SIGTERM that `podman rm` sends it during teardown
#
# Run it as the user that will run `brickkit up`: as root to check rootful
# Podman, as yourself to check rootless Podman. The full story:
#   docs/en/10-troubleshooting/01-up-down-issues.md
#
# Exit code: 0 = looks healthy, 1 = a known issue was reproduced,
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
# anything else (AppArmor included) gets a chance to matter. Rootful and
# rootless Podman each have their own socket: root's is the system unit,
# a normal user's is the user unit.
if [[ $EUID -eq 0 ]]; then
  socket_active() { systemctl is-active --quiet podman.socket 2>/dev/null; }
  socket_fix="systemctl enable --now podman.socket"
  echo "Running as root: checking rootful Podman."
else
  socket_active() { systemctl --user is-active --quiet podman.socket 2>/dev/null; }
  socket_fix="systemctl --user enable --now podman.socket"
  echo "Running as $(id -un): checking rootless Podman (run as root to check rootful Podman)."
fi
if socket_active; then
  pass "podman.socket is active (needed for 'podman compose')."
else
  warn "podman.socket is not active — 'podman compose' will fail to connect."
  warn "Fix: $socket_fix"
fi

status=0
NAME="brickkit-podman-check-$$"
# A fresh log file per run, removed on exit. A fixed name in /tmp breaks the
# documented order (first as yourself, then as root): the file left by the
# first run belongs to you, and with fs.protected_regular=2 (Ubuntu's default)
# not even root may write it — the redirect fails, `podman rm` never runs, and
# the old log is reported as this run's failure.
if ! LOG="$(mktemp "${TMPDIR:-/tmp}/brickkit-podman-check.XXXXXX")"; then
  fail "Could not create a temporary log file."
  exit 2
fi
cleanup() {
  podman rm -f "$NAME" "$NAME-server" >/dev/null 2>&1 || true
  podman network rm "$NAME" >/dev/null 2>&1 || true
  rm -f "$LOG"
}
trap cleanup EXIT

# --- Container-to-container traffic -------------------------------------------
# A deployment can come up all healthy while no call between containers gets
# through: health checks only look at the container itself. Rootful Podman
# routes bridge traffic through the host's FORWARD hook; Docker sets that
# hook's policy to DROP, and with nftables a drop on any chain of a hook is
# final — netavark accepting the traffic in its own table doesn't save it.
# Rootless Podman uses pasta and never touches the host's FORWARD hook.
echo
echo "Checking that two containers on one network can reach each other..."
if ! podman network create "$NAME" >/dev/null 2>&1 ||
   ! podman run -d --name "$NAME-server" --network "$NAME" alpine:latest nc -lk -p 8080 -e echo ok >/dev/null 2>&1; then
  fail "Could not even start a test container — investigate 'podman run' directly."
  exit 2
fi
reached=""
for _ in 1 2 3; do
  if [[ "$(podman run --rm --network "$NAME" alpine:latest nc -w 3 "$NAME-server" 8080 2>/dev/null)" == ok ]]; then
    reached=yes
    break
  fi
  sleep 1
done
podman rm -f "$NAME-server" >/dev/null 2>&1 || true
podman network rm "$NAME" >/dev/null 2>&1 || true
if [[ -n "$reached" ]]; then
  pass "Containers on one network reach each other."
else
  status=1
  fail "A container could not reach another container on the same network."
  if [[ $EUID -eq 0 ]] && iptables -S FORWARD 2>/dev/null | grep -q '^-P FORWARD DROP'; then
    if iptables -S DOCKER-USER >/dev/null 2>&1; then
      fail "This matches rootful Podman next to Docker: the FORWARD policy is DROP and Docker's DOCKER-USER chain exists."
      echo
      echo "Let Podman's bridges through in Docker's chain (persisting the rules is up to this host's setup):"
      echo "  iptables -I DOCKER-USER -i podman+ -j ACCEPT"
      echo "  iptables -I DOCKER-USER -o podman+ -j ACCEPT"
      echo "or don't run rootful Podman and Docker on the same machine. Rootless Podman isn't affected."
    else
      warn "The FORWARD policy is DROP, but there is no DOCKER-USER chain: some other firewall drops forwarded traffic."
      echo "Allow forwarding for Podman's bridges (podman0, podman1, ...) in that firewall."
    fi
  else
    warn "This doesn't match the known signature (rootful Podman with a FORWARD policy of DROP)."
  fi
  echo "Details: docs/en/10-troubleshooting/01-up-down-issues.md"
fi

# --- Container teardown --------------------------------------------------------
# Reading config files can't tell you whether the AppArmor denial actually
# fires — it depends on the loaded kernel policy, not just what's on disk.
# The only reliable check is to actually create and tear down a container.
echo
echo "Running a real create+remove cycle to check teardown reliability..."

if ! podman run -d --name "$NAME" --network bridge alpine:latest sleep 30 >/dev/null 2>&1; then
  fail "Could not even start a test container — investigate 'podman run' directly."
  exit 2
fi

if podman rm -f "$NAME" >"$LOG" 2>&1; then
  pass "Container removed cleanly. This machine does not show the AppArmor teardown issue."
  echo "You don't need scripts/podman/fix-apparmor.sh."
else
  status=1
  fail "Removing the container failed:"
  tail -5 "$LOG"
  echo
  if grep -qi "rootless netns" "$LOG"; then
    fail "This matches the known AppArmor/pasta signal issue."
    echo
    echo "Next step: read docs/en/10-troubleshooting/01-up-down-issues.md"
    echo "and consider running scripts/podman/fix-apparmor.sh (review it first — it's destructive)."
  else
    warn "This doesn't match the known signature (no 'rootless netns' in the error)."
    echo "This looks like a different problem — the fix script below is unlikely to help."
  fi
fi

echo
if [[ $status -eq 0 ]]; then
  echo "Nothing to do."
fi
exit $status
