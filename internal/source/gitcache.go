package source

// 本文件是 git 源的"仓库这一层"：用户级 bare 仓库缓存、克隆、增量 fetch、
// 从 tag 里读文件。它不认识组件——组件 ID → 仓库地址、版本 → tag 由 git.go 决定。
//
// # 为什么放在用户级目录
//
// 一个组件仓库被多个项目、以及分形架构里的子组件工作台共用；放在每个项目的 .brickkit/
// 下面，同一个仓库会被克隆 N 次。缓存目录按**完整仓库地址**命名：不同组织下同名的仓库
// （github.com/a/erp-api 与 github.com/b/erp-api）不会撞在一起。
//
// # 读取优先级
//
//	1 .brickkit/manifests/ 缓存        Client.Manifest 在进这里之前已经查过
//	2 缓存的 bare 仓库 git show        零网络
//	3 git fetch --tags（增量）          仓库在、但没有要的 tag
//	4 git clone --bare（首次）          仓库从未克隆过
//
// # 鉴权
//
// 平台不碰凭据：调用系统 git，SSH key、credential helper、CI 注入的 token
// 全部由宿主机的 git 配置承担。只做一件事：GIT_TERMINAL_PROMPT=0——没有凭据时让 git
// 直接失败，而不是在 CI 里挂住等一个永远不会来的密码。

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
)

// repoCache 是这一趟运行里用到的全部仓库（按地址），多个 git 源与组件级来源共用。
type repoCache struct {
	root  string
	mu    sync.Mutex
	repos map[string]*gitRepo
	// online 为 true 时（联网的命令：add、upgrade、fetch），读一个仓库之前先 fetch 一次，
	// 本地已经有要的 tag 也一样：tag 被挪动过（同一个版本号重发）就在这时发现。
	online bool
	// moved 收到 fetch 发现的、被挪动过的版本 tag。
	moved func(repoURL string, tags []movedTag)
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
		r = &gitRepo{url: url, root: rc.root, dir: repoCacheDir(rc.root, url), moved: rc.moved}
		rc.repos[url] = r
	}
	return r
}

// movedTag 是 fetch 时发现被挪到另一个提交上的版本 tag。
type movedTag struct {
	tag      string
	from, to string
}

// gitRepo 是缓存里的一个 bare 仓库。
type gitRepo struct {
	url  string
	root string
	dir  string

	mu sync.Mutex
	// fetched 表示这一趟已经从远端取过最新的 tag（刚克隆的也算）。
	fetched bool
	// moved 收到这个仓库被挪动过的版本 tag（repoCache.moved）。
	moved func(repoURL string, tags []movedTag)

	compMu sync.Mutex
	// components 记着每个 tag 里有哪些组件（monorepo 源找子目录用），按 tag 名；fetch 会移动 tag，所以清空。
	components map[string]*tagComponents
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
	sweepStaleClones(tmpRoot)
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

// staleCloneAge 之前开始的临时克隆，是被杀掉的进程留下的：正常的克隆不会进行这么久。
const staleCloneAge = time.Hour

// sweepStaleClones 清掉被杀掉的进程留在 .tmp/ 里的半个克隆。只清放了很久的——
// 同一台机器上另一个项目可能正在克隆。清不掉不要紧，下次再清。
func sweepStaleClones(tmpRoot string) {
	entries, err := os.ReadDir(tmpRoot)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && strings.HasPrefix(e.Name(), "clone-") && time.Since(info.ModTime()) > staleCloneAge {
			_ = os.RemoveAll(filepath.Join(tmpRoot, e.Name()))
		}
	}
}

// fetch 增量取远端的 tag 与分支（这一趟只取一次）。tag 被强制移动过时以远端为准，并且说出来：
// 版本发布了就不该再变，挪过版本 tag 的仓库里，别处缓存着的同一个版本已经是另一份内容了。
//
// 分支也要跟着远端走，不能只取 tag：`clone --bare` 不配 fetch refspec，只带 --tags 的
// fetch 永远不会移动缓存里的分支。而 add --repo 从缓存 clone 出源码目录，它的 origin/*
// 就是缓存里的分支——停在第一次克隆那天的话，之后发的每个版本在那份源码目录看来都
// "不在任何远端上"，remove 会把一份一个字没改的 clone 当成有没推送的提交拦下来。
// --prune 让远端删掉的分支在缓存里也消失（只作用于分支；tag 不受 --prune 影响）。
func (r *gitRepo) fetch(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fetched {
		return nil
	}
	before := r.tagCommits(ctx)
	// 克隆时已经配好了 origin：不再把地址放上命令行
	if _, err := runGit(ctx, r.dir, "fetch", "--quiet", "--tags", "--force", "--prune", "origin",
		"+refs/heads/*:refs/heads/*"); err != nil {
		return err
	}
	r.fetched = true
	if moved := movedVersionTags(before, r.tagCommits(ctx)); len(moved) > 0 && r.moved != nil {
		r.moved(r.url, moved)
	}
	r.compMu.Lock()
	r.components = nil
	r.compMu.Unlock()
	return nil
}

