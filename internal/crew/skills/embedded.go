package skills

import "embed"

// embeddedFS is the binary-baked mirror of the dotfiles content. The
// directory layout exactly matches the FilesystemSource expected
// structure so EmbeddedSource and FilesystemSource share the same
// canonical path semantics.
//
// The content is a frozen mirror of klazomenai/dotfiles main HEAD at
// the time this package was authored; until the build/CI infrastructure
// in #148e (Dockerfile multi-stage + Makefile sync-skills target +
// drift CI) lands, bumps are manual: copy the three files from a local
// dotfiles checkout into embedded/ and commit. See #148 for the
// epic-level plan.
//
//go:embed embedded/*.md embedded/*/*.md
var embeddedFS embed.FS
