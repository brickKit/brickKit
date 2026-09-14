# 11. Network Policy and Least Privilege

`deploy.networkPolicy.enabled: true` turns on Kubernetes `NetworkPolicy` generation, computed directly from the dependency graph (AGENTS.md §7) — no separate access-control list to maintain by hand. This article turns it on for `demo/hello` + `demo/caller`, and proves it actually blocks something, not just that it generates plausible-looking YAML.

**Prerequisite, and this matters more than it sounds like it should:** a cluster whose CNI actually enforces `NetworkPolicy`. Plenty of clusters accept these objects without enforcing them at all — `kubectl apply` succeeds, `kubectl get networkpolicy` lists them, and every connection still gets through regardless, silently. `brickkit up` itself warns about this:

```
🔒 已生成 2 份 NetworkPolicy（deploy.networkPolicy.enabled: true）
   ⚠️ 它们只在集群的 CNI 支持执行时才有效。不支持时：apply 会成功、
      kubectl get networkpolicy 看得见、而流量完全不受限制——没有任何报错。
      minikube / kind 的**默认** CNI 就属于这一类。
   平台测不出来（K8s 没有这个 API），只能你自己验一次
```

Minikube's own default CNI is explicitly named as one of the non-enforcing ones — this article's minikube was started with `minikube start --cni=calico` specifically so the demonstration below is real, not theater.

## Turn it on

```yaml
deploy:
  target: k8s
  networkPolicy:
    enabled: true
```

Nothing else — no `allowFrom` entries naming `demo/caller` by hand. The generated policy for `demo/hello`:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
spec:
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: demo-caller-1-0-0
      ports:
        - port: 8080
          protocol: TCP
  podSelector:
    matchLabels:
      app: demo-hello-1-0-0
  policyTypes:
    - Ingress
```

`demo/caller` is `demo/hello`'s only dependent in this project, so it's the only pod selector in the generated ingress rule, on exactly the port `demo/hello` actually listens on — the platform already knows this from the same dependency graph [Dependency resolution and start order](../architecture/dependency-resolution.md) walks through in depth. `allowFrom` (AGENTS.md §7) exists for the case this doesn't cover: something outside this project's own dependency graph — an ingress controller, another team's namespace — that legitimately needs access despite not appearing as a declared dependency anywhere.

## Prove the authorized path still works

```bash
kubectl -n brickkit-hello-world exec deploy/demo-caller-1-0-0 -- \
  wget -qO- --timeout=3 http://demo-hello-1-0-0:8080/api/v1/hello
```
```json
{"component":"demo/hello","greeting":"你好","message":"你好，我是 demo/hello@1.0.0","version":"1.0.0"}
```

Instant, unaffected — the policy that just got generated is exactly permissive enough for the one real caller in this project.

## Prove an unauthorized path actually gets blocked

A plain pod, in the same namespace, matching nothing `demo/hello`'s policy allows:

```bash
kubectl -n brickkit-hello-world run intruder --image=busybox:1.36 --restart=Never --command -- sleep 3600
kubectl -n brickkit-hello-world exec intruder -- \
  wget -qO- --timeout=5 http://demo-hello-1-0-0:8080/api/v1/hello
```
```
wget: download timed out
command terminated with exit code 1
```

Not a connection refused, not an HTTP error — a timeout. The packet never gets a response at all; Calico is dropping it before it ever reaches `demo/hello`'s container. Being in the same namespace, the same cluster, even able to resolve the same DNS name (`demo-hello-1-0-0` still resolves — `NetworkPolicy` restricts traffic, not name resolution) buys this pod nothing. Being in the dependency graph is what the policy actually keys off.

---

Next: [Multi-project sharing](12-multi-project-sharing.md), the last article in this series — sharing components across projects deliberately, using `brickkit fetch` from Article 7 for real, across two genuinely separate projects.
