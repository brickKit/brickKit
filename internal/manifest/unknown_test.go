// 本文件盯 component.yaml 的未知字段检查。
//
// 这条检查 brickkit.yaml 那边早就有（P33），component.yaml 这边一直没有。
// 两份文件都是使用者手写的，而 component.yaml 这边其实**更危险**：
// brickkit.yaml 的必填字段写错了，语义校验会兜住（"project 缺失"）；
// component.yaml 里真正会咬人的是**可选字段**——它们没有任何兜底，
// 写错一个字母就静默失效，要到运行时才炸。
package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// 可选字段拼错时必须报出来——它们是这条检查存在的全部理由。
//
// 每一条都对应一种"生成物完全正常、运行时才炸"的事故：
func TestUnknownOptionalFieldIsRejected(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wrong   string
		guess   string
		fallout string
	}{
		{
			name:    "dependencies",
			yaml:    minimalYAML + "dependencys:\n  components:\n    - department/tree@1.0.0\n",
			wrong:   "dependencys",
			guess:   "dependencies",
			fallout: "依赖整个消失，调用方一个 *_ENDPOINT 都拿不到",
		},
		{
			name:    "migration",
			yaml:    minimalYAML + "migrations:\n  command: [\"./migrate\"]\n",
			wrong:   "migrations",
			guess:   "migration",
			fallout: "迁移不跑，组件起来报 relation does not exist（002 §8.5.1）",
		},
		{
			name:    "configSchema",
			yaml:    minimalYAML + "configSchemas:\n  type: object\n",
			wrong:   "configSchemas",
			guess:   "configSchema",
			fallout: "配置项默认值一个都不注入",
		},
		{
			name:    "artifacts",
			yaml:    minimalYAML + "artifact:\n  - type: api-docs\n    files: [openapi.json]\n",
			wrong:   "artifact",
			guess:   "artifacts",
			fallout: "产物不下载，调用方拿不到 API 契约",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.yaml), "component.yaml")

			require.Error(t, err, "拼错了必须报出来——%s", c.fallout)
			rendered := clierr.As(err).Format()
			assert.Contains(t, rendered, c.wrong, "要指出是哪个字段")
			assert.Contains(t, rendered, c.guess, "要猜出使用者想写的那个")
		})
	}
}

// 大小写也算拼错：yaml.v3 是**精确比对 tag** 的。
//
// `healthcheck` 匹配不上 `healthCheck`——实测过。必填字段还有语义校验兜底
// （报"healthCheck 缺失"），但那句话说的是"缺失"，而使用者明明写了，
// 只是大小写不对；直接点名才省得他盯着那一行看半天。
func TestWrongCaseIsRejected(t *testing.T) {
	_, err := Parse([]byte(`apiVersion: brickkit/v1
kind: Component
metadata:
  id: infra/tool
  name: 工具组件
  version: 1.0.0
  description: 最小组件
deployment:
  type: container
  image: registry.brickkit.io/tool:1.0.0
  port: 8080
  extraports:
    - name: grpc
      port: 9090
healthCheck:
  type: http
  path: /healthz
`), "component.yaml")

	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "extraPorts", "要猜出正确的大小写")
}

// 嵌套层里的拼错同样要查。
func TestUnknownFieldAtEveryLevel(t *testing.T) {
	cases := map[string]string{
		"metadata": `apiVersion: brickkit/v1
kind: Component
metadata:
  id: infra/tool
  name: 工具组件
  version: 1.0.0
  description: 最小组件
  licence: MIT
deployment:
  type: container
  image: registry.brickkit.io/tool:1.0.0
  port: 8080
healthCheck:
  type: http
  path: /healthz
`,
		"deployment": `apiVersion: brickkit/v1
kind: Component
metadata:
  id: infra/tool
  name: 工具组件
  version: 1.0.0
  description: 最小组件
deployment:
  type: container
  image: registry.brickkit.io/tool:1.0.0
  port: 8080
  extraPort:
    - name: grpc
      port: 9090
healthCheck:
  type: http
  path: /healthz
`,
		"extraPorts 元素": `apiVersion: brickkit/v1
kind: Component
metadata:
  id: infra/tool
  name: 工具组件
  version: 1.0.0
  description: 最小组件
deployment:
  type: container
  image: registry.brickkit.io/tool:1.0.0
  port: 8080
  extraPorts:
    - name: grpc
      prt: 9090
healthCheck:
  type: http
  path: /healthz
`,
		"healthCheck": minimalYAML + "healthCheck:\n  type: http\n  paht: /healthz\n",
	}

	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(text), "component.yaml")
			assert.Error(t, err, "这一层的未知字段也必须被发现")
		})
	}
}

// configSchema.properties 底下的键是**组件作者自己定的**，不查。
//
// 那里写什么都合法——在这里报"未知字段"会把每一个正常的配置项声明都拦下来。
func TestConfigSchemaPropertiesAllowArbitraryKeys(t *testing.T) {
	_, err := Parse([]byte(minimalYAML+`configSchema:
  type: object
  properties:
    whateverIWant:
      type: integer
      default: 20
    anotherOne:
      type: string
`), "component.yaml")

	assert.NoError(t, err, "配置项名字由组件作者定，平台不该管")
}

// 正常的 Manifest 不该被这条检查打扰。
func TestFullManifestPassesUnknownFieldCheck(t *testing.T) {
	_, err := Parse([]byte(minimalYAML+`tags:
  - people
artifacts:
  - type: api-docs
    format: openapi
    description: HTTP API
    files: [openapi.json]
dependencies:
  components:
    - department/tree@1.0.0
    - id: infra/bus@1.0.0
      optional: true
  resources:
    - kind: database
      engine: postgresql
migration:
  command: ["./migrate", "up"]
observability:
  metrics: false
compatibility:
  minCliVersion: 1.0.0
`), "component.yaml")

	assert.NoError(t, err)
}
