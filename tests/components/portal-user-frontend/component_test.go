// portal/user-frontend 没有业务代码，只有 nginx 配置与静态文件。
//
// 但它照样需要测试——而且是最需要的那一类：nginx 配置写错时，容器会**启动失败**，
// 平台看到的只是"容器起不来"，日志里那句 nginx 报错还得进容器才看得到。
// 与其等到部署时才发现，不如在这里用真 nginx 跑一遍 `nginx -t`。
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// placeholderPattern 匹配未被替换的环境变量占位符（形如 ${SOME_VAR}）。
var placeholderPattern = regexp.MustCompile(`\$\{[A-Z_][A-Z0-9_]*\}`)

// ============================================================
// 26.5 后端地址来自环境变量
// ============================================================

// TestConfigUsesInjectedBackendEndpoint 是 26.5。
//
// 前端硬编码后端地址的话，换一个部署环境这个组件就废了——而平台明明
// 已经按强依赖把地址注入进来了（003 §4.5）。
func TestConfigUsesInjectedBackendEndpoint(t *testing.T) {
	conf := readFile(t, "templates/default.conf.template")

	if !strings.Contains(conf, "${ERP_BACKEND_ENDPOINT}") {
		t.Error("nginx 配置应当引用 ${ERP_BACKEND_ENDPOINT}")
	}
	// 反过来：不能有任何写死的后端地址
	for _, forbidden := range []string{"localhost", "127.0.0.1", "erp-backend-1-0-0", "http://erp"} {
		if strings.Contains(conf, forbidden) {
			t.Errorf("nginx 配置里出现了硬编码地址 %q", forbidden)
		}
	}
}

// TestIndexHasNoHardcodedBackend：页面里的 JS 也不能写死后端地址。
//
// 配置改对了、JS 里却写着后端地址，是很容易漏的一处——浏览器直连后端
// 还会撞上跨域，症状看起来完全是另一回事。
func TestIndexHasNoHardcodedBackend(t *testing.T) {
	html := readFile(t, "html/index.html")

	for _, forbidden := range []string{"http://localhost", "127.0.0.1", "erp-backend-1-0-0"} {
		if strings.Contains(html, forbidden) {
			t.Errorf("index.html 里出现了硬编码地址 %q", forbidden)
		}
	}
	if !strings.Contains(html, `"/api/v1/`) {
		t.Error("页面应当用相对路径 /api/v1/… 请求，由 nginx 转发")
	}
}

// ============================================================
// 反向代理必须扛得住"后端还没起来"
// ============================================================

// TestProxyResolvesAtRequestTime 挡住一个 nginx 的经典坑。
//
// nginx 对**写死在 proxy_pass 里的主机名**只在启动时解析一次：后端还没起来时，
// nginx 直接以 "host not found in upstream" 退出，整个前端起不来——
// 而它本该照常把页面发出去，只是 /api/ 暂时 502。
//
// 把地址放进变量就变成每次请求时解析。这两行是配套的，缺一不可：
// 有 set 没 resolver 会在启动时报语法错，有 resolver 没 set 则等于没改。
func TestProxyResolvesAtRequestTime(t *testing.T) {
	conf := readFile(t, "templates/default.conf.template")

	if !strings.Contains(conf, "resolver ") {
		t.Error("用变量做 proxy_pass 时必须显式声明 resolver")
	}
	if !strings.Contains(conf, "set $backend") {
		t.Error("后端地址应当放进变量，才能在每次请求时解析")
	}
	if !strings.Contains(conf, "proxy_pass $backend$request_uri") {
		t.Error("proxy_pass 用了变量之后，nginx 不再自动转发原始 URI，必须带上 $request_uri")
	}
}

