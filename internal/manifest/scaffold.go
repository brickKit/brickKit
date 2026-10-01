package manifest

import (
	"fmt"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcomment"
)

// 契约占位格式。跟 Artifact.Format 一样是自由字符串，这里只收窄到
// Scaffold 认识怎么生成占位文件的那几种——不代表平台限定了这个枚举，
// 组件发布时完全可以用别的 format（type/format 都不限枚举）。
const (
	ContractOpenAPI = "openapi"
	ContractProto   = "proto"
)

// ScaffoldOptions 是 Scaffold 生成骨架时的可选项。
type ScaffoldOptions struct {
	// Contract 是契约占位格式：ContractOpenAPI、ContractProto，或空字符串
	// （不生成契约文件，也不写 artifacts 段）。
	Contract string
	// Shell 生成外壳骨架：带 shell.members，BRICKKIT.md 的外壳声明一节随之填写。
	Shell bool
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
// 起步代码由 docs/{en,zh}/03-component-guide/04-new-and-skeleton.md 这类"带读一个真实组件"
// 的文档承担，不是这里的事。
func Scaffold(id string, opts ScaffoldOptions) ([]ScaffoldFile, error) {
	if problem := ComponentIDProblem(id); problem != "" {
		return nil, clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.InvalidComponentID, id)).
			WithDetail(i18n.T(msgid.LabelReason), problem).
			WithHint(i18n.T(msgid.HintComponentIDFormat)).
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

	// 外壳骨架：校验不接受空的成员列表，骨架又必须能通过校验，所以放一个占位成员；
	// 不改就 add 会大声失败、点出这个成员
	var shellBlock string
	if opts.Shell {
		shellBlock = "\nshell:\n" + yamlcomment.Block("  ", i18n.T(msgid.ManifestScaffoldShellMembersTodo)) +
			"  members:\n    - " + scaffoldPlaceholderMember + "\n"
	}

	manifestContent := yamlcomment.Block("", i18n.T(msgid.ManifestScaffoldHeader, id)) + fmt.Sprintf(`apiVersion: brickkit/v1
kind: Component

metadata:
  id: %s
  name: %s # %s
  version: 0.1.0
  description: %s
  # repository: https://… # %s
%s%s
deployment:
  type: container
  build: # %s
    context: .
    dockerfile: Dockerfile
  port: 8080 # %s

healthCheck:
  type: http
  path: /healthz
%s`, id, name, i18n.T(msgid.ManifestScaffoldNameTodo), i18n.T(msgid.ManifestScaffoldDescriptionTodo),
		i18n.T(msgid.ManifestScaffoldRepositoryComment), artifactsBlock, shellBlock,
		i18n.T(msgid.ManifestScaffoldBuildComment), i18n.T(msgid.ManifestScaffoldPortTodo),
		yamlcomment.Block("  ", i18n.T(msgid.ManifestScaffoldStartPeriodComment)))

	files := []ScaffoldFile{{Path: FileName, Content: []byte(manifestContent)}}
	var contractPath string
	if contractFile != "" {
		ext := "yaml"
		if opts.Contract == ContractProto {
			ext = "proto"
		}
		filename := "openapi"
		if opts.Contract == ContractProto {
			filename = "service"
		}
		contractPath = "api/" + filename + "." + ext
		files = append(files, ScaffoldFile{
			Path:    contractPath,
			Content: []byte(contractFile),
		})
	}
	// 文档紧跟 component.yaml：BRICKKIT.md 是消费方读这个组件的入口，AGENTS.md 是开发它的 AI 的入口
	files = append(files[:1], append(docFiles(id, contractPath, opts.Shell), files[1:]...)...)
	return files, nil
}

// FileDoc 是组件仓库根目录的组件文档（随版本发布给使用方的那一份）。
const FileDoc = docspec.FileBrickkit

// MaxDocBytes 是发布到市场的 BRICKKIT.md 的上限：publish 发之前查，市场收的时候再查。
// 文档是给人与 AI 读的说明，256 KiB 已经是几万字。
const MaxDocBytes = 256 << 10

// MaxDocsTotalBytes 是 BRICKKIT.md 连同全部译本加起来的上限：一次发布的请求体有上限，
// 照它限死合计，市场那头的请求大小上限就不用跟着译本份数涨。
const MaxDocsTotalBytes = 4 * MaxDocBytes

// scaffoldPlaceholderMember 是外壳骨架里的占位成员。
const scaffoldPlaceholderMember = "example/member@0.1.0"

