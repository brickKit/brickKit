package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// Compose 是基于 compose 的引擎实现。
type Compose struct {
	name string
	// bin 与 base 一起构成命令前缀：docker + [compose]。
	bin  string
	base []string
	// runner 执行命令，测试可替换。
	runner func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewDocker 返回 docker compose 引擎。
func NewDocker() *Compose {
	return &Compose{name: Docker, bin: "docker", base: []string{"compose"}, runner: run}
}

// NewPodman 返回 podman compose 引擎。
//
// podman compose 在已验证的机器上直接调用与 Docker 相同的 docker-compose
// 二进制，因此 Compose 结构体的其余行为（命令拼装、ps 输出解析、P27 的
// stdout/stderr 处理）全部原样适用，只有 bin 不同。
func NewPodman() *Compose {
	return &Compose{name: Podman, bin: "podman", base: []string{"compose"}, runner: run}
}

func (c *Compose) Name() string { return c.name }

// Up 启动（compose up -d --wait）。
//
// `--wait` 让 compose 一直等到所有容器 running/healthy 才返回。没有它，
// `up -d` 只保证"启动命令发出去了"：依赖链末端的组件此刻多半还是
// health=starting，紧接着查状态会得到一个假的失败结论。
//
// `--remove-orphans` 由 PruneSelector 是否非空来决定。`--only` 删除之后
// 命令层其实总会给出选择器（每次 up 都按完整配置生成），但这个条件留着：
// 引擎不该假设调用方永远想清理——那是命令层的判断，K8s 侧同一个字段
// 也是这么用的（005 §5.9.1）。
func (c *Compose) Up(ctx context.Context, req UpRequest) error {
	args := append(c.projectArgs(req.File, req.Project, req.ProjectDir), "up", "-d", "--wait")
	if req.PruneSelector != "" {
		args = append(args, "--remove-orphans")
	}
	args = append(args, req.Services...)

	if _, err := c.exec(ctx, args...); err != nil {
		return err
	}
	return nil
}

// Down 停止整个项目。**不带 -v**：数据卷（数据库数据）必须保留（004 §3.6）。
//
// # 只认项目名，不认部署文件
//
// 从前这里带着 `-f <生成的 compose 文件>`，于是 down 停掉的是"**文件里写着**
// 的那些 service"，而不是"这个项目**实际跑着**的那些"。两者会分叉，
// 因为那份文件被不止一条命令写：`up --dry-run` 也会重写它（它本该只回答
// "这次打算跑什么"，却顺手改掉了"上次实际部署了什么"的唯一记录）。
//
// 真跑出来的样子：up 起两个组件 → 给其中一个写 mode: disable → up --dry-run
// 看一眼 → down。文件里此刻只剩一个 service，compose 就只拆那一个，
// 另一个容器**继续跑着**，而 CLI 拿到 exit 0，照样打印"已停止全部组件"。
// 一条谎报成功的命令比一条失败的命令危险得多。
//
// 项目名才是这批容器的身份（compose 把它写在每个容器的标签上），
// 文件只是中间人。去掉 `-f` 之后 compose 从标签认项目，顺手把任何来源的
// 孤儿一并收走——手工改过文件、上一次 up 中断留下的，都在内。
//
// 三种边界都验过：cwd 里另有一份别人的 docker-compose.yaml 不受干扰
// （compose 走标签，不读它）；项目已经空了再执行一次是 exit 0 + 一条警告；
// 项目根本不存在同样是 exit 0。
func (c *Compose) Down(ctx context.Context, req DownRequest) error {
	args := append(append([]string{}, c.base...), "-p", req.Project, "down", "--remove-orphans")
	_, err := c.exec(ctx, args...)
	return err
}

// Status 返回该项目下所有 service 的状态。
//
// **不带 `-f`。** compose 从容器标签就能认项目（实测 v5.3.1：删掉部署文件后
// `-p X ps --format json` 的输出与带 `-f` 时逐字节相同）。不依赖那份文件，
// 是因为它在 .gitignore 里、而且文档明说可以随时删——依赖它的后果是
// 一次 `git clean -xdf` 之后 `status` 谎报"项目尚未启动过"，而容器还跑着。
//
// 顺带一个好处：不带 `-f` 时列出的是**该项目名下的全部容器**，
// 而不只是生成文件里写着的那些。上一次留下的孤儿因此也在视野里。
func (c *Compose) Status(ctx context.Context, project string) ([]Status, error) {
	args := append(c.projectArgs("", project, ""), "ps", "-a", "--format", "json")

	out, err := c.exec(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parsePS(out)
}

// CheckImage 检查镜像是否可用（004 §3.5 的"检测镜像拉取权限"）。
//
// 先看本地：自己 build 出来的镜像根本不在任何 registry 里，
// 去问 registry 只会得到一个假的"未授权"，把使用者引向 docker login 这条死路。
// 本地没有才去问 registry。
func (c *Compose) CheckImage(ctx context.Context, image string) error {
	if _, err := c.exec(ctx, "image", "inspect", image); err == nil {
		return nil
	}

	out, err := c.exec(ctx, "manifest", "inspect", image)
	if err == nil {
		return nil
	}
	return imageError(image, string(out), err)
}

// imageError 把引擎的输出翻译成一条能指出下一步的错误。
//
// 不一律建议 docker login：网络不通时那条建议只会把人引向错误的方向。
func imageError(image, output string, cause error) error {
	text := strings.ToLower(output)
	switch {
	// 网络类要先判：`no such host` 是 DNS 查不到，与"镜像不存在"完全是两回事，
	// 但两句话里都有 "no such"，顺序反了就会把 DNS 故障说成镜像名写错了
	case containsAny(text, "dial tcp", "no such host", "connection refused",
		"i/o timeout", "timeout exceeded", "certificate"):
		return clierr.New(clierr.CodeNetworkUnreachable, i18n.T(msgid.EngineRegistryUnreachable)).
			WithDetail(i18n.T(msgid.LabelImage), image).
			WithDetail(i18n.T(msgid.LabelReason), tail(output, 2)).
			WithHint(i18n.T(msgid.EngineHintCheckNetworkRegistry)).
			WithCause(cause)

	case containsAny(text, "unauthorized", "authentication required", "denied", "forbidden"):
		return clierr.New(clierr.CodeImageUnauthorized, i18n.T(msgid.EngineImageUnauthorized)).
			WithDetail(i18n.T(msgid.LabelImage), image).
			WithHint(
				i18n.T(msgid.EngineHintDockerLogin),
				i18n.T(msgid.EngineHintCheckPullPermission),
			).
			WithCause(cause)

	case containsAny(text, "manifest unknown", "no such image", "not found",
		"repository does not exist"):
		return clierr.New(clierr.CodeImageUnauthorized, i18n.T(msgid.EngineImageNotFound)).
			WithDetail(i18n.T(msgid.LabelImage), image).
			WithHint(
				i18n.T(msgid.EngineHintCheckImageField),
				i18n.T(msgid.EngineHintBuildLocalImage),
			).
			WithCause(cause)

	default:
		return clierr.New(clierr.CodeNetworkUnreachable, i18n.T(msgid.EngineRegistryUnreachable)).
			WithDetail(i18n.T(msgid.LabelImage), image).
			WithDetail(i18n.T(msgid.LabelReason), tail(output, 2)).
			WithHint(i18n.T(msgid.EngineHintCheckNetworkRegistry)).
			WithCause(cause)
	}
}

// CurrentContext 对 compose 没有意义：容器就起在本机上，没有"部到哪个集群"这回事。
func (c *Compose) CurrentContext(context.Context) (string, error) { return "", nil }

// projectArgs 拼出 `-p <项目>`，按需再加 `-f <文件>` 与 `--project-directory`。
//
// **项目名必须显式给。** compose 不带 `-p` 时拿当前目录名当项目名，
// 而我们的文件固定放在 .brickkit/generated/ 下——那样同一台机器上
// 所有 BrickKit 项目在引擎眼里都叫 "generated"，彼此的容器互相顶替。
//
// file 为空时不带 `-f`：只有 `up` 需要那份文件（要照着它创建容器），
// `ps` / `down` 从容器标签就认得出项目。projectDir 同理——它是为了让 compose
// 去项目根找 `.env` 做插值，而没有 `-f` 就没有文件要插值（见 UpRequest.ProjectDir）。
func (c *Compose) projectArgs(file, project, projectDir string) []string {
	args := append([]string{}, c.base...)
	if projectDir != "" {
		args = append(args, "--project-directory", projectDir)
	}
	if project != "" {
		args = append(args, "-p", project)
	}
	if file != "" {
		args = append(args, "-f", file)
	}
	return args
}

func (c *Compose) exec(ctx context.Context, args ...string) ([]byte, error) {
	out, err := c.runner(ctx, c.bin, args...)
	if err == nil {
		return out, nil
	}
	if isMissingBinary(err) {
		return out, clierr.New(clierr.CodeEngineMissing, i18n.T(msgid.EngineBinaryMissing, c.bin)).
			WithHint(installHint(c.bin)).
			WithCause(err)
	}

	failure := clierr.New(clierr.CodeEngineFailed, i18n.T(msgid.EngineExecFailed, c.bin)).
		WithDetail(i18n.T(msgid.LabelCommand), c.bin+" "+strings.Join(args, " ")).
		WithDetail(i18n.T(msgid.LabelOutput), tail(string(out), 3)).
		WithCause(err)
	if strings.Contains(string(out), "kill network process: permission denied") {
		failure = failure.WithHint(i18n.T(msgid.EnginePodmanDownBlockedByAppArmor))
	}
	return out, failure
}

// installHint 按缺失的二进制给出对应的安装建议——Podman 引擎缺 podman 时
// 不能还建议装 Docker，那是完全不同的两条路。
func installHint(bin string) string {
	if bin == "podman" {
		return i18n.T(msgid.EngineHintInstallPodman)
	}
	return i18n.T(msgid.EngineHintInstallDocker)
}

// run 执行一条命令：**成功时只返回 stdout，失败时把 stderr 也带上**。
//
// 两个流不能无条件合并。有些工具在**成功路径**上也往 stderr 写东西，
// 混进来就会毁掉后续解析：
//
//	podman compose  每次打一行 "Executing external compose provider ..." 横幅
//	kubectl         弃用警告
//
// 真撞到过（P27）：容器起来了、也 healthy，而 `ps --format json` 的输出变成
// "横幅 + JSON"，解析失败，**一次成功的部署被报成了失败**。
//
// 失败时则相反，必须带上 stderr——错误信息几乎总在那里，
// 丢了它使用者只会看到一句没有内容的"执行失败"。
func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return append(stderr.Bytes(), stdout.Bytes()...), err
	}
	return stdout.Bytes(), nil
}

