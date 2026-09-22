package runcmd

import "go/build"

// probeGo：有 go.mod，且根目录是 main 包 → `go run .`；
// 根目录不是 main 包时看 cmd/*，恰好一个 main 包才用它，多个就交给用户选。
// 判断"是不是 main 包"交给标准库的 go/build（它认得构建约束、_test.go、平台后缀），不需要装 go。
func probeGo(s *scan) outcome {
	if !s.isFile("go.mod") {
		return outcome{}
	}
	if isGoCommand(s.path(".")) {
		return s.ok([]string{"go", "run", "."}, "go.mod", "package main")
	}
	var mains []string
	for _, name := range s.subdirs("cmd") {
		if isGoCommand(s.path("cmd/" + name)) {
			mains = append(mains, "./cmd/"+name)
		}
	}
	switch len(mains) {
	case 0:
		return problemOutcome(ReasonNoEntryPoint, "go.mod")
	case 1:
		return s.ok([]string{"go", "run", mains[0]}, "go.mod", mains[0])
	default:
		return problemOutcome(ReasonMultipleEntryPoints, "cmd", mains...)
	}
}

// isGoCommand 判断 dir 是不是一个能 `go run` 的 main 包：包名是 main，且至少有一个非测试的源文件。
func isGoCommand(dir string) bool {
	pkg, err := build.Default.ImportDir(dir, 0)
	return err == nil && pkg.IsCommand() && len(pkg.GoFiles)+len(pkg.CgoFiles) > 0
}
