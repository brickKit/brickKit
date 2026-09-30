package cli

// 本文件是焦点运行在命令层的部分：从使用者所在的目录认出"我在哪个组件里"，
// 把焦点写进 deploy.local.yaml，并在每次读它的命令里说一句。
//
// 焦点只写在个人部署文件里，从不只活在命令参数里：早先删掉的 --only 就是"谁启动"
// 在文件之外另有一套判据，up 与 sync 各说各的（提交 f2dc54c）。

import (
	"bytes"
	"errors"
	"io/fs"
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
		if fileExists(filepath.Join(root, parts[0], parts[1], manifest.FileName)) {
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
func ensureFocus(opts *Options, l project.Layout, id string) (bool, error) {
	// 先确认焦点放得进去，再动任何东西：设不上就不该留下一份刚复制的个人文件、一个打开了的本地模式
	if err := checkFocusFits(l, id); err != nil {
		return false, err
	}
	if !fileExists(l.DeployLocalPath()) {
		if err := copyDeployToLocal(l); err != nil {
			return false, err
		}
		opts.Printf("%s\n", i18n.T(msgid.CliLocalOn, project.FileDeployLocal))
		opts.Printf("   %s\n", i18n.T(msgid.CliLocalCopied, project.FileDeployLocal, project.FileDeploy))
		if err := project.SetLocalMode(l, true); err != nil {
			return false, localSwitchError(l, err)
		}
	} else if on, err := project.LocalModeOn(l); err != nil {
		return false, err
	} else if !on {
		if err := project.SetLocalMode(l, true); err != nil {
			return false, localSwitchError(l, err)
		}
		opts.Printf("%s\n", i18n.T(msgid.CliLocalOn, project.FileDeployLocal))
		opts.Printf("   %s\n", i18n.T(msgid.CliLocalReused, project.FileDeployLocal))
	}
	return writeFocus(opts, l, id)
}

// checkFocusFits 按"写完之后"的个人文件校验一遍：个人文件还没有时，就是 local on 会复制出来的那一份。
func checkFocusFits(l project.Layout, id string) error {
	data, err := os.ReadFile(l.DeployLocalPath())
	if errors.Is(err, fs.ErrNotExist) {
		team, teamErr := os.ReadFile(l.DeployPath())
		if teamErr != nil {
			return localIOError(l.DeployPath(), teamErr)
		}
		data, err = project.LocalDeployContent(team), nil
	}
	if err != nil {
		return localIOError(l.DeployLocalPath(), err)
	}
	return validFocusFile(data, id, l.DeployLocalPath())
}

// validFocusFile 把焦点设进 data，用读它时的同一套校验过一遍（k8s 上设焦点就通不过）。
func validFocusFile(data []byte, id, path string) error {
	out, err := deployfile.SetFocus(data, id)
	if err != nil {
		return err
	}
	_, _, err = deployfile.Parse(out, path, deployfile.RoleLocal)
	return err
}

// clearFocus 删掉个人文件里的焦点；没有个人文件或本来就没写焦点时什么都不做。
func clearFocus(opts *Options, l project.Layout) (bool, error) {
	if !fileExists(l.DeployLocalPath()) {
		return false, nil
	}
	return writeFocus(opts, l, "")
}

// writeFocus 只改 focus: 这一行（deployfile.SetFocus），内容不变就不写；写了返回 true。
func writeFocus(opts *Options, l project.Layout, id string) (bool, error) {
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
	// 写之前用读它时的同一套校验过一遍：通不过就原样报错、一个字节都不写，
	// 否则留下一份之后每条命令都读不了的个人文件
	if _, _, err := deployfile.Parse(out, path, deployfile.RoleLocal); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return false, localIOError(path, err)
	}
	if id == "" {
		opts.Printf("%s\n", i18n.T(msgid.CliFocusCleared))
	} else {
		opts.Printf("%s\n", i18n.T(msgid.CliFocusSet, id, project.FileDeployLocal))
	}
	return true, nil
}

// renderFocus 在读部署文件的命令里说一句焦点，与本地模式的提醒放在一起。
func renderFocus(opts *Options, proj *project.Project) {
	if proj.Deploy != nil && proj.Deploy.Focus != "" {
		opts.Printf("%s\n", i18n.T(msgid.CliFocusLine, proj.Deploy.Focus))
	}
}
