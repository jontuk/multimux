#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

# Build artefacts go to a scratch dir, never into the working tree.
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

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

# After pnpm build: the binary embeds web/dist, so it must exist first.
echo "== go build =="
go build -o "$tmpdir/multimux" .

echo "== smoke =="
./scripts/smoke.sh "$tmpdir/multimux"

echo "verify OK"
