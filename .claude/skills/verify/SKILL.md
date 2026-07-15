---
name: verify
description: Build/launch/drive recipe for verifying multimux changes end-to-end
---

# Verifying multimux

Go daemon + web UI. CI-style checks: `./verify.sh` (gofmt, vet, go test, pnpm lint/test/build).

## Runtime drive (CLI/server surface)

Build and run against a throwaway data dir — never the real install:

```bash
go build -o /tmp/mm .
export MULTIMUX_DATA_DIR=$(mktemp -d)
/tmp/mm serve --port 8790 >serve.log 2>&1 &   # pick a port ≠ 8686 (real daemon may be running)
sleep 2; cat serve.log; kill %1
```

- First run prints the setup banner (`Open: https://<host>:<port>/setup?code=...`).
- Settings live in sqlite: `sqlite3 $MULTIMUX_DATA_DIR/multimux.db "select * from settings;"`.
- Fake a registered passkey (to test credential-gated paths):
  `sqlite3 $MULTIMUX_DATA_DIR/multimux.db "insert into credentials (id,name,data,created_at,last_used_at) values ('x','k','{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');"`
- `auth reset --yes`, `ca trust` etc. work against the same env var.
- Requires tmux on PATH (daemon refuses to start without it).

## Gotchas

- Port 8686 is often taken by the real daemon on this machine — bind failures look like test failures.
- Browser-side flows (passkey ceremony, WS terminals) need CA trust + reachable hostname; usually out of scope for CLI-level verification.
