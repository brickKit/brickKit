package runcmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGoRunsTheRootPackageWhenItIsMain(t *testing.T) {
	dir := write(t, map[string]string{"go.mod": "module x\n", "main.go": mainGo})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, []string{"go", "run", "."}, cmd.Argv)
	assert.Equal(t, []string{"go.mod", "package main"}, cmd.Evidence)
}

func TestGoFallsBackToTheOnlyMainPackageUnderCmd(t *testing.T) {
	dir := write(t, map[string]string{
		"go.mod":             "module x\n",
		"lib.go":             libGo,
		"cmd/api/main.go":    mainGo,
		"cmd/tools/lib.go":   libGo, // cmd/ 下不是 main 包的目录不算候选
		"internal/x/main.go": mainGo,
	})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, []string{"go", "run", "./cmd/api"}, cmd.Argv)
	assert.Equal(t, []string{"go.mod", "./cmd/api"}, cmd.Evidence)
}

func TestGoWithSeveralMainPackagesLeavesTheChoiceToTheUser(t *testing.T) {
	dir := write(t, map[string]string{
		"go.mod":          "module x\n",
		"cmd/worker/m.go": mainGo,
		"cmd/api/main.go": mainGo,
	})

	problems := problemsOf(t, dir, Hints{})

	assert.Equal(t, []Problem{{
		Language: "go", Reason: ReasonMultipleEntryPoints, Detail: "cmd",
		Options: []string{"./cmd/api", "./cmd/worker"},
	}}, problems, "候选按名字排序，结果稳定")
}

func TestGoWithoutAnyMainPackageHasNoEntryPoint(t *testing.T) {
	dir := write(t, map[string]string{"go.mod": "module x\n", "lib.go": libGo})

	assert.Equal(t, []Problem{{Language: "go", Reason: ReasonNoEntryPoint, Detail: "go.mod"}},
		problemsOf(t, dir, Hints{}))
}

func TestGoTheRootMainPackageWinsOverCmd(t *testing.T) {
	dir := write(t, map[string]string{
		"go.mod": "module x\n", "main.go": mainGo,
		"cmd/a/main.go": mainGo, "cmd/b/main.go": mainGo,
	})

	assert.Equal(t, []string{"go", "run", "."}, mustDetect(t, dir, Hints{}).Argv)
}

func TestGoIgnoresGoFilesThatCannotBeRun(t *testing.T) {
	// 只有测试文件的 main 包、以及 //go:build ignore 的脚本，都不是能 go run 的入口。
	dir := write(t, map[string]string{
		"go.mod":       "module x\n",
		"main_test.go": mainGo,
		"gen.go":       "//go:build ignore\n\npackage main\n\nfunc main() {}\n",
	})

	assert.Equal(t, []Problem{{Language: "go", Reason: ReasonNoEntryPoint, Detail: "go.mod"}},
		problemsOf(t, dir, Hints{}))
}

func TestGoWithoutAGoModIsNotGo(t *testing.T) {
	dir := write(t, map[string]string{"main.go": mainGo})

	assert.Empty(t, problemsOf(t, dir, Hints{}))
}

func TestGoIsTheSameOnWindows(t *testing.T) {
	dir := write(t, map[string]string{"go.mod": "module x\n", "main.go": mainGo})

	cmd, err := detectAs(dir, "windows", Hints{})

	assert.NoError(t, err)
	assert.Equal(t, []string{"go", "run", "."}, cmd.Argv, "go 在 Windows 上由 PATHEXT 解析成 go.exe，命令本身不变")
}
