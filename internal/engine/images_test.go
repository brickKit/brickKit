package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// Compose 实现了 Images：只有 docker / podman 有本机镜像这回事。
var _ Images = (*Compose)(nil)

func TestImageExists(t *testing.T) {
	rec := newRecorder()
	rec.output["erp-api:1.0.0"] = "sha256:abc\n"
	ok, err := dockerWith(rec).ImageExists(context.Background(), "erp-api:1.0.0")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "docker image inspect --format {{.Id}} erp-api:1.0.0", rec.lastCall(t))

	rec = newRecorder()
	rec.output["missing:1.0.0"] = "Error response from daemon: No such image: missing:1.0.0"
	rec.fail["missing:1.0.0"] = errors.New("exit status 1")
	ok, err = dockerWith(rec).ImageExists(context.Background(), "missing:1.0.0")
	require.NoError(t, err, "不在本机不是错误")
	assert.False(t, ok)

	rec = newRecorder()
	rec.output["x:1"] = "Cannot connect to the Docker daemon at unix:///var/run/docker.sock"
	rec.fail["x:1"] = errors.New("exit status 1")
	_, err = dockerWith(rec).ImageExists(context.Background(), "x:1")
	require.Error(t, err, "守护进程连不上是错误")
}

func TestImageLabelsParsesJSON(t *testing.T) {
	rec := newRecorder()
	rec.output["shell:1.0.0"] = `{"io.brickkit.shell.members":"erp/a@1.0.0,erp/b@2.0.0"}` + "\n"
	labels, ok, err := dockerWith(rec).ImageLabels(context.Background(), "shell:1.0.0")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "erp/a@1.0.0,erp/b@2.0.0", labels["io.brickkit.shell.members"])
	assert.Equal(t, "docker image inspect --format {{json .Config.Labels}} shell:1.0.0", rec.lastCall(t))

	rec = newRecorder()
	rec.output["plain:1"] = "null\n"
	labels, ok, err = dockerWith(rec).ImageLabels(context.Background(), "plain:1")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Empty(t, labels)

	rec = newRecorder()
	rec.output["gone:1"] = "Error: No such image: gone:1"
	rec.fail["gone:1"] = errors.New("exit status 1")
	_, ok, err = dockerWith(rec).ImageLabels(context.Background(), "gone:1")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestBuildArgv(t *testing.T) {
	req := BuildRequest{
		Tag: "erp-shell:1.0.0", Context: "/src/shell", Dockerfile: "/src/shell/Dockerfile",
		Labels: map[string]string{"io.brickkit.version": "1.0.0", "io.brickkit.component": "erp/shell"},
	}
	rec := newRecorder()
	require.NoError(t, dockerWith(rec).Build(context.Background(), req))
	assert.Equal(t, "docker build -t erp-shell:1.0.0 -f /src/shell/Dockerfile --label io.brickkit.component=erp/shell --label io.brickkit.version=1.0.0 /src/shell", rec.lastCall(t))

	rec = newRecorder()
	require.NoError(t, podmanWith(rec).Build(context.Background(), req))
	assert.True(t, strings.HasPrefix(rec.lastCall(t), "podman build "))
}

func TestBuildFailureKeepsOutputTail(t *testing.T) {
	rec := newRecorder()
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, "step "+strings.Repeat("x", i%3)+string(rune('a'+i%26)))
	}
	lines = append(lines, "ERROR: failed to solve: go build returned 1")
	rec.output["build"] = strings.Join(lines, "\n")
	rec.fail["build"] = errors.New("exit status 1")
	err := dockerWith(rec).Build(context.Background(), BuildRequest{Tag: "t:1", Context: "/c", Dockerfile: "/c/Dockerfile"})
	require.Error(t, err)
	e := clierr.As(err)
	var output string
	for _, d := range e.Details {
		if d.Key == i18n.T(msgid.LabelOutput) {
			output = d.Value
		}
	}
	assert.Contains(t, output, "failed to solve")
	assert.LessOrEqual(t, len(strings.Split(output, "\n")), 20)
}
