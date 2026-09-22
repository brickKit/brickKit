package runcmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectUsesTheRealOperatingSystem(t *testing.T) {
	dir := write(t, map[string]string{"go.mod": "module x\n", "main.go": mainGo})

	cmd, err := Detect(dir, Hints{}, Params{Port: testPort})

	require.NoError(t, err)
	assert.Equal(t, []string{"go", "run", "."}, cmd.Argv)
	assert.Equal(t, dir, cmd.Dir)
}

// ---- 手写的 runCommand：不探测，原样使用 ----

func TestAHandWrittenRunCommandIsUsedAsIs(t *testing.T) {
	// 目录里即便有 go.mod，也不会去探测。
	dir := write(t, map[string]string{"go.mod": "module x\n", "main.go": mainGo})

	cmd := mustDetect(t, dir, Hints{RunCommand: []string{"go", "run", "./cmd/server", "--flag"}})

	assert.Equal(t, []string{"go", "run", "./cmd/server", "--flag"}, cmd.Argv)
	assert.Equal(t, dir, cmd.Dir)
	assert.False(t, cmd.Detected, "手写的命令不是探测出来的")
	assert.Empty(t, cmd.Evidence)
	assert.Empty(t, cmd.Language)
	assert.Empty(t, cmd.Env)
}

func TestARelativeProgramInARunCommandIsResolvedAgainstTheComponentDirectory(t *testing.T) {
	dir := write(t, map[string]string{"scripts/dev.sh": "#!/bin/sh\n"})

	cmd := mustDetect(t, dir, Hints{RunCommand: []string{"./scripts/dev.sh", "./keep-me-relative"}})

	assert.Equal(t, []string{filepath.Join(dir, "scripts", "dev.sh"), "./keep-me-relative"}, cmd.Argv,
		"只有程序本身落成绝对路径，参数原样保留")
}

func TestABareProgramInARunCommandIsLeftForPathLookup(t *testing.T) {
	cmd := mustDetect(t, t.TempDir(), Hints{RunCommand: []string{"make", "serve"}})

	assert.Equal(t, []string{"make", "serve"}, cmd.Argv)
}

func TestAnAbsoluteProgramInARunCommandIsNotTouched(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "bin", "server")

	cmd := mustDetect(t, t.TempDir(), Hints{RunCommand: []string{abs}})

	assert.Equal(t, []string{abs}, cmd.Argv)
}

func TestTheRunCommandSliceIsCopied(t *testing.T) {
	hint := []string{"./run.sh"}
	cmd := mustDetect(t, t.TempDir(), Hints{RunCommand: hint})

	assert.Equal(t, "./run.sh", hint[0], "调用方的切片不能被改写")
	assert.NotEqual(t, "./run.sh", cmd.Argv[0])
}

func TestThePortMustBeAValidPort(t *testing.T) {
	dir := t.TempDir()
	for _, port := range []int{0, -1, 65536} {
		_, err := detect(dir, Hints{}, Params{Port: port}, "linux")
		require.Error(t, err, "port=%d", port)
		assert.Contains(t, err.Error(), "out of range")
	}
	_, err := detect(dir, Hints{RunCommand: []string{"x"}}, Params{Port: 65535}, "linux")
	assert.NoError(t, err)
}

