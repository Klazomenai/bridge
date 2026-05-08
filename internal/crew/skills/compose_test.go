package skills_test

import (
	"errors"
	"strings"
	"testing"

	"klazomenai/bridge/internal/crew/skills"
)

func TestComposeEmptyPersonaReturnsError(t *testing.T) {
	src := MapSource{}
	_, err := skills.Compose("", []string{"github"}, src)
	if !errors.Is(err, skills.ErrEmptyPersona) {
		t.Errorf("expected ErrEmptyPersona, got %v", err)
	}
}

func TestComposeNoSkillsReturnsPersonaUnchanged(t *testing.T) {
	src := MapSource{
		"_universal.md": "should not appear when skills is empty",
	}
	got, err := skills.Compose("PERSONA-X", nil, src)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if got != "PERSONA-X" {
		t.Errorf("Compose with no skills = %q, want %q", got, "PERSONA-X")
	}
	if strings.Contains(got, "should not appear") {
		t.Error("universal content leaked into output despite empty skills")
	}
}

func TestComposeOrderUniversalThenSkillThenProfile(t *testing.T) {
	src := MapSource{
		"_universal.md":     "UNIV-X",
		"github/SKILL.md":   "SKILL-Y",
		"github/profile.md": "PROF-Z",
	}
	out, err := skills.Compose("PERSONA", []string{"github"}, src)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	uIdx := strings.Index(out, "UNIV-X")
	sIdx := strings.Index(out, "SKILL-Y")
	pIdx := strings.Index(out, "PROF-Z")
	if uIdx < 0 || sIdx < 0 || pIdx < 0 {
		t.Fatalf("Compose missing one of UNIV/SKILL/PROF in output:\n%s", out)
	}
	if !(uIdx < sIdx && sIdx < pIdx) {
		t.Errorf("ordering wrong: UNIV=%d SKILL=%d PROF=%d", uIdx, sIdx, pIdx)
	}
}

func TestComposeBoundaryMarkers(t *testing.T) {
	src := MapSource{
		"_universal.md":     "UNIV",
		"github/SKILL.md":   "SKILL",
		"github/profile.md": "PROF",
	}
	out, err := skills.Compose("PERSONA", []string{"github"}, src)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	for _, marker := range []string{
		"\n\n## Operator Universal Rules\n",
		"\n\n## Github Workflow Rules\n",
		"\n\n## Github Profile Addendum\n",
	} {
		if !strings.Contains(out, marker) {
			t.Errorf("missing boundary marker %q\nfull output:\n%s", marker, out)
		}
	}
}

func TestComposeMissingProfileFallsThrough(t *testing.T) {
	src := MapSource{
		"_universal.md":   "UNIV",
		"github/SKILL.md": "SKILL",
		// no profile
	}
	out, err := skills.Compose("PERSONA", []string{"github"}, src)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if !strings.Contains(out, "## Github Workflow Rules") {
		t.Error("expected Workflow Rules section present")
	}
	if strings.Contains(out, "Profile Addendum") {
		t.Error("expected NO Profile Addendum section when profile missing")
	}
}

func TestComposeMissingUniversalIsError(t *testing.T) {
	src := MapSource{
		"github/SKILL.md": "SKILL",
		// no universal
	}
	_, err := skills.Compose("PERSONA", []string{"github"}, src)
	if !errors.Is(err, skills.ErrUniversalRequired) {
		t.Errorf("expected ErrUniversalRequired, got %v", err)
	}
}

func TestComposeMissingSkillIsError(t *testing.T) {
	src := MapSource{
		"_universal.md": "UNIV",
		// no github skill
	}
	_, err := skills.Compose("PERSONA", []string{"github"}, src)
	if !errors.Is(err, skills.ErrNotFound) {
		t.Errorf("expected ErrNotFound (wrapped), got %v", err)
	}
}

func TestComposeMultipleSkillsInDeclarationOrder(t *testing.T) {
	src := MapSource{
		"_universal.md":       "UNIV",
		"github/SKILL.md":     "GH-SKILL",
		"kubernetes/SKILL.md": "K8S-SKILL",
	}
	out, err := skills.Compose("PERSONA", []string{"github", "kubernetes"}, src)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	ghIdx := strings.Index(out, "GH-SKILL")
	k8sIdx := strings.Index(out, "K8S-SKILL")
	if ghIdx < 0 || k8sIdx < 0 {
		t.Fatalf("missing one of skill contents:\n%s", out)
	}
	if ghIdx >= k8sIdx {
		t.Errorf("declaration order not preserved: gh=%d k8s=%d", ghIdx, k8sIdx)
	}
}

func TestComposePreservesPersonaTrimsTrailingNewlines(t *testing.T) {
	src := MapSource{
		"_universal.md":   "UNIV",
		"github/SKILL.md": "SKILL",
	}
	out, err := skills.Compose("PERSONA\n\n\n", []string{"github"}, src)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	// Persona's trailing newlines should be trimmed; persona+universal
	// boundary should be exactly "\n\n".
	if !strings.HasPrefix(out, "PERSONA\n\n## Operator Universal Rules") {
		t.Errorf("persona trim/separator wrong, got prefix:\n%s", out[:60])
	}
}
