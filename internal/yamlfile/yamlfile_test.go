package yamlfile_test

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

func TestReadMissingIsNotExist(t *testing.T) {
	_, err := yamlfile.Read(filepath.Join(t.TempDir(), "deploy.yaml"))
	assert.True(t, errors.Is(err, fs.ErrNotExist))
}

func TestDocumentInvalidYAML(t *testing.T) {
	_, err := yamlfile.Document([]byte("a: [1"), "deploy.yaml", false)
	e := clierr.As(err)
	require.NotNil(t, e)
	assert.Equal(t, i18n.T(msgid.LayerNotValidYAML, "deploy.yaml"), e.Message)
}

func TestDocumentEmpty(t *testing.T) {
	doc, err := yamlfile.Document([]byte("# only a comment\n"), "vars.yaml", true)
	require.NoError(t, err)
	assert.Nil(t, doc)

	_, err = yamlfile.Document([]byte(""), "deploy.yaml", false)
	require.Error(t, err)
	assert.Equal(t, i18n.T(msgid.LayerEmpty, "deploy.yaml"), clierr.As(err).Message)
}

func TestDocumentMustBeMapping(t *testing.T) {
	_, err := yamlfile.Document([]byte("- a\n- b\n"), "deploy.yaml", false)
	require.Error(t, err)
}

func TestRequireSequenceAndMapping(t *testing.T) {
	doc, err := yamlfile.Document([]byte("components: {a: 1}\nvars: [1]\nk8s: null\n"), "deploy.yaml", false)
	require.NoError(t, err)
	p := clierr.NewProblemSet(clierr.CodeConfigInvalid, "x")
	yamlfile.RequireSequence(yamlfile.Lookup(doc, "components"), "components", p)
	yamlfile.RequireMapping(yamlfile.Lookup(doc, "vars"), "vars", p)
	yamlfile.RequireMapping(yamlfile.Lookup(doc, "k8s"), "k8s", p)       // null 放行
	yamlfile.RequireMapping(yamlfile.Lookup(doc, "absent"), "absent", p) // 不存在放行
	require.Equal(t, 2, p.Len())
	assert.Equal(t, "components", p.Items()[0].Field)
	assert.Equal(t, "vars", p.Items()[1].Field)
}
