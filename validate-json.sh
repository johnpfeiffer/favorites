#!/usr/bin/env bash
# Verify that every .json file in content/ is valid JSON.
set -uo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/content"

if ! command -v jq >/dev/null 2>&1; then
  echo "Error: jq is required but not installed." >&2
  exit 2
fi

fail=0
found=0
for f in "$DIR"/*.json; do
  [ -e "$f" ] || continue
  found=1
  if jq empty "$f" >/dev/null 2>&1; then
    echo "OK    $f"
  else
    echo "INVALID $f"
    jq empty "$f" 2>&1 | sed 's/^/        /'
    fail=1
  fi
done

if [ "$found" -eq 0 ]; then
  echo "No JSON files found in $DIR" >&2
  exit 1
fi

if [ "$fail" -ne 0 ]; then
  echo "Some files are invalid." >&2
  exit 1
fi

echo "All JSON files are valid."
