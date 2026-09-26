package manifest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/manifest"
)

func withImage(image string, build *manifest.Build) *manifest.Manifest {
	return &manifest.Manifest{
		Metadata:   manifest.Metadata{ID: "erp/backend", Version: "1.2.0"},
		Deployment: manifest.Deployment{Image: image, Build: build},
	}
}

func TestImageRef(t *testing.T) {
	cases := map[string]struct {
		image string
		build *manifest.Build
		want  string
	}{
		"tagged":                {"ghcr.io/org/erp-backend:1.0.0", nil, "ghcr.io/org/erp-backend:1.0.0"},
		"digest":                {"ghcr.io/org/erp@sha256:abc", nil, "ghcr.io/org/erp@sha256:abc"},
		"untagged gets version": {"ghcr.io/org/erp-backend", nil, "ghcr.io/org/erp-backend:1.2.0"},
		"registry port no tag":  {"localhost:5000/erp", nil, "localhost:5000/erp:1.2.0"},
		"build only":            {"", &manifest.Build{}, "erp-backend:1.2.0"},
	}
	for name, tc := range cases {
		assert.Equal(t, tc.want, manifest.ImageRef(withImage(tc.image, tc.build)), name)
	}
}

func TestStandaloneImageAndShell(t *testing.T) {
	assert.True(t, withImage("x", nil).HasStandaloneImage())
	assert.True(t, withImage("", &manifest.Build{}).HasStandaloneImage())
	assert.False(t, withImage("", nil).HasStandaloneImage())

	m := withImage("x", nil)
	assert.False(t, m.IsShell())
	m.Shell = &manifest.Shell{Members: []string{"erp/api"}}
	assert.True(t, m.IsShell())
	assert.True(t, m.CanHost("erp/api"))
	assert.False(t, m.CanHost("erp/other"))
}
