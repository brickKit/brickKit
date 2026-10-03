//go:build unix

package cli

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// secretFileProject：一个本地源组件 erp/sales，SIGNING_KEY_FILE 以文件交付，值是 config/ 里写的 key。
func secretFileProject(t *testing.T, key string) string {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"brickkit.yaml": "project: shop\nsources:\n  - name: local-dev\n    type: local\n    path: ./components\n" +
			"components:\n  - id: erp/sales\n    version: 1.0.0\n",
		"deploy.yaml": "target: docker\ncomponents:\n  - id: erp/sales\n",
		"components/erp/sales/component.yaml": comp{ID: "erp/sales", Version: "1.0.0",
			ConfigSchema: []string{"SIGNING_KEY_FILE:"}, SecretConfig: []string{"SIGNING_KEY_FILE"},
			FileConfig: []string{"SIGNING_KEY_FILE"}}.yamlText(),
	})
	writeSigningKey(t, dir, key)
	return dir
}

func writeSigningKey(t *testing.T, dir, key string) {
	t.Helper()
	writeTree(t, dir, map[string]string{"config/erp-sales.yaml": "SIGNING_KEY_FILE: ${SIGNING_KEY}\n"})
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("SIGNING_KEY="+key+"\n"), 0o600))
}

func inode(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Sys().(*syscall.Stat_t).Ino
}

// 以文件交付的配置项怎么落盘：secrets/ 是 0700（宿主机上别的用户进不来），组件的目录 0755、文件 0644
// （容器里的进程常常既不是 root 也不是宿主机上的这个用户）。值变了，文件原地换成新内容而组件的目录还是
// 同一个 inode——容器挂的就是它，重建目录的话容器里看到的是一个再也不会变的旧目录。内容没变就不动文件。
func TestUpWritesSecretFilesInPlace(t *testing.T) {
	clearAmbientEnvForTest(t, "SIGNING_KEY")
	dir := secretFileProject(t, "key-one")
	root := filepath.Join(dir, ".brickkit", "generated", "secrets")
	service := filepath.Join(root, "erp-sales-1-0-0")
	file := filepath.Join(service, "SIGNING_KEY_FILE")

	r := runWithEngine(t, newFakeEngine(), dir, "up")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, "key-one", readFile(t, file), "文件里就是值本身：不加引号、不转义、不补换行")
	for path, mode := range map[string]os.FileMode{root: 0o700, service: 0o755, file: 0o644} {
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, mode, info.Mode().Perm(), path)
	}
	compose := readFile(t, filepath.Join(dir, ".brickkit", "generated", composeFileName))
	assert.Contains(t, compose, "SIGNING_KEY_FILE=/run/brickkit/secrets/erp-sales-1-0-0/SIGNING_KEY_FILE")
	assert.NotContains(t, compose, "key-one")

	dirBefore, fileBefore := inode(t, service), inode(t, file)
	r = runWithEngine(t, newFakeEngine(), dir, "up")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, fileBefore, inode(t, file), "内容没变：文件不动")

	writeSigningKey(t, dir, "key-two")
	r = runWithEngine(t, newFakeEngine(), dir, "up")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, "key-two", readFile(t, file))
	assert.Equal(t, dirBefore, inode(t, service), "目录还是同一个：正在运行的容器挂的就是它")
	assert.NotEqual(t, fileBefore, inode(t, file), "文件是整份换掉的（写临时文件再改名），读的人读不到写了一半的")
	entries, err := os.ReadDir(service)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "不留临时文件")
}

// --dry-run 不写这些文件：目录挂在正在运行的容器里，一写进去组件读到的就是新值——那已经是部署了。
func TestUpDryRunDoesNotTouchSecretFiles(t *testing.T) {
	clearAmbientEnvForTest(t, "SIGNING_KEY")
	dir := secretFileProject(t, "key-one")
	file := filepath.Join(dir, ".brickkit", "generated", "secrets", "erp-sales-1-0-0", "SIGNING_KEY_FILE")

	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NoFileExists(t, file)

	require.Equal(t, clierr.ExitOK, runWithEngine(t, newFakeEngine(), dir, "up").code)
	writeSigningKey(t, dir, "key-two")
	r = runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, "key-one", readFile(t, file), "运行中的组件读到的仍是上一次 up 的值")
}

// 这一项不再以文件交付（或组件不在了）：它的文件要删掉，不在磁盘上多留一份密钥；一项都没有了，整个目录也不留。
func TestUpRemovesSecretFilesNoLongerGenerated(t *testing.T) {
	clearAmbientEnvForTest(t, "SIGNING_KEY")
	dir := secretFileProject(t, "key-one")
	root := filepath.Join(dir, ".brickkit", "generated", "secrets")
	require.Equal(t, clierr.ExitOK, runWithEngine(t, newFakeEngine(), dir, "up").code)
	require.FileExists(t, filepath.Join(root, "erp-sales-1-0-0", "SIGNING_KEY_FILE"))

	writeTree(t, dir, map[string]string{
		"components/erp/sales/component.yaml": comp{ID: "erp/sales", Version: "1.0.0",
			ConfigSchema: []string{"SIGNING_KEY_FILE:"}, SecretConfig: []string{"SIGNING_KEY_FILE"}}.yamlText(),
	})
	r := runWithEngine(t, newFakeEngine(), dir, "up")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NoDirExists(t, root)
}
