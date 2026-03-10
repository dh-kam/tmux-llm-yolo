#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${1:-$ROOT_DIR/testdata/live-integration}"
WATCHER_SESSION="${WATCHER_SESSION:-watcher}"
SAMPLE_COUNT="${SAMPLE_COUNT:-200}"
SAMPLE_INTERVAL="${SAMPLE_INTERVAL:-2}"
CAPTURE_LINES="${CAPTURE_LINES:-120}"
BASE_INTERVAL="${BASE_INTERVAL:-1}"
SUSPECT_WAIT_1="${SUSPECT_WAIT_1:-1}"
SUSPECT_WAIT_2="${SUSPECT_WAIT_2:-1}"
DURATION_SECONDS="${DURATION_SECONDS:-1800}"
SESSIONS=("${@:2}")

if [ "${#SESSIONS[@]}" -eq 0 ]; then
  SESSIONS=("tmp-gemini" "tmp-codex" "tmp-glm" "tmp-copilot")
fi

PROMPTS=(
  "프로젝트 코드 분석해봐. 개선포인트를 찾고 하나씩 진행하자."
  "진행률을 점검하고 미진한 부분, 누락된 작업, 남은 리스크를 리스트업한 뒤 우선순위 높은 것부터 진행해보자."
  "clean architecture, single responsibility principle, interface-oriented programming 관점에서 구조를 분석하고 어긋난 부분을 개선하자."
  "s/w architect 관점에서 현재 구조를 분석하고 결합도, 책임 분리, 확장성, 테스트 용이성 측면 개선 포인트를 찾아 적용하자."
  "이미 끝난 것과 아직 불완전한 것을 구분하고, 검증이 약한 부분을 보강하면서 계속 진행하자."
)

provider_for_session() {
  case "$1" in
    *gemini*) printf 'gemini' ;;
    *codex*) printf 'codex' ;;
    *copilot*) printf 'copilot' ;;
    *glm*) printf 'glm' ;;
    *) printf 'glm' ;;
  esac
}

json_escape() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

timestamp="$(date -u +%Y%m%d-%H%M%S)"
run_dir="$OUT_DIR/$timestamp"
mkdir -p "$run_dir"

cleanup() {
  for session in "${SESSIONS[@]}"; do
    window="watch-${session}"
    tmux kill-window -t "${WATCHER_SESSION}:${window}" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT

tmux has-session -t "$WATCHER_SESSION" >/dev/null 2>&1 || tmux new-session -d -s "$WATCHER_SESSION" -n control

for session in "${SESSIONS[@]}"; do
  tmux has-session -t "$session" >/dev/null 2>&1
  session_dir="$run_dir/$session"
  state_dir="$session_dir/state"
  mkdir -p "$session_dir/ansi" "$session_dir/plain" "$state_dir"

  provider="$(provider_for_session "$session")"
  fallback="glm"
  if [ "$provider" = "glm" ]; then
    fallback="gemini"
  fi
  window="watch-${session}"
  tmux kill-window -t "${WATCHER_SESSION}:${window}" >/dev/null 2>&1 || true
  tmux new-window -d -t "$WATCHER_SESSION" -n "$window" \
    "cd '$ROOT_DIR' && exec go run . watch -t '$session' --llm '$provider' --fallback-llm '$fallback' --interval-seconds '$BASE_INTERVAL' --suspect-wait-seconds-1 '$SUSPECT_WAIT_1' --suspect-wait-seconds-2 '$SUSPECT_WAIT_2' --duration-seconds '$DURATION_SECONDS' --capture-lines '$CAPTURE_LINES' --state-dir '$state_dir' --log-file '$session_dir/watch.log'"

done

for idx in $(seq 1 "$SAMPLE_COUNT"); do
  for session in "${SESSIONS[@]}"; do
    session_dir="$run_dir/$session"
    sample_id="$(printf '%03d' "$idx")"
    ansi_file="$session_dir/ansi/$sample_id.ansi.txt"
    plain_file="$session_dir/plain/$sample_id.plain.txt"

    if [ "$idx" -eq 1 ] || [ $(( (idx - 1) % 50 )) -eq 0 ]; then
      prompt_idx=$(( ((idx - 1) / 50) % ${#PROMPTS[@]} ))
      prompt="${PROMPTS[$prompt_idx]}"
      tmux send-keys -t "$session" C-u
      tmux send-keys -t "$session" -l "$prompt"
      tmux send-keys -t "$session" C-m
    fi

    tmux capture-pane -p -e -t "$session" -S "-$CAPTURE_LINES" > "$ansi_file"
    tmux capture-pane -p -t "$session" -S "-$CAPTURE_LINES" > "$plain_file"
  done

  sleep "$SAMPLE_INTERVAL"
done

printf '{\n  "timestamp": "%s",\n  "sample_count": %s,\n  "sample_interval_seconds": %s,\n  "capture_lines": %s,\n  "base_interval_seconds": %s,\n  "suspect_wait_1_seconds": %s,\n  "suspect_wait_2_seconds": %s,\n  "watcher_session": "%s",\n  "sessions": [\n' \
  "$timestamp" "$SAMPLE_COUNT" "$SAMPLE_INTERVAL" "$CAPTURE_LINES" "$BASE_INTERVAL" "$SUSPECT_WAIT_1" "$SUSPECT_WAIT_2" "$WATCHER_SESSION" > "$run_dir/manifest.json"

first_session=1
for session in "${SESSIONS[@]}"; do
  provider="$(provider_for_session "$session")"
  window="watch-${session}"
  if [ "$first_session" -eq 0 ]; then
    printf ',\n' >> "$run_dir/manifest.json"
  fi
  first_session=0
  printf '    {\n      "session": "%s",\n      "provider": "%s",\n      "watch_window": "%s",\n      "files": [\n' "$session" "$provider" "$window" >> "$run_dir/manifest.json"

  first_sample=1
  for idx in $(seq 1 "$SAMPLE_COUNT"); do
    sample_id="$(printf '%03d' "$idx")"
    if [ "$first_sample" -eq 0 ]; then
      printf ',\n' >> "$run_dir/manifest.json"
    fi
    first_sample=0
    printf '        {"index": %s, "ansi": "%s", "plain": "%s"}' \
      "$idx" \
      "${session}/ansi/${sample_id}.ansi.txt" \
      "${session}/plain/${sample_id}.plain.txt" >> "$run_dir/manifest.json"
  done
  printf '\n      ]\n    }' >> "$run_dir/manifest.json"
done

printf '\n  ]\n}\n' >> "$run_dir/manifest.json"

for session in "${SESSIONS[@]}"; do
  session_dir="$run_dir/$session"
  {
    printf 'session=%s\n' "$session"
    printf 'prompt_analysis=%s\n' "$(grep -c 'prompt analysis:' "$session_dir/watch.log" || true)"
    printf 'continue_selected=%s\n' "$(grep -c 'continue prompt selected:' "$session_dir/watch.log" || true)"
    printf 'llm_fallback_active=%s\n' "$(grep -c 'active=fallback:' "$session_dir/watch.log" || true)"
    printf 'completed=%s\n' "$(grep -c '추가 작업 없음으로 판단되어 watcher 종료' "$session_dir/watch.log" || true)"
    printf 'deadline_stopped=%s\n' "$(grep -c 'watch deadline exceeded' "$session_dir/watch.log" || true)"
  } > "$session_dir/summary.txt"
done

printf 'live integration run saved to %s\n' "$run_dir"
