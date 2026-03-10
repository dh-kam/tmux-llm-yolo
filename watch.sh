#!/usr/bin/env bash
set -euo pipefail

DEFAULT_SESSION_NAME="dev-pdf-codex"
INTERVAL_SECONDS="${INTERVAL_SECONDS:-60}"
DURATION_SECONDS="${DURATION_SECONDS:-86400}"
CAPTURE_LINES="${CAPTURE_LINES:-25}"
CONTINUE_MESSAGE="${CONTINUE_MESSAGE:-응 계속 이어서 진행해서 포팅 완료까지 진행해보자}"
SUBMIT_KEY="${SUBMIT_KEY:-C-m}"
SUBMIT_KEY_FALLBACK="${SUBMIT_KEY_FALLBACK:-C-m}"
SUBMIT_KEY_FALLBACK_DELAY="${SUBMIT_KEY_FALLBACK_DELAY:-0.15}"
TMUX_SESSION=""
LLM_BIN="${LLM:-${CODEX_BIN:-glm}}"
LLM_MODEL="${LLM_MODEL:-${CODEX_MODEL:-}}"
STATE_DIR="${STATE_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/.watch-state}"
LOG_FILE="${LOG_FILE:-$STATE_DIR/watch.log}"
LLM_EXEC_BIN="${LLM_BIN}"

show_usage() {
  cat <<'USAGE'
Usage:
  watch.sh [options] [SESSION_NAME]

Options:
  -t, --tmux-session SESSION_NAME   Target tmux session name (default: dev-pdf-codex)
  --llm NAME                         LLM command to run (default: glm)
  -h, --help                        Show this help message
USAGE
  exit 1
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    -t|--tmux-session)
      if [ "$#" -lt 2 ]; then
        printf '오류: -t/--tmux-session는 값이 필요합니다\n' >&2
        show_usage
      fi
      TMUX_SESSION="$2"
      shift 2
      ;;
    --llm)
      if [ "$#" -lt 2 ]; then
        printf '오류: --llm은 값이 필요합니다\n' >&2
        show_usage
      fi
      LLM_BIN="$2"
      shift 2
      ;;
    --help|-h)
      show_usage
      ;;
    --)
      shift
      break
      ;;
    -*)
      printf '오류: 알 수 없는 옵션: %s\n' "$1" >&2
      show_usage
      ;;
    *)
      if [ -z "$TMUX_SESSION" ]; then
        TMUX_SESSION="$1"
      fi
      shift
      ;;
  esac
done

TMUX_SESSION="${TMUX_SESSION:-$DEFAULT_SESSION_NAME}"
LLM_KIND="$(printf '%s' "$LLM_BIN" | tr '[:upper:]' '[:lower:]')"
case "$LLM_KIND" in
  *gemini*)
    LLM_EXEC_BIN="gemini"
    ;;
esac

mkdir -p "$STATE_DIR"

log() {
  local ts
  ts="$(date '+%Y-%m-%d %H:%M:%S')"
  printf '[%s] %s\n' "$ts" "$*" | tee -a "$LOG_FILE"
}

run_llm_exec() {
  local output_file="$1"
  shift
  "$@" > "$output_file" 2>&1
}

run_llm_query() {
  local prompt_file="$1"
  local output_file="$2"
  local prompt
  local -a model_args=()
  local -a cmd_args=()

  prompt="$(cat "$prompt_file")"
  if [ -n "$LLM_MODEL" ]; then
    model_args+=(--model "$LLM_MODEL")
  fi

  case "$LLM_KIND" in
    *copilot*)
  cmd_args=("$LLM_EXEC_BIN" -p "$prompt" --silent --allow-all-tools "${model_args[@]}")
      if run_llm_exec "$output_file" "${cmd_args[@]}"; then
        return 0
      fi
      cmd_args=("$LLM_EXEC_BIN" -p "$prompt" --allow-all-tools "${model_args[@]}")
      if run_llm_exec "$output_file" "${cmd_args[@]}"; then
        return 0
      fi
      cmd_args=("$LLM_EXEC_BIN" "$prompt" --silent --allow-all-tools "${model_args[@]}")
      if run_llm_exec "$output_file" "${cmd_args[@]}"; then
        return 0
      fi
      ;;
    *glm*)
      cmd_args=("$LLM_EXEC_BIN" --yolo --print "$prompt" "${model_args[@]}")
      if run_llm_exec "$output_file" "${cmd_args[@]}"; then
        return 0
      fi
      cmd_args=("$LLM_EXEC_BIN" --print "$prompt" "${model_args[@]}")
      if run_llm_exec "$output_file" "${cmd_args[@]}"; then
        return 0
      fi
      cmd_args=("$LLM_EXEC_BIN" -p "$prompt" "${model_args[@]}")
      if run_llm_exec "$output_file" "${cmd_args[@]}"; then
        return 0
      fi
      ;;
  *)
  cmd_args=("$LLM_EXEC_BIN" -p "$prompt" "${model_args[@]}")
      if run_llm_exec "$output_file" "${cmd_args[@]}"; then
        return 0
      fi
  cmd_args=("$LLM_EXEC_BIN" -p "$prompt" "${model_args[@]}")
  if run_llm_exec "$output_file" "${cmd_args[@]}"; then
    return 0
  fi
  ;;
  esac

  cmd_args=("$LLM_EXEC_BIN" "$prompt" "${model_args[@]}")
  if run_llm_exec "$output_file" "${cmd_args[@]}"; then
    return 0
  fi
  return 1
}

