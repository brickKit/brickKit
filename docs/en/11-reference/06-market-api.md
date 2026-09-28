# Market API reference

The HTTP interface of BrickKit Market, the component marketplace. Every endpoint listed here is implemented and matches the server's route table one to one. A test in `market-server` fails if this page lists an endpoint that does not exist, or leaves out one that does.

You rarely need to call these endpoints yourself: `brickkit login`, `publish`, `add` and `fetch` wrap them. This page is for anyone writing their own market client, or trying to understand exactly how the market behaves.

## The market is optional

A market is one kind of **install source**. BrickKit does not need one to work: a project that uses only local and git sources can install, deploy and upgrade components.

- **Git source.** A component is released by tagging its repository. When the component sits at the repository root, the tag is the version (`1.2.0`). When it sits in a subdirectory named `<scope>-<name>/`, the tag is `<scope>-<name>/1.2.0`. `brickkit release` checks the working tree, then creates and pushes the tag for you.
- **Local source.** Reads component directories straight from disk; this is what you use while developing.

On top of that, a market gives you a few things git cannot, or can only do awkwardly:

| What the market adds | Why a git source can't |
| --- | --- |
| Search and browsing by tag | Git fetches by repository URL; it has no idea which components exist |
| Private components, granted to users and organizations | Git permissions apply to a whole repository, not to a component |
| Closed-source distribution: an image and an API contract, no source | Installing from a git source reads the repository itself |
| A place for the publisher's signature to travel with the release | A git tag has nowhere to carry a BrickKit signature |
| Delisting after the fact (`blocked`): a bad release stops being installable | Deleting a git tag doesn't stop anyone who already cloned, and nobody is told why |

Without a market, everything else works the same. When you need the features above, run your own (or use the one your team already runs). There is no public market operated by the BrickKit project.

## Conventions

**Base URL.** The address of the market instance you use. Every path starts with `/api/v1`.

**Authentication.** Endpoints that need it take a bearer token:

```
Authorization: Bearer <token>
```

`POST /api/v1/auth/login` issues the token. It is the value `brickkit login` stores in `.brickkit/credentials`. Reading public components needs no token; everything about a private component, and every publish, does.

**Response envelope.** Every response except the doc endpoint (described below) has the same shape:

```json
{"success": true, "data": { "...": "..." }}
```

```json
{"success": false, "error": {"code": "MANIFEST_INVALID", "message": "the Manifest failed validation", "details": { "...": "..." }}}
```

`error.details` carries structured information such as the individual validation problems or conflict details; not every error has it. Messages are always in English. If you branch on errors, branch on `code`: it is stable.

## Error codes

| Code | HTTP status | Meaning |
| --- | --- | --- |
| `INVALID_REQUEST` | 400 | The body is not valid JSON, is larger than 8 MiB (`details.limitBytes`), or a request field (source type, version, visibility, doc, …) is invalid |
| `MANIFEST_INVALID` | 400 | The Manifest in a publish request failed validation; `details.problems` lists each problem |
| `CONFIG_SCHEMA_RESERVED_VARIABLE_CONFLICT` | 400 | A `configSchema` key has the same name as a variable the platform injects |
| `CLOSED_SOURCE_MISSING_API_CONTRACT` | 400 | A closed-source component declares no `api-contract` artifact |
| `UNAUTHORIZED` | 401 | No token, or the token is invalid or expired |
| `FORBIDDEN` | 403 | The token is valid, but this identity isn't allowed to do this |
| `COMPONENT_BLOCKED` | 403 | A market admin has delisted the component or version |
| `NOT_FOUND` | 404 | The component, version, organization or doc doesn't exist |
| `CONFLICT` | 409 | A state conflict, such as adding a user who already belongs to another organization |
| `VERSION_ALREADY_EXISTS` | 409 | The version number has been used. Version numbers can't be republished, and a soft-deleted version keeps its number |
| `INTERNAL` | 500 | Something failed inside the market. The cause goes to the server log only; it is never sent to the client |

