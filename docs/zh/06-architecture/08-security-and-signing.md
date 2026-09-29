# 安全与签名

## 信任模型：安装即信任

BrickKit 不对第三方组件做事前审查——不扫描、不沙箱、不做静态分析。你安装一个组件，就是信任它，和 npm、VS Code 插件市场、GitHub 是同一套模型。
事前审查要么误报太多（拦下正常组件），要么漏报太多（放过精心伪装的恶意代码）；平台能可靠做到的，是确认"组件出自谁、有没有被改过"。

| 组件来自 | 可信程度取决于 |
| --- | --- |
| 本地安装源 | 你自己 |
| Git 安装源 | 那个仓库、那个托管平台；你在 `brickkit.yaml` 的安装源里写明了它 |
| 组件市场 | 发布者的签名（下文） |

市场上的组件被确认有问题时，市场把它标成 `blocked`，之后就不能再安装。

## 签名：发布时签，安装时验

**发布方**用 cosign 签名：

```bash
brickkit publish --path ./components/people/basic --sign --key cosign.key --signed-by release-bot@example.com
```

被签名的不是 `component.yaml` 的原始字节，而是一份**规范化载荷**：Manifest 解析之后按固定规则重新编码。所以改注释、缩进、键的顺序都不会让签名失效；
而带重复键的 Manifest 会被直接拒绝——同一份文件被不同的解析器读出两种意思，签名就没有意义了。

`publish` 默认先把镜像的 tag 解析成不可变的 digest，再签名：签名覆盖的是一个事后不能被悄悄改指的引用。加 `--no-pin-digest` 可以跳过，
但那样镜像仓库上同名 tag 被换掉时，签名照样有效。

**安装方**只用 Go 标准库验签，不需要装 cosign。

## 公钥放在你自己的项目里

```yaml
# brickkit.yaml
installer:
  requireSignature: true
  publicKeys:
    keys/vendor.pub: keys/vendor.pub
```

`publicKeys` 是你信任的发布者公钥：名字（签名里写的 `publicKeyRef`）→ 项目里的公钥文件。

**公钥不从市场取。** 否则市场一旦被攻破，攻击者可以把组件和公钥一起换掉，验证照样通过——等于市场给自己发证书。信任的根在你自己的仓库里，经过你自己的代码评审。

几种情况：

| 情况 | 结果 |
| --- | --- |
| 组件有签名，签名者的公钥在 `publicKeys` 里，验证通过 | 安装 |
| `requireSignature: true`，组件没签名 | 阻断安装 |
| 签名来自一个没在 `publicKeys` 里声明的发布者 | 警告：未做校验 |
| 一把公钥都没配 | 签名验证**完全关闭**，`requireSignature: true` 也不起作用；CLI 会警告 |

最后一行值得记住：只写 `requireSignature: true` 而不配公钥，什么都没有保护。

## 最小权限

**默认不可达。** 组件之间在同一个网络里可以按服务名互相调用；从外面访问，必须在部署文件里给它写 `expose`（Docker 映射宿主机端口，Kubernetes 生成 Ingress）。

**网络策略从依赖图生成。** Kubernetes 上，部署文件写：

```yaml
k8s:
  networkPolicy:
    enabled: true
```

平台为每个组件生成一条 NetworkPolicy：只放行它在依赖图里真正的调用方，端口只放行它自己监听的那些。依赖图之外的访问者（Ingress 控制器、
另一个团队的命名空间）用 `allowFrom` 手写放行；出站方向也可以打开（`egress`）。你不需要另外维护一份访问控制列表——它永远和依赖声明一致。

**ServiceAccount。** `k8s.serviceAccount.enabled: true` 给每个组件一个单独的 ServiceAccount，而且不挂载访问 Kubernetes API 的令牌。

**Pod 安全。** `k8s.podSecurity: restricted` 按 Pod Security 的 restricted 级别给每个容器生成 `securityContext`：不以 root 运行、禁止提权、
丢弃全部 capabilities、`seccompProfile: RuntimeDefault`。刻意不加只读根文件系统——restricted 并不要求它，而它会让任何往 `/tmp` 写东西的组件直接挂掉。
丢掉全部 capabilities 也意味着绑不了 1024 以下的端口：组件监听这样的端口时，`up` 会警告。

这三项都**不默认开启**：每一项都可能让一个本来能跑的组件起不来（比如组件需要以 root 运行、需要调用 Kubernetes API），这个代价只有你能权衡。

CLI 本身从不挂载 Docker socket，只调用 `docker compose` / `kubectl` 命令行。

## 网络策略可能根本没有生效

这是一个你必须知道的限制：**很多集群接受 NetworkPolicy 对象，却完全不执行它。** `kubectl apply` 成功、`kubectl get networkpolicy` 看得见，
流量却照样畅通无阻，而且没有任何报错。执行与否取决于集群的网络插件（CNI）；minikube 和 kind 的默认 CNI 就不执行。

Kubernetes 没有提供"这个集群执行 NetworkPolicy 吗"的 API，平台测不出来，所以 `up` 每次生成网络策略时都会提醒你：

```text
🔒 已生成 5 份 NetworkPolicy（k8s.networkPolicy.enabled: true）
   ⚠️ 它们只在集群的 CNI 支持执行时才有效。不支持时：apply 会成功、
      kubectl get networkpolicy 看得见、而流量完全不受限制——没有任何报错。
      minikube / kind 的**默认** CNI 就属于这一类。
   平台测不出来（K8s 没有这个 API），只能你自己验一次：
      详见 docs/zh/06-architecture/08-security-and-signing.md（英文版把 zh 换 en）
```

**自己验一次：** 部署之后，分别从一个**被授权**的组件和一个**没被授权**的组件去访问同一个目标。

```bash
# 被授权：demo/caller 依赖 demo/hello，应当能连上
kubectl exec -n brickkit-my-shop deploy/demo-caller-1-0-0 -- \
  wget -qO- -T 3 http://demo-hello-1-0-0:8080/healthz

# 没被授权：demo/bus 不依赖 demo/hello，应当超时
kubectl exec -n brickkit-my-shop deploy/demo-bus-1-0-0 -- \
  wget -qO- -T 3 http://demo-hello-1-0-0:8080/healthz
```

第一条成功、第二条超时，网络策略在生效。两条都成功，说明你的集群不执行它——需要换一个执行 NetworkPolicy 的 CNI（例如 Calico、Cilium），
或者接受"这些策略只是声明"。只看生成的 YAML 说得通，证明不了任何事。

## 密钥

密钥不进 `config/`（它要提交进 Git）：用 `${VAR}` 引用环境变量或 `.env`、用 `file://` 引用 `.secrets/` 下的文件，或者在 Kubernetes 上用
`{ existingSecret, key }` 引用集群里已有的 Secret。部署时密钥走单独的通道，从不明文写进部署文件。详见 [敏感值](../01-three-layers/07-sensitive-values.md)。
