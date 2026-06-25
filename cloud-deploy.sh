#!/bin/bash
set -euo pipefail

DEST="../links-app/app/src/content/"


./validate-json.sh

echo "=== 5 Most Recent Commits ==="
git log --oneline -5
echo "============================="

mkdir -p "$DEST"
cp content/*.json "$DEST"
