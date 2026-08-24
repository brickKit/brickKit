package cli

// 本文件是 `brickkit up --check-resources` 的启动前探测（15.7、P22）。
//
// 查两样：基础资源连不连得上，宿主机端口有没有被别人占着。
// 两样都只警告不阻断——启动之后再发现，代价是一堆半死不活的容器
// 和一段难以定位的日志；但判断"这台机器是不是该起"终究是使用者的事。

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/engine"
)

// checkResources 启动前体检：资源通不通、要占的宿主机端口有没有被别人占着。
//
// 全部只警告不阻断：资源也许正要靠这次启动带起来，端口也可能马上就释放。
// CLI 的职责是"先说一声"，不是替使用者做决定。
func checkResources(ctx context.Context, opts *Options, eng engine.Engine, plan *upPlan) {
	opts.Printf("\n🔍 资源可达性与端口占用检查：\n")
	checkResourceReachability(ctx, opts, plan)
	checkHostPorts(ctx, opts, eng, plan)
}

// checkResourceReachability 探测基础资源（15.7）。
//
// 平台不部署基础资源（006 §9.1），所以这里**一律探**：它们本该在 up 之前
// 就已经跑着。从前还要先排除"由 CLI 托管的那些"（它们正要靠这次启动带起来，
// 探它必然失败），托管取消之后这条例外也没有了。
func checkResourceReachability(ctx context.Context, opts *Options, plan *upPlan) {
	if plan.cfg.Deploy.Target == config.TargetK8s {
		renderK8sResources(opts, plan)
		return
	}

	used := map[string]bool{}
	for _, ref := range plan.states.Running() {
		used[ref.ID] = true
	}

	checked := 0
	for _, r := range plan.cfg.Resources {
		if !boundToAny(r, used) {
			continue
		}
		checked++
		address := fmt.Sprintf("%s:%d", r.Host, r.Port)
		if err := opts.probe(ctx, address); err != nil {
			renderWarnings(opts, []*clierr.Error{
				clierr.Warn(clierr.CodeNetworkUnreachable, "基础资源不可达").
					WithDetail("资源", r.ID).
					WithDetail("地址", address).
					WithDetail("原因", reasonText(err)).
					WithDetail("影响", "依赖它的组件会启动失败或在运行时报错").
					WithHint("确认该资源已启动、地址与端口正确、网络可达"),
			})
			continue
		}
		opts.Printf("   %-24s ● 可达（%s）\n", r.ID, address)
	}
	if checked == 0 {
		opts.Printf("   （本次没有组件绑定基础资源）\n")
	}
}

// renderK8sResources 只把资源地址列出来，不拨号。
//
// K8s 下的资源地址是集群内的 DNS 名，从开发者本机拨号必然失败——
// 那条"不可达"警告会在一切正常时出现，久了就没人看警告了。
func renderK8sResources(opts *Options, plan *upPlan) {
	used := map[string]bool{}
	for _, ref := range plan.states.Running() {
		used[ref.ID] = true
	}

	listed := 0
	for _, r := range plan.cfg.Resources {
		if !boundToAny(r, used) {
			continue
		}
		listed++
		opts.Printf("   %-24s %s:%d（集群内地址，本机不探测）\n", r.ID, r.Host, r.Port)
	}
	if listed == 0 {
		opts.Printf("   （本次没有组件绑定基础资源）\n")
		return
	}
	opts.Printf("   资源由运维部署在集群里；组件起得来就说明它连得上\n")
}

func boundToAny(r config.Resource, used map[string]bool) bool {
	for _, binding := range r.Bindings {
		if used[binding.ComponentID] {
			return true
		}
	}
	return false
}

// checkHostPorts 检查要占的宿主机端口有没有被**别的进程**占着（P22）。
//
// 生成阶段只能保证项目内部不打架。这台机器上别的进程占着某个端口，
// 得真的探一下才知道——真实验证时就撞到过：localPort 写了 9001，
// 而本机一个无关进程正占着它，生成一切正常，跑起来才 503。
func checkHostPorts(ctx context.Context, opts *Options, eng engine.Engine, plan *upPlan) {
	if len(plan.generated.HostPorts) == 0 {
		return
	}
	ours := ownPublishedPorts(ctx, eng, plan)

	for _, hp := range plan.generated.HostPorts {
		if ours[hp.Port] {
			// 上一次 up 留下的自家容器占着它，重复 up 时这是正常的
			continue
		}
		if err := opts.probe(ctx, fmt.Sprintf("127.0.0.1:%d", hp.Port)); err != nil {
			continue // 拨不通 = 没人监听 = 端口空着
		}
		renderWarnings(opts, []*clierr.Error{
			clierr.Warn(clierr.CodePortConflict, "宿主机端口已被占用").
				WithDetailf("端口", "%d", hp.Port).
				WithDetail("本次要用它的是", hp.Owner+"（"+hp.Purpose+"）").
				WithDetail("影响", "启动时会因为端口冲突失败").
				WithHint(
					"先停掉占用该端口的进程（lsof -i :"+itoa(hp.Port)+"）",
					"或在 brickkit.yaml 里改 exposePort / localPort",
				),
		})
	}
}

// ownPublishedPorts 找出本项目自己的容器已经映射出去的宿主机端口。
//
// 不排除它们的话，第二次 up 会把自己上一次留下的容器报成"端口被占用"。
func ownPublishedPorts(ctx context.Context, eng engine.Engine, plan *upPlan) map[int]bool {
	out := map[int]bool{}
	if eng == nil {
		// --dry-run：没有引擎可问，就当没有自家容器占着端口
		return out
	}
	statuses, err := eng.Status(ctx, plan.layout.GeneratedDir()+string(os.PathSeparator)+composeFileName,
		engine.ProjectName(plan.cfg.Project))
	if err != nil {
		return out
	}

	for _, s := range statuses {
		for _, port := range publishedPorts(s.Ports) {
			out[port] = true
		}
	}
	return out
}

// publishedPorts 从 `0.0.0.0:18092->8080/tcp, 9090/tcp` 里取出宿主机端口。
func publishedPorts(text string) []int {
	var out []int
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		arrow := strings.Index(part, "->")
		if arrow < 0 {
			continue // 没有 -> 的是容器内部端口，没映射到宿主机
		}
		hostPart := part[:arrow]
		colon := strings.LastIndex(hostPart, ":")
		if colon < 0 {
			continue
		}
		if port, err := parsePort(hostPart[colon+1:]); err == nil {
			out = append(out, port)
		}
	}
	return out
}

func parsePort(text string) (int, error) {
	port := 0
	for _, r := range strings.TrimSpace(text) {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("不是端口号：%q", text)
		}
		port = port*10 + int(r-'0')
	}
	if port == 0 {
		return 0, fmt.Errorf("不是端口号：%q", text)
	}
	return port, nil
}
