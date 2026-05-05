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
toolchain version is pinned in `go.mod` (currently **Go 1.25**) — running
the suite with an older Go release will fail at module resolution.

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

## L1 — Bridge unit tests

Tests against today's vendored skill-content path (the `id == "chips"`
gate in `internal/crew/registry.go`). These tests will migrate to
`compose_test.go` once the Source/Compose loader rewrite (#148) lands.

Run:

```sh
CGO_ENABLED=0 go test -tags goolm ./internal/crew/...
```

Covered today:

| Test | What it covers | Migration target |
|------|----------------|------------------|
| `TestChipsSystemPromptContainsGitHubSkill` | 10 load-bearing rule fragments survive embedding (`--gpg-sign`, `Refs #N`, `NEVER push to main`, etc.) plus the `\n\n## Git + GitHub Workflow Rules` boundary marker. | `compose_test.go` after #148 — fragments stay; boundary literal updates to match `Compose`'s emitted heading. |
| `TestNonChipsCrewLackGitHubSkill` | Embedding is gated to Chips — Maren / Crest / Bosun / Lookout must not see the github content. | Same gating intent under the new `skills: []` field in `crew.yaml`. |

The test bodies carry `// TODO(#148):` comments flagging the migration
intent so the assertions don't get accidentally deleted or re-written
during the loader refactor.

## L2 — Composition + tool-loop enforcement (pending #148)

Tests the new Source/Compose plumbing AND the orchestrator's
enforcement layer. **Pending Bridge #148** — these test files don't
exist yet; the table below is the planned shape.

The architecture's value is in the *negative* tests: writes refuse
non-allowlisted repos, mutations require operator intent in the most
recent message, `gh pr merge` / `gh pr ready` aren't even registered
as callable, audit trails redact tokens. L2 proves these claims fire.

Planned files: `internal/crew/skills/compose_test.go`,
`internal/crew/skills/loader_test.go` (matching the `loader.go` source
file that hosts the `Source` interface + three implementations), plus
extensions to `internal/crew/registry_test.go` and
`internal/orchestrator/orchestrator_test.go`.

| Planned test | Asserts |
|--------------|---------|
| `TestComposeOrderUniversalThenSkillThenProfile` | Composition order: universal → skill → profile, exact whitespace boundaries |
| `TestComposeMissingProfileFallsThrough` | Skills without a profile addendum compose as universal+skill |
| `TestComposeMissingUniversalIsError` | Universal is mandatory; missing → load-time error |
| `TestFallbackSourceUsesEmbeddedWhenFilesystemMissing` | `FilesystemSource` ENOENT → `EmbeddedSource` content |
| `TestEmbeddedSourceContainsAllSkillsListedInCrewYAML` | Drift check: `crew.yaml` `skills:` entries all have embedded blobs |
| `TestChipsPromptContainsUniversalRules` | "fail-closed", "operator's most recent message", "audit record", "Refused outright" present |
| `TestChipsPromptContainsGitHubProfileRules` | "must not be exposed as callable tools", "draft baked in", "NEVER autonomously resolve Copilot" present |
| `TestChipsPromptDoesNotContainOperatorOnlyContent` | Operator-only sentinel absent from agent-consumed prompt |
| `TestChipsRefusesNonAllowlistedRepo` | Mock Anthropic emits `tool_use` against non-allowlisted target → orchestrator allowlist gate fires before tool `Execute()` |
| `TestChipsRefusesMutationWithoutOperatorIntent` | Mutation not in operator's most recent message → refusal |
| `TestGhPrMergeNotRegistered` | `tools.Registry.Has("gh_pr_merge")` and `gh_pr_ready` return `false` |
| `TestPendingConfirmationExceptionAccepts` | Two-turn: "close issue #99" → "confirm?" → "yes" → write proceeds |
| `TestAuditRecordEmittedOnWrite` | Write tool invocation produces audit record on stderr / captured `io.Writer` |
| `TestAuditRecordRedactsTokens` | Token-bearing argv → audit line shows `***REDACTED***` |

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
