# 10. Sign and Verify Components

Article 9 ended with a real, live warning: a project that installs from a marketplace with no `installer.publicKeys` configured gets `requireSignature: true` for free, and it does nothing. This article closes that gap for real — generating an actual keypair, publishing a signed component, configuring a project to actually trust that key, and watching verification both succeed and genuinely fail.

**Prerequisite, publisher side only:** cosign (`go install github.com/sigstore/cosign/v2/cmd/cosign@latest`) — signing is the one operation in this whole series that needs it; every `brickkit add` in every other article has verified with nothing but the Go standard library (AGENTS.md §5.9), and that doesn't change here either.

## Generate a real keypair and sign a real publish

```bash
mkdir keys && cd keys
cosign generate-key-pair
```
```
Private key written to cosign.key
Public key written to cosign.pub
```

```bash
brickkit publish --path ./components/demo/hello --source-type git \
  --git-url https://example.com/demo/hello.git --no-pin-digest \
  --sign --key keys/cosign.key \
  --public-key-ref demo-hello-release --signed-by release-bot@example.com
```

```
📤 Publishing demo/hello@1.0.0
   ✅ Manifest validation passed
   ✅ Image reference is valid: brickkit-demo/hello:1.0.0
   ✅ Signed (cosign, public key ref: demo-hello-release)
   💡 Users need to declare it under installer.publicKeys in brickkit.yaml:
        demo-hello-release: <public-key-file-path>
   ✅ artifacts uploaded (1 file)
   ✅ Upload succeeded
🎉 Published
```

`--public-key-ref` is just a name — `demo-hello-release` here — not the key material itself. The market stores that name alongside the signature; it never stores or forwards the public key, on purpose ([Signing and the trust model](../06-architecture/06-signing-and-trust.md) covers exactly why the key can't come from the market itself).

## Trust the key, on the consumer's own terms

Hand the `.pub` file to the consuming project out of band (however your team actually shares keys — that part is deliberately outside the platform's scope) and reference it by the same name the publisher used:

```yaml
installer:
  requireSignature: true
  publicKeys:
    demo-hello-release: keys/demo-hello-release.pub
```

```bash
brickkit add demo/hello@1.0.0
```

```
📦 Adding demo/hello@1.0.0
   ├── Manifest ✅
   └── artifacts ✅ (1 file)
🔏 Signature: ✅ verified demo/hello@1.0.0(publisher release-bot@example.com)
✅ Written to brickkit.yaml (1 component)
```

No cosign anywhere on this machine — this verification ran entirely against the Go standard library's ECDSA implementation, checking real bytes against a real public key. `发布者 release-bot@example.com` is the exact `--signed-by` string from the publish step, carried all the way through.

## A verification failure that's genuinely a failure

Configure `publicKeys` to point at a *different* keypair's public key instead — the kind of mistake that happens when a key rotates and a stale reference gets left behind, or when a name collides between two unrelated publishers:

```bash
brickkit add demo/hello@1.0.0
```

```
❌ Error: signature verification failed
   Component: demo/hello@1.0.0
   Public key: demo-hello-release
   Reason: The content doesn't match the signature — it may have been tampered with, or the signature wasn't made with this public key
   Suggestions:
   1. Contact the component publisher to re-sign it
   2. Confirm the ref under installer.publicKeys really points to the publisher's public key
```

The message doesn't — and cryptographically can't — distinguish "this was genuinely tampered with" from "you configured the wrong key": both produce a signature that fails to verify against the public key on file, and there's no way to tell them apart from the outside. Either way, `add` refuses outright. There's no `--skip-verification` half-measure here, and no fallback to "warn and continue" the way a missing optional dependency does — a signature that fails to verify is exactly the one case this series treats as non-negotiable.

---

Next: [Build your first component from scratch](11-build-your-own.md) — building a component from nothing, start to finish, the perspective every other article in this series has been the consuming side of.
