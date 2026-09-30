// Package install 回答"add / remove 要对三份文件做哪些改动"。
//
// 这里只有判断，没有读写：输入是装载好的项目与解析好的依赖图，输出是一份 Plan。
// 命令层（internal/cli）负责取 Manifest、问使用者、按 Plan 改文件、改完再装载一遍核对。
// 把判断单独放在这里，多版本、外壳、requiredBy 的每条规则都能不碰文件系统地测。
package install

import (
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
)

// Line 是 brickkit.yaml 的一行组件声明。
type Line struct {
	ID, Version string
	Shell       bool
	// RequiredBy 为空就是默认版本。
	RequiredBy []string
}

// Ref 返回这一行的组件版本。
func (l Line) Ref() resolver.Ref { return resolver.Ref{ID: l.ID, Version: l.Version} }

// Entry 是部署文件的一条：ID 是条目里写的 id（默认版本写裸 ID，其余写 id@version）；
// Under 非空时嵌在那个外壳条目的 members 下面。
type Entry struct {
	ID    string
	Under string
}

// ConfigFile 是要生成的配置骨架（默认版本用无版本号文件，其余版本带版本号）。
type ConfigFile struct {
	ID, Version string
	Versioned   bool
	Schema      *manifest.ConfigSchema
}

// ConfigRef 指一个组件版本的配置文件：Versioned 为 false 时是无版本号文件（默认版本）。
type ConfigRef struct {
	ID, Version string
	Versioned   bool
}

// Rename 是一次改名（部署条目 id，或配置文件）。
type Rename struct{ From, To string }

// Plan 是一次 add / remove 要对三份文件做的全部改动。每一类改动按列出的顺序应用。
type Plan struct {
	// Moves 是这次 upgrade 的版本移动（给输出用）；ChangeVersions 是原地改版本号的那些行。
	Moves          []Move
	ChangeVersions []Move
	// ChangeKinds 改一行的 kind（组件在新版本里成了外壳，或不再是外壳）。
	ChangeKinds []Line

	AddLines []Line
	// SetRequiredBy 改已有行的 requiredBy（RequiredBy 为空表示去掉字段——默认版本转正）。
	SetRequiredBy []Line
	RemoveLines   []resolver.Ref

	// AddEntries 先加顶层条目、再加嵌套条目（外壳条目要先在）。
	AddEntries []Entry
	// NestEntries 把已有的顶层条目挪到外壳下面。
	NestEntries []Entry
	// UnnestShells 把外壳下面的成员挪回顶层（删外壳时）。
	UnnestShells []string
	// RenameEntries 改部署条目的 id（默认版本转正时 id@v → id）。
	RenameEntries []Rename
	// RemoveEntries 是要删的部署条目 id。
	RemoveEntries []string
	// LiftEntries 把嵌在外壳下面的条目挪到顶层（新外壳不再编进它）。
	LiftEntries []string

	AddConfigs     []ConfigFile
	ArchiveConfigs []ConfigRef
	// RenameConfigs 把带版本号的配置文件改成无版本号文件（默认版本转正）。
	RenameConfigs []ConfigRef
	// DemoteConfigs 把无版本号配置文件改成带版本号的（旧默认版本留作兼容版本）。
	DemoteConfigs []ConfigRef
	// MigrateConfigs 按新版本的 configSchema 迁移配置。
	MigrateConfigs []ConfigMigration

	// Added 与 Removed 是这次进出项目的组件版本（给输出用；Removed 含连带移除的）。
	Added   []resolver.Ref
	Removed []resolver.Ref
	// Promoted 是转正成默认版本的那一个（没有时为零值）。
	Promoted resolver.Ref
	// Notes 是给使用者的说明（成员为什么没进外壳、归档里有旧配置……），不阻断。
	Notes []string
}

// Empty 报告这份计划是否什么都不改。
func (p *Plan) Empty() bool {
	return len(p.ChangeVersions)+len(p.ChangeKinds)+len(p.AddLines)+len(p.SetRequiredBy)+len(p.RemoveLines)+len(p.AddEntries)+
		len(p.NestEntries)+len(p.UnnestShells)+len(p.RenameEntries)+len(p.RemoveEntries)+len(p.LiftEntries)+
		len(p.AddConfigs)+len(p.ArchiveConfigs)+len(p.RenameConfigs)+len(p.DemoteConfigs)+len(p.MigrateConfigs) == 0
}

// EntryID 是一个组件版本在部署文件里的条目 id：默认版本写裸 ID，其余写 id@version。
func EntryID(id, version string, isDefault bool) string {
	if isDefault {
		return id
	}
	return id + "@" + version
}

// hasSchema 报告组件有没有要生成骨架的配置项（没有 configSchema 的组件不生成文件）。
func hasSchema(m *manifest.Manifest) bool {
	return m != nil && m.ConfigSchema != nil && len(m.ConfigSchema.Properties) > 0
}
