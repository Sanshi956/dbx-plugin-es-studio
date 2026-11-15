#!/usr/bin/env bash
# Creates a long-running cluster task so the Tasks page can be exercised with
# REAL data (an empty _tasks list proves nothing).
#
#   scripts/es-task-demo.sh start [docs]
#     RPS=40  throttle, so the task lives ~docs/RPS seconds (default 8000/40 ≈ 200s)
#   scripts/es-task-demo.sh stop           # cancel the task + drop both indices
#   scripts/es-task-demo.sh status
#
# The task is a `POST /_reindex?wait_for_completion=false` throttled to
# requests_per_second=$RPS, so it stays visible for roughly docs/RPS seconds.
# Resource footprint is small (20k tiny docs ≈ 4 MB).
set -euo pipefail

ES="${ES_URL:-http://127.0.0.1:19200}"
SRC="task-src"
DST="task-dst"
DOCS="${2:-20000}"
RPS="${RPS:-40}"
STATE="$(dirname "$0")/../.dbx-dev/task-demo.id"

cmd="${1:-status}"

case "$cmd" in
  start)
    curl -s -X DELETE "$ES/$SRC,$DST" >/dev/null 2>&1 || true
    curl -s -X PUT "$ES/$SRC" -H 'Content-Type: application/json' -d '{"settings":{"number_of_shards":1,"number_of_replicas":0}}' >/dev/null
    echo "[task-demo] bulk indexing $DOCS docs into $SRC …"
    python_bin="${PYTHON:-D:/software/dev/Python/Python313/python.exe}"
    "$python_bin" - "$ES" "$SRC" "$DOCS" <<'PY'
import json, sys, urllib.request
es, index, total = sys.argv[1], sys.argv[2], int(sys.argv[3])
def bulk(chunk, lines):
    body = "\n".join(lines) + "\n"
    req = urllib.request.Request(f"{es}/_bulk", data=body.encode(), method="POST",
                                headers={"Content-Type": "application/x-ndjson"})
    with urllib.request.urlopen(req, timeout=120) as r:
        res = json.loads(r.read())
    if res.get("errors"):
        raise SystemExit("bulk indexing reported errors")
n = 0
while n < total:
    lines, c = [], 0
    while c < 1000 and n < total:
        lines.append(json.dumps({"index": {"_index": index}}))
        lines.append(json.dumps({"msg": f"row {n}", "n": n, "body": "x" * 40}))
        n += 1; c += 1
    bulk(lines, lines)
print(f"[task-demo] indexed {n} docs")
PY
    curl -s -X POST "$ES/_refresh" >/dev/null
    echo "[task-demo] starting throttled async reindex (requests_per_second=$RPS) …"
    resp="$(curl -s -X POST "$ES/_reindex?wait_for_completion=false&slices=1&requests_per_second=$RPS" \
      -H 'Content-Type: application/json' \
      -d "{\"source\":{\"index\":\"$SRC\",\"size\":500},\"dest\":{\"index\":\"$DST\"}}")"
    echo "$resp"
    echo "$resp" | sed -n 's/.*"task":"\([^"]*\)".*/\1/p' > "$STATE"
    echo "[task-demo] task=$(cat "$STATE")"
    ;;
  stop)
    if [ -f "$STATE" ]; then
      tid="$(cat "$STATE")"
      echo "[task-demo] cancelling $tid"
      curl -s -X POST "$ES/_tasks/$tid/_cancel" || true
      echo
    fi
    curl -s -X DELETE "$ES/$SRC,$DST" >/dev/null 2>&1 || true
    rm -f "$STATE"
    echo "[task-demo] cleaned up"
    ;;
  status)
    curl -s "$ES/_tasks?group_by=none&detailed=true" \
      | tr ',' '\n' | grep -E '"action"|"task"' | head -20 || true
    ;;
  *)
    echo "usage: $0 {start [docs]|stop|status}" >&2
    exit 2
    ;;
esac
