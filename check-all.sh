#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK_DIR="${WORK_DIR:-$SCRIPT_DIR/.check-all-results}"
TESTDATA_DIR="${TESTDATA_DIR:-$SCRIPT_DIR/testdata}"
CAPTURE_GLOB="${CAPTURE_GLOB:-$TESTDATA_DIR/*.capture}"
mkdir -p "$WORK_DIR"

# Avoid permission issues in sandboxed build environments.
export GOCACHE="${GOCACHE:-/tmp/tmux-yolo-go-cache}"
mkdir -p "$GOCACHE"

LLM_TIMEOUT_SEC="${LLM_TIMEOUT_SEC:-60}"
OLLAMA_TIMEOUT_SEC="${OLLAMA_TIMEOUT_SEC:-600}"
LLMS_OVERRIDE="${LLMS_OVERRIDE:-}"

if [[ -n "$LLMS_OVERRIDE" ]]; then
  IFS=" " read -r -a LLMS <<<"$LLMS_OVERRIDE"
else
  LLMS=(
    "gemini"
    "copilot"
    "codex"
    "glm"
    "ollama/glm4:9b"
  )
fi

if (( ${#LLMS[@]} == 0 )); then
  echo "No LLMs configured. Set LLMS_OVERRIDE=\"gemini codex ...\""
  exit 1
fi

extract() {
  local key="$1"
  local file="$2"
  awk -v key="$key" 'BEGIN {prefix=key "="}
    $0 ~ "^" prefix {sub("^" prefix, "", $0); print $0; exit}' "$file"
}

escape_md() {
  printf '%s' "$1" | tr '\n' ' ' | tr '\r' ' ' | sed 's/|/\\|/g'
}

printf "| File | LLM | Decision | Status | Working | MultiChoice | Completed | RecommendedChoice | Reason | Error |\n"
printf "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n"

shopt -s nullglob
for capture_file in $CAPTURE_GLOB; do
  [[ -f "$capture_file" ]] || continue
  file_name="$(basename "$capture_file")"

  for llm in "${LLMS[@]}"; do
    run_dir="$WORK_DIR/$(printf '%s' "$llm" | sed 's#[/:]#_#g')"
    mkdir -p "$run_dir"
    out_file="$run_dir/output-${file_name}.log"

    if [[ "$llm" == ollama/* ]]; then
      timeout_sec="$OLLAMA_TIMEOUT_SEC"
    else
      timeout_sec="$LLM_TIMEOUT_SEC"
    fi

    if timeout "$timeout_sec" go run . check --capture-file "$capture_file" --llm "$llm" >"$out_file" 2>&1; then
      rc=0
    else
      rc=$?
    fi

    session_state="$(extract SESSION_STATE "$out_file")"
    decision="$(extract DECISION "$out_file")"
    status="$(extract STATUS "$out_file")"
    working="$(extract WORKING "$out_file")"
    multi="$(extract MULTIPLE_CHOICE "$out_file")"
    completed="$(extract COMPLETED "$out_file")"
    choice="$(extract RECOMMENDED_CHOICE "$out_file")"
    reason="$(extract REASON "$out_file")"

    if [[ -z "$session_state" ]]; then
      if (( rc != 0 )); then
        session_state="CHECK_ERROR"
      else
        session_state="OFFLINE"
      fi
    fi

    [[ -n "$decision" ]] || decision="N/A"
    [[ -n "$status" ]] || status="N/A"
    [[ -n "$working" ]] || working="N/A"
    [[ -n "$multi" ]] || multi="N/A"
    [[ -n "$completed" ]] || completed="N/A"
    [[ -n "$choice" ]] || choice="N/A"
    [[ -n "$reason" ]] || reason="N/A"

    if (( rc != 0 )); then
      error="CHECK_EXIT_${rc}: $(tail -n 8 "$out_file" 2>/dev/null | tr '\n' ' ')"
    else
      error=""
    fi

    printf "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n" \
      "$(escape_md "$file_name")" \
      "$(escape_md "$llm")" \
      "$(escape_md "$decision")" \
      "$(escape_md "$status")" \
      "$(escape_md "$working")" \
      "$(escape_md "$multi")" \
      "$(escape_md "$completed")" \
      "$(escape_md "$choice")" \
      "$(escape_md "$reason")" \
      "$(escape_md "$error")"
  done
done
