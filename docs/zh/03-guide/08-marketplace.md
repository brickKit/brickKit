# 8. 从市场发布与安装

前面每一篇装的 `demo/hello` 都来自一个 `local` 安装源——磁盘上的一个目录。这一篇真跑一个市场（跟[自己搭一套 BrickKit Market](../07-patterns/09-deployment/self-hosted-market.md)部署的是同一套东西），把 `demo/hello` 发布上去，再装进一个完全独立、从没碰过这个组件源码目录的项目里。

## 起一个真实的市场

```bash
cd deploy/market
cp .env.example .env && chmod 600 .env
# 编辑 .env：给 POSTGRES_PASSWORD / RUSTFS_SECRET_KEY / ADMIN_PASSWORD 填真实的值
docker compose up -d --build
curl http://localhost:8080/api/v1/health
```
```json
{"success":true,"data":{"status":"ok","time":"...","version":"dev"}}
```

## 非交互式登录

发布方项目把市场配成一个 `type: market` 的安装源，跟任何别的安装源写法一样：

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

## 发布它

```bash
brickkit publish --path ./components/demo/hello --source-type git \
  --git-url https://example.com/demo/hello.git
```

第一次尝试，什么镜像相关的参数都不加，直接被拒绝：

```
❌ 错误：无法确定镜像的 digest，发布已中止
   镜像：brickkit-demo/hello:1.0.0
   为什么要拦住：发布出去的版本号不可回收。取不到 digest
   通常意味着这个镜像消费方也拉不到——与其在市场里留下一个装不上的
   版本，不如现在停下
```

这正是[签名与信任模型](../06-architecture/06-signing-and-trust.md)里"钉住摘要"那一步在发挥作用，拒绝发布一个别人根本解析不了的引用——`demo/hello` 的镜像只存在于这一台机器的本地 Docker 缓存里，从没推到任何地方去过。`--no-pin-digest` 是像这样一个演示场景里绕过它的老实做法（真对着一个真实 registry 发布不需要这个）：

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

用同一份 Manifest 尝试闭源发布（`--source-type registry`），会因为一个完全不同、真实存在的原因被拒绝：

```
❌ 错误：发布组件版本失败
   原因：闭源组件提供 API 时必须上传 API 契约文件
   hint：在 artifacts 中声明至少一个 type: api-contract 的产物
   （代码可以闭源，API 契约不能闭源）
```

`demo/hello` 唯一声明的产物是 `type: api-docs`——给人看的文档，不是机器能直接消费的契约（一份 protobuf 文件，一份给 codegen 用的 OpenAPI 规范）。市场专门对闭源组件强制这条区分：藏起实现可以，藏起调用方要对接的那个形状不行。

## 从一个完全独立的项目里装它

一个新项目，一个市场安装源，本地什么都没有：

```bash
mkdir consumer-project && cd consumer-project
brickkit init consumer-project
# 配上同一个市场安装源
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

这个项目从没在这个市场装过任何东西，而 Manifest 和产物是真的从市场取回来的——这条警告也是真的，不是假设性的：这个项目现在真的没有配任何信任锚点，正是[签名与信任模型](../06-architecture/06-signing-and-trust.md)描述的那个缺口。`brickkit up` 照样能跑——签名校验没配完是警告，不是拦截：

```bash
brickkit up
curl http://localhost:8099/api/v1/hello
```
```json
{"component":"demo/hello","greeting":"你好","message":"你好，我是 demo/hello@1.0.0","version":"1.0.0"}
```

## 版本号是永久的，可见性会被真正强制

再发布一次 `demo/hello@1.0.0`——哪怕内容一模一样——也会被拒绝：

```
❌ 错误：demo/hello@1.0.0 已经发布过了
   市场上的状态：stable
   原因：版本号一旦发布就不可回收，软删除的版本同样占位
```

换成把 `2.0.0` 发成 `--visibility private`，一个没登录过的项目想装它，会拿到一条真实、具体的拒绝，不是一个假装这个版本不存在的通用 404：

```bash
brickkit add demo/hello@2.0.0   # 来自一个从没登录过的项目
```
```
❌ 错误：无权访问该组件：demo/hello
   建议：
   1. 确认当前账号是否是该组件的所有者
   2. 私有组件需要所有者授权后才能访问
```

---

下一篇：[给组件签名与验签](09-signing.md)——把上面那条警告指出的缺口补上，真正配一个信任锚点，真正验一次签名。
