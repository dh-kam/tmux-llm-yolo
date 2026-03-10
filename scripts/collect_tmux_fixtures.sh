#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${1:-$ROOT_DIR/testdata/live-captures}"
SAMPLE_COUNT="${SAMPLE_COUNT:-100}"
SAMPLE_INTERVAL="${SAMPLE_INTERVAL:-1}"
CAPTURE_LINES="${CAPTURE_LINES:-120}"
PROMPT_TEXT="${PROMPT_TEXT:-프로젝트 코드 분석해봐. 개선포인트찾아서 하나씩 진행하자.}"
SEND_PROMPT="${SEND_PROMPT:-1}"
SESSIONS=("${@:2}")

if [ "${#SESSIONS[@]}" -eq 0 ]; then
  SESSIONS=("tmp-codex" "tmp-gemini" "tmp-glm")
fi

mkdir -p "$OUT_DIR"

json_escape() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

timestamp="$(date -u +%Y%m%d-%H%M%S)"
run_dir="$OUT_DIR/$timestamp"
mkdir -p "$run_dir"

printf '{\n  "timestamp": "%s",\n  "sample_count": %s,\n  "sample_interval_seconds": %s,\n  "capture_lines": %s,\n  "prompt_text": "%s",\n  "sessions": [\n' \
  "$timestamp" "$SAMPLE_COUNT" "$SAMPLE_INTERVAL" "$CAPTURE_LINES" "$(json_escape "$PROMPT_TEXT")" > "$run_dir/manifest.json"

first_session=1
for session in "${SESSIONS[@]}"; do
  tmux has-session -t "$session" >/dev/null 2>&1

  session_dir="$run_dir/$session"
  mkdir -p "$session_dir/ansi" "$session_dir/plain"

  if [ "$SEND_PROMPT" = "1" ]; then
    tmux send-keys -t "$session" -l "$PROMPT_TEXT"
    tmux send-keys -t "$session" C-m
  fi

  if [ "$first_session" -eq 0 ]; then
    printf ',\n' >> "$run_dir/manifest.json"
  fi
  first_session=0
  printf '    {\n      "session": "%s",\n      "files": [\n' "$session" >> "$run_dir/manifest.json"

  first_sample=1
  for idx in $(seq 1 "$SAMPLE_COUNT"); do
    sample_id="$(printf '%03d' "$idx")"
    ansi_file="$session_dir/ansi/$sample_id.ansi.txt"
    plain_file="$session_dir/plain/$sample_id.plain.txt"

    tmux capture-pane -p -e -t "$session" -S "-$CAPTURE_LINES" > "$ansi_file"
    tmux capture-pane -p -t "$session" -S "-$CAPTURE_LINES" > "$plain_file"

    if [ "$first_sample" -eq 0 ]; then
      printf ',\n' >> "$run_dir/manifest.json"
    fi
    first_sample=0
    printf '        {"index": %s, "ansi": "%s", "plain": "%s"}' \
      "$idx" \
      "${session}/ansi/${sample_id}.ansi.txt" \
      "${session}/plain/${sample_id}.plain.txt" >> "$run_dir/manifest.json"

    sleep "$SAMPLE_INTERVAL"
  done

  printf '\n      ]\n    }' >> "$run_dir/manifest.json"
done

printf '\n  ]\n}\n' >> "$run_dir/manifest.json"
printf 'fixture run saved to %s\n' "$run_dir"
