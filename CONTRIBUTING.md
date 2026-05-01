# Contributing to bridge

Welcome aboard, ship's company. Bridge is the AI crew orchestrator at
the heart of the Offshore Fleet — the Matrix bot that routes a captain's
voice to the crew below decks. Below are the articles, set down so the
rigging stays tidy and the watch stays sharp. Read them once, sign
once, sail with us.

## The Crew's Bargain

Three things we ask of every hand who comes aboard:

1. **Sign the CLA.** Every contributor signs the Contributor Licence
   Agreement once — the bot leaves a comment with the link on your first
   PR. Your copyright stays your own. You grant Klazomenai a perpetual
   sublicensable licence so future relicensing decisions can be made
   cleanly — but **bounded to OSI-approved open-source licences only**.
   The CLA does *not* grant Klazomenai the right to take your contribution
   proprietary or source-available. If that boundary ever needs to move,
   contributors are asked again. Bridge currently sails under the
   [LICENSE.md](LICENSE.md) at the root of the repo (AGPL-3.0-or-later),
   and our commitments to contributors are set down in
   [STEWARDSHIP.md](STEWARDSHIP.md).
2. **Be kind in issues and reviews.** Disagreement is fine. Disagreement
   without respect is not. The watch is small and the world is large.
3. **Talk before you build.** Open an issue for anything bigger than a
   typo so we can chart the work together before the keel is laid.

## The Ship's Watch — Workflow

Aye, here's how we move the work from quayside to mast:

1. **Open an issue first** for anything bigger than a typo. Use the
   `enhancement` or `bug` template — both carry a `🏴‍☠️ Quartermaster's
   notes` section that the maintainer fills out before work starts.
   Keeps everyone aligned on motivation, scope, and acceptance.
2. **Branch off the trunk.** Name the branch
   `<type>/<issue-number>-<short-description>`. Types: `feat`, `fix`,
   `chore`, `refactor`, `docs`, `ci`, `security`, `test`.
3. **Commit in conventional form.**
   [Conventional Commits](https://www.conventionalcommits.org/) — subject
   lines `<type>(scope): <description>`. Optional emoji at the **end** of
   the subject (Conventional Commits parsers handle trailing emoji more
   reliably than leading ones).
4. **Sign your commits** (`git commit --gpg-sign` / `-S`). Branch
   protection requires it.
5. **Open a draft PR** the moment you have a working branch. PRs targeting
   `main` always start as drafts; mark ready when the diff is review-shaped.
6. **Wait for review.** Copilot reviews automatically; the maintainer
   follows. Address review comments in new commits — never amend or
   force-push; we squash on merge.
7. **Squash on merge.** The squash message becomes the canonical history
   entry, so write a good PR description.

## Fitting Out — Local Development

Bridge is Go 1.25, pure-Go olm via `-tags goolm`, no CGO. See the
[docs/](docs/) directory for setup, configuration, and runbooks.

```bash
go test ./...                  # unit tests
go vet ./...                   # static checks
go build ./cmd/bridge          # build the binary
```

CI will run the full test suite plus license-audit checks on every PR.

## The Quartermaster's Conventions

Where the rigging is dressed, every line in its place:

- **Branches off `main`** — no long-lived feature branches.
- **Issue/PR title emojis at the END** of the subject. Type emojis: 🐛 (fix),
  ✨ (feat), 📝 (docs), ♻️ (refactor), 🧪 (test), ⚙️ (chore), 🏗️ (ci),
  🔐 (security), ⚡ (perf), 🎓 (skill), 🗺️ (release).
- **Status emojis in issue/PR bodies**: ✅ Done · ❌ Blocked · ⏸️ Paused ·
  🚧 WIP · 📋 Planned.
- **Maritime preference**: ⛵ over 🚀 · 🏴‍☠️ for milestones · ⚓ for stable.
- **Newlines at EOF**, no trailing whitespace, LF line endings.
- **No emojis in branch names, code, or comments.**

## Labels

- Type: `enhancement`, `bug`, `chore`, `documentation`, `refactor`,
  `techdebt`, `security`, `epic`, `spike`
- Namespace: `app:bridge` (and occasionally `app:akeyra`, `app:deckchat`
  for cross-cutting work)
- Domain: `domain:infra`, `domain:ai`, `domain:android`, `domain:matrix`,
  `domain:security`
- Priority: `priority:high`, `priority:medium`, `priority:low`

## The Black Spot — Reporting Security Issues

For now, Bridge has no separate `SECURITY.md`; report security-sensitive
findings privately via GitHub's [Private Vulnerability Reporting](https://github.com/Klazomenai/bridge/security)
or by direct contact with the maintainer rather than in a public issue.
