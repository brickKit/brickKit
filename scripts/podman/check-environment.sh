#!/bin/bash
# Diagnose whether this machine's Podman setup can run a BrickKit project.
# It does NOT change anything — it only reads state and, to get real answers,
# runs a few throwaway containers (and one throwaway network) and removes them.
#
# It checks, in order:
#   - a snap-redirected $XDG_DATA_HOME (rootless; breaks Podman after upgrades)
#   - rootless Podman can create its user namespace where the boot unit runs it
#     (AppArmor on Ubuntu 23.10+ needs the podman profile the apparmor package ships)
#   - podman.socket, which `podman compose` talks through: the user unit when
#     run as a normal user, the system unit when run as root
#   - containers on one network can reach each other. Rootful Podman on a host
#     that also runs Docker fails this silently: Docker sets the FORWARD policy
#     to DROP, and that drop is final for Podman's bridge too
#   - containers stop and remove cleanly. On Ubuntu, AppArmor's default policy
#     can block the `pasta` helper (rootless Podman's userspace network) from
#     receiving the SIGTERM that `podman rm` sends it during teardown
#   - (warnings) what a deployment needs to come back after a reboot: linger,
#     podman-restart.service enabled and waiting for the network, storage mounts
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

status=0

# --- User namespaces outside this terminal ------------------------------------
# Ubuntu 23.10 and later restrict unprivileged user namespaces through AppArmor
# (kernel.apparmor_restrict_unprivileged_userns = 1). Rootless Podman creates one
# and then re-executes itself; that is allowed only under an AppArmor profile
# with `userns`, which the apparmor package ships for podman as
# /etc/apparmor.d/podman. Without it, podman started by the boot unit, over ssh
# or from a plain terminal fails with "failed to reexec: Permission denied" —
# while a terminal inside a snap (VS Code) may still work, since the snap's own
# profile allows user namespaces. So the check runs podman in the user's
# systemd, where the boot unit runs it. It reads the AppArmor label too: with a
# container already running podman joins the existing namespace and succeeds
# even without the profile, but the label still shows whether a profile applies.
if [[ $EUID -ne 0 && "$(cat /proc/sys/kernel/apparmor_restrict_unprivileged_userns 2>/dev/null)" == 1 ]]; then
  echo
  echo "AppArmor restricts unprivileged user namespaces here: checking podman as the user's systemd starts it..."
  if ! command -v systemd-run >/dev/null 2>&1 || ! systemctl --user show-environment >/dev/null 2>&1; then
    warn "No systemd user session to check with (run this as the user, in a login session)."
  elif ! label="$(systemd-run --user --wait --pipe --quiet podman unshare cat /proc/self/attr/current 2>&1)"; then
    status=1
    fail "podman can't create its user namespace outside this terminal: ${label##*$'\n'}"
  elif [[ "$label" == unconfined || "$label" == unprivileged_userns* ]]; then
    status=1
    fail "podman runs without an AppArmor profile that allows user namespaces (label: $label)."
  else
    pass "podman runs under an AppArmor profile that allows user namespaces ($label)."
  fi
  if [[ $status -ne 0 ]]; then
    fail "At boot, over ssh and from a plain terminal, podman will fail with 'failed to reexec: Permission denied'."
    echo "Restore the profile the apparmor package ships for podman, then load it:"
    echo "  sudo apt-get install --reinstall -o Dpkg::Options::=--force-confmiss -o Dpkg::Options::=--force-confold apparmor"
    echo "  sudo apparmor_parser -r /etc/apparmor.d/podman"
    echo "Details: docs/en/10-troubleshooting/01-up-down-issues.md"
  fi
fi

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
# Rootless Podman tears its shared network down — sending pasta SIGTERM — only
# when the last container on a bridge network leaves (its ref-count drops to
# 0). While the count is above 0, removing one container doesn't get there and
# would pass for the wrong reason; a count left over from an earlier failed
# teardown, with no container running, does the same. Run as the user's
# systemd runs podman (the boot unit, ssh), not as this terminal: a snap's
# AppArmor profile would change what is being checked.
echo
echo "Running a real create+remove cycle to check teardown reliability..."

in_user_systemd() {
  if [[ $EUID -ne 0 ]] && command -v systemd-run >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1; then
    systemd-run --user --wait --pipe --quiet "$@"
  else
    "$@"
  fi
}

refs=0
if [[ $EUID -ne 0 ]]; then
  refs="$(cat "/run/user/$(id -u)/containers/networks/rootless-netns/ref-count" 2>/dev/null || echo 0)"
fi
if [[ "$refs" =~ ^[0-9]+$ ]] && (( refs > 0 )); then
  running="$(podman ps -q | wc -l)"
  warn "Rootless Podman's shared network is still held (ref-count $refs, $running running containers):"
  warn "removing a test container wouldn't reach the network teardown, so it can't be checked now."
  if (( running == 0 )); then
    echo "  The count is left over from an earlier teardown that failed. Reset it (nothing is running), or reboot:"
    echo "    printf 0 > /run/user/$(id -u)/containers/networks/rootless-netns/ref-count"
  else
    echo "  Rerun with no containers running."
  fi
