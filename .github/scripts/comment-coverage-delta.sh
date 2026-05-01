#!/usr/bin/env bash
# Post a coverage-delta comment on the current PR.
#
# Usage:
#   .github/scripts/comment-coverage-delta.sh <pr-coverage-profile>
#
# Best-effort: this script never fails the CI run. It tries to fetch the
# most recent main-branch coverage artefact, computes per-package deltas,
# and posts a single comment on the PR. If anything goes wrong (no main
# artefact, gh API hiccup, parser failure), it logs a warning and exits 0.
#
# Required env:
#   GH_TOKEN     — repo read + PR write
#   PR_NUMBER    — pull request number
#
# Optional env:
#   GITHUB_REPOSITORY — owner/repo (auto-set in Actions)

set -uo pipefail

if [[ "${BASH_VERSINFO[0]:-0}" -lt 4 ]]; then
    echo "::warning::comment-coverage-delta.sh requires Bash 4+; got ${BASH_VERSION:-unknown}; skipping" >&2
    echo "  on macOS: brew install bash, then re-run with the Homebrew bash" >&2
    exit 0
fi

# Workdir at script scope — `fetch_main_profile` mkdirs into this and we
# clean up once on EXIT. Avoids accumulating /tmp/tmp.XXXXXX directories
# across local runs. CI runners are ephemeral, but the trap costs nothing
# there either.
WORKDIR=""
cleanup() {
    if [[ -n "$WORKDIR" && -d "$WORKDIR" ]]; then
        rm -rf "$WORKDIR"
    fi
    rm -f /tmp/coverage-comment.md
}
trap cleanup EXIT

