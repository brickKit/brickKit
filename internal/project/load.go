package project

import (
	"errors"
	"io/fs"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/projfile"
)

// DeploySource 说明本次用的是哪一份部署文件。
type DeploySource int

const (
	// DeployTeam：deploy.yaml。
	DeployTeam DeploySource = iota
	// DeployLocal：本地模式开启，用 deploy.local.yaml。
	DeployLocal
	// DeployExplicit：-f / --file 显式指定，本地模式被忽略。
	DeployExplicit
)

// LoadOptions 是装载时的命令行选择。
type LoadOptions struct {
	// DeployFile 是 -f 的值（相对项目根或绝对路径）；空表示按默认规则选择。
	DeployFile string
	// NoLocal 让本次忽略本地模式（--no-local）。
	NoLocal bool
	// ForceLocal 不管本地模式开没开，都按个人文件读 deploy.local.yaml（lint 检查它用）。
	ForceLocal bool
}

// Project 是装载完成、跨文件一致的三层项目。
type Project struct {
	Layout       Layout
	Decl         *projfile.File
	Deploy       *deployfile.File
	DeploySource DeploySource
	DeployPath   string
	// Vars 是 config/vars.yaml；DeployVars 是部署文件的 vars:（同名时优先）。
	Vars       map[string]configdir.Value
	DeployVars map[string]configdir.Value
	// Warnings 是装载过程中不阻断的问题，由命令决定何时打印。
	Warnings []*clierr.Error

	configs map[string]*configdir.File
	// shellOf 以 id@version 为键：被外壳承载的成员版本 → 外壳 ID。
	shellOf      map[string]string
	ignoreShells bool
}

// Load 装载 root 下的项目，并执行全部跨文件校验。任何一条不满足都大声失败。
func Load(root string, opts LoadOptions) (*Project, error) {
	p, err := LoadTopology(root, opts)
	if err != nil {
		return nil, err
	}
	if err := p.loadConfig(); err != nil {
		return nil, err
	}
	return p, nil
}

// LoadTopology 装载两份文件并做拓扑相关的跨文件校验，不读 config/。
//
// 给只回答"现在什么在跑、谁属于谁"的命令用（status）：config/ 里一个未定义的
// $var: 或一处残留的冲突标记，不该让人连项目状态都看不了。拿到的项目给不出配置值。
func LoadTopology(root string, opts LoadOptions) (*Project, error) {
	p, err := LoadFiles(root, opts)
	if err != nil {
		return nil, err
	}
	if err := p.checkTopology(); err != nil {
		return nil, err
	}
	return p, nil
}

// LoadFiles 只解析 brickkit.yaml 与选中的部署文件，不做任何跨文件校验。
//
// 给 down 用：它交给引擎的只有项目名、部署目标与 k8s 设置。手工往 brickkit.yaml
// 加了一行而部署文件还没跟上、config/ 里有笔误——这些都不该挡住"把项目停下来"。
// 拿到的项目不保证两份文件一致，只能读项目级的设置。
func LoadFiles(root string, opts LoadOptions) (*Project, error) {
	l := NewLayout(root)
	decl, err := projfile.ParseFile(l.DeclPath())
	if err != nil {
		return nil, err
	}

	path, source, err := selectDeploy(l, opts)
	if err != nil {
		return nil, err
	}
	role := deployfile.RoleTeam
	if source == DeployLocal {
		role = deployfile.RoleLocal
	}
	deploy, warnings, err := deployfile.ParseFile(path, role)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, deployMissingError(path, source)
	}
	if err != nil {
		return nil, err
	}
	return &Project{
		Layout: l, Decl: decl, Deploy: deploy,
		DeploySource: source, DeployPath: path, Warnings: warnings,
	}, nil
}

// Assemble 用已经解析好的声明与部署文件组装项目，只做拓扑相关的跨文件校验，
// 不读 config/。
//
// 给"文件不在工作区"的场合用：预提交钩子判断的是 git 索引里即将提交的那两份文件，
// 而 config/ 在索引里是什么样无从谈起——拿工作区的 config/ 去配索引里的声明，
// 只会报出一堆与这次提交无关的错。拿到的项目回答得了"谁会启动"，给不出配置值。
func Assemble(l Layout, decl *projfile.File, deploy *deployfile.File) (*Project, error) {
	p := &Project{Layout: l, Decl: decl, Deploy: deploy, DeploySource: DeployTeam, DeployPath: deploy.Source}
	if err := p.checkTopology(); err != nil {
		return nil, err
	}
	return p, nil
}

