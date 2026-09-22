package runcmd

import "strings"

// probeRust：有 Cargo.toml → `cargo run`。
// 只挡一种便宜又确定的情形：纯虚拟工作区（有 [workspace] 没有 [package]），根目录没有可运行的包。
// 一个包里有多个二进制时 cargo 自己会报 "could not determine which binary to run"，
// 那句话会随崩溃的最后几行输出一起给到用户，不必在这里重复实现一遍。
func probeRust(s *scan) outcome {
	text, present, err := s.read("Cargo.toml")
	if !present {
		return outcome{}
	}
	if err != nil {
		return problemOutcome(ReasonUnreadableManifest, "Cargo.toml")
	}
	if tomlHasTable(text, "workspace") && !tomlHasTable(text, "package") {
		return problemOutcome(ReasonNoEntryPoint, "Cargo.toml")
	}
	return s.ok([]string{"cargo", "run"}, "Cargo.toml")
}

// tomlHasTable 判断 TOML 文本里有没有 `[name]` 这个表头（容忍空白与行尾注释）。
func tomlHasTable(text, name string) bool {
	for _, line := range strings.Split(text, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		if strings.Join(strings.Fields(line), "") == "["+name+"]" {
			return true
		}
	}
	return false
}
