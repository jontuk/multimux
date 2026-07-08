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

echo "== web =="
(cd web && pnpm install --frozen-lockfile --silent 2>/dev/null || pnpm install --silent)
(cd web && pnpm lint && pnpm test && pnpm build)

echo "verify OK"
