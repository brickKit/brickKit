// Package release 是无 Market 的组件发布：发布一个版本 = 给组件仓库打一个规范的
// tag 并推送。要么本地与远端都有这个 tag，要么就像从没执行过——推送失败时删掉本地 tag。
// 组件写了 release.goModule 时同一个版本有两个 tag（平台的与 Go 的），一起推送、一起回滚。
//
// 与 internal/gitrepo 分开：那个包只做只读查询，并且刻意不读使用者的全局 git 配置；
// 发布要写（tag、push），推送还要用使用者自己的凭据配置（credential helper、SSH），
// 所以这里的 git 命令照常读全局配置。GIT_TERMINAL_PROMPT=0 与 git 安装源一致：
// 要密码时直接失败，而不是卡在一个看不见的提示上。
package release

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/source"
)

// State 是这个版本在仓库里的发布状态。
type State int

const (
	// Unreleased：这个版本还缺 tag（一个都没有，或者 Go 的与平台的只有一个），检查全部通过，可以发布。
	Unreleased State = iota
	// Released：这个版本的每一个 tag 都已经在远端、就在当前提交上——已经发布过了。
	Released
)

// Target 是一次要发布的组件版本。
type Target struct {
	// Dir 是组件目录（component.yaml 所在处）；Display 是报错里给人看的写法（相对当前目录）。
	Dir, Display string
	Manifest     *manifest.Manifest
	// RepoRoot 是组件所在 git 仓库的根；Subpath 是组件目录相对它的路径（斜杠分隔），在根上时为空。
	RepoRoot, Subpath string
	// Tag 是要打的 tag：<版本>，组件在子目录时是 <scope>-<name>/<版本>，
	// 与 git 安装源读取的名字是同一个函数算出来的。
	Tag string
	// GoTag 是写了 release.goModule 时 Go 认的那个 tag：v<版本>，组件在子目录时是 <Subpath>/v<版本>
	// （Go 按模块所在的目录起名，不按组件 ID）。没写这个字段时为空。
	GoTag string
	// Notes 是这个版本的发版说明（Markdown）：不是空白时写进带注释的 tag，使用者 upgrade 时看得到；
	// 空着就打轻量 tag，跟从前一样。
	Notes string
	// remote 是当前分支的上游所在的远端，head 是当前提交，missing 是还没有的 tag（都在 Check 时确定）。
	remote, head string
	missing      []string
}

// Prepare 读取并校验 dir 下的 component.yaml，定位它所在的 git 仓库。只读 component.yaml——
// 同一目录里的 brickkit.yaml 是作者的本地工作台，与发布无关。
func Prepare(dir string) (*Target, error) { return PrepareAs(dir, "") }

// PrepareAs 同 Prepare；display 是报错里显示的目录写法（空串时显示绝对路径）。
func PrepareAs(dir, display string) (*Target, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	if display == "" {
		display = abs
	}
	m, err := manifest.ParseFile(filepath.Join(abs, manifest.FileName))
	if err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	// 模块路径与版本对不上，Go 不认它的 tag，而推出去就收不回来：一个 tag 都不打
	if err := manifest.GoModuleProblem(m, abs); err != nil {
		return nil, err.WithDetail(i18n.T(msgid.LabelDir), display)
	}
	out, err := git(abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseNotARepo)).
			WithDetail(i18n.T(msgid.LabelDir), display).
			WithHint(i18n.T(msgid.ReleaseHintGitInit))
	}
	root := evalSymlinks(out)
	subpath, err := filepath.Rel(root, evalSymlinks(abs))
	if err != nil {
		return nil, clierr.New(clierr.CodeInternal, i18n.T(msgid.ReleaseNotARepo)).WithCause(err).WithHint(i18n.T(msgid.HintInternalBug))
	}
	subpath = filepath.ToSlash(subpath)
	if subpath == "." {
		subpath = ""
	}
	t := &Target{
		Dir: abs, Display: display, Manifest: m, RepoRoot: root, Subpath: subpath,
		Tag: source.VersionTag(m.Metadata.ID, m.Metadata.Version, subpath),
	}
	if m.ReleasesGoModule() {
		t.GoTag = "v" + m.Metadata.Version
		if subpath != "" {
			t.GoTag = subpath + "/" + t.GoTag
		}
	}
	return t, nil
}

