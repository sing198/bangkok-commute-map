#!/bin/sh
set -eu

if [ "${SEED_ON_START:-false}" = "true" ]; then
  echo "SEED_ON_START=true: running idempotent PostGIS seed pipeline..."
  /app/seed --data-dir /app/data
fi

exec /app/api
