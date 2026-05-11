# Release Process

## Overview

Bridge uses [release-please](https://github.com/googleapis/release-please) to automate
versioning, changelog generation, and GitHub Releases. Each release builds a Docker image
and pushes it to the GitHub Container Registry (`ghcr.io/klazomenai/bridge`).

## How It Works

1. Conventional commits (`feat:`, `fix:`, etc.) merged to `main` are tracked by
   release-please
2. release-please maintains an open PR that accumulates unreleased changes and updates
   the changelog
3. Merging the release PR creates a GitHub Release with a semver tag
4. The `docker` workflow job builds and pushes the image with a version tag
5. Stable releases (non-alpha, non-beta) also receive the `latest` tag

The release PR acts as a gate — changes accumulate until the team decides to cut a release
by merging the PR. This is not continuous deployment; releases are deliberate.

## Versioning Strategy

Versions follow [Semantic Versioning](https://semver.org/) with prerelease suffixes that
reflect the maturity of each milestone.

| Milestone | prerelease-type | Example Versions | Promotion |
|-----------|----------------|------------------|-----------|
| M2: Full Complement | `alpha` | `0.1.0-alpha`, `0.1.0-alpha.1` | → `0.1.0` |
| M3: Open Ocean | `beta` | `0.2.0-beta`, `0.2.0-beta.1` | → `0.2.0` |
| Future | _(stable)_ | `0.3.0`, `1.0.0` | — |

### Reasoning

- **Alpha** (M2): Internal testing. Breaking changes expected. Image deployed to AKeyRA
  cluster for development.
- **Beta** (M3): Feature-complete, stability-focused. Wider testing with deck-chat clients.
- **Stable** (post-M3): Production-grade. Semver guarantees apply.

### Transitioning Between Phases

Each transition is a one-line change in `release-please-config.json`:

- **Alpha → Beta**: Change `"prerelease-type": "alpha"` to `"prerelease-type": "beta"`
- **Beta → Stable**: Set `"prerelease": false` and remove `"prerelease-type"` and
  `"versioning"` fields

## Configuration Files

| File | Purpose |
|------|---------|
| `release-please-config.json` | Release type, prerelease strategy, changelog sections |
| `.release-please-manifest.json` | Tracks the current released version |
| `.github/workflows/release-please.yml` | Workflow: release-please action + Docker build+push |

## Changelog Sections

Conventional commit types map to changelog sections:

| Commit Type | Changelog Section | Visible |
|-------------|------------------|---------|
| `feat` | Added | Yes |
| `fix` | Fixed | Yes |
| `perf`, `refactor` | Changed | Yes |
| `revert` | Reverted | Yes |
| `security` | Security | Yes |
| `docs`, `style`, `chore`, `test`, `build`, `ci` | _(hidden)_ | No |

## Docker Images

### Registry

Images are published to the GitHub Container Registry:

```
ghcr.io/klazomenai/bridge:<version>
```

### Tagging Strategy

| Release Type | Version Tag | Latest Tag |
|-------------|-------------|------------|
| Alpha | `ghcr.io/klazomenai/bridge:0.1.0-alpha` | No |
| Beta | `ghcr.io/klazomenai/bridge:0.2.0-beta` | No |
| Stable | `ghcr.io/klazomenai/bridge:0.3.0` | Yes |

CI builds (non-release) continue to produce `ci-<sha>` tags via the existing
`ci.yml` workflow.

### Pulling an Image

```bash
# Specific version
docker pull ghcr.io/klazomenai/bridge:0.1.0-alpha

# Latest stable (only available after first stable release)
docker pull ghcr.io/klazomenai/bridge:latest
```

### Kubernetes Deployment

Reference the versioned tag in your deployment manifest:

```yaml
image: ghcr.io/klazomenai/bridge:0.1.0-alpha
```

Avoid using `latest` in production — pin to a specific version for reproducibility.

## Build Details

The Docker image uses a multi-stage build:

1. **Build stage** (`golang:1.25`): Compiles a pure-Go binary with `CGO_ENABLED=0`
   and `-tags goolm` (no libolm C dependency)
2. **Runtime stage** (`gcr.io/distroless/static:nonroot`): Minimal image, runs as
   non-root user (uid 65532)

## Failure Modes and Recovery

The release-please pipeline is mostly hands-off, but there are a handful of
failure modes worth documenting before they bite. Each entry has a diagnostic
and a recovery.

### 1. Release PR fails to open after a merge

**Symptom**: A conventional commit lands on `main`, but no release PR appears
(or the existing release PR doesn't update to include it).

**Diagnose**:
- Check the `Release Please` workflow run for the merge commit. The
  `release-please` job logs will show an action error (typically permissions
  or a config parse error).
- Verify `.github/workflows/release-please.yml` `permissions:` includes
  `contents: write` and `pull-requests: write`.
- Verify `release-please-config.json` parses (`jq . release-please-config.json`).

**Recover**: Fix the underlying issue (revert the bad config commit or grant the
missing permission) and re-run the workflow on the latest `main` commit (Actions
tab → Re-run all jobs). release-please is idempotent on the commit history.

### 2. Release PR is created but Docker push fails

**Symptom**: `release_created == true` in the workflow logs, the GitHub Release
exists with a tag, but the `docker` job in the same workflow run failed.

**Diagnose**:
- Check the `docker` job logs. Common causes: GHCR token transient failure,
  build-push-action network timeout, validation step rejection (see entry 4).

**Recover**: The release and Git tag are already created. Re-run **only the
docker job** (Actions tab → failed run → Re-run failed jobs). The release-please
step is a no-op the second time (`release_created` is sticky to the run).

Do NOT delete and re-create the Git tag — the version is published on the GitHub
Releases API and downstream consumers (if any) may have pinned it.

### 3. Wrong version cut (alpha → beta promotion misconfigured)

**Symptom**: A version was tagged with the wrong prerelease channel (e.g. a beta
release came out as `0.2.0-alpha.1` because `release-please-config.json` was not
updated when transitioning M2 → M3).

**Diagnose**: Check `release-please-config.json` `prerelease-type` field against
the intended phase from the Versioning Strategy table above.

**Recover**: This is the hardest case because release-please's source of truth is
`.release-please-manifest.json` plus the GitHub Releases API; both must agree.

1. Delete the incorrectly-tagged GitHub Release via the UI (Releases page → ⋯ →
   Delete). Do NOT delete the Git tag from the Releases page (the option does
   both); delete only the Release.
2. Delete the Git tag locally and on origin only if the bad tag was just pushed
   and not yet consumed downstream: `git push origin :refs/tags/<bad-tag>`. If
   downstream consumers may have pinned it, prefer leaving the tag and cutting a
   new corrected version forward instead.
3. Edit `release-please-config.json` to the correct `prerelease-type`.
4. Edit `.release-please-manifest.json` to roll back to the last known-good
   version (e.g. revert from `0.2.0-alpha.1` to `0.1.0-alpha.5`).
5. Commit both edits with a conventional `chore:` commit.
6. Wait for the next push to `main` (or push the chore commit) — release-please
   will re-scan the commit history and reopen the release PR with the correct
   channel.

**Warning**: `.release-please-manifest.json` is the source-of-truth that
release-please uses to decide what's next. If the manifest disagrees with the
latest Release in the API, release-please can get stuck in a loop. Always edit
both atomically.

### 4. Validation step (`Validate release-please outputs`) rejects an output

**Symptom**: `release-please` job logs show
`::error::release-please version output '...' does not match expected SemVer pattern`
or `::error::release-please tag_name '...' does not match expected 'vX.Y.Z'`.

**Diagnose**: This means `googleapis/release-please-action@v4` emitted a value
the workflow doesn't recognise. Two common causes:

- `release-please-config.json` was changed to include a non-default `component`
  or custom `separator`, which makes `tag_name` something other than
  `v{semver}`.
- A new release-please-action version (within the `@v4` major) changed output
  formatting. Check the action's CHANGELOG against the deployed version.

**Recover**:

- If the config change was intentional (e.g. monorepo split is being introduced):
  bump the validation regex in `.github/workflows/release-please.yml` to match
  the new format and the local `tag_name` reconstruction logic in the docker
  job's Summary step. Treat as a chore PR; touch both sites.
- If the config change was accidental: revert it; the validation will pass on
  the next run.
- If the action's behaviour drifted: pin the action to a known-good minor
  version (replace `@v4` with `@v4.4.1` etc.) until the upstream is verified.

### 5. `tag_name` and `v${version}` disagree

**Symptom**: Validation step fails with the second error variant above:
`tag_name '...' does not match expected 'vX.Y.Z'`.

**Diagnose**: This is a strict subset of case 4 — release-please-action emitted
a tag_name with a component or non-standard separator. Bridge's
`release-please-config.json` does not set those, so this would indicate either a
config drift or an action-internal change.

**Recover**: Same as case 4. The validation step is the protective layer; treat
its rejection as success-of-the-guardrail rather than a failure to suppress.