// Ref 是 <组件 ID>@<版本>。
func (t *Target) Ref() string { return t.Manifest.Metadata.ID + "@" + t.Manifest.Metadata.Version }

// Tags 是这个版本要有的全部 tag：平台的 Tag，写了 release.goModule 时还有 GoTag。
func (t *Target) Tags() []string {
	if t.GoTag == "" {
		return []string{t.Tag}
	}
	return []string{t.Tag, t.GoTag}
}

// TagsText 是 Tags 给人看的写法，按语言的列举分隔符连起来。
func (t *Target) TagsText() string { return strings.Join(t.Tags(), i18n.T(msgid.ListSeparator)) }

// Check 做发布前的全部检查，一个都不写：
//   - 组件目录里没有未提交的改动（子目录组件只看自己的目录：monorepo 里旁边组件的改动与它无关）；
//   - 当前分支有上游，且没有未推送的提交——tag 指向的提交必须已经在远端的分支历史里；
//   - 每一个 tag（Tags）都不存在（本地与远端都查）。全都已经在当前提交上时返回 Released；
//     有的在、有的不在时是 Unreleased，Publish 只补上缺的；任何一个在别的提交上都是错误。
func (t *Target) Check() (State, error) {
	changed, err := t.uncommitted()
	if err != nil {
		return 0, err
	}
	if len(changed) > 0 {
		return 0, t.withChanged(clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseDirty, t.Ref())).
			WithHint(i18n.T(msgid.ReleaseHintCommit)), changed)
	}

	branch, _ := git(t.RepoRoot, "symbolic-ref", "-q", "--short", "HEAD")
	if branch == "" {
		// 游离 HEAD（CI 的检出常常是）：没有分支就谈不上"已推送到远端分支历史"
		return 0, clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseDetachedHead, t.Ref())).
			WithHint(i18n.T(msgid.ReleaseHintCheckoutBranch))
	}
	remote, _ := git(t.RepoRoot, "config", "--get", "branch."+branch+".remote")
	if remote == "" {
		return 0, clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseNoUpstream, t.Ref())).
			WithDetail(i18n.T(msgid.ReleaseLabelBranch), branch).
			WithHint(i18n.T(msgid.ReleaseHintSetUpstream, branch))
	}
	t.remote = remote
	ahead, err := git(t.RepoRoot, "rev-list", "--count", "@{u}..HEAD")
	if err != nil {
		return 0, t.gitFailed(err)
	}
	if ahead != "0" {
		return 0, clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseUnpushed, t.Ref())).
			WithDetail(i18n.T(msgid.ReleaseLabelBranch), branch).
			WithDetail(i18n.T(msgid.ReleaseLabelUnpushed), ahead).
			WithHint(i18n.T(msgid.ReleaseHintPush))
	}

	head, err := git(t.RepoRoot, "rev-parse", "HEAD")
	if err != nil {
		return 0, t.gitFailed(err)
	}
	t.head = head
	t.missing = nil
	for _, tag := range t.Tags() {
		missing, err := t.checkTag(tag)
		if err != nil {
			return 0, err
		}
		if missing {
			t.missing = append(t.missing, tag)
		}
	}
	if len(t.missing) == 0 {
		return Released, nil
	}
	return Unreleased, nil
}

// checkTag 看一个 tag 在本地与远端的情况：missing 是两边都没有；已经在远端、就在当前提交上时两个返回值都是零值；
// 其余都是错误。发布与否看远端：消费方只取得到远端的 tag，本地多出来的（手打的、回滚失败留下的）不算。
func (t *Target) checkTag(tag string) (missing bool, err error) {
	local, remote, err := t.existingTag(tag)
	if err != nil {
		return false, err
	}
	head := t.head
	switch {
	case local == "" && remote == "":
		return true, nil
	case remote == head && (local == "" || local == head):
		return false, nil
	case remote == "" && local == head:
		return false, clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseTagOnlyLocal, tag, t.Ref())).
			WithHint(i18n.T(msgid.ReleaseHintPushTag, t.remote, tag), i18n.T(msgid.ReleaseHintDropLocalTag, tag))
	}
	at := remote
	if at == "" || at == head {
		at = local
	}
	return false, clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseTagElsewhere, tag)).
		WithDetail(i18n.T(msgid.ReleaseLabelTagAt), short(at)).
		WithDetail(i18n.T(msgid.ReleaseLabelHead), short(head)).
		WithHint(i18n.T(msgid.ReleaseHintBumpVersion, manifest.FileName))
}