func containsAny(text string, patterns ...string) bool {
	for _, pattern := range patterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

func isMissingBinary(err error) bool {
	return err != nil && strings.Contains(err.Error(), "executable file not found")
}

// tail 取输出里最后 n 行**有内容**的文字，用 " / " 连起来。
//
// compose 的输出是一长串进度行（Creating / Created / Started …），
// **真正的原因在最后一行**。取开头只会得到 "Network xxx Creating" 这种
// 毫无信息量的句子，而"迁移容器 exit 1"那行被丢掉——真跑起来第一次就撞上了。
func tail(text string, n int) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " / ")
}

// psEntry 是 `compose ps --format json` 的一条记录。
//
// 只取需要的字段：compose 各版本输出的字段集合不同，全量映射会很脆。
type psEntry struct {
	Service  string `json:"Service"`
	State    string `json:"State"`
	Health   string `json:"Health"`
	ExitCode int    `json:"ExitCode"`
	// Publishers 在不同版本里既可能是**对象数组**也可能是一整串描述文字。
	// 当初只按字符串映射，第一次真跑就在这里解析失败——所以这里收原始
	// JSON，两种形状都认（CLI 不该因为使用者装的 compose 版本不同就瞎掉）。
	Publishers json.RawMessage `json:"Publishers"`
	Ports      string          `json:"Ports"`
}

