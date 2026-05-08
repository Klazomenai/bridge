# Agent Operating Rules — Git + GitHub Workflow

This file is the GitHub-specific agent profile addendum. It is loaded
alongside `_universal.md` for any persona that consumes the `github`
skill.

This file is **intentionally not referenced from
`claude/skills/github/SKILL.md`** — Claude Code never auto-loads it.
Orchestrators (e.g. `klazomenai/bridge`) fetch this file by path at boot.

The universal workflow rules in `claude/skills/github/SKILL.md` apply to
both human Claude Code users and autonomous agents. The cross-cutting
agent rules in `claude/profiles/_universal.md` apply to every persona
regardless of skill. The rules below are the GitHub-specific additions
for autonomous agents.

## PR Lifecycle Gates

The universal SKILL.md says: NEVER run `gh pr merge` or `gh pr ready` —
merging is a human decision after peer review. For autonomous agents
this is hardened from a rule to a tool-registration constraint:

- `gh pr merge` and `gh pr ready` must not be exposed as callable tools
  to the persona. The persona cannot invoke them even with explicit
  operator confirmation. Removing the tool from the registry is the
  correct enforcement, not a runtime refusal.
- `gh pr create` must be exposed only with `--draft` baked in. The
  tool's input schema must reject `draft=false`.

## Pushing Code

The universal SKILL.md says: don't amend, don't force-push, stack on the
same branch. For autonomous agents:

- Pushing code (even to a feature branch the orchestrator has been
  working on) requires explicit operator confirmation per the high-risk
  mutation gating rule in `_universal.md`. The agent does not infer
  push consent from "we just made a fix".
- The agent never invokes `git push --force` or
  `git push --force-with-lease` regardless of operator confirmation —
  these are refused outright.

## Copilot Review Threads

The universal SKILL.md describes the read → discuss → user-decides →
fix → test → push → reply workflow. For autonomous agents:

- NEVER autonomously resolve Copilot review threads. Reply per the
  universal workflow (citing a fix-commit SHA when changes were made;
  disagree-only replies are valid without a SHA); the operator resolves
  the thread.
- "Apply all suggestions" is not a valid action without explicit
  operator consent on each comment.
- Always present each top-level Copilot comment to the operator with
  the agent's assessment (agree / disagree / partial); do not silently
  filter comments out.

## Branch Operations

- The agent never creates a `master` branch. Refused outright.
- The agent never pushes to `main` or the default branch. Refused
  outright, regardless of operator confirmation.
- The agent never amends commits during PR review or rebases a
  published branch. Refused outright.

## Anti-Patterns

- Resolving Copilot review threads autonomously
- Calling `gh pr merge` or `gh pr ready` (these tools must not be
  registered)
- Calling `gh pr create` without `--draft`
- Creating non-draft PRs through any path
- Pushing code without explicit operator confirmation
- Force-pushing or rebasing published branches
- Amending commits during PR review
