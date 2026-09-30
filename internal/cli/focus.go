package cli

// 本文件是焦点运行在命令层的部分：从使用者所在的目录认出"我在哪个组件里"，
// 把焦点写进 deploy.local.yaml，并在每次读它的命令里说一句。
//
// 焦点只写在个人部署文件里，从不只活在命令参数里：早先删掉的 --only 就是"谁启动"
// 在文件之外另有一套判据，up 与 sync 各说各的（提交 f2dc54c）。

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
)

// componentAt 返回 dir 所在的本地组件：dir 在某个本地安装源的 <scope>/<name>/ 下面（更深的子目录也算），
// 且那里有 component.yaml。
//
// 组件目录就是项目根（或者包着项目根）时不算：那是组件自己的工作台——它继承的本地源 ../..
// 正好提供它自己——在那里，"我所在的组件"是项目本身，不是项目里的一个组件。
func componentAt(l project.Layout, decl *projfile.File, dir string) (string, bool) {
	for _, s := range decl.Sources {
		if s.Type != projfile.SourceTypeLocal || s.Path == "" {
			continue
		}
		root := l.Resolve(s.Path)
		rel, err := filepath.Rel(root, dir)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 2 {
			continue
		}
		compDir := filepath.Join(root, parts[0], parts[1])
		if inside, err := filepath.Rel(compDir, l.Root); err == nil && !strings.HasPrefix(inside, "..") {
			continue
		}
		if fileExists(filepath.Join(compDir, manifest.FileName)) {
			return parts[0] + "/" + parts[1], true
		}
	}
	return "", false
}

// componentHere 是使用者所在目录属于的本地组件（在项目根或非组件目录里时 ok 为 false）。
// build、deps 不带参数时用它：在组件目录里说的"这个"，就是它。
func componentHere(opts *Options) (string, bool) {
	l := project.NewLayout(opts.WorkDir)
	decl, err := projfile.ParseFile(l.DeclPath())
	if err != nil {
		return "", false // 读不了就当没有：命令自己会在装载时把错报清楚
	}
	return componentAt(l, decl, opts.CallDir)
}

// applyFocusIntent 把这次 up 的焦点意图落进文件：--all 清掉；--focus 设成它；
// 在组件目录里、两个都没写就设成这个组件；否则不动。written 说这次改没改文件
// （改了就已经说过一句，up 不再重复打焦点的状态行）。
func applyFocusIntent(opts *Options, flags upOptions) (written bool, err error) {
	if flags.focus != "" && flags.all {
		return false, clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliUpFocusAndAll)).
			WithHint(i18n.T(msgid.CliUpHintFocusOrAll)).WithExit(clierr.ExitUsage)
	}
	// 焦点写在 deploy.local.yaml 里，-f 与 --no-local 都不读它：显式要焦点时说清楚，
	// 没显式要（在组件目录里跑）就按这两个参数的意思不碰个人文件
	if opts.DeployFile != "" || opts.NoLocal {
		if flags.focus != "" || flags.all {
			return false, clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliUpFocusNeedsLocal)).
				WithHint(i18n.T(msgid.CliUpHintFocusNeedsLocal)).WithExit(clierr.ExitUsage)
		}
		return false, nil
	}
	l := project.NewLayout(opts.WorkDir)
	if flags.all {
		return clearFocus(opts, l)
	}
	decl, err := projfile.ParseFile(l.DeclPath())
	if err != nil {
		return false, err
	}
	id := flags.focus
	implicit := id == ""
	if implicit {
		var ok bool
		if id, ok = componentAt(l, decl, opts.CallDir); !ok {
			return false, nil
		}
	}
	if !slices.Contains(decl.IDs(), id) {
		return false, withDidYouMean(clierr.New(clierr.CodeComponentNotFound, i18n.T(msgid.ProjectFocusUnknown, id)).
			WithHint(i18n.T(msgid.ProjectHintFocusClear)), id, decl.IDs())
	}
	written, err = ensureFocus(opts, l, id)
	if err != nil && implicit {
		// 焦点是目录给的，不是使用者要的：设不上时先告诉他怎么不带焦点跑整个项目
		e := clierr.As(err)
		e.Hints = append([]string{i18n.T(msgid.CliUpHintImplicitFocus, id)}, e.Hints...)
		return false, e
	}
	return written, err
}

