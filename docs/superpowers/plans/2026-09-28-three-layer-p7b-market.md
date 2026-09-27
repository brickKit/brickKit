# P7b: align the market with the three-layer model

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** a component that passes `brickkit lint` publishes to a market and `add`s back from it. The market
accepts exactly what the CLI accepts, because both run the same rules; it serves the component's
`BRICKKIT.md` like the git source does; its tests are green again.

**Architecture:**
- **One rule set.** `market-server` stops keeping its own copy of the manifest rules
  (`internal/validator`, ~1350 lines) and validates with the CLI's `internal/manifest`.
- **How it can import that package.** Go's `internal/` rule is path-based: the module is renamed from
  `github.com/brickkit/market-server` to `github.com/brickkit/brickkit/market-server`, with
  `replace github.com/brickkit/brickkit => ../`. That makes the main module's `internal/` packages
  importable while `market-server` stays its own module, with its own deps (aws, pq) that never reach the
  CLI. A throwaway experiment on 2026-09-27 confirmed that a module at `<main>/sub` can import
  `<main>/internal/...` through such a replace.
- **Market-only rules stay in the market.** Visibility, the closed-source contract rule, the rule that a
  market component has an image, and reserved-name refusal are rules about publishing requests, not about
  manifests.
- **Component docs travel through the market.** `publish` sends `BRICKKIT.md`, the market stores and
  serves it, and the CLI's market source caches it (§16.3), the same as local and git sources do.

**Tech Stack:** Go 1.22, net/http + httptest, the market's in-memory repo/storage for tests, PostgreSQL
migrations.

**Spec:**
- `new_plan/提案.md` §16.2–16.3 (component docs, cascade cache), §9.10 (image vs build), §5.2 (reserved
  names, via A10).
- Appendix A: A2 (market untouched until now), A10 (config keys are env var names), A14 (market stays an
  optional source; `publish` stays).
- Roadmap row P7b.
- Market API reference outline: `new_plan/文档修改-布局.md` `11-reference/06-market-api.md`.

## Decisions (rulings on what the spec leaves open or contradicts)

- **Share the rules, don't copy them.** The validator's package comment argues for two independent
  implementations as a "double safeguard". What the safeguard really needs is that the server validates every
  request itself, so a client that skips the CLI is still stopped. That stays true when the server calls the
  same rule code.
  - What two copies actually produced: drift. The market still rejects every three-layer manifest. Its old
    rules are `DATABASE_`-style reserved prefixes, camelCase → SNAKE conversion, `dependencies.resources`,
    bare-ID `shell.members`, `image` required, and no `deployment.build`, `shell` or `local`.
  - Cost if wrong: a rule bug is now in one place instead of two (it is fixed once, too).
