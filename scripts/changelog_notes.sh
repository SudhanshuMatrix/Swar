#!/usr/bin/env bash
# Prints the CHANGELOG.md section for one version, without its heading.
# Usage: bash scripts/changelog_notes.sh 0.2.0
# Exits with status 1 when CHANGELOG.md has no section for that version.
set -euo pipefail

VERSION="${1:?usage: changelog_notes.sh <version>}"
FILE="${2:-CHANGELOG.md}"

awk -v v="$VERSION" '
  /^## / {
    if (found) exit
    # Matches "## [0.2.0]", "## [0.2.0] - 2026-10-20", "## 0.2.0" and "## v0.2.0".
    h = $0
    sub(/^## +\[?v?/, "", h)
    sub(/[] ].*$/, "", h)
    if (h == v) { found = 1; next }
  }
  found { lines[++n] = $0 }
  END {
    if (!found) exit 1
    # Drop blank lines around the section.
    first = 1; while (first <= n && lines[first] ~ /^[ \t]*$/) first++
    last = n; while (last >= first && lines[last] ~ /^[ \t]*$/) last--
    for (i = first; i <= last; i++) print lines[i]
  }
' "$FILE"
