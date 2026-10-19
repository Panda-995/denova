#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
TOOL_DIR="${ROOT_DIR}/tools/creative-eval"
if [ ! -d "${TOOL_DIR}/node_modules/promptfoo" ] || [ ! -d "${TOOL_DIR}/node_modules/smol-toml" ]; then
    pnpm --dir "${TOOL_DIR}" install --frozen-lockfile
fi
exec node "${ROOT_DIR}/tools/creative-eval/run.mjs" "$@"