- **What the market adds on top of `manifest.Parse` + `Validate`:**
  - **`deployment.image` is required for a market release.** A market ships a manifest plus an image
    reference; a consumer of a market component has no source to `brickkit build` from. A build-only
    component is distributed through git (§9.10). The error says exactly that.
  - **A configSchema key that collides with a reserved name is refused** (AGENTS §5.2: "the marketplace
    refuses this at publish time"). It uses the same `staticReserved` rule the CLI warns with; this moves out
    of `inject` into an exported `inject.ReservedHits(m)`.
  - Request-level rules unchanged: visibility, git URL / registry origin, the closed-source api-contract rule,
    the declared-artifact-files rule.
- **Problems keep their structure.** `clierr.Error` gains `Problems []clierr.Problem`, filled by
  `ProblemSet.Err()`. The market maps them one to one onto its `model.Problem{Field, Reason}`. The published
  document is JSON; JSON is valid YAML, so `manifest.Parse` reads it unchanged, and the unknown-field check
  still works.
- **The market's `model.Manifest` is deleted.** The service uses `*manifest.Manifest` for the id, version,
  image and artifacts it indexes. The stored manifest stays the published bytes, verbatim, as today.
- **Messages.** Rule messages come from the CLI's i18n catalogs in English, the market's language (it is
  English-only by earlier decision). The market does not call `i18n.Set`; `i18n.T` falls back to English.
- **`BRICKKIT.md` through the market:**
  - `publish` adds `doc` (the file's text) to the publish request when `BRICKKIT.md` exists in the component
    directory. It is capped at 256 KiB; a larger one is refused client-side before the request.
  - The market stores it with the version (new nullable column, migration `00N_version_doc.sql`, in both
    repos) and serves `GET /api/v1/components/:scope/:name/versions/:version/doc` (`text/markdown`; 404 when
    the version has none; the same visibility checks as `manifest`).
  - The CLI's market source implements `docBytes`, so `add` / `fetch` cache it at
    `.brickkit/manifests/<id>/<ver>/BRICKKIT.md`.
  - The doc is **not** part of the signed payload. It is advisory text: an altered doc cannot change what
    runs, and the manifest and image stay signed. This is stated in the API reference. Cost if wrong: a
    compromised market can mislead a reader, not a deployment.
- **Route docs.** The route guard test read `docs/archive/design/007-…`, which no longer exists and must not
  be referenced (archived decisions go stale). The phase therefore writes the market API reference its outline
  already names: `docs/zh/11-reference/06-market-api.md` and `docs/en/11-reference/06-market-api.md`. They
  cover the endpoint table, response envelope, error codes, the doc endpoint, and the relation to market-less
  distribution; the two languages are written independently. The guard compares `Routes()` with the endpoint
  table of **both** files. Doing this before P9 is the roadmap's "a phase may write a reference page it makes
  obvious" clause.
- **Docker build context.** The market image now needs the parent module, so it builds from the repository
  root: `docker build -f market-server/Dockerfile .`. The Dockerfile copies `go.mod`/`go.sum` of both modules,
  then `internal/` and `market-server/`. `make docker-market`, the Dockerfile header and `deploy/market`
  follow.
- **The round trip is tested for real.** The CLI's own tests keep their fake market (fast, and it pins the
  request shapes). The phase's "done when" is proven by a new test in `market-server` that starts the real
  handler (in-memory repo and storage) on httptest, then drives the real CLI (`cli.NewRootCommand` / `cli.Run`)
  through `login`, `publish`, `add` and `up --dry-run` on a temp project. This is possible now because
  `market-server` may import `github.com/brickkit/brickkit/internal/cli`.

## Global Constraints

- Messages go through `internal/msgid` and both catalogs, on the CLI side as before. The market's own
  messages stay the English strings they are today.
- Commit on main after each task. The message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- After each task, run the P7 verify set plus `make test-market`:
  - `go build ./...`
  - `go vet ./...`
  - `go test -count=1 ./internal/... ./cmd/... ./tests/i18nguard/... ./tests/archguard/...`
  - `.tools/bin/golangci-lint run ./...`
  - `GOOS=windows` / `GOOS=darwin` builds
  - `make check-schemas check-i18n cover-check`
  - `make test-market`
  - `cd market-server && go vet ./... && ../.tools/bin/golangci-lint run ./...`
- `tests/archguard` rules still hold for the main module; a new rule is added that the main module never
  imports `market-server`.
- Out of scope:
  - market UI or search changes;
  - market-side signing changes;
  - P8's `tests/` fixtures;
  - every doc except the market API reference.

## Review Focus

1. A shell component (`shell.members` with exact versions) and a component with `local:` and `deployment.build`
   next to `image` publish, and `add` back with their members nested.
2. A manifest the CLI rejects (unknown field, `^1.0.0` dependency, `DB_HOST` + `DB_HOST_ENDPOINT`-style reserved
   key) is rejected by the market with each problem as its own `{field, reason}`. A raw API call that bypasses
   the CLI is stopped the same way.
3. A version published before this phase (no doc column value) serves 404 on `/doc`, and `add` still succeeds,
   without a doc cached.
4. A private component's `/doc` answers exactly like its `/manifest` for an unauthorized caller (no leak through
   the new endpoint).
5. The Docker image builds from the repository root and the binary reports its version.

---

### Task 1: structured problems and exported reserved hits

**Files:** `internal/clierr/problems.go`, `internal/clierr/clierr.go`, `internal/clierr/*_test.go`,
`internal/inject/reserved.go`, `internal/inject/*_test.go`.

- [ ] Tests:
  - `TestProblemSetErrCarriesProblems`: `clierr.As(err).Problems` equals the added items, in order.
  - `TestReservedHits`: `inject.ReservedHits(m)` returns `{Key, Pattern}` sorted by key, with the same matches
    as `ReservedKeyWarnings`, which is rebuilt on top of it.
- [ ] Implement, verify, commit `feat(clierr): errors keep their problem list; inject exports reserved hits`.

### Task 2: the market validates with `internal/manifest`

**Files:**
- `market-server/go.mod` (module rename + require/replace), `go.sum`, every import path in `market-server/`.
- `market-server/internal/validator/` (rewrite: request rules only, on top of `manifest.Parse` + `Validate`);
  delete `reserved.go` and the manifest field checks.
