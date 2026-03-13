#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOOKS_DIR="${ROOT_DIR}/.githooks"

cd "${ROOT_DIR}"

git rev-parse --is-inside-work-tree >/dev/null 2>&1

chmod +x "${ROOT_DIR}/tools/bump_up.sh"
find "${HOOKS_DIR}" -type f -exec chmod +x {} +
git config core.hooksPath .githooks

printf 'Installed git hooks from %s\n' "${HOOKS_DIR}"
printf 'core.hooksPath=%s\n' "$(git config --get core.hooksPath)"
