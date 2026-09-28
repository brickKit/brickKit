module github.com/brickkit/brickkit/market-server

go 1.22

require (
	github.com/aws/aws-sdk-go-v2 v1.32.3
	github.com/aws/aws-sdk-go-v2/credentials v1.17.42
	github.com/aws/aws-sdk-go-v2/service/s3 v1.66.0
	github.com/lib/pq v1.12.3
	github.com/stretchr/testify v1.11.1
	golang.org/x/crypto v0.21.0
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.6.6 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.3.22 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.6.22 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.3.21 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.12.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.4.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.12.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.18.2 // indirect
	github.com/aws/smithy-go v1.22.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/pretty v0.3.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// 市场用 CLI 的 Manifest 规则校验发布（同一份规则，不是两份各自漂移的拷贝）。
// 模块路径在主模块之下，主模块的 internal/ 包才可以导入；replace 指向本仓库的父目录。
require github.com/brickkit/brickkit v0.0.0

replace github.com/brickkit/brickkit => ../
