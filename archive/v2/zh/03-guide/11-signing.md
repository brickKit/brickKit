# 11. 给组件签名与验签

第 10 篇结尾撞见了一条真实的警告：一个从市场安装、却没配 `installer.publicKeys` 的项目，`requireSignature: true` 是白拿的，什么都不会做。这一篇把这个缺口真正补上——生成一把真实的密钥对，发布一个签过名的组件，配一个真正信任这把密钥的项目，然后看验签一次成功、一次真正失败。

**前置条件，只有发布方需要：** cosign（`go install github.com/sigstore/cosign/v2/cmd/cosign@latest`）——签名是这整个系列里唯一需要它的操作；这个系列里其它每一篇的每一次 `brickkit add`，验签靠的都只是 Go 标准库（AGENTS.zh.md §5.9），这里也不例外。

## 生成一把真实的密钥对，签一次真实的发布

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

`--public-key-ref` 只是一个名字——这里是 `demo-hello-release`——不是密钥材料本身。市场把这个名字跟签名一起存起来；它从来不存、也不转发公钥本身，这是故意的（[签名与信任模型](../06-architecture/06-signing-and-trust.md)详细讲了公钥为什么不能来自市场）。

## 按消费方自己的意愿去信任这把密钥

把 `.pub` 文件通过带外方式（不管你们团队实际怎么共享密钥——这部分故意留在平台范围之外）交给消费方项目，用发布方用过的同一个名字去引用它：

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

这台机器上哪里都没装 cosign——这次验签完全靠 Go 标准库自己的 ECDSA 实现，拿真实的字节对着真实的公钥核对。`发布者 release-bot@example.com` 就是发布那一步 `--signed-by` 的原始字符串，一路带过来的。

## 一次真正失败的验签

把 `publicKeys` 改成指向**另一把**密钥对的公钥——这正是密钥轮换时留下一个过期引用、或者两个不相关的发布者撞上同一个名字时会真实发生的那种失误：

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

这条消息没有——在密码学上也没办法——区分"这确实被篡改过"和"你配错了公钥"：两种情况产生的都是一个对着现有公钥验不过的签名，从外面完全看不出区别。不管是哪一种，`add` 都直接拒绝。这里没有"跳过校验"这种折中方案，也不像一个缺失的弱依赖那样有"警告后继续"的退路——一个验不过的签名，正是这个系列里唯一一件被当成没有商量余地的事。

---

下一篇：[从零开发自己的第一个组件](12-build-your-own.md)——从零开始造一个组件，完整走一遍，这个系列前面每一篇都站在消费方这一侧，这一篇换个角度。
