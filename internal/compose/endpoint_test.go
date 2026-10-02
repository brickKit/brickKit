package compose_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project/projecttest"
)

// 本文件测配置里的 $endpoint:：项目按组件 ID 写地址，平台算出与 *_ENDPOINT 同一条规则的值。
// 场景来自一个真实项目的槽位：authz、iam 两个槽位的地址集中写在 vars.yaml，十几个组件用 $var: 引用，
// 两个成员还互相引用、各自引用自己。

// withKeys 给组件声明几个可选的字符串配置项（required 里的另外标出）。
func withKeys(m *manifest.Manifest, required []string, keys ...string) *manifest.Manifest {
	props := map[string]manifest.ConfigProperty{}
	for _, k := range keys {
		props[k] = manifest.ConfigProperty{Type: "string"}
	}
	m.ConfigSchema = &manifest.ConfigSchema{Type: "object", Properties: props, Required: required}
	return m
}

// slotProject：authz 与 iam 互相引用、引用自己，sales 两个都引用；地址只写在 vars.yaml。
func slotProject(t *testing.T) *builder {
	t.Helper()
	b := newBuilder(t)
	b.spec.Vars = map[string]any{
		"AUTHZ_URL":    "$endpoint:infra/authz",
		"IAM_URL":      "$endpoint:infra/iam",
		"IAM_JWKS_URL": "$endpoint:infra/iam/.well-known/jwks.json",
	}
	refs := map[string]any{"AUTHZ_URL": "$var:AUTHZ_URL", "IAM_URL": "$var:IAM_URL", "IAM_JWKS_URL": "$var:IAM_JWKS_URL"}
	keys := []string{"AUTHZ_URL", "IAM_URL", "IAM_JWKS_URL"}
	b.component(withKeys(simple("infra/authz", "2.0.1", 8223), nil, keys...), projecttest.Entry{Config: refs})
	b.component(withKeys(simple("infra/iam", "1.0.0", 8000), nil, keys...), projecttest.Entry{Config: refs})
	b.component(withKeys(simple("erp/sales", "1.0.0", 8081), nil, keys...), projecttest.Entry{Config: refs})
	return b
}

// 地址是带版本号的服务名加端口，路径接在后面；写在 vars.yaml 的一行，每个引用它的组件拿到的都一样。
func TestEndpointRefResolvesToTheVersionedAddress(t *testing.T) {
	doc := slotProject(t).parsed()
	for _, service := range []string{"erp-sales-1-0-0", "infra-authz-2-0-1", "infra-iam-1-0-0"} {
		env := envOf(t, serviceOf(t, doc, service))
		assert.Equal(t, "http://infra-authz-2-0-1:8223", env["AUTHZ_URL"], service)
		assert.Equal(t, "http://infra-iam-1-0-0:8000", env["IAM_URL"], service)
		assert.Equal(t, "http://infra-iam-1-0-0:8000/.well-known/jwks.json", env["IAM_JWKS_URL"], service)
		assert.NotContains(t, env, "INFRA_AUTHZ_ENDPOINT", "引用不是依赖：不注入 *_ENDPOINT")
	}
}

// authz ↔ iam 互相引用、各自引用自己：不报环，也不生成互等的 depends_on——双方都按"对方暂时不在就重试"写。
func TestEndpointRefsMayFormCyclesAndAddNoStartOrder(t *testing.T) {
	doc := slotProject(t).parsed()
	for _, service := range []string{"erp-sales-1-0-0", "infra-authz-2-0-1", "infra-iam-1-0-0"} {
		assert.NotContains(t, serviceOf(t, doc, service), "depends_on", service)
	}
}

// 被引用的组件跟着引用它的跑；引用它的都关掉了，它也不跑（与弱依赖同一条"跟着上层走"）。
func TestReferencedComponentFollowsItsReferrers(t *testing.T) {
	b := newBuilder(t)
	b.spec.Vars = map[string]any{"AUTHZ_URL": "$endpoint:infra/authz"}
	b.component(simple("infra/authz", "2.0.1", 8223), projecttest.Entry{})
	b.component(withKeys(simple("erp/sales", "1.0.0", 8081), nil, "AUTHZ_URL"),
		projecttest.Entry{Mode: deployfile.ModeDisable, Config: map[string]any{"AUTHZ_URL": "$var:AUTHZ_URL"}})

	services := servicesOf(t, b.parsed())
	assert.NotContains(t, services, "infra-authz-2-0-1", "唯一引用它的组件关掉了：它不是顶层，跟着不跑")
}