has_progress_above_prompt() {
  local capture_path="$1"
  local prompt_line
  local start_line
  local probe

  if [ ! -s "$capture_path" ]; then
    return 1
  fi

  # Use the latest interactive prompt line and inspect nearby lines right above it.
  prompt_line="$(awk '/^[[:space:]]*› / { line=NR } END { if (line) print line }' "$capture_path")"
  if [ -z "$prompt_line" ] || [ "$prompt_line" -le 1 ]; then
    return 1
  fi

  start_line=$((prompt_line - 4))
  if [ "$start_line" -lt 1 ]; then
    start_line=1
  fi

  probe="$(sed -n "${start_line},$((prompt_line - 1))p" "$capture_path")"

  # Codex active-progress marker: "... (2m 02s • esc to interrupt)".
  if printf '%s\n' "$probe" | grep -Eiq 'esc to interrupt'; then
    return 0
  fi

  return 1
}

send_continue_message() {
  local target="$1"
  local message="$2"
  local pane_in_mode

  pane_in_mode="$(tmux display-message -p -t "$target" '#{pane_in_mode}' 2>/dev/null || printf '0')"
  if [ "$pane_in_mode" = "1" ]; then
    tmux send-keys -t "$target" -X cancel
  fi

  # Send literal text (not paste-buffer) to avoid multiline/paste-mode side effects.
  tmux send-keys -t "$target" -l "$message"
  tmux send-keys -t "$target" "$SUBMIT_KEY"

  # Optional extra submit keystroke for environments with non-standard key handling.
  if [ -n "$SUBMIT_KEY_FALLBACK" ]; then
    sleep "$SUBMIT_KEY_FALLBACK_DELAY"
    tmux send-keys -t "$target" "$SUBMIT_KEY_FALLBACK"
  fi
}

check_tmux_session_state() {
  local target="$1"
  local state_file="$2"
  local sessions
  local decision_text
  local decision
  local prompt_file="$state_file.prompt"
  local short_error

  sessions="$(tmux list-sessions -F '#{session_name}' 2>/dev/null || printf '<none>')"

  {
    cat <<'PROMPT'
You are a strict binary classifier.
Decide whether the target tmux session currently exists among the listed sessions.

Return exactly 1 line in this format:
STATE: EXISTS or MISSING

Rules:
- Return EXISTS only if the target session name is present.
- Return MISSING if the target session name is not present.
- Output only that one line, nothing else.
PROMPT
    printf '\nTarget session: %s\n' "$target"
    printf 'Sessions:\n%s\n' "$sessions"
  } > "$prompt_file"

  if run_llm_query "$prompt_file" "$state_file"; then
    decision_text="$(cat "$state_file")"
    decision="$(printf '%s\n' "$decision_text" | grep -Eo 'EXISTS|MISSING' | head -n1 || true)"
    if [ "$decision" = "EXISTS" ]; then
      log "정보: ${LLM_KIND}가 tmux 세션 상태를 판별했습니다. state=EXISTS, session=$target"
      printf 'EXISTS'
      return 0
    fi
    if [ "$decision" = "MISSING" ]; then
      log "정보: ${LLM_KIND}가 tmux 세션 상태를 판별했습니다. state=MISSING, session=$target"
      printf 'MISSING'
      return 1
    fi
    short_error="$(printf '%s\n' "$decision_text" | tr '\n' ' ' | sed 's/[[:space:]]\+/ /g' | cut -c1-160)"
    log "경고: ${LLM_KIND} 판정 응답이 유효하지 않습니다. session=$target, output=$short_error"
  else
    short_error="$(cat "$state_file" 2>/dev/null | tr '\n' ' ' | sed 's/[[:space:]]\+/ /g' | cut -c1-160)"
    log "경고: ${LLM_KIND} 실행 실패(세션 상태 판별). session=$target, error=$short_error"
  fi

  if tmux has-session -t "$target" 2>/dev/null; then
    log "정보: tmux has-session로 폴백 판정 성공. session=$target"
    printf 'EXISTS'
    return 0
  fi

  log "경고: 세션 상태 판별 실패. ${LLM_KIND} 및 tmux 예비 판정 모두 실패, session=$target"
  printf 'MISSING'
  return 1
}

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    log "오류: 필수 명령어를 찾을 수 없습니다: $cmd"
    exit 1
  fi
}

require_cmd tmux
require_cmd "$LLM_EXEC_BIN"

if ! [[ "$INTERVAL_SECONDS" =~ ^[0-9]+$ ]] || ! [[ "$DURATION_SECONDS" =~ ^[0-9]+$ ]] || ! [[ "$CAPTURE_LINES" =~ ^[0-9]+$ ]]; then
  log "오류: INTERVAL_SECONDS, DURATION_SECONDS, CAPTURE_LINES는 정수여야 합니다"
  exit 1