else
  # Create and remove in one transient unit. `podman run -d` returns at once, but the processes it starts
  # (pasta for a new shared network, conmon) stay in that unit, and systemd-run --wait then waits for them
  # until the unit's stop timeout (90 s) and reports a timeout. In one unit, a clean teardown ends them all.
  in_user_systemd sh -c 'podman run -d --name "$1" --network bridge alpine:latest sleep 30 >/dev/null 2>&1 || exit 3
    podman rm -f "$1"' sh "$NAME" >"$LOG" 2>&1
  teardown=$?
  if (( teardown == 3 )); then
    fail "Could not even start a test container — investigate 'podman run' directly."
    exit 2
  elif (( teardown == 0 )); then
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
      echo "Let pasta receive podman's SIGTERM (one rule in pasta's profile), then load it:"
      echo "  sudo sed -i '/^profile pasta .*{\$/a\\  signal (receive) set=(term) peer=podman,' /etc/apparmor.d/usr.bin.pasta"
      echo "  sudo apparmor_parser -r /etc/apparmor.d/usr.bin.pasta"
      echo "Details: docs/en/10-troubleshooting/01-up-down-issues.md (scripts/podman/fix-apparmor.sh does it as part of a full reset)."
    else
      warn "This doesn't match the known signature (no 'rootless netns' in the error)."
      echo "This looks like a different problem — the fix script below is unlikely to help."
    fi
  fi
fi

# --- Coming back after a reboot ----------------------------------------------
# Only a deployment that must keep running needs this, so these are warnings.
# The engine starts the containers at boot, and only when set up to: for
# rootless Podman the user's services must start without a login (linger), the
# boot unit podman-restart.service must be enabled and must wait for the
# network (in the user's systemd, network-online.target means nothing; Podman
# ships podman-user-wait-network-online.service for this, but the boot unit
# doesn't use it — a pasta started before the network has an address never
# reaches the host), and Podman's storage must be mounted before the user's
# systemd starts when it lives on a disk mounted with nofail.
echo
echo "Checking what a deployment needs to come back after a reboot (warnings only)..."
if [[ $EUID -eq 0 ]]; then
  if systemctl is-enabled --quiet podman-restart.service 2>/dev/null; then
    pass "podman-restart.service is enabled."
  else
    warn "podman-restart.service is not enabled: nothing starts the containers at boot. Fix: systemctl enable podman-restart.service"
  fi
else
  user="$(id -un)"
  if [[ "$(loginctl show-user "$user" -p Linger --value 2>/dev/null)" == yes ]]; then
    pass "Linger is on for $user."
  else
    warn "Linger is off for $user: the user's services start only when someone logs in. Fix: sudo loginctl enable-linger $user"
  fi
  if systemctl --user is-enabled --quiet podman-restart.service 2>/dev/null; then
    pass "podman-restart.service (user unit) is enabled."
  else
    warn "podman-restart.service (user unit) is not enabled: nothing starts the containers at boot. Fix: systemctl --user enable podman-restart.service"
  fi
  if systemctl --user show podman-restart.service -p Wants --value 2>/dev/null | grep -qw podman-user-wait-network-online.service; then
    pass "podman-restart.service waits for the network."
  else
    warn "podman-restart.service doesn't wait for the network: if the network comes up late, the containers start without a route to the host."
    echo "  Fix: mkdir -p ~/.config/systemd/user/podman-restart.service.d"
    echo "       printf '[Unit]\\nWants=podman-user-wait-network-online.service\\nAfter=podman-user-wait-network-online.service\\n' \\"
    echo "         > ~/.config/systemd/user/podman-restart.service.d/wait-network.conf"
    echo "       systemctl --user daemon-reload"
  fi
  graphroot="$(podman info --format '{{.Store.GraphRoot}}' 2>/dev/null)"
  mount="$(findmnt -n -o TARGET -T "$graphroot" 2>/dev/null)"
  if [[ -n "$mount" && "$mount" != / ]] && findmnt --fstab -n -o OPTIONS "$mount" 2>/dev/null | grep -qw nofail; then
    required="$(systemctl show "user@$(id -u).service" -p RequiresMountsFor --value 2>/dev/null)"
    if [[ " $required " == *" $mount "* ]]; then
      pass "Podman's storage ($mount, nofail) is mounted before the user's systemd starts."
    else
      warn "Podman's storage is on $mount, mounted with nofail: nothing makes it mount before the user's systemd starts."
      echo "  Fix: sudo systemctl edit user@$(id -u).service, and add: [Unit] RequiresMountsFor=$mount"
    fi
  fi
fi

echo
if [[ $status -eq 0 ]]; then
  echo "Nothing to do."
fi
exit $status
