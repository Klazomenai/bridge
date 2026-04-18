package bot

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
	"maunium.net/go/mautrix/id"
)

// UserAuthorization maps Matrix user IDs to their permitted crew members.
// A nil value means auth is not configured (fail-closed: deny all).
type UserAuthorization struct {
	users map[id.UserID]*crewPermission
}

type crewPermission struct {
	all  bool                // true = wildcard access to all crew
	crew map[string]struct{} // specific crew IDs permitted
}

// IsAuthorized reports whether sender may invoke crewID.
// Returns false when auth is nil or sender is not listed (fail-closed).
func (a *UserAuthorization) IsAuthorized(sender id.UserID, crewID string) bool {
	if a == nil || len(a.users) == 0 {
		return false
	}
	perm, ok := a.users[sender]
	if !ok {
		return false
	}
	if perm.all {
		return true
	}
	_, ok = perm.crew[crewID]
	return ok
}

// Len returns the number of authorized users, or 0 if nil.
func (a *UserAuthorization) Len() int {
	if a == nil {
		return 0
	}
	return len(a.users)
}

// CrewIDs returns all non-wildcard crew IDs referenced in the auth config.
// Used for validation against the crew registry at startup.
func (a *UserAuthorization) CrewIDs() []string {
	if a == nil {
		return nil
	}
	seen := make(map[string]struct{})
	for _, perm := range a.users {
		for c := range perm.crew {
			seen[c] = struct{}{}
		}
	}
	ids := make([]string, 0, len(seen))
	for c := range seen {
		ids = append(ids, c)
	}
	return ids
}

// authYAML mirrors the YAML file structure.
type authYAML struct {
	AuthorizedUsers map[string]authEntryYAML `yaml:"authorized_users"`
}

type authEntryYAML struct {
	Crews []string `yaml:"crews"`
}

// LoadAuth reads and parses the authorization YAML file.
// Returns nil if path is empty (fail-closed: deny all).
func LoadAuth(path string) (*UserAuthorization, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read auth config: %w", err)
	}
	var raw authYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse auth config: %w", err)
	}
	auth := &UserAuthorization{users: make(map[id.UserID]*crewPermission, len(raw.AuthorizedUsers))}
	for userID, entry := range raw.AuthorizedUsers {
		perm := &crewPermission{crew: make(map[string]struct{}, len(entry.Crews))}
		for _, c := range entry.Crews {
			if c == "*" {
				perm.all = true
			} else {
				perm.crew[c] = struct{}{}
			}
		}
		auth.users[id.UserID(userID)] = perm
	}
	return auth, nil
}

// ValidateAuthCrews checks that every non-wildcard crew ID referenced in auth
// exists in knownCrew. Returns an error naming the first unknown crew, or nil
// if all are valid. When auth is nil, validation is skipped (no config = no
// crew references to validate).
func ValidateAuthCrews(auth *UserAuthorization, knownCrew []string) error {
	if auth == nil {
		return nil
	}
	known := make(map[string]struct{}, len(knownCrew))
	for _, c := range knownCrew {
		known[c] = struct{}{}
	}
	for _, c := range auth.CrewIDs() {
		if _, ok := known[c]; !ok {
			return fmt.Errorf("auth config references unknown crew: %q", c)
		}
	}
	return nil
}
