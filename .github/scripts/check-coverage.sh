#!/usr/bin/env bash
# Per-package coverage gate.
#
# Usage:
#   .github/scripts/check-coverage.sh <coverage-profile>
#
# Reads .github/coverage-thresholds.yaml, runs `go tool cover -func` on the
# supplied profile, computes per-package statement coverage, and exits non-zero
# if any threshold is breached. Designed to be called from CI; runnable
# locally too.
#
# Output is structured: one line per package, marked PASS / FAIL / SKIP, plus
# a final summary. Failure rows include the delta vs threshold so the message
# in the CI log tells you exactly how much you dropped.
#
# No external deps beyond bash, awk, grep, sed, and `go`. No yq/jq needed —
# the YAML schema is a flat map so a tiny grep+awk parser handles it.
#
# Requires Bash 4+ (associative arrays, `[[ -v ... ]]`). On macOS the system
# /bin/bash is 3.2; install Bash 4+ via `brew install bash` and run with
# `bash .github/scripts/check-coverage.sh ...` (the env-bash shebang will
# pick up the Homebrew bash if it's earlier on PATH).

set -euo pipefail

if [[ "${BASH_VERSINFO[0]:-0}" -lt 4 ]]; then
    echo "error: this script requires Bash 4+; got ${BASH_VERSION:-unknown}" >&2
    echo "  on macOS: brew install bash, then re-run with the Homebrew bash" >&2
    exit 2
fi

if [[ $# -ne 1 ]]; then
    echo "usage: $0 <coverage-profile>" >&2
    exit 2
fi

profile="$1"
thresholds_yaml="$(dirname "$0")/../coverage-thresholds.yaml"

if [[ ! -f "$profile" ]]; then
    echo "coverage profile not found: $profile" >&2
    exit 2
fi
if [[ ! -f "$thresholds_yaml" ]]; then
    echo "thresholds file not found: $thresholds_yaml" >&2
    exit 2
fi

# Strip the module prefix from coverage profile paths so they match the
# package keys in the YAML. Computed once.
module="$(go list -m)"

# Per-package statement coverage from the profile.
#
# Note: `go tool cover -func` emits *function*-level coverage; averaging those
# numbers per package would NOT yield statement coverage because functions
# have different statement counts. The correct number is statements covered /
# statements total per package, which we derive directly from the profile
# below. The per-function tool is used only for the `total:` line.
per_package_statement_coverage() {
    awk -v module="$module" '
        NR == 1 { next }   # skip mode line: "mode: atomic"
        {
            # Profile line shape: "<file>:<srange> <statements> <covered>"
            split($1, a, ":")
            path = a[1]
            sub(/\/[^\/]+$/, "", path)
            sub("^" module "/", "", path)

            stmts[path] += $2
            if ($3 + 0 > 0) {
                covered[path] += $2
            }
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

# Read thresholds from YAML. Schema is intentionally flat (a single
# `packages:` map plus a `total:` scalar) so we don't need yq.
#
# Output: "<package> <threshold>" per line.
read_thresholds() {
    awk '
        /^packages:/ { in_packages = 1; next }
        in_packages && /^[a-zA-Z]/ { in_packages = 0 }
        in_packages && /^[[:space:]]+[a-zA-Z0-9_\/]+:[[:space:]]*[0-9]+/ {
            # Strip leading whitespace, trailing comment, and the colon.
            line = $0
            sub(/^[[:space:]]+/, "", line)
            sub(/[[:space:]]*#.*$/, "", line)
            split(line, kv, ":")
            key = kv[1]
            val = kv[2]
            gsub(/[[:space:]]/, "", val)
            print key, val
        }
    ' "$thresholds_yaml"
}

# Total coverage from the profile.
total_coverage() {
    go tool cover -func="$profile" | awk '/^total:/ { gsub(/%/, "", $3); print $3 }'
}

# ---------- main ----------

declare -A actual
while read -r pkg cov; do
    actual["$pkg"]="$cov"
done < <(per_package_statement_coverage)

declare -A threshold
while read -r pkg t; do
    threshold["$pkg"]="$t"
done < <(read_thresholds)

failures=0
unknown_packages=()
covered_packages=()

# Walk every package that actually has coverage data; check against threshold.
for pkg in "${!actual[@]}"; do
    cov="${actual[$pkg]}"
    if [[ -v threshold[$pkg] ]]; then
        t="${threshold[$pkg]}"
        # awk for floating-point comparison; portable across mac/linux.
        if awk "BEGIN { exit !(${cov} < ${t}) }"; then
            delta="$(awk "BEGIN { printf \"%.1f\", ${cov} - ${t} }")"
            printf 'FAIL  %-30s %s%% < %s%% (delta %s)\n' "$pkg" "$cov" "$t" "$delta"
            failures=$((failures + 1))
        else
            printf 'PASS  %-30s %s%% (threshold %s%%)\n' "$pkg" "$cov" "$t"
        fi
        covered_packages+=("$pkg")
    else
        unknown_packages+=("$pkg ($cov%)")
    fi
done

# Walk every package in the YAML that DIDN'T appear in coverage data — likely
# a renamed or removed package; surface it so the YAML stays accurate.
for pkg in "${!threshold[@]}"; do
    if [[ ! -v actual[$pkg] ]]; then
        printf 'SKIP  %-30s (no coverage data — package missing or excluded?)\n' "$pkg"
    fi
done

if [[ ${#unknown_packages[@]} -gt 0 ]]; then
    echo
    echo "Untracked packages (have coverage but no threshold in YAML):"
    for p in "${unknown_packages[@]}"; do echo "  - $p"; done
    echo "Add a threshold entry to .github/coverage-thresholds.yaml or accept"
    echo "this as informational. Untracked packages do NOT fail the gate."
fi

echo
echo "Total coverage: $(total_coverage)% (informational; not gating)"

if [[ $failures -gt 0 ]]; then
    echo
    echo "$failures package(s) below threshold."
    exit 1
fi

echo "All ${#covered_packages[@]} tracked package(s) at or above threshold."
