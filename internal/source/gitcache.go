package source

// 本文件是 git 源的"仓库这一层"：用户级 bare 仓库缓存（附录 A12）、克隆、增量 fetch、
// 从 tag 里读文件。它不认识组件——组件 ID → 仓库地址、版本 → tag 由 git.go 决定。
//
// # 为什么放在用户级目录
//
// 一个组件仓库被多个项目、以及分形架构里的子组件工作台共用；放在每个项目的 .brickkit/
// 下面，同一个仓库会被克隆 N 次。缓存目录按**完整仓库地址**命名：不同组织下同名的仓库
// （github.com/a/erp-api 与 github.com/b/erp-api）不会撞在一起。
//
// # 读取优先级（提案 §9.4）
//
//	1 .brickkit/manifests/ 缓存        Client.Manifest 在进这里之前已经查过
//	2 缓存的 bare 仓库 git show        零网络
//	3 git fetch --tags（增量）          仓库在、但没有要的 tag
//	4 git clone --bare（首次）          仓库从未克隆过
//
// # 鉴权
//
// 平台不碰凭据（提案 §9.9）：调用系统 git，SSH key、credential helper、CI 注入的 token
// 全部由宿主机的 git 配置承担。只做一件事：GIT_TERMINAL_PROMPT=0——没有凭据时让 git
// 直接失败，而不是在 CI 里挂住等一个永远不会来的密码。

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// repoCache 是这一趟运行里用到的全部仓库（按地址），多个 git 源与组件级来源共用。
type repoCache struct {
	root  string
	mu    sync.Mutex
	repos map[string]*gitRepo
}

func newRepoCache(root string) *repoCache {
	return &repoCache{root: root, repos: map[string]*gitRepo{}}
}

// get 返回地址对应的仓库（同一趟里同一个地址只有一个对象：fetch 只做一次）。
func (rc *repoCache) get(url string) *gitRepo {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	r, ok := rc.repos[url]
	if !ok {
		r = &gitRepo{url: url, root: rc.root, dir: repoCacheDir(rc.root, url)}
		rc.repos[url] = r
	}
	return r
}

// gitRepo 是缓存里的一个 bare 仓库。
type gitRepo struct {
	url  string
	root string
	dir  string

	mu sync.Mutex
	// fetched 表示这一趟已经从远端取过最新的 tag（刚克隆的也算）。
	fetched bool
}

// ensure 保证仓库在缓存里：不在就克隆。克隆先落到 <root>/.tmp/ 再改名，中途失败或被打断
// 不会留下半个仓库——下一次看到的要么是完整仓库、要么什么都没有。
func (r *gitRepo) ensure(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.root == "" {
		// 拿不到用户缓存目录（没有 HOME / XDG_CACHE_HOME）：不能退回到当前目录里克隆
		return errNoRepoCache
	}
	if isDir(r.dir) {
		return nil
	}
	tmpRoot := filepath.Join(r.root, ".tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(tmpRoot, "clone-")
	if err != nil {
		return err
	}
	// `--` 之后才是地址：以 - 开头的"地址"不能被 git 当成参数（projfile 校验也拒绝它）
	if _, err := runGit(ctx, "", "clone", "--bare", "--quiet", "--", r.url, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.dir), 0o755); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, r.dir); err != nil {
		_ = os.RemoveAll(tmp)
		// 另一个进程同时克隆完、先改了名：用它的
		if isDir(r.dir) {
			return nil
		}
		return err
	}
	r.fetched = true
	return nil
}

// fetch 增量取远端的 tag（这一趟只取一次）。tag 被强制移动过时以远端为准。
func (r *gitRepo) fetch(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fetched {
		return nil
	}
	// 克隆时已经配好了 origin：不再把地址放上命令行
	if _, err := runGit(ctx, r.dir, "fetch", "--quiet", "--tags", "--force", "origin"); err != nil {
		return err
	}
	r.fetched = true
	return nil
}

// hasTag 报告仓库里有没有这个 tag（指向一个提交）。
func (r *gitRepo) hasTag(ctx context.Context, tag string) bool {
	_, err := runGit(ctx, r.dir, "rev-parse", "--verify", "--quiet", "refs/tags/"+tag+"^{commit}")
	return err == nil
}

