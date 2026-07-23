#!/usr/bin/env bash
# Smoke-test a built multimux binary: start the daemon against a throwaway
# data dir and check that "/" serves the embedded SPA shell.
#
# This is the only check that exercises `//go:embed all:web/dist` end to end —
# `go build` succeeds even when web/dist holds nothing but .gitkeep, and the
# server then answers "/" with 501 "web assets missing" instead of the app.
#
# usage: scripts/smoke.sh <path-to-multimux-binary>
set -euo pipefail

bin=${1:?usage: smoke.sh <path-to-multimux-binary>}
[ -x "$bin" ] || { echo "smoke: not executable: $bin" >&2; exit 1; }

data_dir=$(mktemp -d)
log=$(mktemp)
body=$(mktemp)
pid=""

cleanup() {
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  rm -rf "$data_dir" "$log" "$body"
}
trap cleanup EXIT

# --behind-proxy serves plain HTTP on 127.0.0.1, which skips local-CA
# generation and lets curl work without --insecure; the handler chain (and so
# the static/embedded-asset path) is identical to the TLS listener.
# --dev puts the daemon on a tmux socket derived from the throwaway data dir,
# so a smoke run never touches the user's real multimux tmux server.
start_daemon() {
  port=$((20000 + RANDOM % 20000))
  MULTIMUX_DATA_DIR="$data_dir" "$bin" serve --dev --behind-proxy --port "$port" \
    >"$log" 2>&1 &
  pid=$!
  # Poll rather than sleep: ready as soon as /healthz answers, and bail early
  # if the daemon died (e.g. the port was already taken).
  local waited=0
  while [ "$waited" -lt 150 ]; do
    if ! kill -0 "$pid" 2>/dev/null; then
      wait "$pid" 2>/dev/null || true
      pid=""
      return 1
    fi
    if curl -fsS -m 2 -o /dev/null "http://127.0.0.1:$port/healthz" 2>/dev/null; then
      return 0
    fi
    sleep 0.1
    waited=$((waited + 1))
  done
  echo "smoke: daemon did not become ready within 15s" >&2
  cat "$log" >&2
  return 2
}

attempt=1
while :; do
  set +e
  start_daemon
  rc=$?
  set -e
  [ "$rc" -eq 0 ] && break
  if [ "$rc" -eq 1 ] && [ "$attempt" -lt 5 ]; then
    attempt=$((attempt + 1))
    continue # port likely in use — retry on a different one
  fi
  echo "smoke: daemon failed to start" >&2
  cat "$log" >&2
  exit 1
done

code=$(curl -sS -m 10 -o "$body" -w '%{http_code}' "http://127.0.0.1:$port/")
if [ "$code" != "200" ]; then
  echo "smoke: GET / returned $code, want 200" >&2
  head -c 500 "$body" >&2
  echo >&2
  exit 1
fi

# App shell markers: the React mount point, plus a hashed bundle reference that
# only the *built* index.html has (the source one points at /src/main.tsx).
for marker in 'id="root"' '/assets/index-'; do
  if ! grep -qF "$marker" "$body"; then
    echo "smoke: GET / body missing marker: $marker" >&2
    head -c 500 "$body" >&2
    echo >&2
    exit 1
  fi
done

# Fetch the bundle itself, so the embedded-file path is covered too and not
# just the index.html SPA fallback. Check the content type, not only the
# status: the SPA fallback answers *any* unknown path with index.html and a
# 200, so a missing asset would otherwise look like a pass.
asset=$(grep -o '/assets/index-[A-Za-z0-9_-]*\.js' "$body" | head -1)
read -r code ctype <<EOF
$(curl -sS -m 10 -o /dev/null -w '%{http_code} %{content_type}' "http://127.0.0.1:$port$asset")
EOF
case "$code/$ctype" in
  200/*javascript*) ;;
  *)
    echo "smoke: GET $asset returned $code ($ctype), want 200 with a javascript content type" >&2
    exit 1
    ;;
esac

echo "smoke OK (GET / and $asset served from embedded assets)"
