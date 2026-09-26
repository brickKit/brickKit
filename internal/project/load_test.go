package project_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
)

const baseDecl = `project: shop
components:
  - {id: erp/shell, version: 1.0.0, kind: shell}
  - {id: erp/backend, version: 2.0.0}
  - {id: people/basic, version: 1.0.0}
  - {id: people/basic, version: 2.0.0}
`

const baseDeploy = `target: docker
vars:
  DB_PASSWORD: deploy-pwd
components:
  - id: erp/shell
    members: [erp/backend]
  - id: erp/backend
  - id: people/basic
`

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
}

func baseProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, map[string]string{
		"brickkit.yaml":                  baseDecl,
		"deploy.yaml":                    baseDeploy,
		"config/vars.yaml":               "ERP_DB_HOST: pg.internal\nDB_PASSWORD: vars-pwd\n",
		"config/erp-backend.yaml":        "DB_HOST: $var:ERP_DB_HOST\nDB_PASSWORD: $var:DB_PASSWORD\n",
		"config/people-basic@1.0.0.yaml": "LOG_LEVEL: debug\n",
		"config/people-basic@2.0.0.yaml": "LOG_LEVEL: warn\n",
	})
	return root
}

func detailValues(err error) []string {
	var out []string
	for _, d := range clierr.As(err).Details {
		out = append(out, d.Value)
	}
	return out
}

func detailKeys(err error) []string {
	var out []string
	for _, d := range clierr.As(err).Details {
		out = append(out, d.Key)
	}
	return out
}

func TestLoadHappyPath(t *testing.T) {
	p, err := project.Load(baseProject(t), project.LoadOptions{})
	require.NoError(t, err)
	assert.Equal(t, project.DeployTeam, p.DeploySource)
	assert.Empty(t, p.Warnings)

	shell, ok := p.ShellOf("erp/backend", "2.0.0")
	require.True(t, ok)
	assert.Equal(t, "erp/shell", shell)
	_, ok = p.ShellOf("people/basic", "1.0.0")
	assert.False(t, ok)

	level, _ := p.Config("people/basic", "2.0.0").Lookup("LOG_LEVEL")
	assert.Equal(t, "warn", level.Text)
	assert.Nil(t, p.Config("erp/shell", "1.0.0"))
	assert.Equal(t, "people/basic", p.DeployEntry("people/basic", "1.0.0").ID)

	schema := &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"DB_HOST": {Type: "string"}, "DB_PASSWORD": {Type: "string", Secret: true},
	}}
	res, err := configdir.Resolve(p.ConfigInput("erp/backend", "2.0.0", schema))
	require.NoError(t, err)
	host, _ := res.Get("DB_HOST")
	pwd, _ := res.Get("DB_PASSWORD")
	assert.Equal(t, "pg.internal", host.Value.Text)
	assert.Equal(t, "deploy-pwd", pwd.Value.Text, "the deploy file's vars: win over config/vars.yaml")
}

func TestLoadDeploySelection(t *testing.T) {
	root := baseProject(t)
	write(t, root, map[string]string{
		// 改掉原有条目而不是再加一条 erp/backend@2.0.0：那样裸 ID 条目就什么也不覆盖，会被判为多余
		"deploy.local.yaml": strings.Replace(
			strings.Replace(baseDeploy, "target: docker", "target: podman", 1),
			"  - id: erp/backend\n", "  - id: erp/backend\n    mode: debug\n    localPort: 9000\n", 1),
		"deploy.prod.yaml": strings.Replace(baseDeploy, "target: docker", "target: k8s", 1),
	})
	l := project.NewLayout(root)
	require.NoError(t, project.SetLocalMode(l, true))

	p, err := project.Load(root, project.LoadOptions{})
	require.NoError(t, err)
	assert.Equal(t, project.DeployLocal, p.DeploySource)
	assert.Equal(t, "podman", p.Deploy.Target)
	assert.Equal(t, "debug", p.DeployEntry("erp/backend", "2.0.0").Mode)
	// erp/backend 是 erp/shell 的成员：成员必须可以设 debug / local（附录 A18），装载不得拒绝

	p, err = project.Load(root, project.LoadOptions{NoLocal: true})
	require.NoError(t, err)
	assert.Equal(t, project.DeployTeam, p.DeploySource)

	p, err = project.Load(root, project.LoadOptions{DeployFile: "deploy.prod.yaml"})
	require.NoError(t, err)
	assert.Equal(t, project.DeployExplicit, p.DeploySource)
	assert.Equal(t, "k8s", p.Deploy.Target)

	_, err = project.Load(root, project.LoadOptions{DeployFile: "missing.yaml"})
	require.Error(t, err)
	assert.Equal(t, clierr.CodeInvalidArgument, clierr.As(err).Code)
}

