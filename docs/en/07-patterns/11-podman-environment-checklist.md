# Podman on Linux: an environment checklist

> **This is not about BrickKit choosing an engine.** As of today, `internal/engine` only
> implements Docker, and `deploy.target` only accepts `docker` and `k8s` — there is no way to
> tell BrickKit to run compose through Podman instead. This page is a prerequisite checklist for
> anyone who wants to run rootless Podman itself on Ubuntu/Debian (independent of BrickKit, and
> useful groundwork if Podman support is ever added) — not a feature this CLI has today.

## The problem

Rootless Podman (no root privileges) relies on a userspace helper process called `pasta` to give
containers network access. Stopping or removing a container requires Podman to send `pasta` a
`SIGTERM` so it can cleanly tear down that deployment's bridge, IP allocation, and nftables rules.

On Ubuntu (confirmed at least through 26.04), AppArmor's default policy blocks `pasta` from
receiving that signal:

```
$ podman rm -f some-container
Error: removing container ...: 1 error occurred:
	* rootless netns: kill network process: permission denied
```

**This reproduces every time on an affected machine — it isn't occasional.** `up`, `status`, and
ordinary traffic all work fine; only stop/remove fails. That's a worse failure mode than it
sounds: the container itself is usually gone (Podman falls back to `SIGKILL`), but the network
resources may not be, and the command's own exit code says it failed — easy to mistake for "the
deployment didn't come down at all."

This is a distro packaging gap, not a Podman bug and not anything specific to one machine — Podman
upstream has the identical report
([containers/podman#27372](https://github.com/containers/podman/issues/27372)), and the
maintainers' own conclusion was "this does not seem like an upstream problem but rather the distro
level security policies."

## Check whether you're affected

```bash
scripts/podman/check-environment.sh
```

This is read-only except for one thing: to get a real answer, it creates one throwaway container
and removes it again (self-cleaning). Reading config files alone can't tell you whether the
AppArmor denial actually fires — that depends on the currently loaded kernel policy, not just
what's on disk.

## The fix

```bash
scripts/podman/fix-apparmor.sh
```

**Read the script before running it — it is destructive.** It resets Podman to a clean state:
it deletes your existing Podman containers/images/volumes, purges and reinstalls the `podman` and
`apparmor-utils` packages, and regenerates baseline config. Verified on Ubuntu/Debian (apt-based
systems) only. It does not touch Docker or `/etc/cni`.

There are two independent techniques inside it, aimed at the same problem from two different
angles. The script applies both, so treat what follows as "this combination is what we verified
works," not a claim that either one alone is sufficient — we never isolated them.

**1. Removing the stale `podman` profile — Debian's own diagnosis.** Debian tracked this exact
signature ([Debian #1100135](https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=1100135)) to a
subtler cause: Ubuntu/Debian ship a placeholder AppArmor profile for the `podman` binary itself
(`/etc/apparmor.d/podman`, `flags=(unconfined)` — it restricts nothing, it exists only to give the
process a name instead of showing up as "unconfined"). But that name is exactly what triggers
AppArmor's cross-profile signal mediation in the first place: `pasta`'s profile never got a rule
saying "allow a signal from something named podman," so once `podman` carries that label, the
signal is denied. Debian's own fix was to delete that placeholder profile outright — nothing of
value is lost, since it never restricted anything to begin with. The script does the same thing,
but with one detail that matters: `apparmor_parser -R <path>` has to **read** the file to know
which profile to unload from the kernel. Deleting the file first and only then calling `-R` on it
leaves the kernel's copy of the old profile loaded even though the file is gone — an easy mistake
to make, and one that makes this fix silently not take effect. The script avoids it by writing a
fresh, minimal, parseable copy of the profile first, unloading *that*, and only then deleting the
file.

**2. Switching `pasta` itself to complain mode.** This is the one-line fix, once everything else is
reset:

```bash
sudo aa-complain /usr/bin/pasta
```

`aa-complain` is the standard Ubuntu/Debian tool for switching one AppArmor profile into
**complain mode** — log violations instead of blocking them — without hand-editing profile files
(easy to get wrong: `passt` and `pasta` are two distinct binaries shipped in the same package,
with separate, near-identically-named profile files).

**The trade-off, stated plainly:** complain mode means AppArmor no longer enforces anything
against the `pasta` process specifically — it only logs what `pasta` does. Nothing else on the
system is affected, but this one process is no longer confined, only monitored. That's the
trade-off between "precisely diagnose and allow only the one missing signal permission" and
"use the standard tool and accept that this one process loses enforcement."

The script also fixes two things that are unrelated to the signal problem above, but just as
fatal for a non-interactive tool like `brickkit up`:

- **`registries.conf`**: without an `unqualified-search-registries` entry, pulling an image
  reference with no registry prefix (`alpine:latest` — what BrickKit-generated compose files use)
  either fails outright or pops an interactive "which registry?" prompt, which hangs a
  non-interactive `brickkit up` indefinitely.
- **`policy.json`**: without a signature-verification policy, Podman refuses to pull anything at
  all. The script sets `insecureAcceptAnything`, which disables signature verification — this
  brings Podman's default in line with Docker's own default (Docker Content Trust is off unless
  you turn it on), not a step down in security.

The script also enables `podman.socket`. This one is unrelated to AppArmor entirely, and blocks
things even earlier: `podman compose` shells out to the same `docker-compose` binary Docker
itself uses, and that binary talks to Podman over a Docker-API-compatible socket. It isn't running
by default — without it, `podman compose up`/`down` fail immediately with "failed to connect to
the docker API … no such file or directory," before the AppArmor issue above even gets a chance to
matter:

```bash
systemctl --user enable --now podman.socket
```

## Known gap this doesn't cover

If you're testing from a terminal launched by a snap-packaged app (VS Code installed via snap is
the common case), you may hit an unrelated error first:

```
Error: database static dir "…/snap/code/254/…/containers/storage/libpod" does not match
our static dir "…/snap/code/264/…/containers/storage/libpod": database configuration mismatch
```

Snap redirects `$XDG_DATA_HOME` to a per-revision path (`~/snap/<app>/<revision>/…`); Podman
records its storage path and refuses to start once that revision number moves. `check-environment.sh`
flags this for you. The fix is a shell-profile change, not something the fix script touches:

```bash
export XDG_DATA_HOME="$HOME/.local/share"
export XDG_CONFIG_HOME="$HOME/.config"
```
