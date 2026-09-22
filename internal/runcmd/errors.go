package runcmd

import (
	"fmt"
	"strings"
)

// 本文件的 Error() 文字是给开发者看的英文；面向用户的文字由调用方（CLI 层）按
// 这些结构化字段用消息目录生成，才能跟着 BRICKKIT_LANG 走。

// Reason 说明"认得这种语言、却给不出启动命令"的原因，是给调用方映射成消息用的稳定标识。
type Reason string

const (
	// ReasonMarkerMissing：用户指定了 language，但目录里没有这种语言的标记文件。
	ReasonMarkerMissing Reason = "marker-missing"
	// ReasonUnreadableManifest：标记文件在，却读不了或解析不了（package.json 不是合法 JSON 等）。
	ReasonUnreadableManifest Reason = "unreadable-manifest"
	// ReasonNoEntryPoint：找不到可执行入口（Go 没有 main 包、.NET 只有解决方案文件、Rust 是虚拟工作区）。
	ReasonNoEntryPoint Reason = "no-entry-point"
	// ReasonMultipleEntryPoints：有多个可执行入口，选哪个是用户的事（Go 的多个 cmd/*、.NET 的多个项目文件）。
	ReasonMultipleEntryPoints Reason = "multiple-entry-points"
	// ReasonNoStartScript：package.json 里既没有 start 也没有 dev 脚本。
	ReasonNoStartScript Reason = "no-start-script"
	// ReasonConflictingPackageManagers：同时有不同包管理器的锁文件，又没有 packageManager 字段裁决。
	ReasonConflictingPackageManagers Reason = "conflicting-package-managers"
	// ReasonUnsupportedPackageManager：packageManager 字段写的不是 npm / pnpm / yarn。
	ReasonUnsupportedPackageManager Reason = "unsupported-package-manager"
	// ReasonNotSpringBoot：Java 项目没有 Spring Boot 的构建插件，没有通用的"运行"命令可用。
	ReasonNotSpringBoot Reason = "not-spring-boot"
	// ReasonMultiModule：Maven / Gradle 多模块工程，在根目录跑不出"那一个"应用。
	ReasonMultiModule Reason = "multi-module"
	// ReasonConflictingBuildTools：Maven 与 Gradle 的构建文件同时存在。
	ReasonConflictingBuildTools Reason = "conflicting-build-tools"
)

// Problem 是某一种语言"认得出、但给不出命令"的一条记录。
type Problem struct {
	Language string
	Reason   Reason
	// Detail 是出问题的文件、目录或字段（"package.json"、"cmd"、"bun@1.1.0"……），没有就为空。
	Detail string
	// Options 是候选项（多个 cmd/*、冲突的锁文件……），没有则为空。
	Options []string
}

func (p Problem) String() string {
	s := p.Language + ": " + string(p.Reason)
	if p.Detail != "" {
		s += " (" + p.Detail + ")"
	}
	if len(p.Options) > 0 {
		s += " [" + strings.Join(p.Options, ", ") + "]"
	}
	return s
}

// NoCommandError：没有任何一种语言给出了启动命令。Problems 列出"认得出但给不出"的语言，
// 为空表示目录里根本没有认得的标记文件。两种情况调用方给出的提示相同：手写 local.runCommand。
type NoCommandError struct {
	Dir      string
	Problems []Problem
}

func (e *NoCommandError) Error() string {
	if len(e.Problems) == 0 {
		return fmt.Sprintf("runcmd: no start command for %s: no supported language recognised (%s)",
			e.Dir, strings.Join(Languages(), ", "))
	}
	parts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		parts[i] = p.String()
	}
	return fmt.Sprintf("runcmd: no start command for %s: %s", e.Dir, strings.Join(parts, "; "))
}

// Candidate 是歧义报错里的一个候选。
type Candidate struct {
	Language string
	Argv     []string
}

// AmbiguousError：不止一种语言给出了可运行的命令。调用方应让用户在 local.language 里选一个。
type AmbiguousError struct {
	Candidates []Candidate
}

func (e *AmbiguousError) Error() string {
	parts := make([]string, len(e.Candidates))
	for i, c := range e.Candidates {
		parts[i] = c.Language + " (" + strings.Join(c.Argv, " ") + ")"
	}
	return "runcmd: more than one language can start this component: " + strings.Join(parts, ", ")
}

// UnknownLanguageError：Hints.Language 不在 Languages() 里。
type UnknownLanguageError struct {
	Language string
}

func (e *UnknownLanguageError) Error() string {
	return fmt.Sprintf("runcmd: unknown language %q (supported: %s)", e.Language, strings.Join(Languages(), ", "))
}

// ProgramMissingError：命令的可执行文件不存在（通常是这台机器没装对应的工具链）。
type ProgramMissingError struct {
	Program string
}

func (e *ProgramMissingError) Error() string {
	return fmt.Sprintf("runcmd: program %q not found", e.Program)
}