// checkTopology 是与 config/ 无关的那部分跨文件校验。
func (p *Project) checkTopology() error {
	for _, step := range []func() error{p.checkCoverage, p.checkHostPorts, p.checkMembers} {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// selectDeploy 决定读哪一份部署文件：-f > 本地模式 > deploy.yaml（提案 §6.2、§11.6）。
func selectDeploy(l Layout, opts LoadOptions) (string, DeploySource, error) {
	if opts.DeployFile != "" {
		return l.Resolve(opts.DeployFile), DeployExplicit, nil
	}
	if opts.ForceLocal {
		return l.DeployLocalPath(), DeployLocal, nil
	}
	if !opts.NoLocal {
		on, err := LocalModeOn(l)
		if err != nil {
			return "", 0, err
		}
		if on {
			return l.DeployLocalPath(), DeployLocal, nil
		}
	}
	return l.DeployPath(), DeployTeam, nil
}

func deployMissingError(path string, source DeploySource) *clierr.Error {
	switch source {
	case DeployLocal:
		return clierr.New(clierr.CodeProjectMissing, i18n.T(msgid.ProjectLocalFileMissing)).
			WithDetail(i18n.T(msgid.LabelPath), path).
			WithHint(i18n.T(msgid.ProjectHintLocalRefresh), i18n.T(msgid.ProjectHintLocalOff))
	case DeployExplicit:
		return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.ProjectDeployFileNotFound)).
			WithDetail(i18n.T(msgid.LabelPath), path).
			WithExit(clierr.ExitUsage).WithHint(i18n.T(msgid.ProjectHintDeployFilePath))
	default:
		return clierr.New(clierr.CodeProjectMissing, i18n.T(msgid.ProjectDeployMissing)).
			WithDetail(i18n.T(msgid.LabelPath), path).
			WithHint(i18n.T(msgid.ProjectDeployMissingHint))
	}
}

func refKey(id, version string) string { return id + "@" + version }

// Config 返回组件某个版本的配置文件；没有时为 nil。
func (p *Project) Config(id, version string) *configdir.File { return p.configs[refKey(id, version)] }

// DeployEntry 返回覆盖该组件版本的部署条目（一致性校验保证一定存在）。
func (p *Project) DeployEntry(id, version string) deployfile.Entry {
	c, _ := p.Deploy.Entry(id, version, p.Decl.IsDefault(id, version))
	return c
}

// MembersOf 返回外壳条目下面的成员条目（外壳只有一个版本，条目是裸 ID 或 id@该版本）。
func (p *Project) MembersOf(shellID string) []deployfile.Entry {
	for _, c := range p.Deploy.Components {
		if id, _ := c.Key(); id == shellID {
			return c.Members
		}
	}
	return nil
}

// ShellOf 返回某个成员版本所属的外壳：只有被外壳承载的那个版本返回 true（同 ID 的其他版本
// 独立部署）。IgnoreShells 之后一律返回 false。
func (p *Project) ShellOf(memberID, version string) (string, bool) {
	if p.ignoreShells {
		return "", false
	}
	shell, ok := p.shellOf[refKey(memberID, version)]
	return shell, ok
}

// IgnoreShells 让本次运行把每个组件都当独立部署（--ignore-shells）：只改内存，
// 不动任何文件；下游只通过 ShellOf 认成员关系，关掉这一处就够。
func (p *Project) IgnoreShells() { p.ignoreShells = true }

// ConfigInput 组装 configdir.Resolve 的输入；schema 来自组件 Manifest（装载器不读 Manifest）。
func (p *Project) ConfigInput(id, version string, schema *manifest.ConfigSchema) configdir.Input {
	return configdir.Input{
		ComponentID: id, Version: version, Schema: schema,
		File: p.Config(id, version), Vars: p.Vars, DeployVars: p.DeployVars,
	}
}
