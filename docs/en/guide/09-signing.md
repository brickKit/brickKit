# 9. Sign and Verify Components

Article 8 ended with a real, live warning: a project that installs from a marketplace with no `installer.publicKeys` configured gets `requireSignature: true` for free, and it does nothing. This article closes that gap for real — generating an actual keypair, publishing a signed component, configuring a project to actually trust that key, and watching verification both succeed and genuinely fail.

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
📤 发布 demo/hello@1.0.0
   ✅ Manifest 校验通过
   ✅ 镜像引用有效：brickkit-demo/hello:1.0.0
   ✅ 已签名（cosign，公钥 ref：demo-hello-release）
   💡 使用者需要在 brickkit.yaml 的 installer.publicKeys 下声明：
        demo-hello-release: <公钥文件路径>
   ✅ artifacts 上传成功（1 个文件）
   ✅ 上传成功
🎉 发布完成
```

`--public-key-ref` is just a name — `demo-hello-release` here — not the key material itself. The market stores that name alongside the signature; it never stores or forwards the public key, on purpose ([Signing and the trust model](../architecture/signing-and-trust.md) covers exactly why the key can't come from the market itself).

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
📦 添加 demo/hello@1.0.0
   ├── Manifest ✅
   └── artifacts ✅（1 个文件）
🔏 签名：✅ 已校验 demo/hello@1.0.0（发布者 release-bot@example.com）
✅ 已写入 brickkit.yaml（1 个组件）
```

No cosign anywhere on this machine — this verification ran entirely against the Go standard library's ECDSA implementation, checking real bytes against a real public key. `发布者 release-bot@example.com` is the exact `--signed-by` string from the publish step, carried all the way through.

## A verification failure that's genuinely a failure

Configure `publicKeys` to point at a *different* keypair's public key instead — the kind of mistake that happens when a key rotates and a stale reference gets left behind, or when a name collides between two unrelated publishers:

```bash
brickkit add demo/hello@1.0.0
```

```
❌ 错误：签名校验不通过
   组件：demo/hello@1.0.0
   公钥：demo-hello-release
   原因：内容与签名不匹配，可能已被篡改，或签名不是该公钥所签
   建议：
   1. 联系组件发布者重新签名
   2. 确认 installer.publicKeys 中该 ref 指向的确实是发布者的公钥
```

The message doesn't — and cryptographically can't — distinguish "this was genuinely tampered with" from "you configured the wrong key": both produce a signature that fails to verify against the public key on file, and there's no way to tell them apart from the outside. Either way, `add` refuses outright. There's no `--skip-verification` half-measure here, and no fallback to "warn and continue" the way a missing optional dependency does — a signature that fails to verify is exactly the one case this series treats as non-negotiable.

---

Next in this series (see [the guide index](README.md)): building a component from nothing, start to finish — the perspective every other article in this series has been the consuming side of.
