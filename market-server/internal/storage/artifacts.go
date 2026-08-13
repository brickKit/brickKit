package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// ErrObjectNotFound 表示对象不存在。上层据此翻译成 404。
var ErrObjectNotFound = errors.New("产物文件不存在")

// ObjectKey 拼出产物文件在对象存储中的键：
//
//	components/<组件ID>/<版本>/<产物类型>/<文件路径>
//
// 版本在路径里，因此**每个版本的产物天然独立存储**（开发计划 18.22）：
// 升级不会覆盖旧版本的 API 契约，调用方还能拿到自己那一版（002 §7.10）。
func ObjectKey(componentID, version, artifactType, file string) string {
	return path.Join("components", componentID, version, artifactType, path.Clean("/" + file)[1:])
}

// VersionPrefix 返回某个组件版本下所有产物的键前缀。
func VersionPrefix(componentID, version string) string {
	return path.Join("components", componentID, version) + "/"
}

// ============================================================
// 内存实现
// ============================================================

// Memory 是产物存储的进程内实现，用于单元测试与本地模式。
type Memory struct {
	mu      sync.RWMutex
	objects map[string][]byte
}

// NewMemory 创建一个空的内存产物存储。
func NewMemory() *Memory {
	return &Memory{objects: map[string][]byte{}}
}

func (m *Memory) Put(_ context.Context, objectKey string, r io.Reader, _ int64) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[objectKey] = data
	return nil
}

func (m *Memory) Get(_ context.Context, objectKey string) (io.ReadCloser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, ok := m.objects[objectKey]
	if !ok {
		return nil, ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *Memory) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var keys []string
	for key := range m.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// ============================================================
// S3（RustFS）实现
// ============================================================

// S3Store 是基于 S3 兼容对象存储（RustFS）的产物存储。
type S3Store struct {
	client *s3.Client
	bucket string
}

// NewS3Store 按配置创建产物存储。
func NewS3Store(cfg Config) (*S3Store, error) {
	cfg = cfg.withDefaults()
	client, err := NewS3Client(cfg)
	if err != nil {
		return nil, err
	}
	return &S3Store{client: client, bucket: cfg.Bucket}, nil
}

// EnsureBucket 建 bucket（幂等）。市场启动时调用，省去手工初始化。
func (s *S3Store) EnsureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	if err == nil {
		return nil
	}

	_, err = s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.bucket)})
	if err == nil {
		return nil
	}
	// 并发启动多个实例时可能已被别人建好，这不算失败
	var owned *types.BucketAlreadyOwnedByYou
	var exists *types.BucketAlreadyExists
	if errors.As(err, &owned) || errors.As(err, &exists) {
		return nil
	}
	return fmt.Errorf("创建 bucket %s 失败：%w", s.bucket, err)
}

func (s *S3Store) Put(ctx context.Context, objectKey string, r io.Reader, size int64) error {
	// S3 的 PutObject 需要可重放的 body 来做签名，这里先读进内存。
	// 产物是 proto / OpenAPI 这类文本文件，体量可控。
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if size > 0 && int64(len(data)) != size {
		return fmt.Errorf("产物大小与声明不符：声明 %d 字节，实际 %d 字节", size, len(data))
	}

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(objectKey),
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
	})
	if err != nil {
		return fmt.Errorf("上传产物 %s 失败：%w", objectKey, err)
	}
	return nil
}

func (s *S3Store) Get(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		var noKey *types.NoSuchKey
		var notFound *types.NotFound
		if errors.As(err, &noKey) || errors.As(err, &notFound) {
			return nil, ErrObjectNotFound
		}
		return nil, fmt.Errorf("下载产物 %s 失败：%w", objectKey, err)
	}
	return out.Body, nil
}

func (s *S3Store) List(ctx context.Context, prefix string) ([]string, error) {
	var (
		keys  []string
		token *string
	)
	for {
		out, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(s.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("列举产物（前缀 %s）失败：%w", prefix, err)
		}
		for _, obj := range out.Contents {
			keys = append(keys, aws.ToString(obj.Key))
		}
		if out.NextContinuationToken == nil {
			break
		}
		token = out.NextContinuationToken
	}
	sort.Strings(keys)
	return keys, nil
}

// 编译期确认两个实现都满足接口。
var (
	_ ArtifactStore = (*Memory)(nil)
	_ ArtifactStore = (*S3Store)(nil)
)
