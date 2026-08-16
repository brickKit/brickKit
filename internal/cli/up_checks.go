package cli

// 本文件是 `brickkit up` 的三项"启动前的事"：
//
//	--only            把启动集合收窄到指定组件及其强依赖（004 §3.5）
//	升级              版本号变了就拉新版本 Manifest 与产物、做兼容性检查（004 §3.5.1）
//	--check-resources 资源可达性与宿主机端口占用（15.7、P22）
//
// 三者的共同点：都在**真正启动之前**把话说清楚。启动之后再发现，
// 代价是一堆半死不活的容器和一段难以定位的日志。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

// ============================================================
// --only
// ============================================================

// restrictToOnly 把启动集合收窄到 --only 指定的组件**及其强依赖**。
//
// 只启动被点名的那一个是起不来的：它的强依赖不在，容器起来也连不上
// （003 §4.3 的可行性判定同理）。所以这里取的是强依赖闭包。
func restrictToOnly(opts *Options, plan *upPlan, only []string) (*cascade.Result, error) {
	selected, err := selectRefsIn(plan.cfg, only)
	if err != nil {
		return nil, err
	}

	// 15.9：点名一个被显式关掉的组件，两个意图直接冲突
	for _, ref := range selected {
		if stateOf(plan.states, ref) == cascade.StateDisabled {
			return nil, clierr.Newf(clierr.CodeComponentDisabled,
				"错误：--only 指定的组件被显式禁用").
				WithDetail("组件", refText(ref)).
				WithDetail("原因", "brickkit.yaml 中该组件是 enabled: false").
				WithHint(
					"移除该组件的 enabled: false，或改成 enabled: true",
					"确认这次确实要启动它——显式禁用通常是有意的",
				)
		}
	}

	keep := map[resolver.Ref]bool{}
	for _, ref := range selected {
		addWithRequires(plan.graph, ref, keep)
	}

	opts.Printf("🎯 --only：只启动 %s 及其强依赖\n", strings.Join(only, "、"))
	return plan.states.Restrict(keep, "未被 --only 选中"), nil
}

// addWithRequires 把该组件与它的强依赖递归加进集合。
func addWithRequires(graph *resolver.Graph, ref resolver.Ref, keep map[resolver.Ref]bool) {
	if keep[ref] {
		return
	}
	keep[ref] = true

	node := graph.Node(ref)
	if node == nil {
		return
	}
	for _, dep := range node.Requires {
		addWithRequires(graph, dep, keep)
	}
}

// selectRefsIn 解析 --only 的写法（004 §3.5）。
//
//	people/basic          该组件的**所有**版本（多版本默认共存，002 §3.6）
//	people/basic@1.0.0    只这一个版本
func selectRefsIn(cfg *config.Config, only []string) ([]resolver.Ref, error) {
	var out []resolver.Ref
	seen := map[resolver.Ref]bool{}

	for _, item := range only {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		id, version, hasVersion := strings.Cut(item, "@")

		matched := false
		for _, c := range cfg.Components {
			if c.ID != id || (hasVersion && c.Version != version) {
				continue
			}
			ref := resolver.Ref{ID: c.ID, Version: c.Version}
			if !seen[ref] {
				seen[ref] = true
				out = append(out, ref)
			}
			matched = true
		}
		if !matched {
			return nil, unknownComponentError(item, cfg)
		}
	}
	return out, nil
}

func stateOf(states *cascade.Result, ref resolver.Ref) cascade.State {
	for _, c := range states.Components {
		if c.Ref == ref {
			return c.State
		}
	}
	return cascade.StateSkipped
}

// ============================================================
// 升级（004 §3.5.1）
// ============================================================

// upgradeInfo 是一次版本变更。
type upgradeInfo struct {
	ID   string
	From string
	To   string
	// Migration 是新版本声明的迁移命令，空表示新版本没有迁移。
	Migration string
	// Deps / AddedConfig / RemovedConfig / Artifacts / Quota 是新旧 Manifest 的
	// 差异描述（004 §3.5.1 规定的六项里的其余五项），空字符串表示"无变化"。
	Deps          string
	AddedConfig   string
	RemovedConfig string
	Artifacts     string
	Quota         string
}