if [[ $# -ne 1 ]]; then
    echo "::warning::usage: $0 <pr-coverage-profile>" >&2
    exit 0
fi

pr_profile="$1"

if [[ ! -f "$pr_profile" ]]; then
    echo "::warning::PR coverage profile not found: $pr_profile"
    exit 0
fi

if [[ -z "${PR_NUMBER:-}" ]]; then
    echo "::warning::PR_NUMBER not set; skipping delta comment"
    exit 0
fi

repo="${GITHUB_REPOSITORY:-$(gh repo view --json nameWithOwner --jq .nameWithOwner 2>/dev/null || echo)}"
if [[ -z "$repo" ]]; then
    echo "::warning::could not determine repo; skipping delta comment"
    exit 0
fi

module="$(go list -m)"

# Compute per-package statement coverage from a profile. Output: "<pkg> <pct>"
#
# Mirrors the parser in check-coverage.sh — kept inline rather than sourced
# so this script stays standalone.
per_package() {
    local profile="$1"
    awk -v module="$module" '
        NR == 1 { next }
        {
            split($1, a, ":")
            path = a[1]
            sub(/\/[^\/]+$/, "", path)
            sub("^" module "/", "", path)
            stmts[path] += $2
            if ($3 + 0 > 0) { covered[path] += $2 }
        }
        END {
            for (p in stmts) {
                if (stmts[p] > 0) {
                    printf "%s %.1f\n", p, (covered[p] / stmts[p]) * 100
                }
            }
        }
    ' "$profile"
}

# Try to download the most recent main-branch coverage artefact. Returns
# the path to coverage.out on success, empty string otherwise. Uses the
# script-scope WORKDIR which is cleaned up by the EXIT trap.
fetch_main_profile() {
    WORKDIR="$(mktemp -d)"

    # Find the most recent successful CI run on main.
    local run_id
    run_id="$(gh run list \
        --repo "$repo" \
        --workflow CI \
        --branch main \
        --status success \
        --limit 1 \
        --json databaseId \
        --jq '.[0].databaseId' 2>/dev/null || true)"

    if [[ -z "$run_id" || "$run_id" == "null" ]]; then
        # Warnings go to stderr so command substitution captures only the
        # found path (or empty) on stdout. Otherwise the caller's
        # `main_profile="$(fetch_main_profile)"` would contain warning text.
        echo "::warning::no successful main-branch CI run found for delta" >&2
        return
    fi

    # Pull its coverage artefact. Artefact name in ci.yml is keyed on the
    # commit SHA, not the run id, so take whichever artefact starts with
    # `coverage-push-` from that run; there's only one per run.
    if ! gh run download "$run_id" \
        --repo "$repo" \
        --dir "$WORKDIR" \
        --pattern 'coverage-push-*' >/dev/null 2>&1; then
        echo "::warning::failed to download main coverage artefact for run $run_id" >&2
        return
    fi

    # Find the downloaded coverage.out (first match wins).
    local found
    found="$(find "$WORKDIR" -name 'coverage.out' -type f | head -1)"
    if [[ -n "$found" ]]; then
        echo "$found"
    fi
}

main_profile="$(fetch_main_profile)"

# Build the comment body.
# shellcheck disable=SC2016 # printf format strings contain literal backticks (markdown code-formatting), not command substitution
{
    echo "## 📊 Coverage report"
    echo ""

    if [[ -n "$main_profile" && -f "$main_profile" ]]; then
        echo "Per-package statement coverage on this PR vs the most recent successful \`main\` build."
        echo ""
        echo "| Package | Main | PR | Δ |"
        echo "|---|---|---|---|"

        # Build associative arrays for both profiles, then walk.
        declare -A pr_cov main_cov
        while read -r pkg pct; do pr_cov["$pkg"]="$pct"; done < <(per_package "$pr_profile")
        while read -r pkg pct; do main_cov["$pkg"]="$pct"; done < <(per_package "$main_profile")

        # Union of packages, sorted for stable output.
        for pkg in $(printf '%s\n%s\n' "${!pr_cov[@]}" "${!main_cov[@]}" | sort -u); do
            m="${main_cov[$pkg]:-}"
            p="${pr_cov[$pkg]:-}"
            if [[ -z "$m" ]]; then
                printf '| `%s` | — | %s%% | new |\n' "$pkg" "$p"
            elif [[ -z "$p" ]]; then
                printf '| `%s` | %s%% | — | removed |\n' "$pkg" "$m"
            else
                delta="$(awk "BEGIN { printf \"%+.1f\", $p - $m }")"
                arrow="="
                if   awk "BEGIN { exit !($p > $m + 0.05) }"; then arrow="🔼"
                elif awk "BEGIN { exit !($p < $m - 0.05) }"; then arrow="🔽"
                fi
                printf '| `%s` | %s%% | %s%% | %s %s |\n' "$pkg" "$m" "$p" "$arrow" "$delta"
            fi
        done
    else
        echo "Per-package statement coverage on this PR. (No main-branch baseline available; showing current values only.)"
        echo ""
        echo "| Package | Coverage |"
        echo "|---|---|"
        per_package "$pr_profile" | sort | while read -r pkg pct; do
            printf '| `%s` | %s%% |\n' "$pkg" "$pct"
        done
    fi

    echo ""
    echo "Thresholds enforced via \`.github/coverage-thresholds.yaml\`. Override with \`[allow-coverage-drop]\` in the PR body for deliberate drops."
    echo ""
    echo "<sub>Posted by \`.github/scripts/comment-coverage-delta.sh\`. Best-effort; not gating.</sub>"
} > /tmp/coverage-comment.md

# Post (or update) the comment. We use a marker comment to avoid spamming
# the PR on every push — find an existing comment with the marker and edit
# it; otherwise create a new one.
marker='<!-- bridge-coverage-delta-comment -->'
echo "$marker" >> /tmp/coverage-comment.md

# Paginate the comments lookup. Default page size is 30; PRs that go through
# multiple Copilot review rounds easily exceed that, and a missed marker on
# page 2+ would cause this script to spam a fresh comment per push instead
# of updating the existing one.
existing_comment_id="$(gh api \
    --paginate \
    "repos/${repo}/issues/${PR_NUMBER}/comments?per_page=100" \
    --jq ".[] | select(.body | contains(\"$marker\")) | .id" 2>/dev/null \
    | head -1 || true)"

if [[ -n "$existing_comment_id" ]]; then
    if gh api \
            "repos/${repo}/issues/comments/${existing_comment_id}" \
            -X PATCH \
            -F body=@/tmp/coverage-comment.md >/dev/null; then
        echo "::notice::updated coverage comment ($existing_comment_id)"
    else
        echo "::warning::failed to update coverage comment ($existing_comment_id); skipping"
        exit 0
    fi
else
    if gh api \
            "repos/${repo}/issues/${PR_NUMBER}/comments" \
            -X POST \
            -F body=@/tmp/coverage-comment.md >/dev/null; then
        echo "::notice::posted new coverage comment"
    else
        echo "::warning::failed to post new coverage comment; skipping"
        exit 0
    fi
fi
