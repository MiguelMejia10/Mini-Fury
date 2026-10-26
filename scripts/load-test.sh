#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")/.."
umask 077
mkdir -p .local/perf
cleanup() { docker compose --profile perf stop perf-api postgres-perf >/dev/null; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
docker compose --profile perf run --rm --build perf-bootstrap
docker compose --profile perf up -d --build perf-api
scenario="${1:-catalog}"
case "$scenario" in catalog|operations) ;; *) echo "Use catalog or operations" >&2; exit 2;; esac
docker compose --profile perf run --rm load run "/scripts/$scenario.js"
