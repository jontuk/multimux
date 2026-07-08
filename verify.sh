#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

echo "== gofmt =="
unformatted=$(gofmt -l . | grep -v '^web/' || true)
if [ -n "$unformatted" ]; then echo "gofmt needed: $unformatted"; exit 1; fi

echo "== go vet =="
go vet ./...

echo "== go test =="
go test ./...

echo "verify OK"
