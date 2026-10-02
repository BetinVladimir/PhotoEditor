#!/usr/bin/env bash
# Source this file before tools; all writable caches stay inside the repository.
if [ -n "${ZSH_VERSION:-}" ]; then
  PHOTO_ENV_SCRIPT="${(%):-%x}"
else
  PHOTO_ENV_SCRIPT="${BASH_SOURCE[0]}"
fi
PHOTO_REPO_ROOT="$(cd "$(dirname "$PHOTO_ENV_SCRIPT")/.." && pwd)"
if [ ! -f "$PHOTO_REPO_ROOT/workers/pyproject.toml" ] || [ ! -f "$PHOTO_REPO_ROOT/backend/go.mod" ]; then
  printf '%s\n' 'Invalid PhotoEditor repository root' >&2
  return 1
fi
export XDG_CACHE_HOME="$PHOTO_REPO_ROOT/.cache"
export HF_HOME="$PHOTO_REPO_ROOT/.cache/huggingface"
export HF_DATASETS_CACHE="$PHOTO_REPO_ROOT/.cache/datasets"
export TORCH_HOME="$PHOTO_REPO_ROOT/.cache/torch-platform"
export NUMBA_CACHE_DIR="$PHOTO_REPO_ROOT/.cache/numba"
export MPLCONFIGDIR="$PHOTO_REPO_ROOT/.cache/matplotlib"
export HF_HUB_DISABLE_TELEMETRY=1
export UV_CACHE_DIR="$PHOTO_REPO_ROOT/.cache/uv"
export UV_PYTHON_INSTALL_DIR="$PHOTO_REPO_ROOT/.runtime/python"
export UV_PROJECT_ENVIRONMENT="$PHOTO_REPO_ROOT/.venv-platform"
export GOCACHE="$PHOTO_REPO_ROOT/.cache/go"
export GOMODCACHE="$PHOTO_REPO_ROOT/.cache/go-mod"
export GOPATH="$PHOTO_REPO_ROOT/.runtime/go"
export npm_config_cache="$PHOTO_REPO_ROOT/.cache/npm"
export TMPDIR="$PHOTO_REPO_ROOT/.cache/tmp"
export PYTHONDONTWRITEBYTECODE=1
export PYTHONPATH="$PHOTO_REPO_ROOT/workers${PYTHONPATH:+:$PYTHONPATH}"
mkdir -p "$TMPDIR" "$GOCACHE" "$GOMODCACHE"
