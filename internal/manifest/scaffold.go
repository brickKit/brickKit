package manifest

import (
	"fmt"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
)

// 契约占位格式。跟 Artifact.Format 一样是自由字符串，这里只收窄到
// Scaffold 认识怎么生成占位文件的那几种——不代表平台限定了这个枚举，
// 组件发布时完全可以用别的 format（002 §2.3：type/format 都不限枚举）。
const (
	ContractOpenAPI = "openapi"
	ContractProto   = "proto"
)

// ScaffoldOptions 是 Scaffold 生成骨架时的可选项。
type ScaffoldOptions struct {
	// Contract 是契约占位格式：ContractOpenAPI、ContractProto，或空字符串
	// （不生成契约文件，也不写 artifacts 段）。
	Contract string
}

// ScaffoldFile 是 Scaffold 生成的一份文件：相对组件目录的路径 → 内容。
type ScaffoldFile struct {
	Path    string
	Content []byte
}

// Scaffold 生成一个新组件的最小骨架：一份**能通过 Parse + Validate** 的
// component.yaml，以及（可选）一份契约占位文件。
//
// 只管生成内容，不碰文件系统——调用方（brickkit new）负责把这些文件写到
// 它选的目录里，这样测试不用真的建目录就能验证内容对不对。
//
// 语言无关：不生成 Dockerfile，也不生成任何源码。平台不替组件作者选语言，
// 起步代码由 docs/{en,zh}/04-go-component-template.md 这类"带读一个真实组件"
// 的文档承担，不是这里的事。
func Scaffold(id string, opts ScaffoldOptions) ([]ScaffoldFile, error) {
	if problem := ComponentIDProblem(id); problem != "" {
		return nil, clierr.Newf(clierr.CodeInvalidArgument, "错误：组件 ID 不合法：%s", id).
			WithDetail("原因", problem).
			WithHint("组件 ID 格式为 <scope>/<name>，如 people/basic").
			WithExit(clierr.ExitUsage)
	}

	scope, name, _ := strings.Cut(id, "/")

	var artifactsBlock, contractFile string
	switch opts.Contract {
	case "":
		// 不生成契约文件，component.yaml 里也就没有 artifacts 段。
	case ContractOpenAPI:
		artifactsBlock = `
artifacts:
  - type: api-contract
    format: openapi
    files:
      - api/openapi.yaml
`
		contractFile = fmt.Sprintf(`openapi: 3.0.3
info:
  title: %s
  version: 0.1.0
  description: TODO：这个组件对外提供的 API
paths: {}
`, id)
	case ContractProto:
		artifactsBlock = `
artifacts:
  - type: api-contract
    format: proto
    files:
      - api/service.proto
`
		contractFile = fmt.Sprintf(`syntax = "proto3";

package %s.%s.v1;

// TODO：定义这个组件对外提供的 RPC
service Service {
}
`, scope, name)
	default:
		return nil, clierr.Newf(clierr.CodeInvalidArgument, "错误：--contract 取值不合法：%s", opts.Contract).
			WithHint(fmt.Sprintf("必须是 %s 或 %s 之一", ContractOpenAPI, ContractProto)).
			WithExit(clierr.ExitUsage)
	}

	manifestContent := fmt.Sprintf(`# %s —— 由 brickkit new 生成的骨架
# 下面每一处 TODO 都要改成真的；结构本身已经能通过 brickkit up --dry-run 的校验
apiVersion: brickkit/v1
kind: Component

metadata:
  id: %s
  name: %s # TODO：改成人看的展示名
  version: 0.1.0
  description: TODO：一句话说清楚这个组件做什么
%s
deployment:
  type: container
  image: %s:0.1.0 # TODO：换成真实构建出来的镜像（本地开发前先 docker build）
  port: 8080 # TODO：换成组件实际监听的端口

healthCheck:
  type: http
  path: /healthz
  # 冷启动超过 30 秒（Spring Boot / Django 预加载 / .NET 首次 JIT 等）要写
  # startPeriodSeconds，否则 K8s 下会永久 CrashLoopBackOff
`, id, id, name, artifactsBlock, id)

	files := []ScaffoldFile{{Path: FileName, Content: []byte(manifestContent)}}
	if contractFile != "" {
		ext := "yaml"
		if opts.Contract == ContractProto {
			ext = "proto"
		}
		filename := "openapi"
		if opts.Contract == ContractProto {
			filename = "service"
		}
		files = append(files, ScaffoldFile{
			Path:    "api/" + filename + "." + ext,
			Content: []byte(contractFile),
		})
	}
	return files, nil
}
