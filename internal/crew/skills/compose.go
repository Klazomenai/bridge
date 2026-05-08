package skills

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

var (
	// ErrEmptyPersona is returned by Compose when the persona argument
	// is empty.
	ErrEmptyPersona = errors.New("compose: empty persona")
	// ErrUniversalRequired is returned by Compose (wrapped around the
	// underlying ErrNotFound) when the universal addendum cannot be
	// loaded but the calling crew has declared at least one skill.
	ErrUniversalRequired = errors.New("compose: universal addendum required when skills declared")
)

// Compose renders the persona system prompt augmented with the universal
// addendum, the per-skill SKILL.md, and (where present) the per-skill
// profile addendum.
//
// When skills is empty, returns the persona unchanged (no universal
// section). When skills contains at least one entry:
//   - Universal addendum is required (ErrUniversalRequired if missing)
//   - Each skill's SKILL.md is required (wrapped error if missing)
//   - Per-skill profile addendums are optional (silently skipped on
//     ErrNotFound)
//
// Output format: persona, then a sequence of "## <Heading>" sections
// separated by exactly one blank line. Section headings:
//   - "## Operator Universal Rules"
//   - "## <Skill> Workflow Rules" (per skill, in declaration order)
//   - "## <Skill> Profile Addendum" (per skill with profile, in
//     declaration order)
//
// Skill heading title-casing is via a simple first-rune uppercase (so
// "github" → "Github"). Proper-noun casing (e.g. "GitHub") is deferred
// to a future SkillMetadata{DisplayName} field — accepted cosmetic
// regression in #148, see plan decisions.
func Compose(persona string, skills []string, src Source) (string, error) {
	if persona == "" {
		return "", ErrEmptyPersona
	}
	// Reject nil src up front, regardless of skills emptiness, so the
	// API contract is "src must be non-nil" rather than the conditional
	// "src may be nil iff skills is empty". isNilSource also catches
	// the typed-nil-in-interface case which `== nil` would miss.
	if isNilSource(src) {
		return "", ErrNilSource
	}

	if len(skills) == 0 {
		return persona, nil
	}

	var b strings.Builder
	b.WriteString(strings.TrimRight(persona, "\n"))

	universal, err := src.Universal()
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// Multi-%w preserves both sentinels in the wrap chain so
			// callers can match either with errors.Is — required by
			// the doc contract on ErrUniversalRequired.
			return "", fmt.Errorf("%w: %w", ErrUniversalRequired, err)
		}
		return "", fmt.Errorf("compose: load universal: %w", err)
	}
	b.WriteString("\n\n## Operator Universal Rules\n\n")
	b.WriteString(strings.TrimSpace(universal.Content))

	for _, name := range skills {
		skill, err := src.Skill(name)
		if err != nil {
			return "", fmt.Errorf("compose: load skill %q: %w", name, err)
		}
		fmt.Fprintf(&b, "\n\n## %s Workflow Rules\n\n", titleCase(name))
		b.WriteString(strings.TrimSpace(skill.Content))

		profile, err := src.Profile(name)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("compose: load profile %q: %w", name, err)
		}
		fmt.Fprintf(&b, "\n\n## %s Profile Addendum\n\n", titleCase(name))
		b.WriteString(strings.TrimSpace(profile.Content))
	}

	return b.String(), nil
}

// titleCase converts "github" → "Github". Proper-noun overrides
// (e.g. "GitHub") deferred to a future SkillMetadata field.
func titleCase(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
