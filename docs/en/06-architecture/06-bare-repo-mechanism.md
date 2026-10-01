# The Git repository cache

## Why not `git clone` every time

On a Git install source, one component is one repository and one version is one tag. The most direct way for the CLI to
read a version's `component.yaml` and contract files would be to clone the repository every time — twenty components in a
project would be twenty full clones, on every `up`, in every project, in every component's own workbench.

BrickKit instead clones each component repository **once** on this machine, keeping it as a **bare repository** (Git's
data only, with no checked-out working tree), and afterwards only runs incremental `git fetch`es; to read a version's
files, it takes them straight from that tag in the bare repository (`git show <tag>:component.yaml`), with no checkout.

## Where it lives

In a user-level cache directory, organised by the full repository address:

```text
~/.cache/brickkit/repos/
└── git.example.com/
    └── components/
        ├── demo-hello.git
        └── demo-quote.git
```

(On macOS it's `~/Library/Caches/brickkit/repos/`.) It isn't in the project's `.brickkit/` because the same component
repository is shared by many projects, and by the component's own workbench — kept per project, the same repository would
be cloned N times. Named by the full address, same-named repositories in different organisations
(`github.com/a/erp-api` and `github.com/b/erp-api`) never collide.

## The four-level read order

To get a component version's `component.yaml`, the CLI tries these in order, stopping at the first that has it:

| Level | From | Network |
| --- | --- | --- |
| 1 | The project's permanent cache `.brickkit/manifests/<component>/<version>/` | Not needed |
| 2 | This tag in the cached bare repository (`git show`) | Not needed |
| 3 | `git fetch --tags`: the repository is there, but not this tag yet | Incremental |
| 4 | `git clone --bare`: this repository was never cloned | A full clone |

Components from a local install source don't take this path: a local source's `component.yaml` is read from the directory
afresh every time and never cached — change it, and the next command follows the new one.

## The whole flow of fetching a file

1. Check the project's Manifest cache. A hit ends it.
2. Work out the repository address: the install source's `baseUrl` plus `<scope>-<name>`, or `source.repo` when the
   component's entry has one. When the component sits in a repository subdirectory, the tag carries a namespace:
   `<scope>-<name>/<version>`.
3. Make sure the bare repository is in the cache, cloning it when it isn't. A clone lands in a temporary place under the
   cache directory first and is renamed when complete — failing or being interrupted halfway never leaves half a repository
   behind.
4. When the repository has the tag, read the files from the tag directly.
5. When it doesn't, `git fetch --tags` once for the increment (a repository is fetched at most once per run), then read.
   When a tag was force-moved, the remote wins; branches deleted on the remote are deleted in the cache too.
6. Write the `component.yaml` read into the project's permanent cache, together with every `BRICKKIT*.md` in the
   component's directory at that tag — `BRICKKIT.md` and each translation (`BRICKKIT.zh.md` …), when it carries them.

"The latest version" (`add` without a version, `upgrade`) needs the list of tags, and that step always fetches first, so
what it sees is the remote's tags as they are now.

## Offline

Once a version has been fetched, it's in the project's permanent cache, and `up`, `graph` and `deps` work without the
network from then on. When another project wants the same version, the bare repository is already in the user cache and
level 2 finds it, again without the network. Only a version never seen, or a repository never cloned, needs the network.

## Git submodules are never fetched

A component repository may register git submodules. BrickKit fetches none of them, on every path:

| Path | What you get |
| --- | --- |
| The bare-repository cache | Tags and their commits; a submodule is only a pointer in its tree |
| Reading `component.yaml`, `BRICKKIT*.md`, artifacts | Files read straight from the tag — never inside a submodule |
| `build` from a tag | An export of the tag: submodule directories are empty, and `build` warns about them |
| `add --repo` | A clone without `--recurse-submodules`; it names the submodules it left empty |

The reasons: a submodule points at another repository, with its own address and its own credentials; fetching it would
make installing one component quietly reach into repositories nobody declared. The contract of a component is its
`component.yaml`, its artifacts and its image — none of which should need a submodule. When the build does need one, the
component should publish an image; in a cloned repository you can always run `git submodule update --init` yourself.

## Authentication

The CLI calls your system's `git`, with the credentials you've already set up (SSH keys, a credential helper, a token CI
injects). It sets one environment variable, `GIT_TERMINAL_PROMPT=0`: without credentials `git` fails at once, rather
than stopping in CI to wait for a password that never comes. On failure, `git`'s own words appear in the error. See
[Distributing through Git](../03-component-guide/10-git-distribution.md#private-repositories-and-authentication).

## Cleaning up

Deleting the whole of `~/.cache/brickkit/repos/` is safe: it's only a cache, re-cloned the next time it's needed.
Temporary directories left by interrupted clones are cleared automatically by later runs once they're old enough.
