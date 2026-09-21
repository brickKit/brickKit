package manifest

import (
	"fmt"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
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
		return nil, clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.ManifestScaffoldBadID, id)).
			WithDetail(i18n.T(msgid.LabelReason), problem).
			WithHint(i18n.T(msgid.ManifestScaffoldHintIDFormat)).
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
  description: %s
paths: {}
`, id, i18n.T(msgid.ManifestScaffoldOpenAPIDescription))
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

// %s
service Service {
}
`, scope, name, i18n.T(msgid.ManifestScaffoldProtoTodo))
	default:
		return nil, clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.ManifestScaffoldBadContract, opts.Contract)).
			WithHint(i18n.T(msgid.ManifestScaffoldHintContractValues, ContractOpenAPI, ContractProto)).
			WithExit(clierr.ExitUsage)
	}

	manifestContent := commentBlock("", i18n.T(msgid.ManifestScaffoldHeader, id)) + fmt.Sprintf(`apiVersion: brickkit/v1
kind: Component

metadata:
  id: %s
  name: %s # %s
  version: 0.1.0
  description: %s
%s
deployment:
  type: container
  image: %s:0.1.0 # %s
  port: 8080 # %s

healthCheck:
  type: http
  path: /healthz
%s`, id, name, i18n.T(msgid.ManifestScaffoldNameTodo), i18n.T(msgid.ManifestScaffoldDescriptionTodo), artifactsBlock,
		id, i18n.T(msgid.ManifestScaffoldImageTodo), i18n.T(msgid.ManifestScaffoldPortTodo),
		commentBlock("  ", i18n.T(msgid.ManifestScaffoldStartPeriodComment)))

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

// commentBlock 把多行文字变成 YAML 注释：每行前面加 indent 和 "# "，末尾换行。
// 骨架里的说明文字随语言变，各语言要几行由目录自己决定，所以按行拆而不是写死行数。
func commentBlock(indent, text string) string {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		b.WriteString(indent + "# " + line + "\n")
	}
	return b.String()
}