// ensureFocus 把焦点设成 id：本地模式没开就先打开（与 local on 同样复制或沿用），
// 文件里的焦点不同才改，改了就说一句。
//
// 要么全成，要么什么都没变：先记下个人文件、本地模式开关与复制基线的原样，改完按 up 读它的同一套
// 规则装载项目、再看焦点有没有源码可跑；任何一步不成就全部放回，报那一步的错。焦点在 k8s 上、
// 条目写着 mode: disable、没有源码——都不会留下一份之后每条命令都读不了的个人文件。
// 要说的话等全成了才说：还原之后再说"本地模式已开启"就成了假话。
func ensureFocus(opts *Options, l project.Layout, id string) (bool, error) {
	var snap fileSnapshot
	for _, path := range []string{l.DeployLocalPath(), l.LocalModePath(), l.LocalBasePath()} {
		if err := snap.take(path); err != nil {
			return false, err
		}
	}
	notes, written, err := setFocus(l, id)
	if err == nil {
		err = verifyFocus(opts)
	}
	if err != nil {
		snap.restore()
		return false, err
	}
	for _, n := range notes {
		opts.Printf("%s\n", n)
	}
	if written {
		opts.Printf("%s\n", i18n.T(msgid.CliFocusSet, id, project.FileDeployLocal))
	}
	return written, nil
}

// setFocus 打开本地模式（需要时）并写上焦点；返回要说的话，不说。
func setFocus(l project.Layout, id string) (notes []string, written bool, err error) {
	if !fileExists(l.DeployLocalPath()) {
		if err := copyDeployToLocal(l); err != nil {
			return nil, false, err
		}
		if err := project.SetLocalMode(l, true); err != nil {
			return nil, false, localSwitchError(l, err)
		}
		notes = append(notes, i18n.T(msgid.CliLocalOn, project.FileDeployLocal),
			"   "+i18n.T(msgid.CliLocalCopied, project.FileDeployLocal, project.FileDeploy))
	} else if on, err := project.LocalModeOn(l); err != nil {
		return nil, false, err
	} else if !on {
		if err := project.SetLocalMode(l, true); err != nil {
			return nil, false, localSwitchError(l, err)
		}
		notes = append(notes, i18n.T(msgid.CliLocalOn, project.FileDeployLocal),
			"   "+i18n.T(msgid.CliLocalReused, project.FileDeployLocal))
	}
	written, err = writeFocusLine(l, id)
	return notes, written, err
}

// verifyFocus 按 up 读个人文件的同一套规则装载项目（焦点不认识、条目写着 mode: disable、
// 放在 k8s 上都在这里报），再看焦点有没有源码可跑。
func verifyFocus(opts *Options) error {
	proj, err := project.Load(opts.WorkDir, opts.loadOptions())
	if err != nil {
		return err
	}
	return focusSourceError(opts, proj)
}

// focusSourceError：焦点要从源码跑，而本地安装源里找不到它的源码。lint 与 up 设焦点时都问它。
func focusSourceError(opts *Options, proj *project.Project) error {
	id, _, ok := proj.FocusRef()
	if !ok {
		return nil
	}
	if _, found := proj.LocalRepo(id); found {
		return nil
	}
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliUpNoLocalSourceFor, id)).
		WithDetail(i18n.T(msgid.LabelFile), opts.display(proj.DeployPath)).
		WithHint(i18n.T(msgid.CliLintHintFocusSource, id))
}

// clearFocus 删掉个人文件里的焦点；没有个人文件或本来就没写焦点时什么都不做。
func clearFocus(opts *Options, l project.Layout) (bool, error) {
	if !fileExists(l.DeployLocalPath()) {
		return false, nil
	}
	written, err := writeFocusLine(l, "")
	if written {
		opts.Printf("%s\n", i18n.T(msgid.CliFocusCleared))
	}
	return written, err
}

// writeFocusLine 只改 focus: 这一行（deployfile.SetFocus），内容不变就不写；写了返回 true。
func writeFocusLine(l project.Layout, id string) (bool, error) {
	path := l.DeployLocalPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return false, localIOError(path, err)
	}
	out, err := deployfile.SetFocus(data, id)
	if err != nil {
		return false, err
	}
	if bytes.Equal(out, data) {
		return false, nil
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return false, localIOError(path, err)
	}
	return true, nil
}

// renderFocus 在读部署文件的命令里说一句焦点，与本地模式的提醒放在一起。
func renderFocus(opts *Options, proj *project.Project) {
	if proj.Deploy != nil && proj.Deploy.Focus != "" {
		opts.Printf("%s\n", i18n.T(msgid.CliFocusLine, proj.Deploy.Focus))
	}
}
