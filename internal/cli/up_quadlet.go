package cli

// 本文件是 podman.units: quadlet 那条路：单元交给 systemd，由它让容器一直跑着、开机按依赖顺序拉起。
//
// 与 compose 那条路共用前半段（读配置 → 解析依赖 → 级联 → 注入 → 一台机器上的部署计划），
// 从“生成什么文件、交给谁”开始分岔：
//
//	compose  一份 compose.yaml，交给 podman compose
//	Quadlet  一个目录的单元，装进 systemd 的 Quadlet 目录；迁移先单独跑完，再启动组件

import (
	"context"
	"os"
	"path/filepath"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/quadlet"
)

// quadletDirName 是 Quadlet 单元在 .brickkit/generated/ 下的子目录名。
const quadletDirName = "quadlet"

// logsQuadlet 是 logsTarget 给 Quadlet 部署的取值：日志在 journald 里。
const logsQuadlet = "quadlet"

// quadletRootful 报告单元装进的是系统的 systemd（以 root 运行）。注入的引擎说了算（测试），
// 否则看当前用户——与 engine.NewQuadlet 同一个判据。
func quadletRootful(opts *Options) bool {
	if opts != nil && opts.Engine != nil {
		if r, ok := opts.Engine.(interface{ Rootful() bool }); ok {
			return r.Rootful()
		}
	}
	return os.Geteuid() == 0
}

// upQuadlet 写出单元，装进 systemd 并启动。
func upQuadlet(ctx context.Context, opts *Options, flags upOptions, plan *upPlan) error {
	layout := plan.proj.Layout
	if err := pruneOtherTarget(layout, keepQuadlet); err != nil {
		return err
	}
	dir := filepath.Join(layout.GeneratedDir(), quadletDirName)
	if err := writeQuadletFiles(dir, plan.quadlet.Files); err != nil {
		return err
	}
	opts.Printf("%s\n", i18n.T(msgid.CliUpQuadletGenerated,
		i18n.Count(msgid.CountUnits, len(plan.quadlet.Files)), opts.display(dir)))
	// 与另外两条路同一条：在 --dry-run 的分岔之前说清会不会动数据库
	renderMigrations(opts, plan.migrations)

	if flags.dryRun {
		renderUpgradeSummary(opts, plan)
		opts.Printf("\n%s\n", i18n.T(msgid.CliUpQuadletDryRun))
		opts.Printf("%s\n", i18n.T(msgid.CliUpQuadletViewThem, opts.display(dir)))
		logging.Info(i18n.T(msgid.LogDeployFilesGenerated), "path", dir)
		return nil
	}

	// 以文件交付的配置项在 --dry-run 的分岔之后才写：这个目录挂在正在运行的容器里，一写进去组件读到的就是新值
	if err := writeSecretFiles(layout, plan.quadlet.SecretFiles); err != nil {
		return err
	}
	eng, err := resolveEngineFor(opts, plan.proj)
	if err != nil {
		return err
	}
	if err := checkUpImages(ctx, opts, eng, resolveImages(opts, plan.proj), plan.images); err != nil {
		return err
	}

	project := engine.ProjectName(plan.proj.Decl.Project)
	opts.Printf("\n%s\n", i18n.T(msgid.CliUpStarting, i18n.T(msgid.CliUpQuadletEngine)))
	var pruned []string
	if err := eng.Up(ctx, engine.UpRequest{
		Project:  project,
		Services: plan.services,
		Network:  plan.proj.Deploy.Network,
		Quadlet: quadletUnits(plan.quadlet),
		OnPrune: func(item string) { pruned = append(pruned, item) },
	}); err != nil {
		renderRemoved(opts, pruned)
		return engineFailure(i18n.T(msgid.CliUpStart), withMigrationComponent(plan, err))
	}
	renderRemoved(opts, pruned)

	statuses, err := eng.Status(ctx, project)
	if err != nil {
		// 起是起了，只是问不到状态：不该因此判定失败
		opts.Printf("%s\n", i18n.T(msgid.CliUpTheContainerStateCouldNot, clierr.As(err).Message))
		opts.Printf("%s\n", i18n.T(msgid.CliUpK8sCheckAgainWithBrickkitStatus))
		return nil
	}
	return reportStarted(opts, plan, statuses)
}