func TestLoadMissingDeployFiles(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"brickkit.yaml": baseDecl})
	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	assert.Equal(t, i18n.T(msgid.ProjectDeployMissing), clierr.As(err).Message)

	require.NoError(t, project.SetLocalMode(project.NewLayout(root), true))
	_, err = project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	assert.Equal(t, i18n.T(msgid.ProjectLocalFileMissing), clierr.As(err).Message)
}

func TestLoadLocalFileStale(t *testing.T) {
	root := baseProject(t)
	// 本地文件是按旧的 deploy.yaml 生成的；之后团队加了 crm/backend
	write(t, root, map[string]string{
		"deploy.local.yaml": baseDeploy,
		"brickkit.yaml":     baseDecl + "  - {id: crm/backend, version: 1.0.0}\n",
		"deploy.yaml":       baseDeploy + "  - id: crm/backend\n",
	})
	require.NoError(t, project.SetLocalMode(project.NewLayout(root), true))

	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeDeployInconsistent, e.Code)
	assert.Equal(t, i18n.T(msgid.ProjectLocalStale), e.Message)
	assert.Contains(t, detailValues(err), "crm/backend")
	require.NotEmpty(t, e.Hints)
	assert.Equal(t, i18n.T(msgid.ProjectHintLocalRefresh), e.Hints[0])
}

func TestLoadDeployExtraAndMissing(t *testing.T) {
	root := baseProject(t)
	write(t, root, map[string]string{"deploy.yaml": `target: docker
components:
  - {id: erp/shell, members: [erp/backend]}
  - id: erp/backend
  - id: old/thing
`})
	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeDeployInconsistent, e.Code)
	assert.Equal(t, i18n.T(msgid.ProjectDeployInconsistent, "deploy.yaml"), e.Message)
	values := detailValues(err)
	assert.Contains(t, values, "old/thing")
	assert.Contains(t, values, "people/basic@1.0.0", "several versions: missing entries name the exact version")
	assert.Contains(t, values, "people/basic@2.0.0")
}

func TestLoadBareEntryCoveringNothingIsExtra(t *testing.T) {
	root := baseProject(t)
	write(t, root, map[string]string{"deploy.yaml": baseDeploy +
		"  - id: people/basic@1.0.0\n  - id: people/basic@2.0.0\n"})
	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	assert.Contains(t, detailValues(err), "people/basic")
}

func TestLoadDebugRejectedOutsideLocalFile(t *testing.T) {
	root := baseProject(t)
	debug := baseDeploy + "  - {id: erp/backend@2.0.0, mode: debug, localPort: 9000}\n"
	write(t, root, map[string]string{"deploy.prod.yaml": debug})
	_, err := project.Load(root, project.LoadOptions{DeployFile: "deploy.prod.yaml"})
	require.Error(t, err)

	write(t, root, map[string]string{"deploy.yaml": debug})
	_, err = project.Load(root, project.LoadOptions{})
	require.Error(t, err)
}

func TestLoadMemberRules(t *testing.T) {
	cases := map[string]struct{ decl, deploy string }{
		"members on non-shell": {baseDecl, `target: docker
components:
  - id: erp/shell
  - {id: erp/backend, members: [people/basic]}
  - id: people/basic
`},
		"undeclared member": {baseDecl, `target: docker
components:
  - {id: erp/shell, members: [ghost/thing]}
  - id: erp/backend
  - id: people/basic
`},
		"multi-version member written as a bare id": {baseDecl, `target: docker
components:
  - {id: erp/shell, members: [people/basic]}
  - id: erp/backend
  - id: people/basic
`},
		"member version not declared": {baseDecl, `target: docker
components:
  - {id: erp/shell, members: [people/basic@9.9.9]}
  - id: erp/backend
  - id: people/basic
`},
		"member in two shells": {baseDecl + "  - {id: erp/shell2, version: 1.0.0, kind: shell}\n", `target: docker
components:
  - {id: erp/shell, members: [erp/backend]}
  - {id: erp/shell2, members: [erp/backend]}
  - id: erp/backend
  - id: people/basic
`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := baseProject(t)
			write(t, root, map[string]string{"brickkit.yaml": tc.decl, "deploy.yaml": tc.deploy})
			_, err := project.Load(root, project.LoadOptions{})
			require.Error(t, err)
			found := false
			for _, key := range detailKeys(err) {
				found = found || strings.Contains(key, "members")
			}
			assert.True(t, found, "error must point at a members field: %v", detailKeys(err))
		})
	}
}