// publisher 是一条端口映射。
type publisher struct {
	URL           string `json:"URL"`
	TargetPort    int    `json:"TargetPort"`
	PublishedPort int    `json:"PublishedPort"`
	Protocol      string `json:"Protocol"`
}

// portsOf 得出该容器的端口描述。
func portsOf(e psEntry) string {
	if e.Ports != "" {
		return e.Ports
	}

	raw := bytes.TrimSpace(e.Publishers)
	switch {
	case len(raw) == 0 || string(raw) == "null":
		return ""
	case raw[0] == '"':
		var text string
		_ = json.Unmarshal(raw, &text)
		return text
	default:
		var items []publisher
		if err := json.Unmarshal(raw, &items); err != nil {
			return ""
		}
		return describePublishers(items)
	}
}

// describePublishers 把端口映射渲染成 `0.0.0.0:18080->8080/tcp`。
//
// 只列真正映射到宿主机的（PublishedPort > 0）：容器内部端口对使用者
// 没有意义，列出来只会让人以为那个端口在宿主机上能访问。
func describePublishers(items []publisher) string {
	var parts []string
	for _, p := range items {
		if p.PublishedPort == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%d->%d/%s",
			p.URL, p.PublishedPort, p.TargetPort, p.Protocol))
	}
	return strings.Join(parts, ", ")
}