// failedUnitsCommand 列出本项目失败了的单元（迁移的容器跑完就删，失败只在 systemd 那里看得到）。
func failedUnitsCommand(project string) string {
	if quadletRootful(nil) {
		return "systemctl --failed '" + project + "_*'"
	}
	return "systemctl --user --failed '" + project + "_*'"
}

// unitsCommand 列出本项目的单元与它们的状态。
func unitsCommand(project string) string {
	if quadletRootful(nil) {
		return "systemctl list-units --all '" + project + "_*'"
	}
	return "systemctl --user list-units --all '" + project + "_*'"
}

// quadletUnits 把生成结果交给引擎的形状（engine 不依赖 quadlet 包）。
func quadletUnits(r *quadlet.Result) *engine.QuadletUnits {
	units := &engine.QuadletUnits{Migrations: r.Migrations, Components: r.Components, Images: r.Images}
	for _, f := range r.Files {
		units.Files = append(units.Files, engine.UnitFile{Name: f.Name, Content: f.Content, LinkTo: f.LinkTo})
	}
	for _, s := range r.Secrets {
		units.Secrets = append(units.Secrets, engine.Secret{Name: s.Name, Value: s.Value})
	}
	return units
}

// withMigrationComponent 给迁移失败补上是哪个组件的：引擎只认得单元名。
func withMigrationComponent(plan *upPlan, err error) error {
	e, ok := clierr.Structured(err)
	if !ok || e.Code != clierr.CodeMigrationFailed {
		return err
	}
	byUnit := map[string]string{}
	for _, ref := range plan.states.Running() {
		byUnit[quadlet.MigrationUnit(plan.proj.Decl.Project, manifest.ServiceName(ref.ID, ref.Version))] = ref.String()
	}
	for _, d := range e.Details {
		if component, found := byUnit[d.Value]; found {
			e = e.WithDetail(i18n.T(msgid.LabelComponent), component)
		}
	}
	return e
}

// renderRemoved 如实汇报卸下了什么：不再生成的单元，或者换了部署形式之后另一种留下的（compose 的容器、Quadlet 的单元）。
func renderRemoved(opts *Options, pruned []string) {
	if len(pruned) == 0 {
		return
	}
	opts.Printf("\n%s\n", i18n.T(msgid.CliUpQuadletRemoved, i18n.Count(msgid.CountItems, len(pruned))))
	for _, item := range pruned {
		opts.Printf("   - %s\n", item)
	}
}

// writeQuadletFiles 把单元写进生成目录：整个目录按这次重写（不留上一次的单元），副本的实例是指向模板的链接，
// 与装进 systemd 的一模一样——打开这个目录看到的就是在跑的。
func writeQuadletFiles(dir string, files []quadlet.File) error {
	if err := os.RemoveAll(dir); err != nil {
		return quadletWriteError(dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return quadletWriteError(dir, err)
	}
	for _, f := range files {
		path := filepath.Join(dir, f.Name)
		if f.LinkTo != "" {
			if err := os.Symlink(f.LinkTo, path); err != nil {
				return quadletWriteError(dir, err)
			}
			continue
		}
		if err := os.WriteFile(path, f.Content, 0o644); err != nil {
			return quadletWriteError(dir, err)
		}
	}
	return nil
}

func quadletWriteError(dir string, err error) error {
	return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpErrorFailedToCreateThe)).
		WithDetail(i18n.T(msgid.LabelPath), dir).
		WithDetail(i18n.T(msgid.LabelReason), err.Error()).
		WithHint(i18n.T(msgid.CliUpQuadletCheckPermissions)).
		WithCause(err)
}
