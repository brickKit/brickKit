package runcmd

import "strings"

// probeJava 只认 Spring Boot：Java 没有通用的"运行这个项目"命令，唯一有标准答案的是
// Spring Boot 的 Maven 插件（spring-boot:run）与 Gradle 插件（bootRun）。
// 信号取"构建插件在不在"而不是"依赖里有没有 spring-boot-starter"：只有依赖没有插件，
// 那两个命令根本不存在。Maven / Gradle 都优先用项目自带的 wrapper（版本由项目锁定）。
// 已知的边界：Gradle 用版本目录写插件（alias(libs.plugins.…)）时，构建文件里没有
// org.springframework.boot 这个字面量，认不出来，交给 runCommand。
func probeJava(s *scan) outcome {
	pom, hasPom, pomErr := s.read("pom.xml")
	gradleFile := ""
	for _, name := range []string{"build.gradle.kts", "build.gradle"} {
		if s.isFile(name) {
			gradleFile = name
			break
		}
	}

	switch {
	case !hasPom && gradleFile == "":
		return outcome{}
	case hasPom && gradleFile != "":
		return problemOutcome(ReasonConflictingBuildTools, "pom.xml", "pom.xml", gradleFile)
	case hasPom:
		return probeMaven(s, pom, pomErr)
	default:
		return probeGradle(s, gradleFile)
	}
}

func probeMaven(s *scan, pom string, readErr error) outcome {
	if readErr != nil {
		return problemOutcome(ReasonUnreadableManifest, "pom.xml")
	}
	// 聚合工程的插件在子模块里，先判多模块，免得把它误报成"不是 Spring Boot"。
	if strings.Contains(pom, "<modules>") {
		return problemOutcome(ReasonMultiModule, "pom.xml")
	}
	if !strings.Contains(pom, "spring-boot-maven-plugin") {
		return problemOutcome(ReasonNotSpringBoot, "pom.xml")
	}
	prefix, wrapper := s.wrapper("mvnw", "mvnw.cmd", "mvn")
	return s.ok(append(prefix, "spring-boot:run"), evidenceOf("pom.xml", wrapper)...)
}

func probeGradle(s *scan, buildFile string) outcome {
	build, _, err := s.read(buildFile)
	if err != nil {
		return problemOutcome(ReasonUnreadableManifest, buildFile)
	}
	for _, name := range []string{"settings.gradle.kts", "settings.gradle"} {
		if settings, _, _ := s.read(name); gradleIncludes(settings) {
			return problemOutcome(ReasonMultiModule, name)
		}
	}
	if !strings.Contains(build, "org.springframework.boot") {
		return problemOutcome(ReasonNotSpringBoot, buildFile)
	}
	prefix, wrapper := s.wrapper("gradlew", "gradlew.bat", "gradle")
	return s.ok(append(prefix, "bootRun"), evidenceOf(buildFile, wrapper)...)
}

// gradleIncludes 判断 settings.gradle(.kts) 里有没有 include(...) 语句（多项目工程的标志）。
// includeBuild 不算：它是复合构建，不改变根项目自己能不能 bootRun。
func gradleIncludes(settings string) bool {
	for _, line := range strings.Split(settings, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"include(", "include ", "include\t"} {
			if strings.HasPrefix(line, prefix) {
				return true
			}
		}
	}
	return false
}

func evidenceOf(files ...string) []string {
	var out []string
	for _, f := range files {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// javaEnv：Spring Boot 把环境变量 SERVER_PORT 宽松绑定到 server.port，且默认监听所有网卡，
// 所以只需要告诉它端口。
func javaEnv(s *scan) []string {
	return []string{"SERVER_PORT=" + s.port()}
}
