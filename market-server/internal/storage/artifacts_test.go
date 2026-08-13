// 本文件是 Step 18-B 产物存储的测试。
//
// 内存实现与 S3（RustFS）实现共用同一份行为契约；后者只在设置了
// RUSTFS_ENDPOINT 时运行，其余环境自动跳过。
package storage

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 18.22：版本在对象键里，所以两个版本的同名产物天然互不覆盖。
func TestObjectKeyIsPerVersion(t *testing.T) {
	v1 := ObjectKey("people/basic", "1.0.0", "api-contract", "proto/people/v1/people.proto")
	v2 := ObjectKey("people/basic", "2.0.0", "api-contract", "proto/people/v1/people.proto")

	assert.Equal(t, "components/people/basic/1.0.0/api-contract/proto/people/v1/people.proto", v1)
	assert.NotEqual(t, v1, v2, "不同版本必须落在不同的对象键上")
	assert.True(t, strings.HasPrefix(v1, VersionPrefix("people/basic", "1.0.0")))
	assert.False(t, strings.HasPrefix(v2, VersionPrefix("people/basic", "1.0.0")))
}

// 产物文件路径来自 Manifest，必须防住越界写入（008 安全边界）。
func TestObjectKeyIsConfinedToVersionPrefix(t *testing.T) {
	key := ObjectKey("people/basic", "1.0.0", "api-contract", "../../../etc/passwd")

	assert.True(t, strings.HasPrefix(key, "components/people/basic/1.0.0/api-contract/"),
		"路径穿越必须被规整掉，实际：%s", key)
	assert.NotContains(t, key, "..")
}

func TestObjectKeyNormalizesLeadingSlash(t *testing.T) {
	assert.Equal(t,
		"components/people/basic/1.0.0/api-docs/openapi.json",
		ObjectKey("people/basic", "1.0.0", "api-docs", "/openapi.json"))
}

// ============================================================
// 行为契约（两个实现共用）
// ============================================================

func TestArtifactStoreContract(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		runStoreContract(t, NewMemory())
	})

	t.Run("rustfs", func(t *testing.T) {
		if os.Getenv(EnvEndpoint) == "" {
			t.Skipf("未设置 %s，跳过 RustFS 集成测试", EnvEndpoint)
		}
		cfg, err := ConfigFromEnv(os.Getenv)
		require.NoError(t, err)

		store, err := NewS3Store(cfg)
		require.NoError(t, err)
		require.NoError(t, store.EnsureBucket(context.Background()), "bucket 应能自动创建")
		runStoreContract(t, store)
	})
}

func runStoreContract(t *testing.T, store ArtifactStore) {
	t.Helper()
	ctx := context.Background()

	// 用测试名做前缀，多次运行与并行运行互不干扰
	component := "demo/" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	proto := ObjectKey(component, "1.0.0", "api-contract", "proto/demo.proto")
	docsV1 := ObjectKey(component, "1.0.0", "api-docs", "openapi.json")
	docsV2 := ObjectKey(component, "2.0.0", "api-docs", "openapi.json")

	require.NoError(t, store.Put(ctx, proto, strings.NewReader("syntax = \"proto3\";\n"), 0))
	require.NoError(t, store.Put(ctx, docsV1, strings.NewReader(`{"version":"1.0.0"}`), 0))
	require.NoError(t, store.Put(ctx, docsV2, strings.NewReader(`{"version":"2.0.0"}`), 0))

	assert.Equal(t, "syntax = \"proto3\";\n", readObject(t, store, proto))
	assert.Equal(t, `{"version":"1.0.0"}`, readObject(t, store, docsV1))
	assert.Equal(t, `{"version":"2.0.0"}`, readObject(t, store, docsV2), "两个版本互不覆盖")

	// 覆盖写同一个键
	require.NoError(t, store.Put(ctx, docsV1, strings.NewReader(`{"version":"1.0.1"}`), 0))
	assert.Equal(t, `{"version":"1.0.1"}`, readObject(t, store, docsV1))

	// 按版本前缀列举
	keys, err := store.List(ctx, VersionPrefix(component, "1.0.0"))
	require.NoError(t, err)
	assert.Equal(t, []string{proto, docsV1}, keys, "按对象键字母序：api-contract 在 api-docs 之前")

	keys, err = store.List(ctx, VersionPrefix(component, "2.0.0"))
	require.NoError(t, err)
	assert.Equal(t, []string{docsV2}, keys)

	// 不存在的对象
	_, err = store.Get(ctx, ObjectKey(component, "9.9.9", "api-docs", "openapi.json"))
	assert.ErrorIs(t, err, ErrObjectNotFound)

	keys, err = store.List(ctx, VersionPrefix(component, "9.9.9"))
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func readObject(t *testing.T, store ArtifactStore, key string) string {
	t.Helper()
	r, err := store.Get(context.Background(), key)
	require.NoError(t, err, "读取 %s", key)
	defer func() { _ = r.Close() }()

	data, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(data)
}

// 声明的大小与实际不符时拒绝上传（防止半截文件落库）。
func TestS3StoreRejectsSizeMismatch(t *testing.T) {
	if os.Getenv(EnvEndpoint) == "" {
		t.Skipf("未设置 %s，跳过 RustFS 集成测试", EnvEndpoint)
	}
	cfg, err := ConfigFromEnv(os.Getenv)
	require.NoError(t, err)
	store, err := NewS3Store(cfg)
	require.NoError(t, err)

	err = store.Put(context.Background(),
		ObjectKey("demo/size", "1.0.0", "api-docs", "x.json"), strings.NewReader("12345"), 99)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "大小与声明不符")
}