// refresh 是联网的命令在读仓库之前做的那次 fetch：连不上、被拒绝都照用本地已有的——
// 本地有的东西本来就能离线用；本地没有的，接下来那次非 fetch 不可的调用会把错误报出来。
func (r *gitRepo) refresh(ctx context.Context) {
	if err := r.fetch(ctx); err != nil {
		logging.Info(i18n.T(msgid.LogGitRefreshSkipped), "repo", r.url, "error", lastLines(err.Error(), 3))
	}
}

// tagCommits 是仓库里每个 tag 指向的提交（带注释的 tag 取它指向的那个提交）。读不出来时为空。
func (r *gitRepo) tagCommits(ctx context.Context) map[string]string {
	out, err := runGit(ctx, r.dir, "for-each-ref", "--format=%(refname:strip=2) %(objectname) %(*objectname)", "refs/tags")
	if err != nil {
		return nil
	}
	commits := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		switch len(f) {
		case 2:
			commits[f[0]] = f[1]
		case 3:
			commits[f[0]] = f[2]
		}
	}
	return commits
}

// movedVersionTags 是两次之间指向变了的版本 tag（<版本> 或 <scope>-<name>/<版本>），按 tag 排好。
// 别的 tag（latest 之类）本来就会动，不算。
func movedVersionTags(before, after map[string]string) []movedTag {
	var moved []movedTag
	for tag, from := range before {
		to, ok := after[tag]
		if !ok || to == from || !manifest.IsExactVersion(path.Base(tag)) {
			continue
		}
		moved = append(moved, movedTag{tag: tag, from: from, to: to})
	}
	sort.Slice(moved, func(i, j int) bool { return moved[i].tag < moved[j].tag })
	return moved
}

