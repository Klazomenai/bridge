package bot

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"maunium.net/go/mautrix/id"
)

func TestIsAuthorizedNil(t *testing.T) {
	var auth *UserAuthorization
	if auth.IsAuthorized("@alice:server", "maren") {
		t.Error("nil auth should deny all (fail-closed)")
	}
}

func TestIsAuthorizedEmptyUsers(t *testing.T) {
	auth := &UserAuthorization{users: map[id.UserID]*crewPermission{}}
	if auth.IsAuthorized("@alice:server", "maren") {
		t.Error("empty users map should deny all")
	}
}

func TestIsAuthorizedWildcard(t *testing.T) {
	auth := &UserAuthorization{users: map[id.UserID]*crewPermission{
		"@captain:server": {all: true},
	}}

	for _, crew := range []string{"maren", "crest", "bosun", "lookout", "chips"} {
		if !auth.IsAuthorized("@captain:server", crew) {
			t.Errorf("wildcard user should be authorized for %q", crew)
		}
	}
}

func TestIsAuthorizedSpecificCrew(t *testing.T) {
	auth := &UserAuthorization{users: map[id.UserID]*crewPermission{
		"@officer:server": {crew: map[string]struct{}{
			"maren": {},
			"crest": {},
		}},
	}}

	if !auth.IsAuthorized("@officer:server", "maren") {
		t.Error("should be authorized for maren")
	}
	if !auth.IsAuthorized("@officer:server", "crest") {
		t.Error("should be authorized for crest")
	}
	if auth.IsAuthorized("@officer:server", "bosun") {
		t.Error("should NOT be authorized for bosun")
	}
}

func TestIsAuthorizedUnknownUser(t *testing.T) {
	auth := &UserAuthorization{users: map[id.UserID]*crewPermission{
		"@captain:server": {all: true},
	}}

	if auth.IsAuthorized("@stranger:server", "maren") {
		t.Error("unknown user should be denied")
	}
}

func TestLenNil(t *testing.T) {
	var auth *UserAuthorization
	if auth.Len() != 0 {
		t.Error("nil auth Len should be 0")
	}
}

func TestLenPopulated(t *testing.T) {
	auth := &UserAuthorization{users: map[id.UserID]*crewPermission{
		"@alice:server": {all: true},
		"@bob:server":   {crew: map[string]struct{}{"maren": {}}},
	}}
	if auth.Len() != 2 {
		t.Errorf("expected Len=2, got %d", auth.Len())
	}
}

func TestCrewIDsNil(t *testing.T) {
	var auth *UserAuthorization
	if ids := auth.CrewIDs(); ids != nil {
		t.Errorf("nil auth CrewIDs should be nil, got %v", ids)
	}
}

func TestCrewIDsReturnsNonWildcard(t *testing.T) {
	auth := &UserAuthorization{users: map[id.UserID]*crewPermission{
		"@captain:server": {all: true},
		"@officer:server": {crew: map[string]struct{}{
			"maren": {},
			"crest": {},
		}},
		"@ensign:server": {crew: map[string]struct{}{
			"crest":   {},
			"lookout": {},
		}},
	}}

	ids := auth.CrewIDs()
	sort.Strings(ids)
	expected := []string{"crest", "lookout", "maren"}
	if len(ids) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, ids)
	}
	for i, id := range ids {
		if id != expected[i] {
			t.Errorf("expected %q at index %d, got %q", expected[i], i, id)
		}
	}
}

func TestLoadAuthEmptyPath(t *testing.T) {
	auth, err := LoadAuth("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth != nil {
		t.Error("empty path should return nil")
	}
}

func TestLoadAuthMissingFile(t *testing.T) {
	_, err := LoadAuth("/nonexistent/auth.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadAuthValidFile(t *testing.T) {
	content := `authorized_users:
  "@captain:server":
    crews: ["*"]
  "@officer:server":
    crews: ["maren", "crest"]
`
	path := filepath.Join(t.TempDir(), "auth.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	auth, err := LoadAuth(path)
	if err != nil {
		t.Fatalf("LoadAuth: %v", err)
	}
	if auth == nil {
		t.Fatal("expected non-nil auth")
	}
	if auth.Len() != 2 {
		t.Errorf("expected 2 users, got %d", auth.Len())
	}
	if !auth.IsAuthorized("@captain:server", "bosun") {
		t.Error("captain should have wildcard access")
	}
	if !auth.IsAuthorized("@officer:server", "maren") {
		t.Error("officer should be authorized for maren")
	}
	if auth.IsAuthorized("@officer:server", "bosun") {
		t.Error("officer should NOT be authorized for bosun")
	}
}

func TestLoadAuthInvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.yaml")
	if err := os.WriteFile(path, []byte("{{invalid"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	_, err := LoadAuth(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}
