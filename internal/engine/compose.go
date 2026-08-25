package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
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
// 真跑出来的样子：up 起两个组件 → 给其中一个写 enabled: false → up --dry-run
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
		return clierr.Newf(clierr.CodeNetworkUnreachable, "错误：无法连接镜像仓库").
			WithDetail("镜像", image).
			WithDetail("原因", tail(output, 2)).
			WithHint("检查网络与 registry 地址").
			WithCause(cause)

	case containsAny(text, "unauthorized", "authentication required", "denied", "forbidden"):
		return clierr.Newf(clierr.CodeImageUnauthorized, "错误：镜像拉取未授权").
			WithDetail("镜像", image).
			WithHint(
				"执行 docker login <registry> 登录后重试",
				"确认该账号有拉取这个镜像的权限",
			).
			WithCause(cause)

	case containsAny(text, "manifest unknown", "no such image", "not found",
		"repository does not exist"):
		return clierr.Newf(clierr.CodeImageUnauthorized, "错误：镜像不存在").
			WithDetail("镜像", image).
			WithHint(
				"确认 component.yaml 中的 deployment.image 写对了",
				"本地开发的组件请先 build 出这个镜像",
			).
			WithCause(cause)

	default:
		return clierr.Newf(clierr.CodeNetworkUnreachable, "错误：无法连接镜像仓库").
			WithDetail("镜像", image).
			WithDetail("原因", tail(output, 2)).
			WithHint("检查网络与 registry 地址").
			WithCause(cause)
	}
}

// projectArgs 拼出 `-p <项目> -f <文件>`。
//
// 项目名必须显式给：compose 默认拿部署文件所在目录名当项目名，
// 而我们的文件固定放在 .brickkit/generated/ 下——那样同一台机器上
// 所有 BrickKit 项目在引擎眼里都叫 "generated"，彼此顶替。
// CurrentContext 对 compose 没有意义：容器就起在本机上，没有"部到哪个集群"这回事。
func (c *Compose) CurrentContext(context.Context) (string, error) { return "", nil }

// projectDir 决定 compose 去哪里找 .env（见 UpRequest.ProjectDir）。
//
// file 为空时不带 `-f`：只有 `up` 需要那份文件（要照着它创建容器），
// `ps` / `down` 从容器标签就认得出项目。
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
		return out, clierr.Newf(clierr.CodeEngineMissing, "错误：找不到容器引擎 %s", c.bin).
			WithHint("安装 Docker 20.10+ 后重试").
			WithCause(err)
	}
	return out, clierr.Newf(clierr.CodeEngineFailed, "错误：%s 执行失败", c.bin).
		WithDetail("命令", c.bin+" "+strings.Join(args, " ")).
		WithDetail("输出", tail(string(out), 3)).
		WithCause(err)
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
	return clierr.New(clierr.CodeEngineFailed, "错误：无法解析容器引擎的状态输出").
		WithDetail("原因", err.Error()).
		WithHint("确认 Docker Compose 为 V2+（brickkit version 会打印检测到的引擎）").
		WithCause(err)
}

// Detect 挑选可用的容器引擎（005 §7.4）。
//
// 目前只有 Docker 一种。只有真正要启动时才该调用它；只生成文件不需要引擎。
func Detect() (Engine, error) {
	if _, err := exec.LookPath("docker"); err == nil {
		return NewDocker(), nil
	}
	if _, err := exec.LookPath("podman"); err == nil {
		return nil, podmanNotSupported()
	}
	return nil, clierr.New(clierr.CodeEngineMissing, "错误：没有找到可用的容器引擎").
		WithDetail("已尝试", "docker compose").
		WithHint(
			"安装 Docker 20.10+",
			"只想生成部署文件而不启动的话，用 brickkit up --dry-run",
		)
}

// podmanNotSupported 在只装了 Podman 的机器上如实说明现状（005 §7）。
//
// 与"没找到引擎"分开报，是因为这两件事该做的下一步完全不同：
// 前者装个 Docker 就好，后者装了也没用——问题不在使用者的机器上。
//
// 措辞刻意具体：只说"不支持"会让人以为是没做，而真实情况是**做过、
// 跑到了一半、卡在一处我们绕不过去的地方**。把那一处说出来，
// 使用者才能自己判断他的环境会不会一样卡住。
func podmanNotSupported() error {
	return clierr.New(clierr.CodeEngineMissing, "错误：暂不支持 Podman，请使用 Docker").
		WithDetail("检测到", "本机装了 Podman，但没有 Docker").
		WithDetail("卡在哪", "`up` / `status` 都能跑通，但 `down` 会失败："+
			"rootless netns: kill network process: permission denied").
		WithDetail("为什么不留半个", "一个停不掉的项目比不支持更糟——"+
			"容器会一直占着端口和资源，而 CLI 报的是成功").
		WithHint(
			"安装 Docker 20.10+ 后重试",
			"只想生成部署文件而不启动的话，用 brickkit up --dry-run（不需要任何引擎）",
			"详见 design/005 §7",
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

// HasNetwork 查一张 Docker 网络在不在（NetworkChecker）。
//
// 命令层用它在 `up` 之前拦下"external 依赖的项目还没部署"：compose 自己
// 也会失败，但它给的是
//
//	network brickkit-platform-shared-net declared as external, but could not be found
//
// 这句话里没有"external 组件"、没有对方的项目名、也没有下一步该做什么。
// 提前查一次，才能把这三样都说出来。
func (c *Compose) HasNetwork(ctx context.Context, name string) (bool, error) {
	if _, err := c.exec(ctx, "network", "inspect", name); err != nil {
		// 查不到与查不动分不开：docker network inspect 对两者都是非零退出码。
		// 但这里只用于**生成一句更好的错误**，误判的代价是漏报一次提示，
		// 而不是错误地阻断——所以当作"不存在"即可。
		return false, nil
	}
	return true, nil
}