// parsePS 解析 ps 的输出。
//
// compose 有两种格式：整段一个 JSON 数组，或每行一个 JSON 对象（较新的版本）。
// 两种都要认——CLI 不该因为使用者装的 compose 版本不同就瞎掉。
func parsePS(out []byte) ([]Status, error) {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return nil, nil
	}

	var entries []psEntry
	if strings.HasPrefix(text, "[") {
		if err := json.Unmarshal([]byte(text), &entries); err != nil {
			return nil, statusParseError(err)
		}
	} else {
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || !strings.HasPrefix(line, "{") {
				continue
			}
			var entry psEntry
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				return nil, statusParseError(err)
			}
			entries = append(entries, entry)
		}
	}

	out2 := make([]Status, 0, len(entries))
	for _, e := range entries {
		out2 = append(out2, Status{
			Service: e.Service, State: e.State, Health: e.Health,
			Ports: portsOf(e), ExitCode: e.ExitCode,
		})
	}
	return out2, nil
}

func statusParseError(err error) error {
	return clierr.New(clierr.CodeEngineFailed, i18n.T(msgid.EngineStatusParseFailed)).
		WithDetail(i18n.T(msgid.LabelReason), err.Error()).
		WithHint(i18n.T(msgid.EngineHintComposeV2)).
		WithCause(err)
}

// Detect 挑选可用的容器引擎（005 §7.4）。
//
// 没有显式配置时只在 Docker/Podman 之间按 PATH 挑：Docker 优先，只有 Podman
// 也不会把它悄悄当默认——选中哪个引擎必须来自配置，不能来自"猜"（这条线
// resolveEngineFor 的 target: podman 分支也在守）。只有 Podman 时如实提示
// 怎么显式启用它，而不是把它当"找不到引擎"那样笼统报错。
//
// 只有真正要启动时才该调用它；只生成文件不需要引擎。
func Detect() (Engine, error) {
	if _, err := exec.LookPath("docker"); err == nil {
		return NewDocker(), nil
	}
	if _, err := exec.LookPath("podman"); err == nil {
		return nil, podmanNotEnabled()
	}
	return nil, clierr.New(clierr.CodeEngineMissing, i18n.T(msgid.EngineNoneFound)).
		WithDetail(i18n.T(msgid.EngineLabelTried), "docker compose").
		WithHint(
			i18n.T(msgid.EngineHintInstallDocker),
			i18n.T(msgid.EngineHintDryRunOnly),
		)
}

// podmanNotEnabled 在没有显式选择 podman、但机器上只装了 podman 时如实说明现状。
//
// 与"没找到引擎"分开报，是因为下一步不同：前者装个 Docker（或者显式选
// podman）就好；这里则是"你其实可以用它，只是还没告诉配置去用"。也不能因为
// 只有 Podman 就悄悄拿它当默认引擎——那等于让平台替使用者做了一次没人
// 要求过的选择。
func podmanNotEnabled() error {
	return clierr.New(clierr.CodeEngineMissing, i18n.T(msgid.EnginePodmanNotEnabled)).
		WithDetail(i18n.T(msgid.EngineLabelDetected), i18n.T(msgid.EnginePodmanDetectedDetail)).
		WithHint(
			i18n.T(msgid.EngineHintEnablePodman),
			i18n.T(msgid.EngineHintDryRunNoEngine),
		)
}

// ProjectName 是引擎侧的项目名：brickkit-<项目名>。
//
// 与网络名（brickkit-<项目名>-net，005 §5）同源，容器名因此也带上项目前缀，
// 一眼能看出某个容器属于哪个 BrickKit 项目。
func ProjectName(project string) string {
	project = strings.TrimSpace(project)
	if project == "" {
		return "brickkit"
	}
	return fmt.Sprintf("brickkit-%s", project)
}
