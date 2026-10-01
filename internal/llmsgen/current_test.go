package llmsgen

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brickkit/brickkit/internal/mdtext"
)

// 签入的合集就是这份文档树生成的那一份：改了文档要跑 make generate-llms（或装好提交钩子）。
func TestGeneratedBundlesAreCurrent(t *testing.T) {
	outs, err := Generate("../..", DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if problems := Check("../..", outs); len(problems) > 0 {
		t.Fatalf("llms/ is stale — run make generate-llms:\n%s", strings.Join(problems, "\n"))
	}
}

// 真实合集里每一条相对链接（代码之外）都要指得到文件：改写规则漏掉一种写法、或者算错了层级，这里就失败。
func TestBundleLinksResolve(t *testing.T) {
	outs, err := Generate("../..", DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outs {
		if !strings.HasPrefix(o.Path, "llms/") {
			continue
		}
		dir := path.Dir(o.Path)
		mdtext.MapLinks(string(o.Content), func(target string) string {
			file, _, _ := strings.Cut(target, "#")
			if file == "" || strings.Contains(file, "://") || strings.HasPrefix(file, "mailto:") {
				return target
			}
			if _, err := os.Stat(filepath.Join("../..", filepath.FromSlash(path.Join(dir, file)))); err != nil {
				t.Errorf("%s: link %s does not resolve", o.Path, target)
			}
			return target
		})
	}
}
