#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")/.."
# Tests use a separate disposable PostgreSQL service; never the main database.
docker compose --profile test run --rm --build test
sh scripts/ci-frontend.sh
python3 -m unittest discover -s scripts -p 'test_*.py'
