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

// Compose 是基于 compose 的引擎实现（Docker 与 Podman 共用）。
//
// 两者的命令行不同（`docker compose` 是子命令，`podman-compose` 是独立程序），
// 但参数与行为一致，因此只在这里分岔一次。
type Compose struct {
	name string
	// bin 与 base 一起构成命令前缀：docker + [compose] / podman-compose + []。
	bin  string
	base []string
	// runner 执行命令，测试可替换。
	runner func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewDocker 返回 docker compose 引擎。
func NewDocker() *Compose {
	return &Compose{name: Docker, bin: "docker", base: []string{"compose"}, runner: run}
}

// NewPodman 返回基于独立 podman-compose 程序的引擎。
func NewPodman() *Compose {
	return &Compose{name: Podman, bin: "podman-compose", runner: run}
}

// NewPodmanCompose 返回基于 `podman compose` 子命令的引擎。
//
// Podman 4.1 起自带这个子命令，它会去找一台机器上现成的 compose 实现
// （docker-compose 插件或 podman-compose）来执行，容器仍然跑在 Podman 上。
// 很多发行版装了 Podman 却没有 podman-compose（那是另一个 Python 程序），
// 只认后者的话，一台 compose 明明能用的机器会被判成"没有可用的容器引擎"。
func NewPodmanCompose() *Compose {
	return &Compose{name: Podman, bin: "podman", base: []string{"compose"}, runner: run}
}

func (c *Compose) Name() string { return c.name }

// Up 启动（compose up -d --wait）。
//
// `--wait` 让 compose 一直等到所有容器 running/healthy 才返回。没有它，
// `up -d` 只保证"启动命令发出去了"：依赖链末端的组件此刻多半还是
// health=starting，紧接着查状态会得到一个假的失败结论。
func (c *Compose) Up(ctx context.Context, req UpRequest) error {
	args := append(c.projectArgs(req.File, req.Project, req.ProjectDir),
		"up", "-d", "--wait", "--remove-orphans")
	args = append(args, req.Services...)

	if _, err := c.exec(ctx, args...); err != nil {
		return err
	}
	return nil
}

// Down 停止。**不带 -v**：数据卷（数据库数据）必须保留（004 §3.6）。
func (c *Compose) Down(ctx context.Context, req DownRequest) error {
	var args []string
	if len(req.Services) == 0 {
		args = append(c.projectArgs(req.File, req.Project, req.ProjectDir), "down")
	} else {
		// 只停部分组件用 stop + rm，而不是 down：down 会连网络一起拆掉，
		// 剩下还在跑的组件会瞬间失去彼此
		args = append(c.projectArgs(req.File, req.Project, req.ProjectDir), "rm", "-sf")
		args = append(args, req.Services...)
	}

	_, err := c.exec(ctx, args...)
	return err
}

// Status 返回该项目下所有 service 的状态。
func (c *Compose) Status(ctx context.Context, file, project string) ([]Status, error) {
	args := append(c.projectArgs(file, project, ""), "ps", "-a", "--format", "json")

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
func (c *Compose) projectArgs(file, project, projectDir string) []string {
	args := append([]string{}, c.base...)
	if projectDir != "" {
		args = append(args, "--project-directory", projectDir)
	}
	if project != "" {
		args = append(args, "-p", project)
	}
	return append(args, "-f", file)
}

func (c *Compose) exec(ctx context.Context, args ...string) ([]byte, error) {
	out, err := c.runner(ctx, c.bin, args...)
	if err == nil {
		return out, nil
	}
	if isMissingBinary(err) {
		return out, clierr.Newf(clierr.CodeEngineMissing, "错误：找不到容器引擎 %s", c.bin).
			WithHint("安装 Docker（20.10+）或 Podman（4.0+）后重试").
			WithCause(err)
	}
	return out, clierr.Newf(clierr.CodeEngineFailed, "错误：%s 执行失败", c.bin).
		WithDetail("命令", c.bin+" "+strings.Join(args, " ")).
		WithDetail("输出", tail(string(out), 3)).
		WithCause(err)
}

// run 是真正执行外部命令的地方。
func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.Bytes(), err
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
// 顺序：docker compose → podman-compose。两个都没有时返回错误——
// 但只有真正要启动时才该调用它；只生成文件不需要引擎。
func Detect() (Engine, error) {
	if _, err := exec.LookPath("docker"); err == nil {
		return NewDocker(), nil
	}
	if _, err := exec.LookPath("podman-compose"); err == nil {
		return NewPodman(), nil
	}
	// podman 自带的 compose 子命令：装了 Podman 但没装 podman-compose 的机器
	// 走这条路，容器照样跑在 Podman 上
	if _, err := exec.LookPath("podman"); err == nil {
		return NewPodmanCompose(), nil
	}
	return nil, clierr.New(clierr.CodeEngineMissing, "错误：没有找到可用的容器引擎").
		WithDetail("已尝试", "docker compose、podman-compose、podman compose").
		WithHint(
			"安装 Docker 20.10+（推荐）或 Podman 4.0+",
			"只想生成部署文件而不启动的话，用 brickkit up --dry-run",
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
