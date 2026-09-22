package runcmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	springPom = `<project><build><plugins><plugin>
<artifactId>spring-boot-maven-plugin</artifactId></plugin></plugins></build></project>`
	plainPom      = `<project><dependencies></dependencies></project>`
	aggregatorPom = `<project><modules><module>api</module></modules></project>`

	springGradle = "plugins { id 'org.springframework.boot' version '3.3.0' }\n"
	springKts    = "plugins { id(\"org.springframework.boot\") version \"3.3.0\" }\n"
)

func TestJavaMavenRunsTheSpringBootPlugin(t *testing.T) {
	dir := write(t, map[string]string{"pom.xml": springPom})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, LangJava, cmd.Language)
	assert.Equal(t, []string{"mvn", "spring-boot:run"}, cmd.Argv, "没有 wrapper 就用 PATH 里的 mvn")
	assert.Equal(t, []string{"pom.xml"}, cmd.Evidence)
	assert.Equal(t, []string{"SERVER_PORT=18080"}, cmd.Env)
}

func TestJavaMavenPrefersTheWrapper(t *testing.T) {
	dir := write(t, map[string]string{"pom.xml": springPom, "mvnw": "#!/bin/sh\n"})
	makeExecutable(t, dir, "mvnw")

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, []string{filepath.Join(dir, "mvnw"), "spring-boot:run"}, cmd.Argv)
	assert.Equal(t, []string{"pom.xml", "mvnw"}, cmd.Evidence)
}

func TestJavaAWrapperThatLostItsExecutableBitIsRunThroughSh(t *testing.T) {
	// Windows 上 clone、解压 zip 都会丢可执行位；与其让用户撞上 permission denied，不如用 sh 去跑它。
	dir := write(t, map[string]string{"pom.xml": springPom, "mvnw": "#!/bin/sh\n"})
	require.NoError(t, os.Chmod(filepath.Join(dir, "mvnw"), 0o644))

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, []string{"sh", filepath.Join(dir, "mvnw"), "spring-boot:run"}, cmd.Argv)
}

func TestJavaMavenOnWindowsUsesTheCmdWrapper(t *testing.T) {
	dir := write(t, map[string]string{"pom.xml": springPom, "mvnw": "", "mvnw.cmd": ""})

	cmd, err := detectAs(dir, "windows", Hints{})

	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, "mvnw.cmd"), "spring-boot:run"}, cmd.Argv)
	assert.Equal(t, []string{"pom.xml", "mvnw.cmd"}, cmd.Evidence)
}

func TestJavaMavenOnWindowsWithOnlyTheUnixWrapperFallsBackToMvn(t *testing.T) {
	dir := write(t, map[string]string{"pom.xml": springPom, "mvnw": ""})

	cmd, err := detectAs(dir, "windows", Hints{})

	require.NoError(t, err)
	assert.Equal(t, []string{"mvn", "spring-boot:run"}, cmd.Argv)
}

func TestJavaMavenWithoutTheSpringBootPluginHasNoGenericRunCommand(t *testing.T) {
	dir := write(t, map[string]string{"pom.xml": plainPom})

	assert.Equal(t, []Problem{{Language: "java", Reason: ReasonNotSpringBoot, Detail: "pom.xml"}},
		problemsOf(t, dir, Hints{}))
}

func TestJavaAMavenAggregatorIsMultiModule(t *testing.T) {
	dir := write(t, map[string]string{"pom.xml": aggregatorPom})

	assert.Equal(t, []Problem{{Language: "java", Reason: ReasonMultiModule, Detail: "pom.xml"}},
		problemsOf(t, dir, Hints{}), "聚合工程的插件在子模块里，不能被误报成 not-spring-boot")
}

func TestJavaAnUnreadablePomIsReported(t *testing.T) {
	if os.Geteuid() == 0 || filepath.Separator == '\\' {
		t.Skip("root 与 Windows 下 chmod 000 挡不住读取")
	}
	dir := write(t, map[string]string{"pom.xml": springPom})
	require.NoError(t, os.Chmod(filepath.Join(dir, "pom.xml"), 0))

	assert.Equal(t, []Problem{{Language: "java", Reason: ReasonUnreadableManifest, Detail: "pom.xml"}},
		problemsOf(t, dir, Hints{}))
}

