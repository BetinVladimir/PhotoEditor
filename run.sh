#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export PHOTO_INPUT_DIR="${PHOTO_INPUT_DIR:-$ROOT/e2e/input}"
export PHOTO_OUTPUT_DIR="${PHOTO_OUTPUT_DIR:-$ROOT/e2e/output}"
export PHOTO_CONFIG_FILE="${PHOTO_CONFIG_FILE:-$ROOT/e2e/config/studio_soft.yaml}"
export PHOTO_MODELS_DIR="${PHOTO_MODELS_DIR:-$ROOT/.models}"
export MPLCONFIGDIR="${MPLCONFIGDIR:-$ROOT/.cache/matplotlib}"
export XDG_CACHE_HOME="${XDG_CACHE_HOME:-$ROOT/.cache}"
export PYTHONUNBUFFERED=1
export PYTHONPATH="$ROOT/apps/local-editor/src${PYTHONPATH:+:$PYTHONPATH}"
cd "$ROOT"
if [ ! -x .venv/bin/python ] || ! .venv/bin/python -c 'import photoeditor' >/dev/null 2>&1; then
  "$ROOT/scripts/setup.sh"
fi
if [ "$#" -eq 0 ]; then set -- render; fi
exec "$ROOT/.venv/bin/python" -m photoeditor.cli "$@"
