package skills

import "embed"

// embeddedFS is the binary-baked mirror of the dotfiles skill content.
// The directory layout exactly matches the FilesystemSource expected
// structure so EmbeddedSource and FilesystemSource share the same
// canonical path semantics.
//
// Bumps today are manual: copy the relevant files from a local
// dotfiles checkout into embedded/ and commit alongside any code
// changes that depend on the new content.
//
//go:embed embedded/*.md embedded/*/*.md
var embeddedFS embed.FS
