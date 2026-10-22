#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")/../platform-api"
: "${TEST_DATABASE_URL:?Define TEST_DATABASE_URL contra una base de pruebas aislada}"
unformatted=$(gofmt -l cmd internal)
if [ -n "$unformatted" ]; then
  echo "Ejecuta gofmt en estos archivos:"
  echo "$unformatted"
  exit 1
fi
go vet ./...
go test -race -count=1 ./...
go build ./cmd/server ./cmd/user