func TestJavaAnUnreadableGradleBuildFileIsReported(t *testing.T) {
	if os.Geteuid() == 0 || filepath.Separator == '\\' {
		t.Skip("root 与 Windows 下 chmod 000 挡不住读取")
	}
	dir := write(t, map[string]string{"build.gradle": springGradle})
	require.NoError(t, os.Chmod(filepath.Join(dir, "build.gradle"), 0))

	assert.Equal(t, []Problem{{Language: "java", Reason: ReasonUnreadableManifest, Detail: "build.gradle"}},
		problemsOf(t, dir, Hints{}))
}

func TestJavaGradleRunsBootRun(t *testing.T) {
	for file, content := range map[string]string{"build.gradle": springGradle, "build.gradle.kts": springKts} {
		dir := write(t, map[string]string{file: content})

		cmd := mustDetect(t, dir, Hints{})

		assert.Equal(t, []string{"gradle", "bootRun"}, cmd.Argv, file)
		assert.Equal(t, []string{file}, cmd.Evidence, file)
		assert.Equal(t, []string{"SERVER_PORT=18080"}, cmd.Env, file)
	}
}

func TestJavaGradlePrefersTheWrapper(t *testing.T) {
	dir := write(t, map[string]string{"build.gradle": springGradle, "gradlew": "#!/bin/sh\n", "gradlew.bat": ""})
	makeExecutable(t, dir, "gradlew")

	unix := mustDetect(t, dir, Hints{})
	assert.Equal(t, []string{filepath.Join(dir, "gradlew"), "bootRun"}, unix.Argv)
	assert.Equal(t, []string{"build.gradle", "gradlew"}, unix.Evidence)

	windows, err := detectAs(dir, "windows", Hints{})
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, "gradlew.bat"), "bootRun"}, windows.Argv)
}

func TestJavaGradleWithoutTheSpringBootPluginHasNoGenericRunCommand(t *testing.T) {
	dir := write(t, map[string]string{"build.gradle": "plugins { id 'java' }\n"})

	assert.Equal(t, []Problem{{Language: "java", Reason: ReasonNotSpringBoot, Detail: "build.gradle"}},
		problemsOf(t, dir, Hints{}))
}

func TestJavaAMultiProjectGradleBuildIsMultiModule(t *testing.T) {
	for settings, content := range map[string]string{
		"settings.gradle":     "rootProject.name = 'x'\ninclude 'api', 'worker'\n",
		"settings.gradle.kts": "rootProject.name = \"x\"\n  include(\":api\")\n",
	} {
		dir := write(t, map[string]string{"build.gradle": springGradle, settings: content})

		assert.Equal(t, []Problem{{Language: "java", Reason: ReasonMultiModule, Detail: settings}},
			problemsOf(t, dir, Hints{}), settings)
	}
}

func TestJavaGradleIncludeBuildIsNotMultiProject(t *testing.T) {
	dir := write(t, map[string]string{
		"build.gradle":    springGradle,
		"settings.gradle": "rootProject.name = 'x'\nincludeBuild('../shared')\n",
	})

	assert.Equal(t, []string{"gradle", "bootRun"}, mustDetect(t, dir, Hints{}).Argv)
}

func TestJavaBothBuildToolsAtOnceIsAConflict(t *testing.T) {
	dir := write(t, map[string]string{"pom.xml": springPom, "build.gradle.kts": springKts})

	assert.Equal(t, []Problem{{
		Language: "java", Reason: ReasonConflictingBuildTools, Detail: "pom.xml",
		Options: []string{"pom.xml", "build.gradle.kts"},
	}}, problemsOf(t, dir, Hints{}))
}

func TestJavaWithoutABuildFileIsNotJava(t *testing.T) {
	dir := write(t, map[string]string{"Main.java": "class Main {}"})

	assert.Empty(t, problemsOf(t, dir, Hints{}))
}
