#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")/.."
# Tests use their own rows; the integration suite never changes existing users.
docker compose --profile test run --rm --build test
sh scripts/ci-frontend.sh
python3 -m unittest discover -s scripts -p 'test_*.py'
