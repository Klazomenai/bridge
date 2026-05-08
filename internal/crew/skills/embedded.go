package skills

import "embed"

// embeddedFS is the binary-baked mirror of the dotfiles content. The
// directory layout exactly matches the FilesystemSource expected
// structure so EmbeddedSource and FilesystemSource share the same
// canonical path semantics.
//
// The content is re-synced from a pinned dotfiles ref via
// `make sync-skills` (see Makefile). Drift is caught by CI in
// .github/workflows/skills-drift.yml.
//
//go:embed embedded/*.md embedded/*/*.md
var embeddedFS embed.FS
