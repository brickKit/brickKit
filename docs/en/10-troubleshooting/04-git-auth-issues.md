# Git authentication problems

When a component comes from a Git repository, BrickKit calls the `git` on your machine, with the credentials `git` already
has — SSH keys, a credential helper, a token CI injects. The platform itself stores and manages no credentials. So problems
of this kind are almost always between `git` and the hosting platform, not in BrickKit. Background in
[Distributing through Git](../03-component-guide/10-git-distribution.md).

First tell which kind it is: look at git's own words on the **Git error** line of the error.

| git's words include | Kind | Section |
| --- | --- | --- |
| `Permission denied (publickey)` | The SSH key is wrong | [SSH authentication fails](#ssh-authentication-fails) |
| `Host key verification failed` | This host's fingerprint was never confirmed | [Connecting to an SSH host for the first time](#connecting-to-an-ssh-host-for-the-first-time) |
| `could not read Username … terminal prompts disabled` | HTTPS has no credentials | [HTTPS authentication fails](#https-authentication-fails) |
| `Could not resolve host`, `Connection refused`, `timed out` | Never connected at all | [Offline or unreachable](#offline-or-unreachable) |

Scripts can tell the two apart by the error code: never connecting is `NETWORK_UNREACHABLE`, worth retrying later;
connecting and being refused is `AUTH_FAILED`, which retrying as it is never fixes (see [Error codes](../06-architecture/09-error-codes.md)).

## SSH authentication fails

**Symptom**

```text
❌ Failed to fetch component demo/hello
   Install source: company-git (git)
   Repository: git@github.com:brickkit-nonexistent-org/demo-hello
   Git error: git@github.com: Permission denied (publickey).
      fatal: Could not read from remote repository.
      Please make sure you have the correct access rights
      and the repository exists.
   Component: demo/hello@1.0.0
   Suggestions:
   1. SSH: check that ~/.ssh/ holds the right key and that it is added to the repository host
   2. HTTPS: check that a git credential helper is configured (store, cache, osxkeychain, manager…)
   3. CI/CD: check that a token is injected (GIT_ASKPASS, .netrc or the CI system's credentials); a mistyped or missing repository fails the same way
```

**Cause**

The hosting platform doesn't accept your SSH key: the key isn't added to the platform, it isn't the key being used, or
you have no permission on this repository. When the repository address is misspelled or the repository doesn't exist,
many platforms give the same answer too, so as not to leak whether the repository exists. (With a key the platform does
accept, a repository that doesn't exist reads `ERROR: Repository not found.` instead.)

**Fix**

1. Try it without BrickKit: `git ls-remote git@github.com:<organisation>/<repository>`. If that fails too, the problem has
   nothing to do with BrickKit.
2. `ssh -T git@github.com` (or your hosting platform) shows whether the platform recognises your key.
3. Check the repository address: it's derived from the install source's `baseUrl` plus the component ID (`demo/hello` →
   `demo-hello`), and the "Repository" line in the error is the derived result.

## Connecting to an SSH host for the first time

**Symptom**

Run in a terminal, the command stops and asks you:

```text
The authenticity of host 'github.com (140.82.121.4)' can't be established.
ED25519 key fingerprint is: SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU
This key is not known by any other names.
Are you sure you want to continue connecting (yes/no/[fingerprint])?
```

Answer `no`, or run it where there's no terminal (CI, scripts), and you get:

```text
   Git error: Host key verification failed.
      fatal: Could not read from remote repository.
```

**Cause**

`~/.ssh/known_hosts` doesn't hold this host's fingerprint yet, and SSH wants you to confirm "it really is the one I'm
connecting to".

BrickKit sets `GIT_TERMINAL_PROMPT=0`, so that `git` fails at once when credentials are missing instead of hanging in CI,
waiting for input that never comes. But that variable only covers `git`'s own questions (the HTTPS user name and
password). **SSH's questions are read by `ssh` straight from the terminal** — confirming a host fingerprint, entering the
passphrase of a protected private key — and `GIT_TERMINAL_PROMPT` can't reach them. So with a terminal you're asked;
without one, `ssh` can't read an answer and fails.

**Fix**

- On your machine: check the fingerprint matches what the hosting platform publishes, then answer `yes`; you won't be asked
  again.
- In CI: write the host fingerprint into `known_hosts` beforehand. `ssh-keyscan github.com >> ~/.ssh/known_hosts` fetches
  the fingerprint but doesn't verify it — compare what it fetched with what the platform publishes once, then pin it in the
  CI configuration.
- A private key with a passphrase: on your machine, load it into `ssh-agent` beforehand (`ssh-add`); in CI, use a read-only
  deploy key without a passphrase, or switch to HTTPS with a token.

## HTTPS authentication fails

**Symptom**

```text
❌ Failed to fetch component demo/hello
   Install source: company-git (git)
   Repository: https://github.com/brickkit-nonexistent-org/demo-hello
   Git error: fatal: could not read Username for 'https://github.com': terminal prompts disabled
```

**Cause**

`git` needs a user name and password (or token), no credential helper can supply them, and it's forbidden to ask you in the
terminal (`GIT_TERMINAL_PROMPT=0`). When a repository doesn't exist, many platforms ask for a login first, so it's this same
line again.

**Fix**

- On your machine: configure a credential helper (`git config --global credential.helper store`, `cache`, `osxkeychain`,
  `manager`…), then log in once by hand with `git ls-remote <repository address>`, and the credentials are stored.
- Check the repository address: that it really exists, and that you have permission.

## Authentication in CI

CI has no terminal, and none of your personal credentials. BrickKit offers no CI-specific authentication flags — the ways
CI platforms inject credentials are mature already, and `git` understands them all:

| Way | How |
| --- | --- |
| HTTPS + token | `.netrc` (`machine git.example.com login <user> password <token>`), or `GIT_ASKPASS` pointing at a script that prints the token, or the CI platform's own credential injection |
| SSH deploy key | Put a read-only deploy key in the CI's secret store, write it into `~/.ssh/` at run time (mode 600); write the host fingerprint into `known_hosts` beforehand (see the section above) |
| Rewriting addresses | `git config --global url."https://<token>@git.example.com/".insteadOf "https://git.example.com/"`, with no change to the project's `baseUrl` |

Don't write a token into `brickkit.yaml`: it goes into Git.

## Offline or unreachable

**Symptom**

```text
❌ Failed to fetch component demo/bus
   Install source: company-git (git)
   Repository: https://git.example.com/components/demo-bus
   Git error: fatal: unable to access 'https://git.example.com/components/demo-bus/': Could not resolve host: git.example.com
   Component: demo/bus
   Suggestions:
   1. Can't reach the repository: check the network and the host name in the repository address; retry once the network is back
   2. Finding the latest version needs the network. Offline, name the version (demo/bus@<version>): versions already in the local cache need no network — 1.0.0
```

**Cause**

The remote was never reached: offline, a misspelled host name, a proxy or firewall in the way.

Equally offline, **writing a version or not gives different results**:

| Command | Offline |
| --- | --- |
| `brickkit add demo/bus@1.0.0` | When the local repository cache already has this version's tag, it's used directly, without the network |
| `brickkit add demo/bus` (no version) | Knowing which version is "the latest" means asking the remote — fails |

"The latest version" is only ever what the remote says: the highest version in the cache isn't necessarily the remote's
latest, and passing it off as "the latest" would make you believe you installed the newest one. So the CLI doesn't guess;
it lists the versions already in the cache, for you to pick one explicitly. `brickkit upgrade <component>` (no version)
works the same way.

**Fix**

Retry once online; or, as suggested, name a version already in the cache. The repository cache lives in a user-level
directory (`~/.cache/brickkit/repos/` on Linux), shared by every project on the machine — a version fetched online in one
project can be used offline in another.
