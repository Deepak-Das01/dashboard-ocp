#!/usr/bin/env bash
# Compatibility wrapper — use install-operators.sh (both operators + CRDs).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec "$ROOT/scripts/install-operators.sh" "$@"
