#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")/.."
umask 077
mkdir -p .local/perf
cleanup() { docker compose --profile perf stop perf-api postgres-perf >/dev/null; }
trap cleanup EXIT INT TERM
docker compose --profile perf run --rm --build perf-bootstrap
docker compose --profile perf up -d --build perf-api
docker compose --profile perf run --rm load
