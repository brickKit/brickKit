// 本文件是 Step 16-D-2「面向真实集群：集群约束」的业务行为测试。
//
// 这一类问题在 minikube 上都试不出来：默认不开 Pod Security Admission、
// 镜像是本地 load 进去的、只有一个 ingress controller 且是默认 class。
// 真集群上它们要么**直接拒收**（PSA），要么**安静地不生效**（ingress class）。
package k8s_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
)

// ============================================================
// podSecurity：Pod Security Admission
// ============================================================

// 不写就什么都不生成——加 securityContext 可能让本来跑得好好的组件起不来
// （镜像以 root 运行、要绑 1024 以下的端口……），不能默默替使用者决定。
func TestNoSecurityContextByDefault(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	assert.NotContains(t, b.container("people-basic-1-0-0"), "securityContext")
}

// podSecurity: restricted —— 按 K8s 官方 Pod Security Standards 的 restricted 级别生成。
//
// 真集群的命名空间常常打着 `pod-security.kubernetes.io/enforce: restricted`，
// 少任何一项，Pod 会被 API Server **直接拒收**，`kubectl apply` 那一刻就失败。
func TestRestrictedPodSecurity(t *testing.T) {
	b := newBuilder(t)
	b.cfg.Deploy.PodSecurity = config.PodSecurityRestricted
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	c := b.container("people-basic-1-0-0")
	sc := c["securityContext"]

	assert.Equal(t, false, dig(t, sc, "allowPrivilegeEscalation"))
	assert.Equal(t, true, dig(t, sc, "runAsNonRoot"))
	assert.Equal(t, []any{"ALL"}, dig(t, sc, "capabilities", "drop"))
	assert.Equal(t, "RuntimeDefault", dig(t, sc, "seccompProfile", "type"))
}

// readOnlyRootFilesystem 不生成：restricted 级别并不要求它，
// 而它会让任何往 /tmp 写东西的组件直接挂掉。
func TestRestrictedDoesNotForceReadOnlyRoot(t *testing.T) {
	b := newBuilder(t)
	b.cfg.Deploy.PodSecurity = config.PodSecurityRestricted
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	assert.NotContains(t, b.container("people-basic-1-0-0")["securityContext"],
		"readOnlyRootFilesystem")
}

