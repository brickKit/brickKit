# Security and signing

## The trust model: installing means trusting

BrickKit does no upfront review of third-party components — no scanning, no sandboxing, no static analysis. Installing a
component means trusting it, the same model as npm, the VS Code extension marketplace and GitHub. An upfront review either
has too many false positives (stopping legitimate components) or too many false negatives (letting carefully disguised
malicious code through); what the platform can do reliably is confirm "who a component comes from, and whether it was
altered".

| A component from | Is as trustworthy as |
| --- | --- |
| A local install source | You |
| A Git install source | That repository and that hosting platform, which you named in `brickkit.yaml`'s install sources |
| A component market | The publisher's signature (below) |

When a market component is confirmed to be a problem, the market marks it `blocked`, and it can't be installed from then
on.

## Signatures: signed on publishing, verified on installing

**The publisher** signs with cosign:

```bash
brickkit publish --path ./components/people/basic --sign --key cosign.key --signed-by release-bot@example.com
```

What's signed isn't the raw bytes of `component.yaml` but a **canonical payload**: the Manifest parsed and re-encoded by
fixed rules. So changing comments, indentation or key order doesn't break the signature; and a Manifest with duplicate keys
is refused outright — when the same file reads two ways to two different parsers, a signature means nothing.

By default `publish` first resolves the image's tag to an immutable digest, then signs: the signature covers a reference
that can't be quietly re-pointed afterwards. `--no-pin-digest` skips it, but then the signature stays valid when the
same-named tag is replaced in the image registry.

**The installer** verifies with the Go standard library alone, with no need to install cosign.

## Public keys live in your own project

```yaml
# brickkit.yaml
installer:
  requireSignature: true
  publicKeys:
    keys/vendor.pub: keys/vendor.pub
```

`publicKeys` are the publishers' public keys you trust: a name (the `publicKeyRef` written in the signature) → a public
key file in the project.

**Public keys aren't taken from the market.** Otherwise, once the market is broken into, an attacker can swap both the
component and the public key, and verification still passes — the market issuing certificates to itself. The root of
trust is in your own repository, through your own code review.

The cases:

| Situation | Result |
| --- | --- |
| The component is signed, the signer's key is in `publicKeys`, verification passes | Installed |
| `requireSignature: true`, and the component isn't signed | Installation blocked |
| The signature comes from a publisher not declared in `publicKeys` | A warning: not verified |
| No public key configured at all | Signature verification is **switched off entirely**, and `requireSignature: true` does nothing either; the CLI warns |

The last row is worth remembering: writing only `requireSignature: true` without configuring keys protects nothing.

## Least privilege

**Unreachable by default.** Components on the same network can call each other by service name; access from outside needs
`expose` written for it in the deploy file (Docker maps a host port, Kubernetes generates an Ingress).

**Network policies generated from the dependency graph.** On Kubernetes, write in the deploy file:

```yaml
k8s:
  networkPolicy:
    enabled: true
```

The platform generates one NetworkPolicy per component: it lets through only the component's real callers in the
dependency graph, and only on the ports it listens on itself. Callers outside the dependency graph (the Ingress
controller, another team's namespace) are let through by hand with `allowFrom`; the outbound direction can be covered too
(`egress`). You don't maintain a separate access-control list — it always agrees with the declared dependencies.

**ServiceAccounts.** `k8s.serviceAccount.enabled: true` gives every component a ServiceAccount of its own, without mounting
the token for the Kubernetes API.

**Pod security.** `k8s.podSecurity: restricted` generates a `securityContext` for every container following Pod Security's
restricted level: not running as root, no privilege escalation, every capability dropped, `seccompProfile:
RuntimeDefault`. A read-only root filesystem is deliberately not added — restricted doesn't require it, and it would kill
any component writing to `/tmp`. Dropping every capability also means ports below 1024 can't be bound: when a component
listens on such a port, `up` warns.

None of these three is **on by default**: each can stop a component that ran fine from starting (a component that needs to
run as root, or to call the Kubernetes API), a cost only you can weigh.

The CLI itself never mounts the Docker socket; it only calls the `docker compose` / `kubectl` command line.

## Network policies may not take effect at all

This is a limit you must know: **many clusters accept NetworkPolicy objects and don't enforce them at all.** `kubectl
apply` succeeds, `kubectl get networkpolicy` shows them, and traffic flows exactly as before, without any error. Whether
they're enforced depends on the cluster's network plugin (CNI); the default CNI of minikube and kind doesn't.

Kubernetes offers no API to ask "does this cluster enforce NetworkPolicy?", so the platform can't tell, and every time `up`
generates network policies it reminds you:

```text
🔒 Generated 3 NetworkPolicy manifests (k8s.networkPolicy.enabled: true)
   ⚠️ They only take effect when the cluster's CNI enforces them. When it doesn't: apply succeeds,
      kubectl get networkpolicy shows them, yet traffic is not restricted at all — with no error whatsoever.
      The default CNI of minikube / kind is exactly this kind.
   The platform can't detect this (K8s has no API for it), so you have to verify it yourself once:
      See docs/en/06-architecture/08-security-and-signing.md (swap en for zh for the Chinese version)
```

**Verify it yourself once:** after deploying, reach the same target from an **authorised** component and from an
**unauthorised** one.

```bash
# Authorised: demo/caller depends on demo/hello, so this should connect
kubectl exec -n brickkit-my-shop deploy/demo-caller-1-0-0 -- \
  wget -qO- -T 3 http://demo-hello-1-0-0:8080/healthz

# Unauthorised: demo/bus doesn't depend on demo/hello, so this should time out
kubectl exec -n brickkit-my-shop deploy/demo-bus-1-0-0 -- \
  wget -qO- -T 3 http://demo-hello-1-0-0:8080/healthz
```

The first succeeding and the second timing out means the network policies are in effect. Both succeeding means your
cluster doesn't enforce them — you need a CNI that does (Calico or Cilium, for instance), or to accept that "these
policies are declarations only". A generated YAML that looks right proves nothing.

## Secrets

Secrets don't go into `config/` (it's committed to Git): reference an environment variable or `.env` with `${VAR}`, a file
under `.secrets/` with `file://`, or, on Kubernetes, a Secret already in the cluster with `{ existingSecret, key }`. At
deployment, secrets take a separate channel and are never written into deployment files in plain text. More in
[Sensitive values](../01-three-layers/07-sensitive-values.md).