func TestLoadConfigAmbiguousAcrossVersions(t *testing.T) {
	root := baseProject(t)
	require.NoError(t, os.Remove(filepath.Join(root, "config", "people-basic@2.0.0.yaml")))
	write(t, root, map[string]string{"config/people-basic.yaml": "LOG_LEVEL: info\n"})

	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	assert.Equal(t, i18n.T(msgid.ProjectConfigAmbiguous, "people-basic.yaml", "people/basic", "1.0.0, 2.0.0"),
		clierr.As(err).Message)
}

func TestLoadConfigOrphansWarn(t *testing.T) {
	root := baseProject(t)
	write(t, root, map[string]string{
		"config/gone-thing.yaml":           "A: 1\n",
		"config/erp-backend@9.9.9.yaml":    "A: 1\n",
		"config/people-basic.yaml":         "A: 1\n", // 两个版本都有专属文件，无版本文件没人用
		"config/.archive/erp-backend.yaml": "A: 1\n",
		"config/README.md":                 "notes\n",
	})
	p, err := project.Load(root, project.LoadOptions{})
	require.NoError(t, err)
	assert.Len(t, p.Warnings, 3)
	unused := 0
	for _, w := range p.Warnings {
		if w.Message == i18n.T(msgid.ProjectConfigUnversionedUnused, "config/people-basic.yaml", "people/basic", "people-basic") {
			unused++
		}
	}
	assert.Equal(t, 1, unused)
}

func TestLoadUndefinedVarRef(t *testing.T) {
	root := baseProject(t)
	write(t, root, map[string]string{"config/erp-backend.yaml": "DB_HOST: $var:NOPE\n"})
	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	assert.Equal(t, i18n.T(msgid.ProjectVarUndefined), clierr.As(err).Message)
	joined := strings.Join(detailValues(err), "\n")
	assert.Contains(t, joined, "$var:NOPE")
	assert.Contains(t, joined, "erp-backend.yaml")
}

func TestLoadConflictsAcrossFilesReportedTogether(t *testing.T) {
	root := baseProject(t)
	write(t, root, map[string]string{
		"config/people-basic@1.0.0.yaml": "A: 1\nA: 2\n",
		"config/people-basic@2.0.0.yaml": "B: 1\nB: 2\n",
	})
	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigConflict, clierr.As(err).Code)
	joined := strings.Join(detailValues(err), "\n")
	assert.Contains(t, joined, "people-basic@1.0.0.yaml")
	assert.Contains(t, joined, "people-basic@2.0.0.yaml")
}

func TestLoadConfigNameCollision(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"brickkit.yaml": "project: p\ncomponents:\n  - {id: a-b/c, version: 1.0.0}\n  - {id: a/b-c, version: 1.0.0}\n" +
			"  - {id: x-y/z, version: 1.0.0}\n  - {id: x/y-z, version: 1.0.0}\n",
		"deploy.yaml": "target: docker\ncomponents:\n  - id: a-b/c\n  - id: a/b-c\n  - id: x-y/z\n  - id: x/y-z\n",
	})
	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	assert.Equal(t, i18n.T(msgid.ProjectConfigNameCollisions), clierr.As(err).Message)
	joined := strings.Join(detailValues(err), "\n")
	assert.Contains(t, joined, "a-b-c.yaml")
	assert.Contains(t, joined, "x-y-z.yaml", "every collision is reported, not only the first")
}

func TestLoadUndefinedVarLocalModeHint(t *testing.T) {
	root := baseProject(t)
	team := strings.Replace(baseDeploy, "vars:\n", "vars:\n  NEWVAR: x\n", 1)
	write(t, root, map[string]string{
		"deploy.yaml":             team,
		"deploy.local.yaml":       baseDeploy,
		"config/erp-backend.yaml": "DB_HOST: $var:NEWVAR\n",
	})
	require.NoError(t, project.SetLocalMode(project.NewLayout(root), true))
	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, i18n.T(msgid.ProjectVarUndefined), e.Message)
	assert.Contains(t, e.Hints, i18n.T(msgid.ProjectHintVarOnlyInTeamDeploy, "NEWVAR"))
}

func TestLoadVarsAndComponentConflictsTogether(t *testing.T) {
	root := baseProject(t)
	write(t, root, map[string]string{
		"config/vars.yaml":               "A: 1\nA: 2\n",
		"config/people-basic@1.0.0.yaml": "B: 1\nB: 2\n",
	})
	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigConflict, clierr.As(err).Code)
	joined := strings.Join(detailValues(err), "\n")
	assert.Contains(t, joined, "vars.yaml")
	assert.Contains(t, joined, "people-basic@1.0.0.yaml")
}

