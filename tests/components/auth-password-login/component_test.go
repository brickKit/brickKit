package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// 本文件校验的是"组件对平台的承诺"：Manifest 声明的产物文件真的在、
// OpenAPI 是合法的、镜像不以 root 运行。
//
// 这些都能靠人工核对，但人工核对不会在改坏的那一天自动响。

// TestComponentYamlDeclaresArtifactsThatExist：声明了什么产物，就得真有那个文件。
//
// 声明与文件对不上时，`brickkit publish` 会在**上传到一半**才失败，
// 市场里留下一个转不了 stable 的 draft 版本。
func TestComponentYamlDeclaresArtifactsThatExist(t *testing.T) {
	raw, err := os.ReadFile("component.yaml")
	if err != nil {
		t.Fatalf("读取 component.yaml 失败：%v", err)
	}
	manifest := string(raw)

	// 本组件只有 HTTP，因此只该有 openapi 一份产物；
	// 声明了 proto 却没有 gRPC 会误导调用方去生成一个根本不存在的客户端
	if strings.Contains(manifest, "protobuf") {
		t.Error("本组件没有 gRPC，不该声明 protobuf 契约")
	}
	if !strings.Contains(manifest, "openapi.json") {
		t.Fatal("component.yaml 应当声明 openapi.json")
	}
	if _, err := os.Stat("openapi.json"); err != nil {
		t.Fatalf("component.yaml 声明了 openapi.json，但文件不存在：%v", err)
	}
}

// TestComponentYamlDeclaresJWTSecret 是真实装配跑出来的一条。
//
// brickkit.yaml **没有**组件级的 env / secrets 机制：唯一能给组件传值的通道
// 是 config（映射 configSchema）。JWT_SECRET 不声明在 configSchema 里的话，
// 平台根本无法注入它——这个组件在真实项目里压根起不来，而单元测试全绿，
// 因为测试是自己 set 环境变量的。
//
// 同时它**不能有 default**：有默认值就意味着所有装了这个组件的人共用同一把
// 钥匙，任何人都能给任何一处部署签出管理员令牌。
func TestComponentYamlDeclaresJWTSecret(t *testing.T) {
	raw, err := os.ReadFile("component.yaml")
	if err != nil {
		t.Fatalf("读取 component.yaml 失败：%v", err)
	}
	manifest := string(raw)

	if !strings.Contains(manifest, "jwtSecret") {
		t.Fatal("configSchema 必须声明 jwtSecret，否则平台无法注入 JWT_SECRET")
	}

	// 截出 jwtSecret 那一段，确认它没有 default
	section := manifest[strings.Index(manifest, "jwtSecret:"):]
	if end := strings.Index(section, "\nmigration:"); end > 0 {
		section = section[:end]
	}
	if strings.Contains(section, "default:") {
		t.Error("jwtSecret 绝不能有默认值——那等于所有部署共用同一把钥匙")
	}
}

// TestComponentYamlDeclaresStrongDependency：强依赖必须写在 Manifest 里。
//
// 不声明的话平台不会注入 PEOPLE_BASIC_ENDPOINT，组件启动时会因为缺配置而失败——
// 而失败原因看上去像是"运维忘了配"，实际是组件自己没说清楚要什么。
func TestComponentYamlDeclaresStrongDependency(t *testing.T) {
	raw, err := os.ReadFile("component.yaml")
	if err != nil {
		t.Fatalf("读取 component.yaml 失败：%v", err)
	}
	manifest := string(raw)

	if !strings.Contains(manifest, "people/basic@1.0.0") {
		t.Error("应当声明对 people/basic 的强依赖")
	}
	if strings.Contains(manifest, "optional: true") {
		t.Error("people/basic 是强依赖，不该标 optional")
	}
	if !strings.Contains(manifest, "engine: postgresql") {
		t.Error("应当声明 database 资源依赖")
	}
}

// TestMigrationCommandMatchesBinaryPath：迁移命令指向的路径必须与镜像里的一致。
//
// 这一条真踩过：Dockerfile 把二进制放在 /app/x，Manifest 里写成 /x，
// 迁移容器起来就是 "no such file or directory"——而平台会把它当成迁移失败，
// 阻断整个项目启动，日志里只有一句找不到文件。
func TestMigrationCommandMatchesBinaryPath(t *testing.T) {
	manifest, err := os.ReadFile("component.yaml")
	if err != nil {
		t.Fatalf("读取 component.yaml 失败：%v", err)
	}
	dockerfile, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatalf("读取 Dockerfile 失败：%v", err)
	}

	const binaryPath = "/app/auth-password-login"
	if !strings.Contains(string(manifest), binaryPath) {
		t.Errorf("migration.command 应当指向 %s", binaryPath)
	}
	if !strings.Contains(string(dockerfile), binaryPath) {
		t.Errorf("Dockerfile 应当把二进制放在 %s", binaryPath)
	}
}

// TestOpenAPIDocumentIsValid：产物本身得是能用的。
func TestOpenAPIDocumentIsValid(t *testing.T) {
	raw, err := os.ReadFile("openapi.json")
	if err != nil {
		t.Fatalf("读取 openapi.json 失败：%v", err)
	}

	var doc struct {
		OpenAPI string                            `json:"openapi"`
		Paths   map[string]map[string]interface{} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("openapi.json 不是合法 JSON：%v", err)
	}
	if doc.OpenAPI == "" {
		t.Error("缺少 openapi 版本字段")
	}

	// 文档里必须有实现真的提供的那几个端点，否则调用方照着文档写会撞墙
	for _, path := range []string{"/healthz", "/api/v1/login", "/api/v1/verify"} {
		if _, ok := doc.Paths[path]; !ok {
			t.Errorf("openapi.json 缺少端点 %s", path)
		}
	}
}

// TestDockerfileRunsAsNonRoot 对应 002 §1.4。
func TestDockerfileRunsAsNonRoot(t *testing.T) {
	raw, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatalf("读取 Dockerfile 失败：%v", err)
	}

	content := string(raw)
	if !strings.Contains(content, "USER 10001") {
		t.Error("Dockerfile 必须以非 root 用户运行（USER 10001）")
	}
	if strings.Contains(content, "USER root") {
		t.Error("Dockerfile 里出现了 USER root")
	}
}

// TestNoHardcodedEndpoints：源码里不能写死依赖地址。
//
// 写死之后，组件在别人的项目里就永远连不上——而平台注入的
// PEOPLE_BASIC_ENDPOINT 会被静静忽略，看不出任何异常。
func TestNoHardcodedEndpoints(t *testing.T) {
	sources := []string{"main.go", "service.go", "people.go", "config.go", "token.go"}

	for _, name := range sources {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读取 %s 失败：%v", name, err)
		}
		content := string(raw)

		for _, forbidden := range []string{"localhost:", "127.0.0.1", "http://people"} {
			if strings.Contains(content, forbidden) {
				t.Errorf("%s 里出现了硬编码地址 %q", name, forbidden)
			}
		}
	}
}
