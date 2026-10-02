// Package docs carries BrickKit's documentation (docs/en, docs/zh) inside the CLI:
// `brickkit docs` prints the pages of the version that is running, offline.
package docs

import "embed"

// FS holds docs/en and docs/zh. Files starting with "." (.gitkeep) are left out by go:embed.
//
//go:embed en zh
var FS embed.FS
