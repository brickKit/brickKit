package runcmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pkgJSON(scripts, extra string) string {
	if extra != "" {
		extra = ", " + extra
	}
	return `{"name": "x", "scripts": {` + scripts + `}` + extra + `}`
}

func TestNodeRunsTheStartScriptWithNpmByDefault(t *testing.T) {
	dir := write(t, map[string]string{"package.json": pkgJSON(`"start": "node server.js"`, "")})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, LangNode, cmd.Language)
	assert.Equal(t, []string{"npm", "run", "start"}, cmd.Argv)
	assert.Equal(t, []string{"package.json scripts.start"}, cmd.Evidence)
}

func TestNodeStartBeatsDev(t *testing.T) {
	dir := write(t, map[string]string{"package.json": pkgJSON(`"dev": "nodemon .", "start": "node ."`, "")})

	assert.Equal(t, []string{"npm", "run", "start"}, mustDetect(t, dir, Hints{}).Argv)
}

func TestNodeFallsBackToDevWhenThereIsNoStart(t *testing.T) {
	dir := write(t, map[string]string{"package.json": pkgJSON(`"dev": "nodemon ."`, "")})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, []string{"npm", "run", "dev"}, cmd.Argv)
	assert.Equal(t, []string{"package.json scripts.dev"}, cmd.Evidence)
}

func TestNodeABlankStartScriptDoesNotCount(t *testing.T) {
	dir := write(t, map[string]string{"package.json": pkgJSON(`"start": "  ", "dev": "vite"`, "")})

	assert.Equal(t, []string{"npm", "run", "dev"}, mustDetect(t, dir, Hints{}).Argv)
}

func TestNodeWithoutStartOrDevHasNoStartScript(t *testing.T) {
	for name, content := range map[string]string{
		"only lint":    pkgJSON(`"lint": "eslint ."`, ""),
		"no scripts":   `{"name": "x"}`,
		"empty object": `{}`,
	} {
		dir := write(t, map[string]string{"package.json": content})

		assert.Equal(t, []Problem{{Language: "node", Reason: ReasonNoStartScript, Detail: "package.json"}},
			problemsOf(t, dir, Hints{}), name)
	}
}

func TestNodeAPackageJSONThatIsNotValidIsReported(t *testing.T) {
	for name, content := range map[string]string{
		"not json":           "{ nope",
		"scripts not object": `{"scripts": ["start"]}`,
		"script not string":  `{"scripts": {"start": 1}}`,
	} {
		dir := write(t, map[string]string{"package.json": content})

		assert.Equal(t, []Problem{{Language: "node", Reason: ReasonUnreadableManifest, Detail: "package.json"}},
			problemsOf(t, dir, Hints{}), name)
	}
}

func TestNodePicksThePackageManagerFromTheLockfile(t *testing.T) {
	for lockfile, manager := range map[string]string{
		"pnpm-lock.yaml":      "pnpm",
		"yarn.lock":           "yarn",
		"package-lock.json":   "npm",
		"npm-shrinkwrap.json": "npm",
	} {
		dir := write(t, map[string]string{"package.json": pkgJSON(`"start": "node ."`, ""), lockfile: ""})

		cmd := mustDetect(t, dir, Hints{})

		assert.Equal(t, []string{manager, "run", "start"}, cmd.Argv, lockfile)
		assert.Equal(t, []string{"package.json scripts.start", lockfile}, cmd.Evidence, lockfile)
	}
}

func TestNodeTwoLockfilesOfTheSameManagerAreNotAConflict(t *testing.T) {
	dir := write(t, map[string]string{
		"package.json":      pkgJSON(`"start": "node ."`, ""),
		"package-lock.json": "", "npm-shrinkwrap.json": "",
	})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, []string{"npm", "run", "start"}, cmd.Argv)
	assert.Equal(t, []string{"package.json scripts.start", "package-lock.json", "npm-shrinkwrap.json"}, cmd.Evidence)
}

func TestNodeLockfilesOfDifferentManagersConflict(t *testing.T) {
	dir := write(t, map[string]string{
		"package.json":   pkgJSON(`"start": "node ."`, ""),
		"pnpm-lock.yaml": "", "yarn.lock": "",
	})

	assert.Equal(t, []Problem{{
		Language: "node", Reason: ReasonConflictingPackageManagers, Detail: "package.json",
		Options: []string{"pnpm-lock.yaml", "yarn.lock"},
	}}, problemsOf(t, dir, Hints{}))
}

func TestNodeThePackageManagerFieldOutranksLockfiles(t *testing.T) {
	dir := write(t, map[string]string{
		"package.json":   pkgJSON(`"start": "node ."`, `"packageManager": "pnpm@9.1.0"`),
		"pnpm-lock.yaml": "", "yarn.lock": "", // 冲突的锁文件也不再要紧
	})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, []string{"pnpm", "run", "start"}, cmd.Argv)
	assert.Equal(t, []string{"package.json scripts.start", "package.json packageManager"}, cmd.Evidence)
}

func TestNodeThePackageManagerFieldMayCarryAHash(t *testing.T) {
	dir := write(t, map[string]string{
		"package.json": pkgJSON(`"start": "node ."`, `"packageManager": "yarn@4.1.0+sha256.abc123"`),
	})

	assert.Equal(t, []string{"yarn", "run", "start"}, mustDetect(t, dir, Hints{}).Argv)
}

func TestNodeAnUnsupportedPackageManagerIsReported(t *testing.T) {
	dir := write(t, map[string]string{
		"package.json": pkgJSON(`"start": "node ."`, `"packageManager": "bun@1.1.0"`),
	})

	assert.Equal(t, []Problem{{Language: "node", Reason: ReasonUnsupportedPackageManager, Detail: "bun@1.1.0"}},
		problemsOf(t, dir, Hints{}))
}

func TestNodeIsTheSameOnWindows(t *testing.T) {
	dir := write(t, map[string]string{"package.json": pkgJSON(`"start": "node ."`, ""), "pnpm-lock.yaml": ""})

	cmd, err := detectAs(dir, "windows", Hints{})

	require.NoError(t, err)
	assert.Equal(t, []string{"pnpm", "run", "start"}, cmd.Argv, "pnpm 在 Windows 上由 PATHEXT 解析成 pnpm.cmd，命令本身不变")
}
