package skills_test

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"klazomenai/bridge/internal/crew/skills"
)

// MapSource is a test-only Source backed by an in-memory map keyed by
// canonical relative path (e.g. "_universal.md", "github/SKILL.md").
// Used by Compose tests to inject precise document content without
// filesystem or embed.FS dependencies.
//
// Keys are slash-separated (path.Join, not filepath.Join) to match the
// canonical Doc.Path contract — same shape as the production sources
// regardless of host OS.
type MapSource map[string]string

func (m MapSource) Universal() (skills.Doc, error) {
	return m.lookup("_universal.md")
}

func (m MapSource) Skill(name string) (skills.Doc, error) {
	return m.lookup(path.Join(name, "SKILL.md"))
}

func (m MapSource) Profile(name string) (skills.Doc, error) {
	return m.lookup(path.Join(name, "profile.md"))
}

func (m MapSource) lookup(path string) (skills.Doc, error) {
	content, ok := m[path]
	if !ok {
		return skills.Doc{}, skills.ErrNotFound
	}
	return skills.Doc{Path: path, Content: content}, nil
}

// ----------------------------------------------------------------------
// FilesystemSource
// ----------------------------------------------------------------------

func TestFilesystemSourceUniversalReadsFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "_universal.md"), []byte("UNIV"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src := skills.FilesystemSource{Root: dir}
	got, err := src.Universal()
	if err != nil {
		t.Fatalf("Universal: %v", err)
	}
	if got.Content != "UNIV" {
		t.Errorf("Universal content = %q, want %q", got.Content, "UNIV")
	}
	if got.Path != "_universal.md" {
		t.Errorf("Universal path = %q, want %q", got.Path, "_universal.md")
	}
}

func TestFilesystemSourceSkillReadsNestedFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "github"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "github", "SKILL.md"), []byte("SKILL"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src := skills.FilesystemSource{Root: dir}
	got, err := src.Skill("github")
	if err != nil {
		t.Fatalf("Skill: %v", err)
	}
	if got.Content != "SKILL" {
		t.Errorf("Skill content = %q, want %q", got.Content, "SKILL")
	}
}

