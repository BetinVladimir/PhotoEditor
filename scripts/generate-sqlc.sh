#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
bin="$root/work/bin/sqlc-v1.31.1"
out="$root/backend/internal/db/generated"
if [[ ! -x "$bin" ]]; then
  mkdir -p "$(dirname "$bin")" "$root/work/go-sqlc-cache" "$root/work/go-sqlc-mod"
  GOCACHE="$root/work/go-sqlc-cache" GOMODCACHE="$root/work/go-sqlc-mod" GOBIN="$(dirname "$bin")" \
    go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
  mv "$(dirname "$bin")/sqlc" "$bin"
fi
cd "$root"
if [[ "${1:-}" != "--check" ]]; then exec "$bin" generate; fi
snapshot="$(mktemp -d "$root/work/sqlc-check.XXXXXX")"
cleanup(){ find "$out" -maxdepth 1 -type f -delete; cp "$snapshot"/* "$out"/; rm -rf "$snapshot"; }
trap cleanup EXIT
cp "$out"/* "$snapshot"/
"$bin" generate
if ! diff -ru "$snapshot" "$out"; then
  printf '%s\n' 'generated sqlc code is stale; run make sqlc' >&2
  exit 1
fi
