package runcmd

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const djangoManage = "#!/usr/bin/env python\nos.environ.setdefault('DJANGO_SETTINGS_MODULE', 'site.settings')\n"

// ---- Python（只认 Django） ----

func TestPythonRunsDjangoOnAllInterfaces(t *testing.T) {
	dir := write(t, map[string]string{"manage.py": djangoManage})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, LangPython, cmd.Language)
	assert.Equal(t, []string{"python3", "manage.py", "runserver", "0.0.0.0:18080"}, cmd.Argv)
	assert.Equal(t, []string{"manage.py"}, cmd.Evidence)
	assert.Equal(t, []string{"PYTHONUNBUFFERED=1"}, cmd.Env, "输出走管道，Python 会整块缓冲")
}

func TestPythonPrefersTheProjectsVirtualEnvironment(t *testing.T) {
	for _, venv := range []string{".venv", "venv"} {
		dir := write(t, map[string]string{"manage.py": djangoManage, venv + "/bin/python": ""})

		cmd := mustDetect(t, dir, Hints{})

		assert.Equal(t, filepath.Join(dir, venv, "bin", "python"), cmd.Argv[0], venv)
		assert.Equal(t, []string{"manage.py", venv + "/bin/python"}, cmd.Evidence, venv)
	}
}

func TestPythonDotVenvBeatsVenv(t *testing.T) {
	dir := write(t, map[string]string{"manage.py": djangoManage, ".venv/bin/python": "", "venv/bin/python": ""})

	assert.Equal(t, filepath.Join(dir, ".venv", "bin", "python"), mustDetect(t, dir, Hints{}).Argv[0])
}

func TestPythonOnWindowsUsesScriptsAndPlainPython(t *testing.T) {
	plain := write(t, map[string]string{"manage.py": djangoManage})
	cmd, err := detectAs(plain, "windows", Hints{})
	require.NoError(t, err)
	assert.Equal(t, []string{"python", "manage.py", "runserver", "0.0.0.0:18080"}, cmd.Argv)

	withVenv := write(t, map[string]string{"manage.py": djangoManage, ".venv/Scripts/python.exe": ""})
	cmd, err = detectAs(withVenv, "windows", Hints{})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(withVenv, ".venv", "Scripts", "python.exe"), cmd.Argv[0])
	assert.Equal(t, []string{"manage.py", ".venv/Scripts/python.exe"}, cmd.Evidence)

	// Unix 的 .venv/bin/python 在 Windows 上不算。
	unixVenv := write(t, map[string]string{"manage.py": djangoManage, ".venv/bin/python": ""})
	cmd, err = detectAs(unixVenv, "windows", Hints{})
	require.NoError(t, err)
	assert.Equal(t, "python", cmd.Argv[0])
}

func TestPythonAManagePyThatIsNotDjangoIsNotGuessedAt(t *testing.T) {
	dir := write(t, map[string]string{"manage.py": "# flask-script manager\nprint('hi')\n"})

	assert.Empty(t, problemsOf(t, dir, Hints{}), "认不准就不认：交给用户手写 runCommand")
}

func TestPythonTheOtherPopularEntryPointsAreDeliberatelyNotRecognised(t *testing.T) {
	// FastAPI / Flask / 纯脚本：入口是用户自己起的名字，没有约定可循。
	dir := write(t, map[string]string{
		"requirements.txt": "fastapi\nuvicorn\n",
		"app/main.py":      "app = object()\n",
		"pyproject.toml":   "[project]\nname = 'x'\n",
	})

	assert.Empty(t, problemsOf(t, dir, Hints{}))
}

// ---- Ruby（只认 Rails） ----

func TestRubyRunsRailsOnAllInterfaces(t *testing.T) {
	dir := write(t, map[string]string{"bin/rails": "#!/usr/bin/env ruby\n", "Gemfile": "source 'https://rubygems.org'\n"})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, LangRuby, cmd.Language)
	assert.Equal(t, []string{"ruby", "bin/rails", "server", "-b", "0.0.0.0", "-p", "18080"}, cmd.Argv)
	assert.Equal(t, []string{"bin/rails", "Gemfile"}, cmd.Evidence)
	assert.Empty(t, cmd.Env)
}

func TestRubyNeedsBothTheBinstubAndTheGemfile(t *testing.T) {
	onlyBinstub := write(t, map[string]string{"bin/rails": ""})
	assert.Empty(t, problemsOf(t, onlyBinstub, Hints{}))

	onlyGemfile := write(t, map[string]string{"Gemfile": ""})
	assert.Empty(t, problemsOf(t, onlyGemfile, Hints{}))
}

func TestRubyIsTheSameOnWindows(t *testing.T) {
	dir := write(t, map[string]string{"bin/rails": "", "Gemfile": ""})

	cmd, err := detectAs(dir, "windows", Hints{})

	require.NoError(t, err)
	assert.Equal(t, "ruby", cmd.Argv[0], "binstub 靠 shebang，Windows 上没有，所以显式交给 ruby")
}
