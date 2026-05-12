# Bridge — Test Surface

This document maps the layered test surface for verifying the Chips skills
architecture and the Bridge orchestrator's overall correctness. Levels run
from cheapest (static fixtures) to most expensive (live cluster).

The test plan and sequencing live in
[`klazomenai/dotfiles#99`](https://github.com/Klazomenai/dotfiles/issues/99) /
[`klazomenai/bridge#147`](https://github.com/Klazomenai/bridge/issues/147).
This file is the operator-facing reference for *what runs where*.

## Prerequisites

All Bridge Go tests run under the project's standard build flags. The
toolchain version is pinned in `go.mod` (currently **Go 1.25**) — the
`go` directive's version check blocks any older toolchain at command
start (e.g. `go.mod requires go >= 1.25.0`). With `GOTOOLCHAIN=auto`
(the default since Go 1.21) the right toolchain is auto-downloaded;
otherwise the tool exits before reaching the test code.

```sh
CGO_ENABLED=0 go vet -tags goolm ./...
CGO_ENABLED=0 go test -tags goolm ./internal/...
```

`-tags goolm` selects the pure-Go olm implementation (no libolm/CGO);
`CGO_ENABLED=0` keeps the binary statically-linked. Both match the CI
contract — running locally without these will pull in libolm/CGO and
diverge from the supported build path.

## L0 — Static structure (dotfiles)

Validates the dotfiles repo's `claude/skills/` and `claude/profiles/`
layout is structurally correct so Bridge can rely on it. Lives in
dotfiles, not in this repo.

- Script: [`scripts/check-skill-structure.sh`](https://github.com/Klazomenai/dotfiles/blob/main/scripts/check-skill-structure.sh)
- CI: [`lint-skill-structure.yml`](https://github.com/Klazomenai/dotfiles/blob/main/.github/workflows/lint-skill-structure.yml)
- Local: `make check-skills` (in dotfiles checkout)

Asserts every `claude/skills/<name>/SKILL.md` exists, the universal +
github profile addenda have all required H2 sections, and no SKILL.md
references `_universal.md` (the asymmetric reference graph the redesign
deliberately rejected).

## L1 — Bridge unit tests (migrated)

The pre-Compose vendored skill-content path is gone — the `id == "chips"`
gate, the `//go:embed skills/github.md` directive, and the
`chipsGitHubSkill` var all deleted in PR #161 (sub-issue **d** of #148).
Tests migrated to the Compose path:

| Test | Status | Asserts |
|------|--------|---------|
| `TestChipsSystemPromptContainsGitHubSkill` | ✅ migrated | 10 load-bearing rule fragments survive Compose (`--gpg-sign`, `Refs #N`, `NEVER push to "main"`, etc.); boundary literal updated from the legacy `\n\n## Git + GitHub Workflow Rules` to Compose's emitted `\n\n## Github Workflow Rules\n` heading. Lives in `internal/crew/registry_test.go`. |
| `TestNonChipsCrewLackGitHubSkill` | ✅ migrated | Same gating intent under the `skills: []` field in `crew.yaml`. Maren / Crest / Bosun / Lookout don't declare `skills:`, so Compose emits persona+verbosity only and the `Copilot Review Workflow` sentinel is absent from their SystemPrompt. |

## L2 — Composition + tool-loop enforcement (implemented)

The new Source/Compose plumbing AND the orchestrator's enforcement
layer. **Implemented** across sub-issues **a–d** of #148 (PRs #156,
#157+#158, #159+#160, #161, #165). All assertions run on every CI run.

The architecture's value is in the *negative* tests: writes refuse
non-allowlisted repos, mutations require operator intent in the most
recent message, `gh pr merge` / `gh pr ready` aren't even registered
as callable, audit trails redact tokens.

| Test | File | Asserts |
|------|------|---------|
| `TestComposeOrderUniversalThenSkillThenProfile` | `internal/crew/skills/compose_test.go` | Composition order: persona → universal → skill → profile, exact whitespace boundaries |
| `TestComposeBoundaryMarkers` | `internal/crew/skills/compose_test.go` | Each section is preceded by `\n\n## <Heading>\n\n` |
| `TestComposeMissingProfileFallsThrough` | `internal/crew/skills/compose_test.go` | Skills without a profile addendum compose as persona+universal+skill |
| `TestComposeMissingUniversalIsError` | `internal/crew/skills/compose_test.go` | Universal is mandatory when any skill is declared; missing → wrapped `ErrUniversalRequired` |
| `TestComposeMissingSkillIsError` | `internal/crew/skills/compose_test.go` | Missing SKILL.md → wrapped `ErrNotFound` |
| `TestFallbackSourceUsesEmbeddedWhenFilesystemMissing` | `internal/crew/skills/loader_test.go` | `FilesystemSource` ENOENT → `EmbeddedSource` content |
| `TestEmbeddedSourceUniversalContainsExpectedSentinel` | `internal/crew/skills/loader_test.go` | Embedded universal addendum carries a stable sentinel string |
| `TestEmbeddedSourceGitHubSkillContainsExpectedSentinel` | `internal/crew/skills/loader_test.go` | Embedded github/SKILL.md carries a stable sentinel string |
| `TestEmbeddedSourceGitHubProfileContainsExpectedSentinel` | `internal/crew/skills/loader_test.go` | Embedded github/profile.md carries a stable sentinel string |
| `TestEmbeddedSourceContainsAllSkillsDeclaredInRealCrewYAML` | `internal/crew/registry_test.go` | Drift check: every `skills:` entry in `config/crew.yaml` has a resolvable blob in the embedded source |
| `TestChipsPromptContainsUniversalRules` | `internal/crew/registry_test.go` | Sentinels: `Allowlist is fail-closed`, `Operator Intent Required`, `Refused outright`, `Pending-confirmation exception` present |
| `TestChipsPromptContainsGitHubProfileRules` | `internal/crew/registry_test.go` | Sentinels: `must not be exposed as callable tools`, `NEVER autonomously resolve Copilot review threads`, `Refused outright` present |
| `TestNonChipsCrewLackUniversal` | `internal/crew/registry_test.go` | The `## Operator Universal Rules` heading is absent from Maren / Crest / Bosun / Lookout SystemPrompts |
| `TestChipsRefusesNonAllowlistedRepo` | `internal/orchestrator/orchestrator_test.go` | Mock Anthropic emits `tool_use` against non-allowlisted target → orchestrator wraps the tool's allowlist error as `is_error=true` tool_result before Claude sees it |
| `TestChipsRefusesMutationWithoutOperatorIntent` | `internal/orchestrator/orchestrator_test.go` | Chips's SystemPrompt contains the Operator-Intent rule sentinel — the orchestrator sees the rule on every turn |
| `TestPendingConfirmationExceptionAccepts` | `internal/orchestrator/orchestrator_test.go` | Two-turn: "close issue #99" → "confirm?" → "yes" → write proceeds, audit-log captures `"audit: tool invoked"` + `"mutation":true` |
| `TestGhPrMergeNotRegistered` | `internal/tools/registry_test.go` | The production chips registry built via `chipstools.RegisterChipsTools` has `Has("gh_pr_merge") == false && Has("gh_pr_ready") == false`; the 7 expected chips tools ARE present |
| `TestAuditRecordEmittedOnWrite` | `internal/tools/sandbox_test.go` | Mutation:true tool invocation emits structured audit record with the tool name, `mutation:true`, and `argv_redacted` field |
| `TestAuditRecordRedactsTokens` | `internal/tools/sandbox_test.go` | Token-bearing argv → audit-log buffer contains `[REDACTED]`, not the raw token |

L2's mock strategy: register a test-only mock tool via the existing
`tools.Registry` interface. The allowlist gate, mutation-intent rule,
and tool-non-registration assertions all fire *before* any tool's
`Execute()` runs — no real write tool needs to exist for L2 to prove
the architecture's claims.

## L3 — Cluster read-path runbook

A manual runbook executed at AKeyRA cluster cold-starts. Validates the
real DeckChat → Matrix → Bridge → Anthropic → tool execution → response
chain works end-to-end for read-only operations.

- Runbook: `klazomenai/AKeyRA/docs/runbooks/bridge-chips-readpath.md`
  *(skeleton lives in the AKeyRA repo; pending operator availability)*

Steps cover: cluster bring-up via `phase-2-runbook.md`; voice "Chips,
list open PRs in bridge" → expected log lines + response patterns;
negative test ("Chips, list issues in linux") → expected refusal text.
Artefacts (real log captures, real response text) are committed back to
the runbook on first execution.

## L4 — Cluster write-path (deferred)

Validates real `api.github.com` allowlist enforcement, mutation gating
against real GitHub, and audit-trail propagation to Loki. **Deferred**
until:

- AKeyRA #114-#117 land (PAT in Vault, ExternalSecret, per-crew SA,
  per-crew NetworkPolicy)
- Bridge ships at least one write tool (`gh_issue_create` is the
  current candidate)
- L3 has been executed once successfully

Future runbook path: `klazomenai/AKeyRA/docs/runbooks/bridge-chips-writepath.md`.

L4 mirrors L3 plus: `gh_issue_create` against an allowlisted repo
(`klazomenai/bridge` is the current sacrificial target — no synthetic
test-target repo exists; a labelled non-load-bearing issue is the
intended write target); allowlist refusal against an out-of-org repo;
audit reaching Loki; token-bearing argv redacted at Loki rather than
just at stderr.
