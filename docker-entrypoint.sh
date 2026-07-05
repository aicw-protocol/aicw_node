#!/bin/sh
set -e
# Ensure mounted volume dirs are writable by the runtime user.
for d in /app/db /app/backups /app/logs; do
	mkdir -p "$d"
	chown -R node:node "$d"
done
exec su-exec node aicw-node "$@"
