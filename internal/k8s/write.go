package k8s

// 本文件负责把生成结果落到 .brickkit/generated/k8s/ 下（005 §5、开发计划 16.13）。

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
)

// 文件权限。
const (
	dirPerm = 0o755
	// filePerm 是普通清单的权限。
	filePerm = 0o644
	// secretPerm 是 Secret 清单的权限：里面是明文密码，只有自己能读。
	secretPerm = 0o600
)

// secretsDir 是存放 Secret 的子目录。
const secretsDir = "secrets"

// WriteFiles 把生成的清单写进 dir。
//
// 先整个删掉再写：组件从 brickkit.yaml 里删掉之后，上一次生成的那份清单
// 如果还留在目录里，`kubectl apply -f k8s/` 会把它**又部署一遍**——
// 使用者明明已经把组件移除了，集群里却还跑着。
func WriteFiles(dir string, files []File) error {
	if err := os.RemoveAll(dir); err != nil {
		return writeError("清理目录", dir, err)
	}

	for _, f := range files {
		path := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
			return writeError("创建目录", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, f.YAML, permOf(f.Path)); err != nil {
			return writeError("写入文件", path, err)
		}
	}
	return nil
}

// permOf 决定一份清单的权限。
func permOf(path string) os.FileMode {
	if strings.HasPrefix(path, secretsDir+"/") {
		return secretPerm
	}
	return filePerm
}

func writeError(action, path string, cause error) error {
	return clierr.Newf(clierr.CodeInternal, "错误：%s失败", action).
		WithDetail("路径", path).
		WithDetail("原因", cause.Error()).
		WithHint("检查目录权限与磁盘空间").
		WithCause(cause)
}
