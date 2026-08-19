#!/usr/bin/env bash
# G-7: daily Consul snapshot backup (production-gaps-review.md)
# Uses Consul HTTP API — no host consul binary required (Docker deployments).
set -euo pipefail

CONSUL_HTTP_ADDR="${CONSUL_HTTP_ADDR:-127.0.0.1:8500}"
BACKUP_DIR="${BACKUP_DIR:-/opt/aicw/backups/consul}"
RETAIN_DAYS="${RETAIN_DAYS:-30}"
LOG_TAG="consul-snapshot-backup"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
RAW="${BACKUP_DIR}/consul-${STAMP}.snap"
OUT="${RAW}.gz"

mkdir -p "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"

curl -sf -o "$RAW" "http://${CONSUL_HTTP_ADDR}/v1/snapshot"
BYTES_RAW="$(wc -c < "$RAW" | tr -d ' ')"
if [[ "$BYTES_RAW" -le 100 ]]; then
  echo "[$LOG_TAG] ERROR: snapshot too small (${BYTES_RAW} bytes)" >&2
  exit 1
fi
echo "[$LOG_TAG] snapshot downloaded (${BYTES_RAW} bytes)"

gzip -f "$RAW"
chmod 600 "$OUT"
find "$BACKUP_DIR" -name 'consul-*.snap.gz' -type f -mtime +"${RETAIN_DAYS}" -delete
ln -sfn "$(basename "$OUT")" "${BACKUP_DIR}/latest.snap.gz"

echo "[$LOG_TAG] OK saved ${OUT} ($(wc -c < "$OUT" | tr -d ' ') bytes)"
