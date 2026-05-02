## Git + GitHub Workflow Rules

These rules apply when you work on git history, GitHub issues, pull requests, or
review threads. They are vendored from the operator's `github` skill — keep them
in sync (see CONTRIBUTING.md `sync-skills` note).

### Commits

- Conventional commits format: `<type>(scope): description`
- Types: `feat`, `fix`, `chore`, `refactor`, `test`, `docs`, `ci`, `perf`, `security`
- Signed commits required: `git commit --gpg-sign`
- Use `Refs #N` in commit body — NEVER `Closes #N`, `Fixes #N`, or `Resolves #N`.
  Closing an issue is a merge-time decision, not yours.
- NO Claude co-author line on private repos. Check visibility with
  `gh repo view --json isPrivate -q '.isPrivate'` before committing.
- NEVER amend commits or force-push. Stack separate signed commits; squash on merge.
- Commit-message emoji prefixes (in the message body, not in branch names):
  ✨ feat | 🐛 fix | 📝 docs | ♻️ refactor | 🧪 test | ⚙️ chore | 🔐 security | 🏗️ ci | ⚡ perf

### Branches

- NEVER push to `main` or the default branch — NO EXCEPTIONS.
- NEVER create a `master` branch.
- Naming: `<type>/<issue>-<description>` e.g. `feat/595-jaeger-tracing`,
  `fix/784-terraform-exit-code`.
- Types: `feat`, `fix`, `chore`, `refactor`, `test`, `docs`, `security`, `spike`.
- Base branch: `main`. Merge style: squash merge. Delete branch after merge.
- No emojis in branch names.

### Pull Requests

- ALWAYS create PRs as draft: `gh pr create --draft`.
- After creating a PR, STOP. Provide the PR URL. Do NOT suggest merging or next steps.
- NEVER run `gh pr merge` or `gh pr ready` — merging is a human decision.
- CI passing does NOT mean ready to merge.
- Conventional commit format for PR titles. Title emojis go at the END.
- PR body format:

  ```
  ## Summary
  <1-3 bullet points>

  ## Test plan
  - [ ] Bulleted checklist of testing TODOs
  ```

- Use temp files for PR bodies to avoid hook false positives:
  write the body to a file, then pass `-F "body=@file"`.

### Issues

- Conventional prefix on issue titles: `feat:`, `fix:`, `chore:`, etc.
- Emojis go at the END of issue titles.
- Type emojis: 🐙 Epic | 🔍 Spike | 📊 Dashboard | 🔔 Alert | 🚪 Gateway | 🔐 Security | 🪦 Decommission
- Status emojis: ✅ Done | ❌ Blocked | ⏸️ Paused | 🚧 WIP | 📋 Planned

### Labels

- Standard: `epic`, `spike`, `bug`, `enhancement`, `refactor`, `techdebt`
- Namespaced: `app:*`, `env:*`, `fun:*`, `ws:*`, `depth:*`, `priority:*`, `provider:*`

### Copilot Review Workflow

When handling Copilot PR review comments:

1. **Read** — `gh api repos/{owner}/{repo}/pulls/{pr}/comments`, filter top-level
   only (where `in_reply_to_id` is null).
2. **Discuss** — present each comment with assessment (agree/disagree/partial),
   explain reasoning with technical justification.
3. **User decides** — wait for explicit confirmation on which comments to address.
4. **Fix locally** — make the code changes.
5. **Test locally before pushing** — run changed code from working tree.
6. **Commit and push** — new commit on same branch (never amend).
7. **Reply inline** — write reply body to temp file, then
   `gh api repos/{owner}/{repo}/pulls/{pr}/comments -X POST -F "body=@$tmpfile" -F in_reply_to=<id>`,
   reference the fix commit SHA.

Rules:

- Push back on wrong suggestions with technical reasoning — not every comment is correct.
- Reply to EVERY top-level comment, even when disagreeing.
- Reference the fix commit SHA in replies where changes were made.

### File Hygiene

- Newlines at EOF.
- No trailing whitespace.
- LF line endings (no CRLF).
- Never commit secrets (`.env`, credentials, keys, tokens).
- Never commit `.terraform/`, `.tfstate`, `node_modules/`, or other generated artifacts.

### Anti-Patterns to Refuse

- Pushing directly to `main` or the default branch.
- Creating `master` branches.
- Using `Closes #N`, `Fixes #N`, or `Resolves #N` in commit messages.
- Adding Claude co-author to private-repo commits.
- Using `gh pr merge` or `gh pr ready` (merging is a human decision).
- Creating non-draft PRs.
- Committing secrets or credentials.
- Using `gh repo create --push` (pushes to main).
- Amending commits during PR review (`--amend`) — stack new signed commits instead.
- Force-pushing (`--force`, `--force-with-lease`).
- Emojis in branch names or code.