// handleUpgrades 处理"使用者把版本号改了"这件事。
//
// 判据是**缓存里有这个组件的别的版本、却没有现在这个版本**：
//   - 首次安装 → 缓存里一个版本都没有，不是升级
//   - 缓存被清空 → 同上，不是升级（否则每次清缓存都会被当成全量升级）
//
// 检测到之后：兼容性检查（002 §7.7，阻断项在这里就报错）→ 拉产物。
// 都要在解析依赖图之前做完——不然新版本的依赖还没进图就先报"缺依赖"。
func handleUpgrades(
	ctx context.Context, opts *Options, layout config.Layout,
	cfg *config.Config, client *source.Client,
) ([]upgradeInfo, error) {
	upgrades := detectUpgrades(layout, cfg)
	if len(upgrades) == 0 {
		return nil, nil
	}

	opts.Printf("⬆️ 检测到版本变更（升级流程，004 §3.5.1）：\n")
	for _, u := range upgrades {
		opts.Printf("   %s: %s → %s\n", u.ID, u.From, u.To)
	}

	r := resolver.New(resolver.FromSource(client))
	for i, u := range upgrades {
		target := resolver.Ref{ID: u.ID, Version: u.To}

		// 002 §7.7：强依赖不可满足 / 资源未绑定 / 循环依赖 → 阻断
		report, err := r.CheckUpgrade(ctx, cfg, target)
		if err != nil {
			return nil, err
		}
		renderWarnings(opts, report.Warnings)

		node := report.Graph.Node(target)
		if node != nil && node.Manifest != nil {
			if node.Manifest.Migration != nil {
				upgrades[i].Migration = strings.Join(node.Manifest.Migration.Command, " ")
			}
			// 004 §3.5.1 的其余五项：拿缓存里的旧 Manifest 与新的比
			describeUpgradeDiff(&upgrades[i], cachedManifest(layout, u.ID, u.From), node.Manifest)
			// P10：新版本的产物要下载到新的版本化服务名目录下。
			// 旧版本的保留——调用方可能还指着旧版本（002 §7.8）
			if result, err := client.DownloadArtifacts(ctx, node.Manifest); err == nil {
				renderWarnings(opts, result.Warnings)
			} else {
				// 产物是开发时的辅助，取不到不该拦住启动（004 §10.1）
				opts.Printf("⚠️ %s 的产物下载失败：%s\n", refText(target), clierr.As(err).Message)
			}
		}
	}
	opts.Printf("\n")
	return upgrades, nil
}

// detectUpgrades 比对 brickkit.yaml 与 Manifest 缓存，找出版本变更。
func detectUpgrades(layout config.Layout, cfg *config.Config) []upgradeInfo {
	cached := cachedVersions(layout)

	var out []upgradeInfo
	for _, c := range cfg.Components {
		versions := cached[c.ID]
		if len(versions) == 0 {
			continue // 首次安装，或缓存被清空
		}
		if contains(versions, c.Version) {
			continue // 这个版本本来就装着
		}
		// 同一组件的多版本共存时，取字典序最前的那个当"从哪来"——
		// 只用于展示，不影响任何判定
		out = append(out, upgradeInfo{ID: c.ID, From: versions[0], To: c.Version})
	}
	return out
}

// cachedVersions 读出 .brickkit/manifests/ 里每个组件已缓存的版本。
//
// 文件名形如 people-basic-1.0.0.yaml：组件 ID 里的 `/` 在文件名里是 `-`，
// 因此不能直接按 `-` 切——用配置里的组件 ID 反过来匹配前缀才可靠。
func cachedVersions(layout config.Layout) map[string][]string {
	entries, err := os.ReadDir(layout.ManifestsDir())
	if err != nil {
		return nil
	}

	out := map[string][]string{}
	for _, entry := range entries {
		// 只认 Manifest 本身。这个目录里还躺着签名缓存
		// （people-basic-1.0.0.sig.json）——不筛掉的话，去掉一层扩展名会得到
		// people-basic-1.0.0.sig，"版本号"就成了 1.0.0.sig，一路显示到升级摘要里。
		if filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		id, version, ok := splitCachedName(name)
		if !ok {
			continue
		}
		out[id] = append(out[id], version)
	}
	return out
}

