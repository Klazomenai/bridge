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