// TestResolverIsNotHardcoded：DNS 地址不能写死。
//
// Docker 的内嵌 DNS 在 127.0.0.11，K8s 的 CoreDNS 是另一个地址且各集群不同。
// 写死任何一个，另一个环境就直接起不来。
//
// 用的是官方镜像**自带**的 15-local-resolvers.envsh：它从 /etc/resolv.conf
// 生成 NGINX_LOCAL_RESOLVERS，还处理了 IPv6 的方括号与多个 nameserver。
// 这个开关默认关闭，必须在 Dockerfile 里显式打开——漏了这一行，
// resolver 就会拿到空值，nginx 直接语法错误起不来。
func TestResolverIsNotHardcoded(t *testing.T) {
	conf := readFile(t, "templates/default.conf.template")
	if !strings.Contains(conf, "${NGINX_LOCAL_RESOLVERS}") {
		t.Error("resolver 应当用官方镜像提供的 ${NGINX_LOCAL_RESOLVERS}")
	}

	dockerfile := readFile(t, "Dockerfile")
	if !strings.Contains(dockerfile, "NGINX_ENTRYPOINT_LOCAL_RESOLVERS=1") {
		t.Error("必须打开 NGINX_ENTRYPOINT_LOCAL_RESOLVERS，否则那个脚本直接 return 0")
	}
}

// ============================================================
// 26.6 健康检查
// ============================================================

// TestHealthzDoesNotProxy 是 002 §9.4 在 nginx 上的形态。
//
// 健康检查若被 location /api/ 之类的规则捞去代理到后端，后端一抖，
// 编排系统就会把这个本身完全正常的前端容器杀掉重启。
func TestHealthzDoesNotProxy(t *testing.T) {
	conf := readFile(t, "templates/default.conf.template")

	if !strings.Contains(conf, "location = /healthz") {
		t.Error("健康检查应当用精确匹配 location = /healthz，避免被别的规则捞走")
	}
	if !strings.Contains(conf, "return 200") {
		t.Error("健康检查应当直接返回常量，不碰后端")
	}
}

// ============================================================
// 组件对平台的承诺
// ============================================================

func TestComponentYamlDeclaresDependency(t *testing.T) {
	manifest := readFile(t, "component.yaml")

	if !strings.Contains(manifest, "erp/backend@1.0.0") {
		t.Error("应当声明对 erp/backend 的强依赖（平台据此注入 ERP_BACKEND_ENDPOINT）")
	}
	if strings.Contains(manifest, "optional: true") {
		t.Error("erp/backend 是强依赖：没有它这个前端什么也做不了，不该标 optional")
	}
	if strings.Contains(manifest, "kind: database") || strings.Contains(manifest, "kind: cache") {
		t.Error("静态前端不该绑定任何资源")
	}
}

// TestPortsAgree：三处端口必须一致。
//
// Manifest 说 8080、nginx 听 80、Dockerfile 暴露 3000——这类错的表现是
// "容器起来了但连不上"，而每一处单看都没问题。
func TestPortsAgree(t *testing.T) {
	const port = "8080"

	for name, path := range map[string]string{
		"component.yaml": "component.yaml",
		"nginx 模板":       "templates/default.conf.template",
		"Dockerfile":     "Dockerfile",
	} {
		if !strings.Contains(readFile(t, path), port) {
			t.Errorf("%s 里没有出现端口 %s", name, port)
		}
	}

	conf := readFile(t, "templates/default.conf.template")
	if strings.Contains(conf, "listen 80;") {
		t.Error("nginx 不能听 80：容器以非 root 运行，1024 以下的端口需要特权")
	}
}

func TestDockerfileRunsAsNonRoot(t *testing.T) {
	dockerfile := readFile(t, "Dockerfile")

	if !strings.Contains(dockerfile, "USER 101") {
		t.Error("Dockerfile 必须以非 root 用户运行")
	}
	// 非 root 的 nginx 需要这三处改动，缺一个容器就起不来
	for _, need := range []string{"pid /tmp/nginx.pid", "/etc/nginx/conf.d", "/var/cache/nginx"} {
		if !strings.Contains(dockerfile, need) {
			t.Errorf("非 root 运行 nginx 还需要处理 %s", need)
		}
	}
}

// ============================================================
// 用真 nginx 校验配置语法
// ============================================================

