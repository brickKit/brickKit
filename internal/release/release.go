// Package release 是无 Market 的组件发布（提案 §10.2）：发布一个版本 = 给组件仓库打一个规范的
// tag 并推送。要么本地与远端都有这个 tag，要么就像从没执行过——推送失败时删掉本地 tag。
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
	// Unreleased：还没有这个版本的 tag，检查全部通过，可以发布。
	Unreleased State = iota
	// Released：tag 已经在当前提交上（本地或远端）——这个版本已经发布过了。
	Released
)

// Target 是一次要发布的组件版本。
type Target struct {
	// Dir 是组件目录（component.yaml 所在处）。
	Dir      string
	Manifest *manifest.Manifest
	// RepoRoot 是组件所在 git 仓库的根；Subpath 是组件目录相对它的路径（斜杠分隔），在根上时为空。
	RepoRoot, Subpath string
	// Tag 是要打的 tag：<版本>，组件在子目录时是 <scope>-<name>/<版本>（附录 A9），
	// 与 git 安装源读取的名字是同一个函数算出来的。
	Tag string
	// remote 是当前分支的上游所在的远端（Check 时确定）。
	remote string
}

// Prepare 读取并校验 dir 下的 component.yaml，定位它所在的 git 仓库。只读 component.yaml——
// 同一目录里的 brickkit.yaml 是作者的本地工作台，与发布无关（§16.1.1）。
func Prepare(dir string) (*Target, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	m, err := manifest.ParseFile(filepath.Join(abs, manifest.FileName))
	if err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	out, err := git(abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseNotARepo)).
			WithDetail(i18n.T(msgid.LabelDir), abs).
			WithHint(i18n.T(msgid.ReleaseHintGitInit))
	}
	root := evalSymlinks(out)
	subpath, err := filepath.Rel(root, evalSymlinks(abs))
	if err != nil {
		return nil, clierr.New(clierr.CodeInternal, i18n.T(msgid.ReleaseNotARepo)).WithCause(err)
	}
	subpath = filepath.ToSlash(subpath)
	if subpath == "." {
		subpath = ""
	}
	return &Target{
		Dir: abs, Manifest: m, RepoRoot: root, Subpath: subpath,
		Tag: source.VersionTag(m.Metadata.ID, m.Metadata.Version, subpath),
	}, nil
}

// Ref 是 <组件 ID>@<版本>。
func (t *Target) Ref() string { return t.Manifest.Metadata.ID + "@" + t.Manifest.Metadata.Version }

// Check 做发布前的全部检查，一个都不写（提案 §10.2 第 3、4 步）：
//   - 组件目录里没有未提交的改动（子目录组件只看自己的目录：monorepo 里旁边组件的改动与它无关）；
//   - 当前分支有上游，且没有未推送的提交——tag 指向的提交必须已经在远端的分支历史里；
//   - 这个 tag 不存在（本地与远端都查）。已经在当前提交上时返回 Released；在别的提交上是错误。
func (t *Target) Check() (State, error) {
	scope := "."
	if t.Subpath != "" {
		scope = t.Subpath
	}
	status, err := git(t.RepoRoot, "status", "--porcelain", "--", scope)
	if err != nil {
		return 0, t.gitFailed(err)
	}
	if status != "" {
		e := clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseDirty, t.Ref())).
			WithDetail(i18n.T(msgid.LabelDir), t.Dir)
		for _, line := range strings.Split(status, "\n") {
			e = e.WithDetail(i18n.T(msgid.ReleaseLabelChanged), strings.TrimSpace(line))
		}
		return 0, e.WithHint(i18n.T(msgid.ReleaseHintCommit))
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
			WithHint(i18n.T(msgid.ReleaseHintSetUpstream))
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
	local, remote, err := t.existingTag()
	if err != nil {
		return 0, err
	}
	// 发布与否看远端：消费方只取得到远端的 tag。本地多出来的（手打的、回滚失败留下的）不算
	switch {
	case local == "" && remote == "":
		return Unreleased, nil
	case remote == head && (local == "" || local == head):
		return Released, nil
	case remote == "" && local == head:
		return 0, clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseTagOnlyLocal, t.Tag, t.Ref())).
			WithHint(i18n.T(msgid.ReleaseHintPushTag, t.remote, t.Tag), i18n.T(msgid.ReleaseHintDropLocalTag, t.Tag))
	}
	at := remote
	if at == "" || at == head {
		at = local
	}
	return 0, clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseTagElsewhere, t.Tag)).
		WithDetail(i18n.T(msgid.ReleaseLabelTagAt), short(at)).
		WithDetail(i18n.T(msgid.ReleaseLabelHead), short(head)).
		WithHint(i18n.T(msgid.ReleaseHintBumpVersion, manifest.FileName))
}

// existingTag 返回 tag 在本地与远端各指向的提交；没有的一侧为空。远端总是要问：
// 本地有这个 tag 不等于它已经发布出去了。
func (t *Target) existingTag() (local, remote string, err error) {
	if sha, err := git(t.RepoRoot, "rev-parse", "-q", "--verify", "refs/tags/"+t.Tag+"^{commit}"); err == nil {
		local = sha
	}
	out, err := git(t.RepoRoot, "ls-remote", "--tags", t.remote, "refs/tags/"+t.Tag, "refs/tags/"+t.Tag+"^{}")
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

// Publish 打 tag 并推送；推送失败时删掉本地 tag，再报推送失败的原因（提案 §10.2 第 5、6 步）。
// 调用方先 Check：Publish 不重复检查（--local 要先把所有组件都查完再开始打 tag）。
func (t *Target) Publish() error {
	if t.remote == "" {
		state, err := t.Check()
		if err != nil {
			return err
		}
		if state == Released {
			return clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseAlreadyReleased, t.Ref(), t.Tag)).
				WithHint(i18n.T(msgid.ReleaseHintBumpVersion, manifest.FileName))
		}
	}
	if _, err := git(t.RepoRoot, "tag", t.Tag); err != nil {
		return t.gitFailed(err)
	}
	if _, err := git(t.RepoRoot, "push", t.remote, "refs/tags/"+t.Tag); err != nil {
		_, rollbackErr := git(t.RepoRoot, "tag", "-d", t.Tag)
		e := clierr.New(clierr.CodeReleasePushFailed, i18n.T(msgid.ReleasePushFailed, t.Tag, t.remote, t.Ref())).
			WithDetail(i18n.T(msgid.LabelReason), err.Error())
		if rollbackErr != nil {
			return e.WithDetail(i18n.T(msgid.ReleaseLabelRollback), rollbackErr.Error()).
				WithHint(i18n.T(msgid.ReleaseHintDeleteTag, t.Tag)).WithCause(err)
		}
		return e.WithDetail(i18n.T(msgid.ReleaseLabelRollback), i18n.T(msgid.ReleaseRolledBack, t.Tag)).
			WithHint(i18n.T(msgid.ReleaseHintRetry)).WithCause(err)
	}
	return nil
}

func (t *Target) gitFailed(err error) error {
	return clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseGitFailed, t.Ref())).
		WithDetail(i18n.T(msgid.LabelReason), err.Error()).WithCause(err)
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
