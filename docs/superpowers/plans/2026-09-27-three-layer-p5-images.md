# P5: Images — `brickkit build`, the `up` image check, shell image labels

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Building and deploying are separate (§9.10.1).
- `brickkit build` builds the images that need a local build. Each image is tagged exactly `metadata.version`, and a shell image records the member versions it was built with.
- `up` never builds. It checks up front that every image it will use is there, and fails loudly with the `brickkit build` command to run.
- `up` compares a locally built shell image's recorded member versions with the shell's `component.yaml` (A24's image-label safety net).

**Architecture:**
- **`internal/engine` gains an `Images` interface**, implemented by `Compose` (docker / podman) and separate from `Engine`, so the K8s engine is untouched: `ImageExists`, `ImageLabels`, `Build`. `cli.Options.Images` injects a fake in tests, the same way `Options.Engine` does.
- **`internal/source` answers two new questions**:
  - Does a local install source serve this version (`IsLocal`)?
  - Export this version's source tree from its git tag in the bare-repo cache (`ExportSource`), so a version whose code is not in any local repo can still be built.
- **Image references everywhere come from `manifest.ImageRef`**. Today `up`'s check reads `deployment.image` as written, so a build-only component is checked as an empty image name.

**Tech Stack:** Go 1.22, system `docker` / `podman` (tests use fake runners, no daemon), system `git` for `ExportSource` tests.

**Spec:** `new_plan/提案.md` §9.10 (all), §11.2, §8.1 "外壳成员的独立镜像要求", §8.5 rule 3, Appendix A24 (the image label paragraph). `new_plan/命令表.md` command 10 (up, step 3 and the image check table) and command 17 (build).

## Decisions stated to the user (not questioned)

- **Which versions `build` builds:** every declared version whose image is not something to pull:
  - its Manifest has no `deployment.image` (build-only), or
  - a local install source serves it (code under development: the image must come from that code, §9.10.3).

  Components with `image` from git or market are pulled by the engine, not built. `build <id>` builds every declared version of that id; `build <id>@<ver>` builds one. Naming a component that does not need a build prints why and does nothing.
- **Where the build source comes from:**
  - The component's local repo (`Project.LocalRepo`) when it holds exactly that version.
  - Otherwise, the version's git tag exported from the bare-repo cache into a temporary directory. This covers compatibility versions and git components that were never cloned.
  - A market component without `image` cannot be built (no source), which is an error naming the component.
- **Build command:**
  - `docker build` (or `podman build` when the effective deploy target is podman).
  - `-t <manifest.ImageRef>`, `-f <deployment.build.dockerfile>`, context `<deployment.build.context>`, both relative to the source root.
  - Labels:
    - `io.brickkit.component=<id>`
    - `io.brickkit.version=<version>`
    - for shells, `io.brickkit.shell.members=<comma-separated id@version, sorted>`
- **Cache reuse and `--force`:** an existing image with that tag is skipped unless `--force` (§11.2).
- **The `up` check** (docker / podman targets only; K8s pulls from the cluster's registry and stays as it is):
  - a version served by a local source, or with no `image`, must exist locally, or `up` fails with `brickkit build <id>`;
  - a version with `image` from git or market is checked as today: present locally or pullable.

  Every missing image is listed in one error, not only the first.
- **Shell label check:** when a shell's image exists locally, `up` reads its labels.
  - A members label that differs from `shell.members` is an error with `brickkit build <shell> --force`.
  - No label (hand-built or third-party image) is a warning: "the member versions inside this shell image cannot be confirmed".
  - An image that is only in a registry is not checked; a released shell tag is consistent by construction (A24).
- **§8.5 rule 3 (member without its own image):** already enforced for every component by Manifest validation (`deployment.image` or `deployment.build` is required). P5 adds a test that pins it for `add` of a shell member, and the member's migration image is checked by `up` like any other.

## Global Constraints

- Messages go through `internal/msgid` and both catalogs; the i18n guard and the message guard test stay green; no unused msgids.
- Chinese comments matching the surrounding style.
- Commit on main after each task, with the message ending in `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- After each task run all of the following. The doc checks in `make lint` belong to P9.
  - `go build ./...` and `go vet ./...`
  - `go test -count=1 ./internal/... ./cmd/... ./tests/i18nguard/... ./tests/archguard/...`
  - `.tools/bin/golangci-lint run ./...`
  - `GOOS=windows` / `GOOS=darwin` builds
  - `make check-schemas check-i18n cover-check`
- No new flags beyond 命令表 17 (`--force`). `build` accepts `<id>` or `<id>@<version>`, like the other component commands.
- Out of scope:
  - Pushing images to a registry.
  - Loading local images into a K8s cluster (P9 documents it).
  - `release` (P7).

## Review Focus

1. **A build-only component in a fresh project:**
   - `up --dry-run` succeeds (generation does not need images).
   - `up` fails before starting anything, naming `brickkit build <id>`.
   - After `build`, `up` starts it with the image `<scope>-<name>:<version>`.
2. **A local-source component that also declares `image`:** without a local image, `up` fails with the build hint. It never pulls the registry image in its place, because that would run different code than the local source.
3. **A shell rebuilt after its `component.yaml` changed a member version:**
   - Before `build --force`, `up` fails and names both versions.
   - After it, `up` passes.
   - A shell image without the label only warns.
4. **`build` of a compatibility version (non-default, not in the local repo):** the source is exported from the tag and the tag is correct. With the remote gone, it still works from the cache.
5. **`build` when the image exists:** it skips and says so; `--force` rebuilds. A failed `docker build` shows the build's last output lines and stops at the first failure.

---

### Task 1: Image operations on the engine

**Files:**
- Modify: `internal/engine/engine.go` (the `Images` interface and `BuildRequest`), `internal/engine/compose.go` (implementation), msgids.
- Test: `internal/engine/images_test.go`.

**Interfaces (Produces):**
```go
// Images 是本机镜像的操作：只有 docker / podman 有（K8s 从集群的 registry 拉，不经过本机）。
type Images interface {
    ImageExists(ctx context.Context, ref string) (bool, error)
    // ImageLabels 返回本机镜像的标签；镜像不在本机时 ok 为 false。
    ImageLabels(ctx context.Context, ref string) (labels map[string]string, ok bool, err error)
    Build(ctx context.Context, req BuildRequest) error
}
type BuildRequest struct {
    Tag        string
    Context    string            // 构建上下文目录（绝对路径）
    Dockerfile string            // 绝对路径
    Labels     map[string]string
}
```

**Rules:**
- `ImageExists`: `<bin> image inspect --format {{.Id}} <ref>`. Exit 0 → true. A "no such image" style failure → false, nil. Anything else (daemon down, binary missing) → error through the existing `exec` translation.
- `ImageLabels`: `<bin> image inspect --format {{json .Config.Labels}} <ref>`, parsed. `null` → empty map, ok true.
- `Build`: `<bin> build -t <tag> -f <dockerfile> --label k=v … <context>`, labels sorted, streaming nothing. On failure, the error carries the last 20 lines of output under `LabelOutput`.

- [ ] **Step 1: Failing tests** with a fake runner recording argv:
  - `TestImageExists`: exists / missing / daemon error.
  - `TestImageLabelsParsesJSON`: with labels and with `null`.
  - `TestBuildArgv`: exact argv with sorted labels; podman uses `podman`.
  - `TestBuildFailureKeepsOutputTail`.
- [ ] **Steps 2–4.** Commit `feat(engine): local image inspection and build`.

### Task 2: Source answers "is it local" and exports a version's source

**Files:**
- Modify: `internal/source/source.go`, `internal/source/git.go`, `internal/source/gitcache.go`.
- Test: `internal/source/export_test.go`.

**Interfaces (Produces):**
- `func (c *Client) IsLocal(ctx context.Context, id, version string) bool`: a local install source (or the component's own `source.type: local`) serves exactly this version. This is `servedByLocalSource`'s rule, exported and version-exact.
- `func (c *Client) ExportSource(ctx context.Context, id, version, dest string) error`: writes the tree of this version's tag into `dest`, through the component's fetchers in order. For a git subpath, it writes the subdirectory's contents as the root. The implementation is `git archive --format=tar <tag>[:<subpath>]` extracted in Go (`archive/tar`). Paths escaping `dest` are rejected. Local and market sources return `errNotFound`, and the aggregate error names the component.

- [ ] **Step 1: Failing tests:**
  - `TestIsLocalIsVersionExact`
  - `TestExportSourceFromTag`: the files match the tag, not HEAD.
  - `TestExportSourceMonorepoSubpath`
  - `TestExportSourceOfflineFromCache`
  - `TestExportSourceMarketHasNone`
- [ ] **Steps 2–4.** Commit `feat(source): tell local versions apart and export a version's source from its tag`.

### Task 3: `brickkit build`

**Files:**
- Create: `internal/cli/build.go`.
- Test: `internal/cli/build_test.go` (fake `Images`; real git for export).
- Modify: `internal/cli/root.go` (register the command, `Options.Images`), msgids (use, short, long, example, flag, output lines, errors).

**Rules:**
- **Arguments:** `build [<id>[@<version>]] [--force]`. Load the project with `loadForInstall`-style options (team deploy file); the images engine follows the effective target (podman or docker).
- **Candidates:** declared versions whose Manifest has no `image` or which `client.IsLocal` reports as local. When filtered by id or version, a named component that needs no build prints `CliBuildNotNeeded` (its image is pulled) and does not error.
- **For each candidate:**
  1. Compute the tag with `manifest.ImageRef`.
  2. If the image exists and `--force` is off, print "skipped (exists)".
  3. Otherwise resolve the source dir: `proj.LocalRepo(id)` if `project.LocalRepoVersion` equals the version; else `client.ExportSource` into a temp dir (removed afterwards).
  4. Build with the labels in the Decisions section.
  5. Print ✅ with the tag.
- **Failures:** stop at the first build failure, keep what was built so far, and name the failed component.
- **Order:** the dependency order of the resolved graph, so a failure is reported where a user expects it.

- [ ] **Step 1: Failing tests:**
  - `TestBuildBuildsLocalAndBuildOnlyVersions`: a pullable component is not built.
  - `TestBuildTagsWithManifestVersion`
  - `TestBuildSkipsExistingUnlessForce`
  - `TestBuildShellRecordsMemberVersions`
  - `TestBuildCompatibilityVersionFromTag`
  - `TestBuildNamedComponentThatNeedsNoBuild`
  - `TestBuildStopsAtFirstFailure`
- [ ] **Steps 2–4.** Commit `feat(cli): brickkit build builds local and build-only images tagged with their version`.

### Task 4: `up` checks images and shell labels

**Files:**
- Modify: `internal/cli/up.go` (`imageInfo` from `manifest.ImageRef` plus a `needsLocal` flag; the new check replaces `checkImages`), msgids.
- Test: `internal/cli/checkimages_test.go` (the existing concurrency test keeps its shape), `internal/cli/up_images_test.go`.

**Rules:**
- **Scope:** docker / podman targets. K8s keeps `CheckImage` (a no-op there).
- **Images checked:** every workload image this run: containers, member migration images, shell images. Each is `manifest.ImageRef`; `needsLocal` is true for build-only or local-source versions.
- **Checks** (concurrent, results reported in input order):
  - `needsLocal` → `ImageExists`; missing → collected under `CliUpImageNeedsBuild` with the hint `brickkit build <id>@<version>`.
  - Otherwise → `eng.CheckImage`, as today.
  - All failures are reported in one error: one detail line per component, with hints.
- **Shell label check,** after the existence checks pass: for each running container shell whose image exists locally:
  - label present and different from `shell.members` (sorted) → `CliUpShellImageStale` naming the shell, the image's member list and the declared one, with the hint `brickkit build <shell> --force`;
  - label absent → a warning, `CliUpShellImageUnlabelled`.
- **`--dry-run`** does not check images (unchanged): generation needs none.

- [ ] **Step 1: Failing tests:**
  - `TestUpBuildOnlyImageMissingPointsToBuild` (Review Focus 1)
  - `TestUpLocalSourceNeverPullsInstead` (Review Focus 2)
  - `TestUpListsEveryMissingImage`
  - `TestUpShellImageLabelMismatch`
  - `TestUpShellImageWithoutLabelWarns`
  - `TestUpShellImageOnlyInRegistryNotChecked`
  - `TestAddShellMemberWithoutImageFails` (§8.5 rule 3 pinned)
  - `TestUpChecksImagesByImageRef`: a build-only component is checked by its derived tag, never an empty string.
- [ ] **Steps 2–4.** Commit `feat(cli): up checks local images before starting and compares shell image labels`.

### Task 5: Final review

- [ ] Run `review-package` over the plan's range and dispatch the final reviewer (opus) with the Review Focus above, the ledger's `Ruling:` lines, and the Decisions section. Do one fix pass, verified by TDD. Report to the user in Chinese, then update the roadmap row and project memory.
