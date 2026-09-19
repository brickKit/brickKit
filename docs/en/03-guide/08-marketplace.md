# 8. Publish and Install from a Marketplace

Every earlier article installed `demo/hello` from a `local` source — a directory on disk. This one runs a real marketplace (the same one [Self-hosting the BrickKit Market](../07-patterns/09-deployment/self-hosted-market.md) covers deploying), publishes `demo/hello` to it, and installs it into a completely separate project that never touches the component's source directory at all.

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
🔐 登录 BrickKit Market
✅ 登录成功
   Token 已存储到 .brickkit/credentials
   有效期至：2026-10-14T15:47:20Z
```

## Publish it

```bash
brickkit publish --path ./components/demo/hello --source-type git \
  --git-url https://example.com/demo/hello.git
```

The first attempt, with no flags about the image, refuses outright:

```
❌ 错误：无法确定镜像的 digest，发布已中止
   镜像：brickkit-demo/hello:1.0.0
   为什么要拦住：发布出去的版本号不可回收。取不到 digest
   通常意味着这个镜像消费方也拉不到——与其在市场里留下一个装不上的
   版本，不如现在停下
```

This is [Signing and the trust model](../06-architecture/06-signing-and-trust.md)'s digest-pinning step, refusing to publish a reference nobody else could actually resolve — `demo/hello`'s image only ever existed in this one machine's local Docker cache, never pushed anywhere. `--no-pin-digest` is the honest way past that for a demo like this one (a real publish against a real registry wouldn't need it):

```bash
brickkit publish --path ./components/demo/hello --source-type git \
  --git-url https://example.com/demo/hello.git --no-pin-digest
```
```
📤 发布 demo/hello@1.0.0
   ✅ Manifest 校验通过
   ✅ 镜像引用有效：brickkit-demo/hello:1.0.0
   ⚠️ 跳过 digest 钉住（--no-pin-digest）
   ✅ artifacts 上传成功（1 个文件）
   ✅ 上传成功
🎉 发布完成
```

A closed-source attempt (`--source-type registry`) with this exact same Manifest refuses for a completely different, real reason:

```
❌ 错误：发布组件版本失败
   原因：闭源组件提供 API 时必须上传 API 契约文件
   hint：在 artifacts 中声明至少一个 type: api-contract 的产物
   （代码可以闭源，API 契约不能闭源）
```

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
📦 添加 demo/hello@1.0.0
   ├── Manifest ✅
   └── artifacts ✅（1 个文件）
⚠️ 警告：requireSignature 为 true，但项目没有声明任何可信公钥，签名校验实际未生效
   说明：这不是配置错误，是还没配完——requireSignature 默认为 true，
   而 publicKeys 要等你从发布者那里拿到公钥才填得上
```

The Manifest and artifacts really did come from the market this project has never installed anything from before — and the warning is real, not hypothetical: this project genuinely has no trust anchor configured yet, exactly the gap [Signing and the trust model](../06-architecture/06-signing-and-trust.md) describes. `brickkit up` still works — signature enforcement being unconfigured is a warning, not a block:

```bash
brickkit up
curl http://localhost:8099/api/v1/hello
```
```json
{"component":"demo/hello","greeting":"你好","message":"你好，我是 demo/hello@1.0.0","version":"1.0.0"}
```

## Versions are permanent, and visibility is enforced

Publishing `demo/hello@1.0.0` a second time — even identically — is refused:

```
❌ 错误：demo/hello@1.0.0 已经发布过了
   市场上的状态：stable
   原因：版本号一旦发布就不可回收，软删除的版本同样占位
```

Publish `2.0.0` as `--visibility private` instead, and an unauthenticated project trying to install it gets a real, specific denial — not a generic 404 pretending the version doesn't exist:

```bash
brickkit add demo/hello@2.0.0   # from a project that never logged in
```
```
❌ 错误：无权访问该组件：demo/hello
   建议：
   1. 确认当前账号是否是该组件的所有者
   2. 私有组件需要所有者授权后才能访问
```

## Log out when you're done

Back in the publisher's project (that's where you logged in, and where `.brickkit/credentials` lives). Publishing and installing are done, so clear the login:

```bash
brickkit logout
```
```
✅ 已退出登录
   用户：admin
   已删除：.brickkit/credentials
```

`logout` works in two steps: it tells the marketplace to revoke the token, then deletes the local `.brickkit/credentials`. **The local file is always deleted**, even if the marketplace can't be reached at that moment — otherwise one network blip would leave you believing you'd logged out while the credential still sat on disk. If the marketplace couldn't be told, it says so plainly: that token stays valid on the marketplace side until it expires on its own. Working offline and want to skip the notification outright? Add `--keep-remote`:

```bash
brickkit logout --keep-remote
```
```
✅ 已退出登录
   用户：admin
   已删除：.brickkit/credentials
   ⚠️ 按 --keep-remote 跳过了通知市场
      那个 Token 在市场那边仍然有效，直到 2026-10-14 15:47:20 过期
```

Running it again when you're already logged out does nothing, and isn't a failure:

```
📋 当前没有登录凭据（.brickkit/credentials 不存在）
   用 brickkit login 登录市场
```

---

Next: [Sign and verify components](09-signing.md) — closing the gap the warning above pointed at, actually configuring a trust anchor and verifying a signature for real.