// TestNginxConfigIsValid 是这个组件最有价值的一条测试。
//
// nginx 配置写错时容器**启动失败**，平台看到的只是"容器起不来"，
// 那句真正有用的报错还得进容器才看得到。这里直接用真 nginx 跑 `nginx -t`。
//
// 没有 Docker 时跳过：这条测试的价值在于"部署前就发现"，
// 不该因为开发机没装 Docker 就让整个测试套失败。
func TestNginxConfigIsValid(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("没有 docker，跳过 nginx 配置语法校验")
	}

	image := buildImage(t)

	// 用真实的注入值渲染模板，再让 nginx 自己校验
	cmd := exec.Command("docker", "run", "--rm",
		"-e", "ERP_BACKEND_ENDPOINT=http://erp-backend-1-0-0:8080",
		"-e", "NGINX_RESOLVER=127.0.0.11",
		"--entrypoint", "/bin/sh", image,
		"-c", "/docker-entrypoint.sh nginx -t 2>&1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("nginx 配置校验失败：%v\n%s", err, out)
	}
	if !strings.Contains(string(out), "syntax is ok") {
		t.Fatalf("nginx 没有报告配置合法：\n%s", out)
	}
}

// TestRenderedConfigHasNoPlaceholders：渲染之后不能还留着 ${...}。
//
// 漏掉一个变量时 nginx 未必报错——它可能把 ${ERP_BACKEND_ENDPOINT} 当成
// 一个字面主机名去解析，于是每次请求都 502，而配置校验完全通过。
func TestRenderedConfigHasNoPlaceholders(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("没有 docker，跳过")
	}

	image := buildImage(t)

	// 第一个参数必须是 nginx：官方入口脚本只在 $1 为 nginx / nginx-debug 时
	// 才跑 /docker-entrypoint.d/ 下的钩子（也就是渲染模板那一步）。
	// 传别的命令进去，拿到的会是镜像**自带的**那份 default.conf——
	// 测试看起来在跑，其实什么也没验到。
	cmd := exec.Command("docker", "run", "--rm",
		"-e", "ERP_BACKEND_ENDPOINT=http://erp-backend-1-0-0:8080",
		"--entrypoint", "/bin/sh", image,
		"-c", "/docker-entrypoint.sh nginx -t >/dev/null 2>&1; cat /etc/nginx/conf.d/default.conf")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("渲染配置失败：%v\n%s", err, out)
	}

	rendered := string(out)
	// 只找**真正的**占位符（大写下划线形式）。注释里写的 ${环境变量}
	// 这类中文说明不是变量，envsubst 本来就不会动它
	if leftover := placeholderPattern.FindString(rendered); leftover != "" {
		t.Errorf("渲染后仍有未替换的占位符 %s：\n%s", leftover, rendered)
	}
	if !strings.Contains(rendered, "erp-backend-1-0-0:8080") {
		t.Errorf("后端地址没有被替换进去：\n%s", rendered)
	}
	// resolver 必须是一个真地址，而不是空的（空的话 nginx 直接语法错误）
	if strings.Contains(rendered, "resolver ;") || strings.Contains(rendered, "resolver  ") {
		t.Errorf("resolver 没有拿到地址：\n%s", rendered)
	}
}

// buildImage 构建镜像并返回 tag。
func buildImage(t *testing.T) string {
	t.Helper()

	const image = "brickkit-test/portal-user-frontend:test"
	out, err := exec.Command("docker", "build", "-q", "-t", image, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("构建镜像失败：%v\n%s", err, out)
	}
	return image
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败：%v", path, err)
	}
	return string(raw)
}

// ============================================================
// K8s 里的 FQDN（真部署到 minikube 才撞出来的）
// ============================================================