// AlreadyReleased 是"这个版本已经发布过"的错误：Check 返回 Released 之后，release 与 Publish 都这样报。
func (t *Target) AlreadyReleased() *clierr.Error {
	e := clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseAlreadyReleased, t.Ref(), t.Tag))
	if t.GoTag != "" {
		e = clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseAlreadyReleasedTags, t.Ref(), t.TagsText()))
	}
	return e.WithHint(i18n.T(msgid.ReleaseHintBumpVersion, manifest.FileName))
}

// uncommitted 是组件目录里未提交的改动，git status --porcelain 的每一行（子目录组件只看自己的目录）。
func (t *Target) uncommitted() ([]string, error) {
	scope := "."
	if t.Subpath != "" {
		scope = t.Subpath
	}
	status, err := git(t.RepoRoot, "status", "--porcelain", "--", scope)
	if err != nil {
		return nil, t.gitFailed(err)
	}
	if status == "" {
		return nil, nil
	}
	lines := strings.Split(status, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return lines, nil
}

// withChanged 在错误上写明是哪个目录、逐个列出改动。
func (t *Target) withChanged(e *clierr.Error, changed []string) *clierr.Error {
	e = e.WithDetail(i18n.T(msgid.LabelDir), t.Display)
	for _, line := range changed {
		e = e.WithDetail(i18n.T(msgid.ReleaseLabelChanged), line)
	}
	return e
}

// VerifyUnchanged 在组件自己的检查（release.checks）跑完之后、打 tag 之前调用：Check 看过的状态还在不在。
// 检查命令可以做任何事——格式化器、生成器、依赖整理工具会改写已跟踪的文件，有的还会自己提交——
// 而 tag 装的是提交：组件目录不干净，或者当前提交换了，检查时用的文件就不是要打 tag 的那些。
// 先 Check：当前提交是它记下的。
func (t *Target) VerifyUnchanged() error {
	changed, err := t.uncommitted()
	if err != nil {
		return err
	}
	if len(changed) > 0 {
		return t.withChanged(clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseDirtyAfterChecks, t.Ref())).
			WithHint(i18n.T(msgid.ReleaseHintChecksChangedFiles)), changed)
	}
	head, err := git(t.RepoRoot, "rev-parse", "HEAD")
	if err != nil {
		return t.gitFailed(err)
	}
	if head != t.head {
		return clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseHeadMovedAfterChecks, t.Ref())).
			WithDetail(i18n.T(msgid.ReleaseLabelCheckedCommit), short(t.head)).
			WithDetail(i18n.T(msgid.ReleaseLabelHead), short(head)).
			WithHint(i18n.T(msgid.ReleaseHintHeadMoved))
	}
	return nil
}

// existingTag 返回 tag 在本地与远端各指向的提交；没有的一侧为空。远端总是要问：
// 本地有这个 tag 不等于它已经发布出去了。
func (t *Target) existingTag(tag string) (local, remote string, err error) {
	if sha, err := git(t.RepoRoot, "rev-parse", "-q", "--verify", "refs/tags/"+tag+"^{commit}"); err == nil {
		local = sha
	}
	out, err := git(t.RepoRoot, "ls-remote", "--tags", t.remote, "refs/tags/"+tag, "refs/tags/"+tag+"^{}")
	if err != nil {
		return "", "", clierr.New(clierr.CodeNetworkUnreachable, i18n.T(msgid.ReleaseRemoteUnreachable, t.remote)).
			WithDetail(i18n.T(msgid.LabelReason), err.Error()).
			WithHint(i18n.T(msgid.ReleaseHintRemote))
	}
	remote = remoteSHA(out)
	return local, remote, nil
}

// remoteSHA 从 ls-remote 的输出取 tag 指向的提交。附注 tag 有两行：tag 对象本身与 ^{} 解引用出的提交，
// 要的是后者。
func remoteSHA(out string) string {
	sha := ""
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if strings.HasSuffix(fields[1], "^{}") || sha == "" {
			sha = fields[0]
		}
	}
	return sha
}

