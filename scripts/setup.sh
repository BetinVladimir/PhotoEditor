#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
export UV_CACHE_DIR="${UV_CACHE_DIR:-$ROOT/.cache/uv}"
export PHOTO_MODELS_DIR="${PHOTO_MODELS_DIR:-$ROOT/.models}"
export MPLCONFIGDIR="${MPLCONFIGDIR:-$ROOT/.cache/matplotlib}"
export XDG_CACHE_HOME="${XDG_CACHE_HOME:-$ROOT/.cache}"
if command -v uv >/dev/null 2>&1; then
  if [ ! -x .venv/bin/python ]; then uv venv .venv --python "${PHOTO_PYTHON:-python3}"; fi
  uv pip sync --python .venv/bin/python apps/local-editor/requirements.lock
  uv pip install --python .venv/bin/python --no-deps -e apps/local-editor
else
  "${PHOTO_PYTHON:-python3}" -m venv .venv
  .venv/bin/python -m pip install -r apps/local-editor/requirements.lock
  .venv/bin/python -m pip install --no-deps -e apps/local-editor
fi
.venv/bin/python -m photoeditor.cli setup-models
if [ "${1:-}" = '--studio' ]; then
  if command -v uv >/dev/null 2>&1; then
    uv pip install --python .venv/bin/python 'PySide6==6.11.2'
  else
    .venv/bin/python -m pip install 'PySide6==6.11.2'
  fi
fi
