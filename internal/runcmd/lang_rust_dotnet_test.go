package runcmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- Rust ----

func TestRustRunsCargo(t *testing.T) {
	dir := write(t, map[string]string{"Cargo.toml": "[package]\nname = \"x\"\n"})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, LangRust, cmd.Language)
	assert.Equal(t, []string{"cargo", "run"}, cmd.Argv)
	assert.Equal(t, []string{"Cargo.toml"}, cmd.Evidence)
}

func TestRustAWorkspaceRootThatIsAlsoAPackageStillRuns(t *testing.T) {
	dir := write(t, map[string]string{"Cargo.toml": "[package]\nname = \"x\"\n\n[workspace]\nmembers = [\"y\"]\n"})

	assert.Equal(t, []string{"cargo", "run"}, mustDetect(t, dir, Hints{}).Argv)
}

func TestRustAVirtualWorkspaceHasNoEntryPoint(t *testing.T) {
	// 表头里有空白、后面跟着注释，照样认得出来。
	dir := write(t, map[string]string{"Cargo.toml": "[ workspace ]  # members below\nmembers = [\"y\"]\n"})

	assert.Equal(t, []Problem{{Language: "rust", Reason: ReasonNoEntryPoint, Detail: "Cargo.toml"}},
		problemsOf(t, dir, Hints{}))
}

func TestRustACommentedOutPackageTableDoesNotCount(t *testing.T) {
	dir := write(t, map[string]string{"Cargo.toml": "# [package]\n[workspace]\n"})

	assert.Len(t, problemsOf(t, dir, Hints{}), 1)
}

func TestRustAnUnreadableManifestIsReported(t *testing.T) {
	if os.Geteuid() == 0 || filepath.Separator == '\\' {
		t.Skip("root 与 Windows 下 chmod 000 挡不住读取")
	}
	dir := write(t, map[string]string{"Cargo.toml": "[package]\n"})
	require.NoError(t, os.Chmod(filepath.Join(dir, "Cargo.toml"), 0))

	assert.Equal(t, []Problem{{Language: "rust", Reason: ReasonUnreadableManifest, Detail: "Cargo.toml"}},
		problemsOf(t, dir, Hints{}))
}

func TestRustIsTheSameOnWindows(t *testing.T) {
	dir := write(t, map[string]string{"Cargo.toml": "[package]\n"})

	cmd, err := detectAs(dir, "windows", Hints{})

	require.NoError(t, err)
	assert.Equal(t, []string{"cargo", "run"}, cmd.Argv)
}

// ---- .NET ----

func TestDotnetRunsTheOnlyProjectFile(t *testing.T) {
	for _, project := range []string{"App.csproj", "App.fsproj", "App.vbproj"} {
		dir := write(t, map[string]string{project: "<Project />"})

		cmd := mustDetect(t, dir, Hints{})

		assert.Equal(t, LangDotnet, cmd.Language, project)
		assert.Equal(t, []string{"dotnet", "run"}, cmd.Argv, project)
		assert.Equal(t, []string{project}, cmd.Evidence, project)
	}
}

func TestDotnetASolutionFileNextToTheProjectDoesNotGetInTheWay(t *testing.T) {
	dir := write(t, map[string]string{"App.sln": "", "App.csproj": "<Project />"})

	assert.Equal(t, []string{"dotnet", "run"}, mustDetect(t, dir, Hints{}).Argv)
}

func TestDotnetSeveralProjectFilesLeaveTheChoiceToTheUser(t *testing.T) {
	dir := write(t, map[string]string{"B.csproj": "", "A.csproj": "", "C.fsproj": ""})

	assert.Equal(t, []Problem{{
		Language: "dotnet", Reason: ReasonMultipleEntryPoints,
		Options: []string{"A.csproj", "B.csproj", "C.fsproj"},
	}}, problemsOf(t, dir, Hints{}))
}

func TestDotnetASolutionWithoutARootProjectHasNoEntryPoint(t *testing.T) {
	dir := write(t, map[string]string{"All.sln": "", "src/Api/Api.csproj": "<Project />"})

	assert.Equal(t, []Problem{{Language: "dotnet", Reason: ReasonNoEntryPoint, Detail: "All.sln"}},
		problemsOf(t, dir, Hints{}))
}

func TestDotnetANewStyleSolutionFileCountsToo(t *testing.T) {
	dir := write(t, map[string]string{"All.slnx": ""})

	assert.Len(t, problemsOf(t, dir, Hints{}), 1)
}

func TestDotnetWithNothingIsNotDotnet(t *testing.T) {
	dir := write(t, map[string]string{"notes.txt": ""})

	assert.Empty(t, problemsOf(t, dir, Hints{}))
}

func TestDotnetTheExtensionMatchIsCaseInsensitive(t *testing.T) {
	dir := write(t, map[string]string{"App.CSPROJ": "<Project />"})

	assert.Equal(t, []string{"dotnet", "run"}, mustDetect(t, dir, Hints{}).Argv)
}

func TestDotnetIsTheSameOnWindows(t *testing.T) {
	dir := write(t, map[string]string{"App.csproj": "<Project />"})

	cmd, err := detectAs(dir, "windows", Hints{})

	require.NoError(t, err)
	assert.Equal(t, []string{"dotnet", "run"}, cmd.Argv)
}
