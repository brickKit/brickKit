package cli

// 本文件是焦点运行在命令层的部分（设计 §4）：从使用者所在的目录认出"我在哪个组件里"，
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

// applyFocusIntent 把这次 up 的焦点意图落进文件（设计 §4.3）：--all 清掉；--focus 设成它；
// 在组件目录里、两个都没写就设成这个组件；否则不动。
func applyFocusIntent(opts *Options, flags upOptions) error {
	if flags.focus != "" && flags.all {
		return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliUpFocusAndAll)).
			WithHint(i18n.T(msgid.CliUpHintFocusOrAll)).WithExit(clierr.ExitUsage)
	}
	// 焦点写在 deploy.local.yaml 里，-f 与 --no-local 都不读它：显式要焦点时说清楚，
	// 没显式要（在组件目录里跑）就按这两个参数的意思不碰个人文件
	if opts.DeployFile != "" || opts.NoLocal {
		if flags.focus != "" || flags.all {
			return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliUpFocusNeedsLocal)).
				WithHint(i18n.T(msgid.CliUpHintFocusNeedsLocal)).WithExit(clierr.ExitUsage)
		}
		return nil
	}
	l := project.NewLayout(opts.WorkDir)
	if flags.all {
		return clearFocus(opts, l)
	}
	decl, err := projfile.ParseFile(l.DeclPath())
	if err != nil {
		return err
	}
	id := flags.focus
	if id == "" {
		var ok bool
		if id, ok = componentAt(l, decl, opts.CallDir); !ok {
			return nil
		}
	}
	if !slices.Contains(decl.IDs(), id) {
		return clierr.New(clierr.CodeComponentNotFound, i18n.T(msgid.ProjectFocusUnknown, id)).
			WithHint(i18n.T(msgid.ProjectHintFocusClear))
	}
	return ensureFocus(opts, l, id)
}

// ensureFocus 把焦点设成 id：本地模式没开就先打开（与 local on 同样复制或沿用），
// 文件里的焦点不同才改，改了就说一句。
func ensureFocus(opts *Options, l project.Layout, id string) error {
	if !fileExists(l.DeployLocalPath()) {
		if err := copyDeployToLocal(l); err != nil {
			return err
		}
		opts.Printf("%s\n", i18n.T(msgid.CliLocalOn, project.FileDeployLocal))
		opts.Printf("   %s\n", i18n.T(msgid.CliLocalCopied, project.FileDeployLocal, project.FileDeploy))
		if err := project.SetLocalMode(l, true); err != nil {
			return localSwitchError(l, err)
		}
	} else if on, err := project.LocalModeOn(l); err != nil {
		return err
	} else if !on {
		if err := project.SetLocalMode(l, true); err != nil {
			return localSwitchError(l, err)
		}
		opts.Printf("%s\n", i18n.T(msgid.CliLocalOn, project.FileDeployLocal))
		opts.Printf("   %s\n", i18n.T(msgid.CliLocalReused, project.FileDeployLocal))
	}
	return writeFocus(opts, l, id)
}

// clearFocus 删掉个人文件里的焦点；没有个人文件或本来就没写焦点时什么都不做。
func clearFocus(opts *Options, l project.Layout) error {
	if !fileExists(l.DeployLocalPath()) {
		return nil
	}
	return writeFocus(opts, l, "")
}

// writeFocus 只改 focus: 这一行（deployfile.SetFocus），内容不变就不写。
func writeFocus(opts *Options, l project.Layout, id string) error {
	path := l.DeployLocalPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return localIOError(path, err)
	}
	out, err := deployfile.SetFocus(data, id)
	if err != nil {
		return err
	}
	if bytes.Equal(out, data) {
		return nil
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return localIOError(path, err)
	}
	if id == "" {
		opts.Printf("%s\n", i18n.T(msgid.CliFocusCleared))
	} else {
		opts.Printf("%s\n", i18n.T(msgid.CliFocusSet, id, project.FileDeployLocal))
	}
	return nil
}

// renderFocus 在读部署文件的命令里说一句焦点，与本地模式的提醒放在一起。
func renderFocus(opts *Options, proj *project.Project) {
	if proj.Deploy != nil && proj.Deploy.Focus != "" {
		opts.Printf("%s\n", i18n.T(msgid.CliFocusLine, proj.Deploy.Focus))
	}
}
