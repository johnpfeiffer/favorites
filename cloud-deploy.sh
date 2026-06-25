#!/bin/bash
set -euo pipefail

DEST="../codespaces-react/apps/links/src/content/"

echo "=== 5 Most Recent Commits ==="
git log --oneline -5
echo "============================="

mkdir -p "$DEST"
cp content/*.json "$DEST"