func TestFilesystemSourceMissingReturnsErrNotFound(t *testing.T) {
	src := skills.FilesystemSource{Root: t.TempDir()}
	_, err := src.Universal()
	if !errors.Is(err, skills.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ----------------------------------------------------------------------
// EmbeddedSource — sanity checks against the bundled dotfiles content
// ----------------------------------------------------------------------

func TestEmbeddedSourceUniversalContainsExpectedSentinel(t *testing.T) {
	src := skills.EmbeddedSource{}
	doc, err := src.Universal()
	if err != nil {
		t.Fatalf("Universal: %v", err)
	}
	// Distinctive H2 heading from claude/profiles/_universal.md;
	// unique vs github/SKILL.md and github/profile.md.
	const sentinel = "Operator Intent Required"
	if !strings.Contains(doc.Content, sentinel) {
		t.Errorf("Universal content missing sentinel %q", sentinel)
	}
}

func TestEmbeddedSourceGitHubSkillContainsExpectedSentinel(t *testing.T) {
	src := skills.EmbeddedSource{}
	doc, err := src.Skill("github")
	if err != nil {
		t.Fatalf("Skill(github): %v", err)
	}
	// Distinctive prose from claude/skills/github/SKILL.md commit-conventions
	// section; unique vs the universal/profile addenda.
	const sentinel = "Conventional commits format"
	if !strings.Contains(doc.Content, sentinel) {
		t.Errorf("github SKILL.md missing sentinel %q", sentinel)
	}
}

func TestEmbeddedSourceGitHubProfileContainsExpectedSentinel(t *testing.T) {
	src := skills.EmbeddedSource{}
	doc, err := src.Profile("github")
	if err != nil {
		t.Fatalf("Profile(github): %v", err)
	}
	// Distinctive PR Lifecycle Gates rule from
	// claude/profiles/github.md; unique vs SKILL.md and universal.
	const sentinel = "must not be exposed as callable tools"
	if !strings.Contains(doc.Content, sentinel) {
		t.Errorf("github profile.md missing sentinel %q", sentinel)
	}
}

func TestEmbeddedSourceUnknownSkillReturnsErrNotFound(t *testing.T) {
	src := skills.EmbeddedSource{}
	_, err := src.Skill("nonexistent-skill")
	if !errors.Is(err, skills.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestEmbeddedSourceProfileForUnknownSkillReturnsErrNotFound(t *testing.T) {
	// Profile() for an unknown skill name (one that has neither SKILL.md
	// nor profile.md in the embedded fixtures) must return ErrNotFound,
	// not a different sentinel — Compose treats this as soft and skips
	// the profile section. The complementary "skill exists but profile
	// missing" semantic is exercised separately in
	// TestFilesystemSourceProfileMissingForExistingSkillReturnsErrNotFound,
	// where a temp-dir fixture lets us pin that contract directly.
	src := skills.EmbeddedSource{}
	_, err := src.Profile("nonexistent-skill")
	if !errors.Is(err, skills.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestFilesystemSourceProfileMissingForExistingSkillReturnsErrNotFound(t *testing.T) {
	// Genuine "skill present, profile absent" coverage: seed only
	// <tmp>/foo/SKILL.md (no profile.md), then assert Profile("foo")
	// returns ErrNotFound. Compose's missing-profile-falls-through
	// branch relies on this exact sentinel.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "foo"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "foo", "SKILL.md"), []byte("SKILL"), 0o600); err != nil {
		t.Fatalf("seed SKILL.md: %v", err)
	}
	// Sanity: skill itself is present.
	if _, err := (skills.FilesystemSource{Root: dir}).Skill("foo"); err != nil {
		t.Fatalf("Skill(foo) seeding broken: %v", err)
	}
	// The contract under test: profile is absent → ErrNotFound.
	_, err := (skills.FilesystemSource{Root: dir}).Profile("foo")
	if !errors.Is(err, skills.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ----------------------------------------------------------------------
// Skill name validation — applied at Source.Skill / Source.Profile
// entry points across all impls. Defence-in-depth against path-traversal
// even though skill names are operator-controlled via crew.yaml today.
// ----------------------------------------------------------------------

func TestSourceRejectsTraversalSkillName(t *testing.T) {
	cases := []string{
		"../etc",
		"../../../etc/passwd",
		"foo/../bar",
		"/absolute",
		"foo/bar",
		`foo\bar`,
		"..",
		".",
		".hidden",
		"",
		"Github",
		"my_skill",
		"foo bar",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			fsSrc := skills.FilesystemSource{Root: t.TempDir()}
			if _, err := fsSrc.Skill(name); !errors.Is(err, skills.ErrInvalidSkillName) {
				t.Errorf("FilesystemSource.Skill(%q): expected ErrInvalidSkillName, got %v", name, err)
			}
			if _, err := fsSrc.Profile(name); !errors.Is(err, skills.ErrInvalidSkillName) {
				t.Errorf("FilesystemSource.Profile(%q): expected ErrInvalidSkillName, got %v", name, err)
			}

			emSrc := skills.EmbeddedSource{}
			if _, err := emSrc.Skill(name); !errors.Is(err, skills.ErrInvalidSkillName) {
				t.Errorf("EmbeddedSource.Skill(%q): expected ErrInvalidSkillName, got %v", name, err)
			}
			if _, err := emSrc.Profile(name); !errors.Is(err, skills.ErrInvalidSkillName) {
				t.Errorf("EmbeddedSource.Profile(%q): expected ErrInvalidSkillName, got %v", name, err)
			}
		})
	}
}

func TestSourceAcceptsValidSkillName(t *testing.T) {
	cases := []string{
		"github",
		"kubernetes",
		"my-skill",
		"k8s",
		"skill1",
		"a",
		"foo-bar-baz",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			// Pins one thing only: validation accepts the name. The
			// downstream lookup outcome varies per case (nil for
			// "github" which exists in embedded fixtures; ErrNotFound
			// for the others) and is irrelevant here — what matters
			// is that the error is NOT ErrInvalidSkillName, proving
			// validation didn't over-reject.
			_, err := skills.EmbeddedSource{}.Skill(name)
			if errors.Is(err, skills.ErrInvalidSkillName) {
				t.Errorf("Skill(%q): unexpectedly rejected as invalid", name)
			}
		})
	}
}

// ----------------------------------------------------------------------
// FallbackSource
// ----------------------------------------------------------------------

func TestFallbackSourceUsesEmbeddedWhenFilesystemMissing(t *testing.T) {
	// Construct a guaranteed-missing path under t.TempDir() rather than
	// hardcoding "/nonexistent/path" (POSIX-only and host-dependent).
	// The TempDir itself exists; the subdir we name does not.
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	src := skills.FallbackSource{
		Primary:   skills.FilesystemSource{Root: missing},
		Secondary: skills.EmbeddedSource{},
	}
	doc, err := src.Skill("github")
	if err != nil {
		t.Fatalf("Skill(github): %v", err)
	}
	const sentinel = "Conventional commits format"
	if !strings.Contains(doc.Content, sentinel) {
		t.Errorf("fallback to embedded missing sentinel %q", sentinel)
	}
}

func TestFallbackSourceUsesFilesystemWhenPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "github"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "github", "SKILL.md"), []byte("OVERRIDE-SENTINEL"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src := skills.FallbackSource{
		Primary:   skills.FilesystemSource{Root: dir},
		Secondary: skills.EmbeddedSource{},
	}
	doc, err := src.Skill("github")
	if err != nil {
		t.Fatalf("Skill(github): %v", err)
	}
	if doc.Content != "OVERRIDE-SENTINEL" {
		t.Errorf("filesystem primary should win, got %q", doc.Content)
	}
}

func TestFallbackSourceRejectsNilPrimaryOrSecondary(t *testing.T) {
	// Misconfiguration (forgetting Primary or Secondary at construction
	// time) should surface as ErrNilSource, not panic at the closure
	// dereference inside tryFallback. Covers BOTH the bare-nil and
	// typed-nil shapes — the latter is the Go gotcha where a non-nil
	// interface holds a nil pointer (`var p *FilesystemSource = nil`)
	// and would otherwise pass an `== nil` check yet panic on call.
	var typedNilFS *skills.FilesystemSource     // nil pointer
	var typedNilSrc skills.Source = typedNilFS  // typed-nil in interface

	cases := []struct {
		name string
		src  skills.FallbackSource
	}{
		{"nil primary", skills.FallbackSource{Primary: nil, Secondary: skills.EmbeddedSource{}}},
		{"nil secondary", skills.FallbackSource{Primary: skills.EmbeddedSource{}, Secondary: nil}},
		{"both nil", skills.FallbackSource{}},
		{"typed-nil primary", skills.FallbackSource{Primary: typedNilSrc, Secondary: skills.EmbeddedSource{}}},
		{"typed-nil secondary", skills.FallbackSource{Primary: skills.EmbeddedSource{}, Secondary: typedNilSrc}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.src.Universal(); !errors.Is(err, skills.ErrNilSource) {
				t.Errorf("Universal: expected ErrNilSource, got %v", err)
			}
			if _, err := tc.src.Skill("github"); !errors.Is(err, skills.ErrNilSource) {
				t.Errorf("Skill: expected ErrNilSource, got %v", err)
			}
			if _, err := tc.src.Profile("github"); !errors.Is(err, skills.ErrNilSource) {
				t.Errorf("Profile: expected ErrNilSource, got %v", err)
			}
		})
	}
}

func TestFallbackSourcePropagatesNonNotFoundErrors(t *testing.T) {
	// If the primary returns a real error (not ErrNotFound), it should
	// short-circuit rather than fall through to the secondary. This
	// avoids masking permission-denied / real I/O errors.
	src := skills.FallbackSource{
		Primary:   errSource{err: errors.New("permission denied")},
		Secondary: skills.EmbeddedSource{},
	}
	_, err := src.Skill("github")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, skills.ErrNotFound) {
		t.Errorf("expected non-ErrNotFound to short-circuit, got wrapped ErrNotFound")
	}
}

