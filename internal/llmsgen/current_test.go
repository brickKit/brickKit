package llmsgen

import (
	"strings"
	"testing"
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
