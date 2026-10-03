package release

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
)

// RunChecks 依次跑组件 component.yaml 里 release.checks 声明的命令（见 manifest.Release），
// 第一条失败就停，后面的不跑。release 打 tag 之前、publish 上传之前调用它：失败时什么都没写。
//
// 每条命令在组件目录 dir 下执行，argv 原样传给 exec（不经 shell）；Argv[0] 含路径分隔符时相对组件目录。
// 输出实时接到 stdout / stderr，不攒着——一致性套件可能要跑好几分钟，失败的原因就在它自己的输出里。
// 标准输入不接：检查不该停下来等人。started 在每条命令开始前调用，给调用方打一行"在跑什么"。
func RunChecks(dir, display, ref string, checks [][]string, started func(argv []string), stdout, stderr io.Writer) error {
	for _, argv := range checks {
		started(argv)
		if err := runCheck(dir, argv, stdout, stderr); err != nil {
			return checkFailed(err, display, ref, argv)
		}
	}
	return nil
}

func runCheck(dir string, argv []string, stdout, stderr io.Writer) error {
	name := argv[0]
	if strings.ContainsAny(name, `/\`) && !filepath.IsAbs(name) {
		name = filepath.Join(dir, name)
	}
	cmd := exec.Command(name, argv[1:]...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func checkFailed(err error, display, ref string, argv []string) error {
	command := strings.Join(argv, " ")
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() >= 0 {
		return clierr.New(clierr.CodeReleaseCheckFailed, i18n.T(msgid.ReleaseCheckFailed, command, ref, strconv.Itoa(exit.ExitCode()))).
			WithDetail(i18n.T(msgid.LabelDir), display).
			WithHint(i18n.T(msgid.ReleaseHintFixCheck), i18n.T(msgid.ReleaseHintSkipChecks))
	}
	return clierr.New(clierr.CodeReleaseCheckFailed, i18n.T(msgid.ReleaseCheckNotRun, command, ref)).
		WithDetail(i18n.T(msgid.LabelDir), display).
		WithDetail(i18n.T(msgid.LabelReason), err.Error()).WithCause(err).
		WithHint(i18n.T(msgid.ReleaseHintCheckCommand, manifest.FileName), i18n.T(msgid.ReleaseHintSkipChecks))
}
