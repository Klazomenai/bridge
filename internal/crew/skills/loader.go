// Package skills provides the loader/compose surface for Bridge's
// skill-based system-prompt augmentation. It is consumed by
// klazomenai/bridge/internal/crew at registry-load time.
//
// The package implements three Source variants (FilesystemSource,
// EmbeddedSource, FallbackSource) over the documented dotfiles layout:
//
//	_universal.md
//	<skill>/SKILL.md
//	<skill>/profile.md
//
// Compose renders a persona system prompt augmented with the universal
// addendum, per-skill SKILL.md content, and per-skill profile content
// (where present). See compose.go for output format.
package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrNotFound is the sentinel returned by Source implementations when
// a requested document does not exist. Wrapped errors that include it
// are matched via errors.Is.
var ErrNotFound = errors.New("skills: doc not found")

// Doc is a single loaded skill artefact. Path is the source-relative
// canonical key (e.g. "_universal.md", "github/SKILL.md",
// "github/profile.md") used for diagnostics and golden-file comparison.
type Doc struct {
	Path    string
	Content string
}

// Source resolves skill documents by their canonical relative path.
// Implementations mirror the dotfiles layout exactly.
type Source interface {
	// Universal returns the operator-universal addendum (_universal.md).
	// Returns ErrNotFound (wrapped) if absent — Compose treats this as
	// fatal when the calling crew has declared at least one skill.
	Universal() (Doc, error)
	// Skill returns the SKILL.md for a named skill (e.g. "github").
	// Returns ErrNotFound (wrapped) if the source doesn't contain the
	// named skill — Compose treats this as fatal.
	Skill(name string) (Doc, error)
	// Profile returns the per-skill profile addendum (e.g.
	// "github/profile.md"). Returns ErrNotFound (wrapped) if no profile
	// exists for that skill — Compose treats this as soft (skill renders
	// without a profile section).
	Profile(name string) (Doc, error)
}

// FilesystemSource reads from a configurable root path.
//
// Production: /var/lib/klazomenai/skills (set in the Bridge image
// by the Dockerfile dotfiles stage).
// Development: KLAZOMENAI_SKILLS_DIR env var override.
type FilesystemSource struct {
	Root string
}

// Universal returns the universal addendum at <Root>/_universal.md.
func (s FilesystemSource) Universal() (Doc, error) {
	return s.read("_universal.md")
}

// Skill returns <Root>/<name>/SKILL.md.
func (s FilesystemSource) Skill(name string) (Doc, error) {
	return s.read(filepath.Join(name, "SKILL.md"))
}

// Profile returns <Root>/<name>/profile.md.
func (s FilesystemSource) Profile(name string) (Doc, error) {
	return s.read(filepath.Join(name, "profile.md"))
}

func (s FilesystemSource) read(rel string) (Doc, error) {
	full := filepath.Join(s.Root, rel)
	content, err := os.ReadFile(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Doc{}, fmt.Errorf("filesystem source %s: %w", full, ErrNotFound)
		}
		return Doc{}, fmt.Errorf("filesystem source %s: %w", full, err)
	}
	return Doc{Path: rel, Content: string(content)}, nil
}

// EmbeddedSource reads from the //go:embed FS rooted at the package's
// embedded/ subdirectory. Used as the test fallback and as the
// production fallback when FilesystemSource fails (e.g. the image
// mount is missing or the dotfiles ref pinned at build time has not
// been bundled correctly).
type EmbeddedSource struct{}

// Universal reads embedded/_universal.md.
func (EmbeddedSource) Universal() (Doc, error) {
	return readFromFS(embeddedFS, "embedded/_universal.md", "_universal.md")
}

// Skill reads embedded/<name>/SKILL.md.
func (EmbeddedSource) Skill(name string) (Doc, error) {
	rel := filepath.Join(name, "SKILL.md")
	return readFromFS(embeddedFS, "embedded/"+rel, rel)
}

// Profile reads embedded/<name>/profile.md.
func (EmbeddedSource) Profile(name string) (Doc, error) {
	rel := filepath.Join(name, "profile.md")
	return readFromFS(embeddedFS, "embedded/"+rel, rel)
}

func readFromFS(fsys fs.FS, embeddedPath, canonicalPath string) (Doc, error) {
	content, err := fs.ReadFile(fsys, embeddedPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Doc{}, fmt.Errorf("embedded source %s: %w", embeddedPath, ErrNotFound)
		}
		return Doc{}, fmt.Errorf("embedded source %s: %w", embeddedPath, err)
	}
	return Doc{Path: canonicalPath, Content: string(content)}, nil
}

// FallbackSource composes Primary then Secondary. ErrNotFound from
// Primary falls through to Secondary; all other errors short-circuit.
//
// Used in production to layer FilesystemSource (allowing operator
// override via the image's /var/lib/klazomenai/skills mount) over
// EmbeddedSource (the binary-baked fallback).
type FallbackSource struct {
	Primary, Secondary Source
}

// Universal tries Primary, falling back to Secondary on ErrNotFound.
func (f FallbackSource) Universal() (Doc, error) {
	return f.tryFallback(f.Primary.Universal, f.Secondary.Universal)
}

// Skill tries Primary, falling back to Secondary on ErrNotFound.
func (f FallbackSource) Skill(name string) (Doc, error) {
	return f.tryFallback(
		func() (Doc, error) { return f.Primary.Skill(name) },
		func() (Doc, error) { return f.Secondary.Skill(name) },
	)
}

// Profile tries Primary, falling back to Secondary on ErrNotFound.
func (f FallbackSource) Profile(name string) (Doc, error) {
	return f.tryFallback(
		func() (Doc, error) { return f.Primary.Profile(name) },
		func() (Doc, error) { return f.Secondary.Profile(name) },
	)
}

func (FallbackSource) tryFallback(primary, secondary func() (Doc, error)) (Doc, error) {
	doc, err := primary()
	if err == nil {
		return doc, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Doc{}, err
	}
	return secondary()
}
