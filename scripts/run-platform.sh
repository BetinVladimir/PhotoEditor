#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/platform-env.sh"
cd "$PHOTO_REPO_ROOT"
exec "$@"