- `market-server/internal/model/manifest.go` (delete `Manifest` and its parts).
- `market-server/internal/service/service.go`, tests.
- `tests/archguard` (the main module never imports market-server).

- [ ] Tests first (`market-server/internal/validator/validator_test.go`, rewritten around the new behaviour):
  - `TestValidateAcceptsThreeLayerManifest` (configSchema `DB_HOST`, `shell.members: [a/b@1.0.0]`, `local:`,
    `deployment.build` + `image`)
  - `TestValidateRejectsWhatTheCLIRejects` (unknown field, range dependency, bare-ID member: each problem
    is its own `{field, reason}`, and the field paths are those `manifest.Parse` reports)
  - `TestValidateRequiresImageForTheMarket`
  - `TestValidateRefusesReservedKey` (`PORT`, `X_ENDPOINT`)
  - `TestValidateKeepsRequestRules` (visibility, closed-source contract, undeclared artifact file)
- [ ] Service tests keep passing on the new types; delete the tests of removed rules.
- [ ] Verify (including `make test-market`, still red only on the route docs), commit
  `refactor(market): validate manifests with the CLI's rules`.

### Task 3: `BRICKKIT.md` through the market

**Files:**
- Market side: `market-server/migrations/00N_version_doc.sql`, `internal/repo/{memory,postgres}.go` + the
  shared contract test, `internal/model/model.go` (`PublishRequest.Doc`, `Version.Doc`),
  `internal/service/service.go`, `internal/handler/{handler,versions}.go`.
- CLI side: `internal/market/client.go` (`Doc`), `internal/cli/publish.go`, `internal/source/market.go`
  (`docBytes`), msgid + catalogs.

- [ ] Tests:
  - market: `TestPublishStoresDoc`, `TestDocEndpointServesMarkdown`, `TestDocEndpoint404WithoutDoc`,
    `TestDocEndpointHonoursVisibility`, and the repo contract `TestRepoVersionDocRoundTrip` (memory; postgres
    when `MARKET_TEST_DATABASE_URL` is set).
  - CLI: `TestPublishSendsBrickkitMd`, `TestPublishRefusesOversizedDoc`, `TestAddFromMarketCachesDoc`,
    `TestAddFromMarketWithoutDoc` (fake market).
- [ ] Verify, commit `feat(market): publish, store and serve BRICKKIT.md`.

### Task 4: the market API reference, and the route guard reads it

**Files:** `docs/zh/11-reference/06-market-api.md`, `docs/en/11-reference/06-market-api.md` (remove their
`.gitkeep`), `market-server/internal/handler/routes_doc_test.go`.

- [ ] Test first: the guard parses the endpoint table of both files (`| METHOD | /api/v1/… | … |`) and compares
  with `Routes()` both ways; its self-check requires at least one row per file and the two files to list the
  same routes.
- [ ] Write the zh reference, then the en one independently (outline from `文档修改-布局.md`: optional
  infrastructure, endpoint list, response envelope and error codes, the doc endpoint and why the doc is
  unsigned, relation to market-less git distribution).
- [ ] Verify (`make test-market` green now), commit `docs(market): API reference; route guard reads it`.

### Task 5: Docker build from the repository root

**Files:** `market-server/Dockerfile`, `Makefile` (`docker-market`, `build-market`), `deploy/market/**` build
references.

- [ ] `make docker-market` builds; `docker run --rm brickkit/market-server:dev --version` (or the health
  endpoint in a short-lived container) reports the version. Review Focus 5.
- [ ] Commit `build(market): image builds from the repository root`.

### Task 6: the real round trip

**Files:** `market-server/internal/e2e/roundtrip_test.go` (new).

- [ ] Test: start the handler with the in-memory repo and storage on httptest. With a temp HOME and project,
  and a component skeleton made by `brickkit new` then filled in (image, configSchema, `BRICKKIT.md`), run:
  - `lint` → 0
  - `login` (register + login through the API)
  - `publish --market <url>` → 0
  - in a second project with a `type: market` source: `add <id>@<ver>` → 0, the doc cached, the config
    skeleton written
  - `up --dry-run` → 0

  A second case publishes a shell with one member and adds the shell back: members nested, `kind: shell`.
  A third case posts a raw manifest with an unknown field straight to the API: 4xx with that field.
- [ ] Verify, commit `test(market): publish and add back through the real server`.
