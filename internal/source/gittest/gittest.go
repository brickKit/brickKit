// Package gittest 为测试造真实的 git 远端：一个本地 bare 仓库，按版本打 tag，经 file:// 访问。
// 它只被测试引用；平台代码不依赖它。机器上没有 git 时调用方的测试直接跳过。
package gittest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Remote 是一个组件仓库的远端：bare 是被拉取的那一个，work 是造提交用的工作区。
type Remote struct {
	t    testing.TB
	bare string
	work string
}

// NewRemote 在临时目录里建一个名为 name 的远端仓库（bare 目录名为 <name>.git）。
func NewRemote(t testing.TB, name string) *Remote {
	t.Helper()
	return NewRemoteIn(t, t.TempDir(), name)
}

// NewRemoteIn 在 org 目录下建远端仓库 <org>/<name>.git：同一个 org 下的仓库共用一个
// baseUrl（BaseURL(org)），正是 git 源按组件 ID 推导仓库地址的样子。
func NewRemoteIn(t testing.TB, org, name string) *Remote {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	r := &Remote{t: t, bare: filepath.Join(org, name+".git"), work: filepath.Join(t.TempDir(), "work-"+name)}
	r.git("", "init", "--quiet", "--bare", r.bare)
	r.git("", "init", "--quiet", r.work)
	r.git(r.work, "remote", "add", "origin", r.bare)
	return r
}

// BaseURL 是 org 目录的 file:// 地址，给 git 源的 baseUrl 用（组件 erp/api → <BaseURL>erp-api）。
// 仓库目录名带 .git 后缀，与托管平台上 https://host/org/erp-api 同样能被 git 找到。
func BaseURL(org string) string { return "file://" + filepath.ToSlash(org) + "/" }

// Dir 返回 bare 仓库目录。
func (r *Remote) Dir() string { return r.bare }

// URL 返回 file:// 形式的仓库地址（与真实远端一样走 git 的传输层）。
func (r *Remote) URL() string { return "file://" + filepath.ToSlash(r.bare) }

// Tag 在仓库根目录写入 files（整个工作区换成这些文件），提交并打 tag，推到远端。
func (r *Remote) Tag(tag string, files map[string]string) { r.TagAt("", tag, files) }

// TagAt 把 files 写进子目录 subpath（monorepo 里的一个组件；subpath 为空即仓库根），
// 提交并打 tag。子目录之外的文件保留——同一个仓库里还有别的组件。
func (r *Remote) TagAt(subpath, tag string, files map[string]string) {
	r.t.Helper()
	r.TagAtWithNotes(subpath, tag, "", files)
}

// TagWithNotes 与 Tag 一样，但打的是带发版说明的 tag（brickkit release --notes 打的那种）。
func (r *Remote) TagWithNotes(tag, notes string, files map[string]string) {
	r.t.Helper()
	r.TagAtWithNotes("", tag, notes, files)
}

// TagAtWithNotes 是 TagAt 加上发版说明：notes 不为空时打带注释的 tag，说明原样保留。
func (r *Remote) TagAtWithNotes(subpath, tag, notes string, files map[string]string) {
	r.t.Helper()
	dir := filepath.Join(r.work, filepath.FromSlash(subpath))
	if subpath == "" {
		entries, err := os.ReadDir(r.work)
		if err != nil {
			r.t.Fatal(err)
		}
		for _, e := range entries {
			if e.Name() != ".git" {
				_ = os.RemoveAll(filepath.Join(r.work, e.Name()))
			}
		}
	} else {
		_ = os.RemoveAll(dir)
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
	r.git(r.work, "add", "-A")
	r.git(r.work, "commit", "--quiet", "--allow-empty", "-m", tag)
	if notes == "" {
		r.git(r.work, "tag", tag)
	} else {
		r.git(r.work, "tag", "-a", tag, "--cleanup=verbatim", "-m", notes)
	}
	r.git(r.work, "push", "--quiet", "origin", "HEAD:refs/heads/main", "refs/tags/"+tag)
}

// Remove 删掉远端（模拟离线、仓库被删）。
func (r *Remote) Remove() {
	r.t.Helper()
	if err := os.RemoveAll(r.bare); err != nil {
		r.t.Fatal(err)
	}
}

func (r *Remote) git(dir string, args ...string) {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "init.defaultBranch=main", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