// errSource is a test-only Source that always returns a fixed error.
type errSource struct{ err error }

func (e errSource) Universal() (skills.Doc, error)       { return skills.Doc{}, e.err }
func (e errSource) Skill(name string) (skills.Doc, error) { return skills.Doc{}, e.err }
func (e errSource) Profile(name string) (skills.Doc, error) {
	return skills.Doc{}, e.err
}

// ----------------------------------------------------------------------
// FallbackSource happy-path coverage for Universal and Profile
// (Skill is already exercised; Universal and Profile previously only
// hit the validate-error branch via TestFallbackSourceRejectsNil...).
// ----------------------------------------------------------------------

func TestFallbackSourceUniversalFallsBackToSecondary(t *testing.T) {
	// Primary path doesn't exist → ErrNotFound → falls through to
	// EmbeddedSource which has _universal.md.
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	src := skills.FallbackSource{
		Primary:   skills.FilesystemSource{Root: missing},
		Secondary: skills.EmbeddedSource{},
	}
	doc, err := src.Universal()
	if err != nil {
		t.Fatalf("Universal: %v", err)
	}
	const sentinel = "Operator Intent Required"
	if !strings.Contains(doc.Content, sentinel) {
		t.Errorf("fallback Universal missing sentinel %q", sentinel)
	}
}

func TestFallbackSourceProfileFallsBackToSecondary(t *testing.T) {
	// Primary path doesn't exist → ErrNotFound → falls through to
	// EmbeddedSource which has github/profile.md.
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	src := skills.FallbackSource{
		Primary:   skills.FilesystemSource{Root: missing},
		Secondary: skills.EmbeddedSource{},
	}
	doc, err := src.Profile("github")
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	const sentinel = "must not be exposed as callable tools"
	if !strings.Contains(doc.Content, sentinel) {
		t.Errorf("fallback Profile missing sentinel %q", sentinel)
	}
}

func TestFallbackSourceProfileUsesPrimaryWhenPresent(t *testing.T) {
	// Primary has profile.md with distinctive content → must win
	// without falling through. Mirrors TestFallbackSourceUsesFilesystemWhenPresent
	// for the Profile method.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "github"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "github", "profile.md"), []byte("OVERRIDE-PROFILE"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src := skills.FallbackSource{
		Primary:   skills.FilesystemSource{Root: dir},
		Secondary: skills.EmbeddedSource{},
	}
	doc, err := src.Profile("github")
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if doc.Content != "OVERRIDE-PROFILE" {
		t.Errorf("filesystem primary should win, got %q", doc.Content)
	}
}
