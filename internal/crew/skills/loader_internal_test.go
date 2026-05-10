package skills

// Internal tests reaching unexported helpers (titleCase, readFromFS,
// FilesystemSource.read) for branches that aren't naturally hit
// through the public Source/Compose API. Each test pins a specific
// branch the external test files cannot reach without fragile
// real-world setup (permission-denied filesystems, root/non-root
// dependencies).

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTitleCaseEmpty(t *testing.T) {
	// Compose's per-skill section emits "## <Title> Workflow Rules"
	// where <Title> is titleCase(name). A future caller passing an
	// empty name would index runes[0] on an empty rune slice and
	// panic — the empty-string guard returns "" instead. Callable
	// here directly because the test is in the same package.
	if got := titleCase(""); got != "" {
		t.Errorf(`titleCase("") = %q, want ""`, got)
	}
}

// errFS is a test-only fs.FS whose Open always returns the configured
// error. Used to drive the non-ErrNotFound branch in readFromFS.
type errFS struct{ err error }

func (e errFS) Open(_ string) (fs.File, error) {
	return nil, e.err
}

func TestReadFromFSNonNotFoundError(t *testing.T) {
	// readFromFS must wrap ErrNotFound for missing files (covered by
	// existing tests via embed.FS) AND propagate non-ErrNotFound
	// errors (e.g. permission-denied) without rewriting them as
	// ErrNotFound. Without this, a future fs.FS implementation that
	// returns I/O errors would be silently misreported as missing.
	const realErr = "synthetic-io-failure"
	fsys := errFS{err: errors.New(realErr)}
	_, err := readFromFS(fsys, "anything", "anything")
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Error("non-ErrNotFound must NOT be wrapped as ErrNotFound")
	}
	if !strings.Contains(err.Error(), realErr) {
		t.Errorf("expected underlying error preserved, got %v", err)
	}
}

func TestFilesystemSourceReadNonNotFoundError(t *testing.T) {
	// FilesystemSource.read must wrap ENOENT as ErrNotFound (covered
	// by existing tests) AND propagate other I/O errors. Trigger by
	// pointing Root at a regular file rather than a directory —
	// filepath.Join + os.ReadFile then yields a non-ENOENT error
	// ("not a directory" on Linux) the wrapper must NOT collapse to
	// ErrNotFound.
	dir := t.TempDir()
	notADir := filepath.Join(dir, "regular-file")
	if err := os.WriteFile(notADir, []byte("data"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src := FilesystemSource{Root: notADir}
	_, err := src.read("_universal.md")
	if err == nil {
		t.Fatal("expected error reading inside a regular file")
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("non-ENOENT error must NOT be wrapped as ErrNotFound, got: %v", err)
	}
}