## Endpoints

### Operations and audit

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| GET | `/api/v1/health` | No | Health check: returns `status`, the server version and the current time. A self-hosted deployment's container healthcheck probes this |
| GET | `/api/v1/audit` | Yes | The audit log. Query parameters: `componentId`, `action`, `limit` |

### Accounts

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| POST | `/api/v1/auth/register` | No | Create an account. Body: `username`, `password`, optional `email` |
| POST | `/api/v1/auth/login` | No | Log in. Body: `username`, `password`. The response carries `token` and `expiresAt` |
| POST | `/api/v1/auth/logout` | Yes | Revoke the token this request carries (the account's other tokens are untouched). Calling it again is not an error |

⚠️ An `orgId` in a registration body is ignored. Organization membership is what grants read access to an organization's private components; if people could declare their own organization at sign-up, anyone could read anyone's private components. The only way into an organization is the "add member" endpoint below, called by the organization's owner or a market admin.

### Organizations

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| GET | `/api/v1/organizations` | Yes | List organizations. A regular user sees only their own; an admin sees all |
| POST | `/api/v1/organizations` | Yes | Create an organization. The creator becomes its owner and a member |
| POST | `/api/v1/organizations/{orgId}/members` | Yes (organization owner or admin) | Add a member |

A user belongs to at most one organization. Adding someone who is already in another organization returns `CONFLICT` rather than silently moving them, which would cut them off from their current organization's private components. Adding the same person twice is not an error.

### Components

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| GET | `/api/v1/components` | Depends on visibility | Search. Query parameters: `keyword`, `page`, `pageSize`, `tags` (comma-separated, or repeated). Returns only components that aren't delisted and that the caller can see |
| GET | `/api/v1/components/{scope}/{name}` | Depends on visibility | Component details and its list of version numbers |
| PUT | `/api/v1/components/{scope}/{name}/visibility` | Yes (owner) | Set visibility: `public` or `private` |
| GET | `/api/v1/components/{scope}/{name}/access` | Yes (owner) | Which users and organizations a private component is granted to |
| PUT | `/api/v1/components/{scope}/{name}/access` | Yes (owner) | Replace the access policy |

A component ID has two parts, `scope/name`, and the path has both: `/api/v1/components/people/basic`.

A component is created by its first publish, and the publisher becomes its owner. Namespaces are first come, first served, except `brickkit/` and `infra/`, which are reserved for market admins.

### Versions

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| GET | `/api/v1/components/{scope}/{name}/versions` | Depends on visibility | List versions (without the Manifest body). `draft` versions are visible to the owner only; deleted versions are not listed |
| POST | `/api/v1/components/{scope}/{name}/versions` | Yes (only the owner, for an existing component) | Publish a new version; see [Publishing](#publishing) |
| PUT | `/api/v1/components/{scope}/{name}/versions/{version}` | Yes (owner; delisting is admin-only) | Change the status to `draft`, `stable` or `deprecated`; only an admin can set `blocked`. Moving to `stable` requires every declared artifact file to be uploaded |
| DELETE | `/api/v1/components/{scope}/{name}/versions/{version}` | Yes (owner) | Soft delete: the version behaves as if it doesn't exist, but its number stays taken |
| GET | `/api/v1/components/{scope}/{name}/versions/{version}/manifest` | Depends on visibility | The version's Manifest; this is where `brickkit add` gets it |
| GET | `/api/v1/components/{scope}/{name}/versions/{version}/doc` | Depends on visibility | The version's `BRICKKIT.md`; see [Component docs](#component-docs) |

The `data` of `/manifest` holds `manifest` (the `component.yaml` as JSON), `status`, `sourceType` (`git` for open source, `registry` for closed source), `gitUrl` (the open-source repository, which `add --repo` clones) and `signature` (the signature supplied at publish time; absent when there is none).

Whether a caller gets a version: a `draft` is visible to its owner only, a `blocked` version answers `COMPONENT_BLOCKED`, a deleted one answers `NOT_FOUND`, and a private component answers `FORBIDDEN` to anyone without a grant. `/doc` applies exactly the same rules as `/manifest`.

### Artifacts

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| GET | `/api/v1/components/{scope}/{name}/versions/{version}/artifacts` | Depends on visibility | The version's registered artifacts, each with `id`, `type`, `format` and `files` |
| POST | `/api/v1/components/{scope}/{name}/versions/{version}/artifacts/{artifactId}/upload` | Yes (owner) | Upload one file of a registered artifact. The `file` query parameter is its path inside the component; the body is the file content. At most 64 MiB per file |
| GET | `/api/v1/components/{scope}/{name}/versions/{version}/artifacts/{artifactId}/download` | Depends on visibility | Download one artifact file; `file` as above |

The artifact **list** (which artifacts exist and which files each has) is registered at publish time from the Manifest's `artifacts`. An upload can only deliver a file that list declares.

## Publishing

The body of `POST /api/v1/components/{scope}/{name}/versions`:

```json
{
  "version": "1.2.0",
  "status": "draft",
  "manifest": { "apiVersion": "brickkit/v1", "kind": "Component", "metadata": { "id": "people/basic", "version": "1.2.0" } },
  "sourceType": "git",
  "gitUrl": "https://github.com/example/people-basic",
  "changelog": "Add a status field to people",
  "visibility": "public",
  "doc": "# people/basic\n\nHow to call it…\n",
  "signature": {
    "algorithm": "cosign",
    "publicKeyRef": "keys/vendor.pub",
    "value": "<base64 signature>",
    "signedBy": "release-bot@example.com"
  }
}
```

| Field | Required | Description |
| --- | --- | --- |
| `version` | Yes | Must equal `manifest.metadata.version` |
| `status` | No | `draft` when omitted |
| `manifest` | Yes | The `component.yaml` as JSON; stored as sent |
| `sourceType` | Yes | `git` (open source; `gitUrl` is then required) or `registry` (closed source) |
| `gitUrl` | For open source | The component's git repository |
| `changelog` | No | What changed in this version |
| `visibility` | No | `public` or `private`. When given, the component is set to it, existing components included; when omitted, a new component is `public` and an existing one keeps its setting |
| `doc` | No | The full text of `BRICKKIT.md` from the component's root: UTF-8, at most 256 KiB |
| `signature` | No | The publisher's signature over the Manifest. The market checks only its structure (a known algorithm, required fields present, `value` valid base64); it does no cryptographic check, because it holds no public key worth trusting. Verification happens on the installing side, against the public keys the project configures |

### What the market checks

**The Manifest rules are the CLI's rules: the same code.** A `component.yaml` that `brickkit lint` accepts in the component repository passes the market's Manifest checks, and whatever the CLI rejects, the market rejects: misspelled field names (an unknown field is refused, never silently ignored), version ranges such as `^1.0.0`, shell members without an exact version. A request that bypasses the CLI and calls this endpoint directly gets exactly the same checks.

Each problem is reported separately in `details.problems`:

```json
{
  "success": false,
  "error": {
    "code": "MANIFEST_INVALID",
    "message": "the Manifest failed validation",
    "details": {
      "problems": [
        {"field": "dependencies.components[0]", "reason": "..."},
        {"field": "futureField", "reason": "..."}
      ]
    }
  }
}
```

Manifest problems and request-field problems (`version`, `sourceType`, `visibility`, …) come back together, so one round of fixes is enough.

**On top of that, the market checks a few things that only matter for a market release:**

1. **`deployment.image` is required.** Whoever installs from the market has no source code, so they can't build an image. A component that only declares `deployment.build` (built from source) is distributed through a git source.
2. **No config key may take the name of a platform-injected variable.** A `configSchema` key is the name of the environment variable the component receives; if it collides with a name the platform injects itself, the component never sees its own value. The reserved names are `COMPONENT_ID`, `COMPONENT_VERSION`, `BRICKKIT_SERVED_MEMBERS`, `BRICKKIT_SERVED_MEMBERS_CONFIG`, `PORT`, and every name ending in `_ENDPOINT`. When the CLI deploys such a component it only warns and skips that key, since the component may come from a source that never went through a market. The market refuses it at publish time with `CONFIG_SCHEMA_RESERVED_VARIABLE_CONFLICT`; each entry in `details.conflicts` has `configKey`, `conflictPattern`, and a `suggestion` that avoids the pattern.
3. **A closed-source component must ship its API contract.** A `sourceType: registry` component declares at least one artifact with `type: api-contract`: the code may stay private, the interface its callers depend on may not.
4. **`doc` is at most 256 KiB.** It arrives as a JSON string, so it is text by construction; `brickkit publish` checks the size before it creates the version, and refuses a file that isn't UTF-8. This problem is reported together with the Manifest and request-field problems.

### Publishing takes three requests

`brickkit publish` wraps them in one command:

```mermaid
sequenceDiagram
    participant CLI as brickkit publish
    participant Market as Market

    CLI->>Market: POST .../versions (Manifest, doc, status: draft)
    Market-->>CLI: 201 (version registered, artifact list known)
    loop each artifact file
        CLI->>Market: POST .../artifacts/{artifactId}/upload?file=...
        Market-->>CLI: 200
    end
    CLI->>Market: PUT .../versions/{version} (status: stable)
    Market-->>CLI: 200 (installable with add)
```

A version left at `draft` can't be installed. If a publish fails halfway, run `brickkit publish` again: it recognizes the unfinished `draft` and carries on uploading. If `component.yaml` or `BRICKKIT.md` changed in the meantime, even with the same version number, the CLI refuses to resume and asks for a new version number: what is registered can't be changed, and resuming would pair the old Manifest or doc with the new artifacts.

## Component docs

`GET /api/v1/components/{scope}/{name}/versions/{version}/doc` returns the `BRICKKIT.md` published with that version:

- On success, the body is the file itself with `Content-Type: text/markdown; charset=utf-8`. It is **not** wrapped in the envelope; it is a file.
- A version published without a doc answers `404` with code `NOT_FOUND`, in the usual JSON envelope. Every version published before the market supported docs answers this way.
- Who may read it, and which versions they see, follows exactly the rules of `/manifest`.

`BRICKKIT.md` is what a component tells its callers: how to call it, how to configure it, what to watch out for. It is written for people and for AI assistants alike. When `brickkit add` or `fetch` gets a Manifest, it gets the doc too and caches it at `.brickkit/manifests/<scope>/<name>/<version>/BRICKKIT.md`, the same as for local and git sources. A missing doc is not an error; the component installs and runs as usual.

**The doc is not signed.** The signature protects what gets executed: the Manifest decides what is deployed, what is injected and which migration runs, and the image is the code that runs. The doc is explanatory text; changing it changes nothing that runs. The worst a compromised market can do with it is mislead a reader, not alter a deployment. Signing the doc would mean burning a new version number to fix a typo.

## Endpoints that deliberately don't exist

| Missing endpoint | Why |
| --- | --- |
| `POST /api/v1/components` (create a component on its own) | The first publish creates the component; a separate create call would have no caller |
| `PUT /api/v1/components/{scope}/{name}` (edit component details) | Name, description and vendor follow the Manifest and update with each new version. Editing them separately would let the market's description drift from the Manifest |
| `DELETE /api/v1/components/{scope}/{name}` (hard delete) | Someone may already have the component in a project. To stop new installs, delist it (`blocked`) |
| `GET /api/v1/components/{scope}/{name}/versions/{version}` (one version's details) | The version list already has everything |
