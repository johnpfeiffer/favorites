#!/bin/bash
set -euo pipefail

DEST="../track-favorites/app/src/graph/data/"

echo "=== 5 Most Recent Commits ==="
git log --oneline -5 ./graph
echo "============================="

mkdir -p "$DEST"
cp -a graph/*.json "$DEST"

