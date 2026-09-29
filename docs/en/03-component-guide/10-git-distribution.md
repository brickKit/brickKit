# Distributing through Git

## The idea

No dedicated server is needed: **components live in Git repositories, and a version is a Git tag.** The GitLab, Gitea or
GitHub organisation your company already has is a component source. Releasing is pushing a tag (`brickkit release`);
installing is fetching files by tag.

## How component addresses are derived

Users declare a Git install source in `brickkit.yaml`:

```yaml
sources:
  - name: company-git
    type: git
    baseUrl: https://git.example.com/components/
```

From then on each component's repository address is derived by rule, without configuring them one by one:

| Component | Repository |
| --- | --- |
| `demo/quote` | `https://git.example.com/components/demo-quote` |
| `erp/backend` | `https://git.example.com/components/erp-backend` |

A version maps to a tag: version `0.1.0` is tag `0.1.0` (no `v`). "The latest version" is the highest tag in exact-version
form; tags like `v1.0.0` or `latest` don't count as versions.

**When it can't be derived, name it.** For a repository in another organisation, a name that doesn't follow the rule, or a
component in a monorepo subdirectory, write `source` on that component's entry:

```yaml
components:
  - id: erp/backend
    version: 2.1.0
    source:
      type: git
      repo: https://git.example.com/platform/erp.git
      path: services/backend      # the component's subdirectory in the repository; the tag is then erp-backend/2.1.0
```

`source.type: local` points at a component directory on this machine instead (`path` relative to the project root); see
[Developing inside a component](05-local-dev-fractal.md).

## The cache: each repository cloned once

The CLI keeps a bare repository (Git data only, no working tree) per component repository in a **user-level** cache
directory, organised by the full repository address:

```text
~/.cache/brickkit/repos/
└── git.example.com/
    └── components/
        ├── demo-bus.git
        ├── demo-caller.git
        ├── demo-hello.git
        └── demo-quote.git
```

(On macOS it's `~/Library/Caches/brickkit/repos/`.) It's user-level rather than under each project's `.brickkit/` because
the same component repository is shared by many projects, and by the component's own workbench — kept per project, the
same repository would be cloned N times.

To get a component version's `component.yaml`, the CLI tries in order:

| Order | From | Network? |
| --- | --- | --- |
| 1 | The project's `.brickkit/manifests/` cache | No |
| 2 | This tag in the cached bare repository (`git show`) | No |
| 3 | `git fetch --tags`: the repository is there but lacks this tag | Incremental |
| 4 | `git clone --bare`: the repository was never cloned | A full clone |

So the first install needs the network, and after that the same version can be used offline at any time; a new version
only fetches the increment. A clone lands in a temporary directory first and is renamed when complete, so an interrupted
clone never leaves half a repository behind.

## Private repositories and authentication

**The CLI doesn't handle authentication.** It calls your system's `git`, using the credentials your `git` already has:

| Situation | What to set up |
| --- | --- |
| SSH addresses (`git@git.example.com:components/…`) | The matching key in `~/.ssh/`, added to the hosting platform |
| HTTPS addresses | A git credential helper (store, cache, osxkeychain, manager…) |
| CI | A token injected into the environment (`GIT_ASKPASS`, `.netrc`, or the CI platform's own credentials) |

Without credentials, the CLI lets `git` fail rather than stop and wait for a password — in CI that would hang the pipeline.
On failure, git's own words are passed on to you:

```text
❌ Failed to fetch component example/member
   Install source: company-git (git)
   Repository: git@github.com:brickkit-nonexistent-org/example-member
   Git error: ERROR: Repository not found.
      fatal: Could not read from remote repository.
      Please make sure you have the correct access rights
      and the repository exists.
   Component: example/member@0.1.0
   Suggestions:
   1. SSH: check that ~/.ssh/ holds the right key and that it is added to the repository host
   2. HTTPS: check that a git credential helper is configured (store, cache, osxkeychain, manager…)
   3. CI/CD: check that a token is injected (GIT_ASKPASS, .netrc or the CI system's credentials); a mistyped or missing repository fails the same way
```

When git never reached the remote at all (offline, an unresolvable host name), the suggestion is about the network
instead. How to tell the cases apart, and what to do in each: [Git authentication problems](../10-troubleshooting/04-git-auth-issues.md).

The platform stays out of credentials because git already solves this well: SSH, the many credential helpers, the ways CI
platforms inject tokens — each has a mature answer. A separate mechanism in the CLI would be one more thing to maintain,
and would never keep up with every hosting platform.

## How components are discovered

The platform has no registry and offers no search: where components are and which exist is a matter of convention and
documentation — for instance, keeping every component in the same Git organisation and listing them on the
organisation's page or an internal wiki, with each component's `BRICKKIT.md` saying what it is. When you need a searchable
catalog, a component market (`sources[].type: market`) is the other route.