func TestTheDirectoryMustExistAndBeADirectory(t *testing.T) {
	_, err := detectAs(filepath.Join(t.TempDir(), "missing"), "linux", Hints{})
	require.ErrorIs(t, err, os.ErrNotExist)

	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	_, err = detectAs(file, "linux", Hints{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestADirectoryThatCannotBeReadIsAnErrorNotAPileOfMisleadingProblems(t *testing.T) {
	if os.Geteuid() == 0 || filepath.Separator == '\\' {
		t.Skip("root 与 Windows 下 chmod 000 挡不住读取")
	}
	dir := write(t, map[string]string{"package.json": "{}", "Cargo.toml": "", "pom.xml": ""})
	require.NoError(t, os.Chmod(dir, 0))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := detectAs(dir, "linux", Hints{})

	require.ErrorIs(t, err, os.ErrPermission, "报的是目录读不了，而不是每种语言各报一条清单文件读不了")
	var nce *NoCommandError
	assert.NotErrorAs(t, err, &nce)
}

func TestARelativeDirectoryBecomesAbsolute(t *testing.T) {
	dir := write(t, map[string]string{"go.mod": "module x\n", "main.go": mainGo})
	wd, err := os.Getwd()
	require.NoError(t, err)
	rel, err := filepath.Rel(wd, dir)
	if err != nil {
		t.Skip("临时目录与当前目录不在同一个盘上，算不出相对路径")
	}

	cmd := mustDetect(t, rel, Hints{})

	assert.Equal(t, dir, cmd.Dir)
}

// ---- 多种语言的取舍 ----

func TestNothingRecognisedIsAnErrorThatSaysSo(t *testing.T) {
	dir := write(t, map[string]string{"README.md": "hi"})

	_, err := detectAs(dir, "linux", Hints{})

	var nce *NoCommandError
	require.ErrorAs(t, err, &nce)
	assert.Equal(t, dir, nce.Dir)
	assert.Empty(t, nce.Problems)
	assert.Contains(t, err.Error(), "no supported language recognised")
}

func TestAnExplicitLanguageWithoutItsMarkerFileSaysWhichFileIsMissing(t *testing.T) {
	dir := write(t, map[string]string{"package.json": `{"scripts":{"start":"x"}}`})

	problems := problemsOf(t, dir, Hints{Language: LangGo})

	assert.Equal(t, []Problem{{Language: "go", Reason: ReasonMarkerMissing, Detail: "go.mod"}}, problems)
}

func TestTheDetectedCommandCarriesTheDirectoryEvidenceAndLanguage(t *testing.T) {
	dir := write(t, map[string]string{"go.mod": "module x\n", "main.go": mainGo})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, Command{
		Language: "go",
		Argv:     []string{"go", "run", "."},
		Dir:      dir,
		Evidence: []string{"go.mod", "package main"},
		Detected: true,
	}, cmd)
}

// ---- 错误文字 ----

func TestErrorTextsNameTheirSubject(t *testing.T) {
	problem := Problem{Language: "node", Reason: ReasonConflictingPackageManagers, Detail: "package.json",
		Options: []string{"pnpm-lock.yaml", "yarn.lock"}}
	assert.Equal(t, "node: conflicting-package-managers (package.json) [pnpm-lock.yaml, yarn.lock]", problem.String())
	assert.Equal(t, "go: no-entry-point", Problem{Language: "go", Reason: ReasonNoEntryPoint}.String())

	nce := &NoCommandError{Dir: "/x", Problems: []Problem{problem}}
	assert.Contains(t, nce.Error(), "/x")
	assert.Contains(t, nce.Error(), "conflicting-package-managers")

	assert.Contains(t, (&ProgramMissingError{Program: "mvn"}).Error(), `"mvn"`)
}

// ---- CheckProgram ----

func TestCheckProgramFindsABareProgramOnPath(t *testing.T) {
	name := "sh"
	if runtime.GOOS == "windows" {
		name = "cmd"
	}

	assert.NoError(t, Command{Argv: []string{name}}.CheckProgram())
}

func TestCheckProgramNamesAMissingBareProgram(t *testing.T) {
	err := Command{Argv: []string{"definitely-not-installed-xyz"}}.CheckProgram()

	var missing *ProgramMissingError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, "definitely-not-installed-xyz", missing.Program)
}

func TestCheckProgramChecksAnAbsolutePathDirectly(t *testing.T) {
	dir := write(t, map[string]string{"run.sh": "#!/bin/sh\n"})

	assert.NoError(t, Command{Argv: []string{filepath.Join(dir, "run.sh")}}.CheckProgram())

	var missing *ProgramMissingError
	assert.ErrorAs(t, Command{Argv: []string{filepath.Join(dir, "gone.sh")}}.CheckProgram(), &missing)
	assert.ErrorAs(t, Command{Argv: []string{dir}}.CheckProgram(), &missing, "目录不是程序")
}

func TestCheckProgramRejectsAnEmptyCommand(t *testing.T) {
	var missing *ProgramMissingError
	assert.ErrorAs(t, Command{}.CheckProgram(), &missing)
}

func TestTwoLanguagesThatCanBothRunAreAmbiguous(t *testing.T) {
	dir := write(t, map[string]string{
		"go.mod": "module x\n", "main.go": mainGo,
		"package.json": `{"scripts":{"start":"node server.js"}}`,
	})

	_, err := detectAs(dir, "linux", Hints{})

	var amb *AmbiguousError
	require.ErrorAs(t, err, &amb)
	assert.Equal(t, []Candidate{
		{Language: "go", Argv: []string{"go", "run", "."}},
		{Language: "node", Argv: []string{"npm", "run", "start"}},
	}, amb.Candidates)
	assert.Contains(t, err.Error(), "go (go run .), node (npm run start)")
}

func TestAnExplicitLanguageResolvesTheAmbiguity(t *testing.T) {
	dir := write(t, map[string]string{
		"go.mod": "module x\n", "main.go": mainGo,
		"package.json": `{"scripts":{"start":"node server.js"}}`,
	})

	cmd := mustDetect(t, dir, Hints{Language: LangNode})

	assert.Equal(t, LangNode, cmd.Language)
	assert.Equal(t, []string{"npm", "run", "start"}, cmd.Argv)
}

func TestALanguageThatCannotRunDoesNotBlockOneThatCan(t *testing.T) {
	// 很常见的形状：Go 仓库里放了一个只有 lint 脚本的 package.json。
	dir := write(t, map[string]string{
		"go.mod": "module x\n", "main.go": mainGo,
		"package.json": `{"scripts":{"lint":"eslint ."}}`,
	})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, LangGo, cmd.Language)
}