// splitCachedName 把 `people-basic-1.0.0` 拆成 people/basic + 1.0.0。
//
// 版本号一定在最后一个 `-` 之后，且带点；前面的 `-` 分隔的是 scope 与 name。
func splitCachedName(name string) (id, version string, ok bool) {
	idx := strings.LastIndex(name, "-")
	if idx <= 0 {
		return "", "", false
	}
	version = name[idx+1:]
	if !strings.Contains(version, ".") {
		return "", "", false
	}

	prefix := name[:idx]
	slash := strings.Index(prefix, "-")
	if slash <= 0 {
		return "", "", false
	}
	return prefix[:slash] + "/" + prefix[slash+1:], version, true
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// renderUpgradeSummary 输出 --dry-run 的升级变更摘要（004 §3.5.1）。
//
// 只是信息展示，不阻断任何操作。
func renderUpgradeSummary(opts *Options, plan *upPlan) {
	if len(plan.upgrades) == 0 {
		return
	}

	opts.Printf("\n📋 升级变更摘要：\n")
	for _, u := range plan.upgrades {
		opts.Printf("   %s: %s → %s\n", u.ID, u.From, u.To)

		// 六项固定都出（004 §3.5.1）。没变化的写"无"而不是隐藏——
		// 藏起来会让人分不清"没有变化"和"平台没检查这一方面"。
		for _, row := range []struct{ label, value string }{
			{"依赖变更", u.Deps},
			{"新增配置项", u.AddedConfig},
			{"删除配置项", u.RemovedConfig},
			{"数据库迁移", u.Migration},
			{"artifacts 变更", u.Artifacts},
			{"资源配额变更", u.Quota},
		} {
			opts.Printf("   ├── %s：%s\n", row.label, valueOrNone(row.value))
		}
		opts.Printf("   └── 旧版本产物：保留（调用方可能仍指向旧版本）\n")
	}
}

func valueOrNone(value string) string {
	if strings.TrimSpace(value) == "" {
		return "无"
	}
	return value
}

// ============================================================
// --check-resources（15.7、P22）
// ============================================================

// checkResources 启动前体检：资源通不通、要占的宿主机端口有没有被别人占着。
//
// 全部只警告不阻断：资源也许正要靠这次启动带起来，端口也可能马上就释放。
// CLI 的职责是"先说一声"，不是替使用者做决定。
func checkResources(ctx context.Context, opts *Options, eng engine.Engine, plan *upPlan) {
	opts.Printf("\n🔍 资源可达性与端口占用检查：\n")
	checkResourceReachability(ctx, opts, plan)
	checkHostPorts(ctx, opts, eng, plan)
}

// checkResourceReachability 探测外部资源（15.7）。
//
// 只探外部资源：CLI 托管的资源还在这次启动里，现在当然连不上——
// 去探它只会产生一条必然出现、又毫无意义的警告。
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
		if compose.IsManagedHost(r.Host) || !boundToAny(r, used) {
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
		opts.Printf("   （没有需要探测的外部资源；CLI 托管的资源随本次启动一起起来）\n")
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

// ============================================================
// P5 资源密码硬编码告警（006 §3.3、008）
// ============================================================

// warnHardcodedPasswords 提醒 brickkit.yaml 里写了明文密码。
//
// 是警告不是错误：本地开发写个 dev 密码很常见，阻断只会让人绕开 CLI。
// 警告里**不打印密码本身**——那等于把它又抄了一遍到终端和 CI 日志里。
func warnHardcodedPasswords(opts *Options, cfg *config.Config) {
	var offenders []string
	for _, r := range cfg.Resources {
		if isHardcodedSecret(r) {
			offenders = append(offenders, r.ID)
		}
	}
	if len(offenders) == 0 {
		return
	}

	err := clierr.Warn(clierr.CodeConfigInvalid, "brickkit.yaml 中存在明文密码").
		WithDetail("资源", strings.Join(offenders, "、")).
		WithDetail("要求", "密码必须用 ${ENV_VAR} 引用（006 §3.3、008）").
		WithHint(
			"改成 password: ${POSTGRES_PASSWORD}，并把真实值放进 .env",
			".env 必须在 .gitignore 中",
		)
	renderWarnings(opts, []*clierr.Error{err})
}

// isHardcodedSecret 判断这条资源的密码是不是写死在 brickkit.yaml 里的。
//
// 判据是**原文**写没写 ${ENV_VAR}，不是解析后的值：解析器在读配置时就把
// ${VAR} 展开掉了（003 §5.4），拿展开后的值去判断，只会在变量**真的配了**
// 的时候把使用者骂一顿，而变量漏配时（占位符原样保留）反倒不吭声——
// 正好反了。空值表示没配密码（比如不需要密码的资源），不算问题。
func isHardcodedSecret(r config.Resource) bool {
	if r.PasswordFromEnv || strings.TrimSpace(r.Password) == "" {
		return false
	}
	return true
}

// ============================================================
// 35.17 config 里的明文密钥告警
// ============================================================

// secretishKey 判断一个配置项名字看不看得出是密钥。
//
// **判据刻意收窄，宁可漏报也不误报。** 一个见谁都喊的告警，两天之内就会被
// 所有人无视，那时它连真的密钥也保护不了——所以只认那些几乎不可能有别的
// 含义的词，而不是"包含 key 就算"（apiKey 是密钥，但 sortKey、cacheKey 不是）。
func secretishKey(name string) bool {
	lower := strings.ToLower(name)
	for _, word := range []string{
		"password", "passwd", "secret", "token", "credential",
		"privatekey", "private_key", "apikey", "api_key", "accesskey", "access_key",
	} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// warnConfigSecrets 提醒 component.config 里写了明文密钥（35.17）。
//
// 泄漏路径不是生成物，而是 **brickkit.yaml 本身**：那个文件是明确建议
// 提交进 Git 的（003 §1.2）。写在 resources[].password 里的密码有 P5 告警
// 兜着，写在 config 里的此前一声不吭。
//
// 与 P5 一样是警告不是错误：config 里放什么由使用者决定，
// 平台不该替他判断哪个值算密钥；但看着像密钥的东西必须说一声。
//
// **绝不打印值本身**——那等于把密钥又抄了一遍到终端和 CI 日志里。
func warnConfigSecrets(opts *Options, cfg *config.Config) {
	var offenders []string
	for _, c := range cfg.Components {
		for name := range c.Config {
			// 写成 ${ENV_VAR} 就是做对了。判据取的是**展开前**的原文
			// （config.ConfigFromEnv），理由见那个字段的说明
			if c.ConfigFromEnv[name] || !secretishKey(name) {
				continue
			}
			offenders = append(offenders, c.Ref()+" → "+name)
		}
	}
	if len(offenders) == 0 {
		return
	}
	sort.Strings(offenders)

	renderWarnings(opts, []*clierr.Error{
		clierr.Warn(clierr.CodeConfigInvalid, "brickkit.yaml 的 config 里可能写了明文密钥").
			WithDetail("配置项", strings.Join(offenders, "、")).
			WithDetail("为什么要紧", "brickkit.yaml 是建议提交进 Git 的（003 §1.2），"+
				"写在这里的密钥会跟着进版本库，而且历史里删不掉").
			WithHint(
				"改成 ${MY_TOKEN} 这样的引用，把真实值放进 .env",
				".env 必须在 .gitignore 中",
				"确实不是密钥的话可以忽略这条——判据只看名字，不看值",
			),
	})
}
