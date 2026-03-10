#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION_FILE="${ROOT_DIR}/internal/buildinfo/version.go"

usage() {
  cat <<'EOF'
Usage:
  tools/bump_up.sh --to v1.2.3-202603.1
  tools/bump_up.sh --major
  tools/bump_up.sh --minor
  tools/bump_up.sh --rev

Rules:
  - Version format: vMAJOR.MINOR.REV-YYYYMM.SEQ
  - --to sets the exact version.
  - --major increments MAJOR and resets MINOR/REV to 0.
  - --minor increments MINOR and resets REV to 0.
  - --rev increments REV.
  - For --major/--minor/--rev, the suffix uses the current UTC YYYYMM.
  - If the current version already uses the current YYYYMM, SEQ increments by 1.
    Otherwise SEQ resets to 1.
EOF
}

die() {
  printf '%s\n' "$*" >&2
  exit 1
}

validate_version() {
  [[ "$1" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)-([0-9]{6})\.([0-9]+)$ ]]
}

current_version() {
  sed -n 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"\(.*\)"/\1/p' "${VERSION_FILE}"
}

write_version() {
  local new_version="$1"
  local tmp
  tmp="$(mktemp)"
  sed "s/^\([[:space:]]*Version[[:space:]]*=[[:space:]]*\"\).*\(\".*\)$/\1${new_version}\2/" "${VERSION_FILE}" > "${tmp}"
  mv "${tmp}" "${VERSION_FILE}"
}

mode=""
target_version=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --to)
      shift
      [[ $# -gt 0 ]] || die "--to requires a version"
      target_version="$1"
      mode="to"
      ;;
    --major)
      [[ -z "${mode}" ]] || die "only one of --to/--major/--minor/--rev may be used"
      mode="major"
      ;;
    --minor)
      [[ -z "${mode}" ]] || die "only one of --to/--major/--minor/--rev may be used"
      mode="minor"
      ;;
    --rev)
      [[ -z "${mode}" ]] || die "only one of --to/--major/--minor/--rev may be used"
      mode="rev"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
  shift
done

[[ -n "${mode}" ]] || die "one of --to/--major/--minor/--rev is required"

current="$(current_version)"
validate_version "${current}" || die "current version is invalid: ${current}"

if [[ "${mode}" == "to" ]]; then
  validate_version "${target_version}" || die "invalid version format: ${target_version}"
  next="${target_version}"
else
  [[ "${current}" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)-([0-9]{6})\.([0-9]+)$ ]]
  major="${BASH_REMATCH[1]}"
  minor="${BASH_REMATCH[2]}"
  rev="${BASH_REMATCH[3]}"
  current_yyyymm="${BASH_REMATCH[4]}"
  current_seq="${BASH_REMATCH[5]}"
  now_yyyymm="$(date -u +%Y%m)"

  case "${mode}" in
    major)
      major="$((major + 1))"
      minor="0"
      rev="0"
      ;;
    minor)
      minor="$((minor + 1))"
      rev="0"
      ;;
    rev)
      rev="$((rev + 1))"
      ;;
  esac

  if [[ "${current_yyyymm}" == "${now_yyyymm}" ]]; then
    next_seq="$((current_seq + 1))"
  else
    next_seq="1"
  fi
  next="v${major}.${minor}.${rev}-${now_yyyymm}.${next_seq}"
fi

write_version "${next}"
printf '%s\n' "${next}"
