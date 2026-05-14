package chips

import (
	"slices"
	"testing"
)

// Internal tests for the buildGhChildEnv helper. These cover the
// pure gating + filter logic without subprocess setup — gh-CLI
// integration is exercised in chips_test.go via a stub binary, but
// edge cases like path-resilient basename matching and exact env
// ordering are easier to pin here.

func TestBuildGhChildEnv_GatedOnGhBasenameAndNonEmptyToken(t *testing.T) {
	parent := []string{"PATH=/usr/bin", "HOME=/root"}

	cases := []struct {
		name      string
		cmdName   string
		token     string
		wantNil   bool
		wantToken string // value of GITHUB_TOKEN= in the returned env, when non-nil
	}{
		{"empty token, gh cmd → nil (inherit)", "gh", "", true, ""},
		{"non-empty token, git cmd → nil", "git", "tok", true, ""},
		{"non-empty token, sh cmd → nil", "sh", "tok", true, ""},
		{"non-empty token, bare 'gh' → inject", "gh", "tok", false, "tok"},
		{"non-empty token, ./gh → inject (basename matches)", "./gh", "tok", false, "tok"},
		{"non-empty token, /usr/bin/gh → inject", "/usr/bin/gh", "tok", false, "tok"},
		{"non-empty token, gh-foo → nil (basename is gh-foo, not gh)", "gh-foo", "tok", true, ""},
		{"non-empty token, ghs → nil (basename is ghs, not gh)", "ghs", "tok", true, ""},
		{"empty token, git cmd → nil", "git", "", true, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildGhChildEnv(parent, tc.cmdName, tc.token)
			if tc.wantNil {
				if got != nil {
					t.Errorf("want nil, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("want non-nil env, got nil")
			}
			if !slices.Contains(got, "GITHUB_TOKEN="+tc.wantToken) {
				t.Errorf("expected GITHUB_TOKEN=%s in env, got %v", tc.wantToken, got)
			}
		})
	}
}

func TestBuildGhChildEnv_FiltersGhTokenAndGithubTokenFromParent(t *testing.T) {
	// Both gh-CLI auth env vars in the parent must be dropped so
	// the injected value is authoritative.
	parent := []string{
		"PATH=/usr/bin",
		"GH_TOKEN=stray_gh_token_must_be_dropped",
		"HOME=/root",
		"GITHUB_TOKEN=stray_github_token_must_be_dropped",
		"BRIDGE_MARKER=ok",
	}

	got := buildGhChildEnv(parent, "gh", "ghp_authoritative")
	if got == nil {
		t.Fatal("expected non-nil env when gating passes")
	}

	for _, e := range got {
		if e == "GH_TOKEN=stray_gh_token_must_be_dropped" {
			t.Error("parent GH_TOKEN leaked through filter")
		}
		if e == "GITHUB_TOKEN=stray_github_token_must_be_dropped" {
			t.Error("parent GITHUB_TOKEN leaked through filter")
		}
	}
	if !slices.Contains(got, "GITHUB_TOKEN=ghp_authoritative") {
		t.Errorf("injected GITHUB_TOKEN not present: %v", got)
	}
	// Non-gh-auth entries must survive.
	for _, want := range []string{"PATH=/usr/bin", "HOME=/root", "BRIDGE_MARKER=ok"} {
		if !slices.Contains(got, want) {
			t.Errorf("expected %q to survive filter, got %v", want, got)
		}
	}
}

func TestBuildGhChildEnv_AppendsAtEndForLastWriteWinsClarity(t *testing.T) {
	// The injected GITHUB_TOKEN= must be the LAST element with that
	// key in the returned env. Linux execve's behaviour when the
	// same key appears multiple times is implementation-defined,
	// but every common libc treats "last wins". The filter step
	// above already prevents duplicates — this test just pins the
	// append-at-end shape so a future refactor that inserts the
	// injection somewhere else still produces the expected ordering.
	parent := []string{"FIRST=a", "SECOND=b"}
	got := buildGhChildEnv(parent, "gh", "tok")
	if got == nil {
		t.Fatal("expected non-nil env")
	}
	last := got[len(got)-1]
	if last != "GITHUB_TOKEN=tok" {
		t.Errorf("last env entry = %q, want GITHUB_TOKEN=tok", last)
	}
}

func TestBuildGhChildEnv_DoesNotMutateParent(t *testing.T) {
	// The parent slice passed in is os.Environ() — must NOT be
	// modified by the helper. Pin via a defensive comparison after
	// the call.
	parent := []string{
		"PATH=/usr/bin",
		"GH_TOKEN=parent_value",
		"GITHUB_TOKEN=parent_value",
		"HOME=/root",
	}
	parentCopy := slices.Clone(parent)
	_ = buildGhChildEnv(parent, "gh", "tok")
	if !slices.Equal(parent, parentCopy) {
		t.Errorf("buildGhChildEnv mutated parent:\n  got:  %v\n  want: %v", parent, parentCopy)
	}
}
