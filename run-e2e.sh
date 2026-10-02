#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export PHOTO_INPUT_DIR="$ROOT/e2e/input"
export PHOTO_OUTPUT_DIR="$ROOT/e2e/output"
export PHOTO_CONFIG_FILE="$ROOT/e2e/config/studio_soft.yaml"
"$ROOT/run.sh" validate
exec "$ROOT/run.sh" render "$@"
