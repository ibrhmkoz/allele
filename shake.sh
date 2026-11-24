#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
BIN="$ROOT/_build/allele-build"

if [ ! -x "$BIN" ] || [ "$ROOT/build/Build.hs" -nt "$BIN" ] || [ "$ROOT/build/allele-build.cabal" -nt "$BIN" ]; then
  mkdir -p "$ROOT/_build"
  (cd "$ROOT/build" && cabal build -v0 exe:allele-build)
  BUILT="$(cd "$ROOT/build" && cabal list-bin exe:allele-build)"
  cp "$BUILT" "$BIN"
fi

exec "$BIN" "$@"