// TestClusterFQDNHookExists 记录一个 nginx 特有的坑。
//
// nginx 的 `resolver` 指令**不使用 /etc/resolv.conf 的 search 域**——它只拿
// nameserver 的地址。而 K8s 正是靠 search 域让裸服务名可解析。于是平台注入的
// `http://erp-backend-1-0-0:8080` 在别的组件里好好的（Go/Python 的 DNS 解析
// 会走 search 域），到 nginx 这里就是：
//
//	erp-backend-1-0-0 could not be resolved (2: Server failure)
//
// 浏览器收到 502，跟"后端挂了"一模一样——而后端好好的。
func TestClusterFQDNHookExists(t *testing.T) {
	script := readFile(t, "docker-entrypoint.d/15-cluster-fqdn.envsh")

	if !strings.Contains(script, "resolv.conf") {
		t.Error("要从 /etc/resolv.conf 里读 search 域")
	}
	if !strings.Contains(script, "export ERP_BACKEND_ENDPOINT") {
		t.Error("必须 export，否则后面的 envsubst 看不到改过的值")
	}
	// 文件名要以 .envsh 结尾：官方入口对 .envsh 是 source、对 .sh 是执行。
	// 执行的话变量留在子 shell 里，改了等于没改
	if _, err := os.Stat("docker-entrypoint.d/15-cluster-fqdn.envsh"); err != nil {
		t.Fatalf("钩子必须以 .envsh 结尾：%v", err)
	}

	dockerfile := readFile(t, "Dockerfile")
	if !strings.Contains(dockerfile, "15-cluster-fqdn.envsh") {
		t.Error("Dockerfile 要把这个钩子拷进镜像")
	}
}

// TestClusterFQDNHookOnlyActsInKubernetes：Docker 环境不能被误伤。
//
// Docker 里容器也可能有 search 域（网络名），把它拼上去反而解析不了。
// `.svc.` 是 Kubernetes 的通用标志（集群域可自定义，但 svc 这一段固定）。
func TestClusterFQDNHookOnlyActsInKubernetes(t *testing.T) {
	script := readFile(t, "docker-entrypoint.d/15-cluster-fqdn.envsh")

	if !strings.Contains(script, "svc") {
		t.Error("要靠 .svc. 判断是不是在 K8s 里，否则会误伤 Docker 环境")
	}
}

// TestClusterFQDNHookBehaviour 直接跑这个脚本，验证三种输入。
//
// 光看源码里有没有某个字符串是不够的——这条逻辑有分支，
// 而分支写反的表现是"Docker 好好的、K8s 502"，或者反过来。
func TestClusterFQDNHookBehaviour(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("没有 sh，跳过")
	}

	cases := map[string]struct {
		resolvConf string
		endpoint   string
		want       string
	}{
		"K8s 里补成 FQDN": {
			resolvConf: "nameserver 10.96.0.10\nsearch erp-demo.svc.cluster.local svc.cluster.local\n",
			endpoint:   "http://erp-backend-1-0-0:8080",
			want:       "http://erp-backend-1-0-0.erp-demo.svc.cluster.local:8080",
		},
		"Docker 里原样不动": {
			resolvConf: "nameserver 127.0.0.11\nsearch brickkit-erp-demo-net\n",
			endpoint:   "http://erp-backend-1-0-0:8080",
			want:       "http://erp-backend-1-0-0:8080",
		},
		"已经是 FQDN 就不动": {
			resolvConf: "nameserver 10.96.0.10\nsearch erp-demo.svc.cluster.local\n",
			endpoint:   "http://erp-backend.example.com:8080",
			want:       "http://erp-backend.example.com:8080",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			resolv := filepath.Join(dir, "resolv.conf")
			if err := os.WriteFile(resolv, []byte(c.resolvConf), 0o644); err != nil {
				t.Fatalf("写 resolv.conf 失败：%v", err)
			}

			// 脚本里写死了 /etc/resolv.conf，测试时把它替换成临时文件
			script := strings.ReplaceAll(
				readFile(t, "docker-entrypoint.d/15-cluster-fqdn.envsh"),
				"/etc/resolv.conf", resolv)
			path := filepath.Join(dir, "hook.envsh")
			if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
				t.Fatalf("写脚本失败：%v", err)
			}

			// 脚本用 return 退出，必须 source 而不是执行
			cmd := exec.Command("sh", "-c",
				". "+path+" >/dev/null 2>&1; printf '%s' \"$ERP_BACKEND_ENDPOINT\"")
			cmd.Env = append(os.Environ(), "ERP_BACKEND_ENDPOINT="+c.endpoint)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("跑脚本失败：%v\n%s", err, out)
			}
			if got := string(out); got != c.want {
				t.Errorf("期望 %q，实际 %q", c.want, got)
			}
		})
	}
}
