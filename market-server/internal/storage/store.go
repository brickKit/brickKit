// Package storage 定义市场的产物（artifacts）存储抽象。
//
// 市场需要存储组件发布时上传的 artifacts（API 契约、SDK、文档等，002 §5），
// 每个版本独立存储（开发计划 Step 18 任务 8）。默认实现基于 MinIO（S3 兼容）。
//
// 本文件在 Step 1 中只建立接口骨架与 MinIO 客户端构造，具体实现见 Step 18。
package storage

import (
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
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

// MinIOConfig 是 MinIO 连接配置，全部来自环境变量（006 §6 密钥管理）。
type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

// NewMinIOClient 按配置创建 MinIO 客户端。
func NewMinIOClient(cfg MinIOConfig) (*minio.Client, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("MinIO endpoint 未配置")
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 MinIO 客户端失败：%w", err)
	}
	return client, nil
}
