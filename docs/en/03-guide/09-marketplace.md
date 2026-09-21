# 9. Publish and Install from a Marketplace

Every earlier article installed `demo/hello` from a directory on disk — a `local` source, or the local Git repositories of the previous article. This one runs a real marketplace (the same one [Self-hosting the BrickKit Market](../07-patterns/09-deployment/self-hosted-market.md) covers deploying), publishes `demo/hello` to it, and installs it into a completely separate project that never touches the component's source directory at all.

## Start a real marketplace

```bash
cd deploy/market
cp .env.example .env && chmod 600 .env
# edit .env: set real values for POSTGRES_PASSWORD / RUSTFS_SECRET_KEY / ADMIN_PASSWORD
docker compose up -d --build
curl http://localhost:8080/api/v1/health
```
```json
{"success":true,"data":{"status":"ok","time":"...","version":"dev"}}
```

## Log in, non-interactively

A publishing project points at the market as a `type: market` source, same shape as any other:

```yaml
sources:
  - id: brickkit-market
    type: market
    url: http://localhost:8080/api/v1
```

```bash
echo -n "guideAdminPw1" | brickkit login --username admin --password-stdin
```
```
🔐 Logging in to the BrickKit Market
✅ Logged in
   Token stored at .brickkit/credentials
   Valid until: 2026-10-14T15:47:20Z
```

## Publish it

```bash
brickkit publish --path ./components/demo/hello --source-type git \
  --git-url https://example.com/demo/hello.git
```

The first attempt, with no flags about the image, refuses outright:

```
❌ Error: the image digest could not be determined; publishing was aborted
   Image: brickkit-demo/hello:1.0.0
   Why it is blocked: A published version number can't be taken back. Not being able
   to get the digest usually means consumers can't pull this image either — better to
   stop now than leave an uninstallable version in the Market
```

This is [Signing and the trust model](../06-architecture/06-signing-and-trust.md)'s digest-pinning step, refusing to publish a reference nobody else could actually resolve — `demo/hello`'s image only ever existed in this one machine's local Docker cache, never pushed anywhere. `--no-pin-digest` is the honest way past that for a demo like this one (a real publish against a real registry wouldn't need it):

```bash
brickkit publish --path ./components/demo/hello --source-type git \
  --git-url https://example.com/demo/hello.git --no-pin-digest
```
```
📤 Publishing demo/hello@1.0.0
   ✅ Manifest validation passed
   ✅ Image reference is valid: brickkit-demo/hello:1.0.0
   ⚠️ Digest pinning skipped (--no-pin-digest)
   ✅ artifacts uploaded (1 file)
   ✅ Upload succeeded
🎉 Published
```

A closed-source attempt (`--source-type registry`) with this exact same Manifest refuses for a completely different, real reason:

```
❌ Error: publishing the component version failed
   Reason: A closed-source component that offers an API must upload an API contract file
   hint: Declare at least one artifact with type: api-contract under artifacts
   (the code may be closed-source; the API contract may not)
```

(The `Reason` text and the `hint` in this block are the Market **server's** own words, forwarded verbatim by the CLI — everywhere else on this page is the CLI's own output.)

`demo/hello`'s only declared artifact is `type: api-docs` — human-readable documentation, not a machine-consumable contract (a protobuf file, an OpenAPI spec meant for codegen). The marketplace enforces this distinction specifically for closed-source components: hiding the implementation is fine, hiding the shape callers need to integrate against is not.

## Install it from a completely separate project

A new project, a market source, nothing local at all:

```bash
mkdir consumer-project && cd consumer-project
brickkit init consumer-project
# add the same market source
brickkit add demo/hello@1.0.0
```

```
📦 Adding demo/hello@1.0.0
   ├── Manifest ✅
   └── artifacts ✅ (1 file)
⚠️ Warning: requireSignature is true, but the project declares no trusted public keys, so signature verification isn't actually in effect
   Note: This isn't a misconfiguration, just an unfinished setup — requireSignature
   defaults to true, while publicKeys can only be filled in once you have the publisher's
   public key
```

The Manifest and artifacts really did come from the market this project has never installed anything from before — and the warning is real, not hypothetical: this project genuinely has no trust anchor configured yet, exactly the gap [Signing and the trust model](../06-architecture/06-signing-and-trust.md) describes. `brickkit up` still works — signature enforcement being unconfigured is a warning, not a block:

```bash
brickkit up
curl http://localhost:8099/api/v1/hello
```
```json
{"component":"demo/hello","greeting":"Hello","message":"Hello, I'm demo/hello@1.0.0","version":"1.0.0"}
```

## Versions are permanent, and visibility is enforced

Publishing `demo/hello@1.0.0` a second time — even identically — is refused:

```
❌ Error: demo/hello@1.0.0 has already been published
   Status on the Market: stable
   Reason: Once published, a version number can't be taken back, and a soft-deleted version still holds its place
```

Publish `2.0.0` as `--visibility private` instead, and an unauthenticated project trying to install it gets a real, specific denial — not a generic 404 pretending the version doesn't exist:

```bash
brickkit add demo/hello@2.0.0   # from a project that never logged in
```
```
❌ Error: no access to this component: demo/hello
   Suggestions:
   1. Confirm the current account owns this component
   2. Private components can only be accessed with the owner's authorization
```

## Log out when you're done

Back in the publisher's project (that's where you logged in, and where `.brickkit/credentials` lives). Publishing and installing are done, so clear the login:

```bash
brickkit logout
```
```
✅ Logged out
   User: admin
   Deleted: .brickkit/credentials
```

`logout` works in two steps: it tells the marketplace to revoke the token, then deletes the local `.brickkit/credentials`. **The local file is always deleted**, even if the marketplace can't be reached at that moment — otherwise one network blip would leave you believing you'd logged out while the credential still sat on disk. If the marketplace couldn't be told, it says so plainly: that token stays valid on the marketplace side until it expires on its own. Working offline and want to skip the notification outright? Add `--keep-remote`:

```bash
brickkit logout --keep-remote
```
```
✅ Logged out
   User: admin
   Deleted: .brickkit/credentials
   ⚠️ Skipped notifying the Market because of --keep-remote
      That token remains valid on the Market side until 2026-10-14 15:47:20
```

Running it again when you're already logged out does nothing, and isn't a failure:

```
📋 There are no login credentials right now (.brickkit/credentials does not exist)
   Log in to the Market with brickkit login
```

---

Next: [Sign and verify components](10-signing.md) — closing the gap the warning above pointed at, actually configuring a trust anchor and verifying a signature for real.
