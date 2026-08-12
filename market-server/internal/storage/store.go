// Package storage 定义市场的产物（artifacts）存储抽象。
//
// 市场需要存储组件发布时上传的 artifacts（API 契约、SDK、文档等，002 §5），
// 每个版本独立存储（开发计划 Step 18 任务 8）。
//
// 默认实现基于 **RustFS**（S3 兼容对象存储），通过 aws-sdk-go-v2 访问。
// 设计书 007 只要求"对象存储"，不绑定具体产品：任何 S3 兼容服务
// （RustFS / Ceph / S3 本体）都能通过同一份配置接入。
//
// 本文件只建立接口骨架、连接配置与客户端构造，读写实现见 Step 18。
package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// 连接配置的环境变量名（006 §6：密钥不落配置文件，只走环境变量）。
const (
	EnvEndpoint  = "RUSTFS_ENDPOINT"
	EnvAccessKey = "RUSTFS_ACCESS_KEY"
	EnvSecretKey = "RUSTFS_SECRET_KEY"
	EnvBucket    = "RUSTFS_BUCKET"
	EnvRegion    = "RUSTFS_REGION"
)

// 默认值。
const (
	// DefaultBucket 是存放 artifacts 的默认 bucket。
	DefaultBucket = "brickkit-artifacts"
	// DefaultRegion 是签名用的区域名。S3 协议要求必填，
	// RustFS 不校验其内容，给一个稳定值即可。
	DefaultRegion = "us-east-1"
)

// ArtifactStore 是产物存储的抽象接口。
type ArtifactStore interface {
	// Put 上传一个产物文件。objectKey 形如
	// <组件ID>/<版本>/<artifact type>/<文件路径>。
	Put(ctx context.Context, objectKey string, r io.Reader, size int64) error
	// Get 下载一个产物文件，调用方负责 Close。
	Get(ctx context.Context, objectKey string) (io.ReadCloser, error)
	// List 列出指定前缀下的所有产物 objectKey。
	List(ctx context.Context, prefix string) ([]string, error)
}

// Config 是对象存储的连接配置。
type Config struct {
	// Endpoint 是 S3 API 地址，必须带 scheme，如 http://localhost:9000。
	Endpoint string
	// AccessKey / SecretKey 是访问凭据。
	AccessKey string
	SecretKey string
	// Bucket 是存放 artifacts 的 bucket 名。
	Bucket string
	// Region 是签名用的区域名。
	Region string
}

// Validate 校验配置完整性，缺失项一次全部报出。
func (c Config) Validate() error {
	var missing []string
	if strings.TrimSpace(c.Endpoint) == "" {
		missing = append(missing, EnvEndpoint)
	}
	if strings.TrimSpace(c.AccessKey) == "" {
		missing = append(missing, EnvAccessKey)
	}
	if strings.TrimSpace(c.SecretKey) == "" {
		missing = append(missing, EnvSecretKey)
	}
	if len(missing) > 0 {
		return fmt.Errorf("对象存储配置缺失：%s", strings.Join(missing, ", "))
	}

	u, err := url.Parse(c.Endpoint)
	if err != nil {
		return fmt.Errorf("%s 不是合法的 URL：%w", EnvEndpoint, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s 必须以 http:// 或 https:// 开头（当前是 %q）", EnvEndpoint, c.Endpoint)
	}
	if u.Host == "" {
		return fmt.Errorf("%s 缺少主机地址（当前是 %q）", EnvEndpoint, c.Endpoint)
	}
	return nil
}

// withDefaults 填充可选项的默认值。
func (c Config) withDefaults() Config {
	if c.Bucket == "" {
		c.Bucket = DefaultBucket
	}
	if c.Region == "" {
		c.Region = DefaultRegion
	}
	return c
}

// ConfigFromEnv 从环境变量读取连接配置。
func ConfigFromEnv(lookup func(string) string) (Config, error) {
	cfg := Config{
		Endpoint:  lookup(EnvEndpoint),
		AccessKey: lookup(EnvAccessKey),
		SecretKey: lookup(EnvSecretKey),
		Bucket:    lookup(EnvBucket),
		Region:    lookup(EnvRegion),
	}.withDefaults()

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// NewS3Client 按配置创建 S3 客户端。
//
// 两个关键选项：
//   - BaseEndpoint：指向 RustFS，而不是 AWS 的公有端点
//   - UsePathStyle：RustFS 等自建服务用 path-style 寻址
//     （http://host/bucket/key），而非 AWS 的 virtual-hosted 风格
func NewS3Client(cfg Config) (*s3.Client, error) {
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return s3.New(s3.Options{
		Region:       cfg.Region,
		BaseEndpoint: aws.String(cfg.Endpoint),
		UsePathStyle: true,
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
	}), nil
}