func TestLoadCrossVersionHostPortCollision(t *testing.T) {
	root := baseProject(t)
	write(t, root, map[string]string{"deploy.yaml": strings.Replace(baseDeploy,
		"  - id: people/basic\n", "  - id: people/basic\n    expose: true\n    exposePort: 8080\n", 1)})
	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodePortConflict, e.Code)
	assert.Equal(t, i18n.T(msgid.ProjectHostPortCollision, 8080), e.Message)
	joined := strings.Join(detailValues(err), "\n")
	assert.Contains(t, joined, "people/basic@1.0.0")
	assert.Contains(t, joined, "people/basic@2.0.0")
}

func TestLoadLocalPortVsExposePort(t *testing.T) {
	root := baseProject(t)
	write(t, root, map[string]string{"deploy.yaml": strings.Replace(strings.Replace(baseDeploy,
		"  - id: erp/backend\n", "  - id: erp/backend\n    mode: local\n    localPort: 9000\n", 1),
		"  - id: people/basic\n", "  - id: people/basic@1.0.0\n    expose: true\n    exposePort: 9000\n  - id: people/basic@2.0.0\n", 1)})
	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	assert.Equal(t, clierr.CodePortConflict, clierr.As(err).Code)

	// k8s 下没有宿主机端口，不查
	write(t, root, map[string]string{"deploy.yaml": strings.Replace(strings.Replace(baseDeploy, "target: docker", "target: k8s", 1),
		"  - id: people/basic\n", "  - id: people/basic\n    exposePort: 8080\n    expose: true\n    hostname: p.example.com\n", 1)})
	_, err = project.Load(root, project.LoadOptions{})
	require.NoError(t, err)
}

// 预提交钩子从 git 索引里读出两份文件来判断，config/ 在索引里是什么样无从谈起：
// Assemble 只做拓扑相关的跨文件校验，不碰 config/。
func TestAssembleChecksTopologyWithoutConfig(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"config/orphan@9.9.9.yaml": "X: 1\n"})
	decl, err := projfile.Parse([]byte(baseDecl), "index:brickkit.yaml")
	require.NoError(t, err)
	deploy, _, err := deployfile.Parse([]byte(baseDeploy), "index:deploy.yaml", deployfile.RoleTeam)
	require.NoError(t, err)

	p, err := project.Assemble(project.NewLayout(root), decl, deploy)
	require.NoError(t, err)
	shell, ok := p.ShellOf("erp/backend", "2.0.0")
	assert.True(t, ok)
	assert.Equal(t, "erp/shell", shell)

	missing, _, err := deployfile.Parse([]byte("target: docker\ncomponents:\n  - id: erp/shell\n"),
		"index:deploy.yaml", deployfile.RoleTeam)
	require.NoError(t, err)
	_, err = project.Assemble(project.NewLayout(root), decl, missing)
	require.Error(t, err, "部署文件没覆盖到的组件照样要报")
}

// 成员有两个版本时，members 写明外壳里的是哪一个：那个版本进外壳，另一个版本照常独立部署。
func TestLoadMemberVersionPinned(t *testing.T) {
	root := baseProject(t)
	write(t, root, map[string]string{"deploy.yaml": `target: docker
components:
  - {id: erp/shell, members: [erp/backend, people/basic@1.0.0]}
  - id: erp/backend
  - id: people/basic
`})
	p, err := project.Load(root, project.LoadOptions{})
	require.NoError(t, err)
	shell, ok := p.ShellOf("people/basic", "1.0.0")
	assert.True(t, ok)
	assert.Equal(t, "erp/shell", shell)
	_, ok = p.ShellOf("people/basic", "2.0.0")
	assert.False(t, ok, "2.0.0 不在外壳里")
	_, ok = p.ShellOf("erp/backend", "2.0.0")
	assert.True(t, ok, "只有一个版本的成员写裸 id 即可")
}

// 多个版本却写了裸 id：报错要提示写明版本。
func TestLoadMemberBareWithMultipleVersionsSaysWhichToWrite(t *testing.T) {
	root := baseProject(t)
	write(t, root, map[string]string{"deploy.yaml": `target: docker
components:
  - {id: erp/shell, members: [people/basic]}
  - id: erp/backend
  - id: people/basic
`})
	_, err := project.Load(root, project.LoadOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "people/basic@")
}
