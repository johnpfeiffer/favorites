#!/usr/bin/env bash
# Regenerate podcasts/<slug>.json episode indexes from podcasts/registry.json feeds.
# Requires: python3, curl. Safe to re-run; indexes are deterministic given the same feeds.
set -uo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT_DIR="$DIR/podcasts"

if ! command -v python3 >/dev/null 2>&1; then
  echo "Error: python3 is required but not installed." >&2
  exit 2
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "Error: curl is required but not installed." >&2
  exit 2
fi

python3 - "$OUT_DIR" <<'PYEOF'
import json
import re
import subprocess
import sys
import xml.etree.ElementTree as ET
from datetime import timezone
from email.utils import parsedate_to_datetime
from pathlib import Path

out_dir = Path(sys.argv[1])
registry = json.loads((out_dir / "registry.json").read_text())

def fetch(url):
    result = subprocess.run(
        ["curl", "-sL", "--compressed", "--max-time", "60",
         "-A", "Mozilla/5.0 (podcast-index-builder)", url],
        capture_output=True,
    )
    if result.returncode != 0:
        raise RuntimeError(f"curl exited {result.returncode}")
    return result.stdout

def iso_date(pub):
    if not pub:
        return None
    try:
        dt = parsedate_to_datetime(pub.strip())
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
        return dt.date().isoformat()
    except Exception:
        return None

fail = 0
for show in registry["shows"]:
    slug, feed = show["slug"], show["feedUrl"]
    try:
        raw = fetch(feed)
        root = ET.fromstring(raw)
    except Exception as e:
        print(f"FAIL  {slug}: {e}")
        fail = 1
        continue

    episodes = []
    for item in root.iter("item"):
        def text(tag):
            el = item.find(tag)
            return el.text.strip() if el is not None and el.text else None
        title = text("title")
        link = text("link")
        pub = text("pubDate")
        guid_el = item.find("guid")
        guid = guid_el.text.strip() if guid_el is not None and guid_el.text else None
        if not link and guid and guid.startswith("http"):
            link = guid
        episodes.append({
            "title": re.sub(r"\s+", " ", title) if title else None,
            "published": iso_date(pub),
            "url": link,
        })

    index = {
        "show": show["name"],
        "slug": slug,
        "appleId": show["appleId"],
        "feedUrl": feed,
        "homeUrl": show["homeUrl"],
        "canonicalPattern": show["canonicalPattern"],
        "episodeCount": len(episodes),
        "episodes": episodes,
    }
    target = out_dir / f"{slug}.json"
    target.write_text(json.dumps(index, indent=2, ensure_ascii=False) + "\n")
    print(f"OK    {slug}: {len(episodes)} episodes -> {target.name}")

sys.exit(fail)
PYEOF