// file 读 tag 里的一个文件。文件不在时 ok 为 false、err 为 nil。
func (r *gitRepo) file(ctx context.Context, tag, path string) (data []byte, ok bool, err error) {
	object := tag + ":" + path
	if _, err := runGit(ctx, r.dir, "cat-file", "-e", object); err != nil {
		return nil, false, nil
	}
	data, err = runGit(ctx, r.dir, "cat-file", "blob", object)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// tags 列出仓库里的全部 tag。
func (r *gitRepo) tags(ctx context.Context) ([]string, error) {
	out, err := runGit(ctx, r.dir, "tag", "--list")
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// errNoRepoCache 是"拿不到用户缓存目录"：git 源的仓库缓存没有地方放。
// 给人看的说法在 gitSource.failed 里（这里只是哨兵，不直接显示）。
var errNoRepoCache = errors.New("no user cache directory")

// gitFailure 是一次 git 调用的失败：保留 git 自己的 stderr，原样给使用者看（提案 §9.9）。
type gitFailure struct {
	args   []string
	stderr string
	err    error
}

func (e *gitFailure) Error() string {
	if e.stderr != "" {
		return e.stderr
	}
	return e.err.Error()
}

func (e *gitFailure) Unwrap() error { return e.err }

// runGit 调系统 git。dir 非空时对那个 bare 仓库操作（--git-dir）。
func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if dir != "" {
		args = append([]string{"--git-dir=" + dir}, args...)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && !errors.Is(err, ctxErr) {
			err = errors.Join(ctxErr, err)
		}
		return nil, &gitFailure{args: args, stderr: strings.TrimSpace(stderr.String()), err: err}
	}
	return stdout.Bytes(), nil
}

// repoCacheDir 把仓库地址映射成缓存下的目录：<root>/<主机>/<路径>.git。
//
//	https://github.com/org/erp-api(.git)   github.com/org/erp-api.git
//	git@github.com:org/erp-api.git         github.com/org/erp-api.git（scp 写法，同一个仓库）
//	https://host:8443/a/b                  host_8443/a/b.git（冒号在 Windows 路径里不合法）
//	file:///srv/repos/x、/srv/repos/x      local/srv/repos/x.git
//
// 用户名与 token（https://user:tok@host/…）不进目录名；`..` 这类段直接丢掉，缓存目录
// 永远落在 root 下面。
func repoCacheDir(root, repoURL string) string {
	host, path := splitRepoURL(repoURL)
	hostSegs := cleanSegments(host)
	if len(hostSegs) == 0 {
		hostSegs = []string{"unknown"}
	}
	name := strings.TrimSuffix(strings.Join(cleanSegments(path), "/"), ".git") + ".git"
	return filepath.Join(root, filepath.FromSlash(strings.Join(hostSegs, "_")), filepath.FromSlash(name))
}

// cleanSegments 把地址的一部分拆成能放进目录名的段：冒号换掉，空段、`.`、`..` 丢掉——
// 主机名与路径用同一条规则，缓存目录才永远落在缓存根目录下面。
func cleanSegments(s string) []string {
	var out []string
	for _, seg := range strings.Split(strings.ReplaceAll(s, "\\", "/"), "/") {
		seg = strings.ReplaceAll(seg, ":", "_")
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		out = append(out, seg)
	}
	return out
}

// splitRepoURL 拆出仓库地址的主机与路径。本地路径与 file:// 的主机记为 local。
func splitRepoURL(repoURL string) (host, path string) {
	switch {
	case strings.HasPrefix(repoURL, "file://"):
		return "local", strings.TrimPrefix(repoURL, "file://")
	case strings.Contains(repoURL, "://"):
		if u, err := url.Parse(repoURL); err == nil {
			return u.Host, u.Path
		}
	case isSCPLike(repoURL):
		at := strings.Index(repoURL, "@")
		colon := strings.Index(repoURL, ":")
		return repoURL[at+1 : colon], repoURL[colon+1:]
	}
	return "local", repoURL
}

// isSCPLike 判断 user@host:path 写法（冒号前没有斜杠，也不是 Windows 盘符 C:\…）。
func isSCPLike(s string) bool {
	colon := strings.Index(s, ":")
	if colon <= 1 || strings.Contains(s[:colon], "/") {
		return false
	}
	return strings.Contains(s[:colon], "@")
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