// Publish 打上 Check 发现缺的 tag 并推送，返回推送了哪些；推送失败时删掉这次打的本地 tag，再报推送失败的原因。
// 两个 tag 用同一份说明，用 git push --atomic 一起推：要么都上去，要么都没上去。
// 调用方先 Check：Publish 不重复检查（--local 要先把所有组件都查完再开始打 tag）。
func (t *Target) Publish() ([]string, error) {
	if t.remote == "" {
		state, err := t.Check()
		if err != nil {
			return nil, err
		}
		if state == Released {
			return nil, t.AlreadyReleased()
		}
	}
	var created []string
	for _, tag := range t.missing {
		tagArgs := []string{"tag", tag}
		if strings.TrimSpace(t.Notes) != "" {
			// --cleanup=verbatim：默认会把 # 开头的行当注释删掉，Markdown 的标题就全没了
			tagArgs = []string{"tag", "-a", tag, "--cleanup=verbatim", "-m", strings.TrimRight(t.Notes, "\n")}
		}
		if _, err := git(t.RepoRoot, tagArgs...); err != nil {
			_ = t.deleteLocal(created)
			return nil, t.gitFailed(err)
		}
		created = append(created, tag)
	}
	push := []string{"push"}
	if len(created) > 1 {
		push = append(push, "--atomic")
	}
	push = append(push, t.remote)
	for _, tag := range created {
		push = append(push, "refs/tags/"+tag)
	}
	if _, err := git(t.RepoRoot, push...); err != nil {
		return nil, t.pushFailed(created, err, t.deleteLocal(created))
	}
	return created, nil
}

// pushFailed 是推送失败的错误，说明回滚的结果；一个 tag 与两个 tag 的说法分开写。
func (t *Target) pushFailed(tags []string, err, rollbackErr error) error {
	names, several := strings.Join(tags, i18n.T(msgid.ListSeparator)), len(tags) > 1
	e := clierr.New(clierr.CodeReleasePushFailed, i18n.T(msgid.ReleasePushFailed, names, t.remote, t.Ref()))
	if several {
		e = clierr.New(clierr.CodeReleasePushFailed, i18n.T(msgid.ReleasePushFailedTags, names, t.remote, t.Ref()))
	}
	e = e.WithDetail(i18n.T(msgid.LabelReason), err.Error()).WithCause(err)
	switch {
	case rollbackErr != nil && several:
		return e.WithDetail(i18n.T(msgid.ReleaseLabelRollback), rollbackErr.Error()).
			WithHint(i18n.T(msgid.ReleaseHintDeleteTags, strings.Join(tags, " ")))
	case rollbackErr != nil:
		return e.WithDetail(i18n.T(msgid.ReleaseLabelRollback), rollbackErr.Error()).
			WithHint(i18n.T(msgid.ReleaseHintDeleteTag, names))
	case several:
		return e.WithDetail(i18n.T(msgid.ReleaseLabelRollback), i18n.T(msgid.ReleaseRolledBackTags, names)).
			WithHint(i18n.T(msgid.ReleaseHintRetry))
	}
	return e.WithDetail(i18n.T(msgid.ReleaseLabelRollback), i18n.T(msgid.ReleaseRolledBack, names)).
		WithHint(i18n.T(msgid.ReleaseHintRetry))
}

// deleteLocal 删掉这次打的本地 tag（回滚）。
func (t *Target) deleteLocal(tags []string) error {
	if len(tags) == 0 {
		return nil
	}
	_, err := git(t.RepoRoot, append([]string{"tag", "-d"}, tags...)...)
	return err
}

func (t *Target) gitFailed(err error) error {
	return clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseGitFailed, t.Ref())).
		WithDetail(i18n.T(msgid.LabelReason), err.Error()).WithCause(err).WithHint(i18n.T(msgid.ReleaseHintRunGit))
}

// git 在 dir 里跑一条 git 命令，返回去掉首尾空白的 stdout；失败时错误里带 stderr。
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", &gitError{args: strings.Join(args, " "), msg: msg}
	}
	return strings.TrimSpace(out.String()), nil
}

type gitError struct{ args, msg string }

func (e *gitError) Error() string { return "git " + e.args + ": " + e.msg }

func evalSymlinks(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