// 额外端口按名字取；目标被外壳承载时指向外壳，端口仍是它自己的（与 *_ENDPOINT 同一条规则）。
func TestEndpointRefToExtraPortOfHostedMember(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell", "1.0.0", 9000), projecttest.Entry{})
	b.component(withExtraPort(simple("infra/authz", "2.0.1", 8223), "grpc", 9223), servedByEntry("infra/shell", "1.0.0"))
	b.component(withKeys(simple("erp/sales", "1.0.0", 8081), nil, "AUTHZ_URL", "AUTHZ_GRPC"), projecttest.Entry{
		Config: map[string]any{"AUTHZ_URL": "$endpoint:infra/authz", "AUTHZ_GRPC": "$endpoint:infra/authz:grpc"},
	})

	env := envOf(t, serviceOf(t, b.parsed(), "erp-sales-1-0-0"))
	assert.Equal(t, "http://infra-shell-1-0-0:8223", env["AUTHZ_URL"])
	assert.Equal(t, "http://infra-shell-1-0-0:9223", env["AUTHZ_GRPC"])
}

// 引用方是本机进程：地址换成宿主机上映射出来的端口；指向自己的引用换成它自己在宿主机上监听的端口
// （把自己的回调地址交给外部系统）。容器里的组件引用这个本机进程：服务名不变、端口是它的 localPort，并且能解析到宿主机。
func TestEndpointRefsFollowProcessesOnTheHost(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/authz", "2.0.1", 8223), projecttest.Entry{})
	b.component(withKeys(simple("infra/iam", "1.0.0", 8000), nil, "AUTHZ_URL", "SELF_URL"), projecttest.Entry{
		Mode: deployfile.ModeDebug, LocalPort: 18500,
		Config: map[string]any{"AUTHZ_URL": "$endpoint:infra/authz", "SELF_URL": "$endpoint:infra/iam/api/iam/webhooks/casdoor"},
	})
	b.component(withKeys(simple("erp/sales", "1.0.0", 8081), nil, "IAM_URL"), projecttest.Entry{
		Config: map[string]any{"IAM_URL": "$endpoint:infra/iam"},
	})

	result := b.generate()
	local := localEnv(t, result, "infra-iam-1-0-0")
	assert.Equal(t, "http://localhost:18223", local["AUTHZ_URL"], "容器依赖映射到 10000 + 容器端口")
	assert.Equal(t, "http://localhost:18500/api/iam/webhooks/casdoor", local["SELF_URL"])

	sales := serviceOf(t, b.parsed(), "erp-sales-1-0-0")
	assert.Equal(t, "http://infra-iam-1-0-0:18500", envOf(t, sales)["IAM_URL"])
	assert.Contains(t, extraHostsOf(t, sales), "infra-iam-1-0-0:host-gateway")
}

// 被引用的组件这次不跑：可选项不注入（组件自己降级，与弱依赖不在时一样）；必填项报错，说清是谁没跑。
func TestEndpointRefToComponentThatDoesNotRun(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/search", "1.0.0", 9200), projecttest.Entry{Mode: deployfile.ModeDisable})
	b.component(withKeys(simple("erp/sales", "1.0.0", 8081), nil, "SEARCH_URL"), projecttest.Entry{
		Config: map[string]any{"SEARCH_URL": "$endpoint:infra/search"},
	})
	assert.NotContains(t, envOf(t, serviceOf(t, b.parsed(), "erp-sales-1-0-0")), "SEARCH_URL")

	b = newBuilder(t)
	b.component(simple("infra/search", "1.0.0", 9200), projecttest.Entry{Mode: deployfile.ModeDisable})
	b.component(withKeys(simple("erp/sales", "1.0.0", 8081), []string{"SEARCH_URL"}, "SEARCH_URL"), projecttest.Entry{
		Config: map[string]any{"SEARCH_URL": "$endpoint:infra/search"},
	})
	err := b.generateErr()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SEARCH_URL")
	assert.Contains(t, err.Error(), "infra/search doesn't run this time")
}

// 指向项目里没有的组件、或没有的端口名：up 报错并点名，不生成一个连不上的地址。
func TestEndpointRefMustPointAtSomethingReal(t *testing.T) {
	b := newBuilder(t)
	b.component(withKeys(simple("erp/sales", "1.0.0", 8081), nil, "IAM_URL"), projecttest.Entry{
		Config: map[string]any{"IAM_URL": "$endpoint:infra/iam"},
	})
	err := b.generateErr()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "infra/iam is not in brickkit.yaml")

	b = newBuilder(t)
	b.component(simple("infra/iam", "1.0.0", 8000), projecttest.Entry{})
	b.component(withKeys(simple("erp/sales", "1.0.0", 8081), nil, "IAM_URL"), projecttest.Entry{
		Config: map[string]any{"IAM_URL": "$endpoint:infra/iam:grpc"},
	})
	err = b.generateErr()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no extra port named grpc")
}
