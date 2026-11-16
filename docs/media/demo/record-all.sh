#!/usr/bin/env bash
# Record every README GIF from a fresh demo. Run from the repository root, with
# a built devtopo and playwright-core installed where node finds it:
#
#   docs/media/demo/record-all.sh ./devtopo.exe [scene...]
#
# Each scene gets a freshly seeded demo, served at 14:20 today, as some change it.
set -euo pipefail
exe=$1; shift
scenes=${*:-hero today sessions planner history ask}
work=$(mktemp -d)
day=$(date +%Y-%m-%d)
for s in $scenes; do
  python -X utf8 docs/media/demo/seed.py --exe "$exe" --db "$work/demo.sqlite" >/dev/null
  "$exe" serve --db "$work/demo.sqlite" --addr 127.0.0.1:8799 --at "${day}T14:20" >/dev/null 2>&1 &
  pid=$!
  sleep 2
  DEMO_DB="$work/demo.sqlite" node docs/media/demo/record.js "$s" "$work/$s" http://127.0.0.1:8799 "$day"
  kill $pid 2>/dev/null || true
  wait $pid 2>/dev/null || true
  # Only demo data may be in a GIF: stop if anything else got into the database.
  python -X utf8 -c "import sqlite3,sys; c=sqlite3.connect(sys.argv[1]); bad=c.execute(\"select repo from external_items where repo not like 'acme/%' and repo not like 'ENG-%'\").fetchall(); sys.exit('not demo data in $s: '+str(bad[:3])) if bad else None" "$work/demo.sqlite"
  python -X utf8 docs/media/demo/make-gif.py "$work/$s" "docs/media/$s.gif"
done