func TestSeveralRecognisedButUnrunnableLanguagesAreAllReported(t *testing.T) {
	dir := write(t, map[string]string{
		"go.mod":       "module x\n",
		"package.json": `{"scripts":{"lint":"eslint ."}}`,
	})

	problems := problemsOf(t, dir, Hints{})

	assert.Equal(t, []Problem{
		{Language: "go", Reason: ReasonNoEntryPoint, Detail: "go.mod"},
		{Language: "node", Reason: ReasonNoStartScript, Detail: "package.json"},
	}, problems)
}

func TestLanguagesAreCompleteAndInAFixedOrder(t *testing.T) {
	assert.Equal(t, []string{"go", "rust", "dotnet", "node", "java", "python", "ruby"}, Languages())
}

func TestALanguageGivenWithARunCommandStillContributesItsEnvironment(t *testing.T) {
	dir := t.TempDir()

	python := mustDetect(t, dir, Hints{Language: LangPython, RunCommand: []string{"uvicorn", "app:app"}})
	assert.Equal(t, LangPython, python.Language)
	assert.Equal(t, []string{"PYTHONUNBUFFERED=1"}, python.Env)

	java := mustDetect(t, dir, Hints{Language: LangJava, RunCommand: []string{"java", "-jar", "app.jar"}})
	assert.Equal(t, []string{"SERVER_PORT=18080"}, java.Env)

	goCmd := mustDetect(t, dir, Hints{Language: LangGo, RunCommand: []string{"air"}})
	assert.Equal(t, LangGo, goCmd.Language)
	assert.Empty(t, goCmd.Env, "Go 没有额外的环境变量")
}

// ---- 语言与参数的校验 ----

func TestAnUnknownLanguageIsRejected(t *testing.T) {
	dir := t.TempDir()

	_, err := detectAs(dir, "linux", Hints{Language: "cobol"})
	var unknown *UnknownLanguageError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, "cobol", unknown.Language)
	assert.Contains(t, err.Error(), "cobol")
	assert.Contains(t, err.Error(), "ruby", "报错里列出所有支持的语言")

	_, err = detectAs(dir, "linux", Hints{Language: "cobol", RunCommand: []string{"x"}})
	assert.ErrorAs(t, err, &unknown, "手写了 runCommand 也要校验 language，别让拼错的值悄悄溜过去")
}