// 迁移 Job 的容器同样要满足——它跑在同一个命名空间里。
func TestRestrictedAppliesToMigrationJob(t *testing.T) {
	b := newBuilder(t)
	b.cfg.Deploy.PodSecurity = config.PodSecurityRestricted
	b.component(migrating(withDatabase(simple("people/basic", "1.0.0", 8080))), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	doc := b.doc("migrations/people-basic-1-0-0-migration.yaml")
	containers, _ := dig(t, doc, "spec", "template", "spec", "containers").([]any)
	job, _ := containers[0].(map[string]any)

	assert.Equal(t, true, dig(t, job["securityContext"], "runAsNonRoot"))
}

// ============================================================
// imagePullSecrets：私有 registry
// ============================================================

func TestImagePullSecrets(t *testing.T) {
	b := newBuilder(t)
	b.cfg.Deploy.ImagePullSecrets = []string{"regcred", "backup-regcred"}
	b.component(migrating(withDatabase(simple("people/basic", "1.0.0", 8080))), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	want := []any{
		map[string]any{"name": "regcred"},
		map[string]any{"name": "backup-regcred"},
	}

	assert.Equal(t, want,
		dig(t, b.doc("deployments/people-basic-1-0-0.yaml"), "spec", "template", "spec", "imagePullSecrets"))
	assert.Equal(t, want,
		dig(t, b.doc("migrations/people-basic-1-0-0-migration.yaml"), "spec", "template", "spec", "imagePullSecrets"),
		"迁移容器用的是同一个私有镜像，同样要拉得下来")
}

func TestNoImagePullSecretsByDefault(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	spec := dig(t, b.doc("deployments/people-basic-1-0-0.yaml"), "spec", "template", "spec")

	assert.NotContains(t, spec, "imagePullSecrets")
}

// ============================================================
// Ingress：class / 注解 / TLS
// ============================================================

// 不写 ingressClassName 时，只有集群配了「默认 class」才会有人认领这条 Ingress。
// 没有默认 class 的集群上，`kubectl apply` 成功，域名却打不开——最难查的那种。
func TestIngressClassName(t *testing.T) {
	b := newBuilder(t)
	b.cfg.Deploy.IngressClass = "nginx"
	b.component(simple("portal/user-frontend", "1.0.0", 80),
		config.Component{Expose: true, Hostname: "portal.example.com"})

	assert.Equal(t, "nginx",
		dig(t, b.doc("ingress/portal-user-frontend-1-0-0.yaml"), "spec", "ingressClassName"))
}

// 注解是留给集群侧能力的口子：cert-manager 签证书、nginx 调 body size……
// 平台不认识它们，原样透传即可。
func TestIngressAnnotations(t *testing.T) {
	b := newBuilder(t)
	b.cfg.Deploy.IngressAnnotations = map[string]string{
		"cert-manager.io/cluster-issuer":              "letsencrypt-prod",
		"nginx.ingress.kubernetes.io/proxy-body-size": "50m",
	}
	b.component(simple("portal/user-frontend", "1.0.0", 80),
		config.Component{Expose: true, Hostname: "portal.example.com"})

	annotations := dig(t, b.doc("ingress/portal-user-frontend-1-0-0.yaml"), "metadata", "annotations")

	assert.Equal(t, "letsencrypt-prod", dig(t, annotations, "cert-manager.io/cluster-issuer"))
	assert.Equal(t, "50m", dig(t, annotations, "nginx.ingress.kubernetes.io/proxy-body-size"))
	assert.Equal(t, "portal/user-frontend", dig(t, annotations, "brickkit.io/component-id"),
		"平台自己的注解不能被挤掉")
}

func TestIngressTLS(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("portal/user-frontend", "1.0.0", 80),
		config.Component{Expose: true, Hostname: "portal.example.com", TLSSecret: "portal-tls"})

	tls := dig(t, b.doc("ingress/portal-user-frontend-1-0-0.yaml"), "spec", "tls")

	assert.Equal(t, []any{map[string]any{
		"hosts":      []any{"portal.example.com"},
		"secretName": "portal-tls",
	}}, tls)
}

func TestNoTLSByDefault(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("portal/user-frontend", "1.0.0", 80),
		config.Component{Expose: true, Hostname: "portal.example.com"})

	assert.NotContains(t, dig(t, b.doc("ingress/portal-user-frontend-1-0-0.yaml"), "spec"), "tls")
}

// 这些字段都只对 Ingress 有意义，不该漏进别的清单。
func TestClusterFieldsDoNotLeakIntoOtherManifests(t *testing.T) {
	b := newBuilder(t)
	b.cfg.Deploy.IngressClass = "nginx"
	b.cfg.Deploy.IngressAnnotations = map[string]string{"cert-manager.io/cluster-issuer": "x"}
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	for _, name := range []string{"deployments/people-basic-1-0-0.yaml", "services/people-basic-1-0-0.yaml"} {
		text := string(b.file(name).YAML)
		assert.NotContains(t, text, "ingressClassName", name)
		assert.NotContains(t, text, "cert-manager", name)
	}
}

// 端口小于 1024 时提醒：restricted 下 drop 掉了 NET_BIND_SERVICE，绑不了特权端口。
func TestRestrictedWarnsAboutPrivilegedPort(t *testing.T) {
	b := newBuilder(t)
	b.cfg.Deploy.PodSecurity = config.PodSecurityRestricted
	b.component(simple("portal/user-frontend", "1.0.0", 80),
		config.Component{Expose: true, Hostname: "portal.example.com"})

	result := b.generate()

	require.NotEmpty(t, result.Warnings, "80 端口 + restricted 必然起不来，要提前说")
	text := result.Warnings[0].Error()
	assert.Contains(t, text, "portal/user-frontend")
	assert.Contains(t, text, "80")
}

// extraPorts 也要一起看。
func TestRestrictedChecksExtraPorts(t *testing.T) {
	m := simple("infra/dns", "1.0.0", 8080)
	m.Deployment.ExtraPorts = []manifest.ExtraPort{{Name: "dns", Port: 53}}

	b := newBuilder(t)
	b.cfg.Deploy.PodSecurity = config.PodSecurityRestricted
	b.component(m, config.Component{})

	require.NotEmpty(t, b.generate().Warnings)
	assert.Contains(t, b.generate().Warnings[0].Error(), "53")
}