fi

if [ "$INTERVAL_SECONDS" -le 0 ] || [ "$DURATION_SECONDS" -le 0 ] || [ "$CAPTURE_LINES" -le 0 ]; then
  log "오류: INTERVAL_SECONDS, DURATION_SECONDS, CAPTURE_LINES는 0보다 커야 합니다"
  exit 1
fi

end_ts=$(( $(date +%s) + DURATION_SECONDS ))
iteration=0
session_missing=0

log "감시 시작: session=$TMUX_SESSION interval=${INTERVAL_SECONDS}s duration=${DURATION_SECONDS}s"

while [ "$(date +%s)" -lt "$end_ts" ]; do
  iteration=$((iteration + 1))
  now="$(date +%s)"
  stamp="$(date '+%Y%m%d-%H%M%S')"
  capture_file="$STATE_DIR/capture-$stamp.txt"
  prompt_file="$STATE_DIR/prompt-$stamp.txt"
  decision_file="$STATE_DIR/decision-$stamp.txt"
  session_state_file="$STATE_DIR/session-state-$stamp.txt"
  session_state="$(check_tmux_session_state "$TMUX_SESSION" "$session_state_file" || true)"

  if [ "$session_state" != "EXISTS" ]; then
    if [ "$session_missing" -eq 0 ]; then
      log "경고: tmux 세션을 찾을 수 없습니다: $TMUX_SESSION"
      session_missing=1
    fi
  else
    if [ "$session_missing" -eq 1 ]; then
      log "정보: tmux 세션이 다시 연결됨: $TMUX_SESSION"
      session_missing=0
    fi
    if tmux capture-pane -p -J -t "$TMUX_SESSION" -S "-$CAPTURE_LINES" > "$capture_file" 2>/dev/null; then
      if [ ! -s "$capture_file" ]; then
        log "정보: 캡처 결과가 비어 있습니다 (iteration=$iteration)"
      else
        if has_progress_above_prompt "$capture_file"; then
          log "정보: 건너뜀 (iteration=$iteration, reason=프롬프트 위에 진행 텍스트가 표시되어 에이전트가 계속 작업 중입니다.)"
        else
			        {
			          cat <<'PROMPT'
You are a strict classifier.
Decide whether the following terminal output indicates the agent is waiting for user input or next instruction after finishing a task.

Return exactly 2 lines in this format:
DECISION: SEND or SKIP
REASON: <one short sentence>

Rules:
- Use SEND only if the terminal clearly waits for user response or next instruction.
- Use SKIP otherwise.
- Treat prompts like "› Explain this codebase" as possible placeholder suggestions, not guaranteed user-entered instructions.
- If progress text appears right above the prompt (for example containing "esc to interrupt"), the agent is still working, so return SKIP.
- Do not output anything except those 2 lines.
PROMPT
	          printf '\n[Terminal Output Start]\n'
	          cat "$capture_file"
	          printf '\n[Terminal Output End]\n'
	        } > "$prompt_file"

        codex_args=(--allow-all-tools)
        if [ -n "$CODEX_MODEL" ]; then
          codex_args+=(--model "$CODEX_MODEL")
        fi

        prompt_text="$(cat "$prompt_file")"
        if run_llm_query "$prompt_file" "$decision_file"; then
          if [ ! -f "$decision_file" ]; then
            log "ERROR: decision file missing after codex exec (iteration=$iteration, file=$decision_file)"
            continue
          fi
          decision_text="$(tr -d '\r' < "$decision_file")"
          decision="$(printf '%s\n' "$decision_text" | grep -Eo 'SEND|SKIP' | head -n1 || true)"
          reason="$(printf '%s\n' "$decision_text" | sed -n 's/^REASON:[[:space:]]*//p' | head -n1)"
          if [ -z "$reason" ]; then
            reason="(no reason provided)"
          fi

          if [ "$decision" = "SEND" ]; then
            send_continue_message "$TMUX_SESSION" "$CONTINUE_MESSAGE"
            log "조치: $TMUX_SESSION에 메시지를 전송했습니다 (iteration=$iteration, reason=$reason)"
          elif [ "$decision" = "SKIP" ]; then
            log "정보: 건너뜀 (iteration=$iteration, reason=$reason)"
          else
            log "경고: 판정을 해석할 수 없습니다 (iteration=$iteration, raw=$(tr '\n' ' ' < "$decision_file" | sed 's/[[:space:]]\+/ /g' | cut -c1-120))"
          fi
	        else
	          log "오류: codex 실행 실패 (iteration=$iteration)"
	        fi
        fi
      fi
    else
      log "오류: tmux 패널 캡처 실패 (session=$TMUX_SESSION)"
    fi
  fi

  now_after="$(date +%s)"
  remaining=$(( end_ts - now_after ))
  if [ "$remaining" -le 0 ]; then
    break
  fi

  sleep_for="$INTERVAL_SECONDS"
  if [ "$remaining" -lt "$sleep_for" ]; then
    sleep_for="$remaining"
  fi

  sleep "$sleep_for"
done

log "감시 종료"