// tagCommit 是 tag 指向的提交；仓库里没有这个 tag 时 ok 为 false。
func (r *gitRepo) tagCommit(ctx context.Context, tag string) (string, bool) {
	out, err := runGit(ctx, r.dir, "rev-parse", "--verify", "--quiet", "refs/tags/"+tag+"^{commit}")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// hasTag 报告仓库里有没有这个 tag（指向一个提交）。
func (r *gitRepo) hasTag(ctx context.Context, tag string) bool {
	_, err := runGit(ctx, r.dir, "rev-parse", "--verify", "--quiet", "refs/tags/"+tag+"^{commit}")
	return err == nil
}

// file 读 tag 里的一个文件。文件不在时 ok 为 false、err 为 nil；git 自己失败了（被取消、
// 仓库坏了）是错误——不能说成"tag 里没有这个文件"，那会把人引向完全不相干的方向。
func (r *gitRepo) file(ctx context.Context, tag, path string) (data []byte, ok bool, err error) {
	// ls-tree 对不存在的路径输出为空、正常退出；对坏掉的仓库或被取消的调用才失败
	listing, err := runGit(ctx, r.dir, "ls-tree", "--name-only", tag, "--", path)
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(string(listing)) == "" {
		return nil, false, nil
	}
	data, err = runGit(ctx, r.dir, "cat-file", "blob", tag+":"+path)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// names 列出 tag 里某个目录下的条目名（不递归）；dir 为空串时是仓库根。
func (r *gitRepo) names(ctx context.Context, tag, dir string) ([]string, error) {
	spec := tag
	if dir != "" {
		spec = tag + ":" + dir
	}
	out, err := runGit(ctx, r.dir, "ls-tree", "--name-only", spec)
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// tagComponents 是一个 tag 的文件树里的全部 component.yaml。
type tagComponents struct {
	// byID 按 metadata.id 列出组件所在的目录（仓库根目录是空串）。
	byID map[string][]string
	// files 是每一份 component.yaml 的目录与 metadata.id（读不懂的 id 为空），按目录排好，报错时列出来。
	files []tagComponentFile
}

type tagComponentFile struct{ dir, id string }

// describe 列出 tag 里的每一份 component.yaml 与它的 id，给"找不到这个组件"的报错用。
func (tc *tagComponents) describe() string {
	if len(tc.files) == 0 {
		return i18n.T(msgid.SourceGitNoComponentFiles)
	}
	parts := make([]string, len(tc.files))
	for i, f := range tc.files {
		id := f.id
		if id == "" {
			id = i18n.T(msgid.SourceGitComponentFileUnreadable)
		}
		parts[i] = displayDir(f.dir) + " (" + id + ")"
	}
	return strings.Join(parts, i18n.T(msgid.ListSeparator))
}

// displayDir 是仓库里的一个目录给人看的样子：仓库根目录写成 "."。
func displayDir(dir string) string {
	if dir == "" {
		return "."
	}
	return dir
}

func displayDirs(dirs []string) []string {
	out := make([]string, len(dirs))
	for i, d := range dirs {
		out[i] = displayDir(d)
	}
	return out
}

// componentDirs 读 tag 的文件树里的全部 component.yaml（manifest.InRepository 认的那些），
// 按 metadata.id 分组（每个 tag 只读一次）。
func (r *gitRepo) componentDirs(ctx context.Context, tag string) (*tagComponents, error) {
	r.compMu.Lock()
	defer r.compMu.Unlock()
	if tc, ok := r.components[tag]; ok {
		return tc, nil
	}
	listing, err := runGit(ctx, r.dir, "ls-tree", "-r", "-z", "--name-only", tag)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(string(listing), "\x00") {
		if manifest.InRepository(p) && !strings.ContainsAny(p, "\n") {
			paths = append(paths, p)
		}
	}
	blobs, err := r.blobs(ctx, tag, paths)
	if err != nil {
		return nil, err
	}
	tc := &tagComponents{byID: map[string][]string{}}
	for _, p := range paths {
		dir := path.Dir(p)
		if dir == "." {
			dir = ""
		}
		var h componentHeader
		if yaml.Unmarshal(blobs[p], &h) != nil {
			h.Metadata.ID = ""
		}
		tc.files = append(tc.files, tagComponentFile{dir: dir, id: h.Metadata.ID})
		if h.Metadata.ID != "" {
			tc.byID[h.Metadata.ID] = append(tc.byID[h.Metadata.ID], dir)
		}
	}
	sort.Slice(tc.files, func(i, j int) bool { return tc.files[i].dir < tc.files[j].dir })
	if r.components == nil {
		r.components = map[string]*tagComponents{}
	}
	r.components[tag] = tc
	return tc, nil
}

// blobs 一次读出 tag 里的多个文件（git cat-file --batch：一个进程，不是每个文件一个）。
func (r *gitRepo) blobs(ctx context.Context, tag string, paths []string) (map[string][]byte, error) {
	out := map[string][]byte{}
	if len(paths) == 0 {
		return out, nil
	}
	var in bytes.Buffer
	for _, p := range paths {
		in.WriteString(tag + ":" + p + "\n")
	}
	data, err := runGitInput(ctx, r.dir, &in, "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	// 每个文件：<对象> <类型> <大小>\n<内容>\n；不存在的是 <名字> missing\n
	for _, p := range paths {
		nl := bytes.IndexByte(data, '\n')
		if nl < 0 {
			return nil, errors.New("git cat-file --batch: truncated output")
		}
		header := strings.Fields(string(data[:nl]))
		data = data[nl+1:]
		if len(header) != 3 {
			continue
		}
		size, err := strconv.Atoi(header[2])
		if err != nil || size+1 > len(data) {
			return nil, errors.New("git cat-file --batch: malformed output")
		}
		out[p] = data[:size]
		data = data[size+1:]
	}
	return out, nil
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

// gitFailure 是一次 git 调用的失败：保留 git 自己的 stderr，原样给使用者看。
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
	return runGitInput(ctx, dir, nil, args...)
}

// runGitInput 与 runGit 一样，只是把 stdin 交给 git。
func runGitInput(ctx context.Context, dir string, stdin io.Reader, args ...string) ([]byte, error) {
	if dir != "" {
		args = append([]string{"--git-dir=" + dir}, args...)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Stdin = stdin
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
